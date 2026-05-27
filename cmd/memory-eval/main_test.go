package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestRunLiveExtractionMockWritesReports(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "LE001_mock_live.yaml"), []byte(minimalCLILiveExtractionFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	reportDir := filepath.Join(t.TempDir(), "reports")
	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--suite", "extract",
		"--mode", "live",
		"--provider", "mock",
		"--root", root,
		"--temp-dir", t.TempDir(),
		"--report-dir", reportDir,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("run code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	for _, name := range []string{"report.md", "report.json"} {
		if _, err := os.Stat(filepath.Join(reportDir, name)); err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
	}
	for _, want := range []string{
		"live_extraction_report",
		"case_id: LE001_mock_live",
		"status: pass",
		"accepted=1",
		"raw_log_paths:",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout =\n%s\nwant %q", stdout.String(), want)
		}
	}
	jsonReport, err := os.ReadFile(filepath.Join(reportDir, "report.json"))
	if err != nil {
		t.Fatalf("read live report: %v", err)
	}
	for _, want := range []string{
		`"suite": "quality_extract"`,
		`"mode": "live"`,
		`"case_id": "LE001_mock_live"`,
		`"status": "pass"`,
		`"accepted_count": 1`,
		`"raw_log_paths"`,
	} {
		if !strings.Contains(string(jsonReport), want) {
			t.Fatalf("report.json =\n%s\nwant %q", string(jsonReport), want)
		}
	}
}

func TestRunLiveExtractionThinkingFlagControlsOpenAICompatiblePayload(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "LE001_live.yaml"), []byte(minimalCLILiveExtractionFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_EXTRACTION_API_KEY", "test-key")

	for _, tt := range []struct {
		name         string
		args         []string
		wantThinking string
	}{
		{name: "default_false", wantThinking: "disabled"},
		{name: "explicit_false", args: []string{"--thinking", "false"}, wantThinking: "disabled"},
		{name: "explicit_true", args: []string{"--thinking", "true"}, wantThinking: "enabled"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var payload map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				responseText := liveExtractionProviderResponse(t, payload)
				writeLiveExtractionProviderResponse(t, w, responseText)
			}))
			defer server.Close()

			args := []string{
				"--suite", "extract",
				"--mode", "live",
				"--provider", "openai-compatible",
				"--base-url", server.URL,
				"--model", "deepseek-v4-flash",
				"--api-key-env", "TEST_EXTRACTION_API_KEY",
				"--root", root,
				"--temp-dir", t.TempDir(),
				"--report-dir", filepath.Join(t.TempDir(), "reports"),
			}
			args = append(args, tt.args...)
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)

			if code != 0 {
				t.Fatalf("run code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
			}
			thinking, ok := payload["thinking"].(map[string]any)
			if !ok {
				t.Fatalf("thinking = %#v, want object", payload["thinking"])
			}
			if thinking["type"] != tt.wantThinking {
				t.Fatalf("thinking.type = %#v, want %s; payload=%#v", thinking["type"], tt.wantThinking, payload)
			}
		})
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

func TestParseOptionsLoadsConfigRuntimeDefaults(t *testing.T) {
	configPath := writeMemoryEvalConfig(t, `
schema_version: memorycore.config.v0.2
providers:
  llm:
    - id: default_llm
      provider: openai
      protocol: openai_compatible
      enabled: true
      timeout_ms: 30000
sidecar:
  enabled: true
  url: http://127.0.0.1:8765
pipelines:
  query_analysis:
    enabled: true
    provider_id: default_llm
    mode: sidecar
    runtime_mode: semantic_always
    timeout_ms: 2300
    budget:
      max_semantic_latency_ms: 700
`)
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{
		"--config", configPath,
		"--mode", "matrix",
		"--profiles", "mirror_real_dense",
	}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	if opts.sidecarURL != "http://127.0.0.1:8765" {
		t.Fatalf("sidecarURL = %q", opts.sidecarURL)
	}
	if opts.queryAnalysis.Mode != "semantic_always" || opts.queryAnalysis.Provider != "sidecar" {
		t.Fatalf("query analysis options = %#v", opts.queryAnalysis)
	}
	if opts.queryAnalysis.Timeout != 2300*time.Millisecond {
		t.Fatalf("query analysis timeout = %s, want 2300ms", opts.queryAnalysis.Timeout)
	}
	if opts.queryAnalysis.MaxSemanticLatency != 700*time.Millisecond {
		t.Fatalf("query analysis max semantic latency = %s, want 700ms", opts.queryAnalysis.MaxSemanticLatency)
	}
}

func TestParseOptionsCLIFlagsOverrideConfigRuntimeDefaults(t *testing.T) {
	configPath := writeMemoryEvalConfig(t, `
schema_version: memorycore.config.v0.2
providers:
  llm:
    - id: default_llm
      provider: openai
      protocol: openai_compatible
      enabled: true
      timeout_ms: 30000
sidecar:
  enabled: true
  url: http://127.0.0.1:8765
pipelines:
  query_analysis:
    enabled: true
    provider_id: default_llm
    mode: sidecar
    runtime_mode: semantic_always
    timeout_ms: 2300
    budget:
      max_semantic_latency_ms: 700
`)
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{
		"--config", configPath,
		"--mode", "matrix",
		"--profiles", "mirror_real_dense",
		"--sidecar-url", "http://127.0.0.1:9999",
		"--query-analysis-mode", "semantic_rewrite_only",
		"--query-analysis-timeout-ms", "3100",
		"--query-analysis-max-semantic-latency-ms", "1200",
	}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	if opts.sidecarURL != "http://127.0.0.1:9999" {
		t.Fatalf("sidecarURL = %q", opts.sidecarURL)
	}
	if opts.queryAnalysis.Mode != "semantic_rewrite_only" {
		t.Fatalf("query analysis mode = %q", opts.queryAnalysis.Mode)
	}
	if opts.queryAnalysis.Timeout != 3100*time.Millisecond {
		t.Fatalf("query analysis timeout = %s, want 3100ms", opts.queryAnalysis.Timeout)
	}
	if opts.queryAnalysis.MaxSemanticLatency != 1200*time.Millisecond {
		t.Fatalf("query analysis max semantic latency = %s, want 1200ms", opts.queryAnalysis.MaxSemanticLatency)
	}
}

func TestParseOptionsConfigDoesNotInferProfiles(t *testing.T) {
	configPath := writeMemoryEvalConfig(t, `
schema_version: memorycore.config.v0.2
providers:
  llm:
    - id: default_llm
      provider: openai
      protocol: openai_compatible
      enabled: true
      timeout_ms: 30000
sidecar:
  enabled: true
  url: http://127.0.0.1:8765
mirror:
  enabled: true
pipelines:
  query_analysis:
    runtime_mode: rule_only
`)
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{"--config", configPath}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	if len(opts.profiles) != 1 || opts.profiles[0] != "sqlite_go" {
		t.Fatalf("profiles = %#v, want sqlite_go only", opts.profiles)
	}
}

func TestParseOptionsLoadsConfigRuntimeModeWhenPipelineDisabled(t *testing.T) {
	configPath := writeMemoryEvalConfig(t, `
schema_version: memorycore.config.v0.2
pipelines:
  query_analysis:
    runtime_mode: shadow_adaptive
    timeout_ms: 1900
    budget:
      max_semantic_latency_ms: 800
`)
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{
		"--config", configPath,
		"--mode", "matrix",
		"--profiles", "sqlite_go",
	}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	if opts.queryAnalysis.Mode != "shadow_adaptive" {
		t.Fatalf("query analysis mode = %q, want shadow_adaptive", opts.queryAnalysis.Mode)
	}
}

func TestParseOptionsLoadsConfigMirrorRuntimeOptions(t *testing.T) {
	configPath := writeMemoryEvalConfig(t, `
schema_version: memorycore.config.v0.2
sidecar:
  enabled: true
  url: http://127.0.0.1:8765
  total_timeout_ms: 3600
  mirror_timeout_ms: 1700
  activation_timeout_ms: 1800
  rerank_timeout_ms: 1900
`)
	var stderr bytes.Buffer
	opts, ok := parseOptions([]string{"--config", configPath}, &stderr)

	if !ok {
		t.Fatalf("parseOptions failed: %s", stderr.String())
	}
	if opts.mirrorAdapter == nil {
		t.Fatal("mirrorAdapter is nil, want config-derived adapter")
	}
	if opts.sidecarResilience.Timeouts.Total != 3600*time.Millisecond {
		t.Fatalf("total timeout = %s, want 3600ms", opts.sidecarResilience.Timeouts.Total)
	}
	if opts.sidecarResilience.Timeouts.Mirror != 1700*time.Millisecond {
		t.Fatalf("mirror timeout = %s, want 1700ms", opts.sidecarResilience.Timeouts.Mirror)
	}
	if opts.sidecarResilience.Timeouts.Activation != 1800*time.Millisecond {
		t.Fatalf("activation timeout = %s, want 1800ms", opts.sidecarResilience.Timeouts.Activation)
	}
	if opts.sidecarResilience.Timeouts.Rerank != 1900*time.Millisecond {
		t.Fatalf("rerank timeout = %s, want 1900ms", opts.sidecarResilience.Timeouts.Rerank)
	}
}

func TestParseOptionsRejectsMissingConfigFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	var stderr bytes.Buffer
	_, ok := parseOptions([]string{"--config", missing}, &stderr)

	if ok {
		t.Fatal("parseOptions accepted missing config file")
	}
	if !strings.Contains(stderr.String(), "missing.yaml") {
		t.Fatalf("stderr = %q, want missing config path", stderr.String())
	}
}

func TestParseOptionsRejectsInvalidConfigFormat(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "memorycore.yaml")
	if err := os.WriteFile(configPath, []byte("sidecar: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	_, ok := parseOptions([]string{"--config", configPath}, &stderr)

	if ok {
		t.Fatal("parseOptions accepted invalid config format")
	}
	if !strings.Contains(stderr.String(), "load yaml config") {
		t.Fatalf("stderr = %q, want config load error", stderr.String())
	}
}

func TestParseOptionsRejectsConfigSemanticQueryAnalysisWithoutMirrorProfile(t *testing.T) {
	configPath := writeMemoryEvalConfig(t, `
schema_version: memorycore.config.v0.2
providers:
  llm:
    - id: default_llm
      provider: openai
      protocol: openai_compatible
      enabled: true
      timeout_ms: 30000
sidecar:
  enabled: true
  url: http://127.0.0.1:8765
pipelines:
  query_analysis:
    enabled: true
    provider_id: default_llm
    mode: sidecar
    runtime_mode: semantic_always
`)
	var stderr bytes.Buffer
	_, ok := parseOptions([]string{
		"--config", configPath,
		"--mode", "matrix",
		"--profiles", "sqlite_go",
	}, &stderr)

	if ok {
		t.Fatal("parseOptions accepted semantic query analysis without a mirror profile")
	}
	if !strings.Contains(stderr.String(), "query-analysis-mode requires at least one mirror/semantic profile") {
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
		"--query-analysis-max-semantic-latency-ms", "900",
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
	if opts.queryAnalysis.MaxSemanticLatency != 900*time.Millisecond {
		t.Fatalf("query analysis max semantic latency = %s, want 900ms", opts.queryAnalysis.MaxSemanticLatency)
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

func writeMemoryEvalConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memorycore.yaml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
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

func minimalCLILiveExtractionFixture() string {
	return `
schema_version: memory_eval.v0.2
suite: quality_extract
quality_mode: true
allow_stub: false
case_id: LE001_mock_live
description: Mock live extraction should run through the explicit live eval path.
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
      content: 我喜欢咖啡。
      occurred_at: "2026-05-27T09:00:00+08:00"
live_extraction:
  session_id: s1
  provider: mock
  mode: dry-run
  raw_log: true
expect:
  gate:
    min_accepted_facts: 1
    max_review: 0
    max_rejected: 0
  accepted_facts:
    - subject_entity_id: user
      predicate_any_of: [likes]
      summary_contains_any: [咖啡]
  forbidden:
    - raw_prompt_leak
`
}

func liveExtractionProviderResponse(t *testing.T, payload map[string]any) string {
	t.Helper()
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		t.Fatalf("messages = %#v, want non-empty array", payload["messages"])
	}
	last, ok := messages[len(messages)-1].(map[string]any)
	if !ok {
		t.Fatalf("last message = %#v, want object", messages[len(messages)-1])
	}
	content, ok := last["content"].(string)
	if !ok {
		t.Fatalf("last message content = %#v, want string", last["content"])
	}
	jsonStart := strings.Index(content, `{"schema_version":`)
	if jsonStart < 0 {
		t.Fatalf("user message missing request JSON: %s", content)
	}
	var req struct {
		RequestID string  `json:"request_id"`
		PersonaID string  `json:"persona_id"`
		SessionID *string `json:"session_id"`
		Trigger   string  `json:"trigger"`
		Episodes  []struct {
			EpisodeID string `json:"episode_id"`
		} `json:"episodes"`
	}
	if err := json.Unmarshal([]byte(content[jsonStart:]), &req); err != nil {
		t.Fatalf("decode extraction request: %v\n%s", err, content[jsonStart:])
	}
	episodeID := "ep1"
	if len(req.Episodes) > 0 {
		episodeID = req.Episodes[0].EpisodeID
	}
	body := map[string]any{
		"schema_version": memoryExtractionResponseSchemaVersionForTest,
		"request_id":     req.RequestID,
		"persona_id":     req.PersonaID,
		"session_id":     req.SessionID,
		"trigger":        req.Trigger,
		"source_window":  map[string]any{"episode_ids": []string{episodeID}, "started_at": nil, "ended_at": nil},
		"entities":       []any{},
		"facts": []any{map[string]any{
			"candidate_id":                "f1",
			"subject_entity_candidate_id": "user",
			"predicate":                   "likes",
			"object_entity_candidate_id":  nil,
			"object_literal":              "咖啡",
			"content_summary":             "用户喜欢咖啡。",
			"fact_type":                   "stable_preference",
			"valid_from":                  nil,
			"valid_to":                    nil,
			"temporal_precision":          "unknown",
			"extraction_confidence":       "explicit",
			"extraction_confidence_score": 0.95,
			"importance":                  0.7,
			"valence":                     0.2,
			"arousal":                     0.2,
			"sensitivity_level":           "normal",
			"source_episode_ids":          []string{episodeID},
			"evidence_notes":              "用户直接表达喜欢咖啡。",
			"reasoning":                   nil,
			"operation_hint":              "insert_candidate",
			"pinned":                      false,
			"user_requested":              false,
			"searchable_hint":             true,
			"quality_decision":            "accept_for_consolidation",
			"quality_reasons":             []string{"explicit_user_statement"},
		}},
		"links":               []any{},
		"affect_events":       []any{},
		"deletion_intents":    []any{},
		"pin_intents":         []any{},
		"correction_hints":    []any{},
		"rejected_candidates": []any{},
		"quality_flags":       []any{},
		"gate_summary": map[string]any{
			"accepted_fact_count":   1,
			"needs_review_count":    0,
			"rejected_count":        0,
			"has_deletion_intent":   false,
			"has_pin_intent":        false,
			"requires_human_review": false,
			"notes":                 "通过",
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal provider response content: %v", err)
	}
	return string(data)
}

func writeLiveExtractionProviderResponse(t *testing.T, w http.ResponseWriter, responseText string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{
		"model": "deepseek-v4-flash",
		"choices": []any{map[string]any{
			"finish_reason": "stop",
			"message": map[string]any{
				"content": responseText,
			},
		}},
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encode provider response: %v", err)
	}
}

const memoryExtractionResponseSchemaVersionForTest = "memory_extraction_protocol.v0.1"
