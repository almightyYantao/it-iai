package config

import (
	"github.com/caarlos0/env/v10"
)

type ControlPlane struct {
	ListenAddr     string `env:"CP_LISTEN_ADDR" envDefault:":8080"`
	DatabaseURL    string `env:"CP_DATABASE_URL,required"`
	PublicBaseURL  string `env:"CP_PUBLIC_BASE_URL" envDefault:"http://localhost:8080"`
	AppBaseDomain  string `env:"CP_APP_BASE_DOMAIN" envDefault:"lab.localhost"`

	S3Endpoint     string `env:"CP_S3_ENDPOINT,required"`
	S3Region       string `env:"CP_S3_REGION" envDefault:"us-east-1"`
	S3AccessKey    string `env:"CP_S3_ACCESS_KEY,required"`
	S3SecretKey    string `env:"CP_S3_SECRET_KEY,required"`
	S3BucketSource string `env:"CP_S3_BUCKET_SOURCE" envDefault:"vibedeploy-source"`
	S3UseSSL       bool   `env:"CP_S3_USE_SSL" envDefault:"false"`

	// Public endpoint baked into presigned URLs that the Skill consumes.
	// Leave empty to reuse S3Endpoint (fine for cluster-internal clients only).
	S3PublicEndpoint string `env:"CP_S3_PUBLIC_ENDPOINT"`
	S3PublicUseSSL   bool   `env:"CP_S3_PUBLIC_USE_SSL" envDefault:"false"`

	RegistryHost        string `env:"CP_REGISTRY_HOST,required"`         // registry hostname Build Service pushes to
	RegistryHostFromK3D string `env:"CP_REGISTRY_HOST_FROM_K3D"`          // hostname K3s nodes use to pull (may differ from build-side)

	KEKBase64 string `env:"CP_KEK_BASE64,required"`

	KCIssuer   string `env:"CP_KC_ISSUER"`
	KCJWKSURL  string `env:"CP_KC_JWKS_URL"`
	// Empty by default — Keycloak's `aud` claim usually doesn't carry the
	// resource server name without a custom mapper. Issuer + signature is
	// the real check. Set this only after you've added a KC "Audience"
	// mapper that injects the value you put here.
	KCAudience string `env:"CP_KC_AUDIENCE"`

	// OIDC Authorization Code flow (browser → Keycloak → /v1/auth/oidc-callback).
	// All four required for the Web UI's "Sign in with Keycloak" button to work.
	KCAuthorizationURL string `env:"CP_KC_AUTHORIZATION_URL"`  // e.g. https://kc/realms/lb/protocol/openid-connect/auth
	KCTokenURL         string `env:"CP_KC_TOKEN_URL"`          // e.g. https://kc/realms/lb/protocol/openid-connect/token
	KCClientID         string `env:"CP_KC_CLIENT_ID"`          // confidential client for the proxy/web
	KCClientSecret     string `env:"CP_KC_CLIENT_SECRET"`
	KCRedirectURL      string `env:"CP_KC_REDIRECT_URL"`       // <public-base-url>/v1/auth/oidc-callback

	DevBootstrapToken string `env:"CP_DEV_BOOTSTRAP_TOKEN"`

	Kubeconfig    string `env:"CP_KUBECONFIG" envDefault:"/root/.kube/config"`
	IngressClass  string `env:"CP_K8S_INGRESS_CLASS" envDefault:"traefik"`

	// User-app sidecar provisioning (PG / Redis). Same dual-host pattern as
	// the image registry above:
	//   * UserPGAdminURL — control-plane uses this URL to CREATE DATABASE /
	//     CREATE USER. Resolved inside the docker-compose network, so the
	//     hostname is the compose service name `user-postgres`.
	//   * UserPGPublicHost — what we put into the DATABASE_URL we hand the
	//     user pod. The pod is in K3s, not in compose's network, so it
	//     reaches the platform host's published port (e.g. 5433).
	// Leave both empty to disable PG auto-provisioning (manifest.needs.postgres
	// is then a no-op; users must inject DATABASE_URL themselves).
	UserPGAdminURL   string `env:"CP_USER_PG_ADMIN_URL"`
	UserPGPublicHost string `env:"CP_USER_PG_PUBLIC_HOST"`
	// Redis is simpler — we share one instance and namespace by key prefix,
	// no per-project DB. Template gets {{slug}} substituted before being
	// written into project_env. Empty disables Redis auto-provisioning.
	UserRedisURLTemplate string `env:"CP_USER_REDIS_URL_TEMPLATE"`
}

type BuildService struct {
	DatabaseURL    string `env:"BS_DATABASE_URL,required"`
	S3Endpoint     string `env:"BS_S3_ENDPOINT,required"`
	S3AccessKey    string `env:"BS_S3_ACCESS_KEY,required"`
	S3SecretKey    string `env:"BS_S3_SECRET_KEY,required"`
	S3BucketSource string `env:"BS_S3_BUCKET_SOURCE" envDefault:"vibedeploy-source"`
	S3UseSSL       bool   `env:"BS_S3_USE_SSL" envDefault:"false"`

	RegistryHost   string        `env:"BS_REGISTRY_HOST,required"`
	WorkDir        string        `env:"BS_WORK_DIR" envDefault:"/var/lib/vibedeploy/build"`
	PollInterval   string        `env:"BS_POLL_INTERVAL" envDefault:"2s"`
	MaxConcurrent  int           `env:"BS_MAX_CONCURRENT" envDefault:"2"`
}

func LoadControlPlane() (ControlPlane, error) {
	var c ControlPlane
	if err := env.Parse(&c); err != nil {
		return c, err
	}
	return c, nil
}

func LoadBuildService() (BuildService, error) {
	var c BuildService
	if err := env.Parse(&c); err != nil {
		return c, err
	}
	return c, nil
}
