package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunRetrieveUsesConfigEvenWhenDisabled(t *testing.T) {
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
	requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "拿铁",
		"--summary", "用户喜欢拿铁咖啡。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: false
core:
  db_path: `+yamlSingleQuote(dbPath)+`
retrieval:
  final_memory_count: 1
  context_budget_tokens: 1200
`)

	stdout, stderr, code := runCLI("retrieve", "--config", configPath, "--query", "咖啡")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if got := strings.Count(stdout, "- fact "); got != 1 {
		t.Fatalf("retrieved fact count = %d, want 1\n%s", got, stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestRunRetrieveFlagOverridesConfigAndWarns(t *testing.T) {
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
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: `+yamlSingleQuote(dbPath)+`
retrieval:
  final_memory_count: 1
  context_budget_tokens: 1
`)

	stdout, stderr, code := runCLI("retrieve", "--config", configPath, "--query", "咖啡", "--budget", "1200")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	requireContains(t, stdout, "用户喜欢咖啡。")
	requireContains(t, stderr, "warning: --budget overrides memory.retrieval.context_budget_tokens from config")
}

func TestRunRetrieveConfigFakeAdapterIsNotOverriddenByConfiguredURL(t *testing.T) {
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
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "fake config should not call sidecar", http.StatusInternalServerError)
	}))
	defer server.Close()
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: `+yamlSingleQuote(dbPath)+`
retrieval:
  use_mirror: true
sidecar:
  enabled: true
  url: `+yamlSingleQuote(server.URL)+`
  adapter: fake
`)

	stdout, stderr, code := runCLI("retrieve", "--config", configPath, "--query", "咖啡")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	requireContains(t, stdout, "mirror_status=adapter_missing")
	if called {
		t.Fatal("configured sidecar URL was called even though sidecar.adapter=fake")
	}
}

func TestRunMirrorSyncUsesConfigLimitAndFakeAdapter(t *testing.T) {
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
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: `+yamlSingleQuote(dbPath)+`
sidecar:
  enabled: true
  adapter: fake
mirror:
  sync_limit: 1
`)

	out := requireRunOK(t, "mirror-sync-run", "--config", configPath, "--format", "text")
	requireContains(t, out, "claimed=1")
}

func TestRunMirrorSyncConfigFakeAdapterIgnoresConfiguredURL(t *testing.T) {
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
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "fake config should not call sidecar", http.StatusInternalServerError)
	}))
	defer server.Close()
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: `+yamlSingleQuote(dbPath)+`
sidecar:
  enabled: true
  url: `+yamlSingleQuote(server.URL)+`
  adapter: fake
mirror:
  sync_limit: 1
`)

	out := requireRunOK(t, "mirror-sync-run", "--config", configPath, "--format", "text")
	requireContains(t, out, "claimed=1")
	if called {
		t.Fatal("configured sidecar URL was called even though sidecar.adapter=fake")
	}
}

func TestRunMirrorRebuildUsesConfigSidecarURL(t *testing.T) {
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
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schema_version":  "memory_mirror_operation_result.v0.1",
				"operation_id":    request.OperationID,
				"status":          "ok",
				"trivium_node_id": int64(12345 + len(request.Node.SQLiteNodeID)),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: `+yamlSingleQuote(dbPath)+`
sidecar:
  enabled: true
  url: `+yamlSingleQuote(server.URL)+`
  adapter: trivium
`)

	out := requireRunOK(t, "mirror-rebuild", "--config", configPath, "--format", "text")
	requireContains(t, out, "failed=0")
	if !clearCalled {
		t.Fatalf("sidecar clear namespace was not called")
	}
}

func TestRunMirrorRebuildIgnoresConfigURLWhenSidecarDisabled(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		http.Error(w, "disabled sidecar config should not be used", http.StatusInternalServerError)
	}))
	defer server.Close()
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: `+yamlSingleQuote(dbPath)+`
sidecar:
  enabled: false
  url: `+yamlSingleQuote(server.URL)+`
  adapter: trivium
`)

	_, stderr, code := runCLI("mirror-rebuild", "--config", configPath)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--sidecar-url is required")
	if called {
		t.Fatal("configured sidecar URL was called even though sidecar.enabled=false")
	}
}

func TestRunRetentionJobsUsesConfigJobsAndDeepArchiveThreshold(t *testing.T) {
	dbPath, expiringFactID := seedCLIRetentionFact(t, "retention_config_expiring", "2026-05-01T00:00:00Z")
	_, oldArchivedFactID := seedCLIRetentionFactInDB(t, dbPath, "retention_config_old_archived", "")
	setCLIFactArchivedAt(t, dbPath, oldArchivedFactID, "2026-01-01T00:00:00Z")
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: `+yamlSingleQuote(dbPath)+`
retention:
  jobs:
    - daily_ttl_expiry
    - monthly_deep_archive
  deep_archive_after_days: 180
`)

	out := requireRunOK(t, "retention-jobs-run", "--config", configPath, "--now", "2026-08-01T00:00:00Z", "--format", "text")
	requireContains(t, out, "jobs=daily_ttl_expiry,monthly_deep_archive")
	requireFactCLIState(t, dbPath, expiringFactID, "invalidated", "archived")
	requireFactCLIState(t, dbPath, oldArchivedFactID, "valid", "deep_archived")
}

func TestRunRetentionJobsExplicitJobsOverrideInvalidConfigJobs(t *testing.T) {
	dbPath, expiringFactID := seedCLIRetentionFact(t, "retention_config_override_invalid", "2026-05-01T00:00:00Z")
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: `+yamlSingleQuote(dbPath)+`
retention:
  jobs:
    - monthly_deep_archive
  deep_archive_after_days: 0
`)

	out := requireRunOK(t, "retention-jobs-run", "--config", configPath, "--jobs", "daily_ttl_expiry", "--now", "2026-08-01T00:00:00Z", "--format", "text")
	requireContains(t, out, "jobs=daily_ttl_expiry")
	requireFactCLIState(t, dbPath, expiringFactID, "invalidated", "archived")
}

func yamlSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
