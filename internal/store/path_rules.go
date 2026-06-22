package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/iai/vibedeploy/internal/model"
)

// ErrPathRuleTaken is returned by CreateProjectPathRule when the project
// already has a rule for the same prefix (UNIQUE (project_id, path_prefix)).
// Lets the API layer translate to 409 without parsing pg error strings.
var ErrPathRuleTaken = errors.New("a path rule with this prefix already exists for this project")

const pathRuleCols = `id, project_id, path_prefix, mode, created_at, updated_at`

func scanPathRule(row pgx.Row, r *model.ProjectPathRule) error {
	return row.Scan(&r.ID, &r.ProjectID, &r.PathPrefix, &r.Mode, &r.CreatedAt, &r.UpdatedAt)
}

func isPathRuleUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ListProjectPathRules returns every path-prefix auth override for the given
// project, ordered by descending path length so the most specific match wins
// when the deployer walks the list to emit IngressRoute routes.
func (s *Store) ListProjectPathRules(ctx context.Context, projectID uuid.UUID) ([]*model.ProjectPathRule, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+pathRuleCols+`
		   FROM project_path_rules
		  WHERE project_id = $1
		  ORDER BY length(path_prefix) DESC, path_prefix ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ProjectPathRule
	for rows.Next() {
		var r model.ProjectPathRule
		if err := scanPathRule(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// CreateProjectPathRule inserts a new override. Returns ErrPathRuleTaken if a rule
// with the same prefix already exists for this project — the UNIQUE constraint
// keeps owners from accidentally double-defining a prefix with conflicting
// modes.
func (s *Store) CreateProjectPathRule(ctx context.Context, projectID uuid.UUID, pathPrefix, mode string) (*model.ProjectPathRule, error) {
	q := `INSERT INTO project_path_rules (project_id, path_prefix, mode)
	      VALUES ($1, $2, $3)
	      RETURNING ` + pathRuleCols
	var r model.ProjectPathRule
	err := scanPathRule(s.Pool.QueryRow(ctx, q, projectID, pathPrefix, mode), &r)
	if err != nil {
		if isPathRuleUniqueViolation(err) {
			return nil, ErrPathRuleTaken
		}
		return nil, err
	}
	return &r, nil
}

// UpdateProjectPathRule modifies an existing rule's mode (or prefix — rare,
// but supported for typo fixes). The (project_id, id) compound check makes
// owner-scoped tenant isolation explicit.
func (s *Store) UpdateProjectPathRule(ctx context.Context, projectID, ruleID uuid.UUID, pathPrefix, mode string) (*model.ProjectPathRule, error) {
	q := `UPDATE project_path_rules
	         SET path_prefix = $3, mode = $4, updated_at = now()
	       WHERE id = $1 AND project_id = $2
	   RETURNING ` + pathRuleCols
	var r model.ProjectPathRule
	err := scanPathRule(s.Pool.QueryRow(ctx, q, ruleID, projectID, pathPrefix, mode), &r)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil && isPathRuleUniqueViolation(err) {
		return nil, ErrPathRuleTaken
	}
	return &r, err
}

// DeleteProjectPathRule removes the rule. Idempotent — silently no-ops if the
// rule doesn't exist (returns nil), so retries from the UI on flaky networks
// don't surface as user-visible errors.
func (s *Store) DeleteProjectPathRule(ctx context.Context, projectID, ruleID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM project_path_rules WHERE id = $1 AND project_id = $2`, ruleID, projectID)
	return err
}
