package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/migrations"
)

func (d *DB) Migrate(ctx context.Context) error {
	all, err := migrations.All()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	for _, migration := range all {
		if err := d.execMigration(ctx, migration); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) execMigration(ctx context.Context, migration migrations.Migration) error {
	for _, statement := range splitSQLStatements(migration.SQL) {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := d.db.ExecContext(ctx, statement); err != nil {
			if isOptionalFTSStatement(statement) && isFTSUnavailable(err) {
				continue
			}
			return fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
	}
	return nil
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

func isOptionalFTSStatement(statement string) bool {
	upper := strings.ToUpper(statement)
	return strings.Contains(upper, "CREATE VIRTUAL TABLE IF NOT EXISTS MEMORY_SEARCH_FTS")
}

func isFTSUnavailable(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fts5") || strings.Contains(msg, "virtual table")
}
