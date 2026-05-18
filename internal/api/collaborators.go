package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/iai/vibedeploy/internal/store"
)

// Collaborators API:
//
//   GET    /v1/projects/:slug/collaborators
//   POST   /v1/projects/:slug/collaborators       body: { email: "..." }
//   DELETE /v1/projects/:slug/collaborators/:email
//
// Only the project owner (or an admin) can add/remove collaborators.

type addCollaboratorRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (s *Server) handleListCollaborators(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	rows, err := s.store.Pool.Query(r.Context(),
		`SELECT u.id, u.email, COALESCE(u.name,''), c.role, c.added_at
		 FROM project_collaborators c JOIN users u ON u.id = c.user_id
		 WHERE c.project_id = $1 ORDER BY c.added_at ASC`,
		p.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer rows.Close()
	type row struct {
		UserID  string `json:"user_id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Role    string `json:"role"`
		AddedAt string `json:"added_at"`
	}
	var out []row
	for rows.Next() {
		var r row
		var t any
		if err := rows.Scan(&r.UserID, &r.Email, &r.Name, &r.Role, &t); err != nil {
			writeError(w, http.StatusInternalServerError, "scan", err.Error())
			return
		}
		// added_at is timestamptz — let pgx render it as default
		r.AddedAt = sprintTime(t)
		out = append(out, r)
	}
	writeJSON(w, http.StatusOK, map[string]any{"collaborators": out})
}

func (s *Server) handleAddCollaborator(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "forbidden", "only the project owner or an admin can add collaborators")
		return
	}
	var req addCollaboratorRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "bad_email", "valid email required")
		return
	}
	role := req.Role
	if role == "" {
		role = "editor"
	}

	// User must already exist (they've logged in once). We don't pre-create
	// users — that would let any caller spray random emails into the table.
	var collabID string
	err := s.store.Pool.QueryRow(r.Context(),
		`SELECT id FROM users WHERE email = $1`, email,
	).Scan(&collabID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_such_user",
			"that email has never signed in; ask them to log in once first")
		return
	}
	// Resolve actor user id for "added_by" — for token actors fall back to NULL.
	addedBy, _ := s.actorUserID(r.Context(), actor)

	if err := s.store.AddCollaborator(r.Context(), p.ID, parseUUID(collabID), addedBy, role); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(), "collaborator.add", &p.ID, map[string]string{"email": email, "role": role})
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *Server) handleRemoveCollaborator(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "forbidden", "only the project owner or an admin can remove collaborators")
		return
	}
	email := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "email")))
	if email == "" {
		writeError(w, http.StatusBadRequest, "bad_email", "missing email")
		return
	}
	var collabID string
	err := s.store.Pool.QueryRow(r.Context(),
		`SELECT id FROM users WHERE email = $1`, email,
	).Scan(&collabID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_such_user", "email not on this project")
		return
	}
	if err := s.store.RemoveCollaborator(r.Context(), p.ID, parseUUID(collabID)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "not a collaborator")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(), "collaborator.remove", &p.ID, map[string]string{"email": email})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
