// Package provision creates and tears down per-project backing services
// (Postgres database, Redis URL) so user apps can declare `needs.postgres
// = true` in their manifest and get `DATABASE_URL` injected automatically.
//
// Design rules:
//   * Idempotent: re-provisioning an existing project is a no-op (caller
//     also checks project_env first; this is belt-and-suspenders).
//   * No transactions around CREATE DATABASE — Postgres forbids that.
//   * Identifier sanitisation is bounded by the existing slug rules
//     (sanitizeForSlug emits [a-z0-9-]+), so we only have to swap '-' for
//     '_' to get a valid PG identifier.
//   * Passwords are 32 random url-safe chars — never logged, never returned
//     anywhere except inside the final DATABASE_URL.
package provision

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PostgresProvisioner is constructed once on boot, holds the admin
// connection URL (`postgres://iaiadmin:…@user-postgres:5432/postgres`)
// and the public host string used in the URL we hand back to user pods.
// Either field empty disables the provisioner — exposed via Enabled().
type PostgresProvisioner struct {
	adminURL   string
	publicHost string
}

func NewPostgresProvisioner(adminURL, publicHost string) *PostgresProvisioner {
	return &PostgresProvisioner{adminURL: adminURL, publicHost: publicHost}
}

// Enabled returns true when both connection halves are configured. The
// reconciler checks this before honouring manifest.needs.postgres so a
// half-configured platform won't fail every deploy with a confusing
// "couldn't reach 'user-postgres'" error — it'll just no-op the auto
// provisioning and let the user inject DATABASE_URL manually.
func (p *PostgresProvisioner) Enabled() bool {
	return p != nil && p.adminURL != "" && p.publicHost != ""
}

// dbIdentFor maps a project slug to a Postgres-safe identifier. Slugs already
// match [a-z0-9-]+ from sanitizeForSlug, so this is purely '-' → '_' plus a
// `proj_` prefix to avoid colliding with Postgres reserved names like
// `public` or `postgres`.
func dbIdentFor(slug string) string {
	return "proj_" + strings.ReplaceAll(slug, "-", "_")
}

// randomPassword returns 32 url-safe base64 chars (256 bits of entropy).
// Postgres has no functional length limit on passwords; the cap is for
// readability when admins eyeball the DATABASE_URL during debugging.
func randomPassword() (string, error) {
	b := make([]byte, 24) // 24 bytes → 32 chars base64
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
	return s, nil
}

// Provision creates a fresh database + role for the project and returns the
// connection URL ready to drop into DATABASE_URL. Safe to call repeatedly:
//   - If the role exists, we rotate its password (the caller already lost
//     the old one — it was never stored outside the encrypted project_env
//     row that the caller is about to overwrite anyway).
//   - If the database exists, we leave its contents alone and just re-grant.
//
// We deliberately use pgx.Identifier.Sanitize to quote names — even though
// our slug → ident mapping is tight, an extra layer of defense costs nothing
// and protects against future slug-format changes.
func (p *PostgresProvisioner) Provision(ctx context.Context, slug string) (string, error) {
	if !p.Enabled() {
		return "", errors.New("postgres provisioner not enabled")
	}

	conn, err := pgx.Connect(ctx, p.adminURL)
	if err != nil {
		return "", fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx)

	dbname := dbIdentFor(slug)
	username := dbname
	password, err := randomPassword()
	if err != nil {
		return "", err
	}

	dbQ := pgx.Identifier{dbname}.Sanitize()       // "proj_foo"
	userQ := pgx.Identifier{username}.Sanitize()   // same here

	// Role: create if missing, rotate password regardless. The literal $1 form
	// doesn't work inside CREATE/ALTER ROLE — we have to splice the password
	// into the SQL. pgx.QuoteString escapes single-quotes.
	pwLit := quoteSQLString(password)

	var roleExists bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, username,
	).Scan(&roleExists); err != nil {
		return "", fmt.Errorf("check role: %w", err)
	}
	if roleExists {
		if _, err := conn.Exec(ctx, `ALTER ROLE `+userQ+` WITH LOGIN PASSWORD `+pwLit); err != nil {
			return "", fmt.Errorf("rotate role: %w", err)
		}
	} else {
		if _, err := conn.Exec(ctx, `CREATE ROLE `+userQ+` WITH LOGIN PASSWORD `+pwLit); err != nil {
			return "", fmt.Errorf("create role: %w", err)
		}
	}

	// Database: create if missing. CREATE DATABASE can't run inside a tx,
	// pgx already opens these in autocommit so we're fine.
	var dbExists bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, dbname,
	).Scan(&dbExists); err != nil {
		return "", fmt.Errorf("check db: %w", err)
	}
	if !dbExists {
		if _, err := conn.Exec(ctx, `CREATE DATABASE `+dbQ+` OWNER `+userQ); err != nil {
			return "", fmt.Errorf("create db: %w", err)
		}
	} else {
		// Make sure owner is current (in case role was just recreated).
		if _, err := conn.Exec(ctx, `ALTER DATABASE `+dbQ+` OWNER TO `+userQ); err != nil {
			return "", fmt.Errorf("set db owner: %w", err)
		}
	}

	// Always grant — covers the corner case where the DB existed but the
	// privileges were revoked (e.g. test scenario).
	if _, err := conn.Exec(ctx, `GRANT ALL PRIVILEGES ON DATABASE `+dbQ+` TO `+userQ); err != nil {
		return "", fmt.Errorf("grant: %w", err)
	}

	url := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable",
		username, password, p.publicHost, dbname)
	return url, nil
}

// Deprovision drops the project's database + role. Best-effort: returns the
// FIRST error but continues attempting the rest, so a partial cleanup is
// preferred to a half-cleaned-up stuck state.
func (p *PostgresProvisioner) Deprovision(ctx context.Context, slug string) error {
	if !p.Enabled() {
		return nil
	}
	conn, err := pgx.Connect(ctx, p.adminURL)
	if err != nil {
		return fmt.Errorf("connect admin: %w", err)
	}
	defer conn.Close(ctx)

	dbname := dbIdentFor(slug)
	username := dbname
	dbQ := pgx.Identifier{dbname}.Sanitize()
	userQ := pgx.Identifier{username}.Sanitize()

	var firstErr error
	// Terminate live connections so DROP DATABASE doesn't fail with
	// "database is being accessed by other users".
	_, _ = conn.Exec(ctx,
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, dbname)
	if _, err := conn.Exec(ctx, `DROP DATABASE IF EXISTS `+dbQ); err != nil {
		firstErr = fmt.Errorf("drop db: %w", err)
	}
	if _, err := conn.Exec(ctx, `DROP ROLE IF EXISTS `+userQ); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("drop role: %w", err)
	}
	return firstErr
}

// quoteSQLString escapes a password for inline use in CREATE/ALTER ROLE.
// pgx doesn't expose this as a public function — we re-implement: doubled
// single-quotes is sufficient for SQL string literals; passwords contain
// only url-safe-base64 chars so there are no single quotes to worry about,
// but we double-escape defensively anyway.
func quoteSQLString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
