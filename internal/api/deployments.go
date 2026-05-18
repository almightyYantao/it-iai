package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/storage"
	"github.com/iai/vibedeploy/internal/store"
)

type createDeploymentRequest struct {
	Manifest json.RawMessage `json:"manifest"`
}

type createDeploymentResponse struct {
	DeploymentID string `json:"deployment_id"`
	UploadURL    string `json:"upload_url"`
	UploadExpiry int    `json:"upload_expiry_seconds"`
	SourceKey    string `json:"source_key"`
	EventsURL    string `json:"events_url"`
}

func (s *Server) handleCreateDeployment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := projectFrom(ctx)
	actor, _ := ActorFrom(ctx)

	var req createDeploymentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	if len(req.Manifest) == 0 {
		writeError(w, http.StatusBadRequest, "no_manifest", "manifest required")
		return
	}

	dep, err := s.store.CreateDeployment(ctx, p.ID, string(actor.Kind), actor.identityString(), req.Manifest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}

	key := storage.SourceKey(p.Slug, dep.ID.String())
	url, err := s.objects.PresignedUploadURL(ctx, key, 15*time.Minute)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "minio", err.Error())
		return
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(), "deployment.create", &p.ID, map[string]string{"deployment_id": dep.ID.String()})
	// events_url must point to the host the *client* used to reach us, not whatever
	// the platform node thinks its IP is. See externalBaseURL for the precedence.
	base := s.externalBaseURL(r)
	writeJSON(w, http.StatusCreated, createDeploymentResponse{
		DeploymentID: dep.ID.String(),
		UploadURL:    url,
		UploadExpiry: 15 * 60,
		SourceKey:    key,
		EventsURL:    fmt.Sprintf("%s/v1/projects/%s/deployments/%s/events", base, p.Slug, dep.ID),
	})
}

// handleDeploymentUploaded is called by the Skill after the source tarball PUT succeeded.
// It flips the deployment to "queued" so a Build Service worker can claim it.
func (s *Server) handleDeploymentUploaded(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", err.Error())
		return
	}
	p := projectFrom(ctx)
	dep, err := s.store.GetDeployment(ctx, depID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "deployment not found")
			return
		}
		log.Printf("handleDeploymentUploaded: GetDeployment(%s) failed: %v", depID, err)
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if dep.ProjectID != p.ID {
		writeError(w, http.StatusNotFound, "not_found", "deployment not found")
		return
	}
	key := storage.SourceKey(p.Slug, dep.ID.String())
	if err := s.store.MarkDeploymentQueued(ctx, dep.ID, key); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if err := s.store.SupersedeOlderDeployments(ctx, p.ID, dep.ID); err != nil {
		// non-fatal
		_, _ = s.store.AppendEvent(ctx, dep.ID, "queue", "warn", "supersede older deployments: "+err.Error())
	}
	if _, err := s.store.AppendEvent(ctx, dep.ID, "queue", "info", "source uploaded; queued for build"); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	s.bus.Publish(dep.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListDeployments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := projectFrom(ctx)
	limit, offset := parsePager(r, 10, 200)
	var total int
	_ = s.store.Pool.QueryRow(ctx, `SELECT count(1) FROM deployments WHERE project_id = $1`, p.ID).Scan(&total)
	list, err := s.store.ListDeployments(ctx, p.ID, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deployments": list,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

func (s *Server) handleGetDeployment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := projectFrom(ctx)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", err.Error())
		return
	}
	dep, err := s.store.GetDeployment(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "deployment not found")
			return
		}
		log.Printf("handleGetDeployment: GetDeployment(%s) failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if dep.ProjectID != p.ID {
		writeError(w, http.StatusNotFound, "not_found", "deployment not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deployment": dep})
}

func (s *Server) handleDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := projectFrom(ctx)
	phase := r.URL.Query().Get("phase")
	tail := int64(200)
	if v := r.URL.Query().Get("n"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 5000 {
			tail = int64(n)
		}
	}
	if phase == "runtime" {
		out, err := s.deployer.TailLogs(ctx, p.Slug, tail)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "runtime_logs", err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(out))
		return
	}
	// Build phase: stream stored events for the most recent deployment (or specified id).
	depIDStr := chi.URLParam(r, "id")
	depID, err := uuid.Parse(depIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_id", err.Error())
		return
	}
	if _, err := s.store.GetDeployment(ctx, depID); err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not_found", "deployment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	events, err := s.store.ListEvents(ctx, depID, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	for _, e := range events {
		fmt.Fprintf(w, "[%s] %s: %s\n", e.Phase, e.Level, e.Message)
	}
}
