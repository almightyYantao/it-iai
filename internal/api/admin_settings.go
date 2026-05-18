package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

// GET  /v1/admin/settings — return the effective KC settings + per-key DB metadata.
//
// Effective = env defaults overlaid with DB rows. The Web Settings form shows
// the effective values so admins see exactly what the platform is currently
// using. Secrets (client_secret, cookie_secret) are returned redacted —
// the UI shows a placeholder and only POSTs them back if the admin types a
// new value.
func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	ctx := r.Context()

	kc := s.runtime.KC()

	entries, err := s.store.ListSystemConfig(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	type meta struct {
		HasOverride bool       `json:"has_override"`
		UpdatedAt   *time.Time `json:"updated_at,omitempty"`
	}
	dbMeta := map[string]meta{}
	for _, e := range entries {
		t := e.UpdatedAt
		dbMeta[e.Key] = meta{HasOverride: true, UpdatedAt: &t}
	}

	// Secrets are returned as a redaction sentinel ("is anything set?") not the value.
	redact := func(v string) string {
		if v == "" {
			return ""
		}
		return "********"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"kc": map[string]any{
			"issuer":            kc.Issuer,
			"jwks_url":          kc.JWKSURL,
			"audience":          kc.Audience,
			"authorization_url": kc.AuthorizationURL,
			"token_url":         kc.TokenURL,
			"client_id":         kc.ClientID,
			"client_secret":     redact(kc.ClientSecret),
			"redirect_url":      kc.RedirectURL,
			"auth_host":         kc.AuthHost,
			"cookie_secret":     redact(kc.CookieSecret),
			"cookie_domain":     kc.CookieDomain,
			"brand_name":        kc.BrandName,
		},
		"meta": dbMeta,
	})
}

// PATCH /v1/admin/settings — update any subset of keys.
// Body: { kc: { issuer?: string, ... } }
//
// Empty string means "clear the DB override and fall back to the env default."
// Secrets that the UI didn't touch are sent back as "********" — we treat that
// sentinel as "leave whatever is in DB alone" so admins can save other fields
// without re-typing client_secret every time.
func (s *Server) handleAdminPatchSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	var body struct {
		KC map[string]*string `json:"kc"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if body.KC == nil {
		writeError(w, http.StatusBadRequest, "no_op", "kc payload required")
		return
	}

	// Map UI field name → system_config key.
	keyMap := map[string]string{
		"issuer":            "kc.issuer",
		"jwks_url":          "kc.jwks_url",
		"audience":          "kc.audience",
		"authorization_url": "kc.authorization_url",
		"token_url":         "kc.token_url",
		"client_id":         "kc.client_id",
		"client_secret":     "kc.client_secret",
		"redirect_url":      "kc.redirect_url",
		"auth_host":         "auth.host",
		"cookie_secret":     "auth.cookie_secret",
		"cookie_domain":     "auth.cookie_domain",
		"brand_name":        "auth.brand_name",
	}
	const redactSentinel = "********"

	updates := map[string]string{}
	for field, v := range body.KC {
		key, ok := keyMap[field]
		if !ok {
			continue // ignore unknown fields rather than failing the whole save
		}
		if v == nil {
			continue
		}
		// "********" means "don't change" — skip the field entirely.
		if *v == redactSentinel {
			continue
		}
		updates[key] = *v
	}
	if len(updates) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": 0})
		return
	}

	ctx := r.Context()
	var updatedBy *uuid.UUID
	if actor.Kind == actorUser {
		id := actor.UserID
		updatedBy = &id
	}

	if err := s.store.BulkSetSystemConfig(ctx, updates, updatedBy); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if err := s.runtime.Reload(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "reload", err.Error())
		return
	}
	// Rebuild the JWT verifier with the new issuer/audience/JWKS.
	s.RebuildJWTVerifier()

	// Audit which keys changed (not the values — secrets must not leak into the log).
	changedKeys := make([]string, 0, len(updates))
	for k := range updates {
		changedKeys = append(changedKeys, k)
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"settings.update", nil, map[string]any{"keys": changedKeys})

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "changed": len(updates)})
}
