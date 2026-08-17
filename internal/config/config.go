package config

import (
	"github.com/caarlos0/env/v10"
)

type ControlPlane struct {
	ListenAddr    string `env:"CP_LISTEN_ADDR" envDefault:":8080"`
	DatabaseURL   string `env:"CP_DATABASE_URL,required"`
	PublicBaseURL string `env:"CP_PUBLIC_BASE_URL" envDefault:"http://localhost:8080"`
	AppBaseDomain string `env:"CP_APP_BASE_DOMAIN" envDefault:"lab.localhost"`

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

	RegistryHost        string `env:"CP_REGISTRY_HOST,required"` // registry hostname Build Service pushes to
	RegistryHostFromK3D string `env:"CP_REGISTRY_HOST_FROM_K3D"` // hostname K3s nodes use to pull (may differ from build-side)

	KEKBase64 string `env:"CP_KEK_BASE64,required"`

	KCIssuer  string `env:"CP_KC_ISSUER"`
	KCJWKSURL string `env:"CP_KC_JWKS_URL"`
	// Empty by default — Keycloak's `aud` claim usually doesn't carry the
	// resource server name without a custom mapper. Issuer + signature is
	// the real check. Set this only after you've added a KC "Audience"
	// mapper that injects the value you put here.
	KCAudience string `env:"CP_KC_AUDIENCE"`

	// OIDC Authorization Code flow (browser → Keycloak → /v1/auth/oidc-callback).
	// All four required for the Web UI's "Sign in with Keycloak" button to work.
	KCAuthorizationURL string `env:"CP_KC_AUTHORIZATION_URL"` // e.g. https://kc/realms/lb/protocol/openid-connect/auth
	KCTokenURL         string `env:"CP_KC_TOKEN_URL"`         // e.g. https://kc/realms/lb/protocol/openid-connect/token
	KCClientID         string `env:"CP_KC_CLIENT_ID"`         // confidential client for the proxy/web
	KCClientSecret     string `env:"CP_KC_CLIENT_SECRET"`
	KCRedirectURL      string `env:"CP_KC_REDIRECT_URL"` // <public-base-url>/v1/auth/oidc-callback

	DevBootstrapToken string `env:"CP_DEV_BOOTSTRAP_TOKEN"`

	Kubeconfig   string `env:"CP_KUBECONFIG" envDefault:"/root/.kube/config"`
	IngressClass string `env:"CP_K8S_INGRESS_CLASS" envDefault:"traefik"`

	// Name of the cert-manager ClusterIssuer used when a project has TLS
	// enabled. Defaults to the issuer that deploy/install-cert-manager.sh
	// creates. Override to point at a staging issuer or an internal CA.
	TLSClusterIssuer string `env:"CP_TLS_CLUSTER_ISSUER" envDefault:"letsencrypt-prod"`

	// kube-dns's ClusterIP. The per-project egress policy denies the whole
	// service network, so DNS has to be named explicitly to be reachable —
	// see k8sdriver.ensureEgressPolicy. Default is k3s's (service CIDR .10).
	DNSClusterIP string `env:"CP_DNS_CLUSTER_IP" envDefault:"10.43.0.10"`

	// Hard cap on how many custom hostnames a single project can register
	// (vanity subdomain + fully-custom FQDN combined). The default subdomain
	// <slug>.<base> is platform-issued and doesn't count. Default 1 keeps
	// each project tied to a single recognizable URL and bounds the Let's
	// Encrypt SAN size (one cert per project). Bump for projects that
	// genuinely need apex + www + branded variants.
	MaxCustomDomains int `env:"CP_MAX_CUSTOM_DOMAINS" envDefault:"1"`

	// User-app sidecar provisioning. Each service follows the same
	// "admin URL (internal compose network) + public host (what user pods
	// see in their injected env)" pattern as the image registry above.
	// Leaving the admin half empty disables provisioning for that service
	// (manifest.needs.* becomes a no-op; users must inject the env var
	// themselves).
	//
	// PG: control-plane runs CREATE DATABASE / CREATE USER on AdminURL,
	// pods connect using PublicHost in DATABASE_URL.
	UserPGAdminURL   string `env:"CP_USER_PG_ADMIN_URL"`
	UserPGPublicHost string `env:"CP_USER_PG_PUBLIC_HOST"`
	// Redis: ACL SETUSER on AdminURL (default user + admin password),
	// per-project user with key prefix restriction. Pods connect via
	// PublicHost with their project-specific user/password.
	UserRedisAdminURL   string `env:"CP_USER_REDIS_ADMIN_URL"`
	UserRedisPublicHost string `env:"CP_USER_REDIS_PUBLIC_HOST"`
	// S3 / MinIO: reuses the platform's MinIO admin creds (already configured
	// above for source-tarball storage) — that admin can create per-project
	// buckets, users, and policies. PublicHost is what pods see in S3_ENDPOINT.
	UserS3PublicHost   string `env:"CP_USER_S3_PUBLIC_HOST"`
	UserS3PublicUseSSL bool   `env:"CP_USER_S3_PUBLIC_USE_SSL" envDefault:"false"`
}

type BuildService struct {
	DatabaseURL    string `env:"BS_DATABASE_URL,required"`
	S3Endpoint     string `env:"BS_S3_ENDPOINT,required"`
	S3AccessKey    string `env:"BS_S3_ACCESS_KEY,required"`
	S3SecretKey    string `env:"BS_S3_SECRET_KEY,required"`
	S3BucketSource string `env:"BS_S3_BUCKET_SOURCE" envDefault:"vibedeploy-source"`
	S3Region       string `env:"BS_S3_REGION" envDefault:"us-east-1"`
	S3UseSSL       bool   `env:"BS_S3_USE_SSL" envDefault:"false"`

	RegistryHost  string `env:"BS_REGISTRY_HOST,required"`
	WorkDir       string `env:"BS_WORK_DIR" envDefault:"/var/lib/vibedeploy/build"`
	PollInterval  string `env:"BS_POLL_INTERVAL" envDefault:"2s"`
	MaxConcurrent int    `env:"BS_MAX_CONCURRENT" envDefault:"2"`
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
