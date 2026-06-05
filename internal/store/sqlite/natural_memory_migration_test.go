package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestNaturalMigrationCreatesTables(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	requireTable(t, db.SQLDB(), "memory_natural_states")
	requireTable(t, db.SQLDB(), "memory_natural_events")
	requireTable(t, db.SQLDB(), "memory_natural_runs")
	requireTable(t, db.SQLDB(), "memory_natural_compression_candidates")
}

func TestNaturalMigrationLimitsMarkedSleepCycleOncePerDay(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	insertNaturalRunForMigrationTest(t, db.SQLDB(), "run_marked_1", "manual", "2026-06-05", true, false)
	if _, err := db.SQLDB().Exec(`
INSERT INTO memory_natural_runs (
    id, persona_id, run_kind, algorithm_version, local_date, local_time, timezone,
    dry_run, force, mark_sleep_cycle, status
) VALUES (
    'run_marked_2', 'default', 'manual', 'natural_power_sleep_v1',
    '2026-06-05', '03:30', 'Asia/Shanghai', 0, 0, 1, 'completed'
)`); err == nil {
		t.Fatal("second marked sleep-cycle run inserted, want unique constraint")
	}

	insertNaturalRunForMigrationTest(t, db.SQLDB(), "run_marked_forced", "manual", "2026-06-05", true, true)
	if _, err := db.SQLDB().Exec(`
INSERT INTO memory_natural_runs (
    id, persona_id, run_kind, algorithm_version, local_date, local_time, timezone,
    dry_run, force, mark_sleep_cycle, status
) VALUES (
    'run_sleep_same_day', 'default', 'sleep_cycle', 'natural_power_sleep_v1',
    '2026-06-05', '03:30', 'Asia/Shanghai', 0, 0, 0, 'completed'
)`); err == nil {
		t.Fatal("sleep_cycle inserted after marked manual for same day, want unique constraint")
	}
}

func TestNaturalMigrationDeduplicatesExistingQuotaRowsBeforeUniqueIndex(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}
	mustExec(t, db.SQLDB(), `DROP INDEX IF EXISTS idx_memory_natural_quota_once_per_day`)
	insertNaturalRunForMigrationTest(t, db.SQLDB(), "run_sleep_existing", "sleep_cycle", "2026-06-05", false, false)
	insertNaturalRunForMigrationTest(t, db.SQLDB(), "run_marked_conflict", "manual", "2026-06-05", true, false)
	insertNaturalRunForMigrationTest(t, db.SQLDB(), "run_marked_keep", "manual", "2026-06-06", true, false)
	insertNaturalRunForMigrationTest(t, db.SQLDB(), "run_marked_duplicate", "manual", "2026-06-06", true, false)

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate with existing duplicates: %v", err)
	}
	requireMarkedSleepCycleFlag(t, db.SQLDB(), "run_marked_conflict", 0)
	requireMarkedSleepCycleFlag(t, db.SQLDB(), "run_marked_keep", 1)
	requireMarkedSleepCycleFlag(t, db.SQLDB(), "run_marked_duplicate", 0)
}

func insertNaturalRunForMigrationTest(t *testing.T, db *sql.DB, id string, runKind string, localDate string, markSleepCycle bool, force bool) {
	t.Helper()
	_, err := db.Exec(`
INSERT INTO memory_natural_runs (
    id, persona_id, run_kind, algorithm_version, local_date, local_time, timezone,
    dry_run, force, mark_sleep_cycle, status
) VALUES (?, 'default', ?, 'natural_power_sleep_v1', ?, '03:30', 'Asia/Shanghai', 0, ?, ?, 'completed')`,
		id,
		runKind,
		localDate,
		boolIntForMigrationTest(force),
		boolIntForMigrationTest(markSleepCycle),
	)
	if err != nil {
		t.Fatalf("insert natural run %s: %v", id, err)
	}
}

func requireMarkedSleepCycleFlag(t *testing.T, db *sql.DB, runID string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT mark_sleep_cycle FROM memory_natural_runs WHERE id = ?`, runID).Scan(&got); err != nil {
		t.Fatalf("query mark_sleep_cycle for %s: %v", runID, err)
	}
	if got != want {
		t.Fatalf("mark_sleep_cycle for %s = %d, want %d", runID, got, want)
	}
}

func boolIntForMigrationTest(value bool) int {
	if value {
		return 1
	}
	return 0
}
