package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type SystemConfigEntry struct {
	Key       string     `json:"key"`
	Value     string     `json:"value"`
	UpdatedAt time.Time  `json:"updated_at"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
}

// GetSystemConfig returns every key currently in the table.
// Returned as a plain map for callers that want to overlay env defaults.
func (s *Store) GetSystemConfig(ctx context.Context) (map[string]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT key, value FROM system_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// ListSystemConfig returns the full audit-friendly form (with timestamps).
func (s *Store) ListSystemConfig(ctx context.Context) ([]SystemConfigEntry, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT key, value, updated_at, updated_by FROM system_config ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SystemConfigEntry
	for rows.Next() {
		var e SystemConfigEntry
		if err := rows.Scan(&e.Key, &e.Value, &e.UpdatedAt, &e.UpdatedBy); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// BulkSetSystemConfig upserts every key in `kv`. An empty value DELETES the row
// — that's how the UI clears a setting (e.g. wiping a Keycloak field to fall
// back to whatever env provided).
func (s *Store) BulkSetSystemConfig(ctx context.Context, kv map[string]string, updatedBy *uuid.UUID) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for k, v := range kv {
		if v == "" {
			if _, err := tx.Exec(ctx, `DELETE FROM system_config WHERE key = $1`, k); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO system_config (key, value, updated_by)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (key) DO UPDATE
			   SET value = EXCLUDED.value,
			       updated_at = now(),
			       updated_by = EXCLUDED.updated_by`,
			k, v, updatedBy,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
