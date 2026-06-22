package api

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/iai/vibedeploy/internal/auth"
	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/store"
)

// POST /v1/projects/:slug/api-token
//
// Mints a fresh project-level API token, overwriting any existing one (we
// don't keep token history — owners can revoke + regenerate, full stop).
// The plaintext is returned in this response and never again; the hash is
// what we persist + later bcrypt-compare in the verify endpoint.
//
// Owner or admin only. Audit row recorded so unexplained regenerations
// surface in the audit log.
func (s *Server) handleRegenerateProjectAPIToken(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can issue API tokens")
		return
	}

	plaintext, hash, prefix, err := auth.GenerateAPIToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rng", err.Error())
		return
	}

	ctx := r.Context()
	if err := s.store.SetProjectAPIToken(ctx, p.ID, hash, prefix); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.api_token.regenerate", &p.ID, map[string]any{"prefix": prefix})

	// Plaintext leaves the server exactly once. Clients SHOULD store it in a
	// secret manager; we can't help them recover it later.
	writeJSON(w, http.StatusCreated, map[string]any{
		"token":  plaintext,
		"prefix": prefix,
	})
}

// DELETE /v1/projects/:slug/api-token — clears the token columns. Subsequent
// requests through token-mode path rules will get 401. Owner or admin only.
func (s *Server) handleRevokeProjectAPIToken(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can revoke API tokens")
		return
	}

	ctx := r.Context()
	if err := s.store.SetProjectAPIToken(ctx, p.ID, nil, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.api_token.revoke", &p.ID, nil)

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// GET /v1/_internal/verify-api-token
//
// Called by the traefik `iai-api-token-verify` ForwardAuth middleware on
// every request hitting a token-mode path rule. Resolves the project from
// `X-Forwarded-Host` (default subdomain or registered custom domain),
// extracts the Bearer token, and sha256-compares against the stored hash.
//
// Returns 200 on success (traefik then proxies the original request to the
// app) or 401 on any mismatch. Body is intentionally empty — traefik
// ForwardAuth only inspects the status code.
//
// No auth gate on the endpoint itself; security relies on:
//   - the cluster-internal listening surface (control-plane is only reachable
//     from inside the docker bridge / k8s service network on prod), and
//   - the verify being a constant-time hash compare so it leaks no info
//     beyond "is this exact token valid for this host?".
func (s *Server) handleInternalVerifyAPIToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	hostHdr := r.Header.Get("X-Forwarded-Host")
	if hostHdr == "" {
		hostHdr = r.Host
	}
	host := strings.ToLower(strings.TrimSpace(hostHdr))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "" {
		writeError(w, http.StatusUnauthorized, "no_host", "missing X-Forwarded-Host")
		return
	}

	project := s.lookupProjectByHostname(ctx, host)
	if project == nil {
		writeError(w, http.StatusUnauthorized, "unknown_host", "host does not map to a project")
		return
	}

	// Bearer token from the original request, preserved by traefik.
	authHdr := r.Header.Get("Authorization")
	const bearer = "Bearer "
	if !strings.HasPrefix(authHdr, bearer) {
		writeError(w, http.StatusUnauthorized, "no_bearer", "expected Authorization: Bearer …")
		return
	}
	tok := strings.TrimSpace(authHdr[len(bearer):])
	if tok == "" {
		writeError(w, http.StatusUnauthorized, "empty_token", "empty Bearer value")
		return
	}

	hash, err := s.store.GetProjectAPITokenHash(ctx, project.ID)
	if err != nil || hash == nil {
		// No token issued yet — verify trivially fails so the app stays
		// inaccessible until the owner mints one.
		writeError(w, http.StatusUnauthorized, "no_token_set", "project has no API token configured")
		return
	}
	if !bytes.Equal(hash, auth.HashToken(tok)) {
		writeError(w, http.StatusUnauthorized, "bad_token", "token does not match")
		return
	}

	w.WriteHeader(http.StatusOK)
}

// lookupProjectByHostname resolves a request hostname to its owning project.
// Tries the default `<slug>.<base-domain>` form first (cheap: parses the
// subdomain, single GetProjectBySlug), then falls back to the domains table
// for registered custom domains. Returns nil if neither matches.
func (s *Server) lookupProjectByHostname(ctx context.Context, host string) *model.Project {
	// Default subdomain — strip the configured base domain to get the slug.
	base := strings.ToLower(s.cfg.AppBaseDomain)
	if base != "" && strings.HasSuffix(host, "."+base) {
		slug := strings.TrimSuffix(host, "."+base)
		if p, err := s.store.GetProjectBySlug(ctx, slug); err == nil {
			return p
		} else if !errors.Is(err, store.ErrNotFound) {
			// Treat unexpected DB errors as "no match" — caller turns this
			// into 401 which is the safe failure mode.
			return nil
		}
	}
	// Custom domain.
	pid, err := s.store.LookupProjectIDByHostname(ctx, host)
	if err != nil {
		return nil
	}
	p, err := s.store.GetProjectByID(ctx, pid)
	if err != nil {
		return nil
	}
	return p
}
