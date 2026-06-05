package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/migrations"
)

type MigrateOptions struct {
	EnableFTS bool
}

func (d *DB) Migrate(ctx context.Context) error {
	return d.MigrateWithOptions(ctx, MigrateOptions{EnableFTS: true})
}

func (d *DB) MigrateWithOptions(ctx context.Context, opts MigrateOptions) error {
	all, err := migrations.All()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	if err := d.applyMigrations(ctx, all, opts); err != nil {
		return err
	}
	if opts.EnableFTS {
		if err := ensureSearchFTS(ctx, d.db); err != nil {
			return fmt.Errorf("ensure search fts: %w", err)
		}
	}
	return nil
}

func (d *DB) applyMigrations(ctx context.Context, all []migrations.Migration, _ MigrateOptions) error {
	if err := d.ensureMigrationLedger(ctx); err != nil {
		return err
	}
	applied, err := d.loadMigrationLedger(ctx)
	if err != nil {
		return err
	}
	for _, row := range applied {
		if row.Dirty != 0 {
			return fmt.Errorf("migration %s (%s) is dirty; rebuild the database before running automatic migrations", row.Version, row.Name)
		}
	}

	for _, migration := range all {
		row, ok := applied[migration.Version]
		if ok {
			if row.Name != migration.Name {
				return fmt.Errorf("migration %s name mismatch: applied %s, repo %s", migration.Version, row.Name, migration.Name)
			}
			if row.Checksum != migration.Checksum {
				return fmt.Errorf("migration %s checksum mismatch: applied %s, repo %s", migration.Version, row.Checksum, migration.Checksum)
			}
			continue
		}
		if err := d.execMigration(ctx, migration); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureMigrationLedger(ctx context.Context) error {
	exists, err := d.schemaMigrationsExists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		if _, err := d.db.ExecContext(ctx, `
CREATE TABLE schema_migrations (
    version      TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    checksum     TEXT NOT NULL,
    applied_at   TEXT NOT NULL,
    dirty        INTEGER NOT NULL
        CHECK (dirty IN (0, 1)),
    execution_ms INTEGER NOT NULL
)`); err != nil {
			return fmt.Errorf("create schema_migrations ledger: %w", err)
		}
		return nil
	}
	return d.validateMigrationLedger(ctx)
}

func (d *DB) schemaMigrationsExists(ctx context.Context) (bool, error) {
	var count int
	if err := d.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_master
WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect schema_migrations: %w", err)
	}
	return count > 0, nil
}

func (d *DB) validateMigrationLedger(ctx context.Context) error {
	rows, err := d.db.QueryContext(ctx, `PRAGMA table_info(schema_migrations)`)
	if err != nil {
		return fmt.Errorf("inspect schema_migrations columns: %w", err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("read schema_migrations columns: %w", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema_migrations columns: %w", err)
	}
	for _, required := range []string{"version", "name", "checksum", "applied_at", "dirty", "execution_ms"} {
		if !columns[required] {
			return fmt.Errorf("legacy schema_migrations table is not supported; rebuild the database before running automatic migrations")
		}
	}
	return nil
}

type migrationLedgerRow struct {
	Version     string
	Name        string
	Checksum    string
	AppliedAt   string
	Dirty       int
	ExecutionMS int64
}

func (d *DB) loadMigrationLedger(ctx context.Context) (map[string]migrationLedgerRow, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT version, name, checksum, applied_at, dirty, execution_ms
FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations ledger: %w", err)
	}
	defer rows.Close()

	applied := map[string]migrationLedgerRow{}
	for rows.Next() {
		var row migrationLedgerRow
		if err := rows.Scan(&row.Version, &row.Name, &row.Checksum, &row.AppliedAt, &row.Dirty, &row.ExecutionMS); err != nil {
			return nil, fmt.Errorf("scan schema_migrations ledger: %w", err)
		}
		applied[row.Version] = row
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan schema_migrations ledger: %w", err)
	}
	return applied, nil
}

func (d *DB) execMigration(ctx context.Context, migration migrations.Migration) error {
	start := time.Now()
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.Version, err)
	}

	for _, statement := range splitSQLStatements(migration.SQL) {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			if dirtyErr := d.markMigrationDirty(ctx, migration, time.Since(start)); dirtyErr != nil {
				return fmt.Errorf("apply migration %s: %w; mark dirty: %v", migration.Version, err, dirtyErr)
			}
			return fmt.Errorf("apply migration %s (%s): %w", migration.Version, migration.Name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO schema_migrations(version, name, checksum, applied_at, dirty, execution_ms)
VALUES (?, ?, ?, ?, 0, ?)`,
		migration.Version,
		migration.Name,
		migration.Checksum,
		time.Now().UTC().Format(time.RFC3339Nano),
		migrationExecutionMS(time.Since(start)),
	); err != nil {
		_ = tx.Rollback()
		if dirtyErr := d.markMigrationDirty(ctx, migration, time.Since(start)); dirtyErr != nil {
			return fmt.Errorf("record migration %s: %w; mark dirty: %v", migration.Version, err, dirtyErr)
		}
		return fmt.Errorf("record migration %s (%s): %w", migration.Version, migration.Name, err)
	}
	if err := tx.Commit(); err != nil {
		if dirtyErr := d.markMigrationDirty(ctx, migration, time.Since(start)); dirtyErr != nil {
			return fmt.Errorf("commit migration %s: %w; mark dirty: %v", migration.Version, err, dirtyErr)
		}
		return fmt.Errorf("commit migration %s (%s): %w", migration.Version, migration.Name, err)
	}
	return nil
}

func (d *DB) markMigrationDirty(ctx context.Context, migration migrations.Migration, elapsed time.Duration) error {
	_, err := d.db.ExecContext(ctx, `
INSERT INTO schema_migrations(version, name, checksum, applied_at, dirty, execution_ms)
VALUES (?, ?, ?, ?, 1, ?)
ON CONFLICT(version) DO UPDATE SET
    name = excluded.name,
    checksum = excluded.checksum,
    applied_at = excluded.applied_at,
    dirty = 1,
    execution_ms = excluded.execution_ms`,
		migration.Version,
		migration.Name,
		migration.Checksum,
		time.Now().UTC().Format(time.RFC3339Nano),
		migrationExecutionMS(elapsed),
	)
	return err
}

func migrationExecutionMS(elapsed time.Duration) int64 {
	ms := elapsed.Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func splitSQLStatements(script string) []string {
	parts := strings.Split(script, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt == "" {
			continue
		}
		statements = append(statements, stmt)
	}
	return statements
}
