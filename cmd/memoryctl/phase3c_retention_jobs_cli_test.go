package main

import (
	"path/filepath"
	"testing"
)

func TestRunRetentionJobsDailyAndMonthlyTextOutputAndState(t *testing.T) {
	dbPath, expiringFactID := seedCLIRetentionFact(t, "retention_jobs_expiring", "2026-05-01T00:00:00Z")
	_, oldArchivedFactID := seedCLIRetentionFactInDB(t, dbPath, "retention_jobs_old_archived", "")
	setCLIFactArchivedAt(t, dbPath, oldArchivedFactID, "2026-01-01T00:00:00Z")

	out := requireRunOK(t,
		"retention-jobs-run",
		"--db", dbPath,
		"--now", "2026-08-01T00:00:00Z",
		"--jobs", "daily_ttl_expiry,monthly_deep_archive",
		"--deep-archive-after-days", "180",
		"--format", "text",
	)
	want := "jobs=daily_ttl_expiry,monthly_deep_archive\n" +
		"evaluated_facts=2\n" +
		"expired_facts=1\n" +
		"archived_facts=1\n" +
		"deep_archived_facts=1\n" +
		"search_documents_synced=2\n" +
		"mirror_updates_enqueued=0\n"
	if out != want {
		t.Fatalf("retention jobs output = %q, want %q", out, want)
	}

	requireFactCLIState(t, dbPath, expiringFactID, "invalidated", "archived")
	requireFactCLIState(t, dbPath, oldArchivedFactID, "valid", "deep_archived")
}

func TestRunRetentionJobsRejectsUnknownJob(t *testing.T) {
	dbPath, factID := seedCLIRetentionFact(t, "retention_jobs_unknown_job", "2026-05-01T00:00:00Z")

	_, stderr, code := runCLI("retention-jobs-run", "--db", dbPath, "--jobs", "daily_ttl_expiry,unknown_job", "--now", "2026-08-01T00:00:00Z")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "unknown retention job")
	requireFactCLIState(t, dbPath, factID, "valid", "active")
}

func TestRunRetentionJobsRejectsMonthlyWithoutPositiveDeepArchiveAfterDays(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)

	_, stderr, code := runCLI("retention-jobs-run", "--db", dbPath, "--jobs", "monthly_deep_archive")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "monthly_deep_archive requires --deep-archive-after-days > 0")
}

func TestRunRetentionJobsDryRunDoesNotMutate(t *testing.T) {
	dbPath, expiringFactID := seedCLIRetentionFact(t, "retention_jobs_dry_expiring", "2026-05-01T00:00:00Z")
	_, oldArchivedFactID := seedCLIRetentionFactInDB(t, dbPath, "retention_jobs_dry_old_archived", "")
	setCLIFactArchivedAt(t, dbPath, oldArchivedFactID, "2026-01-01T00:00:00Z")

	out := requireRunOK(t,
		"retention-jobs-run",
		"--db", dbPath,
		"--now", "2026-08-01T00:00:00Z",
		"--jobs", "daily_ttl_expiry,monthly_deep_archive",
		"--deep-archive-after-days", "180",
		"--dry-run",
		"--format", "text",
	)
	want := "jobs=daily_ttl_expiry,monthly_deep_archive\n" +
		"evaluated_facts=2\n" +
		"expired_facts=1\n" +
		"archived_facts=1\n" +
		"deep_archived_facts=1\n" +
		"search_documents_synced=0\n" +
		"mirror_updates_enqueued=0\n"
	if out != want {
		t.Fatalf("dry-run retention jobs output = %q, want %q", out, want)
	}

	requireFactCLIState(t, dbPath, expiringFactID, "valid", "active")
	requireFactCLIState(t, dbPath, oldArchivedFactID, "valid", "archived")
}

func seedCLIRetentionFactInDB(t *testing.T, dbPath string, factID string, validTo string) (string, string) {
	t.Helper()

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
