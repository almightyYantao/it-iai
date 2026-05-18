package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTVerifier verifies Keycloak-issued RS256 access tokens against a JWKS
// endpoint. Keys are cached for 10 minutes and refreshed on miss.
//
// Disabled state: Enabled() returns false when issuer or jwksURL is empty,
// and Verify returns a clear error. The control-plane gates the JWT codepath
// on Enabled() so dev-only Deploy Token mode still works without Keycloak.
type JWTVerifier struct {
	issuer   string
	audience string // optional — Keycloak's `azp` / `aud` may differ between client setups
	jwksURL  string

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

// Claims is the subset we care about. Keycloak puts the user's email in
// `email`, name in `preferred_username` / `name`, and groups under `groups`
// (or `realm_access.roles` depending on the protocol mapper).
type Claims struct {
	jwt.RegisteredClaims
	Email             string   `json:"email"`
	EmailVerified     bool     `json:"email_verified"`
	Name              string   `json:"name"`
	GivenName         string   `json:"given_name"`
	FamilyName        string   `json:"family_name"`
	PreferredUsername string   `json:"preferred_username"`
	Groups            []string `json:"groups"`
	RealmAccess       struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

// DisplayName builds the human-readable name we render across the app from
// the OIDC claims. Preference order:
//
//   1. "GivenName (FamilyName)" — Chinese-name convention used by our org,
//      e.g. "张三 (Zhang San)". Used whenever both halves are present.
//   2. `name` — Keycloak's pre-composed display name, if the mapper set one.
//   3. `preferred_username` — fallback (typically the local-part of the email).
//   4. "" — caller will substitute the email.
//
// Keeping this on the claims type (instead of a free function) so every
// caller — OIDC code-flow and bearer-token middleware — produces the same
// label without copy-paste drift.
func (c *Claims) DisplayName() string {
	g := strings.TrimSpace(c.GivenName)
	f := strings.TrimSpace(c.FamilyName)
	if g != "" && f != "" {
		return g + " (" + f + ")"
	}
	if c.Name != "" {
		return c.Name
	}
	return c.PreferredUsername
}

func NewJWTVerifier(issuer, audience, jwksURL string) *JWTVerifier {
	return &JWTVerifier{
		issuer:   strings.TrimRight(issuer, "/"),
		audience: audience,
		jwksURL:  jwksURL,
		keys:     map[string]*rsa.PublicKey{},
	}
}

func (v *JWTVerifier) Enabled() bool {
	return v != nil && v.issuer != "" && v.jwksURL != ""
}

func (v *JWTVerifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	if !v.Enabled() {
		return nil, errors.New("jwt verifier not configured")
	}
	tok, err := jwt.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method %s", t.Method.Alg())
		}
		kid, _ := t.Header["kid"].(string)
		return v.keyFor(ctx, kid)
	}, jwt.WithIssuer(v.issuer))
	if err != nil {
		return nil, err
	}
	c, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, errors.New("invalid jwt")
	}
	// Audience check is best-effort — Keycloak tokens often have `aud` set to
	// the resource server name OR the client name, and orgs configure mappers
	// differently. If we asked for an audience, require ANY match.
	if v.audience != "" {
		if !audienceContains(c.Audience, v.audience) {
			return nil, fmt.Errorf("audience %q not in token: %v", v.audience, c.Audience)
		}
	}
	return c, nil
}

func audienceContains(aud jwt.ClaimStrings, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

// keyFor returns the cached RSA public key for the given kid, refreshing the
// JWKS cache when the entry is missing or older than 10 minutes.
func (v *JWTVerifier) keyFor(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	k, ok := v.keys[kid]
	fresh := time.Since(v.fetched) < 10*time.Minute
	v.mu.RUnlock()
	if ok && fresh {
		return k, nil
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("kid %q not found in JWKS", kid)
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (v *JWTVerifier) refresh(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", v.jwksURL, nil)
	cli := &http.Client{Timeout: 8 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("fetch JWKS: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var set jwkSet
	if err := json.Unmarshal(body, &set); err != nil {
		return fmt.Errorf("parse JWKS: %w", err)
	}
	parsed := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		key, err := jwkToRSAPublic(k.N, k.E)
		if err != nil {
			continue
		}
		parsed[k.Kid] = key
	}
	if len(parsed) == 0 {
		return errors.New("no usable RSA keys in JWKS")
	}
	v.mu.Lock()
	v.keys = parsed
	v.fetched = time.Now()
	v.mu.Unlock()
	return nil
}

// jwkToRSAPublic converts the base64url-encoded modulus + exponent into a
// real *rsa.PublicKey. golang-jwt expects the *rsa.PublicKey directly from
// the key function, so we do the conversion here instead of carrying the
// raw JWK around.
func jwkToRSAPublic(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}
	// Build E from the (typically 3-byte) big-endian uint representation.
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: e,
	}, nil
}
