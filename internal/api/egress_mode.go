package api

import (
	"bufio"
	"context"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var egressModes = map[string]bool{"off": true, "observe": true, "enforce": true}

// PATCH /v1/projects/:slug/egress-mode  {"mode":"off|observe|enforce"}
//
// Admin only. Allocates the proxy listener, re-renders squid, rewrites the
// NetworkPolicy and injects/removes HTTPS_PROXY (one pod rollout).
func (s *Server) handlePatchEgressMode(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "only platform admins can change a project's egress mode")
		return
	}
	p := projectFrom(r.Context())
	var body struct {
		Mode string `json:"mode"`
	}
	if err := decodeJSON(r, &body); err != nil || !egressModes[body.Mode] {
		writeError(w, http.StatusBadRequest, "bad_mode", "mode must be off, observe or enforce")
		return
	}
	ctx := r.Context()
	if body.Mode != "off" {
		if _, err := s.store.EnsureEgressProxyPort(ctx, p.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "port_alloc", err.Error())
			return
		}
	}
	if err := s.store.SetProjectEgressMode(ctx, p.ID, body.Mode); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	// Proxy first so the listener exists before pods are pointed at it.
	if err := s.syncEgressProxy(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "proxy_sync", err.Error())
		return
	}
	if s.deployer != nil {
		allows, enforce, proxyURL, err := s.egressConfigForProject(ctx, p.ID)
		if err == nil {
			err = s.deployer.SyncEgressPolicy(ctx, p.Slug, allows, enforce)
		}
		if err == nil {
			err = s.deployer.SetProxyEnv(ctx, p.Slug, proxyURL)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "egress_sync", err.Error())
			return
		}
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(),
		"project.egress.mode", &p.ID, map[string]any{"mode": body.Mode})
	writeJSON(w, http.StatusOK, map[string]any{"mode": body.Mode})
}

// GET /v1/projects/:slug/egress-mode — readable by anyone who sees the project.
func (s *Server) handleGetEgressMode(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	mode, port, err := s.store.GetProjectEgressMode(r.Context(), p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	actor, _ := ActorFrom(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"mode": mode, "port": port, "can_edit": isAdminActor(actor)})
}

type proxyLine struct {
	port   int
	status string // e.g. TCP_TUNNEL/200, TCP_DENIED/403
	host   string
}

// parseProxyLog reads the `iai` logformat: ts port status method url.
func parseProxyLog(raw string) []proxyLine {
	var out []proxyLine
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 {
			continue
		}
		port, err := strconv.Atoi(f[1])
		if err != nil || port == 3128 {
			continue
		}
		host := f[4]
		if i := strings.Index(host, "://"); i >= 0 {
			host = host[i+3:]
		}
		if i := strings.IndexAny(host, "/"); i >= 0 {
			host = host[:i]
		}
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		out = append(out, proxyLine{port: port, status: f[2], host: host})
	}
	return out
}

// GET /v1/admin/egress-proxy/status
func (s *Server) handleEgressProxyStatus(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	ctx := r.Context()
	counts, _ := s.store.EgressModeCounts(ctx)
	resp := map[string]any{"modes": counts, "checked_at": time.Now()}
	if s.deployer == nil {
		resp["proxy"] = map[string]any{"phase": "NoCluster"}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	st, err := s.deployer.ProxyStatus(ctx)
	if err != nil {
		resp["proxy"] = map[string]any{"phase": "Unknown", "last_error": err.Error()}
	} else {
		resp["proxy"] = st
	}
	allowed, denied := 0, 0
	if raw, err := s.deployer.ProxyLogs(ctx, 5000); err == nil {
		for _, l := range parseProxyLog(raw) {
			if strings.Contains(l.status, "DENIED") {
				denied++
			} else {
				allowed++
			}
		}
	}
	resp["recent"] = map[string]int{"allowed": allowed, "denied": denied}
	writeJSON(w, http.StatusOK, resp)
}

// GET /v1/projects/:slug/egress-report — domains seen in the proxy log for
// this project, most frequent first. Admin only.
func (s *Server) handleEgressReport(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	p := projectFrom(r.Context())
	ctx := r.Context()
	_, port, err := s.store.GetProjectEgressMode(ctx, p.ID)
	type row struct {
		Host    string `json:"host"`
		Allowed int    `json:"allowed"`
		Denied  int    `json:"denied"`
	}
	out := []row{}
	if err != nil || port == 0 || s.deployer == nil {
		writeJSON(w, http.StatusOK, map[string]any{"domains": out})
		return
	}
	raw, _ := s.deployer.ProxyLogs(ctx, 20000)
	agg := map[string]*row{}
	for _, l := range parseProxyLog(raw) {
		if l.port != port || l.host == "" {
			continue
		}
		x := agg[l.host]
		if x == nil {
			x = &row{Host: l.host}
			agg[l.host] = x
		}
		if strings.Contains(l.status, "DENIED") {
			x.Denied++
		} else {
			x.Allowed++
		}
	}
	for _, x := range agg {
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Allowed+out[i].Denied > out[j].Allowed+out[j].Denied })
	writeJSON(w, http.StatusOK, map[string]any{"domains": out})
}

// SweepEgressModesAtStartup converges every project whose egress mode isn't
// off: proxy listener, NetworkPolicy and HTTPS_PROXY env. Lets an admin flip
// modes in bulk in the database and have them take effect on restart.
func (s *Server) SweepEgressModesAtStartup(ctx context.Context) {
	if s.deployer == nil {
		return
	}
	rows, err := s.store.Pool.Query(ctx,
		`SELECT id, slug FROM projects WHERE deleted_at IS NULL AND egress_mode <> 'off' ORDER BY slug`)
	if err != nil {
		log.Printf("egress mode sweep: %v", err)
		return
	}
	type pr struct {
		id   uuid.UUID
		slug string
	}
	var ps []pr
	for rows.Next() {
		var x pr
		if rows.Scan(&x.id, &x.slug) == nil {
			ps = append(ps, x)
		}
	}
	rows.Close()
	for _, p := range ps {
		if _, err := s.store.EnsureEgressProxyPort(ctx, p.id); err != nil {
			log.Printf("egress mode sweep %s: port: %v", p.slug, err)
		}
	}
	// Listeners must exist before pods are pointed at them.
	if err := s.syncEgressProxy(ctx); err != nil {
		log.Printf("egress mode sweep: proxy: %v", err)
		return
	}
	for _, p := range ps {
		allows, enforce, proxyURL, err := s.egressConfigForProject(ctx, p.id)
		if err == nil {
			err = s.deployer.SyncEgressPolicy(ctx, p.slug, allows, enforce)
		}
		if err == nil {
			err = s.deployer.SetProxyEnv(ctx, p.slug, proxyURL)
		}
		if err != nil {
			log.Printf("egress mode sweep %s: %v", p.slug, err)
		}
	}
	log.Printf("egress mode sweep: %d project(s)", len(ps))
}
