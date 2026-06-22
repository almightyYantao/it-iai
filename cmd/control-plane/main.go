package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iai/vibedeploy/internal/api"
	"github.com/iai/vibedeploy/internal/auth"
	"github.com/iai/vibedeploy/internal/config"
	"github.com/iai/vibedeploy/internal/events"
	"github.com/iai/vibedeploy/internal/k8sdriver"
	"github.com/iai/vibedeploy/internal/storage"
	"github.com/iai/vibedeploy/internal/store"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "seed-dev-token":
			runSeedDevToken()
			return
		case "migrate":
			runMigrate()
			return
		}
	}
	runServer()
}

func runServer() {
	cfg, err := config.LoadControlPlane()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer s.Close()

	if err := s.Migrate(ctx, "/migrations"); err != nil {
		// Fall back to repo path when running outside container.
		if err2 := s.Migrate(ctx, "migrations"); err2 != nil {
			log.Fatalf("migrate: %v / %v", err, err2)
		}
	}

	if err := ensureBootstrapToken(ctx, s, cfg.DevBootstrapToken); err != nil {
		log.Printf("bootstrap token: %v", err)
	}

	kek, err := auth.LoadKEK(cfg.KEKBase64)
	if err != nil {
		log.Fatalf("load kek: %v", err)
	}

	// Runtime config layer: env defaults overlaid with system_config table.
	// Settings-page saves call rt.Reload() and the server hot-swaps the JWT verifier.
	rt := config.NewRuntime(s, cfg)
	if err := rt.Reload(ctx); err != nil {
		log.Printf("runtime config reload: %v (using env defaults)", err)
	}
	kc := rt.KC()
	jwt := auth.NewJWTVerifier(kc.Issuer, kc.Audience, kc.JWKSURL)

	obj, err := storage.New(storage.Options{
		Endpoint:       cfg.S3Endpoint,
		PublicEndpoint: cfg.S3PublicEndpoint,
		AccessKey:      cfg.S3AccessKey,
		SecretKey:      cfg.S3SecretKey,
		Bucket:         cfg.S3BucketSource,
		Region:         cfg.S3Region,
		UseSSL:         cfg.S3UseSSL,
		PublicUseSSL:   cfg.S3PublicUseSSL,
	})
	if err != nil {
		log.Fatalf("minio: %v", err)
	}
	if cfg.S3PublicEndpoint == "" {
		log.Printf("warning: CP_S3_PUBLIC_ENDPOINT is empty; presigned URLs will use %q which is only reachable from inside the cluster network", cfg.S3Endpoint)
	}
	if err := obj.EnsureBucket(ctx); err != nil {
		log.Printf("ensure bucket: %v", err)
	}

	registryHostFromCluster := cfg.RegistryHostFromK3D
	if registryHostFromCluster == "" {
		registryHostFromCluster = cfg.RegistryHost
	}
	dep, err := k8sdriver.New(k8sdriver.DeployerOpts{
		Kubeconfig:              cfg.Kubeconfig,
		BaseDomain:              cfg.AppBaseDomain,
		IngressClass:            cfg.IngressClass,
		TLSClusterIssuer:        cfg.TLSClusterIssuer,
		RegistryHostFromCluster: registryHostFromCluster,
	})
	if err != nil {
		log.Printf("k8s driver disabled: %v (control plane will run but Apply will fail)", err)
	}

	bus := events.NewBus()
	server := api.NewServer(cfg, s, jwt, kek, obj, dep, bus, rt)
	if dep != nil {
		go api.NewReconciler(server).Run(ctx)
		// One-shot ingress sweep so a migration-level change to tls_enabled
		// (or any DB → cluster drift) gets reflected without waiting on a
		// redeploy. Runs in the background to avoid delaying the HTTP listen.
		go server.SweepIngressesAtStartup(ctx)
	}

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("control-plane listening on %s", cfg.ListenAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutting down...")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutCtx)
}

// ensureBootstrapToken creates a platform-wide deploy token if one doesn't exist yet.
// Prints the plaintext value to stderr exactly once on first boot — copy it into the Skill.
func ensureBootstrapToken(ctx context.Context, s *store.Store, preset string) error {
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(1) FROM deploy_tokens WHERE project_id IS NULL`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		log.Printf("bootstrap token already exists; use existing or re-issue with `seed-dev-token --force`")
		return nil
	}
	plaintext := preset
	var hash []byte
	var prefix string
	if plaintext == "" {
		var err error
		plaintext, hash, prefix, err = auth.GenerateToken("live")
		if err != nil {
			return err
		}
	} else {
		hash = auth.HashToken(plaintext)
		prefix = plaintext
		if len(prefix) > 16 {
			prefix = prefix[:16]
		}
	}
	_, err := s.CreateDeployToken(ctx, store.CreateTokenInput{
		Name:      "bootstrap-dev",
		Hash:      hash,
		Prefix:    prefix,
		Scopes:    []string{"admin"},
		AllowDB:   true,
		ExpiresAt: time.Now().Add(365 * 24 * time.Hour),
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\n===== VIBEDEPLOY BOOTSTRAP DEPLOY TOKEN =====\n%s\nSave it. Export it: export VIBEDEPLOY_TOKEN=%s\n=============================================\n\n", plaintext, plaintext)
	return nil
}

func runSeedDevToken() {
	cfg, err := config.LoadControlPlane()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer s.Close()
	plaintext, hash, prefix, err := auth.GenerateToken("live")
	if err != nil {
		log.Fatalf("gen: %v", err)
	}
	_, err = s.CreateDeployToken(ctx, store.CreateTokenInput{
		Name:      "dev-cli",
		Hash:      hash,
		Prefix:    prefix,
		Scopes:    []string{"admin"},
		AllowDB:   true,
		ExpiresAt: time.Now().Add(90 * 24 * time.Hour),
	})
	if err != nil {
		log.Fatalf("create: %v", err)
	}
	fmt.Println(plaintext)
}

func runMigrate() {
	cfg, err := config.LoadControlPlane()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer s.Close()
	if err := s.Migrate(ctx, "/migrations"); err != nil {
		if err2 := s.Migrate(ctx, "migrations"); err2 != nil {
			log.Fatalf("migrate: %v / %v", err, err2)
		}
	}
	log.Printf("migrations applied")
}
