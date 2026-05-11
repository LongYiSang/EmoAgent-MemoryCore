package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunCompressionApplyFileDrivenSuccess(t *testing.T) {
	dbPath, firstFactID, secondFactID := seedCLICompressionFacts(t)
	requestPath := writeCompressionRequestFile(t, firstFactID, secondFactID, `
{
  "source_fact_ids": [%q, %q],
  "narrative": {
    "id": "narrative_cli",
    "scope": "topic",
    "scope_ref": "stress_support",
    "summary": "用户在压力场景中更需要先被情绪承接。",
    "emotional_tone": "stressed",
    "importance": 0.72,
    "sensitivity_level": "normal"
  },
  "insights": [
    {
      "id": "insight_cli",
      "insight_type": "coping_strategy",
      "content": "压力场景中先共情，再提供建议。",
      "confidence": 0.82,
      "importance": 0.76,
      "valence": 0.1,
      "arousal": 0.3,
      "sensitivity_level": "normal"
    }
  ]
}`)

	out := requireRunOK(t, "compression-apply", "--db", dbPath, "--request", requestPath, "--now", "2026-05-12T10:30:00Z", "--format", "text")
	want := "narrative_id=narrative_cli\n" +
		"insight_ids=insight_cli\n" +
		"source_facts_consolidated=2\n" +
		"derived_links=4\n" +
		"search_documents_synced=4\n" +
		"mirror_updates_enqueued=0\n"
	if out != want {
		t.Fatalf("compression output = %q, want %q", out, want)
	}
	requireCompressionCLIState(t, dbPath, firstFactID, secondFactID)
}

func TestRunCompressionApplyValidationError(t *testing.T) {
	dbPath, firstFactID, _ := seedCLICompressionFacts(t)
	requestPath := writeCompressionRequestFile(t, firstFactID, "", `
{
  "source_fact_ids": [%q],
  "narrative": {
    "scope": "topic",
    "summary": "用户在压力场景中更需要先被情绪承接。",
    "importance": 0.72,
    "sensitivity_level": "normal"
  }
}`)

	_, stderr, code := runCLI("compression-apply", "--db", dbPath, "--request", requestPath, "--format", "text")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "at least two unique")
}

func seedCLICompressionFacts(t *testing.T) (string, string, string) {
	t.Helper()

	dbPath := seedCLIConsolidationDB(t)
	first := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "先理解",
		"--summary", "用户压力大时希望先被理解。",
		"--source-episode", "ep_seed",
		"--confidence", "explicit",
		"--format", "id",
	)
	second := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "不要直接建议",
		"--summary", "用户压力大时不喜欢直接给建议。",
		"--source-episode", "ep_seed",
		"--confidence", "explicit",
		"--format", "id",
	)
	return dbPath, first, second
}

func writeCompressionRequestFile(t *testing.T, firstFactID string, secondFactID string, format string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "compression_request.json")
	content := format
	if secondFactID == "" {
		content = fmt.Sprintf(format, firstFactID)
	} else {
		content = fmt.Sprintf(format, firstFactID, secondFactID)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write compression request: %v", err)
	}
	return path
}

func requireCompressionCLIState(t *testing.T, dbPath string, firstFactID string, secondFactID string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	for _, factID := range []string{firstFactID, secondFactID} {
		var lifecycle string
		if err := db.QueryRow(`SELECT lifecycle_status FROM facts WHERE id = ?`, factID).Scan(&lifecycle); err != nil {
			t.Fatalf("query fact lifecycle: %v", err)
		}
		if lifecycle != "consolidated" {
			t.Fatalf("fact %s lifecycle = %q, want consolidated", factID, lifecycle)
		}
	}
	var narrativeSummary string
	if err := db.QueryRow(`SELECT summary FROM narratives WHERE id = 'narrative_cli'`).Scan(&narrativeSummary); err != nil {
		t.Fatalf("query narrative: %v", err)
	}
	if narrativeSummary != "用户在压力场景中更需要先被情绪承接。" {
		t.Fatalf("narrative summary = %q", narrativeSummary)
	}
	var insightContent string
	if err := db.QueryRow(`SELECT content FROM insights WHERE id = 'insight_cli'`).Scan(&insightContent); err != nil {
		t.Fatalf("query insight: %v", err)
	}
	if insightContent != "压力场景中先共情，再提供建议。" {
		t.Fatalf("insight content = %q", insightContent)
	}
}
