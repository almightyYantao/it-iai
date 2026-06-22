package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/iai/vibedeploy/internal/auth"
	"github.com/iai/vibedeploy/internal/config"
	"github.com/iai/vibedeploy/internal/events"
	"github.com/iai/vibedeploy/internal/k8sdriver"
	"github.com/iai/vibedeploy/internal/provision"
	"github.com/iai/vibedeploy/internal/storage"
	"github.com/iai/vibedeploy/internal/store"
)

type Server struct {
	cfg      config.ControlPlane
	store    *store.Store
	// jwt is rebuilt on Settings-save, so reads must go through atomic.Pointer.
	// Use s.JWT() rather than touching jwtPtr directly.
	jwtPtr   atomic.Pointer[auth.JWTVerifier]
	kek      *auth.KEK
	objects  *storage.Client
	deployer *k8sdriver.Deployer
	bus      *events.Bus
	runtime  *config.Runtime
	// Optional sidecar provisioners. Each one may be nil/disabled — the
	// reconciler checks Enabled() before honouring manifest.needs.*.
	pgProv    *provision.PostgresProvisioner
	redisProv *provision.RedisProvisioner
	s3Prov    *provision.S3Provisioner
}

func NewServer(cfg config.ControlPlane, s *store.Store, jwt *auth.JWTVerifier, kek *auth.KEK, obj *storage.Client, dep *k8sdriver.Deployer, bus *events.Bus, rt *config.Runtime) *Server {
	srv := &Server{
		cfg: cfg, store: s, kek: kek, objects: obj, deployer: dep, bus: bus, runtime: rt,
		// Provisioners are nil when the corresponding env vars are empty —
		// platform won't try to auto-create DBs / Redis URLs.
		pgProv:    provision.NewPostgresProvisioner(cfg.UserPGAdminURL, cfg.UserPGPublicHost),
		redisProv: provision.NewRedisProvisioner(cfg.UserRedisAdminURL, cfg.UserRedisPublicHost),
		// S3 reuses the platform's MinIO admin creds (same instance, namespaced
		// per project). UserS3PublicHost can differ from the admin endpoint
		// when pods need a routable IP instead of the compose service name.
		s3Prov: provision.NewS3Provisioner(
			cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey,
			cfg.UserS3PublicHost, cfg.S3Region,
			cfg.S3UseSSL, cfg.UserS3PublicUseSSL,
		),
	}
	srv.jwtPtr.Store(jwt)
	return srv
}

// JWT returns the currently-active JWT verifier. Settings updates swap it out
// via RebuildJWTVerifier without touching the request path.
func (s *Server) JWT() *auth.JWTVerifier { return s.jwtPtr.Load() }

// RebuildJWTVerifier creates a new verifier from the current runtime KC view
// and atomically swaps it in. Callers fire this after r.runtime.Reload().
func (s *Server) RebuildJWTVerifier() {
	kc := s.runtime.KC()
	s.jwtPtr.Store(auth.NewJWTVerifier(kc.Issuer, kc.Audience, kc.JWKSURL))
}

func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)
	r.Use(loggingMiddleware)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	// Internal endpoint for Traefik ForwardAuth (called only from cluster network).
	r.Get("/internal/auth/check", s.handleAuthCheck)

	// OIDC code-flow endpoints — unauthenticated by design.
	r.Get("/v1/auth/oidc-status",   s.handleOIDCStatus)
	r.Get("/v1/auth/oidc-login",    s.handleOIDCLogin)
	r.Get("/v1/auth/oidc-callback", s.handleOIDCCallback)

	r.Route("/v1", func(r chi.Router) {
		r.Use(s.AuthMiddleware)

		r.Get("/whoami", s.handleWhoami)

		r.Route("/admin", func(r chi.Router) {
			r.Get("/projects", s.handleAdminProjects)
			r.Get("/metrics", s.handleAdminMetrics)
			r.Get("/audit", s.handleAdminAudit)
			r.Get("/users", s.handleAdminListUsers)
			r.Patch("/users/{id}", s.handleAdminPatchUser)
			r.Get("/settings", s.handleAdminGetSettings)
			r.Patch("/settings", s.handleAdminPatchSettings)
			r.Get("/cidr-presets", s.handleListCIDRPresets)
			r.Put("/cidr-presets/{name}", s.handlePutCIDRPreset)
			r.Delete("/cidr-presets/{name}", s.handleDeleteCIDRPreset)
		})

		r.Route("/projects", func(r chi.Router) {
			r.Get("/", s.handleListProjects)
			r.Post("/", s.handleCreateProject)
			r.Route("/{slug}", func(r chi.Router) {
				r.Use(s.requireProjectAccess)
				r.Get("/", s.handleGetProject)
				r.Delete("/", s.handleDeleteProject)
				r.Patch("/access", s.handlePatchProjectAccess)
				r.Patch("/tls", s.handlePatchProjectTLS)
				r.Patch("/name", s.handlePatchProjectName)
				r.Patch("/visibility", s.handlePatchProjectVisibility)
				r.Route("/deployments", func(r chi.Router) {
					r.Post("/", s.handleCreateDeployment)
					r.Get("/", s.handleListDeployments)
					r.Get("/{id}", s.handleGetDeployment)
					r.Get("/{id}/events", s.handleDeploymentEvents)
					r.Get("/{id}/logs", s.handleDeploymentLogs)
					r.Post("/{id}/uploaded", s.handleDeploymentUploaded)
				})
				r.Route("/domains", func(r chi.Router) {
					r.Get("/", s.handleListDomains)
					r.Post("/", s.handleAddDomain)
					r.Delete("/{hostname}", s.handleRemoveDomain)
				})
				r.Route("/collaborators", func(r chi.Router) {
					r.Get("/", s.handleListCollaborators)
					r.Post("/", s.handleAddCollaborator)
					r.Delete("/{email}", s.handleRemoveCollaborator)
				})
				r.Route("/env", func(r chi.Router) {
					r.Get("/", s.handleListProjectEnv)
					r.Put("/", s.handlePutProjectEnv)
					r.Delete("/{key}", s.handleDeleteProjectEnv)
				})
			})
		})
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		log.Printf("%s %s -> %d (%dB)", r.Method, r.URL.Path, ww.Status(), ww.BytesWritten())
	})
}

// corsMiddleware permits the admin Web UI to call the API from a different origin
// during dev. In M1 we accept any origin (internal network, dev laptops); lock down in M3.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Last-Event-ID, Accept")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "300")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
