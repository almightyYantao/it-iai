package config

import (
	"context"
	"strings"
	"sync"

	"github.com/iai/vibedeploy/internal/store"
)

// KCSettings is the subset of platform config that admins can edit at runtime
// from the Web Settings page. Everything in here is loaded from env at startup
// (with whatever the operator set in .env) and then overlaid with rows from
// the system_config DB table — DB wins. Saves through the API call Reload()
// so changes take effect without restarting the container.
type KCSettings struct {
	Issuer           string
	JWKSURL          string
	Audience         string
	AuthorizationURL string
	TokenURL         string
	ClientID         string
	ClientSecret     string
	RedirectURL      string

	// oauth2-proxy (gateway for user apps) settings — Phase B will consume.
	AuthHost      string // e.g. auth.example.com
	CookieSecret  string // 32 bytes base64
	CookieDomain  string // e.g. .example.com — defaults to ".${BASE_DOMAIN}"

	// BrandName is the human-readable identity of the SSO provider as shown
	// to end users ("Longbridge Account" at LB; "Acme SSO" elsewhere). Drives
	// the {{brand}} placeholder in every login-flow UI string so the same
	// codebase can be re-deployed without leaking the operator's brand.
	// Empty falls back to the literal "SSO".
	BrandName string
}

// Runtime is the in-memory mutable side of the config. Read-heavy, so guarded
// by an RWMutex. Construct once at boot, call Reload(ctx) on Settings-save.
type Runtime struct {
	store *store.Store
	envKC KCSettings // env-derived defaults; used when DB has nothing for a key

	mu sync.RWMutex
	kc KCSettings
}

// KC keys in system_config — central list keeps the API and the loader in sync.
var KCConfigKeys = []string{
	"kc.issuer",
	"kc.jwks_url",
	"kc.audience",
	"kc.authorization_url",
	"kc.token_url",
	"kc.client_id",
	"kc.client_secret",
	"kc.redirect_url",
	"auth.host",
	"auth.cookie_secret",
	"auth.cookie_domain",
	"auth.brand_name",
}

func NewRuntime(s *store.Store, base ControlPlane) *Runtime {
	envKC := KCSettings{
		Issuer:           base.KCIssuer,
		JWKSURL:          base.KCJWKSURL,
		Audience:         base.KCAudience,
		AuthorizationURL: base.KCAuthorizationURL,
		TokenURL:         base.KCTokenURL,
		ClientID:         base.KCClientID,
		ClientSecret:     base.KCClientSecret,
		RedirectURL:      base.KCRedirectURL,
		// Phase B keys — env defaults are empty; admin sets in UI.
		AuthHost:     "",
		CookieSecret: "",
		CookieDomain: "",
	}
	return &Runtime{store: s, envKC: envKC, kc: envKC}
}

func (r *Runtime) KC() KCSettings {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.kc
}

// Reload re-reads system_config and rebuilds the in-memory KC view.
// Call it from Settings-save handlers. Cheap (one SELECT + a struct copy).
func (r *Runtime) Reload(ctx context.Context) error {
	kv, err := r.store.GetSystemConfig(ctx)
	if err != nil {
		return err
	}
	next := r.envKC
	apply := func(key string, dst *string) {
		if v, ok := kv[key]; ok && v != "" {
			*dst = v
		}
	}
	apply("kc.issuer", &next.Issuer)
	apply("kc.jwks_url", &next.JWKSURL)
	apply("kc.audience", &next.Audience)
	apply("kc.authorization_url", &next.AuthorizationURL)
	apply("kc.token_url", &next.TokenURL)
	apply("kc.client_id", &next.ClientID)
	apply("kc.client_secret", &next.ClientSecret)
	apply("kc.redirect_url", &next.RedirectURL)
	apply("auth.host", &next.AuthHost)
	apply("auth.cookie_secret", &next.CookieSecret)
	apply("auth.cookie_domain", &next.CookieDomain)
	apply("auth.brand_name", &next.BrandName)

	// Normalise issuer (strip trailing slash) so audience checks etc. stay stable.
	next.Issuer = strings.TrimRight(next.Issuer, "/")

	r.mu.Lock()
	r.kc = next
	r.mu.Unlock()
	return nil
}
