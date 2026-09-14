package k8sdriver

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func node(name string, controlPlane, ready, unschedulable bool) *corev1.Node {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{}},
		Spec:       corev1.NodeSpec{Unschedulable: unschedulable},
	}
	if controlPlane {
		n.Labels[controlPlaneNodeLabel] = "true"
	}
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	n.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: status}}
	return n
}

// The label selector has to exclude control-plane nodes server-side; a fake
// clientset that ignored it would make every case below pass for the wrong
// reason, so the cluster always contains a control-plane node.
func TestSchedulingConstraints(t *testing.T) {
	tests := []struct {
		name    string
		avoid   bool
		nodes   []*corev1.Node
		wantAff bool
	}{
		{
			name:    "pins to agents when one is available",
			avoid:   true,
			nodes:   []*corev1.Node{node("server", true, true, false), node("agent-01", false, true, false)},
			wantAff: true,
		},
		{
			name:    "single-node install keeps scheduling",
			avoid:   true,
			nodes:   []*corev1.Node{node("server", true, true, false)},
			wantAff: false,
		},
		{
			name:    "agent that is not ready does not count",
			avoid:   true,
			nodes:   []*corev1.Node{node("server", true, true, false), node("agent-01", false, false, false)},
			wantAff: false,
		},
		{
			name:    "cordoned agent does not count",
			avoid:   true,
			nodes:   []*corev1.Node{node("server", true, true, false), node("agent-01", false, true, true)},
			wantAff: false,
		},
		{
			name:    "disabled by config",
			avoid:   false,
			nodes:   []*corev1.Node{node("server", true, true, false), node("agent-01", false, true, false)},
			wantAff: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := make([]runtime.Object, 0, len(tt.nodes))
			for _, n := range tt.nodes {
				objs = append(objs, n)
			}
			d := &Deployer{cs: fake.NewSimpleClientset(objs...), avoidControlPlaneNodes: tt.avoid}

			aff, err := d.schedulingConstraints(context.Background())
			if err != nil {
				t.Fatalf("schedulingConstraints: %v", err)
			}
			if tt.wantAff != (aff != nil) {
				t.Fatalf("affinity present = %v, want %v", aff != nil, tt.wantAff)
			}
			if !tt.wantAff {
				return
			}
			req := aff.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
			if req == nil || len(req.NodeSelectorTerms) != 1 {
				t.Fatalf("expected exactly one required node selector term, got %+v", req)
			}
			exprs := req.NodeSelectorTerms[0].MatchExpressions
			if len(exprs) != 1 || exprs[0].Key != controlPlaneNodeLabel ||
				exprs[0].Operator != corev1.NodeSelectorOpDoesNotExist {
				t.Fatalf("expected %s DoesNotExist, got %+v", controlPlaneNodeLabel, exprs)
			}
		})
	}
}
