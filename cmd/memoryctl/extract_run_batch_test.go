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

func TestRunExtractOpenAIProviderValidationIsEarlyAndSanitized(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	secret := "SECRET_KEY_VALUE_SHOULD_NOT_LEAK"
	t.Setenv("MEMORYCORE_OPENAI_VALIDATION_KEY", secret)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing base url",
			args: []string{"extract-run", "--db", dbPath, "--session", "session_seed", "--provider", "openai-compatible", "--model", "test-model", "--api-key-env", "MEMORYCORE_OPENAI_VALIDATION_KEY"},
			want: "--base-url is required",
		},
		{
			name: "missing model",
			args: []string{"extract-run", "--db", dbPath, "--session", "session_seed", "--provider", "openai-compatible", "--base-url", "https://example.invalid", "--api-key-env", "MEMORYCORE_OPENAI_VALIDATION_KEY"},
			want: "--model is required",
		},
		{
			name: "empty api key env",
			args: []string{"extract-run", "--db", dbPath, "--session", "session_seed", "--provider", "openai-compatible", "--base-url", "https://example.invalid", "--model", "test-model", "--api-key-env", ""},
			want: "--api-key-env is required",
		},
		{
			name: "unset api key env",
			args: []string{"extract-run", "--db", dbPath, "--session", "session_seed", "--provider", "openai-compatible", "--base-url", "https://example.invalid", "--model", "test-model", "--api-key-env", "MEMORYCORE_OPENAI_UNSET_KEY"},
			want: "api key env MEMORYCORE_OPENAI_UNSET_KEY is not set",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCLI(tc.args...)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
			}
			requireContains(t, stderr, tc.want)
			requireNotContains(t, stderr, secret)
		})
	}
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

func TestRunExtractBatchSessionAndEpisodeLimits(t *testing.T) {
	dbPath := seedCLIBatchDB(t)
	requireRunOK(t, "append-episode", "--db", dbPath, "--id", "ep_seed_extra", "--session", "session_seed", "--content", "我也喜欢手冲咖啡。", "--format", "id")

	oneSession := requireRunText(t,
		"extract-batch",
		"--db", dbPath,
		"--provider", "mock",
		"--mode", "dry-run",
		"--session-limit", "1",
		"--episode-limit", "1",
		"--audit", "off",
		"--format", "json",
	)
	requireContains(t, oneSession, `"processed_count":1`)
	requireContains(t, oneSession, `"original_episode_count":1`)

	legacyLimit := requireRunText(t,
		"extract-batch",
		"--db", dbPath,
		"--provider", "mock",
		"--mode", "dry-run",
		"--limit", "1",
		"--audit", "off",
		"--format", "json",
	)
	requireContains(t, legacyLimit, `"processed_count":1`)
}

func TestRunExtractBatchPartialFailureExitCode(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)

	stdout, stderr, code := runCLI(
		"extract-batch",
		"--db", dbPath,
		"--session", "missing_session",
		"--provider", "mock",
		"--mode", "dry-run",
		"--format", "json",
	)
	if code != 1 {
		t.Fatalf("partial failure exit code = %d, want 1; stdout=%q stderr=%q", code, stdout, stderr)
	}
	requireContains(t, stdout, `"status":"partial_failure"`)

	stdout, stderr, code = runCLI(
		"extract-batch",
		"--db", dbPath,
		"--session", "missing_session",
		"--provider", "mock",
		"--mode", "dry-run",
		"--allow-partial-failure",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("allowed partial failure exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	requireContains(t, stdout, `"status":"partial_failure"`)
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
