package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/store"
)

// Slug rules for preset names match what the API URL can take without
// encoding. Keeps lookup simple and avoids `internal/foo` ambiguity.
var presetNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,30}$`)

// GET /v1/admin/cidr-presets — list everything. Returns the system presets
// (public/internal) plus any admin-created ones.
func (s *Server) handleListCIDRPresets(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	list, err := s.store.ListCIDRPresets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if list == nil {
		list = []*model.CIDRPreset{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"presets": list})
}

// PUT /v1/admin/cidr-presets/{name}
// Body: { "label": "...", "description": "...", "cidrs": ["10.0.0.0/8"] }
//
// Creates the preset if missing, otherwise updates label/description/cidrs in
// place. is_system is server-controlled — only the migration sets it, never
// the API.
func (s *Server) handlePutCIDRPreset(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if !presetNamePattern.MatchString(name) {
		writeError(w, http.StatusBadRequest, "bad_name",
			"preset name must match [a-z0-9][a-z0-9_-]{0,30}")
		return
	}

	var body struct {
		Label       string   `json:"label"`
		Description string   `json:"description"`
		CIDRs       []string `json:"cidrs"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if strings.TrimSpace(body.Label) == "" {
		writeError(w, http.StatusBadRequest, "bad_label", "label required")
		return
	}
	cleaned, err := normaliseCIDRs(body.CIDRs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_cidr", err.Error())
		return
	}

	ctx := r.Context()
	var updatedBy = func() *struct{} { return nil }() // placeholder for type
	_ = updatedBy
	preset := &model.CIDRPreset{
		Name:        name,
		Label:       strings.TrimSpace(body.Label),
		Description: strings.TrimSpace(body.Description),
		CIDRs:       cleaned,
	}
	if actor.Kind == actorUser {
		id := actor.UserID
		if err := s.store.UpsertCIDRPreset(ctx, preset, &id); err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
	} else {
		if err := s.store.UpsertCIDRPreset(ctx, preset, nil); err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
	}

	// Re-sync every project pointing at this preset so the new CIDR list
	// reaches the cluster without each project owner having to redeploy.
	if err := s.resyncProjectsUsingPreset(ctx, name); err != nil {
		// Persisted, but cluster fan-out failed — surface the warning. The
		// preset itself is saved, so the worst case is admins re-PATCH later.
		writeJSON(w, http.StatusOK, map[string]any{
			"preset":      preset,
			"warn_resync": err.Error(),
		})
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"cidr_preset.update", nil, map[string]any{"name": name, "cidrs": cleaned})

	writeJSON(w, http.StatusOK, map[string]any{"preset": preset})
}

// DELETE /v1/admin/cidr-presets/{name}
// System presets (public/internal) refuse with 409.
func (s *Server) handleDeleteCIDRPreset(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	name := chi.URLParam(r, "name")
	if err := s.store.DeleteCIDRPreset(r.Context(), name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "no such preset")
			return
		}
		if errors.Is(err, store.ErrPresetIsSystem) {
			writeError(w, http.StatusConflict, "system_preset",
				"system presets (public/internal) cannot be deleted")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// FK is ON DELETE SET NULL, so projects pointing at this preset now have
	// access_preset = NULL → they fall back to custom mode with their own
	// (possibly empty) allow_cidrs. Re-sync them so the cluster sees the
	// change without each owner having to redeploy.
	_ = s.resyncProjectsUsingPreset(r.Context(), name)

	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(),
		"cidr_preset.delete", nil, map[string]any{"name": name})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// resyncProjectsUsingPreset walks every project currently bound to the named
// preset and re-runs the ingress sync. Called after the preset's CIDR list
// changes so the cluster reflects the new policy without round-tripping
// through each project owner.
//
// Cheap: most setups have <100 projects and each sync is a single ingress +
// middleware write. Sequential to keep the audit trail readable.
func (s *Server) resyncProjectsUsingPreset(ctx context.Context, name string) error {
	rows, err := s.store.Pool.Query(ctx,
		`SELECT id FROM projects WHERE deleted_at IS NULL AND access_preset = $1`, name)
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()

	var firstErr error
	for _, id := range ids {
		p, err := s.store.GetProjectByID(ctx, id)
		if err != nil || p == nil {
			continue
		}
		if err := s.syncIngressForProject(ctx, p); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
