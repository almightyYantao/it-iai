package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/iai/vibedeploy/internal/k8sdriver"
)

// Resource quota: platform defaults live in system_config (quota.*), a
// project may override any field. Admin only.

var quotaKeys = [4]string{"quota.cpu_request", "quota.mem_request", "quota.cpu_limit", "quota.mem_limit"}

func (s *Server) platformQuota(ctx context.Context) k8sdriver.Quota {
	cfg, _ := s.store.GetSystemConfig(ctx)
	return k8sdriver.Quota{CPURequest: cfg[quotaKeys[0]], MemRequest: cfg[quotaKeys[1]], CPULimit: cfg[quotaKeys[2]], MemLimit: cfg[quotaKeys[3]]}
}

func (s *Server) projectQuotaOverride(ctx context.Context, id uuid.UUID) k8sdriver.Quota {
	var q k8sdriver.Quota
	var a, b, c, d *string
	_ = s.store.Pool.QueryRow(ctx, `SELECT cpu_request, mem_request, cpu_limit, mem_limit FROM projects WHERE id = $1`, id).Scan(&a, &b, &c, &d)
	for _, x := range []struct {
		src *string
		dst *string
	}{{a, &q.CPURequest}, {b, &q.MemRequest}, {c, &q.CPULimit}, {d, &q.MemLimit}} {
		if x.src != nil {
			*x.dst = *x.src
		}
	}
	return q
}

// quotaFor: project override > platform default > (manifest / built-in, in the deployer).
func (s *Server) quotaFor(ctx context.Context, id uuid.UUID) k8sdriver.Quota {
	return s.projectQuotaOverride(ctx, id).Merge(s.platformQuota(ctx))
}

func validateQuota(q k8sdriver.Quota) error {
	parse := func(v string) (*resource.Quantity, error) {
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		x, err := resource.ParseQuantity(strings.TrimSpace(v))
		if err != nil {
			return nil, errors.New("invalid quantity " + v + " (e.g. 100m, 0.5, 256Mi, 2Gi)")
		}
		return &x, nil
	}
	cr, err := parse(q.CPURequest)
	if err != nil {
		return err
	}
	mr, err := parse(q.MemRequest)
	if err != nil {
		return err
	}
	cl, err := parse(q.CPULimit)
	if err != nil {
		return err
	}
	ml, err := parse(q.MemLimit)
	if err != nil {
		return err
	}
	if cr != nil && cl != nil && cr.Cmp(*cl) > 0 {
		return errors.New("CPU request exceeds the CPU limit")
	}
	if mr != nil && ml != nil && mr.Cmp(*ml) > 0 {
		return errors.New("memory request exceeds the memory limit")
	}
	return nil
}

func trimQuota(q k8sdriver.Quota) k8sdriver.Quota {
	return k8sdriver.Quota{CPURequest: strings.TrimSpace(q.CPURequest), MemRequest: strings.TrimSpace(q.MemRequest),
		CPULimit: strings.TrimSpace(q.CPULimit), MemLimit: strings.TrimSpace(q.MemLimit)}
}

// GET /v1/admin/quota-defaults
func (s *Server) handleGetQuotaDefaults(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"defaults": s.platformQuota(r.Context()), "builtin": k8sdriver.DefaultQuota})
}

// PUT /v1/admin/quota-defaults  {cpu_request, mem_request, cpu_limit, mem_limit}; "" = built-in.
// Takes effect for projects without an override on their next deploy or restart.
func (s *Server) handlePutQuotaDefaults(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "admin scope required")
		return
	}
	var q k8sdriver.Quota
	if err := decodeJSON(r, &q); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	q = trimQuota(q)
	if err := validateQuota(q.Merge(k8sdriver.DefaultQuota)); err != nil {
		writeError(w, http.StatusBadRequest, "bad_quota", err.Error())
		return
	}
	kv := map[string]string{quotaKeys[0]: q.CPURequest, quotaKeys[1]: q.MemRequest, quotaKeys[2]: q.CPULimit, quotaKeys[3]: q.MemLimit}
	if err := s.store.BulkSetSystemConfig(r.Context(), kv, nil); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	s.store.WriteAudit(r.Context(), string(actor.Kind), actor.identityString(), "quota.defaults.update", nil, q)
	writeJSON(w, http.StatusOK, map[string]any{"defaults": q})
}

// GET /v1/projects/:slug/quota
func (s *Server) handleGetProjectQuota(w http.ResponseWriter, r *http.Request) {
	p := projectFrom(r.Context())
	actor, _ := ActorFrom(r.Context())
	ctx := r.Context()
	writeJSON(w, http.StatusOK, map[string]any{
		"override":  s.projectQuotaOverride(ctx, p.ID),
		"effective": s.quotaFor(ctx, p.ID).Merge(k8sdriver.DefaultQuota),
		"can_edit":  isAdminActor(actor),
	})
}

// PUT /v1/projects/:slug/quota — admin only; "" = inherit. Rolls the pod.
func (s *Server) handlePutProjectQuota(w http.ResponseWriter, r *http.Request) {
	actor, _ := ActorFrom(r.Context())
	if !isAdminActor(actor) {
		writeError(w, http.StatusForbidden, "not_admin", "only platform admins can change a project's quota")
		return
	}
	p := projectFrom(r.Context())
	ctx := r.Context()
	var q k8sdriver.Quota
	if err := decodeJSON(r, &q); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	q = trimQuota(q)
	eff := q.Merge(s.platformQuota(ctx)).Merge(k8sdriver.DefaultQuota)
	if err := validateQuota(eff); err != nil {
		writeError(w, http.StatusBadRequest, "bad_quota", err.Error())
		return
	}
	nz := func(v string) any {
		if v == "" {
			return nil
		}
		return v
	}
	if _, err := s.store.Pool.Exec(ctx, `UPDATE projects SET cpu_request=$2, mem_request=$3, cpu_limit=$4, mem_limit=$5 WHERE id=$1`,
		p.ID, nz(q.CPURequest), nz(q.MemRequest), nz(q.CPULimit), nz(q.MemLimit)); err != nil {
		writeError(w, http.StatusInternalServerError, "db", err.Error())
		return
	}
	if s.deployer != nil {
		_, _, proxyURL, _ := s.egressConfigForProject(ctx, p.ID)
		if err := s.deployer.SetProxyEnv(ctx, p.Slug, proxyURL, s.quotaFor(ctx, p.ID)); err != nil {
			writeError(w, http.StatusInternalServerError, "apply", err.Error())
			return
		}
	}
	s.store.WriteAudit(ctx, string(actor.Kind), actor.identityString(), "project.quota.update", &p.ID, q)
	writeJSON(w, http.StatusOK, map[string]any{"override": q, "effective": eff})
}
