package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/migrations"
)

func TestApplyMigrationsRecordsLedgerAndSkipsAppliedMigration(t *testing.T) {
	ctx := context.Background()
	db := openMigrationRunnerTestDB(t, ctx)
	defer db.Close()

	migration := migrationRunnerTestMigration("0001", "initial", `
CREATE TABLE applied_once (
    id TEXT PRIMARY KEY
);`)
	if err := db.applyMigrations(ctx, []migrations.Migration{migration}, MigrateOptions{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	first := readMigrationRunnerLedger(t, db.SQLDB(), "0001")

	if err := db.applyMigrations(ctx, []migrations.Migration{migration}, MigrateOptions{}); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	second := readMigrationRunnerLedger(t, db.SQLDB(), "0001")

	if first != second {
		t.Fatalf("ledger changed on second migrate: first=%#v second=%#v", first, second)
	}
}

func TestApplyMigrationsRejectsChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	db := openMigrationRunnerTestDB(t, ctx)
	defer db.Close()

	original := migrationRunnerTestMigration("0001", "initial", `
CREATE TABLE original_table (
    id TEXT PRIMARY KEY
);`)
	if err := db.applyMigrations(ctx, []migrations.Migration{original}, MigrateOptions{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}

	changed := migrationRunnerTestMigration("0001", "initial", `
CREATE TABLE changed_table (
    id TEXT PRIMARY KEY
);`)
	err := db.applyMigrations(ctx, []migrations.Migration{changed}, MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("checksum mismatch error = %v", err)
	}
	if tableExistsForMigrationRunnerTest(t, db.SQLDB(), "changed_table") {
		t.Fatal("changed migration body was executed after checksum mismatch")
	}
}

func TestApplyMigrationsBlocksDirtyLedger(t *testing.T) {
	ctx := context.Background()
	db := openMigrationRunnerTestDB(t, ctx)
	defer db.Close()

	migration := migrationRunnerTestMigration("0001", "initial", `
CREATE TABLE dirty_block_seed (
    id TEXT PRIMARY KEY
);`)
	if err := db.applyMigrations(ctx, []migrations.Migration{migration}, MigrateOptions{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	mustExecMigrationRunnerTest(t, db.SQLDB(), `UPDATE schema_migrations SET dirty = 1 WHERE version = '0001'`)

	next := migrationRunnerTestMigration("0002", "next", `
CREATE TABLE must_not_run (
    id TEXT PRIMARY KEY
);`)
	err := db.applyMigrations(ctx, []migrations.Migration{migration, next}, MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty ledger error = %v", err)
	}
	if tableExistsForMigrationRunnerTest(t, db.SQLDB(), "must_not_run") {
		t.Fatal("migration ran after dirty ledger")
	}
}

func TestApplyMigrationsMarksDirtyAndRollsBackOnFailure(t *testing.T) {
	ctx := context.Background()
	db := openMigrationRunnerTestDB(t, ctx)
	defer db.Close()

	broken := migrationRunnerTestMigration("0001", "broken", `
CREATE TABLE rolled_back (
    id TEXT PRIMARY KEY
);
INSERT INTO missing_table(id) VALUES ('x');`)
	err := db.applyMigrations(ctx, []migrations.Migration{broken}, MigrateOptions{})
	if err == nil {
		t.Fatal("broken migration succeeded")
	}
	if tableExistsForMigrationRunnerTest(t, db.SQLDB(), "rolled_back") {
		t.Fatal("failed migration was not rolled back")
	}

	ledger := readMigrationRunnerLedger(t, db.SQLDB(), "0001")
	if ledger.Dirty != 1 {
		t.Fatalf("dirty = %d, want 1", ledger.Dirty)
	}
	if ledger.Name != "broken" {
		t.Fatalf("dirty name = %q, want broken", ledger.Name)
	}
}

func TestApplyMigrationsRejectsLegacySchemaMigrationsTable(t *testing.T) {
	ctx := context.Background()
	db := openMigrationRunnerTestDB(t, ctx)
	defer db.Close()
	mustExecMigrationRunnerTest(t, db.SQLDB(), `
CREATE TABLE schema_migrations (
    version TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`)

	err := db.applyMigrations(ctx, []migrations.Migration{migrationRunnerTestMigration("0001", "initial", `SELECT 1;`)}, MigrateOptions{})
	if err == nil || !strings.Contains(err.Error(), "rebuild") {
		t.Fatalf("legacy schema error = %v", err)
	}
}

func TestMigrateCanCreateFTSAfterInitialMigrateWithoutFTS(t *testing.T) {
	ctx := context.Background()
	db := openMigrationRunnerTestDB(t, ctx)
	defer db.Close()

	if err := db.MigrateWithOptions(ctx, MigrateOptions{EnableFTS: false}); err != nil {
		t.Fatalf("migrate without fts: %v", err)
	}
	if tableExistsForMigrationRunnerTest(t, db.SQLDB(), "memory_search_fts") {
		t.Fatal("memory_search_fts exists after EnableFTS=false")
	}
	if err := db.MigrateWithOptions(ctx, MigrateOptions{EnableFTS: true}); err != nil {
		t.Fatalf("migrate with fts: %v", err)
	}
	if !tableExistsForMigrationRunnerTest(t, db.SQLDB(), "memory_search_fts") {
		t.Fatal("memory_search_fts was not created after EnableFTS=true")
	}
}

type migrationRunnerLedgerRow struct {
	Name        string
	Checksum    string
	AppliedAt   string
	Dirty       int
	ExecutionMS int64
}

func openMigrationRunnerTestDB(t *testing.T, ctx context.Context) *DB {
	t.Helper()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

func migrationRunnerTestMigration(version string, name string, sqlBody string) migrations.Migration {
	sum := sha256.Sum256([]byte(sqlBody))
	return migrations.Migration{
		Version:  version,
		Name:     name,
		Checksum: fmt.Sprintf("%x", sum),
		SQL:      sqlBody,
	}
}

func readMigrationRunnerLedger(t *testing.T, db *sql.DB, version string) migrationRunnerLedgerRow {
	t.Helper()
	var row migrationRunnerLedgerRow
	err := db.QueryRow(`
SELECT name, checksum, applied_at, dirty, execution_ms
FROM schema_migrations
WHERE version = ?`, version).Scan(&row.Name, &row.Checksum, &row.AppliedAt, &row.Dirty, &row.ExecutionMS)
	if err != nil {
		t.Fatalf("read migration ledger %s: %v", version, err)
	}
	return row
}

func tableExistsForMigrationRunnerTest(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return count > 0
}

func mustExecMigrationRunnerTest(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
