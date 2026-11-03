package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iai/vibedeploy/internal/model"
)

const egressRuleCols = `id, project_id, kind, value, port, note, created_at, created_by`

func scanEgressRule(row pgx.Row, r *model.ProjectEgressRule) error {
	return row.Scan(&r.ID, &r.ProjectID, &r.Kind, &r.Value, &r.Port, &r.Note, &r.CreatedAt, &r.CreatedBy)
}

// ListProjectEgressRules returns the project's allow-list. Ordered so the two
// kinds stay grouped and the output is stable — the deployer and the proxy
// config renderer both diff against what they wrote last time, and a shuffled
// list would look like a change.
func (s *Store) ListProjectEgressRules(ctx context.Context, projectID uuid.UUID) ([]*model.ProjectEgressRule, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+egressRuleCols+`
		   FROM project_egress_rules
		  WHERE project_id = $1
		  ORDER BY kind ASC, value ASC, port ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.ProjectEgressRule
	for rows.Next() {
		var r model.ProjectEgressRule
		if err := scanEgressRule(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// EgressRuleInput is one row as the API layer received it, already validated.
type EgressRuleInput struct {
	Kind  string
	Value string
	Port  int
	Note  string
}

// ReplaceProjectEgressRules swaps the whole allow-list for a project.
//
// Replace rather than incremental add/remove because the admin UI edits the list
// as a whole: a PUT of the full set is the shape the form produces, and it can't
// drift from what the admin sees. Done in one transaction so a failure halfway
// through can't leave a project with a partially-applied policy — the failure
// mode we care about is "half the allow-list disappeared", which would look like
// an outage with no obvious cause.
//
// created_by records who made the change; the audit row the caller writes is the
// per-change history, this is just "who owns the current state".
func (s *Store) ReplaceProjectEgressRules(ctx context.Context, projectID uuid.UUID, rules []EgressRuleInput, createdBy *uuid.UUID) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM project_egress_rules WHERE project_id = $1`, projectID); err != nil {
		return err
	}
	for _, r := range rules {
		if _, err := tx.Exec(ctx,
			`INSERT INTO project_egress_rules (project_id, kind, value, port, note, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (project_id, kind, value, port) DO NOTHING`,
			projectID, r.Kind, r.Value, r.Port, r.Note, createdBy,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// EgressRuleCount is one row of the admin overview: how wide open each project
// is, without dumping every rule.
type EgressRuleCount struct {
	Slug      string `json:"slug"`
	Domains   int    `json:"domains"`
	Endpoints int    `json:"endpoints"`
}

// CountEgressRulesByProject powers the "who opened what" overview. Includes
// live projects with no rules at all so the list doubles as "everything that is
// currently sealed".
func (s *Store) CountEgressRulesByProject(ctx context.Context) ([]EgressRuleCount, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.slug,
		       count(*) FILTER (WHERE r.kind = 'domain')   AS domains,
		       count(*) FILTER (WHERE r.kind = 'endpoint') AS endpoints
		  FROM projects p
		  LEFT JOIN project_egress_rules r ON r.project_id = p.id
		 WHERE p.deleted_at IS NULL
		 GROUP BY p.slug
		 ORDER BY p.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EgressRuleCount
	for rows.Next() {
		var c EgressRuleCount
		if err := rows.Scan(&c.Slug, &c.Domains, &c.Endpoints); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
