package k8sdriver

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeOps is one node as the ops dashboard shows it.
type NodeOps struct {
	Name       string     `json:"name"`
	Role       string     `json:"role"` // control-plane | agent
	IP         string     `json:"ip"`
	Ready      bool       `json:"ready"`
	Pressure   []string   `json:"pressure"` // MemoryPressure, DiskPressure, PIDPressure
	CPUMilli   int64      `json:"cpu_milli"`
	CPUCap     int64      `json:"cpu_cap_milli"`
	MemBytes   int64      `json:"mem_bytes"`
	MemCap     int64      `json:"mem_cap_bytes"`
	DiskAvail  int64      `json:"disk_avail_bytes"`
	DiskCap    int64      `json:"disk_cap_bytes"`
	Pods       int        `json:"pods"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	Kubelet    string     `json:"kubelet"`
	HasMetrics bool       `json:"has_metrics"`
	Cordoned   bool       `json:"cordoned"`
}

// PodOps is one pod in a watched namespace (projects + platform).
type PodOps struct {
	Namespace   string     `json:"namespace"`
	Name        string     `json:"name"`
	Node        string     `json:"node"`
	Phase       string     `json:"phase"`
	Reason      string     `json:"reason,omitempty"`
	Ready       bool       `json:"ready"`
	Restarts    int32      `json:"restarts"`
	LastRestart *time.Time `json:"last_restart,omitempty"`
	CPUMilli    int64      `json:"cpu_milli"`
	MemBytes    int64      `json:"mem_bytes"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
}

type metricsList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Usage      map[string]string `json:"usage"`
		Containers []struct {
			Usage map[string]string `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

func qty(s string, milli bool) int64 {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	if milli {
		return q.MilliValue()
	}
	return q.Value()
}

func (d *Deployer) rawMetrics(ctx context.Context, path string) (*metricsList, error) {
	b, err := d.cs.Discovery().RESTClient().Get().AbsPath(path).DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	var m metricsList
	return &m, json.Unmarshal(b, &m)
}

// OpsNodes returns every node with live usage (metrics-server) and disk from
// the kubelet summary. Missing metrics degrade to zeros, never an error.
func (d *Deployer) OpsNodes(ctx context.Context) ([]NodeOps, error) {
	nodes, err := d.cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	usage := map[string]map[string]string{}
	if m, err := d.rawMetrics(ctx, "/apis/metrics.k8s.io/v1beta1/nodes"); err == nil {
		for _, it := range m.Items {
			usage[it.Metadata.Name] = it.Usage
		}
	}
	podCount := map[string]int{}
	if pods, err := d.cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: "status.phase=Running"}); err == nil {
		for _, p := range pods.Items {
			podCount[p.Spec.NodeName]++
		}
	}
	out := make([]NodeOps, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		o := NodeOps{Name: n.Name, Role: "agent", Pressure: []string{}, Kubelet: n.Status.NodeInfo.KubeletVersion, Pods: podCount[n.Name]}
		if _, ok := n.Labels["node-role.kubernetes.io/control-plane"]; ok {
			o.Role = "control-plane"
		}
		for _, a := range n.Status.Addresses {
			if a.Type == corev1.NodeInternalIP {
				o.IP = a.Address
			}
		}
		for _, c := range n.Status.Conditions {
			switch c.Type {
			case corev1.NodeReady:
				o.Ready = c.Status == corev1.ConditionTrue
			case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
				if c.Status == corev1.ConditionTrue {
					o.Pressure = append(o.Pressure, string(c.Type))
				}
			}
		}
		o.Cordoned = n.Spec.Unschedulable
		o.CPUCap = n.Status.Capacity.Cpu().MilliValue()
		o.MemCap = n.Status.Capacity.Memory().Value()
		if u, ok := usage[n.Name]; ok {
			o.HasMetrics = true
			o.CPUMilli = qty(u["cpu"], true)
			o.MemBytes = qty(u["memory"], false)
		}
		// Disk + node start time from the kubelet summary.
		if b, err := d.cs.CoreV1().RESTClient().Get().AbsPath("/api/v1/nodes/" + n.Name + "/proxy/stats/summary").DoRaw(ctx); err == nil {
			var s struct {
				Node struct {
					StartTime time.Time `json:"startTime"`
					Fs        struct {
						Available int64 `json:"availableBytes"`
						Capacity  int64 `json:"capacityBytes"`
					} `json:"fs"`
				} `json:"node"`
			}
			if json.Unmarshal(b, &s) == nil {
				o.DiskAvail, o.DiskCap = s.Node.Fs.Available, s.Node.Fs.Capacity
				if !s.Node.StartTime.IsZero() {
					t := s.Node.StartTime
					o.StartedAt = &t
				}
			}
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role == "control-plane"
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// opsNamespace: projects plus the platform pieces that live in the cluster.
func opsNamespace(ns string) bool {
	switch ns {
	case "kube-system", egressProxyNS, "oauth2-proxy", "cert-manager":
		return true
	}
	return len(ns) > 5 && ns[:5] == "proj-"
}

// OpsPods lists pods in project and platform namespaces with usage.
func (d *Deployer) OpsPods(ctx context.Context) ([]PodOps, error) {
	pods, err := d.cs.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	type k struct{ ns, name string }
	use := map[k][2]int64{}
	if m, err := d.rawMetrics(ctx, "/apis/metrics.k8s.io/v1beta1/pods"); err == nil {
		for _, it := range m.Items {
			var c, mem int64
			for _, ct := range it.Containers {
				c += qty(ct.Usage["cpu"], true)
				mem += qty(ct.Usage["memory"], false)
			}
			use[k{it.Metadata.Namespace, it.Metadata.Name}] = [2]int64{c, mem}
		}
	}
	out := []PodOps{}
	for _, p := range pods.Items {
		if !opsNamespace(p.Namespace) || p.Status.Phase == corev1.PodSucceeded {
			continue
		}
		o := PodOps{Namespace: p.Namespace, Name: p.Name, Node: p.Spec.NodeName, Phase: string(p.Status.Phase), Reason: p.Status.Reason}
		if p.Status.StartTime != nil {
			t := p.Status.StartTime.Time
			o.StartedAt = &t
		}
		o.Ready = len(p.Status.ContainerStatuses) > 0
		for _, c := range p.Status.ContainerStatuses {
			o.Restarts += c.RestartCount
			if !c.Ready {
				o.Ready = false
			}
			if c.State.Waiting != nil && c.State.Waiting.Reason != "" {
				o.Reason = c.State.Waiting.Reason
			}
			if t := c.LastTerminationState.Terminated; t != nil && (o.LastRestart == nil || t.FinishedAt.Time.After(*o.LastRestart)) {
				ft := t.FinishedAt.Time
				o.LastRestart = &ft
			}
		}
		if u, ok := use[k{p.Namespace, p.Name}]; ok {
			o.CPUMilli, o.MemBytes = u[0], u[1]
		}
		out = append(out, o)
	}
	return out, nil
}
