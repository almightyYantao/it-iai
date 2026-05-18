package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/model"
	"github.com/iai/vibedeploy/internal/store"
)

// handleDeploymentEvents streams deployment_events rows over SSE.
// Reconnect: client can send "Last-Event-ID" header (or ?after=<id>) to resume.
func (s *Server) handleDeploymentEvents(w http.ResponseWriter, r *http.Request) {
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
		log.Printf("handleDeploymentEvents: GetDeployment(%s) failed: %v", id, err)
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if dep.ProjectID != p.ID {
		writeError(w, http.StatusNotFound, "not_found", "deployment not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "no_flusher", "streaming not supported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	var afterID int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		afterID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			afterID = n
		}
	}

	wake, cancel := s.bus.Subscribe(dep.ID)
	defer cancel()

	// Initial flush: drain anything we already have.
	drained, lastID, terminal, err := s.streamEvents(ctx, w, flusher, dep.ID, afterID)
	if err != nil {
		return
	}
	afterID = lastID
	_ = drained
	if terminal {
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			_, lastID, terminal, err := s.streamEvents(ctx, w, flusher, dep.ID, afterID)
			if err != nil {
				return
			}
			afterID = lastID
			if terminal {
				return
			}
		case <-heartbeat.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
			// Polling fallback in case a publish came from another process.
			_, lastID, terminal, err := s.streamEvents(ctx, w, flusher, dep.ID, afterID)
			if err != nil {
				return
			}
			afterID = lastID
			if terminal {
				return
			}
		}
	}
}

// streamEvents writes any new events to the SSE stream and returns the latest id seen
// plus a bool indicating the deployment reached a terminal state.
func (s *Server) streamEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, depID uuid.UUID, afterID int64) (count int, lastID int64, terminal bool, err error) {
	events, err := s.store.ListEvents(ctx, depID, afterID)
	if err != nil {
		return 0, afterID, false, err
	}
	for _, e := range events {
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Phase, b)
		if e.ID > lastID {
			lastID = e.ID
		}
	}
	if lastID < afterID {
		lastID = afterID
	}
	flusher.Flush()

	// Check terminal state on every flush — cheap.
	dep, err := s.store.GetDeployment(ctx, depID)
	if err != nil {
		return len(events), lastID, false, err
	}
	switch dep.Status {
	case model.DeployRunning, model.DeployFailed, model.DeploySuperseded:
		// One final "status" event so the client knows we're done.
		fmt.Fprintf(w, "event: end\ndata: {\"status\":%q}\n\n", string(dep.Status))
		flusher.Flush()
		return len(events), lastID, true, nil
	}
	return len(events), lastID, false, nil
}
