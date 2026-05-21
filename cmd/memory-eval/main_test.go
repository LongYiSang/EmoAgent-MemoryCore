package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		"test_plan_version: memory_eval_matrix.v0.2",
		"profile: sqlite_go",
		"status: pass",
		"selected_recall_at_8: 1.000",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout =\n%s\nwant %q", stdout.String(), want)
		}
	}
	for _, want := range []string{
		"field_accuracy_time_mode:",
		"field_accuracy_memory_ability:",
		"field_accuracy_memory_domain:",
		"field_accuracy_evidence_need:",
		"semantic_trigger_precision:",
		"semantic_trigger_recall:",
		"false_skip_semantic_rate:",
		"unnecessary_semantic_call_rate:",
		"semantic_mode_accuracy:",
		"forget_route_accuracy:",
		"candidate_recall@20:",
		"candidate_recall@80:",
		"selected_recall@8:",
		"precision@8:",
		"required_hit_rate:",
		"forbidden_recall_rate:",
		"causal_chain_coverage:",
		"temporal_correctness_hard_failures:",
		"redundancy_rate:",
		"restraint_violation_rate:",
		"semantic_calls_per_1000_queries:",
		"semantic_cost_per_1000_queries:",
		"semantic_latency_p95:",
		"retrieval_latency_p95:",
		"post_eval_corrective_action_rate:",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout =\n%s\nwant Phase 8 metric %q", stdout.String(), want)
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
		"test_plan_version: memory_eval_matrix.v0.2",
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
	jsonReport, err := os.ReadFile(filepath.Join(reportDir, "report.json"))
	if err != nil {
		t.Fatalf("read json report: %v", err)
	}
	if !strings.Contains(string(jsonReport), `"test_plan_version": "memory_eval_matrix.v0.2"`) {
		t.Fatalf("json report =\n%s\nwant test_plan_version", string(jsonReport))
	}
	queryAnalysisReport, err := os.ReadFile(filepath.Join(reportDir, "query_analysis.json"))
	if err != nil {
		t.Fatalf("read query analysis report: %v", err)
	}
	for _, want := range []string{
		`"test_plan_version": "memory_eval_matrix.v0.2"`,
		`"case_id": "cli_matrix_sqlite"`,
		`"profile": "sqlite_go"`,
		`"question_id": "q1"`,
		`"source": "rule_only"`,
		`"query_analysis"`,
		`"semantic"`,
	} {
		if !strings.Contains(string(queryAnalysisReport), want) {
			t.Fatalf("query analysis report =\n%s\nwant %q", string(queryAnalysisReport), want)
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
	for _, name := range []string{"report.md", "detail.md", "report.json", "query_analysis.json"} {
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
	queryAnalysisReport, err := os.ReadFile(filepath.Join(reportDir, "query_analysis.json"))
	if err != nil {
		t.Fatalf("read combined query analysis report: %v", err)
	}
	for _, want := range []string{
		`"case_id": "cli_matrix_sqlite"`,
		`"case_id": "cli_matrix_sqlite_second"`,
		`"question_id": "q1"`,
		`"query_analysis"`,
	} {
		if !strings.Contains(string(queryAnalysisReport), want) {
			t.Fatalf("combined query analysis report =\n%s\nwant %q", string(queryAnalysisReport), want)
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

func TestParseOptionsAcceptsSemanticQueryAnalysisForMirrorProfiles(t *testing.T) {
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{
		"--mode", "matrix",
		"--profiles", "sqlite_go,mirror_real_dense",
		"--sidecar-url", "http://127.0.0.1:8765",
		"--query-analysis-mode", "semantic_always",
		"--query-analysis-timeout-ms", "2500",
		"--query-analysis-soft-join-timeout-ms", "1200",
	}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	if opts.queryAnalysis.Mode != "semantic_always" || opts.queryAnalysis.SidecarURL != "http://127.0.0.1:8765" {
		t.Fatalf("query analysis options = %#v", opts.queryAnalysis)
	}
	if opts.queryAnalysis.Timeout != 2500*time.Millisecond || opts.queryAnalysis.SoftJoinTimeout != 1200*time.Millisecond {
		t.Fatalf("query analysis timeouts = timeout:%s soft_join:%s", opts.queryAnalysis.Timeout, opts.queryAnalysis.SoftJoinTimeout)
	}
}

func TestParseOptionsRejectsSemanticQueryAnalysisWithoutSidecarURL(t *testing.T) {
	var stderr bytes.Buffer
	_, ok := parseOptions([]string{
		"--mode", "matrix",
		"--profiles", "mirror_real_dense",
		"--query-analysis-mode", "semantic_always",
	}, &stderr)

	if ok {
		t.Fatal("parseOptions accepted semantic query analysis without sidecar URL")
	}
	if !strings.Contains(stderr.String(), "--sidecar-url is required when --query-analysis-mode is not rule_only") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestParseOptionsRejectsSemanticQueryAnalysisWithoutMirrorProfile(t *testing.T) {
	var stderr bytes.Buffer
	_, ok := parseOptions([]string{
		"--mode", "matrix",
		"--profiles", "sqlite_go",
		"--sidecar-url", "http://127.0.0.1:8765",
		"--query-analysis-mode", "semantic_always",
	}, &stderr)

	if ok {
		t.Fatal("parseOptions accepted semantic query analysis without a mirror profile")
	}
	if !strings.Contains(stderr.String(), "query-analysis-mode requires at least one mirror/semantic profile") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestParseOptionsAcceptsSemanticRewriteOnlyMode(t *testing.T) {
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{
		"--mode", "matrix",
		"--profiles", "semantic_rewrite_only",
		"--sidecar-url", "http://127.0.0.1:8765",
		"--query-analysis-mode", "semantic_rewrite_only",
		"--query-analysis-timeout-ms", "2500",
	}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	if opts.queryAnalysis.Mode != "semantic_rewrite_only" {
		t.Fatalf("query analysis options = %#v", opts.queryAnalysis)
	}
}

func TestParseOptionsAcceptsShadowAdaptiveForSQLiteProfileWithoutSidecar(t *testing.T) {
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{
		"--mode", "matrix",
		"--profiles", "sqlite_go",
		"--query-analysis-mode", "shadow_adaptive",
	}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	if opts.queryAnalysis.Mode != "shadow_adaptive" {
		t.Fatalf("query analysis mode = %q, want shadow_adaptive", opts.queryAnalysis.Mode)
	}
	if opts.queryAnalysis.Provider != "" || opts.queryAnalysis.SidecarURL != "" {
		t.Fatalf("shadow adaptive query analysis options = %#v, want no sidecar provider", opts.queryAnalysis)
	}
}

func TestParseOptionsDefaultsQueryAnalysisSuiteRoot(t *testing.T) {
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{
		"--mode", "matrix",
		"--suite", "query_analysis",
		"--profiles", "sqlite_go",
		"--query-analysis-mode", "shadow_adaptive",
	}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	wantSuffix := filepath.Join("testdata", "memory_eval", "query_analysis")
	if !strings.HasSuffix(filepath.Clean(opts.root), wantSuffix) {
		t.Fatalf("root = %q, want suffix %q", opts.root, wantSuffix)
	}
	if strings.Contains(filepath.Clean(opts.root), filepath.Join("quality", "query_analysis")) {
		t.Fatalf("root = %q, should not point at quality/query_analysis", opts.root)
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
