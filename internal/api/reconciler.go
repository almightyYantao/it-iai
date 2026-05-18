package api

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/k8sdriver"
	"github.com/iai/vibedeploy/internal/model"
)

// Reconciler polls the deployments table for rows in status=pushing or =deploying and
// drives them onto the cluster. Build Service flips a deployment to "pushing" once the
// image is in the registry; this loop picks it up and applies the K8s manifests.
type Reconciler struct {
	srv      *Server
	interval time.Duration

	// lastSummary remembers the last pod-state line we wrote for a given
	// deployment, so we only emit on change. Without this the deploy log
	// would be flooded ("container running, waiting for readiness probe"
	// every 2s for 30s of startup).
	mu          sync.Mutex
	lastSummary map[uuid.UUID]string
}

func NewReconciler(s *Server) *Reconciler {
	return &Reconciler{srv: s, interval: 2 * time.Second, lastSummary: map[uuid.UUID]string{}}
}

func (r *Reconciler) Run(ctx context.Context) {
	// Two independent loops:
	//   - tick (2s) drives in-flight deployments forward (pushing → deploying → running)
	//   - healthTick (30s) re-checks live projects against the cluster so the
	//     DB status doesn't drift (e.g. pod evicted, node drained, manual
	//     kubectl scale --replicas=0)
	go r.runHealthLoop(ctx)

	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.tick(ctx)
		}
	}
}

func (r *Reconciler) runHealthLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	// One immediate pass on startup so we don't sit on stale 'running' rows
	// for the first 30s after a control-plane restart.
	r.reconcileProjectHealth(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.reconcileProjectHealth(ctx)
		}
	}
}

// reconcileProjectHealth probes each live project's pod readiness and
// flips the DB status when it drifts. Read-only against the cluster — only
// writes to the DB, and only when the status actually changes.
//
// Rules:
//   pod Ready  → status='running'  (bumps last_active_at via UpdateProjectStatus)
//   not Ready  → status='stopped'  (no timestamp bump — we observed it down)
//   probe err  → no-op             (likely "deployment not found" because the
//                                   project was never successfully deployed;
//                                   leave whatever 'created' or 'error' state)
func (r *Reconciler) reconcileProjectHealth(ctx context.Context) {
	rows, err := r.srv.store.ListProjectsForHealthReconcile(ctx)
	if err != nil {
		log.Printf("project-health: list: %v", err)
		return
	}
	for _, p := range rows {
		ready, err := r.srv.deployer.IsDeploymentReady(ctx, p.Slug)
		if err != nil {
			// Most likely apierrors.IsNotFound — namespace or Deployment gone.
			// We could special-case that to flip to 'error', but leaving it be
			// is safer (lots of transient k8s errors we don't want to react to).
			continue
		}
		var desired string
		if ready {
			desired = string(model.ProjectRunning)
		} else {
			desired = string(model.ProjectStopped)
		}
		if desired == p.Status {
			continue
		}
		if desired == string(model.ProjectRunning) {
			// Flip up — also bump last_active_at because we just confirmed alive.
			_ = r.srv.store.UpdateProjectStatus(ctx, p.ID, model.ProjectStatus(desired))
		} else {
			// Flip down — don't lie about being active.
			_ = r.srv.store.SetProjectStatusOnly(ctx, p.ID, model.ProjectStatus(desired))
		}
		log.Printf("project-health: %s status %s → %s (pod ready=%v)", p.Slug, p.Status, desired, ready)
	}
}

type pendingDeploy struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	Status    string
	Image     string
	Manifest  []byte
}

func (r *Reconciler) tick(ctx context.Context) {
	const q = `
		SELECT id, project_id, status, COALESCE(image_tag,''), manifest
		FROM deployments
		WHERE status IN ('pushing','deploying')
		ORDER BY queued_at ASC
		LIMIT 8
	`
	rows, err := r.srv.store.Pool.Query(ctx, q)
	if err != nil {
		log.Printf("reconciler query: %v", err)
		return
	}
	var todo []pendingDeploy
	for rows.Next() {
		var p pendingDeploy
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Status, &p.Image, &p.Manifest); err != nil {
			log.Printf("reconciler scan: %v", err)
			continue
		}
		todo = append(todo, p)
	}
	rows.Close()
	for _, p := range todo {
		r.handle(ctx, p)
	}
}

func (r *Reconciler) handle(ctx context.Context, p pendingDeploy) {
	switch p.Status {
	case "pushing":
		var m model.Manifest
		if err := json.Unmarshal(p.Manifest, &m); err != nil {
			r.fail(ctx, p.ID, "manifest_parse", err.Error())
			return
		}
		port := m.Port
		if port == 0 {
			port = 3000
		}
		slug, err := r.slugFor(ctx, p.ProjectID)
		if err != nil {
			r.fail(ctx, p.ID, "lookup_project", err.Error())
			return
		}
		// Env layering: manifest.env (from user's .vibedeploy.toml, in git)
		// goes in first, then project_env (encrypted in DB, edited from the
		// Web Settings panel or auto-written by the platform's provisioning
		// code) overrides. The "secrets win" direction matters — it lets
		// admins overwrite a placeholder DATABASE_URL in the user's manifest
		// with the real one without making the user re-push.
		envs := map[string]string{}
		for k, v := range m.Env {
			envs[k] = v
		}
		dbEnv, eerr := r.srv.store.GetProjectEnv(ctx, p.ProjectID, r.srv.kek)
		if eerr != nil {
			// Don't silently drop env on a decrypt failure — fail the deploy
			// so the operator sees the problem instead of a half-broken pod.
			r.fail(ctx, p.ID, "env_decrypt", eerr.Error())
			return
		}

		// Auto-provision sidecars when manifest says so and a value isn't
		// already cached in project_env. First-deploy creates the DB / role;
		// subsequent deploys reuse the stored URL. Provisioning is idempotent
		// so a re-trigger (e.g. operator wiped project_env) just rotates the
		// password and reuses the database.
		if m.Needs.Postgres && r.srv.pgProv.Enabled() {
			if _, have := dbEnv["DATABASE_URL"]; !have {
				_, _ = r.srv.store.AppendEvent(ctx, p.ID, "deploy", "info", "provisioning Postgres database")
				url, perr := r.srv.pgProv.Provision(ctx, slug)
				if perr != nil {
					r.fail(ctx, p.ID, "provision_pg", perr.Error())
					return
				}
				if err := r.srv.store.SetProjectEnv(ctx, p.ProjectID, "DATABASE_URL", url, true, r.srv.kek, nil); err != nil {
					r.fail(ctx, p.ID, "store_pg_url", err.Error())
					return
				}
				dbEnv["DATABASE_URL"] = url
			}
		}
		if m.Needs.Redis && r.srv.redisProv.Enabled() {
			if _, have := dbEnv["REDIS_URL"]; !have {
				url, perr := r.srv.redisProv.Provision(ctx, slug)
				if perr != nil {
					r.fail(ctx, p.ID, "provision_redis", perr.Error())
					return
				}
				if err := r.srv.store.SetProjectEnv(ctx, p.ProjectID, "REDIS_URL", url, true, r.srv.kek, nil); err != nil {
					r.fail(ctx, p.ID, "store_redis_url", err.Error())
					return
				}
				dbEnv["REDIS_URL"] = url
			}
		}

		for k, v := range dbEnv {
			envs[k] = v
		}

		_ = r.srv.store.MarkDeploymentStatus(ctx, p.ID, model.DeployDeploying, "")
		_, _ = r.srv.store.AppendEvent(ctx, p.ID, "deploy", "info", "applying Kubernetes manifests")
		r.srv.bus.Publish(p.ID)

		if err := r.srv.deployer.Apply(ctx, k8sdriver.ApplyInput{
			Slug:   slug,
			Image:  p.Image,
			Port:   port,
			Env:    envs,
			CPU:    m.Resources.CPU,
			Memory: m.Resources.Memory,
		}); err != nil {
			r.fail(ctx, p.ID, "k8s_apply", err.Error())
			return
		}
		// Always re-sync the ingress after a deploy so custom hostnames, the
		// per-project IP allow-list, AND the oauth2-proxy ForwardAuth chain
		// are reapplied. Apply() only writes the platform subdomain with no
		// middleware — without this, a redeploy would erase any custom
		// hostnames and drop both the IP whitelist AND the auth gate.
		//
		// Delegate to the shared helper so reconciler + access PATCH + domain
		// add/remove all derive RequireAuth from visibility the same way.
		if pr, perr := r.srv.store.GetProjectByID(ctx, p.ProjectID); perr == nil && pr != nil {
			if err := r.srv.syncIngressForProject(ctx, pr); err != nil {
				_, _ = r.srv.store.AppendEvent(ctx, p.ID, "deploy", "warn", "ingress sync failed: "+err.Error())
			}
		}
		_, _ = r.srv.store.AppendEvent(ctx, p.ID, "deploy", "info", "manifests applied; waiting for readiness")
		r.srv.bus.Publish(p.ID)

	case "deploying":
		slug, err := r.slugFor(ctx, p.ProjectID)
		if err != nil {
			return
		}

		// PodSummary gives us a short state line, a ready bool, and (when the pod
		// has reached a state that won't self-heal) a terminalReason. Each tick we:
		//   - Emit the summary IFF it changed since last tick (no log flooding).
		//   - On terminal reason, fail the deployment so the user sees a clear error.
		//   - On ready, transition to running.
		summary, ready, terminalReason, perr := r.srv.deployer.PodSummary(ctx, slug)
		if perr != nil {
			log.Printf("PodSummary(%s): %v", slug, perr)
			return
		}
		if summary != "" {
			r.mu.Lock()
			changed := r.lastSummary[p.ID] != summary
			if changed {
				r.lastSummary[p.ID] = summary
			}
			r.mu.Unlock()
			if changed {
				_, _ = r.srv.store.AppendEvent(ctx, p.ID, "deploy", "info", summary)
				r.srv.bus.Publish(p.ID)
			}
		}

		if terminalReason != "" {
			r.fail(ctx, p.ID, "pod_unhealthy", terminalReason)
			r.mu.Lock()
			delete(r.lastSummary, p.ID)
			r.mu.Unlock()
			return
		}

		if ready {
			_ = r.srv.store.MarkDeploymentStatus(ctx, p.ID, model.DeployRunning, "")
			_ = r.srv.store.UpdateProjectStatus(ctx, p.ProjectID, model.ProjectRunning)
			_ = r.srv.store.TouchProjectPushed(ctx, p.ProjectID)
			_, _ = r.srv.store.AppendEvent(ctx, p.ID, "deploy", "info", "deployment ready")
			r.srv.bus.Publish(p.ID)
			r.mu.Lock()
			delete(r.lastSummary, p.ID)
			r.mu.Unlock()
		}
	}
}

func (r *Reconciler) fail(ctx context.Context, depID uuid.UUID, code, msg string) {
	_ = r.srv.store.MarkDeploymentStatus(ctx, depID, model.DeployFailed, code+": "+msg)
	_, _ = r.srv.store.AppendEvent(ctx, depID, "deploy", "error", code+": "+msg)
	r.srv.bus.Publish(depID)
}

func (r *Reconciler) slugFor(ctx context.Context, projectID uuid.UUID) (string, error) {
	var slug string
	err := r.srv.store.Pool.QueryRow(ctx, `SELECT slug FROM projects WHERE id = $1`, projectID).Scan(&slug)
	return slug, err
}
