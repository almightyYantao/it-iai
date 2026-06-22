package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iai/vibedeploy/internal/model"
)

var ErrNotFound = errors.New("not found")

func (s *Store) UpsertUserByEmail(ctx context.Context, email, name string) (*model.User, error) {
	const q = `
		INSERT INTO users (email, name, last_seen_at)
		VALUES ($1, $2, now())
		ON CONFLICT (email) DO UPDATE SET last_seen_at = now(), name = COALESCE(NULLIF(EXCLUDED.name, ''), users.name)
		RETURNING id, email, COALESCE(name,''), is_admin, created_at, last_seen_at
	`
	var u model.User
	err := s.Pool.QueryRow(ctx, q, email, name).Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.CreatedAt, &u.LastSeenAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Project SELECT columns kept as a single string so every read site stays in
// sync — adding columns without this forces touching every Scan site.
const projectCols = `id, slug, name, owner_id, visibility, status, manifest, allow_cidrs, access_preset, tls_enabled, created_at, last_pushed_at, last_active_at`

func scanProject(row pgx.Row, p *model.Project) error {
	return row.Scan(
		&p.ID, &p.Slug, &p.Name, &p.OwnerID, &p.Visibility, &p.Status,
		&p.Manifest, &p.AllowCIDRs, &p.AccessPreset, &p.TLSEnabled,
		&p.CreatedAt, &p.LastPushedAt, &p.LastActiveAt,
	)
}

func (s *Store) CreateProject(ctx context.Context, slug, name string, ownerID uuid.UUID, manifest json.RawMessage) (*model.Project, error) {
	q := `INSERT INTO projects (slug, name, owner_id, manifest)
	      VALUES ($1, $2, $3, $4)
	      RETURNING ` + projectCols
	var p model.Project
	if err := scanProject(s.Pool.QueryRow(ctx, q, slug, name, ownerID, manifest), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) GetProjectBySlug(ctx context.Context, slug string) (*model.Project, error) {
	q := `SELECT ` + projectCols + ` FROM projects WHERE slug = $1 AND deleted_at IS NULL`
	var p model.Project
	err := scanProject(s.Pool.QueryRow(ctx, q, slug), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// GetProjectByID is the same lookup keyed on the primary id — used by the
// reconciler, which knows project IDs but not slugs in the hot path.
func (s *Store) GetProjectByID(ctx context.Context, id uuid.UUID) (*model.Project, error) {
	q := `SELECT ` + projectCols + ` FROM projects WHERE id = $1 AND deleted_at IS NULL`
	var p model.Project
	err := scanProject(s.Pool.QueryRow(ctx, q, id), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// SetProjectAllowCIDRs replaces the IP allow-list for a project. Pass an empty
// or nil slice to clear it (= open access). Caller normalises / validates the
// CIDR strings; we just persist verbatim.
func (s *Store) SetProjectAllowCIDRs(ctx context.Context, id uuid.UUID, cidrs []string) error {
	if cidrs == nil {
		cidrs = []string{}
	}
	_, err := s.Pool.Exec(ctx,
		`UPDATE projects SET allow_cidrs = $2 WHERE id = $1`, id, cidrs)
	return err
}

// SetProjectName renames a project. The slug stays fixed (URL stability); this
// only updates the human-readable display name shown in the UI / Skill output.
// Empty strings are rejected at the API layer — the column is NOT NULL and the
// app keeps a non-empty invariant.
func (s *Store) SetProjectName(ctx context.Context, id uuid.UUID, name string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE projects SET name = $2 WHERE id = $1`, id, name)
	return err
}

// SetProjectTLSEnabled flips the per-project HTTPS toggle. The reconciler
// reads this on the next sync and rewrites the Ingress with (or without) a
// cert-manager annotation + TLS section accordingly.
func (s *Store) SetProjectTLSEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE projects SET tls_enabled = $2 WHERE id = $1`, id, enabled)
	return err
}

// SetProjectAccessPreset points a project at a named preset. Pass an empty
// string / nil to clear it (project falls back to its own allow_cidrs).
// The FK with ON DELETE SET NULL means callers don't need to scrub references
// when a preset is deleted — DB does it.
func (s *Store) SetProjectAccessPreset(ctx context.Context, id uuid.UUID, preset *string) error {
	var v any
	if preset != nil && *preset != "" {
		v = *preset
	} else {
		v = nil
	}
	_, err := s.Pool.Exec(ctx,
		`UPDATE projects SET access_preset = $2 WHERE id = $1`, id, v)
	return err
}

// ListProjectsForUser returns projects the user OWNS or COLLABORATES on — and
// nothing else. Org-visibility / public projects are NOT here; visibility
// controls who can hit the deployed-app URL, not who sees admin entries.
//
// Admins who want every project across the cluster use ListAllProjects via the
// /v1/admin/projects endpoint.
func (s *Store) ListProjectsForUser(ctx context.Context, userID uuid.UUID) ([]*model.Project, error) {
	q := `SELECT DISTINCT ` + projectCols + `
	      FROM projects p
	      LEFT JOIN project_collaborators c ON c.project_id = p.id AND c.user_id = $1
	      WHERE p.deleted_at IS NULL
	        AND (p.owner_id = $1 OR c.user_id IS NOT NULL)
	      ORDER BY created_at DESC`
	rows, err := s.Pool.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Project
	for rows.Next() {
		var p model.Project
		if err := scanProject(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// AddCollaborator grants a user editor access to a project (owner+collab list).
func (s *Store) AddCollaborator(ctx context.Context, projectID, userID, addedBy uuid.UUID, role string) error {
	if role == "" {
		role = "editor"
	}
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO project_collaborators (project_id, user_id, role, added_by)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (project_id, user_id) DO UPDATE SET role = EXCLUDED.role`,
		projectID, userID, role, addedBy,
	)
	return err
}

// RemoveCollaborator revokes access.
func (s *Store) RemoveCollaborator(ctx context.Context, projectID, userID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM project_collaborators WHERE project_id = $1 AND user_id = $2`,
		projectID, userID,
	)
	return err
}

// IsCollaborator returns true when the user is a collaborator on the project.
func (s *Store) IsCollaborator(ctx context.Context, projectID, userID uuid.UUID) (bool, error) {
	var n int
	err := s.Pool.QueryRow(ctx,
		`SELECT count(1) FROM project_collaborators WHERE project_id = $1 AND user_id = $2`,
		projectID, userID,
	).Scan(&n)
	return n > 0, err
}

func (s *Store) UpdateProjectStatus(ctx context.Context, id uuid.UUID, status model.ProjectStatus) error {
	const q = `UPDATE projects SET status = $2, last_active_at = now() WHERE id = $1`
	_, err := s.Pool.Exec(ctx, q, id, status)
	return err
}

// SetProjectStatusOnly updates the status column WITHOUT bumping last_active_at.
// Used by the periodic health-reconcile loop — flipping a project to 'stopped'
// or 'error' shouldn't lie and say it was active just now.
func (s *Store) SetProjectStatusOnly(ctx context.Context, id uuid.UUID, status model.ProjectStatus) error {
	const q = `UPDATE projects SET status = $2 WHERE id = $1`
	_, err := s.Pool.Exec(ctx, q, id, status)
	return err
}

// ProjectHealthRow is the minimal info the periodic reconciler needs to decide
// whether a project's DB status still matches reality.
type ProjectHealthRow struct {
	ID     uuid.UUID
	Slug   string
	Status string
}

// ListProjectsForIngressSweep returns the full project rows the startup ingress
// sweep needs to re-apply k8s ingress + middleware after any DB → cluster drift
// (typically: a migration flipped tls_enabled, but the projects' ingresses
// still have the old cert-manager annotation state).
//
// Skips 'created' (no deployment + no ingress yet) and 'deleting' (transient
// teardown). Matches ListProjectsForHealthReconcile's filter so the two stay
// consistent.
func (s *Store) ListProjectsForIngressSweep(ctx context.Context) ([]*model.Project, error) {
	q := `SELECT ` + projectCols + `
	      FROM projects
	      WHERE deleted_at IS NULL AND status IN ('running','stopped','error')`
	rows, err := s.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Project
	for rows.Next() {
		var p model.Project
		if err := scanProject(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// ListProjectsForHealthReconcile returns projects we should periodically probe.
// Skips 'created' (never deployed) and 'deleting' (transient) — those statuses
// are owned by other code paths.
func (s *Store) ListProjectsForHealthReconcile(ctx context.Context) ([]ProjectHealthRow, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, slug, status FROM projects
		 WHERE deleted_at IS NULL AND status IN ('running','stopped','error')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectHealthRow
	for rows.Next() {
		var p ProjectHealthRow
		if err := rows.Scan(&p.ID, &p.Slug, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) TouchProjectPushed(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE projects SET last_pushed_at = now() WHERE id = $1`
	_, err := s.Pool.Exec(ctx, q, id)
	return err
}

// SoftDeleteProject flips deleted_at — keeps the row + audit history but
// removes the project from every list query. The cluster-side cleanup
// (namespace, deployments, ingresses) is done by the caller via the deployer.
func (s *Store) SoftDeleteProject(ctx context.Context, id uuid.UUID) error {
	tag, err := s.Pool.Exec(ctx,
		`UPDATE projects SET deleted_at = now(), status = 'deleting'
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SlugAvailable returns true when no live project has the slug.
func (s *Store) SlugAvailable(ctx context.Context, slug string) (bool, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(1) FROM projects WHERE slug=$1 AND deleted_at IS NULL`, slug).Scan(&n)
	return n == 0, err
}

// Used by audit_logs writer.
func (s *Store) WriteAudit(ctx context.Context, actorType, actorID, action string, projectID *uuid.UUID, meta any) {
	b, _ := json.Marshal(meta)
	_, _ = s.Pool.Exec(ctx,
		`INSERT INTO audit_logs (actor_type, actor_id, action, project_id, metadata, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		actorType, actorID, action, projectID, b, time.Now(),
	)
}
