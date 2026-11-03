// rekey-env re-encrypts every project_env row under a new KEK, binding each
// value to its (project_id, key) via AAD.
//
// It covers two jobs:
//
//   - KEK rotation. Rows already AAD-bound are read under the old key and
//     re-sealed under the new one. This is the routine case — run it whenever
//     CP_KEK_BASE64 has to change (suspected key exposure, scheduled rotation).
//
//   - The 2026-07 AAD migration. Ciphertext used to be sealed with AAD=nil, so a
//     row could be copied between projects and the platform would happily
//     decrypt it on the next deploy. EnvAAD binds each value to its
//     (project_id, key); passing --old-kek == --new-kek converts that corpus
//     without changing keys.
//
// Both source formats are accepted in the same run, so a half-migrated corpus
// converges. Restart the control plane with the new CP_KEK_BASE64 afterwards —
// until then it cannot read the rows this tool just rewrote.
//
// Usage:
//
//	rekey-env --dsn <postgres-dsn> --old-kek <b64> --new-kek <b64> [--dry-run]
//
// Safe to re-run: rows already readable under (new KEK + AAD) are skipped, so an
// interrupted run can simply be repeated.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iai/vibedeploy/internal/auth"
)

type row struct {
	ProjectID uuid.UUID
	Key       string
	CT        []byte
}

func main() {
	dsn := flag.String("dsn", os.Getenv("CP_DATABASE_URL"), "control-plane postgres DSN")
	oldB64 := flag.String("old-kek", "", "current CP_KEK_BASE64 (used to read existing rows)")
	newB64 := flag.String("new-kek", "", "replacement CP_KEK_BASE64 (may equal old-kek to only add AAD)")
	dryRun := flag.Bool("dry-run", false, "report what would change, write nothing")
	flag.Parse()

	if *dsn == "" || *oldB64 == "" || *newB64 == "" {
		log.Fatal("--dsn, --old-kek and --new-kek are all required")
	}

	oldKEK, err := auth.LoadKEK(*oldB64)
	if err != nil {
		log.Fatalf("load old KEK: %v", err)
	}
	newKEK, err := auth.LoadKEK(*newB64)
	if err != nil {
		log.Fatalf("load new KEK: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, *dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	rows, err := readAll(ctx, pool)
	if err != nil {
		log.Fatalf("read project_env: %v", err)
	}
	log.Printf("project_env rows: %d", len(rows))

	type change struct {
		row row
		ct  []byte
	}
	var (
		todo    []change
		skipped int
		failed  []row
	)

	for _, r := range rows {
		aad := auth.EnvAAD(r.ProjectID.String(), r.Key)

		// Already sealed under the target key? Then leave it alone — keeps the
		// run idempotent.
		if _, err := newKEK.Decrypt(r.CT, aad); err == nil {
			skipped++
			continue
		}

		// Two source formats are accepted, tried in order:
		//   1. old KEK + AAD — a plain key rotation of an already-migrated row.
		//      This is the normal case once the 2026-07 AAD migration has run;
		//      without it the tool could only ever be used once.
		//   2. old KEK, no AAD — the pre-AAD legacy format.
		pt, err := oldKEK.Decrypt(r.CT, aad)
		if err != nil {
			pt, err = oldKEK.Decrypt(r.CT, nil)
		}
		if err != nil {
			failed = append(failed, r)
			continue
		}
		ct, err := newKEK.Encrypt(pt, aad)
		if err != nil {
			log.Fatalf("re-encrypt %s/%s: %v", r.ProjectID, r.Key, err)
		}
		todo = append(todo, change{r, ct})
	}

	log.Printf("to migrate: %d   already migrated: %d   unreadable: %d", len(todo), skipped, len(failed))
	for _, r := range failed {
		log.Printf("  UNREADABLE %s / %s (%d bytes) — neither new+AAD nor old+no-AAD", r.ProjectID, r.Key, len(r.CT))
	}
	if len(failed) > 0 {
		// Refuse to do a partial migration: a row we cannot read is either
		// corrupt or sealed under a third key, and either way the operator
		// needs to look at it before the KEK is retired.
		log.Fatal("aborting: some rows are unreadable, resolve them before migrating")
	}
	if *dryRun {
		log.Print("--dry-run: no writes performed")
		return
	}
	if len(todo) == 0 {
		log.Print("nothing to do")
		return
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	for _, c := range todo {
		// updated_at deliberately untouched: this is a storage-format change,
		// not a value change, and the column is shown to users as "last edited".
		if _, err := tx.Exec(ctx,
			`UPDATE project_env SET value_encrypted = $3 WHERE project_id = $1 AND key = $2`,
			c.row.ProjectID, c.row.Key, c.ct,
		); err != nil {
			log.Fatalf("update %s/%s: %v", c.row.ProjectID, c.row.Key, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err)
	}
	log.Printf("migrated %d rows", len(todo))

	if err := verify(ctx, pool, newKEK); err != nil {
		log.Fatalf("post-migration verify FAILED: %v", err)
	}
	log.Print("post-migration verify: all rows decrypt under new KEK + AAD")
}

func readAll(ctx context.Context, pool *pgxpool.Pool) ([]row, error) {
	rs, err := pool.Query(ctx, `SELECT project_id, key, value_encrypted FROM project_env ORDER BY project_id, key`)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.ProjectID, &r.Key, &r.CT); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rs.Err()
}

func verify(ctx context.Context, pool *pgxpool.Pool, kek *auth.KEK) error {
	rows, err := readAll(ctx, pool)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := kek.Decrypt(r.CT, auth.EnvAAD(r.ProjectID.String(), r.Key)); err != nil {
			return fmt.Errorf("%s / %s: %w", r.ProjectID, r.Key, err)
		}
	}
	if len(rows) == 0 {
		return errors.New("no rows read back")
	}
	return nil
}
