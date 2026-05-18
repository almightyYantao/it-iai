package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iai/vibedeploy/internal/model"
)

type DeployTokenRecord struct {
	ID         uuid.UUID
	ProjectID  *uuid.UUID
	Name       string
	Scopes     []string
	AllowDB    bool
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedBy  *uuid.UUID
	TokenPrefix string
}

// LookupTokenByHash returns the active token matching the given sha256 hash.
func (s *Store) LookupTokenByHash(ctx context.Context, hash []byte) (*DeployTokenRecord, error) {
	const q = `
		SELECT id, project_id, name, scopes, allow_db, expires_at, revoked_at, created_by, token_prefix
		FROM deploy_tokens
		WHERE token_hash = $1
	`
	var r DeployTokenRecord
	err := s.Pool.QueryRow(ctx, q, hash).Scan(
		&r.ID, &r.ProjectID, &r.Name, &r.Scopes, &r.AllowDB, &r.ExpiresAt, &r.RevokedAt, &r.CreatedBy, &r.TokenPrefix,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) TouchTokenLastUsed(ctx context.Context, id uuid.UUID) {
	_, _ = s.Pool.Exec(ctx, `UPDATE deploy_tokens SET last_used_at = now() WHERE id = $1`, id)
}

type CreateTokenInput struct {
	ProjectID *uuid.UUID
	Name      string
	Hash      []byte
	Prefix    string
	Scopes    []string
	AllowDB   bool
	ExpiresAt time.Time
	CreatedBy *uuid.UUID
}

func (s *Store) CreateDeployToken(ctx context.Context, in CreateTokenInput) (*model.DeployToken, error) {
	const q = `
		INSERT INTO deploy_tokens (project_id, name, token_hash, token_prefix, scopes, allow_db, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, project_id, name, token_prefix, scopes, allow_db, created_at, expires_at, last_used_at, revoked_at
	`
	var t model.DeployToken
	err := s.Pool.QueryRow(ctx, q, in.ProjectID, in.Name, in.Hash, in.Prefix, in.Scopes, in.AllowDB, in.ExpiresAt, in.CreatedBy).
		Scan(&t.ID, &t.ProjectID, &t.Name, &t.TokenPrefix, &t.Scopes, &t.AllowDB, &t.CreatedAt, &t.ExpiresAt, &t.LastUsedAt, &t.RevokedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
