package main

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunExtractRunMockDryRunApplyAndAuditFlag(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)

	dry := requireRunText(t,
		"extract-run",
		"--db", dbPath,
		"--session", "session_seed",
		"--provider", "mock",
		"--mode", "dry-run",
		"--audit", "off",
		"--format", "json",
	)
	requireContains(t, dry, `"status":"dry_run"`)
	requireFactCount(t, dbPath, 0)

	apply := requireRunText(t,
		"extract-run",
		"--db", dbPath,
		"--session", "session_seed",
		"--provider", "mock",
		"--mode", "apply",
		"--format", "json",
	)
	requireContains(t, apply, `"status":"applied"`)
	requireFactCount(t, dbPath, 1)
	requireAuditRows(t, dbPath, 1)
}

func TestRunExtractRunOpenAIProviderMissingKeyIsSanitized(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)

	stdout, stderr, code := runCLI(
		"extract-run",
		"--db", dbPath,
		"--session", "session_seed",
		"--provider", "openai-compatible",
		"--base-url", "https://example.invalid",
		"--api-key-env", "MEMORYCORE_LLM_API_KEY_FOR_TEST",
		"--model", "test-model",
		"--mode", "dry-run",
		"--format", "json",
	)
	if code == 0 {
		t.Fatalf("missing key exit code = 0; stdout=%q stderr=%q", stdout, stderr)
	}
	requireContains(t, stderr, "MEMORYCORE_LLM_API_KEY_FOR_TEST")
	requireNotContains(t, stderr, "Bearer")
	requireNotContains(t, stderr, "api_key")
}

func TestRunExtractBatchMockSkipsSuccessfulFingerprintAndForceReruns(t *testing.T) {
	dbPath := seedCLIBatchDB(t)

	first := requireRunText(t,
		"extract-batch",
		"--db", dbPath,
		"--provider", "mock",
		"--mode", "dry-run",
		"--limit", "10",
		"--format", "json",
	)
	requireContains(t, first, `"processed_count":2`)
	requireContains(t, first, `"skipped_count":0`)

	second := requireRunText(t,
		"extract-batch",
		"--db", dbPath,
		"--provider", "mock",
		"--mode", "dry-run",
		"--limit", "10",
		"--format", "json",
	)
	requireContains(t, second, `"processed_count":0`)
	requireContains(t, second, `"skipped_count":2`)

	forced := requireRunText(t,
		"extract-batch",
		"--db", dbPath,
		"--provider", "mock",
		"--mode", "dry-run",
		"--limit", "10",
		"--force",
		"--format", "json",
	)
	requireContains(t, forced, `"processed_count":2`)
}

func seedCLIBatchDB(t *testing.T) string {
	t.Helper()
	dbPath := seedCLIConsolidationDB(t)
	requireRunOK(t, "start-session", "--db", dbPath, "--id", "session_two", "--format", "id")
	requireRunOK(t, "append-episode", "--db", dbPath, "--id", "ep_two", "--session", "session_two", "--content", "我喜欢手冲咖啡。", "--format", "id")
	return dbPath
}

func requireFactCount(t *testing.T, dbPath string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&got); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if got != want {
		t.Fatalf("fact count = %d, want %d", got, want)
	}
}

func requireAuditRows(t *testing.T, dbPath string, wantAtLeast int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM extraction_runs`).Scan(&got); err != nil {
		t.Fatalf("count extraction_runs: %v", err)
	}
	if got < wantAtLeast {
		t.Fatalf("audit rows = %d, want at least %d", got, wantAtLeast)
	}
}
