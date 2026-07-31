package api

import (
	"context"
	"errors"
	"log"
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

// SweepIngressesAtStartup re-applies the ingress + middleware for every live
// project once at startup, so any DB → cluster drift introduced by a migration
// (most commonly the tls_enabled default flip in 0008) gets reconciled without
// waiting for a redeploy or a manual toggle. Idempotent — hitting an
// already-correct ingress just writes the same yaml.
//
// Failures on a single project are logged and skipped; the sweep keeps going
// so one broken project can't strand the rest.
func (s *Server) SweepIngressesAtStartup(ctx context.Context) {
	if s.deployer == nil {
		return
	}
	projects, err := s.store.ListProjectsForIngressSweep(ctx)
	if err != nil {
		log.Printf("ingress sweep: list projects: %v", err)
		return
	}
	if len(projects) == 0 {
		return
	}
	log.Printf("ingress sweep: reconciling %d project(s)", len(projects))
	var ok, fail int
	for _, p := range projects {
		if err := s.syncIngressForProject(ctx, p); err != nil {
			fail++
			log.Printf("ingress sweep: %s: %v", p.Slug, err)
			continue
		}
		ok++
	}
	log.Printf("ingress sweep: done (%d ok, %d failed)", ok, fail)
}

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

	// Path-prefix overrides ride on a separate IngressRoute. Best-effort: a
	// failure to load rules shouldn't strand the ingress sync — log via the
	// audit channel and proceed with no rules (which keeps the project's
	// default visibility in force, the safe fallback).
	var pathRules []k8sdriver.PathRule
	if rules, err := s.store.ListProjectPathRules(ctx, p.ID); err == nil {
		for _, r := range rules {
			pathRules = append(pathRules, k8sdriver.PathRule{
				PathPrefix: r.PathPrefix,
				Mode:       r.Mode,
			})
		}
	}

	return s.deployer.SyncIngressHostnames(ctx, k8sdriver.IngressHostsInput{
		Slug:        p.Slug,
		Hostnames:   hostnames,
		AllowCIDRs:  s.effectiveAllowCIDRs(ctx, p),
		RequireAuth: requireAuth,
		TLSEnabled:  p.TLSEnabled,
		PathRules:   pathRules,
	})
}

// accessPolicySelfService gates whether a project owner may change their own
// inbound access policy (SSO scope + IP allow-list).
//
// Turned off: the access-control panel was withdrawn from the project page, and
// hiding UI alone would just move the same capability behind a raw API call.
// Inbound policy is now owned centrally — admins maintain the named CIDR presets
// (PUT /v1/admin/cidr-presets/{name}), which re-syncs every project bound to
// them, and new projects default to the `internal` preset.
//
// Flip to true to restore self-service; the handler bodies are untouched.
const accessPolicySelfService = false

func accessPolicyFrozen(w http.ResponseWriter, what string) {
	writeError(w, http.StatusForbidden, "access_policy_frozen",
		"per-project "+what+" can no longer be changed through the API — "+
			"inbound access policy is maintained by platform admins via the global CIDR presets")
}

// PATCH /v1/projects/:slug/access
//
// Body: one of
//
//	{ "preset": "internal" }                      // switch to preset mode
//	{ "preset": null, "allow_cidrs": [...] }      // switch to custom mode
//
// Sending only `allow_cidrs` (legacy clients) is also accepted — preset is
// cleared and the project goes into custom mode automatically. Empty
// `allow_cidrs` in custom mode means "no IP restriction".
//
// Frozen — see accessPolicySelfService. When self-service was on, only the
// project owner or an admin could change this; collaborators explicitly could
// not, because access policy is not a development concern.
func (s *Server) handlePatchProjectAccess(w http.ResponseWriter, r *http.Request) {
	if !accessPolicySelfService {
		accessPolicyFrozen(w, "IP allow-list / access preset")
		return
	}
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

// PATCH /v1/projects/:slug/name
//
// Body: { "name": "New display name" }
//
// Renames the project's display name. The slug (URL fragment) stays fixed —
// changing that would break existing deploy tokens, ingresses, and bookmarks.
// Owner or admin only. Trims whitespace; rejects empty after trim, and caps
// at 120 chars to keep the table layout sane.
func (s *Server) handlePatchProjectName(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can rename this project")
		return
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "empty_name", "name cannot be empty")
		return
	}
	if len([]rune(name)) > 120 {
		writeError(w, http.StatusBadRequest, "name_too_long", "name must be 120 characters or fewer")
		return
	}

	ctx := r.Context()
	if err := s.store.SetProjectName(ctx, p.ID, name); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.name.update", &p.ID, map[string]any{"old": p.Name, "new": name})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"name": name,
	})
}

// PATCH /v1/projects/:slug/tls
//
// Body: { "enabled": true|false }
//
// Flips the per-project HTTPS opt-in. When turning on, the reconciler adds a
// cert-manager.io/cluster-issuer annotation to the Ingress and a tls: section
// listing every hostname; cert-manager solves an HTTP-01 challenge per host
// and stores the resulting cert in iai-tls-<slug>. When turning off, both
// the annotation and the tls section are removed and cert-manager garbage-
// collects the Certificate.
//
// Only the project owner or an admin may change this. Cert issuance is
// asynchronous — clients should poll project status / browser-test the URL
// to confirm the cert is live (typically ~30s for a fresh HTTP-01).
func (s *Server) handlePatchProjectTLS(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can change the TLS setting")
		return
	}

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}

	ctx := r.Context()
	if err := s.store.SetProjectTLSEnabled(ctx, p.ID, body.Enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	p.TLSEnabled = body.Enabled

	if err := s.syncIngressForProject(ctx, p); err != nil {
		writeError(w, http.StatusInternalServerError, "ingress_sync", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.tls.update", &p.ID, map[string]any{"enabled": body.Enabled})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"tls_enabled": p.TLSEnabled,
	})
}

// PATCH /v1/projects/:slug/visibility
//
// Body: { "visibility": "org" | "restricted" }
//
// Switches the SSO gating mode for the project:
//
//   - org        traffic must carry a valid Longbridge OIDC cookie (any
//     Keycloak-issued user passes).
//   - restricted same as org plus an app-level collaborator check.
//
// "public" (no auth gate at all) is no longer selectable — the platform is
// internal-access-only. To let a specific machine caller through without SSO,
// use a per-project API token plus a path rule instead of opening the whole
// app; see handleCreatePathRule.
//
// Owner or admin only. Triggers an immediate ingress re-sync so the
// ForwardAuth middleware is added or removed without waiting for a redeploy.
func (s *Server) handlePatchProjectVisibility(w http.ResponseWriter, r *http.Request) {
	// Visibility (the SSO scope) was rendered in the same withdrawn panel, so it
	// is frozen alongside the allow-list — see accessPolicySelfService.
	if !accessPolicySelfService {
		accessPolicyFrozen(w, "visibility")
		return
	}
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can change visibility")
		return
	}

	var body struct {
		Visibility string `json:"visibility"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	v := model.Visibility(body.Visibility)
	switch v {
	case model.VisibilityOrg, model.VisibilityRestricted:
	case model.VisibilityPublic:
		// Deliberately a distinct message: callers that used to send "public"
		// should know it was withdrawn on purpose, not that they typo'd.
		writeError(w, http.StatusBadRequest, "visibility_public_withdrawn",
			"public visibility is no longer available — the platform is internal-access-only. "+
				"Use a project API token + path rule to exempt a specific machine caller.")
		return
	default:
		writeError(w, http.StatusBadRequest, "bad_visibility", "visibility must be one of: org, restricted")
		return
	}

	ctx := r.Context()
	old := p.Visibility
	if err := s.store.SetProjectVisibility(ctx, p.ID, v); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	p.Visibility = v

	if err := s.syncIngressForProject(ctx, p); err != nil {
		writeError(w, http.StatusInternalServerError, "ingress_sync", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.visibility.update", &p.ID, map[string]any{"old": string(old), "new": string(v)})

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"visibility": string(v),
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
