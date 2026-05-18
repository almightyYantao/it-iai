package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/iai/vibedeploy/internal/k8sdriver"
	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/store"
)

// effectiveAllowCIDRs resolves the actual CIDR list that should reach the
// ingress middleware. A project pointing at a preset uses the preset's list;
// projects without a preset fall back to their own AllowCIDRs ("custom" mode).
// Errors fetching the preset are swallowed and we treat the project as having
// no restriction — preferable to accidentally locking out an app because the
// DB blipped.
func (s *Server) effectiveAllowCIDRs(ctx context.Context, p *model.Project) []string {
	if p.AccessPreset == nil || *p.AccessPreset == "" {
		return p.AllowCIDRs
	}
	pr, err := s.store.GetCIDRPreset(ctx, *p.AccessPreset)
	if err != nil || pr == nil {
		return nil
	}
	return pr.CIDRs
}

// normaliseCIDR validates a single CIDR or bare IP and returns the canonical
// `/32` (IPv4) or `/128` (IPv6) form for bare IPs. Empty input is filtered
// out by the caller. Used by both /v1/projects/:slug/access and the preset
// upsert endpoint so the two never disagree on what's "valid".
func normaliseCIDR(raw string) (string, error) {
	c := strings.TrimSpace(raw)
	if c == "" {
		return "", errors.New("empty")
	}
	if !strings.Contains(c, "/") {
		ip := net.ParseIP(c)
		if ip == nil {
			return "", errors.New("invalid IP or CIDR: " + raw)
		}
		if ip.To4() != nil {
			return c + "/32", nil
		}
		return c + "/128", nil
	}
	if _, _, err := net.ParseCIDR(c); err != nil {
		return "", errors.New("invalid CIDR: " + raw + " (" + err.Error() + ")")
	}
	return c, nil
}

// normaliseCIDRs runs normaliseCIDR over a slice and skips blank entries.
// Returns the first error verbatim so the UI can pinpoint the bad row.
func normaliseCIDRs(raws []string) ([]string, error) {
	out := make([]string, 0, len(raws))
	for _, raw := range raws {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		c, err := normaliseCIDR(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// errPresetIsSystem exposes the store error to the API layer without leaking
// the dependency in every handler.
var errPresetIsSystem = store.ErrPresetIsSystem

// syncIngressForProject rebuilds the project's ingress (default subdomain +
// custom domains) and reapplies the IP allow-list Middleware + the cluster-
// wide ForwardAuth chain when the project is non-public. Called from every
// code path that may have changed any of those inputs (domain add/remove,
// allow_cidrs save, deploy reconciler). Cheap: one ingress write + one CR upsert.
func (s *Server) syncIngressForProject(ctx context.Context, p *model.Project) error {
	if s.deployer == nil {
		return nil
	}
	hostnames := []string{p.Slug + "." + s.cfg.AppBaseDomain}
	if others, err := s.store.ListDomains(ctx, p.ID); err == nil {
		for _, o := range others {
			hostnames = append(hostnames, o.Hostname)
		}
	}
	// "public" means literally no auth gate — everyone on the network can hit
	// the URL. "org" and "restricted" both go behind oauth2-proxy; "restricted"
	// further requires a collaborator entry, but that's app-level, not L7.
	requireAuth := p.Visibility != model.VisibilityPublic
	return s.deployer.SyncIngressHostnames(ctx, k8sdriver.IngressHostsInput{
		Slug:        p.Slug,
		Hostnames:   hostnames,
		AllowCIDRs:  s.effectiveAllowCIDRs(ctx, p),
		RequireAuth: requireAuth,
	})
}

// PATCH /v1/projects/:slug/access
//
// Body: one of
//   { "preset": "internal" }                      // switch to preset mode
//   { "preset": null, "allow_cidrs": [...] }      // switch to custom mode
//
// Sending only `allow_cidrs` (legacy clients) is also accepted — preset is
// cleared and the project goes into custom mode automatically. Empty
// `allow_cidrs` in custom mode means "no IP restriction".
//
// Only the project owner or an admin may change this — collaborators
// explicitly cannot, because access policy is not a development concern.
func (s *Server) handlePatchProjectAccess(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can change access policy")
		return
	}

	// We need to know whether the client meant "set preset to null" versus
	// "didn't touch preset", so model the field as **string. Same for
	// allow_cidrs — nil slice = untouched.
	var body struct {
		Preset     *string  `json:"preset"`
		AllowCIDRs []string `json:"allow_cidrs"`
		// Sentinel sentinel for clients that explicitly want to keep allow_cidrs
		// untouched (omit allow_cidrs entirely).
	}
	// json.Decoder doesn't differentiate "missing key" from "key set to null"
	// for slices, so we peek at the raw JSON to figure out what the client sent.
	rawBody := map[string]any{}
	if err := decodeJSON(r, &rawBody); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	_, presetSent := rawBody["preset"]
	_, cidrsSent := rawBody["allow_cidrs"]
	if presetSent {
		if v, ok := rawBody["preset"].(string); ok && v != "" {
			s := v
			body.Preset = &s
		} else {
			body.Preset = nil
		}
	}
	if cidrsSent {
		if arr, ok := rawBody["allow_cidrs"].([]any); ok {
			for _, x := range arr {
				if s, ok := x.(string); ok {
					body.AllowCIDRs = append(body.AllowCIDRs, s)
				}
			}
		}
	}

	ctx := r.Context()

	// --- preset path -------------------------------------------------------
	// If the caller sent the preset field, that's the authoritative signal
	// for which mode the project is in. Setting preset to "" / null = custom
	// mode; a non-empty value points at a preset row.
	if presetSent {
		if body.Preset != nil {
			// Validate the preset exists; refusing here gives a clean error
			// instead of a silent FK violation deep in SetProjectAccessPreset.
			if _, err := s.store.GetCIDRPreset(ctx, *body.Preset); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					writeError(w, http.StatusBadRequest, "no_such_preset", "unknown preset: "+*body.Preset)
					return
				}
				writeError(w, http.StatusInternalServerError, "db", err.Error())
				return
			}
		}
		if err := s.store.SetProjectAccessPreset(ctx, p.ID, body.Preset); err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		p.AccessPreset = body.Preset
	} else if cidrsSent {
		// Legacy / custom-only callers: any allow_cidrs without a preset key
		// implies "drop the preset, this is custom mode now".
		p.AccessPreset = nil
		if err := s.store.SetProjectAccessPreset(ctx, p.ID, nil); err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
	}

	// --- custom CIDRs path -------------------------------------------------
	// Only persist allow_cidrs when the client actually sent them — saves
	// callers from having to re-send the current list when they're only
	// flipping a preset.
	if cidrsSent {
		cleaned, err := normaliseCIDRs(body.AllowCIDRs)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_cidr", err.Error())
			return
		}
		if err := s.store.SetProjectAllowCIDRs(ctx, p.ID, cleaned); err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		p.AllowCIDRs = cleaned
	}

	if err := s.syncIngressForProject(ctx, p); err != nil {
		// Persisted but cluster failed — surface it so the operator knows the
		// policy isn't enforced yet.
		writeError(w, http.StatusInternalServerError, "ingress_sync", err.Error())
		return
	}

	meta := map[string]any{}
	if presetSent {
		meta["preset"] = body.Preset
	}
	if cidrsSent {
		meta["allow_cidrs"] = p.AllowCIDRs
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.access.update", &p.ID, meta)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"preset":      p.AccessPreset,
		"allow_cidrs": p.AllowCIDRs,
	})
}

// DELETE /v1/projects/:slug
//
// Soft-deletes the project (deleted_at + status=deleting) and tears down the
// K8s namespace. Owner or admin only. Audit row preserved.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can delete this project")
		return
	}
	ctx := r.Context()

	// Tear down the cluster first; if it fails we still mark the row deleted so
	// the project disappears from the UI, and the orphaned namespace can be
	// hand-cleaned. Keeping a half-deleted row visible would be worse than
	// having a stale namespace.
	if s.deployer != nil {
		if err := s.deployer.DeleteProject(ctx, p.Slug); err != nil {
			s.store.WriteAudit(ctx, "system", "delete-cleanup", "project.delete.k8s_failed",
				&p.ID, map[string]string{"error": err.Error()})
		}
	}

	// Best-effort sidecar cleanup. Logged as audit on failure but doesn't
	// block the delete — an orphaned DB on user-postgres is recoverable
	// (admin can DROP DATABASE by hand) and far less bad than a project
	// stuck in "can't delete" state from the user's perspective.
	if s.pgProv.Enabled() {
		if err := s.pgProv.Deprovision(ctx, p.Slug); err != nil {
			s.store.WriteAudit(ctx, "system", "delete-cleanup", "project.delete.pg_failed",
				&p.ID, map[string]string{"error": err.Error()})
		}
	}
	if s.redisProv.Enabled() {
		if err := s.redisProv.Deprovision(ctx, p.Slug); err != nil {
			s.store.WriteAudit(ctx, "system", "delete-cleanup", "project.delete.redis_failed",
				&p.ID, map[string]string{"error": err.Error()})
		}
	}
	if s.s3Prov.Enabled() {
		// S3 deprovision drops the user + policy but intentionally leaves the
		// bucket — see provision/s3.go. Operators clean it up manually after
		// confirming the project is truly gone (an `mc rb --force` undoes
		// possibly years of stored objects).
		if err := s.s3Prov.Deprovision(ctx, p.Slug); err != nil {
			s.store.WriteAudit(ctx, "system", "delete-cleanup", "project.delete.s3_failed",
				&p.ID, map[string]string{"error": err.Error()})
		}
	}
	// Clear encrypted env so we don't keep secrets attached to a tombstoned
	// project row. Best-effort — soft-delete should still succeed even if
	// the env wipe fails (the audit trail records the delete either way).
	_ = s.store.DeleteAllProjectEnv(ctx, p.ID)

	if err := s.store.SoftDeleteProject(ctx, p.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.delete", &p.ID, map[string]string{"slug": p.Slug})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
