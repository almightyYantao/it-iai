package api

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/auth"
	"github.com/iai/vibedeploy/internal/k8sdriver"
)

// ---- Persistent paths (admin) -----------------------------------------------

func (s *Server) projectVolumes(ctx context.Context, id uuid.UUID) []k8sdriver.Volume {
	out := []k8sdriver.Volume{}
	rows, err := s.store.Pool.Query(ctx, `SELECT path, size_gi FROM project_volumes WHERE project_id = $1 ORDER BY path`, id)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var v k8sdriver.Volume
		if rows.Scan(&v.Path, &v.SizeGi) == nil {
			out = append(out, v)
		}
	}
	return out
}

// GET /v1/projects/:slug/volumes
func (s *Server) handleListVolumes(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	resp := map[string]any{"volumes": s.projectVolumes(r.Context(), p.ID), "can_edit": isAdminActor(actor),
		"storage_class": k8sdriver.VolumeStorageClass, "storage_ready": s.deployer != nil && s.deployer.StorageClassReady(r.Context())}
	if s.deployer != nil {
		if st, err := s.deployer.VolumeStatuses(r.Context(), p.Slug); err == nil {
			resp["status"] = st
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

var reservedMounts = []string{"/", "/bin", "/sbin", "/usr", "/lib", "/lib64", "/etc", "/proc", "/sys", "/dev", "/var/run/secrets"}

// PUT /v1/projects/:slug/volumes {"volumes":[{"path":"/app/data","size_gi":5}]}
// Admin only. Sizes can only grow; removed paths are unmounted, data kept.
func (s *Server) handlePutVolumes(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "only platform admins can change persistent paths")
		return
	}
	p := projectFrom(r.Context())
	ctx := r.Context()
	var body struct {
		Volumes []k8sdriver.Volume `json:"volumes"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if len(body.Volumes) > 0 && s.deployer != nil && !s.deployer.StorageClassReady(ctx) {
		writeError(w, http.StatusPreconditionFailed, "no_storage",
			"storage class "+k8sdriver.VolumeStorageClass+" is not installed yet; persistent paths are unavailable")
		return
	}
	prev := map[string]int{}
	for _, v := range s.projectVolumes(ctx, p.ID) {
		prev[v.Path] = v.SizeGi
	}
	seen := map[string]bool{}
	clean := []k8sdriver.Volume{}
	for _, v := range body.Volumes {
		pth := path.Clean(strings.TrimSpace(v.Path))
		if !strings.HasPrefix(pth, "/") || pth == "/" {
			writeError(w, http.StatusBadRequest, "bad_path", "path must be absolute, e.g. /app/data")
			return
		}
		for _, rsv := range reservedMounts {
			if pth == rsv || (rsv != "/" && strings.HasPrefix(pth, rsv+"/")) {
				writeError(w, http.StatusBadRequest, "bad_path", pth+" is a system path")
				return
			}
		}
		if v.SizeGi < 1 || v.SizeGi > 500 {
			writeError(w, http.StatusBadRequest, "bad_size", "size must be 1–500 GiB")
			return
		}
		if old, ok := prev[pth]; ok && v.SizeGi < old {
			writeError(w, http.StatusBadRequest, "shrink", fmt.Sprintf("%s: volumes can grow but not shrink (%dGi → %dGi)", pth, old, v.SizeGi))
			return
		}
		if seen[pth] {
			continue
		}
		seen[pth] = true
		clean = append(clean, k8sdriver.Volume{Path: pth, SizeGi: v.SizeGi})
	}
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM project_volumes WHERE project_id = $1`, p.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	for _, v := range clean {
		if _, err := tx.Exec(ctx, `INSERT INTO project_volumes (project_id, path, size_gi) VALUES ($1,$2,$3)`, p.ID, v.Path, v.SizeGi); err != nil {
			writeError(w, http.StatusInternalServerError, "db", err.Error())
			return
		}
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if s.deployer != nil {
		_, _, proxyURL, _ := s.egressConfigForProject(ctx, p.ID)
		if err := s.deployer.SetProxyEnv(ctx, p.Slug, proxyURL, s.quotaFor(ctx, p.ID), clean); err != nil {
			writeError(w, http.StatusInternalServerError, "apply", err.Error())
			return
		}
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(), "project.volumes.update", &p.ID, clean)
	writeJSON(w, http.StatusOK, map[string]any{"volumes": clean})
}

// ---- Logs ------------------------------------------------------------------

// GET /v1/projects/:slug/pods
func (s *Server) handleProjectPods(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if s.deployer == nil {
		writeJSON(w, http.StatusOK, map[string]any{"pods": []any{}})
		return
	}
	pods, err := s.deployer.ProjectPods(r.Context(), p.Slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "k8s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pods": pods, "can_exec": s.canManageProject(actor, p)})
}

// GET /v1/projects/:slug/logs?pod=&container=&tail=500&follow=1&previous=0
// Server-sent events, one `data:` line per log line. Readable by anyone who
// can see the project.
func (s *Server) handleProjectLogs(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	q := r.URL.Query()
	tail, _ := strconv.ParseInt(q.Get("tail"), 10, 64)
	if tail <= 0 || tail > 5000 {
		tail = 500
	}
	if s.deployer == nil {
		writeError(w, http.StatusServiceUnavailable, "no_cluster", "k8s driver disabled")
		return
	}
	rc, err := s.deployer.StreamLogs(r.Context(), p.Slug, q.Get("pod"), q.Get("container"), tail, q.Get("follow") == "1", q.Get("previous") == "1")
	if err != nil {
		writeError(w, http.StatusBadRequest, "logs", err.Error())
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	fl, _ := w.(http.Flusher)
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		b, _ := json.Marshal(sc.Text())
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	fmt.Fprint(w, "event: end\ndata: \"\"\n\n")
	if fl != nil {
		fl.Flush()
	}
}

// ---- Terminal ---------------------------------------------------------------

// POST /v1/projects/:slug/exec-ticket {"pod","container"} — admin or owner.
// Returns a 60s single-use ticket for the WebSocket.
func (s *Server) handleExecTicket(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	if !s.canManageProject(actor, p) {
		writeError(w, http.StatusForbidden, "not_authorised", "only the project owner or an admin can open a terminal")
		return
	}
	var body struct {
		Pod       string `json:"pod"`
		Container string `json:"container"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Pod == "" {
		writeError(w, http.StatusBadRequest, "bad_json", "pod required")
		return
	}
	if body.Container == "" {
		body.Container = "app"
	}
	var raw [24]byte
	_, _ = rand.Read(raw[:])
	tok := base64.RawURLEncoding.EncodeToString(raw[:])
	if _, err := s.store.Pool.Exec(r.Context(), `INSERT INTO exec_tickets (ticket_hash, project_id, pod, container, actor, expires_at)
		VALUES ($1,$2,$3,$4,$5, now() + interval '60 seconds')`, auth.HashToken(tok), p.ID, body.Pod, body.Container, actor.identityString()); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": tok})
}

type wsFrame struct {
	Type string `json:"type"` // input | resize
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// GET /v1/exec?ticket= — WebSocket. Public route; the ticket is the credential.
func (s *Server) handleExecWS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var projectID uuid.UUID
	var pod, container, who string
	err := s.store.Pool.QueryRow(ctx, `UPDATE exec_tickets SET used_at = now()
		WHERE ticket_hash = $1 AND used_at IS NULL AND expires_at > now()
		RETURNING project_id, pod, container, actor`, auth.HashToken(r.URL.Query().Get("ticket"))).Scan(&projectID, &pod, &container, &who)
	if err != nil {
		http.Error(w, "invalid or expired terminal ticket", http.StatusUnauthorized)
		return
	}
	p, err := s.store.GetProjectByID(ctx, projectID)
	if err != nil || s.deployer == nil {
		http.Error(w, "project unavailable", http.StatusNotFound)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	start := time.Now()
	s.store.WriteAudit(ctx, "user", who, "project.exec.open", &p.ID, map[string]any{"pod": pod, "container": container})
	defer func() {
		s.store.WriteAudit(context.Background(), "user", who, "project.exec.close", &p.ID,
			map[string]any{"pod": pod, "seconds": int(time.Since(start).Seconds())})
	}()

	// Hard cap so a forgotten tab doesn't hold a shell forever.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	inR, inW := io.Pipe()
	sizes := make(k8sdriver.TerminalSizeQueue, 4)
	go func() {
		defer inW.Close()
		defer close(sizes)
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			var f wsFrame
			if json.Unmarshal(b, &f) != nil {
				continue
			}
			switch f.Type {
			case "input":
				if _, err := inW.Write([]byte(f.Data)); err != nil {
					return
				}
			case "resize":
				select {
				case sizes <- struct{ Width, Height uint16 }{f.Cols, f.Rows}:
				default:
				}
			}
		}
	}()
	out := wsWriter{ctx: ctx, c: c}
	if err := s.deployer.Exec(ctx, p.Slug, pod, container, inR, out, sizes); err != nil && ctx.Err() == nil {
		_ = c.Write(ctx, websocket.MessageText, []byte("\r\n\x1b[31m"+err.Error()+"\x1b[0m\r\n"))
	}
	c.Close(websocket.StatusNormalClosure, "session ended")
}

type wsWriter struct {
	ctx context.Context
	c   *websocket.Conn
}

func (w wsWriter) Write(b []byte) (int, error) {
	if err := w.c.Write(w.ctx, websocket.MessageBinary, b); err != nil {
		return 0, err
	}
	return len(b), nil
}
