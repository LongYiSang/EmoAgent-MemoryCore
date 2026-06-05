package main

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"

	_ "modernc.org/sqlite"
)

func TestNaturalMemoryRunCLIDryRunJSON(t *testing.T) {
	configPath := filepath.Join("..", "..", "examples", "config", "memorycore.yaml")
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	stdout, stderr, code := runCLI(
		"natural-memory-run",
		"--config", configPath,
		"--db", dbPath,
		"--mode", "manual",
		"--dry-run",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if decoded["run_kind"] != "manual" || decoded["dry_run"] != true {
		t.Fatalf("decoded = %#v, want manual dry-run", decoded)
	}
}

func TestNaturalMemoryRunCLIDefaultsToManual(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)
	seedCLINaturalFact(t, dbPath, "fact_cli_default_manual")

	stdout, stderr, code := runCLI(
		"natural-memory-run",
		"--db", dbPath,
		"--now", "2026-06-05T03:31:00+08:00",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if decoded["run_kind"] != "manual" {
		t.Fatalf("run_kind = %v, want manual", decoded["run_kind"])
	}
}

func TestNaturalMemoryRunCLISleepCycleUsesDueCheck(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)
	seedCLINaturalFact(t, dbPath, "fact_cli_sleep_due")

	stdout, stderr, code := runCLI(
		"natural-memory-run",
		"--db", dbPath,
		"--mode", "sleep_cycle",
		"--now", "2026-06-05T02:00:00+08:00",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if decoded["status"] != "skipped" || decoded["run_kind"] != "sleep_cycle" {
		t.Fatalf("decoded = %#v, want skipped sleep_cycle", decoded)
	}
	requireCLINaturalTableCount(t, dbPath, "memory_natural_runs", 0)
}

func TestNaturalMemoryRunCLISleepCycleDryRunDoesNotWrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)
	seedCLINaturalFact(t, dbPath, "fact_cli_sleep_dry_run")

	stdout, stderr, code := runCLI(
		"natural-memory-run",
		"--db", dbPath,
		"--mode", "sleep_cycle",
		"--now", "2026-06-05T03:31:00+08:00",
		"--dry-run",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if decoded["dry_run"] != true || decoded["status"] != "completed" {
		t.Fatalf("decoded = %#v, want completed dry-run", decoded)
	}
	requireCLINaturalTableCount(t, dbPath, "memory_natural_runs", 0)
}

func TestNaturalMemoryRunCLIMaxWritesCapsWrites(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	requireRunOK(t, "init-db", "--db", dbPath)
	seedCLINaturalFact(t, dbPath, "fact_cli_write_1")
	seedCLINaturalFact(t, dbPath, "fact_cli_write_2")

	stdout, stderr, code := runCLI(
		"natural-memory-run",
		"--db", dbPath,
		"--mode", "manual",
		"--now", "2026-06-05T03:31:00+08:00",
		"--max-writes", "1",
		"--format", "json",
	)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode json: %v\n%s", err, stdout)
	}
	if decoded["evaluated_nodes"] != float64(2) || decoded["search_tier_updates"] != float64(1) {
		t.Fatalf("decoded = %#v, want two evaluated nodes and one tier update", decoded)
	}
	requireCLINaturalTableCount(t, dbPath, "memory_natural_states", 1)
}

func seedCLINaturalFact(t *testing.T, dbPath string, factID string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	created := time.Date(2026, 4, 21, 3, 31, 0, 0, time.FixedZone("CST", 8*60*60)).Format(time.RFC3339Nano)
	if _, err := db.Exec(`
INSERT OR IGNORE INTO personas(id, display_name, created_at, updated_at)
VALUES ('default', 'Default', ?, ?)`, created, created); err != nil {
		t.Fatalf("insert persona: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO facts (
    id, persona_id, predicate, object_literal, content_summary, fact_type,
    ingested_at, extraction_confidence, extraction_confidence_score, importance,
    sensitivity_level, validity_status, visibility_status, lifecycle_status,
    pinned, access_count, reinforcement_count, searchable, created_at
) VALUES (?, 'default', 'likes', ?, ?, ?, ?, 'explicit', 0.8, 0.7,
          'normal', 'valid', 'visible', 'active', 0, 0, 0, 1, ?)`,
		factID, factID, "用户临时关注 CLI 写入上限。", string(core.FactTypeTransientContext), created, created); err != nil {
		t.Fatalf("insert fact %s: %v", factID, err)
	}
	if _, err := db.Exec(`
INSERT INTO memory_search_documents (
    id, persona_id, node_type, node_id, search_text, search_tier,
    visibility_status, sensitivity_level, lifecycle_status, searchable, updated_at
) VALUES (?, 'default', 'fact', ?, ?, 'hot', 'visible', 'normal', 'active', 1, ?)`,
		"search_"+factID, factID, "用户临时关注 CLI 写入上限。", created); err != nil {
		t.Fatalf("insert search document %s: %v", factID, err)
	}
}

func requireCLINaturalTableCount(t *testing.T, dbPath string, table string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}
