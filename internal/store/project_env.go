package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/iai/vibedeploy/internal/auth"
)

// ProjectEnvEntry is the metadata-only view returned by ListProjectEnvKeys —
// no decrypted value, so the keys can be displayed without touching the KEK.
type ProjectEnvEntry struct {
	Key       string     `json:"key"`
	UpdatedAt time.Time  `json:"updated_at"`
	UpdatedBy *uuid.UUID `json:"updated_by,omitempty"`
	System    bool       `json:"system"` // true = managed by the platform (e.g. auto-provisioned DATABASE_URL), not user-editable
}

// SetProjectEnv upserts a single env var, encrypting the value with the KEK.
// `system=true` flags this row as platform-managed (auto-provisioned DBs etc),
// which the API uses to forbid the UI from overwriting it.
//
// Caller responsibility: passing the right `system` flag. Users editing via
// the API should always pass false; the provisioning code paths pass true.
func (s *Store) SetProjectEnv(ctx context.Context, projectID uuid.UUID, key, value string, system bool, kek *auth.KEK, updatedBy *uuid.UUID) error {
	ct, err := kek.Encrypt([]byte(value), auth.EnvAAD(projectID.String(), key))
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO project_env (project_id, key, value_encrypted, system, updated_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (project_id, key) DO UPDATE SET
		    value_encrypted = EXCLUDED.value_encrypted,
		    system          = EXCLUDED.system,
		    updated_at      = now(),
		    updated_by      = EXCLUDED.updated_by
	`, projectID, key, ct, system, updatedBy)
	return err
}

// GetProjectEnv returns every project_env row decrypted into a plain map —
// what gets injected into the pod's env. Heavy operation (decrypts per row),
// but project env sets are typically small (<20 keys); fine for the deploy
// hot path. Returns nil on a decrypt failure for ANY key — the safer thing
// is to refuse to deploy with a partial env than to silently drop keys.
func (s *Store) GetProjectEnv(ctx context.Context, projectID uuid.UUID, kek *auth.KEK) (map[string]string, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT key, value_encrypted FROM project_env WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key string
		var ct []byte
		if err := rows.Scan(&key, &ct); err != nil {
			return nil, err
		}
		pt, err := kek.Decrypt(ct, auth.EnvAAD(projectID.String(), key))
		if err != nil {
			// Also fires when a ciphertext was written for a different
			// project/key — i.e. someone moved the row. Deliberately not
			// distinguished from a genuine key mismatch in the message.
			return nil, errors.New("decrypt failed for env key: " + key)
		}
		out[key] = string(pt)
	}
	return out, rows.Err()
}

// ListProjectEnvKeys returns key names + audit metadata only — no values,
// no decryption. Cheap; safe to call from any handler that just needs to
// render a key list in the UI.
func (s *Store) ListProjectEnvKeys(ctx context.Context, projectID uuid.UUID) ([]ProjectEnvEntry, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT key, updated_at, updated_by, COALESCE(system, false)
		FROM project_env WHERE project_id = $1
		ORDER BY key ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectEnvEntry
	for rows.Next() {
		var e ProjectEnvEntry
		if err := rows.Scan(&e.Key, &e.UpdatedAt, &e.UpdatedBy, &e.System); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteProjectEnv removes a single key. Refuses to drop system-managed
// rows (those carry the credentials for auto-provisioned services and
// must only be removed by the corresponding deprovisioning code path).
// ErrSystemEnv lets the API translate this into a 409 instead of leaking
// the constraint to the user.
var ErrSystemEnv = errors.New("env key is platform-managed and can't be removed manually")

func (s *Store) DeleteProjectEnv(ctx context.Context, projectID uuid.UUID, key string) error {
	var system bool
	err := s.Pool.QueryRow(ctx,
		`SELECT COALESCE(system, false) FROM project_env WHERE project_id = $1 AND key = $2`,
		projectID, key,
	).Scan(&system)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if system {
		return ErrSystemEnv
	}
	_, err = s.Pool.Exec(ctx,
		`DELETE FROM project_env WHERE project_id = $1 AND key = $2`, projectID, key)
	return err
}

// DeleteAllProjectEnv wipes every row for a project — used by the project
// delete path. Doesn't distinguish system vs user since the project is going
// away entirely; deprovisioning the underlying service is a separate concern.
func (s *Store) DeleteAllProjectEnv(ctx context.Context, projectID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `DELETE FROM project_env WHERE project_id = $1`, projectID)
	return err
}
