package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunMirrorSyncWithSidecarURL(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	factID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "咖啡",
		"--summary", "用户喜欢咖啡。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mirror/operation" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			OperationID string `json:"operation_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version":  "memory_mirror_operation_result.v0.1",
			"operation_id":    request.OperationID,
			"status":          "ok",
			"trivium_node_id": 12345,
		})
	}))
	defer server.Close()

	out := requireRunOK(t, "mirror-sync-run", "--db", dbPath, "--limit", "10", "--sidecar-url", server.URL, "--format", "text")
	want := "claimed=3\ncompleted=3\nfailed=0\nskipped=1\n"
	if out != want {
		t.Fatalf("mirror sync output = %q, want %q", out, want)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var triviumID int64
	if err := db.QueryRow(`
SELECT trivium_node_id
FROM memory_index_map
WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&triviumID); err != nil {
		t.Fatalf("query mirror index map: %v", err)
	}
	if triviumID != 12345 {
		t.Fatalf("trivium_node_id = %d, want 12345", triviumID)
	}
}

func TestRunMirrorSyncRejectsMissingMirrorAdapterChoice(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	_, stderr, code := runCLI("mirror-sync-run", "--db", dbPath)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--fake-adapter or --sidecar-url is required")
}

func TestRunMirrorSyncRejectsBothFakeAdapterAndSidecarURL(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	_, stderr, code := runCLI("mirror-sync-run", "--db", dbPath, "--fake-adapter", "--sidecar-url", "http://127.0.0.1:1")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "choose only one")
}

func TestRunMirrorSyncRejectsRemoteSidecarURL(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	_, stderr, code := runCLI("mirror-sync-run", "--db", dbPath, "--sidecar-url", "https://example.com")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "loopback")
}

func TestRunMirrorSyncSidecarFailureReturnsErrorAndLeavesRowsRetryable(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "咖啡",
		"--summary", "用户喜欢咖啡。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sidecar unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, stderr, code := runCLI("mirror-sync-run", "--db", dbPath, "--limit", "10", "--sidecar-url", server.URL)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "mirror sync failed rows")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var failedRows int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM index_sync_queue
WHERE status = 'failed' AND attempts = 1`).Scan(&failedRows); err != nil {
		t.Fatalf("query failed queue rows: %v", err)
	}
	if failedRows == 0 {
		t.Fatalf("failed queue rows = 0, want retryable failed rows")
	}
}
