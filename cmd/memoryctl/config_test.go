package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
)

func TestRunValidateConfigSucceedsForYAML(t *testing.T) {
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: ./memory.db
retrieval:
  context_budget_tokens: 900
`)

	stdout, stderr, code := runCLI("validate-config", "--config", configPath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "config ok" {
		t.Fatalf("stdout = %q, want config ok", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestRunValidateConfigReportsFieldPathError(t *testing.T) {
	configPath := writeCLIConfigFile(t, "memory.yaml", `
enabled: true
core:
  db_path: ./memory.db
retrieval:
  final_memory_count: 0
`)

	_, stderr, code := runCLI("validate-config", "--config", configPath)
	if code == 0 {
		t.Fatalf("exit code = 0, want failure")
	}
	requireContains(t, stderr, "retrieval.final_memory_count")
}

func TestRunValidateConfigJSONOutput(t *testing.T) {
	configPath := writeCLIConfigFile(t, "memory.json", `{"enabled":true,"core":{"db_path":"./memory.db"}}`)

	stdout, stderr, code := runCLI("validate-config", "--config", configPath, "--format", "json", "--check-env")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout)
	}
	if decoded["status"] != "ok" {
		t.Fatalf("status = %v, want ok", decoded["status"])
	}
}

func TestRunValidateConfigSucceedsForExampleConfig(t *testing.T) {
	examplePath := filepath.Join("..", "..", "examples", "config", "memorycore.yaml")
	stdout, stderr, code := runCLI("validate-config", "--config", examplePath)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) != "config ok" {
		t.Fatalf("stdout = %q, want config ok", stdout)
	}
}

func TestRunConfigDocsMarkdownAndJSON(t *testing.T) {
	stdout, stderr, code := runCLI("config-docs", "--format", "markdown")
	if code != 0 {
		t.Fatalf("markdown exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	requireContains(t, stdout, "core.db_path")
	requireContains(t, stdout, "retrieval.context_budget_tokens")
	requireContains(t, stdout, "sidecar.url")

	stdout, stderr, code = runCLI("config-docs", "--format", "json")
	if code != 0 {
		t.Fatalf("json exit code = %d, want 0; stdout=%q stderr=%q", code, stdout, stderr)
	}
	var fields []memconfig.FieldDescriptor
	if err := json.Unmarshal([]byte(stdout), &fields); err != nil {
		t.Fatalf("decode docs json: %v\n%s", err, stdout)
	}
	if len(fields) == 0 {
		t.Fatal("docs json returned no fields")
	}
}

func writeCLIConfigFile(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
