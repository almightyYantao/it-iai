package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iai/vibedeploy/internal/model"
)

const presetCols = `name, label, description, cidrs, is_system, updated_at, updated_by`

func scanPreset(row pgx.Row, p *model.CIDRPreset) error {
	return row.Scan(&p.Name, &p.Label, &p.Description, &p.CIDRs, &p.IsSystem, &p.UpdatedAt, &p.UpdatedBy)
}

// ListCIDRPresets returns every preset, sorted with system presets first
// (so the UI dropdown leads with the curated standards) then alphabetically
// by label so user-added presets are predictable.
func (s *Store) ListCIDRPresets(ctx context.Context) ([]*model.CIDRPreset, error) {
	q := `SELECT ` + presetCols + ` FROM cidr_presets ORDER BY is_system DESC, label ASC`
	rows, err := s.Pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.CIDRPreset
	for rows.Next() {
		var p model.CIDRPreset
		if err := scanPreset(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) GetCIDRPreset(ctx context.Context, name string) (*model.CIDRPreset, error) {
	q := `SELECT ` + presetCols + ` FROM cidr_presets WHERE name = $1`
	var p model.CIDRPreset
	err := scanPreset(s.Pool.QueryRow(ctx, q, name), &p)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// UpsertCIDRPreset creates or updates a preset by name. is_system is NOT
// mutable from this API — only the seeded rows are system, set once by
// migration. cidrs is overwritten verbatim (caller normalises / validates).
func (s *Store) UpsertCIDRPreset(ctx context.Context, p *model.CIDRPreset, updatedBy *uuid.UUID) error {
	if p.CIDRs == nil {
		p.CIDRs = []string{}
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO cidr_presets (name, label, description, cidrs, is_system, updated_by)
		VALUES ($1, $2, $3, $4, false, $5)
		ON CONFLICT (name) DO UPDATE SET
		    label = EXCLUDED.label,
		    description = EXCLUDED.description,
		    cidrs = EXCLUDED.cidrs,
		    updated_at = now(),
		    updated_by = EXCLUDED.updated_by
	`, p.Name, p.Label, p.Description, p.CIDRs, updatedBy)
	return err
}

// DeleteCIDRPreset removes a non-system preset. Refuses (returns ErrPresetIsSystem)
// to delete `public` / `internal` because the project FK has ON DELETE SET NULL —
// removing a system preset would silently strip protection from any project
// pointing at it.
var ErrPresetIsSystem = errors.New("preset is a system preset and cannot be deleted")

func (s *Store) DeleteCIDRPreset(ctx context.Context, name string) error {
	// Check is_system first; better than relying on a DB constraint because
	// the API can return a clear 409 instead of a generic foreign-key error.
	var isSystem bool
	err := s.Pool.QueryRow(ctx, `SELECT is_system FROM cidr_presets WHERE name = $1`, name).Scan(&isSystem)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if isSystem {
		return ErrPresetIsSystem
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM cidr_presets WHERE name = $1`, name)
	return err
}
