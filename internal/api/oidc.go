package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iai/vibedeploy/internal/auth"
	"github.com/iai/vibedeploy/internal/store"
)

// OIDC Authorization Code flow (Keycloak).
//
//   GET /v1/auth/oidc-status          → { enabled: bool }
//   GET /v1/auth/oidc-login?next=...  → 302 to Keycloak's /auth (with HMAC-signed state)
//   GET /v1/auth/oidc-callback        → verify state HMAC → exchange code →
//                                       verify access token → upsert user
//                                       → mint a long-lived deploy token tied to that user
//                                       → 302 back to /login?token=...
//
// Why stateless state instead of a session cookie:
//   The browser sets the state cookie under the origin that served
//   /v1/auth/oidc-login (typically the Web UI's host:port via nginx proxy).
//   If CP_KC_REDIRECT_URL points at a different origin (e.g. the API on a
//   different port, or a public hostname), the callback request is to that
//   origin and the browser doesn't send the cookie — every flow fails with
//   "state mismatch". Signing the state with the KEK removes the dependency
//   on cookie scope entirely. The signature also enforces a 10-minute TTL
//   without touching the database.

const oidcStateTTL = 10 * time.Minute

// oidcEnabled returns true iff the operator configured the full code-flow set.
// Reads from Runtime so Settings-page edits take effect immediately.
func (s *Server) oidcEnabled() bool {
	kc := s.runtime.KC()
	return kc.AuthorizationURL != "" &&
		kc.TokenURL != "" &&
		kc.ClientID != "" &&
		kc.ClientSecret != "" &&
		kc.RedirectURL != "" &&
		s.JWT().Enabled()
}

// handleOIDCStatus is the only unauthenticated endpoint that needs to know the
// SSO brand name — the Login page calls it before the user has any token.
// Returning the brand here lets the UI render "Sign in with <brand>" without
// either hardcoding a company name in the codebase or making the Login page
// hit an authenticated settings endpoint.
func (s *Server) handleOIDCStatus(w http.ResponseWriter, _ *http.Request) {
	brand := s.runtime.KC().BrandName
	if brand == "" {
		brand = "SSO"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": s.oidcEnabled(),
		"brand":   brand,
	})
}

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if !s.oidcEnabled() {
		writeError(w, http.StatusServiceUnavailable, "oidc_disabled", "Keycloak is not configured on this control-plane")
		return
	}
	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/"
	}
	state, err := s.signOIDCState(next)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_sign", err.Error())
		return
	}
	kc := s.runtime.KC()
	params := url.Values{}
	params.Set("client_id", kc.ClientID)
	params.Set("redirect_uri", kc.RedirectURL)
	params.Set("response_type", "code")
	params.Set("scope", "openid email profile")
	params.Set("state", state)
	http.Redirect(w, r, kc.AuthorizationURL+"?"+params.Encode(), http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !s.oidcEnabled() {
		writeError(w, http.StatusServiceUnavailable, "oidc_disabled", "Keycloak is not configured")
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		writeError(w, http.StatusBadRequest, "oidc_error",
			fmt.Sprintf("Keycloak returned: %s — %s", errParam, r.URL.Query().Get("error_description")))
		return
	}
	code := r.URL.Query().Get("code")
	stateBlob := r.URL.Query().Get("state")
	if code == "" || stateBlob == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "missing code/state")
		return
	}

	next, err := s.verifyOIDCState(stateBlob)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_state", err.Error())
		return
	}

	// Exchange the code for tokens.
	access, err := s.exchangeOIDCCode(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusBadGateway, "exchange", err.Error())
		return
	}

	// Verify the access token to extract identity.
	claims, err := s.JWT().Verify(r.Context(), access)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "bad_access_token", err.Error())
		return
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "no_email", "Keycloak token has no email claim — check the profile scope mapping")
		return
	}
	// "GivenName (FamilyName)" when both are present; falls back through
	// claims.Name → preferred_username → "" (the upsert will use email).
	name := claims.DisplayName()

	u, err := s.store.UpsertUserByEmail(r.Context(), email, name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "upsert", err.Error())
		return
	}

	// Mint a deploy token (90 days). Admin → "admin" scope; everyone else gets
	// the standard push/logs/env set.
	plain, hash, prefix, err := auth.GenerateToken("live")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "mint", err.Error())
		return
	}
	scopes := []string{"push", "logs:read", "env:read", "env:write"}
	if u.IsAdmin {
		scopes = []string{"admin"}
	}
	uid := u.ID
	_, err = s.store.CreateDeployToken(r.Context(), store.CreateTokenInput{
		ProjectID: nil,
		Name:      "oidc-session-" + time.Now().UTC().Format("2006-01-02"),
		Hash:      hash,
		Prefix:    prefix,
		Scopes:    scopes,
		AllowDB:   u.IsAdmin,
		ExpiresAt: time.Now().Add(90 * 24 * time.Hour),
		CreatedBy: &uid,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_token", err.Error())
		return
	}
	s.store.WriteAudit(r.Context(), "user", u.ID.String(), "auth.oidc-login", nil, map[string]string{"email": email})

	dest := next
	if dest == "" || !strings.HasPrefix(dest, "/") {
		dest = "/"
	}
	target := "/login?token=" + url.QueryEscape(plain) + "&next=" + url.QueryEscape(dest)
	http.Redirect(w, r, target, http.StatusFound)
}

// signOIDCState builds: base64(random[16] || expires[8] || next-bytes || hmac[32]).
// next is encoded inside the state so it survives the round-trip without a server-side store.
func (s *Server) signOIDCState(next string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	exp := time.Now().Add(oidcStateTTL).UnixNano()
	expBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(expBuf, uint64(exp))

	payload := append([]byte{}, nonce...)
	payload = append(payload, expBuf...)
	payload = append(payload, []byte(next)...)

	mac := hmac.New(sha256.New, s.oidcSigningKey())
	mac.Write(payload)
	sig := mac.Sum(nil)

	full := append(payload, sig...)
	return base64.RawURLEncoding.EncodeToString(full), nil
}

func (s *Server) verifyOIDCState(blob string) (string, error) {
	full, err := base64.RawURLEncoding.DecodeString(blob)
	if err != nil {
		return "", fmt.Errorf("state not base64: %w", err)
	}
	const minLen = 16 + 8 + 32 // nonce + expires + hmac, "next" optional
	if len(full) < minLen {
		return "", fmt.Errorf("state truncated")
	}
	sig := full[len(full)-32:]
	payload := full[:len(full)-32]

	mac := hmac.New(sha256.New, s.oidcSigningKey())
	mac.Write(payload)
	expected := mac.Sum(nil)
	if !hmac.Equal(sig, expected) {
		return "", fmt.Errorf("signature mismatch")
	}

	exp := int64(binary.BigEndian.Uint64(payload[16:24]))
	if time.Now().UnixNano() > exp {
		return "", fmt.Errorf("state expired (>%s old)", oidcStateTTL)
	}
	next := string(payload[24:])
	return next, nil
}

// oidcSigningKey derives a 32-byte signing key from the platform KEK with a
// label separator (see auth.KEK.Derive). Using the existing KEK avoids
// introducing a second secret; the label isolates this key material from
// AEAD use.
func (s *Server) oidcSigningKey() []byte {
	return s.kek.Derive("vibedeploy-oidc-state-v1")
}

// exchangeOIDCCode performs the token endpoint POST and returns the access_token.
func (s *Server) exchangeOIDCCode(ctx context.Context, code string) (string, error) {
	kc := s.runtime.KC()
	body := url.Values{}
	body.Set("grant_type", "authorization_code")
	body.Set("code", code)
	body.Set("redirect_uri", kc.RedirectURL)
	body.Set("client_id", kc.ClientID)
	body.Set("client_secret", kc.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", kc.TokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	cli := &http.Client{Timeout: 10 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("%s: %s", out.Error, out.ErrorDescription)
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("token endpoint returned HTTP %d", resp.StatusCode)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("no access_token in response")
	}
	return out.AccessToken, nil
}
