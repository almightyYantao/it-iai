package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Port window for per-project egress-proxy listeners. Above the ephemeral-port
// floor most kernels use for outbound connections, and clear of everything the
// platform publishes on the host, so a listener can't collide with either.
const (
	EgressProxyPortMin = 13128
	EgressProxyPortMax = 13999
)

// ErrNoEgressProxyPort means the window is exhausted — every port in the range
// belongs to a live project.
var ErrNoEgressProxyPort = errors.New("no free egress proxy port in range")

// EgressProxyTarget is one project's proxy listener plus the domains it may
// reach through it. Everything the proxy config renderer needs.
type EgressProxyTarget struct {
	Slug    string
	Port    int
	Mode    string // observe | enforce (off projects with a port render as enforce: deny)
	Domains []DomainRule
}

// DomainRule is a hostname the project may reach and the port it may reach it
// on (80 or 443 — the proxy speaks HTTP).
type DomainRule struct {
	Value string
	Port  int
}

// EnsureEgressProxyPort returns the project's listener port, allocating the
// lowest free one on first call.
//
// Allocation is a single statement so two concurrent deploys can't both take the
// same port: the partial unique index rejects the loser, and the caller retries.
func (s *Store) EnsureEgressProxyPort(ctx context.Context, projectID uuid.UUID) (int, error) {
	var existing *int
	if err := s.Pool.QueryRow(ctx,
		`SELECT egress_proxy_port FROM projects WHERE id = $1 AND deleted_at IS NULL`,
		projectID).Scan(&existing); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if existing != nil {
		return *existing, nil
	}

	// generate_series minus what's taken; lowest wins so the range stays dense
	// and the config file diffs stay small.
	var port int
	err := s.Pool.QueryRow(ctx, `
		UPDATE projects SET egress_proxy_port = (
		    SELECT p FROM generate_series($2::int, $3::int) AS p
		     WHERE p NOT IN (
		         SELECT egress_proxy_port FROM projects
		          WHERE egress_proxy_port IS NOT NULL AND deleted_at IS NULL
		     )
		     ORDER BY p
		     LIMIT 1
		)
		 WHERE id = $1 AND deleted_at IS NULL
		RETURNING egress_proxy_port`,
		projectID, EgressProxyPortMin, EgressProxyPortMax).Scan(&existing)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		return 0, ErrNoEgressProxyPort
	}
	port = *existing
	return port, nil
}

// ListEgressProxyTargets returns every live project that has a listener port,
// with its domain rules attached. Ordered by slug so the rendered config is
// byte-stable — the renderer's output is hashed to decide whether the proxy
// needs a reload, and a shuffled list would trigger pointless restarts.
func (s *Store) ListEgressProxyTargets(ctx context.Context) ([]EgressProxyTarget, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.slug, p.egress_proxy_port, p.egress_mode,
		       coalesce(array_agg(r.value ORDER BY r.value, r.port)
		                FILTER (WHERE r.id IS NOT NULL), '{}') AS domains,
		       coalesce(array_agg(r.port  ORDER BY r.value, r.port)
		                FILTER (WHERE r.id IS NOT NULL), '{}') AS ports
		  FROM projects p
		  LEFT JOIN project_egress_rules r
		         ON r.project_id = p.id AND r.kind = 'domain'
		 WHERE p.deleted_at IS NULL AND p.egress_proxy_port IS NOT NULL
		 GROUP BY p.slug, p.egress_proxy_port, p.egress_mode
		 ORDER BY p.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EgressProxyTarget
	for rows.Next() {
		var t EgressProxyTarget
		var values []string
		var ports []int32
		if err := rows.Scan(&t.Slug, &t.Port, &t.Mode, &values, &ports); err != nil {
			return nil, err
		}
		for i := range values {
			p := 443
			if i < len(ports) {
				p = int(ports[i])
			}
			t.Domains = append(t.Domains, DomainRule{Value: values[i], Port: p})
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetProjectEgressMode returns the project's egress mode and proxy port (0 when
// none is allocated).
func (s *Store) GetProjectEgressMode(ctx context.Context, projectID uuid.UUID) (string, int, error) {
	var mode string
	var port *int
	err := s.Pool.QueryRow(ctx,
		`SELECT egress_mode, egress_proxy_port FROM projects WHERE id = $1 AND deleted_at IS NULL`,
		projectID).Scan(&mode, &port)
	if err != nil {
		return "", 0, err
	}
	if port == nil {
		return mode, 0, nil
	}
	return mode, *port, nil
}

func (s *Store) SetProjectEgressMode(ctx context.Context, projectID uuid.UUID, mode string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE projects SET egress_mode = $2 WHERE id = $1`, projectID, mode)
	return err
}

// EgressModeCounts maps mode → number of live projects.
func (s *Store) EgressModeCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT egress_mode, count(*) FROM projects WHERE deleted_at IS NULL GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var m string
		var n int
		if err := rows.Scan(&m, &n); err != nil {
			return nil, err
		}
		out[m] = n
	}
	return out, rows.Err()
}

// ProxyPortSlugs maps a proxy listener port to its project slug.
func (s *Store) ProxyPortSlugs(ctx context.Context) (map[int]string, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT egress_proxy_port, slug FROM projects WHERE deleted_at IS NULL AND egress_proxy_port IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var p int
		var slug string
		if err := rows.Scan(&p, &slug); err != nil {
			return nil, err
		}
		out[p] = slug
	}
	return out, rows.Err()
}
