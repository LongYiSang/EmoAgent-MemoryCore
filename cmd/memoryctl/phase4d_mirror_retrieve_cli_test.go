package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	requireContains(t, out, "mirror_status=used")
	requireContains(t, out, "mirror_candidates sidecar=1 mapped=1 dropped=0")

	jsonOut := requireRunText(t, "retrieve", "--db", dbPath, "--query", "espresso-only", "--use-mirror", "--sidecar-url", server.URL, "--format", "json")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &decoded); err != nil {
		t.Fatalf("decode json: %v\n%s", err, jsonOut)
	}
	mirror, ok := decoded["Mirror"].(map[string]any)
	if !ok {
		t.Fatalf("json mirror missing: %v", decoded)
	}
	if mirror["status"] != "used" {
		t.Fatalf("mirror status = %v, want used", mirror["status"])
	}
	mirrorJSON, err := json.Marshal(mirror)
	if err != nil {
		t.Fatalf("marshal mirror: %v", err)
	}
	mirrorText := string(mirrorJSON)
	if strings.Contains(mirrorText, "query_text") || strings.Contains(mirrorText, "summary") || strings.Contains(mirrorText, "search_text") {
		t.Fatalf("mirror diagnostics leaked payload fields: %s", mirrorText)
	}
}

func TestRunRetrieveUseMirrorRequiresSidecarURL(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	_, stderr, code := runCLI("retrieve", "--db", dbPath, "--query", "咖啡", "--use-mirror")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--sidecar-url is required when --use-mirror is set")
}
