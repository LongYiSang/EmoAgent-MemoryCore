package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunRetrieveWithMirrorSidecarURL(t *testing.T) {
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
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
INSERT INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES ('map_cli_mirror', 'default', 'fact', ?, 5001, 'indexed')`, factID); err != nil {
		t.Fatalf("insert mirror map: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/candidates" {
			http.NotFound(w, r)
			return
		}
		var request struct {
			RequestID string `json:"request_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_mirror_candidates.v0.1",
			"request_id":     request.RequestID,
			"candidates": []map[string]any{
				{"trivium_node_id": 5001, "score": 0.88, "source": "fake_sparse"},
			},
			"degraded": false,
		})
	}))
	defer server.Close()

	out := requireRunOK(t, "retrieve", "--db", dbPath, "--query", "espresso-only", "--use-mirror", "--sidecar-url", server.URL)
	requireContains(t, out, factID)
	requireContains(t, out, "用户喜欢咖啡。")
}

func TestRunRetrieveUseMirrorRequiresSidecarURL(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	_, stderr, code := runCLI("retrieve", "--db", dbPath, "--query", "咖啡", "--use-mirror")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--sidecar-url is required when --use-mirror is set")
}
