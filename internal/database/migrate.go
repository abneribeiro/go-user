package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
)


var migrationsFS embed.FS

const lockID = 4242

func Migrate(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)

	if err != nil {
		return  err
	}

	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockID); err != nil {
		return err;
	}

	defer conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", lockID)

	const create = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version	TEXT PRIMARY KEY,
		applied_at	TIMESTAMPTZ NOT NULL DEFAULT now())`
	
	if _, err := conn.ExecContext(ctx, create); err != nil {
		return  err
	}

	entries, err := migrationsFS.ReadDir("migrations")

	if err != nil {
		return  err
	}

	names := make([]string, 0, len(entries))

	for _, e := range entries {
		names = append(names, e.Name())
	}

	sort.Strings(names) // 0001, 0002, 0003: a order is the name

	for _, name := range names {
		var applied bool
		const q = "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)"

		if err := conn.QueryRowContext(ctx, q, name).Scan(&applied); err != nil {
			return  err
		}

		if applied {
			continue
		}

		body, err := migrationsFS.ReadFile("migrations/" + name)

		if err != nil {
			return err
		}

		tx, err := conn.BeginTx(ctx, nil)

		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		
		const ins = "INSERT INTO schema_migrations (version) values ($1)"
		if _, err := tx.ExecContext(ctx, ins, name); err != nil {
				_ = tx.Rollback()
				return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

