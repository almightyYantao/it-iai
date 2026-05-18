package api

import (
	"net/http"
	"strconv"
	"time"
)

// isAdminActor returns true for the bootstrap deploy token (admin scope) or admin users.
func isAdminActor(a Actor) bool {
	if a.Kind == actorUser && a.IsAdmin {
		return true
	}
	if a.Kind == actorToken {
		for _, s := range a.TokenScopes {
			if s == "admin" || s == "*" {
				return true
			}
		}
	}
	return false
}

// parsePager pulls limit + offset off the URL with sane bounds.
func parsePager(r *http.Request, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= maxLimit {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return
}

// handleAdminProjects returns every project regardless of ownership.
// Paginated; total returned in the body so the client can render a pager.
func (s *Server) handleAdminProjects(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	limit, offset := parsePager(r, 10, 200)
	ctx := r.Context()

	var total int
	if err := s.store.Pool.QueryRow(ctx,
		`SELECT count(1) FROM projects WHERE deleted_at IS NULL`,
	).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	const q = `
		SELECT p.id, p.slug, p.name, p.owner_id, p.visibility, p.status, p.created_at,
		       p.last_pushed_at, p.last_active_at, u.email
		FROM projects p
		LEFT JOIN users u ON u.id = p.owner_id
		WHERE p.deleted_at IS NULL
		ORDER BY p.created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := s.store.Pool.Query(ctx, q, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer rows.Close()
	type row struct {
		ID           string     `json:"id"`
		Slug         string     `json:"slug"`
		Name         string     `json:"name"`
		OwnerID      string     `json:"owner_id"`
		OwnerEmail   string     `json:"owner_email"`
		Visibility   string     `json:"visibility"`
		Status       string     `json:"status"`
		CreatedAt    time.Time  `json:"created_at"`
		LastPushedAt *time.Time `json:"last_pushed_at,omitempty"`
		LastActiveAt *time.Time `json:"last_active_at,omitempty"`
		URL          string     `json:"url"`
	}
	out := []row{}
	for rows.Next() {
		var r row
		var ownerEmail *string
		if err := rows.Scan(&r.ID, &r.Slug, &r.Name, &r.OwnerID, &r.Visibility, &r.Status, &r.CreatedAt, &r.LastPushedAt, &r.LastActiveAt, &ownerEmail); err != nil {
			writeError(w, http.StatusInternalServerError, "scan", err.Error())
			return
		}
		if ownerEmail != nil {
			r.OwnerEmail = *ownerEmail
		}
		r.URL = s.projectURL(r.Slug)
		out = append(out, r)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"projects": out,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

func (s *Server) handleAdminMetrics(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	ctx := r.Context()
	type metrics struct {
		Projects struct {
			Total   int `json:"total"`
			Running int `json:"running"`
			Failed  int `json:"failed"`
		} `json:"projects"`
		Deployments struct {
			Last24h       int `json:"last_24h"`
			InFlight      int `json:"in_flight"`
			FailedLast24h int `json:"failed_last_24h"`
		} `json:"deployments"`
	}
	var m metrics
	_ = s.store.Pool.QueryRow(ctx, `SELECT count(1) FROM projects WHERE deleted_at IS NULL`).Scan(&m.Projects.Total)
	_ = s.store.Pool.QueryRow(ctx, `SELECT count(1) FROM projects WHERE deleted_at IS NULL AND status='running'`).Scan(&m.Projects.Running)
	_ = s.store.Pool.QueryRow(ctx, `SELECT count(1) FROM projects WHERE deleted_at IS NULL AND status='error'`).Scan(&m.Projects.Failed)
	_ = s.store.Pool.QueryRow(ctx, `SELECT count(1) FROM deployments WHERE created_at > now() - interval '24 hours'`).Scan(&m.Deployments.Last24h)
	_ = s.store.Pool.QueryRow(ctx, `SELECT count(1) FROM deployments WHERE status IN ('queued','building','pushing','deploying')`).Scan(&m.Deployments.InFlight)
	_ = s.store.Pool.QueryRow(ctx, `SELECT count(1) FROM deployments WHERE status='failed' AND created_at > now() - interval '24 hours'`).Scan(&m.Deployments.FailedLast24h)
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	limit, offset := parsePager(r, 10, 500)
	ctx := r.Context()

	var total int
	if err := s.store.Pool.QueryRow(ctx,
		`SELECT count(1) FROM audit_logs`,
	).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	// Joins resolve actor_id → human-readable label:
	//   user-kind  → users.email
	//   token-kind → deploy_tokens.name
	// Comparing as text avoids a CAST that would fail if a row ever held a
	// non-UUID actor_id (legacy/fixture data).
	rows, err := s.store.Pool.Query(ctx,
		`SELECT al.id, al.actor_type, al.actor_id, al.action, al.project_id,
		        al.metadata, al.created_at,
		        u.email, dt.name
		 FROM audit_logs al
		 LEFT JOIN users         u  ON al.actor_type = 'user'  AND u.id::text  = al.actor_id
		 LEFT JOIN deploy_tokens dt ON al.actor_type = 'token' AND dt.id::text = al.actor_id
		 ORDER BY al.created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer rows.Close()
	type row struct {
		ID         string    `json:"id"`
		ActorType  string    `json:"actor_type"`
		ActorID    string    `json:"actor_id"`
		ActorLabel string    `json:"actor_label"` // email (user) or token name (token); falls back to actor_id
		Action     string    `json:"action"`
		ProjectID  *string   `json:"project_id,omitempty"`
		Metadata   any       `json:"metadata,omitempty"`
		CreatedAt  time.Time `json:"created_at"`
	}
	out := []row{}
	for rows.Next() {
		var r row
		var meta []byte
		var email, tokenName *string
		if err := rows.Scan(&r.ID, &r.ActorType, &r.ActorID, &r.Action, &r.ProjectID, &meta, &r.CreatedAt, &email, &tokenName); err != nil {
			writeError(w, http.StatusInternalServerError, "scan", err.Error())
			return
		}
		if len(meta) > 0 {
			r.Metadata = string(meta)
		}
		switch {
		case email != nil && *email != "":
			r.ActorLabel = *email
		case tokenName != nil && *tokenName != "":
			r.ActorLabel = *tokenName
		default:
			// Token rows still expose the UUID so admins can correlate revoked tokens.
			r.ActorLabel = r.ActorID
		}
		out = append(out, r)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": out,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// handleAdminListUsers — paginated list of all known users.
func (s *Server) handleAdminListUsers(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	limit, offset := parsePager(r, 10, 200)
	ctx := r.Context()

	var total int
	if err := s.store.Pool.QueryRow(ctx, `SELECT count(1) FROM users`).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	rows, err := s.store.Pool.Query(ctx,
		`SELECT id, email, COALESCE(name,''), is_admin, created_at, last_seen_at
		 FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer rows.Close()
	type row struct {
		ID         string     `json:"id"`
		Email      string     `json:"email"`
		Name       string     `json:"name"`
		IsAdmin    bool       `json:"is_admin"`
		CreatedAt  time.Time  `json:"created_at"`
		LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	}
	out := []row{}
	for rows.Next() {
		var u row
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &u.LastSeenAt); err != nil {
			writeError(w, http.StatusInternalServerError, "scan", err.Error())
			return
		}
		out = append(out, u)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"users":  out,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// handleAdminPatchUser — set is_admin on a user. Admin-only.
// Body: { "is_admin": bool }. Refuses to demote the very last admin to avoid
// locking the platform out.
func (s *Server) handleAdminPatchUser(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	id := r.URL.Path
	if i := lastSlash(id); i >= 0 {
		id = id[i+1:]
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "bad_id", "missing user id in path")
		return
	}
	var body struct {
		IsAdmin *bool `json:"is_admin"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if body.IsAdmin == nil {
		writeError(w, http.StatusBadRequest, "no_op", "nothing to update")
		return
	}
	ctx := r.Context()

	if !*body.IsAdmin {
		// Don't allow demoting the last user-admin. Bootstrap token still has
		// admin scope and can re-promote someone, but we keep the UX guard.
		var adminCount int
		_ = s.store.Pool.QueryRow(ctx,
			`SELECT count(1) FROM users WHERE is_admin = true`,
		).Scan(&adminCount)
		var currentlyAdmin bool
		_ = s.store.Pool.QueryRow(ctx,
			`SELECT is_admin FROM users WHERE id = $1::uuid`, id,
		).Scan(&currentlyAdmin)
		if currentlyAdmin && adminCount <= 1 {
			writeError(w, http.StatusBadRequest, "last_admin",
				"refusing to demote the last admin — promote someone else first")
			return
		}
	}

	tag, err := s.store.Pool.Exec(ctx,
		`UPDATE users SET is_admin = $2 WHERE id = $1::uuid`, id, *body.IsAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "not_found", "user not found")
		return
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"user.set_admin", nil, map[string]any{"user_id": id, "is_admin": *body.IsAdmin})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}
