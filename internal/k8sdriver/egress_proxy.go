package k8sdriver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

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
						Ports: containerPorts,
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
								corev1.ResourceMemory: resource.MustParse("256Mi"),
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
	// A readiness probe on a listener only works once at least one project has a
	// port; before that squid is up with no listener at all and any probe would
	// fail forever, so it's attached conditionally.
	if len(ports) > 0 {
		dep.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(int(ports[0]))},
			},
			InitialDelaySeconds: 3,
			PeriodSeconds:       10,
		}
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
