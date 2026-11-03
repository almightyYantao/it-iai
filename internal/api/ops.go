package api

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/iai/vibedeploy/internal/k8sdriver"
)

// Ops dashboard. Platform services run in docker-compose next to the
// control-plane, which deliberately has no docker.sock — so they're checked by
// probing their ports / health endpoints instead.

type serviceProbe struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // compose | cluster
	OK      bool   `json:"ok"`
	Detail  string `json:"detail,omitempty"`
	Latency int64  `json:"latency_ms"`
}

type opsAlert struct {
	Level  string `json:"level"` // warn | crit
	Target string `json:"target"`
	Text   string `json:"text"`
	Link   string `json:"link,omitempty"`
}

func hostPort(raw, def string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		raw = u.Host
	}
	if _, _, err := net.SplitHostPort(raw); err != nil {
		raw = net.JoinHostPort(raw, def)
	}
	return raw
}

func tcpProbe(ctx context.Context, name, addr string, send, expect string) serviceProbe {
	p := serviceProbe{Name: name, Kind: "compose"}
	if addr == "" {
		p.Detail = "not configured"
		return p
	}
	t0 := time.Now()
	d := net.Dialer{Timeout: 3 * time.Second}
	c, err := d.DialContext(ctx, "tcp", addr)
	p.Latency = time.Since(t0).Milliseconds()
	if err != nil {
		p.Detail = err.Error()
		return p
	}
	defer c.Close()
	if send != "" {
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := c.Write([]byte(send)); err != nil {
			p.Detail = err.Error()
			return p
		}
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		reply := string(buf[:n])
		// Any well-formed reply means the service is up; an auth challenge
		// (Redis with requirepass answers PING with -NOAUTH) counts too.
		if !strings.Contains(reply, expect) && !strings.Contains(reply, "NOAUTH") {
			p.Detail = fmt.Sprintf("unexpected reply %q", strings.TrimSpace(reply))
			return p
		}
	}
	p.OK = true
	return p
}

func httpProbe(ctx context.Context, name, u string, okCodes ...int) serviceProbe {
	p := serviceProbe{Name: name, Kind: "compose"}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	t0 := time.Now()
	resp, err := http.DefaultClient.Do(req)
	p.Latency = time.Since(t0).Milliseconds()
	if err != nil {
		p.Detail = err.Error()
		return p
	}
	resp.Body.Close()
	for _, c := range okCodes {
		if resp.StatusCode == c {
			p.OK = true
			return p
		}
	}
	p.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
	return p
}

func (s *Server) probeServices(ctx context.Context) []serviceProbe {
	host := s.proxyHost() // platform node address
	checks := []func() serviceProbe{
		func() serviceProbe { return serviceProbe{Name: "control-plane", Kind: "compose", OK: true} },
		func() serviceProbe {
			p := serviceProbe{Name: "postgres", Kind: "compose"}
			t0 := time.Now()
			err := s.store.Pool.Ping(ctx)
			p.Latency, p.OK = time.Since(t0).Milliseconds(), err == nil
			if err != nil {
				p.Detail = err.Error()
			}
			return p
		},
		func() serviceProbe {
			return tcpProbe(ctx, "user-postgres", hostPort(s.cfg.UserPGPublicHost, "5432"), "", "")
		},
		func() serviceProbe {
			return tcpProbe(ctx, "redis", hostPort(s.cfg.UserRedisPublicHost, "6379"), "PING\r\n", "PONG")
		},
		func() serviceProbe {
			ep := hostPort(s.cfg.S3Endpoint, "9000")
			return httpProbe(ctx, "minio", "http://"+ep+"/minio/health/live", 200)
		},
		func() serviceProbe {
			if host == "" {
				return serviceProbe{Name: "registry", Kind: "compose", Detail: "not configured"}
			}
			return httpProbe(ctx, "registry", "http://"+net.JoinHostPort(host, "5001")+"/v2/", 200, 401)
		},
		func() serviceProbe { return httpProbe(ctx, "web", "http://web/", 200) },
		func() serviceProbe { return s.probeBuildService(ctx) },
	}
	out := make([]serviceProbe, len(checks))
	var wg sync.WaitGroup
	for i, f := range checks {
		wg.Add(1)
		go func(i int, f func() serviceProbe) { defer wg.Done(); out[i] = f() }(i, f)
	}
	wg.Wait()
	return out
}

// build-service has no listener; judge it by the queue: a build that has sat
// in queued/building for over 20 minutes means the worker is stuck or down.
func (s *Server) probeBuildService(ctx context.Context) serviceProbe {
	p := serviceProbe{Name: "build-service", Kind: "compose"}
	var stuck int
	err := s.store.Pool.QueryRow(ctx, `SELECT count(*) FROM deployments
		WHERE status IN ('queued','building') AND created_at < now() - interval '20 minutes'`).Scan(&stuck)
	if err != nil {
		p.Detail = err.Error()
		return p
	}
	p.OK = stuck == 0
	if stuck > 0 {
		p.Detail = fmt.Sprintf("%d build(s) stuck > 20m", stuck)
	}
	return p
}

// GET /v1/admin/ops
func (s *Server) handleOps(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	resp, _ := s.opsSnapshot(r.Context())
	writeJSON(w, http.StatusOK, resp)
}

// opsSnapshot gathers everything the dashboard and the notifier need.
func (s *Server) opsSnapshot(ctx context.Context) (map[string]any, []opsAlert) {
	resp := map[string]any{"checked_at": time.Now()}
	var nodes []k8sdriver.NodeOps
	var pods []k8sdriver.PodOps
	alerts := []opsAlert{}
	services := s.probeServices(ctx)
	if s.deployer == nil {
		alerts = append(alerts, opsAlert{"crit", "cluster", "k8s driver disabled", ""})
	} else {
		var err error
		if nodes, err = s.deployer.OpsNodes(ctx); err != nil {
			alerts = append(alerts, opsAlert{"crit", "cluster", "kube API: " + err.Error(), ""})
		}
		if pods, err = s.deployer.OpsPods(ctx); err != nil {
			alerts = append(alerts, opsAlert{"crit", "cluster", "list pods: " + err.Error(), ""})
		}
	}
	if nodes == nil {
		nodes = []k8sdriver.NodeOps{}
	}
	if pods == nil {
		pods = []k8sdriver.PodOps{}
	}
	// Cluster-side platform services, derived from their pods.
	for _, c := range []struct{ name, ns, prefix string }{
		{"traefik", "kube-system", "traefik-"}, {"egress-proxy", "iai-egress", "egress-proxy-"},
		{"oauth2-proxy", "oauth2-proxy", "oauth2-proxy-"}, {"coredns", "kube-system", "coredns-"},
	} {
		sp := serviceProbe{Name: c.name, Kind: "cluster", Detail: "no pod"}
		total, ready := 0, 0
		for _, p := range pods {
			if p.Namespace == c.ns && strings.HasPrefix(p.Name, c.prefix) && p.Phase != "Failed" {
				total++
				if p.Ready {
					ready++
				}
			}
		}
		if total > 0 {
			sp.OK = ready == total
			sp.Detail = fmt.Sprintf("%d/%d ready", ready, total)
		}
		services = append(services, sp)
	}

	for _, n := range nodes {
		if !n.Ready {
			alerts = append(alerts, opsAlert{"crit", n.Name, "节点 NotReady", ""})
		}
		for _, pr := range n.Pressure {
			alerts = append(alerts, opsAlert{"crit", n.Name, pr, ""})
		}
		if n.MemCap > 0 && n.HasMetrics {
			pct := n.MemBytes * 100 / n.MemCap
			if pct >= 95 {
				alerts = append(alerts, opsAlert{"crit", n.Name, fmt.Sprintf("内存 %d%%", pct), ""})
			} else if pct >= 85 {
				alerts = append(alerts, opsAlert{"warn", n.Name, fmt.Sprintf("内存 %d%%", pct), ""})
			}
		}
		if n.DiskCap > 0 {
			free := n.DiskAvail * 100 / n.DiskCap
			if free <= 10 {
				alerts = append(alerts, opsAlert{"crit", n.Name, fmt.Sprintf("磁盘剩余 %d%%", free), ""})
			} else if free <= 20 {
				alerts = append(alerts, opsAlert{"warn", n.Name, fmt.Sprintf("磁盘剩余 %d%%", free), ""})
			}
		}
	}
	for _, sp := range services {
		if !sp.OK {
			alerts = append(alerts, opsAlert{"crit", sp.Name, "服务异常: " + sp.Detail, ""})
		}
	}
	for _, p := range pods {
		link := ""
		if strings.HasPrefix(p.Namespace, "proj-") {
			link = "/projects/" + strings.TrimPrefix(p.Namespace, "proj-")
		}
		recent := p.LastRestart != nil && time.Since(*p.LastRestart) < time.Hour
		switch {
		case p.Phase != "Running" && p.Phase != "Pending":
			alerts = append(alerts, opsAlert{"crit", p.Namespace + "/" + p.Name, "Pod " + p.Phase + " " + p.Reason, link})
		case p.Reason == "CrashLoopBackOff" || p.Reason == "ImagePullBackOff" || p.Reason == "ErrImagePull":
			alerts = append(alerts, opsAlert{"crit", p.Namespace + "/" + p.Name, p.Reason, link})
		case recent && p.Restarts >= 3:
			alerts = append(alerts, opsAlert{"warn", p.Namespace + "/" + p.Name, fmt.Sprintf("重启 %d 次，最近 1 小时内有重启", p.Restarts), link})
		}
	}
	resp["nodes"], resp["pods"], resp["services"], resp["alerts"] = nodes, pods, services, alerts
	return resp, alerts
}

// GET /v1/admin/ops/history?hours=1 — per-node cpu/mem points.
func (s *Server) handleOpsHistory(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	hours := 1
	if h := r.URL.Query().Get("hours"); h != "" {
		fmt.Sscanf(h, "%d", &hours)
	}
	if hours < 1 || hours > 168 {
		hours = 1
	}
	rows, err := s.store.Pool.Query(r.Context(), `
		SELECT node, ts, cpu_milli, cpu_cap, mem_bytes, mem_cap FROM ops_samples
		 WHERE ts > now() - make_interval(hours => $1) ORDER BY node, ts`, hours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer rows.Close()
	type pt struct {
		TS  time.Time `json:"ts"`
		CPU float64   `json:"cpu"`
		Mem float64   `json:"mem"`
	}
	out := map[string][]pt{}
	for rows.Next() {
		var node string
		var ts time.Time
		var c, cc, m, mc int64
		if rows.Scan(&node, &ts, &c, &cc, &m, &mc) != nil {
			continue
		}
		p := pt{TS: ts}
		if cc > 0 {
			p.CPU = float64(c) * 100 / float64(cc)
		}
		if mc > 0 {
			p.Mem = float64(m) * 100 / float64(mc)
		}
		out[node] = append(out[node], p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"series": out})
}

// RunOpsSampler stores node usage once a minute and keeps 7 days.
func (s *Server) RunOpsSampler(ctx context.Context, interval time.Duration) {
	if s.deployer == nil {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if nodes, err := s.deployer.OpsNodes(ctx); err == nil {
			for _, n := range nodes {
				if !n.HasMetrics {
					continue
				}
				if _, err := s.store.Pool.Exec(ctx, `INSERT INTO ops_samples (node, cpu_milli, cpu_cap, mem_bytes, mem_cap)
					VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, n.Name, n.CPUMilli, n.CPUCap, n.MemBytes, n.MemCap); err != nil {
					log.Printf("ops sampler: %v", err)
					break
				}
			}
			_, _ = s.store.Pool.Exec(ctx, `DELETE FROM ops_samples WHERE ts < now() - interval '7 days'`)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
