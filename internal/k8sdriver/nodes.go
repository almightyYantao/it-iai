package k8sdriver

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (d *Deployer) guardAgent(ctx context.Context, name string) (*corev1.Node, error) {
	n, err := d.cs.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if _, cp := n.Labels["node-role.kubernetes.io/control-plane"]; cp {
		return nil, fmt.Errorf("%s is the control-plane node; manage it on the host", name)
	}
	return n, nil
}

// SetNodeSchedulable cordons (false) or uncordons (true) an agent node.
func (d *Deployer) SetNodeSchedulable(ctx context.Context, name string, schedulable bool) error {
	if _, err := d.guardAgent(ctx, name); err != nil {
		return err
	}
	patch := fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, !schedulable)
	_, err := d.cs.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

// DrainNode cordons the node and evicts every non-DaemonSet pod via the
// Eviction API (respects PDBs). Returns how many pods were evicted.
func (d *Deployer) DrainNode(ctx context.Context, name string) (int, error) {
	if err := d.SetNodeSchedulable(ctx, name, false); err != nil {
		return 0, err
	}
	pods, err := d.cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + name})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range pods.Items {
		ds := false
		for _, o := range p.OwnerReferences {
			if o.Kind == "DaemonSet" {
				ds = true
			}
		}
		if ds || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		ev := &policyv1.Eviction{ObjectMeta: metav1.ObjectMeta{Name: p.Name, Namespace: p.Namespace}}
		if err := d.cs.PolicyV1().Evictions(p.Namespace).Evict(ctx, ev); err != nil && !apierr.IsNotFound(err) {
			return n, fmt.Errorf("evict %s/%s: %w", p.Namespace, p.Name, err)
		}
		n++
	}
	return n, nil
}

// DeleteNode removes an agent from the cluster. Refuses while it still runs
// non-DaemonSet pods so a mistaken click can't strand workloads.
func (d *Deployer) DeleteNode(ctx context.Context, name string) error {
	if _, err := d.guardAgent(ctx, name); err != nil {
		return err
	}
	pods, err := d.cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "spec.nodeName=" + name})
	if err != nil {
		return err
	}
	for _, p := range pods.Items {
		ds := false
		for _, o := range p.OwnerReferences {
			ds = ds || o.Kind == "DaemonSet"
		}
		if !ds && p.Status.Phase == corev1.PodRunning {
			return fmt.Errorf("%s still runs %s/%s; drain it first", name, p.Namespace, p.Name)
		}
	}
	return d.cs.CoreV1().Nodes().Delete(ctx, name, metav1.DeleteOptions{})
}

// NodeExists reports whether a node with this name has joined.
func (d *Deployer) NodeExists(ctx context.Context, name string) (bool, bool, error) {
	n, err := d.cs.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if apierr.IsNotFound(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return true, c.Status == corev1.ConditionTrue, nil
		}
	}
	return true, false, nil
}
