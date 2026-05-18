package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	"github.com/iai/vibedeploy/internal/builder"
	"github.com/iai/vibedeploy/internal/config"
	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/storage"
	"github.com/iai/vibedeploy/internal/store"
)

func main() {
	cfg, err := config.LoadBuildService()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	pollInterval, err := time.ParseDuration(cfg.PollInterval)
	if err != nil {
		pollInterval = 2 * time.Second
	}
	if err := os.MkdirAll(cfg.WorkDir, 0o755); err != nil {
		log.Fatalf("mkdir workdir: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	s, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer s.Close()

	obj, err := storage.New(storage.Options{
		Endpoint:  cfg.S3Endpoint,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
		Bucket:    cfg.S3BucketSource,
		UseSSL:    cfg.S3UseSSL,
	})
	if err != nil {
		log.Fatalf("minio: %v", err)
	}

	bld := &builder.Builder{WorkDir: cfg.WorkDir, RegistryHost: cfg.RegistryHost}

	sem := semaphore.NewWeighted(int64(cfg.MaxConcurrent))
	log.Printf("build-service ready; concurrency=%d poll=%s", cfg.MaxConcurrent, pollInterval)

	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("shutting down")
			return
		case <-t.C:
			if !sem.TryAcquire(1) {
				continue
			}
			go func() {
				defer sem.Release(1)
				if err := claimAndRun(ctx, s, obj, bld); err != nil {
					log.Printf("worker: %v", err)
				}
			}()
		}
	}
}

func claimAndRun(ctx context.Context, s *store.Store, obj *storage.Client, bld *builder.Builder) error {
	dep, err := s.ClaimNextQueued(ctx)
	if err != nil {
		return err
	}
	if dep == nil {
		return nil
	}
	log.Printf("claimed deployment %s", dep.ID)
	return run(ctx, s, obj, bld, dep)
}

func run(ctx context.Context, s *store.Store, obj *storage.Client, bld *builder.Builder, dep *model.Deployment) error {
	emit := func(phase, level, msg string) {
		_, _ = s.AppendEvent(ctx, dep.ID, phase, level, msg)
	}
	emit("build", "info", "claimed by build-service")

	slug, err := lookupSlug(ctx, s, dep.ProjectID)
	if err != nil {
		fail(ctx, s, dep.ID, "lookup_slug", err.Error())
		return err
	}

	workdir := filepath.Join(bld.WorkDir, dep.ID.String())
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		fail(ctx, s, dep.ID, "mkdir", err.Error())
		return err
	}
	defer os.RemoveAll(workdir)

	// Fetch source.
	emit("build", "info", "downloading source from object storage")
	if err := downloadAndExtract(ctx, obj, dep.SourceBlobKey, workdir); err != nil {
		fail(ctx, s, dep.ID, "fetch_source", err.Error())
		return err
	}
	emit("build", "info", "source ready")

	in := builder.Input{
		DeploymentID: dep.ID.String(),
		Slug:         slug,
		SrcDir:       filepath.Join(workdir, "src"),
	}
	res, err := bld.Build(ctx, in, func(line string) {
		emit("build", "info", line)
	})
	if err != nil {
		fail(ctx, s, dep.ID, "build", err.Error())
		return err
	}
	emit("build", "info", "image pushed: "+res.Image)

	if err := s.SetDeploymentImage(ctx, dep.ID, res.Image); err != nil {
		fail(ctx, s, dep.ID, "set_image", err.Error())
		return err
	}
	// Hand off to control-plane reconciler.
	if err := s.MarkDeploymentStatus(ctx, dep.ID, model.DeployPushing, ""); err != nil {
		return err
	}
	emit("build", "info", "handed off to control-plane for deploy")
	return nil
}

func lookupSlug(ctx context.Context, s *store.Store, projectID uuid.UUID) (string, error) {
	var slug string
	err := s.Pool.QueryRow(ctx, `SELECT slug FROM projects WHERE id = $1`, projectID).Scan(&slug)
	return slug, err
}

func fail(ctx context.Context, s *store.Store, id uuid.UUID, code, msg string) {
	_, _ = s.AppendEvent(ctx, id, "build", "error", fmt.Sprintf("%s: %s", code, msg))
	_ = s.MarkDeploymentStatus(ctx, id, model.DeployFailed, code+": "+msg)
}

// downloadAndExtract pulls the tar.zst from MinIO and extracts it under <workdir>/src.
// Uses `tar` + `zstd` from PATH so we don't pull in Go libs.
func downloadAndExtract(ctx context.Context, obj *storage.Client, key, workdir string) error {
	if err := os.MkdirAll(filepath.Join(workdir, "src"), 0o755); err != nil {
		return err
	}
	r, err := obj.Get(ctx, key)
	if err != nil {
		return err
	}
	defer r.Close()
	tarball := filepath.Join(workdir, "src.tar.zst")
	f, err := os.Create(tarball)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	f.Close()

	cmd := exec.CommandContext(ctx, "tar", "-I", "zstd", "-xf", tarball, "-C", filepath.Join(workdir, "src"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tar: %v: %s", err, out)
	}
	return nil
}
