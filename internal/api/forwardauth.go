package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/store"
)

// handleAuthCheck is the endpoint Traefik ForwardAuth middleware hits to ask
// "is this request to <host> allowed?". Called only from cluster network — not auth-protected.
//
// Inputs (headers):
//   X-Auth-Request-Email   — set by oauth2-proxy (downstream of Keycloak)
//   X-Auth-Request-Groups  — set by oauth2-proxy
//   X-Forwarded-Host       — set by Traefik (the user app hostname)
//
// Output: 200 (allow) / 403 (deny) / 401 (re-auth — but oauth2-proxy upstream handles this)
func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	host = strings.ToLower(strings.TrimSpace(host))
	// Strip port if present (Host can be "foo.example.com:8443")
	if i := strings.LastIndexByte(host, ':'); i > 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}

	// Resolve `host` to a project in two passes:
	//   1. Platform-issued subdomain pattern <slug>.<base-domain>
	//   2. Custom domain lookup in the `domains` table
	var p *model.Project
	suffix := "." + s.cfg.AppBaseDomain
	if strings.HasSuffix(host, suffix) {
		slug := strings.TrimSuffix(host, suffix)
		if slug == "" {
			http.Error(w, "unknown host", http.StatusForbidden)
			return
		}
		pp, err := s.store.GetProjectBySlug(r.Context(), slug)
		if err == nil {
			p = pp
		}
	}
	if p == nil {
		projID, err := s.store.LookupProjectIDByHostname(r.Context(), host)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "unknown host", http.StatusForbidden)
				return
			}
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		// Pull the project row by id — reuse GetProjectBySlug by going through SQL.
		var slug string
		if err := s.store.Pool.QueryRow(r.Context(),
			`SELECT slug FROM projects WHERE id = $1 AND deleted_at IS NULL`, projID,
		).Scan(&slug); err != nil {
			http.Error(w, "unknown project", http.StatusForbidden)
			return
		}
		pp, err := s.store.GetProjectBySlug(r.Context(), slug)
		if err != nil {
			http.Error(w, "unknown project", http.StatusForbidden)
			return
		}
		p = pp
	}
	if p == nil {
		http.Error(w, "unknown host", http.StatusForbidden)
		return
	}
	switch p.Visibility {
	case model.VisibilityPublic:
		w.WriteHeader(http.StatusOK)
		return
	case model.VisibilityOrg:
		email := r.Header.Get("X-Auth-Request-Email")
		if email == "" {
			http.Error(w, "auth required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("X-Vibedeploy-User-Email", email)
		w.Header().Set("X-Vibedeploy-User-Groups", r.Header.Get("X-Auth-Request-Groups"))
		w.WriteHeader(http.StatusOK)
		return
	case model.VisibilityRestricted:
		// M1: only owner. Full ACL lookup arrives in M3.
		email := r.Header.Get("X-Auth-Request-Email")
		if email == "" {
			http.Error(w, "auth required", http.StatusUnauthorized)
			return
		}
		owner, err := s.lookupOwnerEmail(r.Context(), p.OwnerID.String())
		if err != nil || !strings.EqualFold(owner, email) {
			http.Error(w, "not on ACL", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Error(w, "unknown visibility", http.StatusForbidden)
}

func (s *Server) lookupOwnerEmail(ctx context.Context, userID string) (string, error) {
	var email string
	err := s.store.Pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1::uuid`, userID).Scan(&email)
	return email, err
}
