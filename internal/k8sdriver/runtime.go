package k8sdriver

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// ---- Persistent paths -------------------------------------------------------

// Volume mounts a persistent claim at Path inside the app container.
type Volume struct {
	Path   string `json:"path"`
	SizeGi int    `json:"size_gi"`
}

// VolumeStorageClass is the class persistent paths are provisioned from.
var VolumeStorageClass = "longhorn"

func volumeName(path string) string {
	h := sha256.Sum256([]byte(path))
	return fmt.Sprintf("data-%x", h[:4])
}

// EnsureVolumes creates or grows one PVC per path. Shrinking is refused by the
// API layer; PVCs for removed paths are kept (data is never deleted implicitly).
func (d *Deployer) EnsureVolumes(ctx context.Context, slug string, vols []Volume) error {
	ns := d.namespaceFor(slug)
	api := d.cs.CoreV1().PersistentVolumeClaims(ns)
	for _, v := range vols {
		name := volumeName(v.Path)
		size := resource.MustParse(fmt.Sprintf("%dGi", v.SizeGi))
		cur, err := api.Get(ctx, name, metav1.GetOptions{})
		if apierr.IsNotFound(err) {
			sc := VolumeStorageClass
			labels := appLabels(slug)
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels,
					Annotations: map[string]string{"iai.io/mount-path": v.Path}},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					StorageClassName: &sc,
					Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: size}},
				},
			}
			if _, err := api.Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
				return fmt.Errorf("create volume for %s: %w", v.Path, err)
			}
			continue
		}
		if err != nil {
			return err
		}
		if cur.Spec.Resources.Requests.Storage().Cmp(size) < 0 {
			cur.Spec.Resources.Requests[corev1.ResourceStorage] = size
			if _, err := api.Update(ctx, cur, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("expand volume for %s: %w", v.Path, err)
			}
		}
	}
	return nil
}

// applyVolumes rewrites the pod template's persistent mounts. RWO volumes can
// attach to one pod at a time, so a rolling update would deadlock: switch to
// Recreate whenever any are mounted.
func applyVolumes(spec *appsv1.DeploymentSpec, vols []Volume) {
	ps := &spec.Template.Spec
	keep := ps.Volumes[:0]
	for _, v := range ps.Volumes {
		if !strings.HasPrefix(v.Name, "data-") {
			keep = append(keep, v)
		}
	}
	ps.Volumes = keep
	for i := range ps.Containers {
		if ps.Containers[i].Name != "app" {
			continue
		}
		c := &ps.Containers[i]
		mk := c.VolumeMounts[:0]
		for _, m := range c.VolumeMounts {
			if !strings.HasPrefix(m.Name, "data-") {
				mk = append(mk, m)
			}
		}
		c.VolumeMounts = mk
		for _, v := range vols {
			n := volumeName(v.Path)
			c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{Name: n, MountPath: v.Path})
		}
	}
	for _, v := range vols {
		n := volumeName(v.Path)
		ps.Volumes = append(ps.Volumes, corev1.Volume{Name: n,
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: n}}})
	}
	if len(vols) > 0 {
		spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	} else {
		spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType}
	}
}

// VolumeStatus reports each claim's phase and capacity.
type VolumeStatus struct {
	Path     string `json:"path"`
	Phase    string `json:"phase"`
	Capacity string `json:"capacity"`
}

func (d *Deployer) VolumeStatuses(ctx context.Context, slug string) ([]VolumeStatus, error) {
	list, err := d.cs.CoreV1().PersistentVolumeClaims(d.namespaceFor(slug)).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := []VolumeStatus{}
	for _, p := range list.Items {
		st := VolumeStatus{Path: p.Annotations["iai.io/mount-path"], Phase: string(p.Status.Phase)}
		if c, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
			st.Capacity = c.String()
		}
		out = append(out, st)
	}
	return out, nil
}

// ---- Pods, logs, exec -------------------------------------------------------

type ContainerInfo struct {
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`
	State    string `json:"state"`
}
type PodInfo struct {
	Name       string          `json:"name"`
	Node       string          `json:"node"`
	Phase      string          `json:"phase"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	Containers []ContainerInfo `json:"containers"`
}

// ProjectPods lists the app pods of a project, newest first.
func (d *Deployer) ProjectPods(ctx context.Context, slug string) ([]PodInfo, error) {
	pods, err := d.cs.CoreV1().Pods(d.namespaceFor(slug)).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].CreationTimestamp.After(pods.Items[j].CreationTimestamp.Time)
	})
	out := []PodInfo{}
	for _, p := range pods.Items {
		pi := PodInfo{Name: p.Name, Node: p.Spec.NodeName, Phase: string(p.Status.Phase)}
		if p.Status.StartTime != nil {
			t := p.Status.StartTime.Time
			pi.StartedAt = &t
		}
		for _, c := range p.Status.ContainerStatuses {
			st := "running"
			if c.State.Waiting != nil {
				st = c.State.Waiting.Reason
			} else if c.State.Terminated != nil {
				st = c.State.Terminated.Reason
			}
			pi.Containers = append(pi.Containers, ContainerInfo{Name: c.Name, Ready: c.Ready, Restarts: c.RestartCount, State: st})
		}
		out = append(out, pi)
	}
	return out, nil
}

// podInProject refuses names outside the project's namespace.
func (d *Deployer) podInProject(ctx context.Context, slug, pod string) error {
	_, err := d.cs.CoreV1().Pods(d.namespaceFor(slug)).Get(ctx, pod, metav1.GetOptions{})
	return err
}

// StreamLogs streams a container's log. previous=true reads the last crashed run.
func (d *Deployer) StreamLogs(ctx context.Context, slug, pod, container string, tail int64, follow, previous bool) (io.ReadCloser, error) {
	if err := d.podInProject(ctx, slug, pod); err != nil {
		return nil, err
	}
	opts := &corev1.PodLogOptions{Container: container, Follow: follow, Previous: previous, Timestamps: true}
	if tail > 0 {
		opts.TailLines = &tail
	}
	return d.cs.CoreV1().Pods(d.namespaceFor(slug)).GetLogs(pod, opts).Stream(ctx)
}

// TerminalSizeQueue adapts a channel of sizes for remotecommand.
type TerminalSizeQueue chan remotecommand.TerminalSize

func (q TerminalSizeQueue) Next() *remotecommand.TerminalSize {
	s, ok := <-q
	if !ok {
		return nil
	}
	return &s
}

// Exec opens an interactive TTY shell in the container.
func (d *Deployer) Exec(ctx context.Context, slug, pod, container string, stdin io.Reader, stdout io.Writer, sizes TerminalSizeQueue) error {
	if err := d.podInProject(ctx, slug, pod); err != nil {
		return err
	}
	req := d.cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(d.namespaceFor(slug)).Name(pod).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			// bash when present, else sh — distroless images have neither and fail visibly.
			Command: []string{"/bin/sh", "-c", "command -v bash >/dev/null && exec bash -l || exec sh"},
			Stdin:   true, Stdout: true, TTY: true,
		}, scheme.ParameterCodec)
	ex, err := remotecommand.NewSPDYExecutor(d.restCfg, "POST", req.URL())
	if err != nil {
		return err
	}
	return ex.StreamWithContext(ctx, remotecommand.StreamOptions{Stdin: stdin, Stdout: stdout, Tty: true, TerminalSizeQueue: sizes})
}

// ---- Crash detection ---------------------------------------------------------

// Crash is one container termination worth alerting on.
type Crash struct {
	Slug       string    `json:"slug"`
	Pod        string    `json:"pod"`
	PodUID     string    `json:"pod_uid"`
	Container  string    `json:"container"`
	Reason     string    `json:"reason"`
	ExitCode   int32     `json:"exit_code"`
	FinishedAt time.Time `json:"finished_at"`
	Restarts   int32     `json:"restarts"`
}

// RecentCrashes returns project-container terminations (not clean exits) that
// finished after since.
func (d *Deployer) RecentCrashes(ctx context.Context, since time.Time) ([]Crash, error) {
	pods, err := d.cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var out []Crash
	for _, p := range pods.Items {
		if !strings.HasPrefix(p.Namespace, "proj-") {
			continue
		}
		for _, c := range p.Status.ContainerStatuses {
			t := c.LastTerminationState.Terminated
			if t == nil {
				t = c.State.Terminated
			}
			if t == nil || t.FinishedAt.Time.Before(since) || (t.ExitCode == 0 && t.Reason == "Completed") {
				continue
			}
			out = append(out, Crash{Slug: strings.TrimPrefix(p.Namespace, "proj-"), Pod: p.Name, PodUID: string(p.UID),
				Container: c.Name, Reason: t.Reason, ExitCode: t.ExitCode, FinishedAt: t.FinishedAt.Time, Restarts: c.RestartCount})
		}
	}
	return out, nil
}

// PreviousLogTail returns the last n lines of the crashed run.
func (d *Deployer) PreviousLogTail(ctx context.Context, slug, pod, container string, n int64) string {
	rc, err := d.cs.CoreV1().Pods(d.namespaceFor(slug)).GetLogs(pod, &corev1.PodLogOptions{Container: container, Previous: true, TailLines: &n}).Stream(ctx)
	if err != nil {
		return ""
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, 8192))
	return string(b)
}

// StorageClassReady reports whether the persistent-path storage class exists.
func (d *Deployer) StorageClassReady(ctx context.Context) bool {
	_, err := d.cs.StorageV1().StorageClasses().Get(ctx, VolumeStorageClass, metav1.GetOptions{})
	return err == nil
}
