package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/scan"
	"github.com/iai/vibedeploy/internal/store"
)

type ctxProjectKey int

const ctxProject ctxProjectKey = 1

func projectFrom(ctx context.Context) *model.Project {
	p, _ := ctx.Value(ctxProject).(*model.Project)
	return p
}

// requireProjectAccess loads the project and enforces visibility / token-scope rules.
func (s *Server) requireProjectAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		slug := chi.URLParam(r, "slug")
		p, err := s.store.GetProjectBySlug(ctx, slug)
		if err != nil {
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "not_found", "project not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
		actor, _ := ActorFrom(ctx)
		if !s.canAccessProject(ctx, actor, p) {
			writeError(w, http.StatusForbidden, "forbidden", "no access to this project")
			return
		}
		p.URL = s.projectURL(p.Slug)
		ctx = context.WithValue(ctx, ctxProject, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// canAccessProject decides whether the actor may read/write a specific project
// via the admin API.
//
//	Tokens:
//	  - platform-wide (project_id NULL) → all projects
//	  - project-scoped → only that project
//	Users:
//	  - admin → all
//	  - owner → own projects
//	  - collaborator → projects they're added to
//	  (visibility=org/public is about hitting the deployed app's URL via
//	  oauth2-proxy, NOT about who sees admin entries — see /internal/auth/check)
func (s *Server) canAccessProject(ctx context.Context, a Actor, p *model.Project) bool {
	switch a.Kind {
	case actorToken:
		if a.TokenProjectID == nil {
			return true
		}
		return *a.TokenProjectID == p.ID
	case actorUser:
		if a.IsAdmin {
			return true
		}
		if p.OwnerID == a.UserID {
			return true
		}
		ok, _ := s.store.IsCollaborator(ctx, p.ID, a.UserID)
		return ok
	}
	return false
}

type createProjectRequest struct {
	Slug     string          `json:"slug,omitempty"`
	Name     string          `json:"name,omitempty"`
	Manifest json.RawMessage `json:"manifest"`
}

type createProjectResponse struct {
	Project *model.Project `json:"project"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actor, _ := ActorFrom(ctx)
	if actor.Kind == actorToken && actor.TokenProjectID != nil {
		writeError(w, http.StatusForbidden, "scope", "project-scoped tokens cannot create new projects")
		return
	}

	var req createProjectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	// Decode manifest to validate + extract name fallback.
	var m model.Manifest
	if len(req.Manifest) > 0 {
		if err := json.Unmarshal(req.Manifest, &m); err != nil {
			writeError(w, http.StatusBadRequest, "bad_manifest", err.Error())
			return
		}
	}

	slug := strings.ToLower(strings.TrimSpace(req.Slug))
	if slug == "" {
		base := strings.ToLower(req.Name)
		if base == "" {
			base = m.Name
		}
		if base == "" {
			base = "app"
		}
		slug = sanitizeForSlug(base) + "-" + scan.RandomSuffix(4)
	}
	if err := scan.ValidateSlug(slug); err != nil {
		writeError(w, http.StatusBadRequest, "bad_slug", err.Error())
		return
	}
	if ok, err := s.store.SlugAvailable(ctx, slug); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	} else if !ok {
		writeError(w, http.StatusConflict, "slug_taken", "slug already in use")
		return
	}

	// We need an owner. For deploy-token-created projects in dev, use a synthetic
	// "platform" user (idempotent upsert by email).
	ownerID, err := s.actorUserID(ctx, actor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "owner", err.Error())
		return
	}

	name := req.Name
	if name == "" {
		name = m.Name
	}
	if name == "" {
		name = slug
	}

	p, err := s.store.CreateProject(ctx, slug, name, ownerID, req.Manifest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	p.URL = s.projectURL(p.Slug)
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(), "project.create", &p.ID, nil)
	writeJSON(w, http.StatusCreated, createProjectResponse{Project: p})
}

func (a Actor) identityString() string {
	if a.Kind == actorUser {
		return a.UserID.String()
	}
	return a.TokenID.String()
}

func (s *Server) actorUserID(ctx context.Context, a Actor) (uuid.UUID, error) {
	if a.Kind == actorUser {
		return a.UserID, nil
	}
	// Synthesise a placeholder owner for deploy-token created projects.
	u, err := s.store.UpsertUserByEmail(ctx, "platform@vibedeploy.local", "platform")
	if err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	actor, _ := ActorFrom(ctx)
	userID, err := s.actorUserID(ctx, actor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "owner", err.Error())
		return
	}
	// /v1/projects always means "mine" (owner + collaborator). Admins who
	// want the full cluster view use /v1/admin/projects.
	list, err := s.store.ListProjectsForUser(ctx, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	for _, p := range list {
		p.URL = s.projectURL(p.Slug)
	}
	// Apply pagination in-memory. Mine lists are small in practice (a single
	// user rarely owns hundreds of projects); not worth a second SQL round trip.
	total := len(list)
	limit, offset := parsePager(r, 10, 200)
	end := offset + limit
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}
	page := list[offset:end]
	writeJSON(w, http.StatusOK, map[string]any{
		"projects": page,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	resp := map[string]any{"project": p}

	// Best-effort: surface the node + pod the current Deployment is running on
	// so the admin UI can show "running on it-iai-k3s-worker-1" instead of
	// leaving the user to ssh in and run kubectl. Cluster-read errors are
	// silent — the project endpoint must keep working even when the K8s API
	// is briefly unreachable.
	if s.deployer != nil {
		if pl, err := s.deployer.PodPlacement(r.Context(), p.Slug); err == nil && pl != nil {
			resp["pod"] = pl
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	switch actor.Kind {
	case actorUser:
		writeJSON(w, http.StatusOK, map[string]any{
			"kind":  "user",
			"email": actor.Email,
			"name":  actor.Name,
			"admin": actor.IsAdmin,
		})
	case actorToken:
		writeJSON(w, http.StatusOK, map[string]any{
			"kind":       "token",
			"token_id":   actor.TokenID,
			"scopes":     actor.TokenScopes,
			"project_id": actor.TokenProjectID,
		})
	default:
		writeError(w, http.StatusUnauthorized, "no_actor", "unidentified")
	}
}

func (s *Server) projectURL(slug string) string {
	return "https://" + slug + "." + s.cfg.AppBaseDomain
}

// sanitizeForSlug squashes a name to slug-safe chars.
func sanitizeForSlug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		case !prevHyphen && b.Len() > 0:
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) < 3 {
		out = "app"
	}
	if len(out) > 24 {
		out = out[:24]
	}
	return out
}
