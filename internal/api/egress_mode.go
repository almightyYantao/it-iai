package api

import (
	"bufio"
	"context"
	"log"
	"net/http"
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
	ts     float64
	port   int
	method string
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
		ts, _ := strconv.ParseFloat(f[0], 64)
		out = append(out, proxyLine{ts: ts, port: port, method: f[3], status: f[2], host: host})
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

// GET /v1/projects/:slug/egress-report — domains the proxy has seen for this
// project (persisted by the collector), most frequent first. Admin only.
func (s *Server) handleEgressReport(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	p := projectFrom(r.Context())
	type row struct {
		Host      string    `json:"host"`
		Allowed   int64     `json:"allowed"`
		Denied    int64     `json:"denied"`
		FirstSeen time.Time `json:"first_seen"`
		LastSeen  time.Time `json:"last_seen"`
	}
	out := []row{}
	rows, err := s.store.Pool.Query(r.Context(),
		`SELECT host, allowed, denied, first_seen, last_seen FROM egress_domain_stats
		  WHERE project_id = $1 ORDER BY allowed + denied DESC, host LIMIT 500`, p.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.Host, &x.Allowed, &x.Denied, &x.FirstSeen, &x.LastSeen); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"domains": out})
}

// RunEgressLogCollector folds the proxy access log into egress_domain_stats
// every interval. Resumes from the newest last_seen so a control-plane restart
// doesn't double count; lines at or before that instant are skipped.
func (s *Server) RunEgressLogCollector(ctx context.Context, interval time.Duration) {
	if s.deployer == nil {
		return
	}
	var cursor float64
	var last *time.Time
	if err := s.store.Pool.QueryRow(ctx, `SELECT max(last_seen) FROM egress_domain_stats`).Scan(&last); err == nil && last != nil {
		cursor = float64(last.UnixMilli()) / 1000
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if c, err := s.collectEgressLog(ctx, cursor); err != nil {
			log.Printf("egress log collector: %v", err)
		} else {
			cursor = c
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) collectEgressLog(ctx context.Context, cursor float64) (float64, error) {
	raw, err := s.deployer.ProxyLogs(ctx, 20000)
	if err != nil {
		return cursor, err
	}
	ports, err := s.store.ProxyPortSlugs(ctx)
	if err != nil {
		return cursor, err
	}
	type key struct{ slug, host string }
	type agg struct {
		allowed, denied int64
		first, last     float64
	}
	m := map[key]*agg{}
	next := cursor
	var lts []float64
	var lslugs, lmethods, lhosts, lstatus []string
	var ldenied []bool
	for _, l := range parseProxyLog(raw) {
		if l.ts <= cursor || l.host == "" {
			continue
		}
		slug, ok := ports[l.port]
		if !ok {
			continue
		}
		k := key{slug, l.host}
		a := m[k]
		if a == nil {
			a = &agg{first: l.ts}
			m[k] = a
		}
		lts = append(lts, l.ts)
		lslugs = append(lslugs, slug)
		lmethods = append(lmethods, l.method)
		lhosts = append(lhosts, l.host)
		lstatus = append(lstatus, l.status)
		ldenied = append(ldenied, strings.Contains(l.status, "DENIED"))
		if strings.Contains(l.status, "DENIED") {
			a.denied++
		} else {
			a.allowed++
		}
		a.last = l.ts
		if l.ts > next {
			next = l.ts
		}
	}
	for k, a := range m {
		_, err := s.store.Pool.Exec(ctx, `
			INSERT INTO egress_domain_stats (project_id, host, allowed, denied, first_seen, last_seen)
			SELECT id, $2, $3, $4, to_timestamp($5), to_timestamp($6) FROM projects
			 WHERE slug = $1 AND deleted_at IS NULL
			ON CONFLICT (project_id, host) DO UPDATE SET
			    allowed = egress_domain_stats.allowed + EXCLUDED.allowed,
			    denied  = egress_domain_stats.denied + EXCLUDED.denied,
			    last_seen = greatest(egress_domain_stats.last_seen, EXCLUDED.last_seen)`,
			k.slug, k.host, a.allowed, a.denied, a.first, a.last)
		if err != nil {
			return cursor, err
		}
	}
	if len(lts) > 0 {
		_, err := s.store.Pool.Exec(ctx, `
			INSERT INTO egress_access_log (ts, project_id, slug, method, host, status, denied)
			SELECT to_timestamp(x.ts), p.id, x.slug, x.method, x.host, x.status, x.denied
			  FROM unnest($1::float8[], $2::text[], $3::text[], $4::text[], $5::text[], $6::bool[])
			       AS x(ts, slug, method, host, status, denied)
			  LEFT JOIN projects p ON p.slug = x.slug AND p.deleted_at IS NULL`,
			lts, lslugs, lmethods, lhosts, lstatus, ldenied)
		if err != nil {
			return cursor, err
		}
	}
	_, _ = s.store.Pool.Exec(ctx, `DELETE FROM egress_access_log WHERE ts < now() - interval '30 days'`)
	return next, nil
}

// GET /v1/admin/egress-logs?project=&q=&status=allowed|denied&limit=&offset=
func (s *Server) handleEgressLogs(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	limit, offset := parsePager(r, 50, 500)
	qs := r.URL.Query()
	where := []string{"true"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if v := strings.TrimSpace(qs.Get("project")); v != "" {
		add("slug = ?", v)
	}
	if v := strings.TrimSpace(qs.Get("q")); v != "" {
		add("host ILIKE ?", "%"+v+"%")
	}
	switch qs.Get("status") {
	case "denied":
		where = append(where, "denied")
	case "allowed":
		where = append(where, "NOT denied")
	}
	cond := strings.Join(where, " AND ")
	ctx := r.Context()
	var total int
	if err := s.store.Pool.QueryRow(ctx, `SELECT count(*) FROM egress_access_log WHERE `+cond, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	type row struct {
		ID     int64     `json:"id"`
		TS     time.Time `json:"ts"`
		Slug   string    `json:"slug"`
		Method string    `json:"method"`
		Host   string    `json:"host"`
		Status string    `json:"status"`
		Denied bool      `json:"denied"`
	}
	out := []row{}
	rows, err := s.store.Pool.Query(ctx,
		`SELECT id, ts, slug, method, host, status, denied FROM egress_access_log WHERE `+cond+
			` ORDER BY ts DESC, id DESC LIMIT `+strconv.Itoa(limit)+` OFFSET `+strconv.Itoa(offset), args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.ID, &x.TS, &x.Slug, &x.Method, &x.Host, &x.Status, &x.Denied); err == nil {
			out = append(out, x)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out, "total": total, "limit": limit, "offset": offset})
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
