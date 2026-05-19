package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMatrixSQLiteProfile(t *testing.T) {
	root := t.TempDir()
	fixturePath := filepath.Join(root, "quality_case.yaml")
	if err := os.WriteFile(fixturePath, []byte(minimalCLIQualityFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--suite", "retrieval",
		"--mode", "matrix",
		"--profiles", "sqlite_go",
		"--quality-no-stub",
		"--root", root,
		"--temp-dir", t.TempDir(),
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	for _, want := range []string{
		"matrix_report",
		"profile: sqlite_go",
		"status: pass",
		"selected_recall_at_8: 1.000",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout =\n%s\nwant %q", stdout.String(), want)
		}
	}
}

func TestRunMatrixWritesDetailReport(t *testing.T) {
	root := t.TempDir()
	fixturePath := filepath.Join(root, "quality_case.yaml")
	if err := os.WriteFile(fixturePath, []byte(minimalCLIQualityFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	reportDir := filepath.Join(t.TempDir(), "reports")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--suite", "retrieval",
		"--mode", "matrix",
		"--profiles", "sqlite_go",
		"--quality-no-stub",
		"--root", root,
		"--temp-dir", t.TempDir(),
		"--report-dir", reportDir,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	detail, err := os.ReadFile(filepath.Join(reportDir, "detail.md"))
	if err != nil {
		t.Fatalf("read detail report: %v", err)
	}
	for _, want := range []string{
		"matrix_detail_report",
		"question_id: q1",
		"问题: 咖啡",
		"期望:",
		"profile: sqlite_go",
		"PASS [selected_recall_at_k] finds coffee",
	} {
		if !strings.Contains(string(detail), want) {
			t.Fatalf("detail report =\n%s\nwant %q", string(detail), want)
		}
	}
}

func TestRunMatrixWritesCombinedReportsForMultipleFixtures(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "case_a.yaml"), []byte(minimalCLIQualityFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	second := strings.ReplaceAll(minimalCLIQualityFixture(), "cli_matrix_sqlite", "cli_matrix_sqlite_second")
	if err := os.WriteFile(filepath.Join(root, "case_b.yaml"), []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	reportDir := filepath.Join(t.TempDir(), "reports")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--suite", "retrieval",
		"--mode", "matrix",
		"--profiles", "sqlite_go",
		"--quality-no-stub",
		"--root", root,
		"--temp-dir", t.TempDir(),
		"--report-dir", reportDir,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	for _, name := range []string{"report.md", "detail.md", "report.json"} {
		if _, err := os.Stat(filepath.Join(reportDir, name)); err != nil {
			t.Fatalf("expected combined %s: %v", name, err)
		}
	}
	detail, err := os.ReadFile(filepath.Join(reportDir, "detail.md"))
	if err != nil {
		t.Fatalf("read combined detail report: %v", err)
	}
	for _, want := range []string{
		"case_id: cli_matrix_sqlite",
		"case_id: cli_matrix_sqlite_second",
		"question_id: q1",
		"profile: sqlite_go",
	} {
		if !strings.Contains(string(detail), want) {
			t.Fatalf("combined detail report =\n%s\nwant %q", string(detail), want)
		}
	}
}

func TestRunControlledFixtureAllowsSemanticStubByDefault(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "testdata", "memory_eval", "controlled", "phase6", "QA001_semantic_fallback_diagnostics.yaml")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--fixture", fixturePath,
		"--mode", "brief",
		"--temp-dir", t.TempDir(),
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if strings.Contains(stderr.String(), "semantic_query_analysis_stub") {
		t.Fatalf("stderr = %q, want controlled semantic stub allowed", stderr.String())
	}
	if !strings.Contains(stdout.String(), "未发现失败结果") {
		t.Fatalf("stdout =\n%s\nwant passing brief report", stdout.String())
	}
}

func TestParseOptionsRejectsInvalidEmbeddingCacheMode(t *testing.T) {
	var stderr bytes.Buffer
	_, ok := parseOptions([]string{"--embedding-cache-mode", "typo"}, &stderr)

	if ok {
		t.Fatal("parseOptions accepted invalid embedding cache mode")
	}
	if !strings.Contains(stderr.String(), "embedding-cache-mode must be one of off, read_write, read_only, refresh") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func minimalCLIQualityFixture() string {
	return `
schema_version: memory_eval.v0.2
suite: quality_retrieval
quality_mode: true
allow_stub: false
case_id: cli_matrix_sqlite
seed:
  sessions:
    - id: s1
      channel: api
  entities:
    - id: user
      canonical_name: EvalUser
      entity_type: user
  episodes:
    - id: ep1
      session_id: s1
      role: user
      content: 用户喜欢咖啡。
      occurred_at: "2026-04-28T10:00:00+08:00"
steps:
  - id: f1
    action: fact
    fact:
      subject_entity_id: user
      predicate: likes
      object_literal: 咖啡
      content_summary: 用户喜欢咖啡。
      fact_type: stable_preference
      confidence: explicit
      confidence_score: 0.95
      importance: 0.9
      source_episode_ids: [ep1]
  - id: rebuild_search
    action: rebuild_search
    rebuild_search: {}
  - id: q1
    action: retrieve
    retrieve:
      query_text: 咖啡
      policy:
        final_memory_count: 4
assertions:
  - type: selected_recall_at_k
    name: finds coffee
    step: q1
    relevant_node_ids: [$f1.fact_id]
    at: 4
    min: 1.0
`
}
