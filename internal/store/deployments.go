package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iai/vibedeploy/internal/model"
)

// deploymentColumns is the single source of truth for the deployments column list
// used by every SELECT and every RETURNING clause that returns a deployment row.
//
// Nullable text columns (source_blob_key, image_tag, failure_reason) are wrapped
// in COALESCE here so the scan target can stay a plain string. Add a new column?
// Add it here ONCE — every caller picks it up. Add a new query? Use this constant
// and scanDeployment, and you can't forget the NULL handling.
const deploymentColumns = `
	id, project_id, trigger_type, triggered_by, status,
	COALESCE(source_blob_key, '') AS source_blob_key,
	COALESCE(image_tag,       '') AS image_tag,
	manifest,
	COALESCE(failure_reason,  '') AS failure_reason,
	created_at, queued_at, build_started_at, build_done_at, deployed_at
`

// rowScanner is implemented by both pgx.Row (QueryRow) and pgx.Rows (Query).
type rowScanner interface {
	Scan(dest ...any) error
}

// scanDeployment scans one row of deploymentColumns into a *model.Deployment.
// Caller is responsible for translating pgx.ErrNoRows into ErrNotFound where
// appropriate (some call sites — e.g. ClaimNextQueued — treat no-rows as a
// non-error idle case instead of a 404).
func scanDeployment(r rowScanner) (*model.Deployment, error) {
	var d model.Deployment
	err := r.Scan(
		&d.ID, &d.ProjectID, &d.TriggerType, &d.TriggeredBy, &d.Status,
		&d.SourceBlobKey, &d.ImageTag,
		&d.Manifest,
		&d.FailureReason,
		&d.CreatedAt, &d.QueuedAt, &d.BuildStartedAt, &d.BuildDoneAt, &d.DeployedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// scanDeployments drains pgx.Rows into a slice. Closes the rows.
func scanDeployments(rows pgx.Rows) ([]*model.Deployment, error) {
	defer rows.Close()
	var out []*model.Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) CreateDeployment(ctx context.Context, projectID uuid.UUID, triggerType, triggeredBy string, manifest json.RawMessage) (*model.Deployment, error) {
	q := `
		INSERT INTO deployments (project_id, trigger_type, triggered_by, status, manifest)
		VALUES ($1, $2, $3, 'pending', $4)
		RETURNING ` + deploymentColumns
	return scanDeployment(s.Pool.QueryRow(ctx, q, projectID, triggerType, triggeredBy, manifest))
}

func (s *Store) GetDeployment(ctx context.Context, id uuid.UUID) (*model.Deployment, error) {
	q := `SELECT ` + deploymentColumns + ` FROM deployments WHERE id = $1`
	d, err := scanDeployment(s.Pool.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

// ListDeployments returns a page of deployments for the project, newest first.
// Pass offset=0 for the first page; total row count is the caller's job to
// query (see handleListDeployments).
func (s *Store) ListDeployments(ctx context.Context, projectID uuid.UUID, limit, offset int) ([]*model.Deployment, error) {
	q := `SELECT ` + deploymentColumns + ` FROM deployments WHERE project_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	rows, err := s.Pool.Query(ctx, q, projectID, limit, offset)
	if err != nil {
		return nil, err
	}
	return scanDeployments(rows)
}

func (s *Store) MarkDeploymentQueued(ctx context.Context, id uuid.UUID, sourceKey string) error {
	const q = `UPDATE deployments SET status='queued', source_blob_key=$2, queued_at=now() WHERE id=$1`
	_, err := s.Pool.Exec(ctx, q, id, sourceKey)
	return err
}

// ClaimNextQueued atomically claims one queued deployment for the worker.
// Returns (nil, nil) when there's nothing to claim.
//
// Implementation note: rewritten from CTE-with-UPDATE-FROM to UPDATE-WHERE-subquery
// so the RETURNING clause sees only the deployments table (no need to prefix every
// column with d.) and can therefore reuse the shared deploymentColumns constant.
func (s *Store) ClaimNextQueued(ctx context.Context) (*model.Deployment, error) {
	q := `
		UPDATE deployments
		SET status = 'building', build_started_at = now()
		WHERE id = (
			SELECT id FROM deployments
			WHERE status = 'queued'
			ORDER BY queued_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING ` + deploymentColumns
	d, err := scanDeployment(s.Pool.QueryRow(ctx, q))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func (s *Store) MarkDeploymentStatus(ctx context.Context, id uuid.UUID, status model.DeploymentStatus, failureReason string) error {
	const q = `
		UPDATE deployments
		SET status = $2,
		    failure_reason = NULLIF($3,''),
		    build_done_at = CASE WHEN $2 IN ('pushing','deploying','running','failed') AND build_done_at IS NULL THEN now() ELSE build_done_at END,
		    deployed_at   = CASE WHEN $2 = 'running' AND deployed_at IS NULL THEN now() ELSE deployed_at END
		WHERE id = $1
	`
	_, err := s.Pool.Exec(ctx, q, id, status, failureReason)
	return err
}

func (s *Store) SetDeploymentImage(ctx context.Context, id uuid.UUID, image string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE deployments SET image_tag=$2 WHERE id=$1`, id, image)
	return err
}

// SupersedeOlderDeployments marks any in-flight deployment for the same project (other than `keep`) as superseded.
func (s *Store) SupersedeOlderDeployments(ctx context.Context, projectID, keep uuid.UUID) error {
	const q = `
		UPDATE deployments SET status='superseded'
		WHERE project_id = $1 AND id <> $2
		  AND status IN ('queued','building','pushing','deploying')
	`
	_, err := s.Pool.Exec(ctx, q, projectID, keep)
	return err
}

// SupersedeRunningSiblings marks any other running deployment of the same project
// as superseded. Called at the moment a fresh deployment reaches running, so the
// previous live record steps off-stage in the UI. Not done at upload time because
// the previous deployment should keep serving traffic until the new one is ready.
func (s *Store) SupersedeRunningSiblings(ctx context.Context, projectID, keep uuid.UUID) error {
	const q = `
		UPDATE deployments SET status='superseded'
		WHERE project_id = $1 AND id <> $2 AND status='running'
	`
	_, err := s.Pool.Exec(ctx, q, projectID, keep)
	return err
}

// AppendEvent stores an event and returns its id for SSE replay.
func (s *Store) AppendEvent(ctx context.Context, deploymentID uuid.UUID, phase, level, message string) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx,
		`INSERT INTO deployment_events (deployment_id, phase, level, message) VALUES ($1,$2,$3,$4) RETURNING id`,
		deploymentID, phase, level, message,
	).Scan(&id)
	return id, err
}

func (s *Store) ListEvents(ctx context.Context, deploymentID uuid.UUID, afterID int64) ([]*model.DeploymentEvent, error) {
	const q = `
		SELECT id, deployment_id, phase, level, message, created_at
		FROM deployment_events WHERE deployment_id = $1 AND id > $2 ORDER BY id ASC
	`
	rows, err := s.Pool.Query(ctx, q, deploymentID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.DeploymentEvent
	for rows.Next() {
		var e model.DeploymentEvent
		if err := rows.Scan(&e.ID, &e.DeploymentID, &e.Phase, &e.Level, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
