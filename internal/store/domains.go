package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDomainTaken is returned by AddDomain when the hostname is already bound
// to another project (the UNIQUE constraint on domains.hostname kicked in).
// Lets the API layer map the error to a clean 409 without parsing pg error
// strings everywhere.
var ErrDomainTaken = errors.New("hostname already in use by another project")

type Domain struct {
	ID         uuid.UUID  `json:"id"`
	ProjectID  uuid.UUID  `json:"project_id"`
	Hostname   string     `json:"hostname"`
	Kind       string     `json:"kind"`        // 'custom'
	Verified   bool       `json:"verified"`
	TLSSecret  string     `json:"tls_secret,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
}

func (s *Store) AddDomain(ctx context.Context, projectID uuid.UUID, hostname string) (*Domain, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	const q = `
		INSERT INTO domains (project_id, hostname, kind, verified)
		VALUES ($1, $2, 'custom', false)
		RETURNING id, project_id, hostname, kind, verified, COALESCE(tls_secret,''), created_at, verified_at
	`
	var d Domain
	err := s.Pool.QueryRow(ctx, q, projectID, hostname).Scan(
		&d.ID, &d.ProjectID, &d.Hostname, &d.Kind, &d.Verified, &d.TLSSecret, &d.CreatedAt, &d.VerifiedAt,
	)
	if err != nil {
		// Postgres unique_violation = 23505. The only UNIQUE constraint on this
		// table is domains.hostname, so this collision means another project
		// already owns the hostname. Surface as a typed error so the API can
		// 409 instead of leaking the raw pg message.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrDomainTaken
		}
		return nil, err
	}
	return &d, nil
}

func (s *Store) RemoveDomain(ctx context.Context, projectID uuid.UUID, hostname string) error {
	tag, err := s.Pool.Exec(ctx,
		`DELETE FROM domains WHERE project_id = $1 AND hostname = $2`,
		projectID, strings.ToLower(strings.TrimSpace(hostname)),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListDomains(ctx context.Context, projectID uuid.UUID) ([]*Domain, error) {
	const q = `
		SELECT id, project_id, hostname, kind, verified, COALESCE(tls_secret,''), created_at, verified_at
		FROM domains
		WHERE project_id = $1
		ORDER BY created_at ASC
	`
	rows, err := s.Pool.Query(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Domain
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Hostname, &d.Kind, &d.Verified, &d.TLSSecret, &d.CreatedAt, &d.VerifiedAt); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// LookupProjectByHostname matches both the project's default subdomain
// (computed at the API layer from slug + base domain) and any verified
// custom domain. Returns ErrNotFound when nothing matches.
func (s *Store) LookupProjectIDByHostname(ctx context.Context, hostname string) (uuid.UUID, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	var id uuid.UUID
	err := s.Pool.QueryRow(ctx,
		`SELECT project_id FROM domains WHERE hostname = $1`,
		hostname,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return id, err
}
