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

func TestDefaultConfigV02Contract(t *testing.T) {
	cfg := memconfig.DefaultConfig()

	if cfg.SchemaVersion != memconfig.SchemaVersion {
		t.Fatalf("schema version = %q", cfg.SchemaVersion)
	}
	if cfg.SchemaVersion != "memorycore.config.v0.2" {
		t.Fatalf("schema version = %q, want v0.2", cfg.SchemaVersion)
	}
	if cfg.Enabled {
		t.Fatal("enabled default = true, want embedding opt-in")
	}
	if cfg.Pipelines.Extraction.Params.Temperature != 0 ||
		cfg.Pipelines.Extraction.Mode != string(memorycore.ExtractionRunModeDryRun) ||
		cfg.Pipelines.Extraction.Params.TopP != 1 ||
		cfg.Pipelines.Extraction.Params.MaxOutputTokens != 6000 ||
		cfg.Pipelines.Extraction.Params.ResponseFormat != "json_schema" ||
		cfg.Pipelines.Extraction.RetryOnSchemaFailure != 1 ||
		!cfg.Pipelines.Extraction.Audit.Enabled ||
		cfg.Pipelines.Extraction.Audit.Force ||
		cfg.Pipelines.Extraction.RawLog.Enabled ||
		cfg.Pipelines.Extraction.RawLog.Directory != "" {
		t.Fatalf("extraction defaults = %#v", cfg.Pipelines.Extraction)
	}
	if cfg.Pipelines.QueryAnalysis.FallbackMode != "rule_only" {
		t.Fatalf("query analysis fallback = %q, want rule_only", cfg.Pipelines.QueryAnalysis.FallbackMode)
	}
	if cfg.Retrieval.FinalMemoryCount != 8 ||
		cfg.Retrieval.ContextBudgetTokens != 1200 ||
		cfg.Retrieval.Activation.MaxHops != 2 ||
		cfg.Retrieval.Activation.MaxHopsForHistoricalOrCausal != 3 ||
		cfg.Retrieval.Ranking.CandidatePoolSize != 80 ||
		!cfg.Retrieval.MMR.Enabled ||
		cfg.Retrieval.MMR.Lambda != 0.72 ||
		cfg.Retrieval.Ranking.AgentAffectWeightCap != 0.03 {
		t.Fatalf("retrieval defaults = %#v", cfg.Retrieval)
	}
	if cfg.AgentAffect.Enabled ||
		!cfg.AgentAffect.StorageEnabled ||
		cfg.AgentAffect.Retrieval.WeightCap != 0.03 ||
		cfg.AgentAffect.Retrieval.SensitiveRecall != "disallow" ||
		cfg.AgentAffect.Retrieval.NegativeRetentionBoost {
		t.Fatalf("agent affect defaults = %#v", cfg.AgentAffect)
	}
	if cfg.Retention.AutoDelete {
		t.Fatal("retention.auto_delete default = true, want false")
	}
	if cfg.SemanticOps.SemanticMirrorMetaEnabled ||
		cfg.SemanticOps.SemanticSidecarAuthTokenEnabled ||
		cfg.SemanticOps.Dedup.Enabled ||
		cfg.SemanticOps.Dedup.Shadow ||
		cfg.SemanticOps.Dedup.Enforce ||
		cfg.SemanticOps.Curation.Enabled ||
		cfg.SemanticOps.Forget.PreviewEnabled ||
		cfg.SemanticOps.Forget.ExecuteEnabled {
		t.Fatalf("semantic ops defaults should be disabled: %#v", cfg.SemanticOps)
	}
	if cfg.SemanticOps.Dedup.CandidateLimit != 12 ||
		cfg.SemanticOps.Dedup.ThresholdProfile != "default_v0" {
		t.Fatalf("semantic dedup defaults = %#v", cfg.SemanticOps.Dedup)
	}
	if cfg.SemanticOps.Curation.Mode != "dry_run" ||
		cfg.SemanticOps.Curation.MaxNewFactsPerRun != 100 ||
		cfg.SemanticOps.Curation.CandidateLimitPerFact != 20 ||
		cfg.SemanticOps.Curation.MaxFactsPerGroup != 8 ||
		cfg.SemanticOps.Curation.MinAutoApplyConfidence != 0.88 ||
		cfg.SemanticOps.Curation.CandidateRetrieval.Mode != "mirror_first" ||
		cfg.SemanticOps.Curation.CandidateRetrieval.MirrorMinSimilarity != 0.70 ||
		cfg.SemanticOps.Curation.RawLog.Enabled ||
		cfg.SemanticOps.Curation.RawLog.Directory != "" ||
		cfg.SemanticOps.Curation.LLM.ProviderID != "default_llm" ||
		cfg.SemanticOps.Curation.LLM.Model != "memory-curator" ||
		cfg.SemanticOps.Curation.LLM.ResponseFormat != "json_object" ||
		cfg.SemanticOps.Curation.LLM.Thinking.Type != "disabled" {
		t.Fatalf("semantic curation defaults = %#v", cfg.SemanticOps.Curation)
	}
}

func TestLoadFullV02ConfigAndMapRuntimeOptions(t *testing.T) {
	path := writeTempFile(t, "memory.yaml", fullV02YAML())

	cfg, err := memconfig.LoadYAML(path)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if cfg.Core.DBPath != "./full.db" || cfg.Core.Timezone != "Asia/Shanghai" {
		t.Fatalf("core = %#v", cfg.Core)
	}
	if got := cfg.ProviderByID("llm_main"); got == nil || got.Protocol != "openai_compatible" || got.APIKeyEnv != "MEMORYCORE_LLM_API_KEY" {
		t.Fatalf("llm provider = %#v", got)
	}
	if cfg.Pipelines.Extraction.Mode != string(memorycore.ExtractionRunModeApply) ||
		cfg.Pipelines.Extraction.Model != "gpt-5.4" ||
		cfg.Pipelines.Extraction.Params.MaxOutputTokens != 7000 ||
		cfg.Pipelines.Extraction.Thinking.Type != "disabled" ||
		!cfg.Pipelines.Extraction.RawLog.Enabled ||
		cfg.Pipelines.Extraction.RawLog.Directory != "./debug/extraction_raw" ||
		cfg.Pipelines.QueryAnalysis.Mode != "rule_then_llm" ||
		cfg.Pipelines.QueryAnalysis.ProviderID != "llm_main" ||
		cfg.Pipelines.QueryAnalysis.FallbackMode != "rule_only" ||
		cfg.Pipelines.Embedding.ProviderID != "embed_main" ||
		cfg.Pipelines.Rerank.ProviderID != "rerank_main" {
		t.Fatalf("pipelines = %#v", cfg.Pipelines)
	}
	if !cfg.WritePolicy.Triggers.ManualForget.Enabled ||
		!cfg.WritePolicy.Prefilter.RouteManualForgetAlways ||
		cfg.WritePolicy.Extraction.AllowSensitiveExtraction {
		t.Fatalf("write policy = %#v", cfg.WritePolicy)
	}
	opts, err := cfg.ToOptions()
	if err != nil {
		t.Fatalf("ToOptions: %v", err)
	}
	if opts.DBPath != "./full.db" || opts.PersonaID != "persona_full" || !opts.AutoMigrate || !opts.EnableFTS {
		t.Fatalf("options core = %#v", opts)
	}
	if !opts.Extraction.Enabled ||
		opts.Extraction.Provider.Kind != memorycore.ExtractionProviderOpenAICompatible ||
		opts.Extraction.Provider.ID != "llm_main" ||
		opts.Extraction.Provider.BaseURL != "https://llm.invalid/v1" ||
		opts.Extraction.Provider.Model != "gpt-5.4" ||
		opts.Extraction.Provider.MaxTokens != 7000 ||
		opts.Extraction.Provider.ResponseFormat != memorycore.ExtractionResponseFormatJSONSchema ||
		opts.Extraction.Defaults.Mode != memorycore.ExtractionRunModeApply ||
		opts.Extraction.Defaults.ExecuteDeletionIntents ||
		!opts.Extraction.Runtime.UsePreFilter ||
		!opts.Extraction.Runtime.RepairEnabled ||
		opts.Extraction.Audit.Enabled ||
		!opts.Extraction.Audit.Force ||
		!opts.Extraction.RawLog.Enabled ||
		opts.Extraction.RawLog.Directory != "./debug/extraction_raw" ||
		opts.Extraction.Defaults.AllowSensitiveExtraction {
		t.Fatalf("extraction options = %#v", opts.Extraction)
	}
	if opts.QueryAnalysis.Provider != memorycore.QueryAnalysisProviderSidecar ||
		opts.QueryAnalysis.Mode != memorycore.QueryAnalysisModeAdaptiveSafe ||
		opts.QueryAnalysis.SidecarURL != "http://127.0.0.1:8765" ||
		opts.QueryAnalysis.Timeout != 1600*time.Millisecond ||
		opts.QueryAnalysis.MaxSemanticLatency != 1600*time.Millisecond {
		t.Fatalf("query analysis options = %#v", opts.QueryAnalysis)
	}
	if opts.SidecarResilience.Timeouts.Total != 2500*time.Millisecond ||
		opts.SidecarResilience.Breaker.Window != 10 ||
		opts.SidecarResilience.ActivationBudget.MaxEdgesScannedPerRequest != 12000 {
		t.Fatalf("sidecar options = %#v", opts.SidecarResilience)
	}
	policy := cfg.RetrievalPolicy()
	if !policy.UseFTS || !policy.UseMirror || policy.FinalMemoryCount != 9 || policy.ContextBudgetTokens != 1500 {
		t.Fatalf("retrieval policy = %#v", policy)
	}
	if jobs := cfg.RetentionJobs(); len(jobs) != 2 ||
		jobs[0] != memorycore.RetentionJobDailyTTLExpiry ||
		jobs[1] != memorycore.RetentionJobMonthlyDeepArchive {
		t.Fatalf("retention jobs = %#v", jobs)
	}
}

func TestMinimalV02ConfigUsesDefaults(t *testing.T) {
	path := writeTempFile(t, "memory.yaml", `
schema_version: memorycore.config.v0.2
enabled: true
core:
  db_path: ./minimal.db
`)

	cfg, err := memconfig.LoadYAML(path)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	if cfg.Core.DBPath != "./minimal.db" || cfg.Core.PersonaID != "default" {
		t.Fatalf("core defaults = %#v", cfg.Core)
	}
	if cfg.Pipelines.Extraction.Params.ResponseFormat != "json_schema" ||
		cfg.Pipelines.QueryAnalysis.FallbackMode != "rule_only" ||
		cfg.Retrieval.FinalMemoryCount != 8 ||
		cfg.AgentAffect.Retrieval.WeightCap != 0.03 {
		t.Fatalf("defaults not applied: pipelines=%#v retrieval=%#v affect=%#v", cfg.Pipelines, cfg.Retrieval, cfg.AgentAffect)
	}
}

func TestSemanticOpsConfigMapsFeatureFlagsToOptions(t *testing.T) {
	path := writeTempFile(t, "memory.yaml", `
schema_version: memorycore.config.v0.2
enabled: true
core:
  db_path: ./semantic.db
sidecar:
  enabled: true
  url: http://127.0.0.1:8765
semantic_ops:
  semantic_mirror_meta_enabled: true
  semantic_sidecar_auth_token_enabled: true
  dedup:
    enabled: true
    shadow: true
    enforce: false
    candidate_limit: 7
    threshold_profile: conservative_v1
  forget:
    preview_enabled: true
    execute_enabled: false
  curation:
    enabled: true
    mode: apply
    max_new_facts_per_run: 42
    candidate_limit_per_fact: 11
    max_facts_per_group: 6
    min_auto_apply_confidence: 0.91
    candidate_retrieval:
      mode: sqlite_only
      mirror_min_similarity: 0.82
    include_fact_types:
      - stable_preference
    exclude_fact_types:
      - core_identity
      - commitment
    raw_log:
      enabled: true
      directory: ./debug/curation_raw
    llm:
      provider_id: default_llm
      provider_kind: mock
      model: memory-curator-test
      temperature: 0
      max_tokens: 2048
      response_format: json_object
      timeout_ms: 12345
      thinking:
        type: enabled
`)

	cfg, err := memconfig.LoadYAML(path)
	if err != nil {
		t.Fatalf("LoadYAML: %v", err)
	}
	opts, err := cfg.ToOptions()
	if err != nil {
		t.Fatalf("ToOptions: %v", err)
	}
	if !opts.SemanticOps.SemanticMirrorMetaEnabled ||
		!opts.SemanticOps.SemanticSidecarAuthTokenEnabled ||
		!opts.SemanticOps.Dedup.Enabled ||
		!opts.SemanticOps.Dedup.Shadow ||
		opts.SemanticOps.Dedup.Enforce ||
		opts.SemanticOps.Dedup.CandidateLimit != 7 ||
		opts.SemanticOps.Dedup.ThresholdProfile != "conservative_v1" ||
		!opts.SemanticOps.Forget.PreviewEnabled ||
		opts.SemanticOps.Forget.ExecuteEnabled ||
		!opts.SemanticOps.Curation.Enabled ||
		opts.SemanticOps.Curation.Mode != "apply" ||
		opts.SemanticOps.Curation.MaxNewFactsPerRun != 42 ||
		opts.SemanticOps.Curation.CandidateLimitPerFact != 11 ||
		opts.SemanticOps.Curation.MaxFactsPerGroup != 6 ||
		opts.SemanticOps.Curation.MinAutoApplyConfidence != 0.91 ||
		opts.SemanticOps.Curation.CandidateRetrieval.Mode != "sqlite_only" ||
		opts.SemanticOps.Curation.CandidateRetrieval.MirrorMinSimilarity != 0.82 ||
		!opts.SemanticOps.Curation.RawLog.Enabled ||
		opts.SemanticOps.Curation.RawLog.Directory != "./debug/curation_raw" ||
		opts.SemanticOps.Curation.LLM.Provider.ID != "default_llm" ||
		opts.SemanticOps.Curation.LLM.Provider.Kind != memorycore.ExtractionProviderMock ||
		opts.SemanticOps.Curation.LLM.Provider.Model != "memory-curator-test" ||
		opts.SemanticOps.Curation.LLM.Provider.MaxTokens != 2048 ||
		opts.SemanticOps.Curation.LLM.Provider.ResponseFormat != memorycore.ExtractionResponseFormatJSONObject ||
		opts.SemanticOps.Curation.LLM.Provider.Timeout != 12345*time.Millisecond ||
		opts.SemanticOps.Curation.LLM.Provider.Thinking == nil ||
		opts.SemanticOps.Curation.LLM.Provider.Thinking.Type != "enabled" {
		t.Fatalf("semantic options = %#v", opts.SemanticOps)
	}
}

func TestLoadRejectsUnknownFieldsAndBypassSwitches(t *testing.T) {
	for name, body := range map[string]string{
		"yaml_unknown": `
schema_version: memorycore.config.v0.2
retrieval:
  allow_purged: true
`,
		"yaml_provider_unknown": `
schema_version: memorycore.config.v0.2
providers:
  llm:
    - id: llm
      enabled: true
      api_token: nope
`,
		"yaml_agent_affect_bypass": `
schema_version: memorycore.config.v0.2
agent_affect:
  allow_memory_writes: true
`,
		"yaml_work_candidate_bypass": `
schema_version: memorycore.config.v0.2
write_policy:
  extraction:
    auto_approve_work_candidates: true
`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := memconfig.LoadYAML(writeTempFile(t, "memory.yaml", body))
			if err == nil {
				t.Fatal("LoadYAML err = nil, want unknown field error")
			}
		})
	}

	_, err := memconfig.LoadJSON(writeTempFile(t, "memory.json", `{"schema_version":"memorycore.config.v0.2","retrieval":{"trust_mirror_authority":true}}`))
	if err == nil {
		t.Fatal("LoadJSON err = nil, want unknown field error")
	}
}

func TestValidateRejectsPlaintextSecretsAndMissingProviderReferences(t *testing.T) {
	t.Run("plaintext provider api key field", func(t *testing.T) {
		path := writeTempFile(t, "memory.yaml", `
schema_version: memorycore.config.v0.2
providers:
  llm:
    - id: llm_main
      provider: openai
      api_key: sk-nope
`)
		_, err := memconfig.LoadYAML(path)
		requireErrorContains(t, err, "api_key")
	})

	t.Run("secret in provider config", func(t *testing.T) {
		cfg := memconfig.DefaultConfig()
		cfg.Providers.LLM = []memconfig.ProviderConfig{{
			ID:      "llm_main",
			Enabled: true,
			Config:  map[string]any{"secret": "nope"},
		}}
		requireErrorContains(t, cfg.Validate(), "secret")
	})

	t.Run("pipeline provider id must exist", func(t *testing.T) {
		cfg := memconfig.DefaultConfig()
		cfg.Pipelines.QueryAnalysis.Enabled = true
		cfg.Pipelines.QueryAnalysis.ProviderID = "missing"
		cfg.Pipelines.QueryAnalysis.Mode = "rule_then_llm"
		requireErrorContains(t, cfg.Validate(), "pipelines.query_analysis.provider_id")
	})

	t.Run("extraction raw log directory required when enabled", func(t *testing.T) {
		cfg := memconfig.DefaultConfig()
		cfg.Pipelines.Extraction.RawLog.Enabled = true
		requireErrorContains(t, cfg.Validate(), "pipelines.extraction.raw_log.directory")
	})

	t.Run("curation raw log directory required when enabled", func(t *testing.T) {
		cfg := memconfig.DefaultConfig()
		cfg.SemanticOps.Curation.RawLog.Enabled = true
		requireErrorContains(t, cfg.Validate(), "semantic_ops.curation.raw_log.directory")
	})

	t.Run("curation response format is json object", func(t *testing.T) {
		cfg := memconfig.DefaultConfig()
		cfg.SemanticOps.Curation.LLM.ResponseFormat = "json_schema"
		requireErrorContains(t, cfg.Validate(), "semantic_ops.curation.llm.response_format")
	})

	t.Run("extraction mode must be supported", func(t *testing.T) {
		cfg := memconfig.DefaultConfig()
		cfg.Pipelines.Extraction.Mode = "invalid"
		requireErrorContains(t, cfg.Validate(), "pipelines.extraction.mode")
	})
}

func TestValidateRuntimeChecksEnvBackedProviders(t *testing.T) {
	cfg := memconfig.DefaultConfig()
	cfg.Providers.LLM = []memconfig.ProviderConfig{{
		ID:        "llm_main",
		Provider:  "openai",
		Enabled:   true,
		APIKeyEnv: "MEMORYCORE_TEST_KEY",
	}}

	err := cfg.ValidateRuntime(memconfig.RuntimeValidationOptions{
		CheckEnv: true,
		Env: func(key string) string {
			return ""
		},
	})
	requireErrorContains(t, err, "MEMORYCORE_TEST_KEY")

	err = cfg.ValidateRuntime(memconfig.RuntimeValidationOptions{
		CheckEnv: true,
		Env: func(key string) string {
			if key == "MEMORYCORE_TEST_KEY" {
				return "present"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatalf("ValidateRuntime: %v", err)
	}
}

func TestApplyOverridesAndProviderRegistry(t *testing.T) {
	cfg := memconfig.DefaultConfig()
	enabled := true
	dbPath := "./override.db"
	finalCount := 5
	llmModel := "gpt-5.4-mini"
	cfg.ApplyOverrides(memconfig.ConfigOverrides{
		Enabled: &enabled,
		Core: &memconfig.CoreOverrides{
			DBPath: &dbPath,
		},
		Retrieval: &memconfig.RetrievalOverrides{
			FinalMemoryCount: &finalCount,
		},
		Pipelines: &memconfig.PipelineOverrides{
			Extraction: &memconfig.LLMPipelineOverrides{
				Model: &llmModel,
			},
		},
	})

	cfg.ApplyProviderRegistry(memconfig.ProviderRegistry{
		LLM: []memconfig.ProviderMapping{{
			ID:        "emo_llm",
			Provider:  "openai",
			Protocol:  "openai_compatible",
			BaseURL:   "https://llm.invalid/v1",
			APIKeyEnv: "EMO_LLM_KEY",
			Enabled:   true,
		}},
	})

	if !cfg.Enabled || cfg.Core.DBPath != "./override.db" || cfg.Retrieval.FinalMemoryCount != 5 {
		t.Fatalf("overrides not applied: %#v", cfg)
	}
	if cfg.Pipelines.Extraction.Model != "gpt-5.4-mini" {
		t.Fatalf("extraction model = %q", cfg.Pipelines.Extraction.Model)
	}
	if got := cfg.ProviderByID("emo_llm"); got == nil || got.BaseURL != "https://llm.invalid/v1" {
		t.Fatalf("registry provider = %#v", got)
	}
}

func TestLoadEffectiveAppliesProviderRegistryBeforeValidation(t *testing.T) {
	path := writeTempFile(t, "memory.yaml", `
schema_version: memorycore.config.v0.2
pipelines:
  extraction:
    enabled: true
    provider_id: emo_llm
    model: gpt-host
`)

	cfg, err := memconfig.LoadEffective(memconfig.LoadEffectiveOptions{
		ConfigPath: path,
		ProviderRegistry: memconfig.ProviderRegistry{
			LLM: []memconfig.ProviderMapping{{
				ID:        "emo_llm",
				Provider:  "openai",
				Protocol:  "openai_compatible",
				APIKeyEnv: "EMO_LLM_KEY",
				Enabled:   true,
			}},
		},
	})
	if err != nil {
		t.Fatalf("LoadEffective: %v", err)
	}
	if got := cfg.ProviderByID("emo_llm"); got == nil {
		t.Fatalf("provider registry was not applied: %#v", cfg.Providers)
	}
	if cfg.Pipelines.Extraction.ProviderID != "emo_llm" {
		t.Fatalf("extraction provider_id = %q", cfg.Pipelines.Extraction.ProviderID)
	}
}

func TestEmbeddedYAMLRejectsUnknownFields(t *testing.T) {
	var parent struct {
		Memory memconfig.Config `yaml:"memory"`
	}
	err := yaml.Unmarshal([]byte(`
memory:
  schema_version: memorycore.config.v0.2
  providers:
    llm:
      - id: llm
        typo: true
`), &parent)
	requireErrorContains(t, err, "providers.llm.typo")
}

func TestDocsDescriptorIsStableAndJSONSerializable(t *testing.T) {
	fields := memconfig.FieldDescriptors()
	if len(fields) == 0 {
		t.Fatal("FieldDescriptors returned no fields")
	}
	markdown := memconfig.MarkdownReference()
	for _, want := range []string{
		"schema_version",
		"providers.llm[].api_key_env",
		"pipelines.extraction.mode",
		"pipelines.extraction.params.max_output_tokens",
		"pipelines.extraction.audit.enabled",
		"pipelines.extraction.audit.force",
		"pipelines.extraction.raw_log.enabled",
		"pipelines.extraction.raw_log.directory",
		"semantic_ops.curation.llm.response_format",
		"write_policy.triggers.manual_forget.enabled",
		"retrieval.activation.max_hops",
		"forgetting_privacy.cleanup.delete_trivium_nodes",
		"agent_affect.retrieval.weight_cap",
	} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("markdown reference missing %q:\n%s", want, markdown)
		}
	}
	if _, err := json.Marshal(fields); err != nil {
		t.Fatalf("marshal field descriptors: %v", err)
	}
}

func fullV02YAML() string {
	return `
schema_version: memorycore.config.v0.2
enabled: true
core:
  db_path: ./full.db
  persona_id: persona_full
  auto_migrate: true
  enable_fts: true
  timezone: Asia/Shanghai
providers:
  llm:
    - id: llm_main
      provider: openai
      protocol: openai_compatible
      base_url: https://llm.invalid/v1
      api_key_env: MEMORYCORE_LLM_API_KEY
      enabled: true
      timeout_ms: 30000
      retry:
        max_attempts: 2
        backoff_ms: 500
      config:
        organization: test_org
  embedder:
    - id: embed_main
      provider: disabled
      enabled: false
  reranker:
    - id: rerank_main
      provider: disabled
      enabled: false
pipelines:
  prefilter:
    enabled: true
    provider_id: llm_main
    model: gpt-5.4-mini
    params:
      temperature: 0
      top_p: 1
      max_output_tokens: 1200
      response_format: json_schema
    thinking:
      type: disabled
    reasoning_effort: high
  extraction:
    enabled: true
    mode: apply
    provider_id: llm_main
    model: gpt-5.4
    params:
      temperature: 0
      top_p: 1
      max_output_tokens: 7000
      response_format: json_schema
      stream: false
      seed: 7
      timeout_ms: 30000
    thinking:
      type: disabled
    reasoning_effort: high
    retry_on_schema_failure: 1
    audit:
      enabled: false
      force: true
    raw_log:
      enabled: true
      directory: ./debug/extraction_raw
  extraction_repair:
    enabled: true
    provider_id: llm_main
    model: gpt-5.4-mini
  query_analysis:
    enabled: true
    mode: rule_then_llm
    runtime_mode: adaptive_safe
    fallback_mode: rule_only
    provider_id: llm_main
    model: gpt-5.4-mini
    timeout_ms: 1600
    params:
      temperature: 0
      top_p: 1
      max_output_tokens: 768
      response_format: json_schema
    thresholds:
      min_rule_fit: 0.67
      min_anchor_readiness: 0.46
      semantic_need: 0.59
      min_complexity_for_semantic: 0.51
      full_semantic_complexity: 0.73
      decompose_complexity: 0.81
      min_semantic_field_confidence: 0.71
      min_override_margin: 0.09
      high_safety_risk: 0.80
    budget:
      max_semantic_calls_per_session: 9
      max_semantic_calls_per_session_window_ms: 1800000
      max_semantic_calls_per_1000_queries: 251
      max_semantic_latency_ms: 1600
    diagnostics:
      include_score_breakdown: true
      include_reason_codes: true
      sample_rate: 0.5
  embedding:
    enabled: false
    provider_id: embed_main
    model: text-embedding-3-small
    batch_size: 32
  rerank:
    enabled: false
    provider_id: rerank_main
    model: disabled
    top_k: 20
  narrative_insight:
    enabled: false
    provider_id: llm_main
    model: gpt-5.4-mini
write_policy:
  triggers:
    idle_detect:
      enabled: true
      min_idle_ms: 8000
    session_end:
      enabled: true
    manual_pin:
      enabled: true
    manual_forget:
      enabled: true
    work_candidate:
      enabled: true
  extraction:
    allow_inference: true
    allow_sensitive_extraction: false
    max_facts_per_request: 12
    max_links_per_request: 20
    sensitive_reasoning_max_chars: 0
  prefilter:
    min_memory_worthiness: 0.55
    min_long_term_value: 0.45
    keep_manual_pin_always: true
    route_manual_forget_always: true
retrieval:
  use_fts: true
  use_mirror: true
  allow_historical: true
  allow_deep_archive: false
  sensitivity_permission: normal
  final_memory_count: 9
  context_budget_tokens: 1500
  anchor:
    max_entity_anchors: 20
    max_sparse_anchors: 30
    max_dense_anchors: 30
    max_pinned_core_anchors: 10
    max_recent_anchors: 10
    max_narrative_anchors: 10
    entity_exact_min_energy: 0.75
    pinned_core_min_energy: 0.70
    seed_energy_cap_per_node: 1
  activation:
    max_hops: 2
    max_hops_for_historical_or_causal: 3
    hop_decay: 0.75
    teleport_alpha: 0.15
    min_energy_threshold: 0.015
    max_active_nodes: 1000
    hub_power: 0.55
    allow_negative_edges: true
  fatigue:
    window_turns: 5
    factor: 0.35
    factor_for_repeated: 0.20
  cooccurrence:
    enabled: true
    max_pairs: 100
  ranking:
    candidate_pool_size: 80
    min_final_score: 0.20
    agent_affect_weight_cap: 0.03
  mmr:
    enabled: true
    lambda: 0.72
    duplicate_threshold: 0.88
  prompt:
    max_source_episode_quotes: 2
    quote_by_default: false
sidecar:
  enabled: true
  url: http://127.0.0.1:8765
  adapter: trivium
  total_timeout_ms: 2500
  mirror_timeout_ms: 1200
  activation_timeout_ms: 1500
  rerank_timeout_ms: 1200
  circuit_breaker:
    enabled: true
    window: 10
    failure_threshold: 3
    open_ms: 60000
  activation_budget:
    max_edges_scanned_per_request: 12000
    max_neighbors_per_node: 100
    max_wall_ms: 120
mirror:
  enabled: true
  sync_limit: 100
  rebuild_on_start: false
  stale_lag_threshold_ms: 30000
retention:
  jobs:
    lazy_decay: true
    daily_ttl_expiry: true
    daily_state_transition: true
    weekly_compression: false
    monthly_archive: true
    mirror_compaction: true
  thresholds:
    active_to_dormant: 0.35
    dormant_to_archived: 0.20
    archived_to_deep_threshold: 0.18
    deep_archive_after_days: 180
  auto_delete: false
forgetting_privacy:
  default_forget_level: soft_forget
  require_confirmation_for_purge: true
  require_confirmation_for_topic_scope: true
  cleanup:
    delete_trivium_nodes: true
    delete_sqlite_search_documents: true
    clean_agent_affect_refs: true
    recompute_derived: true
    verify_after_delete: true
agent_affect:
  enabled: false
  storage_enabled: true
  service_mode: local_stub
  default_profile: default
  neutral_fallback: true
  safety:
    allow_user_fact_writes: false
    allow_sensitivity_bypass: false
    allow_forget_bypass: false
    mood_safety: conservative
  retrieval:
    weight_cap: 0.03
    sensitive_recall: disallow
    negative_retention_boost: false
observability:
  metrics_enabled: true
  include_score_breakdown: false
  include_activation_paths: false
  log_sanitized_debug: true
eval:
  enabled: false
`
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
