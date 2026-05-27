package main

import (
	"context"
	"database/sql"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestRunDebugCleanupDefaultsDryRunAndRequiresConfirmForExecute(t *testing.T) {
	dbPath := seedCLIDebugCleanupDB(t)

	dryRun := requireRunOK(t, "debug-cleanup", "--db", dbPath, "--format", "text")
	requireContains(t, dryRun, "dry_run=true")
	requireContains(t, dryRun, "scope=auto-extraction")
	requireContains(t, dryRun, "facts=1")
	requireContains(t, dryRun, "dev/debug cleanup only")
	requireDebugCleanupCount(t, dbPath, "facts", 1)

	_, stderr, code := runCLI("debug-cleanup", "--db", dbPath, "--execute")
	if code != 2 {
		t.Fatalf("execute without confirm code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--execute requires --profile dev or test")
	requireDebugCleanupCount(t, dbPath, "facts", 1)

	_, stderr, code = runCLI("debug-cleanup", "--db", dbPath, "--execute", "--profile", "dev")
	if code != 2 {
		t.Fatalf("execute without confirm code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--execute requires --confirm")
	requireDebugCleanupCount(t, dbPath, "facts", 1)

	futureDryRun := requireRunOK(t, "debug-cleanup", "--db", dbPath, "--since", "2030-01-01T00:00:00Z", "--format", "text")
	requireContains(t, futureDryRun, "facts=0")
	requireDebugCleanupCount(t, dbPath, "facts", 1)

	executed := requireRunOK(t, "debug-cleanup", "--db", dbPath, "--profile", "dev", "--execute", "--confirm", debugCleanupConfirm, "--format", "text")
	requireContains(t, executed, "dry_run=false")
	for _, table := range []string{
		"facts",
		"memory_links",
		"memory_search_documents",
		"memory_search_fts",
		"memory_index_map",
		"extraction_runs",
		"consolidation_apply_fingerprints",
		"consolidation_session_fact_writes",
		"memory_access_events",
	} {
		requireDebugCleanupCount(t, dbPath, table, 0)
	}
	requireDebugCleanupCount(t, dbPath, "index_sync_queue", 2)
	requireDebugCleanupIndexQueueCount(t, dbPath, "fact", 0)
	requireDebugCleanupCount(t, dbPath, "deletion_events", 0)
	requireDebugCleanupCount(t, dbPath, "episodes", 1)
	requireDebugCleanupCount(t, dbPath, "entities", 1)
}

func TestRunDebugCleanupAutoExtractionSinceDoesNotDeleteOlderSameSessionFacts(t *testing.T) {
	dbPath := seedCLIDebugCleanupDB(t)
	oldFactID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--session", "session_seed",
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "红茶",
		"--summary", "用户喜欢红茶。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)
	db, err := memsqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if _, err := db.SQLDB().Exec(`
INSERT INTO extraction_runs(id, request_id, persona_id, session_id, trigger, mode, status, fingerprint, created_at, updated_at)
VALUES ('run_old', 'req_old', 'default', 'session_seed', 'session_end', 'apply', 'applied', 'fp_old', '2026-05-01T12:00:00Z', '2026-05-01T12:00:00Z');
UPDATE consolidation_apply_fingerprints
SET request_id = 'req_old', candidate_id = 'fact_old'
WHERE persona_id = 'default' AND fact_id = ?;`, oldFactID); err != nil {
		db.Close()
		t.Fatalf("seed old run: %v", err)
	}
	db.Close()

	requireRunOK(t, "debug-cleanup", "--db", dbPath, "--since", "2026-05-05T00:00:00Z", "--profile", "test", "--execute", "--confirm", debugCleanupConfirm, "--format", "text")
	requireDebugCleanupCount(t, dbPath, "facts", 1)
	requireDebugCleanupCount(t, dbPath, "extraction_runs", 1)
	requireDebugCleanupCount(t, dbPath, "consolidation_apply_fingerprints", 1)
	requireDebugCleanupCount(t, dbPath, "consolidation_session_fact_writes", 1)
}

func TestRunDebugCleanupAutoExtractionKeepsManualFacts(t *testing.T) {
	dbPath := seedCLIDebugCleanupDB(t)
	manualFactID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--session", "session_seed",
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "手写记忆",
		"--summary", "用户手工写入的记忆。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)

	requireRunOK(t, "debug-cleanup", "--db", dbPath, "--profile", "dev", "--execute", "--confirm", debugCleanupConfirm, "--format", "text")
	requireDebugCleanupCount(t, dbPath, "facts", 1)
	requireDebugCleanupFactExists(t, dbPath, manualFactID)
}

func TestRunDebugCleanupPersonaAndAllDevScopesRequireDevProfile(t *testing.T) {
	dbPath := seedCLIDebugCleanupDB(t)

	dryRun := requireRunOK(t, "debug-cleanup", "--db", dbPath, "--scope", "persona", "--format", "text")
	requireContains(t, dryRun, "scope=persona")
	requireContains(t, dryRun, "dry_run=true")
	requireContains(t, dryRun, "facts=1")
	requireDebugCleanupCount(t, dbPath, "facts", 1)

	_, stderr, code := runCLI("debug-cleanup", "--db", dbPath, "--scope", "persona", "--profile", "prod", "--execute", "--confirm", debugCleanupConfirm)
	if code != 2 {
		t.Fatalf("persona execute with prod profile code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--execute requires --profile dev or test")
	requireDebugCleanupCount(t, dbPath, "facts", 1)

	requireRunOK(t, "debug-cleanup", "--db", dbPath, "--scope", "persona", "--profile", "test", "--execute", "--confirm", debugCleanupConfirm, "--format", "text")
	requireDebugCleanupCount(t, dbPath, "facts", 0)
	requireDebugCleanupCount(t, dbPath, "index_sync_queue", 0)
	requireDebugCleanupCount(t, dbPath, "episodes", 1)
	requireDebugCleanupCount(t, dbPath, "entities", 1)

	allDevDBPath := seedCLIDebugCleanupDB(t)
	allDev := requireRunOK(t, "debug-cleanup", "--db", allDevDBPath, "--scope", "all-dev", "--profile", "dev", "--execute", "--confirm", debugCleanupConfirm, "--format", "text")
	requireContains(t, allDev, "scope=all-dev")
	requireContains(t, allDev, "dry_run=false")
	requireDebugCleanupCount(t, allDevDBPath, "facts", 0)
	requireDebugCleanupCount(t, allDevDBPath, "memory_links", 0)
	requireDebugCleanupCount(t, allDevDBPath, "memory_search_documents", 0)
	requireDebugCleanupCount(t, allDevDBPath, "memory_search_fts", 0)
	requireDebugCleanupCount(t, allDevDBPath, "index_sync_queue", 0)
	requireDebugCleanupCount(t, allDevDBPath, "extraction_runs", 0)
	requireDebugCleanupCount(t, allDevDBPath, "episodes", 1)
	requireDebugCleanupCount(t, allDevDBPath, "entities", 1)
}

func seedCLIDebugCleanupDB(t *testing.T) string {
	t.Helper()
	dbPath := seedCLIConsolidationDB(t)
	factID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--session", "session_seed",
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "手冲咖啡",
		"--summary", "用户喜欢手冲咖啡。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)
	db, err := memsqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.SQLDB().Exec(`
INSERT INTO memory_search_fts(search_text, persona_id, node_type, node_id)
VALUES ('用户喜欢手冲咖啡。', 'default', 'fact', ?);
INSERT INTO memory_index_map(id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES ('map_debug_fact', 'default', 'fact', ?, 99, 'indexed');
INSERT INTO extraction_runs(id, request_id, persona_id, session_id, trigger, mode, status, fingerprint, created_at, updated_at)
VALUES ('run_debug', 'req_debug', 'default', 'session_seed', 'session_end', 'apply', 'applied', 'fp_debug', '2026-05-10T12:00:00Z', '2026-05-10T12:00:00Z');
INSERT INTO memory_access_events(id, persona_id, session_id, node_type, node_id)
VALUES ('access_debug', 'default', 'session_seed', 'fact', ?);
UPDATE consolidation_apply_fingerprints
SET request_id = 'req_debug', candidate_id = 'fact_debug'
WHERE persona_id = 'default' AND fact_id = ?;
INSERT OR IGNORE INTO index_sync_queue(id, persona_id, node_type, node_id, operation)
VALUES ('queue_keep_entity', 'default', 'entity', 'ent_user', 'upsert_node');`,
		factID,
		factID,
		factID,
		factID,
	); err != nil {
		t.Fatalf("seed debug cleanup tables: %v", err)
	}
	return dbPath
}

func requireDebugCleanupCount(t *testing.T, dbPath string, table string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", table, got, want)
	}
}

func requireDebugCleanupIndexQueueCount(t *testing.T, dbPath string, nodeType string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM index_sync_queue WHERE node_type = ?", nodeType).Scan(&got); err != nil {
		t.Fatalf("count index_sync_queue %s: %v", nodeType, err)
	}
	if got != want {
		t.Fatalf("index_sync_queue %s count = %d, want %d", nodeType, got, want)
	}
}

func requireDebugCleanupFactExists(t *testing.T, dbPath string, factID string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM facts WHERE id = ?", factID).Scan(&got); err != nil {
		t.Fatalf("count fact %s: %v", factID, err)
	}
	if got != 1 {
		t.Fatalf("fact %s count = %d, want 1", factID, got)
	}
}
