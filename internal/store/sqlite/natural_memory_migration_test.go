package sqlite_test

import (
	"context"
	"database/sql"
	"testing"
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

func boolIntForMigrationTest(value bool) int {
	if value {
		return 1
	}
	return 0
}
