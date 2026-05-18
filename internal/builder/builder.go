package builder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type Builder struct {
	WorkDir      string
	RegistryHost string
}

type Input struct {
	DeploymentID string
	Slug         string
	SrcDir       string // unpacked source dir
}

type Result struct {
	Image string // <registry>/<slug>:<dep_id>
}

// Build invokes nixpacks (or docker build if a Dockerfile is present) and pushes to the registry.
// `onLog` is called for each line of build output so the caller can stream it to deployment_events.
func (b *Builder) Build(ctx context.Context, in Input, onLog func(line string)) (Result, error) {
	image := fmt.Sprintf("%s/%s:%s", b.RegistryHost, in.Slug, in.DeploymentID)

	dockerfile := filepath.Join(in.SrcDir, "Dockerfile")
	var cmd *exec.Cmd
	if _, err := os.Stat(dockerfile); err == nil {
		onLog("Dockerfile detected, using docker build")
		cmd = exec.CommandContext(ctx, "docker", "build",
			"--platform=linux/amd64",
			"-t", image,
			in.SrcDir,
		)
	} else {
		onLog("no Dockerfile, using nixpacks")
		cmd = exec.CommandContext(ctx, "nixpacks", "build",
			in.SrcDir,
			"--name", image,
			"--platform", "linux/amd64",
		)
	}
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	if err := runStream(cmd, onLog); err != nil {
		return Result{}, fmt.Errorf("build: %w", err)
	}

	push := exec.CommandContext(ctx, "docker", "push", image)
	if err := runStream(push, onLog); err != nil {
		return Result{}, fmt.Errorf("push: %w", err)
	}
	return Result{Image: image}, nil
}

func runStream(cmd *exec.Cmd, onLog func(line string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{}, 2)
	scan := func(r io.Reader) {
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 0, 4096), 1024*1024)
		for s.Scan() {
			onLog(s.Text())
		}
		done <- struct{}{}
	}
	go scan(stdout)
	go scan(stderr)
	<-done
	<-done
	return cmd.Wait()
}
