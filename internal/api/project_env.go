package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/k8sdriver"
	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/store"
)

// actorUserIDPtr returns a *uuid.UUID for user actors, nil otherwise.
// `updated_by` columns are nullable so token-driven changes can pass nil
// (the audit log has the token id separately).
func actorUserIDPtr(a Actor) *uuid.UUID {
	if a.Kind == actorUser {
		id := a.UserID
		return &id
	}
	return nil
}

// syncProjectEnvToCluster rewrites the project's app-env Secret so updated
// env values reach the running pod without waiting for the next deploy.
// K8s rolls the pod on Secret change automatically.
//
// Reads the manifest's env from the most recent deployment as the base
// layer, then overlays the live project_env values on top (project_env wins,
// so admins can override what's in the user's git).
func (s *Server) syncProjectEnvToCluster(ctx context.Context, p *model.Project) error {
	if s.deployer == nil {
		return nil
	}
	manifestEnv := map[string]string{}
	var m model.Manifest
	if len(p.Manifest) > 0 {
		_ = json.Unmarshal(p.Manifest, &m)
	}
	for k, v := range m.Env {
		manifestEnv[k] = v
	}
	dbEnv, err := s.store.GetProjectEnv(ctx, p.ID, s.kek)
	if err != nil {
		return err
	}
	for k, v := range dbEnv {
		manifestEnv[k] = v
	}
	return s.deployer.UpdateAppEnvSecret(ctx, k8sdriver.AppEnvSecretInput{
		Slug: p.Slug,
		Env:  manifestEnv,
	})
}

// Env-var keys are POSIX-shell-safe identifiers — letters / digits / _, can't
// start with a digit. We enforce this server-side so a typo (`PATH+EXTRA=...`)
// doesn't make it into the pod's env where it'd be silently ignored.
var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// GET /v1/projects/:slug/env
// Returns metadata for every env key on the project. Values are NOT included —
// reading a secret back is intentionally not possible (force the operator to
// re-enter on rotation, prevents shoulder-surfing in screen-sharing demos).
func (s *Server) handleListProjectEnv(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised",
			"only the project owner or an admin can view env keys")
		return
	}
	list, err := s.store.ListProjectEnvKeys(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if list == nil {
		list = []store.ProjectEnvEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"env": list})
}

// PUT /v1/projects/:slug/env
// Body: { "key": "DATABASE_URL", "value": "postgres://..." }
//
// Upserts. Value is encrypted with the platform KEK before going to the DB.
// Refuses to overwrite system-managed keys (their values come from the
// platform's provisioning code; a user PUT here would be silently replaced
// on next deploy — surface as a 409 to remove the ambiguity).
func (s *Server) handlePutProjectEnv(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised",
			"only the project owner or an admin can change env vars")
		return
	}
	var body struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	key := strings.TrimSpace(body.Key)
	if !envKeyRe.MatchString(key) {
		writeError(w, http.StatusBadRequest, "bad_key",
			"env key must match [A-Za-z_][A-Za-z0-9_]{0,127}")
		return
	}

	// Reject overwriting system keys — the rule above kicks in for inserts too,
	// but checking here gives a clearer 409 vs ambiguous DB error.
	existing, err := s.store.ListProjectEnvKeys(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	for _, e := range existing {
		if e.Key == key && e.System {
			writeError(w, http.StatusConflict, "system_env",
				"this key is managed by the platform and can't be edited manually")
			return
		}
	}

	if err := s.store.SetProjectEnv(r.Context(), p.ID, key, body.Value, false, s.kek, actorUserIDPtr(actor)); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// Re-sync the pod env so the new value reaches the running container without
	// a full redeploy. Cheap: K8s rolls the pod on Secret change automatically.
	if err := s.syncProjectEnvToCluster(r.Context(), p); err != nil {
		// Persisted, but cluster failed — leave the user a clear note. Next
		// deploy will pick it up regardless.
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":          true,
			"warn_resync": err.Error(),
		})
		return
	}

	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(),
		"project.env.set", &p.ID, map[string]any{"key": key})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// DELETE /v1/projects/:slug/env/:key
func (s *Server) handleDeleteProjectEnv(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised",
			"only the project owner or an admin can remove env vars")
		return
	}
	key := chi.URLParam(r, "key")
	if !envKeyRe.MatchString(key) {
		writeError(w, http.StatusBadRequest, "bad_key", "invalid env key")
		return
	}
	if err := s.store.DeleteProjectEnv(r.Context(), p.ID, key); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such env key")
			return
		}
		if errors.Is(err, store.ErrSystemEnv) {
			writeError(w, http.StatusConflict, "system_env",
				"this key is managed by the platform and can't be removed manually")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	_ = s.syncProjectEnvToCluster(r.Context(), p)

	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(),
		"project.env.delete", &p.ID, map[string]any{"key": key})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
