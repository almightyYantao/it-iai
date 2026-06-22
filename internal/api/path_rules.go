package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/store"
)

// GET /v1/projects/:slug/path-rules — list all path-prefix auth overrides.
// Read-allowed for anyone with project access (owners, collaborators, admin);
// the rules aren't secret — only the API token is.
func (s *Server) handleListPathRules(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	rules, err := s.store.ListProjectPathRules(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if rules == nil {
		rules = []*model.ProjectPathRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// POST /v1/projects/:slug/path-rules — create a new rule. Body:
//
//	{ "path_prefix": "/api/webhook/", "mode": "no_auth" }
//
// Owner or admin only.
func (s *Server) handleCreatePathRule(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can edit path rules")
		return
	}

	var body struct {
		PathPrefix string `json:"path_prefix"`
		Mode       string `json:"mode"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	prefix, mode, errMsg := normalisePathRuleInput(body.PathPrefix, body.Mode)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, "bad_input", errMsg)
		return
	}

	ctx := r.Context()
	rule, err := s.store.CreateProjectPathRule(ctx, p.ID, prefix, mode)
	if err != nil {
		if errors.Is(err, store.ErrPathRuleTaken) {
			writeError(w, http.StatusConflict, "duplicate_prefix", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	if err := s.syncIngressForProject(ctx, p); err != nil {
		writeError(w, http.StatusInternalServerError, "ingress_sync", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.path_rule.create", &p.ID, map[string]any{"prefix": prefix, "mode": mode})

	writeJSON(w, http.StatusCreated, map[string]any{"rule": rule})
}

// PATCH /v1/projects/:slug/path-rules/:id — update prefix or mode of an
// existing rule. Owner or admin only.
func (s *Server) handleUpdatePathRule(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can edit path rules")
		return
	}
	ruleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", err.Error())
		return
	}

	var body struct {
		PathPrefix string `json:"path_prefix"`
		Mode       string `json:"mode"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	prefix, mode, errMsg := normalisePathRuleInput(body.PathPrefix, body.Mode)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, "bad_input", errMsg)
		return
	}

	ctx := r.Context()
	rule, err := s.store.UpdateProjectPathRule(ctx, p.ID, ruleID, prefix, mode)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "rule not found")
			return
		}
		if errors.Is(err, store.ErrPathRuleTaken) {
			writeError(w, http.StatusConflict, "duplicate_prefix", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	if err := s.syncIngressForProject(ctx, p); err != nil {
		writeError(w, http.StatusInternalServerError, "ingress_sync", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.path_rule.update", &p.ID, map[string]any{"id": ruleID.String(), "prefix": prefix, "mode": mode})

	writeJSON(w, http.StatusOK, map[string]any{"rule": rule})
}

// DELETE /v1/projects/:slug/path-rules/:id — owner or admin only.
func (s *Server) handleDeletePathRule(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can edit path rules")
		return
	}
	ruleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", err.Error())
		return
	}

	ctx := r.Context()
	if err := s.store.DeleteProjectPathRule(ctx, p.ID, ruleID); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	if err := s.syncIngressForProject(ctx, p); err != nil {
		writeError(w, http.StatusInternalServerError, "ingress_sync", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.path_rule.delete", &p.ID, map[string]any{"id": ruleID.String()})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// normalisePathRuleInput trims, validates, and canonicalises the request
// body. Returns (prefix, mode, errMsg) — non-empty errMsg means reject.
func normalisePathRuleInput(rawPrefix, rawMode string) (prefix, mode, errMsg string) {
	prefix = strings.TrimSpace(rawPrefix)
	if prefix == "" {
		return "", "", "path_prefix is required"
	}
	if !strings.HasPrefix(prefix, "/") {
		return "", "", "path_prefix must start with '/'"
	}
	if strings.Contains(prefix, "//") || strings.Contains(prefix, " ") {
		return "", "", "path_prefix is malformed"
	}
	if len(prefix) > 200 {
		return "", "", "path_prefix is too long (max 200 chars)"
	}

	mode = strings.TrimSpace(rawMode)
	if mode != model.PathRuleModeNoAuth && mode != model.PathRuleModeToken {
		return "", "", "mode must be one of: no_auth, token"
	}
	return prefix, mode, ""
}
