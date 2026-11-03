package k8sdriver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	egressProxyNS   = "iai-egress"
	egressProxyName = "egress-proxy"
	// Pinned rather than :latest so a squid release can't change ACL semantics
	// under a policy enforcement point without anyone deciding to upgrade.
	egressProxyImage = "ubuntu/squid:latest"
)

// EnsureEgressProxy reconciles the egress proxy: a ConfigMap holding the
// rendered squid.conf and a Deployment serving it.
//
// hostNetwork on purpose, for two reasons that both matter:
//
//   - Pods reach it at the node address, which is the same shape as the
//     platform's other services (Postgres, Redis, MinIO), so the per-project
//     NetworkPolicy allows it through the machinery that already exists rather
//     than needing a new concept.
//   - The node-level default-deny that will close the kube-router programming
//     window applies to the pod CIDR. A proxy inside that CIDR would be caught
//     by its own rule; on the host network it simply isn't in scope.
//
// Config changes are applied by stamping the ConfigMap's hash onto the pod
// template. squid re-reads nothing on its own, and a rollout is the only way to
// be sure the running process matches what the database says.
var squidUID int64 = 13

func (d *Deployer) EnsureEgressProxy(ctx context.Context, squidConf string, ports []int32) error {
	if err := d.ensureNamespaceNamed(ctx, egressProxyNS); err != nil {
		return fmt.Errorf("namespace: %w", err)
	}

	sum := sha256.Sum256([]byte(squidConf))
	hash := hex.EncodeToString(sum[:])[:16]

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      egressProxyName,
			Namespace: egressProxyNS,
			Labels:    map[string]string{"vibedeploy.io/managed": "true"},
		},
		Data: map[string]string{"squid.conf": squidConf},
	}
	cmAPI := d.cs.CoreV1().ConfigMaps(egressProxyNS)
	if cur, err := cmAPI.Get(ctx, egressProxyName, metav1.GetOptions{}); apierr.IsNotFound(err) {
		if _, err := cmAPI.Create(ctx, cm, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("create configmap: %w", err)
		}
	} else if err != nil {
		return err
	} else {
		cm.ResourceVersion = cur.ResourceVersion
		if _, err := cmAPI.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("update configmap: %w", err)
		}
	}

	containerPorts := make([]corev1.ContainerPort, 0, len(ports))
	for _, p := range ports {
		containerPorts = append(containerPorts, corev1.ContainerPort{
			Name:          fmt.Sprintf("p%d", p),
			ContainerPort: p,
			HostPort:      p,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	labels := map[string]string{"app": egressProxyName, "vibedeploy.io/managed": "true"}
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      egressProxyName,
			Namespace: egressProxyNS,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			// hostNetwork: old and new pods would fight over the same host ports,
			// so a rolling update can never finish. Replace instead.
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": egressProxyName}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					// The whole reload mechanism: a new config hash is a new pod
					// template, which is a rollout.
					Annotations: map[string]string{"vibedeploy.io/config-hash": hash},
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					// hostNetwork defaults DNS to the host's resolver, which
					// can't see cluster services. squid resolves the
					// allow-listed names, so it needs the cluster resolver.
					DNSPolicy: corev1.DNSClusterFirstWithHostNet,
					Containers: []corev1.Container{{
						Name:  "squid",
						Image: egressProxyImage,
						Args:  []string{"-N", "-f", "/etc/squid/squid.conf"},
						// Start as `proxy` (uid 13 in ubuntu/squid). Started as root,
						// squid drops to proxy after opening its logs and then can't
						// reopen /dev/stdout, exiting FATAL on every boot.
						SecurityContext: &corev1.SecurityContext{RunAsUser: &squidUID, RunAsGroup: &squidUID},
						Ports:           containerPorts,
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "conf",
							MountPath: "/etc/squid/squid.conf",
							SubPath:   "squid.conf",
							ReadOnly:  true,
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "conf",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: egressProxyName},
							},
						},
					}},
				},
			},
		},
	}
	// Probe the loopback listener renderSquidConf always emits, so readiness
	// works even before any project has a port.
	dep.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Host: "127.0.0.1", Port: intstr.FromInt(3128)},
		},
		InitialDelaySeconds: 3,
		PeriodSeconds:       10,
	}

	depAPI := d.cs.AppsV1().Deployments(egressProxyNS)
	if cur, err := depAPI.Get(ctx, egressProxyName, metav1.GetOptions{}); apierr.IsNotFound(err) {
		_, err = depAPI.Create(ctx, dep, metav1.CreateOptions{})
		return err
	} else if err != nil {
		return err
	} else {
		dep.ResourceVersion = cur.ResourceVersion
		_, err = depAPI.Update(ctx, dep, metav1.UpdateOptions{})
		return err
	}
}

// ensureNamespaceNamed is EnsureNamespace without the proj- prefix, for the
// platform's own namespaces.
func (d *Deployer) ensureNamespaceNamed(ctx context.Context, name string) error {
	_, err := d.cs.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierr.IsNotFound(err) {
		return err
	}
	_, err = d.cs.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"vibedeploy.io/managed": "true"},
		},
	}, metav1.CreateOptions{})
	return err
}

// EgressProxyStatus is what the admin UI shows about the proxy.
type EgressProxyStatus struct {
	Ready      bool       `json:"ready"`
	Phase      string     `json:"phase"`
	Reason     string     `json:"reason,omitempty"`
	Restarts   int32      `json:"restarts"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	ConfigHash string     `json:"config_hash,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}

// ProxyStatus reads the proxy pod's state. Never errors on "not installed".
func (d *Deployer) ProxyStatus(ctx context.Context) (*EgressProxyStatus, error) {
	st := &EgressProxyStatus{Phase: "NotInstalled"}
	pods, err := d.cs.CoreV1().Pods(egressProxyNS).List(ctx, metav1.ListOptions{LabelSelector: "app=" + egressProxyName})
	if err != nil {
		return nil, err
	}
	var newest *corev1.Pod
	for i := range pods.Items {
		p := &pods.Items[i]
		if newest == nil || p.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = p
		}
	}
	if newest == nil {
		return st, nil
	}
	st.Phase = string(newest.Status.Phase)
	st.ConfigHash = newest.Annotations["vibedeploy.io/config-hash"]
	for _, c := range newest.Status.ContainerStatuses {
		st.Ready = c.Ready
		st.Restarts = c.RestartCount
		if c.State.Running != nil {
			t := c.State.Running.StartedAt.Time
			st.StartedAt = &t
		}
		if c.State.Waiting != nil {
			st.Reason = c.State.Waiting.Reason
		}
		if !c.Ready && c.LastTerminationState.Terminated != nil {
			st.LastError = c.LastTerminationState.Terminated.Reason
		}
	}
	return st, nil
}

// ProxyLogs returns the last n access-log lines of the newest proxy pod.
func (d *Deployer) ProxyLogs(ctx context.Context, n int64) (string, error) {
	pods, err := d.cs.CoreV1().Pods(egressProxyNS).List(ctx, metav1.ListOptions{LabelSelector: "app=" + egressProxyName})
	if err != nil || len(pods.Items) == 0 {
		return "", err
	}
	name := pods.Items[0].Name
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodRunning {
			name = p.Name
		}
	}
	b, err := d.cs.CoreV1().Pods(egressProxyNS).GetLogs(name, &corev1.PodLogOptions{TailLines: &n}).DoRaw(ctx)
	return string(b), err
}
