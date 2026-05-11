package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunRetentionArchivesExpiredFact(t *testing.T) {
	dbPath, factID := seedCLIRetentionFact(t, "retention_fact_archive", "2026-05-01T00:00:00Z")

	out := requireRunOK(t, "retention-run", "--db", dbPath, "--now", "2026-06-01T12:00:00Z", "--format", "text")
	want := "evaluated_facts=1\n" +
		"expired_facts=1\n" +
		"archived_facts=1\n" +
		"deep_archived_facts=0\n" +
		"search_documents_synced=1\n" +
		"mirror_updates_enqueued=0\n"
	if out != want {
		t.Fatalf("retention output = %q, want %q", out, want)
	}

	requireFactCLIState(t, dbPath, factID, "invalidated", "archived")
}

func TestRunRetentionDryRunDoesNotMutate(t *testing.T) {
	dbPath, factID := seedCLIRetentionFact(t, "retention_fact_dry_run", "2026-05-01T00:00:00Z")

	out := requireRunOK(t, "retention-run", "--db", dbPath, "--now", "2026-06-01T12:00:00Z", "--dry-run", "--format", "text")
	want := "evaluated_facts=1\n" +
		"expired_facts=1\n" +
		"archived_facts=1\n" +
		"deep_archived_facts=0\n" +
		"search_documents_synced=0\n" +
		"mirror_updates_enqueued=0\n"
	if out != want {
		t.Fatalf("dry-run output = %q, want %q", out, want)
	}

	requireFactCLIState(t, dbPath, factID, "valid", "active")
}

func TestRunRetentionRejectsInvalidNow(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)

	_, stderr, code := runCLI("retention-run", "--db", dbPath, "--now", "not-a-time")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--now must be RFC3339")
}

func TestRunRetentionDeepArchivesOldArchivedFact(t *testing.T) {
	dbPath, factID := seedCLIRetentionFact(t, "retention_fact_deep_archive", "")
	setCLIFactArchivedAt(t, dbPath, factID, "2026-01-01T00:00:00Z")

	out := requireRunOK(t, "retention-run", "--db", dbPath, "--now", "2026-08-01T00:00:00Z", "--deep-archive-after-days", "180", "--format", "text")
	want := "evaluated_facts=1\n" +
		"expired_facts=0\n" +
		"archived_facts=0\n" +
		"deep_archived_facts=1\n" +
		"search_documents_synced=1\n" +
		"mirror_updates_enqueued=0\n"
	if out != want {
		t.Fatalf("deep archive output = %q, want %q", out, want)
	}

	requireFactCLIState(t, dbPath, factID, "valid", "deep_archived")
}

func TestRunRetentionRejectsNegativeDeepArchiveAfterDays(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)

	_, stderr, code := runCLI("retention-run", "--db", dbPath, "--deep-archive-after-days", "-1")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--deep-archive-after-days must be >= 0")
}

func seedCLIRetentionFact(t *testing.T, factID string, validTo string) (string, string) {
	t.Helper()

	dbPath := seedCLIConsolidationDB(t)
	args := []string{
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", factID,
		"--summary", "retention summary " + factID,
		"--fact-type", "stable_preference",
		"--source-episode", "ep_seed",
		"--format", "id",
	}
	if validTo != "" {
		args = append(args, "--valid-to", validTo)
	}
	insertedID := requireRunID(t, args...)
	return dbPath, insertedID
}

func setCLIFactArchivedAt(t *testing.T, dbPath string, factID string, archivedAt string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`
UPDATE facts
SET lifecycle_status = 'archived',
    updated_at = ?
WHERE id = ?`, archivedAt, factID); err != nil {
		t.Fatalf("set CLI fact archived_at proxy: %v", err)
	}
}

func requireFactCLIState(t *testing.T, dbPath string, factID string, wantValidity string, wantLifecycle string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var validity, lifecycle string
	if err := db.QueryRow(`
SELECT validity_status, lifecycle_status
FROM facts
WHERE id = ?`, factID).Scan(&validity, &lifecycle); err != nil {
		t.Fatalf("query fact state: %v", err)
	}
	if validity != wantValidity || lifecycle != wantLifecycle {
		t.Fatalf("fact state = %s/%s, want %s/%s", validity, lifecycle, wantValidity, wantLifecycle)
	}
}
