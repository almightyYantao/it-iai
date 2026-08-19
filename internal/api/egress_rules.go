package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/k8sdriver"
	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/store"
)

// Ports on the platform host that an egress rule must never re-open. The
// per-project policy denies these deliberately (unauthenticated image registry,
// the control-plane's own database, its API, the admin UI's origin), and an
// endpoint rule pointing at them would quietly undo that from the other side.
//
// Not a substitute for the deny — it's here so a well-meaning admin pasting
// "unblock 5432 for this project" can't hand a workload the platform database.
var egressForbiddenPorts = map[int]string{
	5001: "image registry (unauthenticated)",
	5432: "control-plane database",
	8080: "control-plane API — use the platform hostname over 443 instead",
	5173: "admin UI origin — use the platform hostname over 443 instead",
}

// GET /v1/projects/:slug/egress
//
// Readable by anyone who can see the project: an owner debugging a blocked
// outbound call needs to know what is allowed, even though they cannot change
// it. Writing is admin-only (see handlePutProjectEgress).
func (s *Server) handleListProjectEgress(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	rules, err := s.store.ListProjectEgressRules(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if rules == nil {
		rules = []*model.ProjectEgressRule{}
	}
	actor, _ := ActorFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"rules": rules,
		// Lets the UI render the panel read-only instead of showing controls
		// that will 403 on save.
		"can_edit": isAdminActor(actor),
	})
}

// PUT /v1/projects/:slug/egress
//
// Body: {"rules": [{"kind":"domain","value":"api.anthropic.com","port":443,"note":"…"}, …]}
//
// Replaces the whole list. Admin scope only — this is the allow-list that pod
// egress is otherwise denied by, so project owners deliberately cannot widen
// their own.
func (s *Server) handlePutProjectEgress(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin",
			"only platform admins can change a project's egress allow-list")
		return
	}
	p := projectFrom(r.Context())

	var body struct {
		Rules []struct {
			Kind  string `json:"kind"`
			Value string `json:"value"`
			Port  int    `json:"port"`
			Note  string `json:"note"`
		} `json:"rules"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	seen := map[string]struct{}{}
	out := make([]store.EgressRuleInput, 0, len(body.Rules))
	for i, in := range body.Rules {
		rule, err := normaliseEgressRule(in.Kind, in.Value, in.Port)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_rule",
				"rule "+strconv.Itoa(i+1)+": "+err.Error())
			return
		}
		rule.Note = strings.TrimSpace(in.Note)
		key := rule.Kind + "|" + rule.Value + "|" + strconv.Itoa(rule.Port)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, rule)
	}

	var by *uuid.UUID
	if actor.Kind == actorUser && actor.UserID != uuid.Nil {
		id := actor.UserID
		by = &id
	}
	if err := s.store.ReplaceProjectEgressRules(r.Context(), p.ID, out, by); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// Endpoint rules live in the project's NetworkPolicy, so they only take
	// effect once it is rewritten. Domains are the proxy's business and are
	// picked up when its config is re-rendered.
	if err := s.syncEgressForProject(r.Context(), p); err != nil {
		s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(),
			"project.egress.sync_failed", &p.ID, map[string]string{"error": err.Error()})
		writeError(w, http.StatusInternalServerError, "egress_sync", err.Error())
		return
	}

	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(),
		"project.egress.update", &p.ID, map[string]any{"rules": len(out)})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "rules": len(out)})
}

// GET /v1/admin/egress — one line per live project, so an admin can see at a
// glance which projects have been opened up and which are still sealed.
func (s *Server) handleAdminEgressOverview(w http.ResponseWriter, r *http.Request) {
	counts, err := s.store.CountEgressRulesByProject(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if counts == nil {
		counts = []store.EgressRuleCount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": counts})
}

// normaliseEgressRule validates one rule and returns it in canonical form.
//
// The two kinds are checked against different things on purpose, and the errors
// say why: the most likely mistake is a hostname entered as an endpoint (or an
// address entered as a domain), which would be accepted by a laxer check and
// then silently never match anything.
func normaliseEgressRule(kind, value string, port int) (store.EgressRuleInput, error) {
	v := strings.ToLower(strings.TrimSpace(value))
	switch kind {
	case model.EgressKindDomain:
		if port == 0 {
			port = 443
		}
		if port != 80 && port != 443 {
			return store.EgressRuleInput{}, errors.New(
				"domain rules only apply to 80 and 443 — the proxy speaks HTTP; " +
					"for another port use an endpoint rule")
		}
		d, err := normaliseEgressDomain(v)
		if err != nil {
			return store.EgressRuleInput{}, err
		}
		return store.EgressRuleInput{Kind: kind, Value: d, Port: port}, nil

	case model.EgressKindEndpoint:
		if port < 1 || port > 65535 {
			return store.EgressRuleInput{}, errors.New("endpoint rules need a port between 1 and 65535")
		}
		if what, bad := egressForbiddenPorts[port]; bad {
			return store.EgressRuleInput{}, errors.New(
				"port " + strconv.Itoa(port) + " cannot be allowed: " + what)
		}
		// Reuse the same normaliser the IP allow-list uses so the two features
		// never disagree on what a valid address looks like.
		c, err := normaliseCIDR(v)
		if err != nil {
			return store.EgressRuleInput{}, errors.New(
				err.Error() + " — endpoint rules take an IP or CIDR, not a hostname " +
					"(NetworkPolicy cannot match names; if this is an HTTP service, add it as a domain)")
		}
		return store.EgressRuleInput{Kind: kind, Value: c, Port: port}, nil
	}
	return store.EgressRuleInput{}, errors.New(`kind must be "domain" or "endpoint"`)
}

// normaliseEgressDomain accepts a hostname, optionally with a leading dot to
// include subdomains. Rejects anything carrying a scheme, path, port or wildcard
// character — all of which are ways of writing a domain that the proxy would not
// match, so failing loudly beats storing a rule that never fires.
func normaliseEgressDomain(v string) (string, error) {
	if v == "" {
		return "", errors.New("empty domain")
	}
	if strings.ContainsAny(v, "/:? ") {
		return "", errors.New("just the hostname: no scheme, port or path (e.g. api.anthropic.com)")
	}
	if strings.Contains(v, "*") {
		return "", errors.New(`use a leading dot for subdomains (".anthropic.com"), not "*"`)
	}
	if net.ParseIP(strings.TrimPrefix(v, ".")) != nil {
		return "", errors.New("that's an address, not a hostname — add it as an endpoint rule instead")
	}
	bare := strings.TrimPrefix(v, ".")
	if !strings.Contains(bare, ".") {
		return "", errors.New("needs at least one dot (e.g. example.com)")
	}
	for _, label := range strings.Split(bare, ".") {
		if label == "" {
			return "", errors.New("empty label in domain")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
				return "", errors.New("invalid character in domain: " + string(c))
			}
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("domain labels cannot start or end with '-'")
		}
	}
	return v, nil
}

// egressAllowsForProject maps the project's stored endpoint rules into the shape
// the deployer's NetworkPolicy builder wants. Domain rules are skipped here by
// design — they are the egress proxy's business, and NetworkPolicy has no way to
// express them.
func (s *Server) egressAllowsForProject(ctx context.Context, projectID uuid.UUID) ([]k8sdriver.EgressAllow, error) {
	rules, err := s.store.ListProjectEgressRules(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]k8sdriver.EgressAllow, 0, len(rules))
	for _, r := range rules {
		if r.Kind != model.EgressKindEndpoint {
			continue
		}
		out = append(out, k8sdriver.EgressAllow{CIDR: r.Value, Port: int32(r.Port)})
	}
	return out, nil
}

// syncEgressForProject pushes the endpoint half of the allow-list into the
// cluster. No-op when the deployer is disabled (dev without a cluster).
func (s *Server) syncEgressForProject(ctx context.Context, p *model.Project) error {
	if s.deployer == nil {
		return nil
	}
	allows, err := s.egressAllowsForProject(ctx, p.ID)
	if err != nil {
		return err
	}
	return s.deployer.SyncEgressPolicy(ctx, p.Slug, allows)
}
