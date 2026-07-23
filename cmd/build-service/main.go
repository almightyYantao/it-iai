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
		Region:    cfg.S3Region,
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

	// Derive a cancellable context for the build so a watchdog can bail out
	// on us mid-build. Motivation: when the owner pushes a new deployment
	// while an old one is still building, SupersedeOlderDeployments flips
	// the old one's DB status to 'superseded', but historically the docker
	// buildx subprocess kept running — and if it hung, the concurrency slot
	// was leaked forever (see recent whale-docs-b7k4 incident). Cancelling
	// buildCtx propagates SIGKILL to the buildx child via exec.CommandContext.
	buildCtx, cancelBuild := context.WithCancel(ctx)
	defer cancelBuild()

	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-buildCtx.Done():
				return
			case <-t.C:
				var status string
				// Deliberately use context.Background — we don't want the
				// poll itself to be cancelled by the very cancel it might
				// need to trigger.
				if err := s.Pool.QueryRow(context.Background(),
					`SELECT status FROM deployments WHERE id = $1`, dep.ID).Scan(&status); err != nil {
					continue
				}
				if status != string(model.DeployBuilding) {
					log.Printf("watchdog: deployment %s moved to %q mid-build; cancelling", dep.ID, status)
					cancelBuild()
					return
				}
			}
		}
	}()

	in := builder.Input{
		DeploymentID: dep.ID.String(),
		Slug:         slug,
		SrcDir:       filepath.Join(workdir, "src"),
	}
	res, err := bld.Build(buildCtx, in, func(line string) {
		emit("build", "info", line)
	})
	cancelBuild()  // stop the watchdog goroutine now regardless of outcome
	<-watchdogDone // and wait for it so we know no further cancels will fire

	if err != nil {
		// Distinguish "buildx killed because superseded / parent shutdown"
		// from a genuine build failure. If the deployment is no longer in
		// 'building', the watchdog (or an operator) killed it — that's the
		// correct outcome, not something we should stamp as failed on top of.
		var status string
		_ = s.Pool.QueryRow(context.Background(),
			`SELECT status FROM deployments WHERE id = $1`, dep.ID).Scan(&status)
		if status != string(model.DeployBuilding) {
			emit("build", "info", "build aborted: deployment status changed to "+status)
			return nil
		}
		fail(ctx, s, dep.ID, "build", err.Error())
		return err
	}
	emit("build", "info", "image pushed: "+res.Image)

	if err := s.SetDeploymentImage(ctx, dep.ID, res.Image); err != nil {
		fail(ctx, s, dep.ID, "set_image", err.Error())
		return err
	}
	// Conditional handoff — if the deployment got superseded between buildx
	// exiting and now, don't clobber the terminal state. TryAdvance returns
	// (false, nil) in that race and we bow out silently.
	advanced, err := s.TryAdvanceDeploymentStatus(ctx, dep.ID, model.DeployBuilding, model.DeployPushing)
	if err != nil {
		return err
	}
	if !advanced {
		emit("build", "info", "build finished but deployment already moved on; skipping handoff")
		return nil
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
