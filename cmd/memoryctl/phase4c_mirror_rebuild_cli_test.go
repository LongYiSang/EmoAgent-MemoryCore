package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunMirrorRebuildWithSidecarURL(t *testing.T) {
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
	clearCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mirror/clear-namespace":
			clearCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schema_version": "memory_mirror_clear_namespace_result.v0.1",
				"status":         "ok",
			})
		case "/mirror/operation":
			var request struct {
				OperationID string `json:"operation_id"`
				Node        struct {
					SQLiteNodeID string `json:"sqlite_node_id"`
				} `json:"node"`
			}
			_ = json.NewDecoder(r.Body).Decode(&request)
			triviumID := int64(12345 + len(request.Node.SQLiteNodeID))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schema_version":  "memory_mirror_operation_result.v0.1",
				"operation_id":    request.OperationID,
				"status":          "ok",
				"trivium_node_id": triviumID,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out := requireRunOK(t, "mirror-rebuild", "--db", dbPath, "--sidecar-url", server.URL, "--format", "text")
	requireContains(t, out, "nodes_upserted=2")
	requireContains(t, out, "edges_upserted=1")
	requireContains(t, out, "failed=0")
	if !clearCalled {
		t.Fatalf("sidecar clear namespace was not called")
	}
}

func TestRunMirrorRebuildRequiresSidecarURL(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	_, stderr, code := runCLI("mirror-rebuild", "--db", dbPath)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--sidecar-url is required")
}
