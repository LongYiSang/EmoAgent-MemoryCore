package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"gopkg.in/yaml.v3"
)

func TestDefaultValues(t *testing.T) {
	cfg := memconfig.Default()

	if cfg.SchemaVersion != "memorycore.config.v0.1" {
		t.Fatalf("SchemaVersion = %q", cfg.SchemaVersion)
	}
	if cfg.Enabled {
		t.Fatal("Enabled = true, want false")
	}
	if cfg.Core.DBPath != "./data/memory.db" || cfg.Core.PersonaID != "default" {
		t.Fatalf("core defaults = %#v", cfg.Core)
	}
	if !cfg.Core.AutoMigrate || !cfg.Core.EnableFTS {
		t.Fatalf("core bool defaults = auto_migrate:%v enable_fts:%v", cfg.Core.AutoMigrate, cfg.Core.EnableFTS)
	}
	if !cfg.Retrieval.UseFTS || cfg.Retrieval.UseMirror {
		t.Fatalf("retrieval bool defaults = use_fts:%v use_mirror:%v", cfg.Retrieval.UseFTS, cfg.Retrieval.UseMirror)
	}
	if cfg.Retrieval.FinalMemoryCount != 8 || cfg.Retrieval.ContextBudgetTokens != 1200 {
		t.Fatalf("retrieval count/budget defaults = %d/%d", cfg.Retrieval.FinalMemoryCount, cfg.Retrieval.ContextBudgetTokens)
	}
	if cfg.Retrieval.SensitivityPermission != memorycore.SensitivityNormal {
		t.Fatalf("sensitivity default = %q", cfg.Retrieval.SensitivityPermission)
	}
	if cfg.QueryAnalysis.Provider != "none" ||
		cfg.QueryAnalysis.Mode != "rule_only" ||
		cfg.QueryAnalysis.SidecarURL != "http://127.0.0.1:8765" ||
		cfg.QueryAnalysis.TimeoutMS != 1500 ||
		cfg.QueryAnalysis.ScorerVersion != "query_analysis_scorer_v1" ||
		cfg.QueryAnalysis.RouterVersion != "semantic_router_v1" ||
		cfg.QueryAnalysis.MinConfidenceToOverride != 0.72 ||
		cfg.QueryAnalysis.MinEntitySemanticConfidence != 0.70 ||
		cfg.QueryAnalysis.MinRuleFit != 0.66 ||
		cfg.QueryAnalysis.MinAnchorReadiness != 0.45 ||
		cfg.QueryAnalysis.SemanticNeedThreshold != 0.58 ||
		cfg.QueryAnalysis.MinComplexityForSemantic != 0.50 ||
		cfg.QueryAnalysis.FullSemanticComplexity != 0.72 ||
		cfg.QueryAnalysis.DecomposeSemanticComplexity != 0.80 ||
		cfg.QueryAnalysis.MinSemanticFieldConfidence != 0.70 ||
		cfg.QueryAnalysis.MinOverrideMargin != 0.08 ||
		cfg.QueryAnalysis.HighSafetyRiskThreshold != 0.80 ||
		cfg.QueryAnalysis.MaxQueryRewrites != 5 ||
		cfg.QueryAnalysis.MaxSemanticAnchors != 8 ||
		cfg.QueryAnalysis.SemanticTotalEnergyCap != 5.0 ||
		cfg.QueryAnalysis.MaxGeneratedDenseWeightSum != 3.0 ||
		cfg.QueryAnalysis.IncludeRationaleSummary ||
		cfg.QueryAnalysis.Budget.MaxSemanticCallsPerSession != 8 ||
		cfg.QueryAnalysis.Budget.MaxSemanticCallsPer1000Queries != 250 ||
		cfg.QueryAnalysis.Budget.MaxSemanticLatencyMS != 1500 ||
		!cfg.QueryAnalysis.Diagnostics.IncludeScoreBreakdown ||
		!cfg.QueryAnalysis.Diagnostics.IncludeReasonCodes ||
		cfg.QueryAnalysis.Diagnostics.SampleRate != 1.0 {
		t.Fatalf("query_analysis defaults = %#v", cfg.QueryAnalysis)
	}
	if cfg.Sidecar.Enabled || cfg.Sidecar.URL != "" || cfg.Sidecar.Adapter != "trivium" {
		t.Fatalf("sidecar defaults = %#v", cfg.Sidecar)
	}
	if cfg.Sidecar.TotalTimeoutMS != 400 || cfg.Sidecar.MirrorTimeoutMS != 80 || cfg.Sidecar.ActivationTimeoutMS != 150 || cfg.Sidecar.RerankTimeoutMS != 100 {
		t.Fatalf("sidecar timeout defaults = %#v", cfg.Sidecar)
	}
	if !cfg.Sidecar.BreakerEnabled || cfg.Sidecar.BreakerWindow != 20 || cfg.Sidecar.BreakerFailureThreshold != 3 || cfg.Sidecar.BreakerOpenMS != 60000 {
		t.Fatalf("sidecar breaker defaults = %#v", cfg.Sidecar)
	}
	if cfg.Sidecar.ActivationMaxEdgesScannedPerRequest != 10000 || cfg.Sidecar.ActivationMaxNeighborsPerNode != 100 || cfg.Sidecar.ActivationMaxWallMS != 120 {
		t.Fatalf("sidecar activation budget defaults = %#v", cfg.Sidecar)
	}
	if len(cfg.Retention.Jobs) != 1 || cfg.Retention.Jobs[0] != string(memorycore.RetentionJobDailyTTLExpiry) {
		t.Fatalf("retention jobs default = %#v", cfg.Retention.Jobs)
	}
	if cfg.Mirror.SyncLimit != 100 {
		t.Fatalf("mirror sync limit default = %d", cfg.Mirror.SyncLimit)
	}
}

func TestLoadYAMLFillsDefaultsAndPreservesExplicitFalse(t *testing.T) {
	path := writeTempFile(t, "memory.yaml", `
enabled: true
core:
  db_path: ./custom.db
  auto_migrate: false
  enable_fts: false
retrieval:
  use_fts: false
  final_memory_count: 3
query_analysis:
  provider: sidecar
  mode: adaptive_full
  timeout_ms: 1600
  min_rule_fit: 0.7
  semantic_need: 0.61
  min_override_margin: 0.12
  max_query_rewrites: 4
  max_generated_dense_weight_sum: 2.5
`)

	cfg, err := memconfig.LoadYAML(path)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.Core.DBPath != "./custom.db" || cfg.Core.PersonaID != "default" {
		t.Fatalf("core values = %#v", cfg.Core)
	}
	if cfg.Core.AutoMigrate || cfg.Core.EnableFTS {
		t.Fatalf("explicit false bools were not preserved: %#v", cfg.Core)
	}
	if cfg.Retrieval.UseFTS {
		t.Fatal("explicit retrieval.use_fts=false was not preserved")
	}
	if cfg.Retrieval.ContextBudgetTokens != 1200 {
		t.Fatalf("context budget = %d, want default 1200", cfg.Retrieval.ContextBudgetTokens)
	}
	if cfg.QueryAnalysis.Provider != "sidecar" ||
		cfg.QueryAnalysis.Mode != "adaptive_full" ||
		cfg.QueryAnalysis.TimeoutMS != 1600 ||
		cfg.QueryAnalysis.MinRuleFit != 0.7 ||
		cfg.QueryAnalysis.SemanticNeedThreshold != 0.61 ||
		cfg.QueryAnalysis.MinOverrideMargin != 0.12 ||
		cfg.QueryAnalysis.MaxQueryRewrites != 4 ||
		cfg.QueryAnalysis.MaxSemanticAnchors != 8 ||
		cfg.QueryAnalysis.MaxGeneratedDenseWeightSum != 2.5 {
		t.Fatalf("query_analysis values = %#v", cfg.QueryAnalysis)
	}
	if len(cfg.Retention.Jobs) != 1 || cfg.Retention.Jobs[0] != string(memorycore.RetentionJobDailyTTLExpiry) {
		t.Fatalf("retention jobs = %#v", cfg.Retention.Jobs)
	}
}

func TestLoadYAMLAcceptsNestedAdaptiveQueryAnalysisConfig(t *testing.T) {
	path := writeTempFile(t, "memory.yaml", `
query_analysis:
  provider: sidecar
  mode: adaptive_full
  scorer_version: query_analysis_scorer_v2
  router_version: semantic_router_v2
  thresholds:
    min_rule_fit: 0.71
    min_anchor_readiness: 0.46
    semantic_need: 0.62
    min_complexity_for_semantic: 0.51
    full_semantic_complexity: 0.73
    decompose_complexity: 0.81
    min_semantic_field_confidence: 0.74
    min_override_margin: 0.11
  budget:
    max_semantic_calls_per_session: 2
    max_semantic_calls_per_1000_queries: 3
    max_semantic_latency_ms: 900
  diagnostics:
    include_score_breakdown: false
    include_reason_codes: false
    sample_rate: 0.25
`)

	cfg, err := memconfig.LoadYAML(path)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if cfg.QueryAnalysis.ScorerVersion != "query_analysis_scorer_v2" ||
		cfg.QueryAnalysis.RouterVersion != "semantic_router_v2" {
		t.Fatalf("query_analysis versions = %#v", cfg.QueryAnalysis)
	}
	if cfg.QueryAnalysis.Thresholds.MinRuleFit != 0.71 ||
		cfg.QueryAnalysis.Thresholds.MinAnchorReadiness != 0.46 ||
		cfg.QueryAnalysis.Thresholds.SemanticNeedThreshold != 0.62 ||
		cfg.QueryAnalysis.Thresholds.MinComplexityForSemantic != 0.51 ||
		cfg.QueryAnalysis.Thresholds.FullSemanticComplexity != 0.73 ||
		cfg.QueryAnalysis.Thresholds.DecomposeSemanticComplexity != 0.81 ||
		cfg.QueryAnalysis.Thresholds.MinSemanticFieldConfidence != 0.74 ||
		cfg.QueryAnalysis.Thresholds.MinOverrideMargin != 0.11 {
		t.Fatalf("query_analysis thresholds = %#v", cfg.QueryAnalysis.Thresholds)
	}
	if cfg.QueryAnalysis.Budget.MaxSemanticCallsPerSession != 2 ||
		cfg.QueryAnalysis.Budget.MaxSemanticCallsPer1000Queries != 3 ||
		cfg.QueryAnalysis.Budget.MaxSemanticLatencyMS != 900 {
		t.Fatalf("query_analysis budget = %#v", cfg.QueryAnalysis.Budget)
	}
	if cfg.QueryAnalysis.Diagnostics.IncludeScoreBreakdown ||
		cfg.QueryAnalysis.Diagnostics.IncludeReasonCodes ||
		cfg.QueryAnalysis.Diagnostics.SampleRate != 0.25 {
		t.Fatalf("query_analysis diagnostics = %#v", cfg.QueryAnalysis.Diagnostics)
	}

	opts, err := cfg.ToOptions()
	if err != nil {
		t.Fatalf("ToOptions: %v", err)
	}
	qa := opts.QueryAnalysis
	if qa.ScorerVersion != "query_analysis_scorer_v2" ||
		qa.RouterVersion != "semantic_router_v2" ||
		qa.MinRuleFit != 0.71 ||
		qa.MinAnchorReadiness != 0.46 ||
		qa.SemanticNeedThreshold != 0.62 ||
		qa.MaxSemanticCallsPerSession != 2 ||
		qa.MaxSemanticCallsPer1000Queries != 3 ||
		qa.MaxSemanticLatency != 900*time.Millisecond ||
		!qa.DiagnosticsConfigured ||
		qa.DiagnosticsIncludeScoreBreakdown ||
		qa.DiagnosticsIncludeReasonCodes ||
		qa.DiagnosticsSampleRate != 0.25 {
		t.Fatalf("query analysis options = %#v", qa)
	}
}

func TestLoadYAMLRejectsMixedLegacyAndNestedQueryAnalysisConfig(t *testing.T) {
	path := writeTempFile(t, "memory.yaml", `
query_analysis:
  provider: sidecar
  mode: adaptive_safe
  min_rule_fit: 0.70
  thresholds:
    min_rule_fit: 0.71
`)

	_, err := memconfig.LoadYAML(path)
	if err == nil {
		t.Fatal("LoadYAML err = nil, want mixed legacy/nested query_analysis error")
	}
	if !strings.Contains(err.Error(), "query_analysis.thresholds") ||
		!strings.Contains(err.Error(), "legacy") {
		t.Fatalf("LoadYAML err = %v, want nested/legacy migration error", err)
	}
}

func TestLoadJSONFillsDefaultsAndPreservesExplicitFalse(t *testing.T) {
	path := writeTempFile(t, "memory.json", `{
  "enabled": true,
  "core": {
    "db_path": "./json.db",
    "auto_migrate": false,
    "enable_fts": false
  },
  "retrieval": {
    "use_fts": false,
    "context_budget_tokens": 900
  }
}`)

	cfg, err := memconfig.LoadJSON(path)
	if err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if cfg.Core.AutoMigrate || cfg.Core.EnableFTS || cfg.Retrieval.UseFTS {
		t.Fatalf("explicit false bools were not preserved: core=%#v retrieval=%#v", cfg.Core, cfg.Retrieval)
	}
	if cfg.Retrieval.FinalMemoryCount != 8 || cfg.Retrieval.ContextBudgetTokens != 900 {
		t.Fatalf("retrieval count/budget = %d/%d", cfg.Retrieval.FinalMemoryCount, cfg.Retrieval.ContextBudgetTokens)
	}
}

func TestLoadYAMLPreservesSidecarResilienceConfigAndMapsOptions(t *testing.T) {
	path := writeTempFile(t, "memory.yaml", `
enabled: true
retrieval:
  use_mirror: true
sidecar:
  enabled: true
  adapter: fake
  total_timeout_ms: 401
  mirror_timeout_ms: 81
  activation_timeout_ms: 151
  rerank_timeout_ms: 101
  breaker_enabled: false
  breaker_window: 21
  breaker_failure_threshold: 4
  breaker_open_ms: 61000
  activation_max_edges_scanned_per_request: 10001
  activation_max_neighbors_per_node: 101
  activation_max_wall_ms: 121
`)

	cfg, err := memconfig.LoadYAML(path)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if cfg.Sidecar.BreakerEnabled {
		t.Fatal("breaker_enabled=false was not preserved")
	}
	opts, err := cfg.ToOptions()
	if err != nil {
		t.Fatalf("ToOptions: %v", err)
	}
	if opts.SidecarResilience.Timeouts.Total != 401*time.Millisecond ||
		opts.SidecarResilience.Timeouts.Mirror != 81*time.Millisecond ||
		opts.SidecarResilience.Timeouts.Activation != 151*time.Millisecond ||
		opts.SidecarResilience.Timeouts.Rerank != 101*time.Millisecond {
		t.Fatalf("timeouts = %#v", opts.SidecarResilience.Timeouts)
	}
	if opts.SidecarResilience.Breaker.Mode != memorycore.SidecarBreakerModeDisabled {
		t.Fatalf("breaker mode = %q, want disabled", opts.SidecarResilience.Breaker.Mode)
	}
	if opts.SidecarResilience.ActivationBudget.MaxActivationWall != 121*time.Millisecond {
		t.Fatalf("activation wall = %s, want 121ms", opts.SidecarResilience.ActivationBudget.MaxActivationWall)
	}
	if opts.SidecarResilience.ActivationBudget.MaxEdgesScannedPerRequest != 10001 ||
		opts.SidecarResilience.ActivationBudget.MaxNeighborsPerNode != 101 {
		t.Fatalf("activation budget = %#v", opts.SidecarResilience.ActivationBudget)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	yamlPath := writeTempFile(t, "memory.yaml", "core:\n  db_path: ./memory.db\n  typo: true\n")
	if _, err := memconfig.LoadYAML(yamlPath); err == nil {
		t.Fatal("LoadYAML err = nil, want unknown field error")
	}

	jsonPath := writeTempFile(t, "memory.json", `{"core":{"db_path":"./memory.db","typo":true}}`)
	if _, err := memconfig.LoadJSON(jsonPath); err == nil {
		t.Fatal("LoadJSON err = nil, want unknown field error")
	}
}

func TestValidateRules(t *testing.T) {
	t.Run("enabled false does not require db path", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Core.DBPath = ""
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})

	t.Run("enabled true requires db path", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Enabled = true
		cfg.Core.DBPath = ""
		requireErrorContains(t, cfg.Validate(), "core.db_path")
	})

	t.Run("invalid schema version fails", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.SchemaVersion = "memorycore.config.v9"
		requireErrorContains(t, cfg.Validate(), "schema_version")
	})

	t.Run("invalid sensitivity fails", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Retrieval.SensitivityPermission = "private"
		requireErrorContains(t, cfg.Validate(), "retrieval.sensitivity_permission")
	})

	t.Run("invalid query analysis mode fails", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.QueryAnalysis.Mode = "semantic_when_bored"
		requireErrorContains(t, cfg.Validate(), "query_analysis.mode")
	})

	t.Run("adaptive query analysis modes validate with sidecar", func(t *testing.T) {
		for _, mode := range []string{"legacy_only", "shadow_adaptive", "adaptive", "adaptive_safe", "adaptive_full"} {
			cfg := memconfig.Default()
			cfg.QueryAnalysis.Provider = "sidecar"
			cfg.QueryAnalysis.Mode = mode
			if err := cfg.Validate(); err != nil {
				t.Fatalf("mode %q validate: %v", mode, err)
			}
		}
	})

	t.Run("provider none requires rule only mode", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.QueryAnalysis.Provider = "none"
		cfg.QueryAnalysis.Mode = "adaptive_safe"
		requireErrorContains(t, cfg.Validate(), "query_analysis.mode")
	})

	t.Run("query analysis numeric limits must be positive", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.QueryAnalysis.TimeoutMS = 0
		requireErrorContains(t, cfg.Validate(), "query_analysis.timeout_ms")
	})

	t.Run("query analysis generated dense weight sum must be positive", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.QueryAnalysis.MaxGeneratedDenseWeightSum = 0
		requireErrorContains(t, cfg.Validate(), "query_analysis.max_generated_dense_weight_sum")
	})

	t.Run("query analysis adaptive thresholds must be unit intervals", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.QueryAnalysis.MinRuleFit = 1.2
		requireErrorContains(t, cfg.Validate(), "query_analysis.min_rule_fit")
	})

	t.Run("mirror requires sidecar", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Retrieval.UseMirror = true
		requireErrorContains(t, cfg.Validate(), "sidecar.enabled")
	})

	t.Run("non loopback sidecar URL fails", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Sidecar.Enabled = true
		cfg.Sidecar.URL = "https://example.com"
		requireErrorContains(t, cfg.Validate(), "sidecar.url")
	})

	t.Run("enabled trivium sidecar requires url", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Sidecar.Enabled = true
		cfg.Sidecar.Adapter = "trivium"
		cfg.Sidecar.URL = ""
		requireErrorContains(t, cfg.Validate(), "sidecar.url")
	})

	t.Run("monthly deep archive requires positive days", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Retention.Jobs = []string{string(memorycore.RetentionJobMonthlyDeepArchive)}
		requireErrorContains(t, cfg.Validate(), "retention.deep_archive_after_days")
	})

	t.Run("mirror sync limit must be positive", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Mirror.SyncLimit = 0
		requireErrorContains(t, cfg.Validate(), "mirror.sync_limit")
	})

	t.Run("sidecar timeouts must be positive when sidecar enabled", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Sidecar.Enabled = true
		cfg.Sidecar.Adapter = "fake"
		cfg.Sidecar.MirrorTimeoutMS = 0
		requireErrorContains(t, cfg.Validate(), "sidecar.mirror_timeout_ms")
	})

	t.Run("sidecar activation budget must be positive when mirror enabled", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Retrieval.UseMirror = true
		cfg.Sidecar.Enabled = true
		cfg.Sidecar.Adapter = "fake"
		cfg.Sidecar.ActivationMaxWallMS = -1
		requireErrorContains(t, cfg.Validate(), "sidecar.activation_max_wall_ms")
	})

	t.Run("enabled sidecar breaker requires positive open interval", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Sidecar.Enabled = true
		cfg.Sidecar.Adapter = "fake"
		cfg.Sidecar.BreakerOpenMS = 0
		requireErrorContains(t, cfg.Validate(), "sidecar.breaker_open_ms")
	})

	t.Run("disabled breaker allows zero breaker numeric fields", func(t *testing.T) {
		cfg := memconfig.Default()
		cfg.Sidecar.Enabled = true
		cfg.Sidecar.Adapter = "fake"
		cfg.Sidecar.BreakerEnabled = false
		cfg.Sidecar.BreakerWindow = 0
		cfg.Sidecar.BreakerFailureThreshold = 0
		cfg.Sidecar.BreakerOpenMS = 0
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}

func TestEmbeddedYAMLRejectsUnknownFields(t *testing.T) {
	var parent struct {
		Memory memconfig.Config `yaml:"memory"`
	}
	err := yaml.Unmarshal([]byte(`
memory:
  core:
    db_path: ./memory.db
  retrieval:
    use_miror: true
`), &parent)
	requireErrorContains(t, err, "retrieval.use_miror")
}

func TestMappings(t *testing.T) {
	cfg := memconfig.Default()
	cfg.Core.DBPath = "./custom.db"
	cfg.Core.PersonaID = "persona_a"
	cfg.Core.AutoMigrate = false
	cfg.Core.EnableFTS = false
	cfg.Retrieval.UseFTS = false
	cfg.Retrieval.UseMirror = true
	cfg.Retrieval.FinalMemoryCount = 4
	cfg.Retrieval.ContextBudgetTokens = 512
	cfg.Retrieval.AllowHistorical = true
	cfg.Retrieval.AllowDeepArchive = true
	cfg.Retrieval.SensitivityPermission = memorycore.SensitivitySensitive
	cfg.Sidecar.Enabled = true
	cfg.Sidecar.Adapter = "fake"
	cfg.Sidecar.TotalTimeoutMS = 700
	cfg.Sidecar.MirrorTimeoutMS = 70
	cfg.Sidecar.BreakerEnabled = false
	cfg.Retention.Jobs = []string{
		string(memorycore.RetentionJobDailyTTLExpiry),
		string(memorycore.RetentionJobMonthlyDeepArchive),
	}
	cfg.Retention.DeepArchiveAfterDays = 180

	opts, err := cfg.ToOptions()
	if err != nil {
		t.Fatalf("ToOptions: %v", err)
	}
	if opts.DBPath != "./custom.db" || opts.PersonaID != "persona_a" || opts.AutoMigrate || opts.EnableFTS {
		t.Fatalf("options = %#v", opts)
	}
	if opts.MirrorAdapter == nil {
		t.Fatal("MirrorAdapter = nil, want fake adapter")
	}
	if opts.SidecarResilience.Timeouts.Total != 700*time.Millisecond || opts.SidecarResilience.Timeouts.Mirror != 70*time.Millisecond {
		t.Fatalf("sidecar timeout options = %#v", opts.SidecarResilience.Timeouts)
	}
	if opts.SidecarResilience.Breaker.Mode != memorycore.SidecarBreakerModeDisabled {
		t.Fatalf("sidecar breaker mode = %q, want disabled", opts.SidecarResilience.Breaker.Mode)
	}

	policy := cfg.RetrievalPolicy()
	if policy.UseFTS || !policy.UseMirror || policy.FinalMemoryCount != 4 || policy.ContextBudgetTokens != 512 {
		t.Fatalf("policy = %#v", policy)
	}
	if !policy.AllowHistorical || !policy.AllowDeepArchive || policy.SensitivityPermission != memorycore.SensitivitySensitive {
		t.Fatalf("policy gates = %#v", policy)
	}

	jobs := cfg.RetentionJobs()
	if len(jobs) != 2 || jobs[0] != memorycore.RetentionJobDailyTTLExpiry || jobs[1] != memorycore.RetentionJobMonthlyDeepArchive {
		t.Fatalf("jobs = %#v", jobs)
	}
}

func TestDocsDescriptorIsStableAndJSONSerializable(t *testing.T) {
	fields := memconfig.FieldDescriptors()
	if len(fields) == 0 {
		t.Fatal("FieldDescriptors returned no fields")
	}
	markdown := memconfig.MarkdownReference()
	for _, want := range []string{"core.db_path", "retrieval.context_budget_tokens", "query_analysis.max_generated_dense_weight_sum", "sidecar.url", "sidecar.activation_max_wall_ms"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown reference missing %q:\n%s", want, markdown)
		}
	}
	if _, err := json.Marshal(fields); err != nil {
		t.Fatalf("marshal field descriptors: %v", err)
	}
}

func writeTempFile(t *testing.T, name string, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %q, want it to contain %q", err.Error(), want)
	}
}
