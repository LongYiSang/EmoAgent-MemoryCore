package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"gopkg.in/yaml.v3"
)

const SchemaVersion = "memorycore.config.v0.2"

type Config struct {
	SchemaVersion     string                  `yaml:"schema_version" json:"schema_version"`
	Enabled           bool                    `yaml:"enabled" json:"enabled"`
	Core              CoreConfig              `yaml:"core" json:"core"`
	Providers         ProvidersConfig         `yaml:"providers" json:"providers"`
	Pipelines         PipelinesConfig         `yaml:"pipelines" json:"pipelines"`
	WritePolicy       WritePolicyConfig       `yaml:"write_policy" json:"write_policy"`
	Retrieval         RetrievalConfig         `yaml:"retrieval" json:"retrieval"`
	Sidecar           SidecarConfig           `yaml:"sidecar" json:"sidecar"`
	Mirror            MirrorConfig            `yaml:"mirror" json:"mirror"`
	Retention         RetentionConfig         `yaml:"retention" json:"retention"`
	ForgettingPrivacy ForgettingPrivacyConfig `yaml:"forgetting_privacy" json:"forgetting_privacy"`
	AgentAffect       AgentAffectConfig       `yaml:"agent_affect" json:"agent_affect"`
	Observability     ObservabilityConfig     `yaml:"observability" json:"observability"`
	Eval              *EvalConfig             `yaml:"eval,omitempty" json:"eval,omitempty"`
}

type CoreConfig struct {
	DBPath      string `yaml:"db_path" json:"db_path"`
	PersonaID   string `yaml:"persona_id" json:"persona_id"`
	AutoMigrate bool   `yaml:"auto_migrate" json:"auto_migrate"`
	EnableFTS   bool   `yaml:"enable_fts" json:"enable_fts"`
	Timezone    string `yaml:"timezone" json:"timezone"`
}

type ProvidersConfig struct {
	LLM      []ProviderConfig `yaml:"llm" json:"llm"`
	Embedder []ProviderConfig `yaml:"embedder" json:"embedder"`
	Reranker []ProviderConfig `yaml:"reranker" json:"reranker"`
}

type ProviderConfig struct {
	ID        string         `yaml:"id" json:"id"`
	Provider  string         `yaml:"provider" json:"provider"`
	Protocol  string         `yaml:"protocol" json:"protocol"`
	BaseURL   string         `yaml:"base_url" json:"base_url"`
	APIKeyEnv string         `yaml:"api_key_env" json:"api_key_env"`
	Enabled   bool           `yaml:"enabled" json:"enabled"`
	TimeoutMS int            `yaml:"timeout_ms" json:"timeout_ms"`
	Retry     RetryConfig    `yaml:"retry" json:"retry"`
	Extra     map[string]any `yaml:"extra,omitempty" json:"extra,omitempty"`
	Config    map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

type RetryConfig struct {
	MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`
	BackoffMS   int `yaml:"backoff_ms" json:"backoff_ms"`
}

type PipelinesConfig struct {
	Prefilter        LLMPipelineConfig        `yaml:"prefilter" json:"prefilter"`
	Extraction       ExtractionPipelineConfig `yaml:"extraction" json:"extraction"`
	ExtractionRepair LLMPipelineConfig        `yaml:"extraction_repair" json:"extraction_repair"`
	QueryAnalysis    QueryAnalysisPipeline    `yaml:"query_analysis" json:"query_analysis"`
	Embedding        EmbeddingPipelineConfig  `yaml:"embedding" json:"embedding"`
	Rerank           RerankPipelineConfig     `yaml:"rerank" json:"rerank"`
	NarrativeInsight LLMPipelineConfig        `yaml:"narrative_insight" json:"narrative_insight"`
}

type LLMPipelineConfig struct {
	Enabled              bool              `yaml:"enabled" json:"enabled"`
	ProviderID           string            `yaml:"provider_id" json:"provider_id"`
	Model                string            `yaml:"model" json:"model"`
	Params               ModelParamsConfig `yaml:"params" json:"params"`
	Thinking             ThinkingConfig    `yaml:"thinking" json:"thinking"`
	ReasoningEffort      string            `yaml:"reasoning_effort" json:"reasoning_effort"`
	RetryOnSchemaFailure int               `yaml:"retry_on_schema_failure" json:"retry_on_schema_failure"`
	Config               map[string]any    `yaml:"config,omitempty" json:"config,omitempty"`
}

type ExtractionPipelineConfig struct {
	LLMPipelineConfig `yaml:",inline" json:",inline"`
	Mode              string                 `yaml:"mode" json:"mode"`
	Audit             ExtractionAuditConfig  `yaml:"audit" json:"audit"`
	RawLog            ExtractionRawLogConfig `yaml:"raw_log" json:"raw_log"`
}

type ExtractionAuditConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	Force   bool `yaml:"force" json:"force"`
}

type ExtractionRawLogConfig struct {
	Enabled   bool   `yaml:"enabled" json:"enabled"`
	Directory string `yaml:"directory" json:"directory"`
}

type QueryAnalysisPipeline struct {
	LLMPipelineConfig `yaml:",inline" json:",inline"`

	Mode                        string                         `yaml:"mode" json:"mode"`
	RuntimeMode                 string                         `yaml:"runtime_mode" json:"runtime_mode"`
	FallbackMode                string                         `yaml:"fallback_mode" json:"fallback_mode"`
	TimeoutMS                   int                            `yaml:"timeout_ms" json:"timeout_ms"`
	ScorerVersion               string                         `yaml:"scorer_version" json:"scorer_version"`
	RouterVersion               string                         `yaml:"router_version" json:"router_version"`
	Thresholds                  QueryAnalysisThresholdsConfig  `yaml:"thresholds" json:"thresholds"`
	Budget                      QueryAnalysisBudgetConfig      `yaml:"budget" json:"budget"`
	Diagnostics                 QueryAnalysisDiagnosticsConfig `yaml:"diagnostics" json:"diagnostics"`
	MinConfidenceToOverride     float64                        `yaml:"min_confidence_to_override" json:"min_confidence_to_override"`
	MinEntitySemanticConfidence float64                        `yaml:"min_entity_semantic_confidence" json:"min_entity_semantic_confidence"`
	MaxQueryRewrites            int                            `yaml:"max_query_rewrites" json:"max_query_rewrites"`
	MaxSemanticAnchors          int                            `yaml:"max_semantic_anchors" json:"max_semantic_anchors"`
	SemanticTotalEnergyCap      float64                        `yaml:"semantic_total_energy_cap" json:"semantic_total_energy_cap"`
	MaxGeneratedDenseWeightSum  float64                        `yaml:"max_generated_dense_weight_sum" json:"max_generated_dense_weight_sum"`
	IncludeRationaleSummary     bool                           `yaml:"include_rationale_summary" json:"include_rationale_summary"`
	DisableGeneratedDense       bool                           `yaml:"disable_generated_dense" json:"disable_generated_dense"`
}

type ModelParamsConfig struct {
	Temperature     float64 `yaml:"temperature" json:"temperature"`
	TopP            float64 `yaml:"top_p" json:"top_p"`
	MaxOutputTokens int     `yaml:"max_output_tokens" json:"max_output_tokens"`
	ResponseFormat  string  `yaml:"response_format" json:"response_format"`
	Stream          bool    `yaml:"stream" json:"stream"`
	Seed            int64   `yaml:"seed" json:"seed"`
	TimeoutMS       int     `yaml:"timeout_ms" json:"timeout_ms"`
}

type ThinkingConfig struct {
	Type string `yaml:"type" json:"type"`
}

type EmbeddingPipelineConfig struct {
	Enabled    bool              `yaml:"enabled" json:"enabled"`
	ProviderID string            `yaml:"provider_id" json:"provider_id"`
	Model      string            `yaml:"model" json:"model"`
	BatchSize  int               `yaml:"batch_size" json:"batch_size"`
	Params     ModelParamsConfig `yaml:"params" json:"params"`
}

type RerankPipelineConfig struct {
	Enabled    bool              `yaml:"enabled" json:"enabled"`
	ProviderID string            `yaml:"provider_id" json:"provider_id"`
	Model      string            `yaml:"model" json:"model"`
	TopK       int               `yaml:"top_k" json:"top_k"`
	Params     ModelParamsConfig `yaml:"params" json:"params"`
}

type QueryAnalysisThresholdsConfig struct {
	MinRuleFit                  float64 `yaml:"min_rule_fit" json:"min_rule_fit"`
	MinAnchorReadiness          float64 `yaml:"min_anchor_readiness" json:"min_anchor_readiness"`
	SemanticNeedThreshold       float64 `yaml:"semantic_need" json:"semantic_need"`
	MinComplexityForSemantic    float64 `yaml:"min_complexity_for_semantic" json:"min_complexity_for_semantic"`
	FullSemanticComplexity      float64 `yaml:"full_semantic_complexity" json:"full_semantic_complexity"`
	DecomposeSemanticComplexity float64 `yaml:"decompose_complexity" json:"decompose_complexity"`
	MinSemanticFieldConfidence  float64 `yaml:"min_semantic_field_confidence" json:"min_semantic_field_confidence"`
	MinOverrideMargin           float64 `yaml:"min_override_margin" json:"min_override_margin"`
	HighSafetyRiskThreshold     float64 `yaml:"high_safety_risk" json:"high_safety_risk"`
}

type QueryAnalysisBudgetConfig struct {
	MaxSemanticCallsPerSession         int `yaml:"max_semantic_calls_per_session" json:"max_semantic_calls_per_session"`
	MaxSemanticCallsPerSessionWindowMS int `yaml:"max_semantic_calls_per_session_window_ms" json:"max_semantic_calls_per_session_window_ms"`
	MaxSemanticCallsPer1000Queries     int `yaml:"max_semantic_calls_per_1000_queries" json:"max_semantic_calls_per_1000_queries"`
	MaxSemanticLatencyMS               int `yaml:"max_semantic_latency_ms" json:"max_semantic_latency_ms"`
}

type QueryAnalysisDiagnosticsConfig struct {
	IncludeScoreBreakdown bool    `yaml:"include_score_breakdown" json:"include_score_breakdown"`
	IncludeReasonCodes    bool    `yaml:"include_reason_codes" json:"include_reason_codes"`
	SampleRate            float64 `yaml:"sample_rate" json:"sample_rate"`
}

type WritePolicyConfig struct {
	Triggers   WriteTriggersConfig   `yaml:"triggers" json:"triggers"`
	Extraction WriteExtractionConfig `yaml:"extraction" json:"extraction"`
	Prefilter  WritePrefilterConfig  `yaml:"prefilter" json:"prefilter"`
}

type WriteTriggersConfig struct {
	IdleDetect    IdleDetectTriggerConfig `yaml:"idle_detect" json:"idle_detect"`
	SessionEnd    TriggerConfig           `yaml:"session_end" json:"session_end"`
	ManualPin     TriggerConfig           `yaml:"manual_pin" json:"manual_pin"`
	ManualForget  TriggerConfig           `yaml:"manual_forget" json:"manual_forget"`
	WorkCandidate TriggerConfig           `yaml:"work_candidate" json:"work_candidate"`
}

type TriggerConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type IdleDetectTriggerConfig struct {
	Enabled   bool `yaml:"enabled" json:"enabled"`
	MinIdleMS int  `yaml:"min_idle_ms" json:"min_idle_ms"`
}

type WriteExtractionConfig struct {
	AllowInference             bool `yaml:"allow_inference" json:"allow_inference"`
	AllowSensitiveExtraction   bool `yaml:"allow_sensitive_extraction" json:"allow_sensitive_extraction"`
	MaxFactsPerRequest         int  `yaml:"max_facts_per_request" json:"max_facts_per_request"`
	MaxLinksPerRequest         int  `yaml:"max_links_per_request" json:"max_links_per_request"`
	SensitiveReasoningMaxChars int  `yaml:"sensitive_reasoning_max_chars" json:"sensitive_reasoning_max_chars"`
}

type WritePrefilterConfig struct {
	MinMemoryWorthiness     float64 `yaml:"min_memory_worthiness" json:"min_memory_worthiness"`
	MinLongTermValue        float64 `yaml:"min_long_term_value" json:"min_long_term_value"`
	KeepManualPinAlways     bool    `yaml:"keep_manual_pin_always" json:"keep_manual_pin_always"`
	RouteManualForgetAlways bool    `yaml:"route_manual_forget_always" json:"route_manual_forget_always"`
}

type RetrievalConfig struct {
	UseFTS                bool                        `yaml:"use_fts" json:"use_fts"`
	UseMirror             bool                        `yaml:"use_mirror" json:"use_mirror"`
	AllowHistorical       bool                        `yaml:"allow_historical" json:"allow_historical"`
	AllowDeepArchive      bool                        `yaml:"allow_deep_archive" json:"allow_deep_archive"`
	SensitivityPermission string                      `yaml:"sensitivity_permission" json:"sensitivity_permission"`
	FinalMemoryCount      int                         `yaml:"final_memory_count" json:"final_memory_count"`
	ContextBudgetTokens   int                         `yaml:"context_budget_tokens" json:"context_budget_tokens"`
	Anchor                RetrievalAnchorConfig       `yaml:"anchor" json:"anchor"`
	Activation            RetrievalActivationConfig   `yaml:"activation" json:"activation"`
	Fatigue               RetrievalFatigueConfig      `yaml:"fatigue" json:"fatigue"`
	Cooccurrence          RetrievalCooccurrenceConfig `yaml:"cooccurrence" json:"cooccurrence"`
	Ranking               RetrievalRankingConfig      `yaml:"ranking" json:"ranking"`
	MMR                   RetrievalMMRConfig          `yaml:"mmr" json:"mmr"`
	Prompt                RetrievalPromptConfig       `yaml:"prompt" json:"prompt"`
}

type RetrievalAnchorConfig struct {
	MaxEntityAnchors     int     `yaml:"max_entity_anchors" json:"max_entity_anchors"`
	MaxSparseAnchors     int     `yaml:"max_sparse_anchors" json:"max_sparse_anchors"`
	MaxDenseAnchors      int     `yaml:"max_dense_anchors" json:"max_dense_anchors"`
	MaxPinnedCoreAnchors int     `yaml:"max_pinned_core_anchors" json:"max_pinned_core_anchors"`
	MaxRecentAnchors     int     `yaml:"max_recent_anchors" json:"max_recent_anchors"`
	MaxNarrativeAnchors  int     `yaml:"max_narrative_anchors" json:"max_narrative_anchors"`
	EntityExactMinEnergy float64 `yaml:"entity_exact_min_energy" json:"entity_exact_min_energy"`
	PinnedCoreMinEnergy  float64 `yaml:"pinned_core_min_energy" json:"pinned_core_min_energy"`
	SeedEnergyCapPerNode float64 `yaml:"seed_energy_cap_per_node" json:"seed_energy_cap_per_node"`
}

type RetrievalActivationConfig struct {
	MaxHops                      int     `yaml:"max_hops" json:"max_hops"`
	MaxHopsForHistoricalOrCausal int     `yaml:"max_hops_for_historical_or_causal" json:"max_hops_for_historical_or_causal"`
	HopDecay                     float64 `yaml:"hop_decay" json:"hop_decay"`
	TeleportAlpha                float64 `yaml:"teleport_alpha" json:"teleport_alpha"`
	MinEnergyThreshold           float64 `yaml:"min_energy_threshold" json:"min_energy_threshold"`
	MaxActiveNodes               int     `yaml:"max_active_nodes" json:"max_active_nodes"`
	HubPower                     float64 `yaml:"hub_power" json:"hub_power"`
	AllowNegativeEdges           bool    `yaml:"allow_negative_edges" json:"allow_negative_edges"`
}

type RetrievalFatigueConfig struct {
	WindowTurns       int     `yaml:"window_turns" json:"window_turns"`
	Factor            float64 `yaml:"factor" json:"factor"`
	FactorForRepeated float64 `yaml:"factor_for_repeated" json:"factor_for_repeated"`
}

type RetrievalCooccurrenceConfig struct {
	Enabled  bool `yaml:"enabled" json:"enabled"`
	MaxPairs int  `yaml:"max_pairs" json:"max_pairs"`
}

type RetrievalRankingConfig struct {
	CandidatePoolSize    int     `yaml:"candidate_pool_size" json:"candidate_pool_size"`
	MinFinalScore        float64 `yaml:"min_final_score" json:"min_final_score"`
	AgentAffectWeightCap float64 `yaml:"agent_affect_weight_cap" json:"agent_affect_weight_cap"`
}

type RetrievalMMRConfig struct {
	Enabled            bool    `yaml:"enabled" json:"enabled"`
	Lambda             float64 `yaml:"lambda" json:"lambda"`
	DuplicateThreshold float64 `yaml:"duplicate_threshold" json:"duplicate_threshold"`
}

type RetrievalPromptConfig struct {
	MaxSourceEpisodeQuotes int  `yaml:"max_source_episode_quotes" json:"max_source_episode_quotes"`
	QuoteByDefault         bool `yaml:"quote_by_default" json:"quote_by_default"`
}

type SidecarConfig struct {
	Enabled             bool                          `yaml:"enabled" json:"enabled"`
	URL                 string                        `yaml:"url" json:"url"`
	Adapter             string                        `yaml:"adapter" json:"adapter"`
	TotalTimeoutMS      int                           `yaml:"total_timeout_ms" json:"total_timeout_ms"`
	MirrorTimeoutMS     int                           `yaml:"mirror_timeout_ms" json:"mirror_timeout_ms"`
	ActivationTimeoutMS int                           `yaml:"activation_timeout_ms" json:"activation_timeout_ms"`
	RerankTimeoutMS     int                           `yaml:"rerank_timeout_ms" json:"rerank_timeout_ms"`
	CircuitBreaker      SidecarCircuitBreakerConfig   `yaml:"circuit_breaker" json:"circuit_breaker"`
	ActivationBudget    SidecarActivationBudgetConfig `yaml:"activation_budget" json:"activation_budget"`
}

type SidecarCircuitBreakerConfig struct {
	Enabled          bool `yaml:"enabled" json:"enabled"`
	Window           int  `yaml:"window" json:"window"`
	FailureThreshold int  `yaml:"failure_threshold" json:"failure_threshold"`
	OpenMS           int  `yaml:"open_ms" json:"open_ms"`
}

type SidecarActivationBudgetConfig struct {
	MaxEdgesScannedPerRequest int `yaml:"max_edges_scanned_per_request" json:"max_edges_scanned_per_request"`
	MaxNeighborsPerNode       int `yaml:"max_neighbors_per_node" json:"max_neighbors_per_node"`
	MaxWallMS                 int `yaml:"max_wall_ms" json:"max_wall_ms"`
}

type MirrorConfig struct {
	Enabled             bool `yaml:"enabled" json:"enabled"`
	SyncLimit           int  `yaml:"sync_limit" json:"sync_limit"`
	RebuildOnStart      bool `yaml:"rebuild_on_start" json:"rebuild_on_start"`
	StaleLagThresholdMS int  `yaml:"stale_lag_threshold_ms" json:"stale_lag_threshold_ms"`
}

type RetentionConfig struct {
	Jobs       RetentionJobsConfig       `yaml:"jobs" json:"jobs"`
	Thresholds RetentionThresholdsConfig `yaml:"thresholds" json:"thresholds"`
	AutoDelete bool                      `yaml:"auto_delete" json:"auto_delete"`
}

type RetentionJobsConfig struct {
	LazyDecay            bool `yaml:"lazy_decay" json:"lazy_decay"`
	DailyTTLExpiry       bool `yaml:"daily_ttl_expiry" json:"daily_ttl_expiry"`
	DailyStateTransition bool `yaml:"daily_state_transition" json:"daily_state_transition"`
	WeeklyCompression    bool `yaml:"weekly_compression" json:"weekly_compression"`
	MonthlyArchive       bool `yaml:"monthly_archive" json:"monthly_archive"`
	MirrorCompaction     bool `yaml:"mirror_compaction" json:"mirror_compaction"`
}

type RetentionThresholdsConfig struct {
	ActiveToDormant         float64 `yaml:"active_to_dormant" json:"active_to_dormant"`
	DormantToArchived       float64 `yaml:"dormant_to_archived" json:"dormant_to_archived"`
	ArchivedToDeepThreshold float64 `yaml:"archived_to_deep_threshold" json:"archived_to_deep_threshold"`
	DeepArchiveAfterDays    int     `yaml:"deep_archive_after_days" json:"deep_archive_after_days"`
}

type ForgettingPrivacyConfig struct {
	DefaultForgetLevel               string                  `yaml:"default_forget_level" json:"default_forget_level"`
	RequireConfirmationForPurge      bool                    `yaml:"require_confirmation_for_purge" json:"require_confirmation_for_purge"`
	RequireConfirmationForTopicScope bool                    `yaml:"require_confirmation_for_topic_scope" json:"require_confirmation_for_topic_scope"`
	Cleanup                          ForgettingCleanupConfig `yaml:"cleanup" json:"cleanup"`
}

type ForgettingCleanupConfig struct {
	DeleteTriviumNodes          bool `yaml:"delete_trivium_nodes" json:"delete_trivium_nodes"`
	DeleteSQLiteSearchDocuments bool `yaml:"delete_sqlite_search_documents" json:"delete_sqlite_search_documents"`
	CleanAgentAffectRefs        bool `yaml:"clean_agent_affect_refs" json:"clean_agent_affect_refs"`
	RecomputeDerived            bool `yaml:"recompute_derived" json:"recompute_derived"`
	VerifyAfterDelete           bool `yaml:"verify_after_delete" json:"verify_after_delete"`
}

type AgentAffectConfig struct {
	Enabled         bool                       `yaml:"enabled" json:"enabled"`
	StorageEnabled  bool                       `yaml:"storage_enabled" json:"storage_enabled"`
	ServiceMode     string                     `yaml:"service_mode" json:"service_mode"`
	DefaultProfile  string                     `yaml:"default_profile" json:"default_profile"`
	NeutralFallback bool                       `yaml:"neutral_fallback" json:"neutral_fallback"`
	Safety          AgentAffectSafetyConfig    `yaml:"safety" json:"safety"`
	Retrieval       AgentAffectRetrievalConfig `yaml:"retrieval" json:"retrieval"`
}

type AgentAffectSafetyConfig struct {
	AllowUserFactWrites    bool   `yaml:"allow_user_fact_writes" json:"allow_user_fact_writes"`
	AllowSensitivityBypass bool   `yaml:"allow_sensitivity_bypass" json:"allow_sensitivity_bypass"`
	AllowForgetBypass      bool   `yaml:"allow_forget_bypass" json:"allow_forget_bypass"`
	MoodSafety             string `yaml:"mood_safety" json:"mood_safety"`
}

type AgentAffectRetrievalConfig struct {
	WeightCap              float64 `yaml:"weight_cap" json:"weight_cap"`
	SensitiveRecall        string  `yaml:"sensitive_recall" json:"sensitive_recall"`
	NegativeRetentionBoost bool    `yaml:"negative_retention_boost" json:"negative_retention_boost"`
}

type ObservabilityConfig struct {
	MetricsEnabled         bool `yaml:"metrics_enabled" json:"metrics_enabled"`
	IncludeScoreBreakdown  bool `yaml:"include_score_breakdown" json:"include_score_breakdown"`
	IncludeActivationPaths bool `yaml:"include_activation_paths" json:"include_activation_paths"`
	LogSanitizedDebug      bool `yaml:"log_sanitized_debug" json:"log_sanitized_debug"`
}

type EvalConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type RuntimeValidationOptions struct {
	CheckEnv bool
	Env      func(string) string
}

type ConfigOverrides struct {
	Enabled   *bool
	Core      *CoreOverrides
	Retrieval *RetrievalOverrides
	Pipelines *PipelineOverrides
	Sidecar   *SidecarOverrides
	Mirror    *MirrorOverrides
	Retention *RetentionOverrides
}

type CoreOverrides struct {
	DBPath    *string
	PersonaID *string
}

type RetrievalOverrides struct {
	FinalMemoryCount    *int
	ContextBudgetTokens *int
	UseFTS              *bool
	UseMirror           *bool
}

type PipelineOverrides struct {
	Extraction    *LLMPipelineOverrides
	QueryAnalysis *LLMPipelineOverrides
}

type LLMPipelineOverrides struct {
	ProviderID *string
	Model      *string
}

type SidecarOverrides struct {
	Enabled *bool
	URL     *string
	Adapter *string
}

type MirrorOverrides struct {
	Enabled   *bool
	SyncLimit *int
}

type RetentionOverrides struct {
	DeepArchiveAfterDays *int
}

type ProviderRegistry struct {
	LLM      []ProviderMapping
	Embedder []ProviderMapping
	Reranker []ProviderMapping
}

type ProviderMapping struct {
	ID        string
	Provider  string
	Protocol  string
	BaseURL   string
	APIKeyEnv string
	Enabled   bool
	TimeoutMS int
}

type LoadEffectiveOptions struct {
	ConfigPath       string
	Overrides        ConfigOverrides
	ProviderRegistry ProviderRegistry
	Runtime          RuntimeValidationOptions
}

type ConfiguredRuntime struct {
	Config          Config
	Options         memorycore.Options
	RetrievalPolicy memorycore.RetrievalPolicy
	RetentionJobs   []memorycore.RetentionJobName
	MirrorSyncLimit int
}

type ConfigOpenOptions struct {
	ConfigPath       string
	Overrides        ConfigOverrides
	ProviderRegistry ProviderRegistry
	Runtime          RuntimeValidationOptions
	Now              func() time.Time
}

type ConfiguredService struct {
	memorycore.Service
	Config          Config
	RetrievalPolicy memorycore.RetrievalPolicy
	RetentionJobs   []memorycore.RetentionJobName
	MirrorSyncLimit int
}

func DefaultConfig() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Enabled:       false,
		Core: CoreConfig{
			DBPath:      "./data/memory.db",
			PersonaID:   "default",
			AutoMigrate: true,
			EnableFTS:   true,
			Timezone:    "Asia/Shanghai",
		},
		Providers: ProvidersConfig{
			LLM: []ProviderConfig{{
				ID:        "default_llm",
				Provider:  "disabled",
				Protocol:  "openai_compatible",
				Enabled:   false,
				TimeoutMS: 30000,
				Retry:     RetryConfig{MaxAttempts: 2, BackoffMS: 500},
			}},
			Embedder: []ProviderConfig{{ID: "default_embedder", Provider: "disabled", Enabled: false}},
			Reranker: []ProviderConfig{{ID: "default_reranker", Provider: "disabled", Enabled: false}},
		},
		Pipelines: PipelinesConfig{
			Prefilter: LLMPipelineConfig{
				Enabled:  false,
				Params:   defaultParams(1200),
				Thinking: ThinkingConfig{Type: "disabled"},
			},
			Extraction: ExtractionPipelineConfig{
				LLMPipelineConfig: LLMPipelineConfig{
					Enabled:              false,
					Params:               defaultParams(6000),
					Thinking:             ThinkingConfig{Type: "disabled"},
					RetryOnSchemaFailure: 1,
				},
				Mode:  string(memorycore.ExtractionRunModeDryRun),
				Audit: ExtractionAuditConfig{Enabled: true},
			},
			ExtractionRepair: LLMPipelineConfig{
				Enabled:  false,
				Params:   defaultParams(1200),
				Thinking: ThinkingConfig{Type: "disabled"},
			},
			QueryAnalysis: QueryAnalysisPipeline{
				LLMPipelineConfig: LLMPipelineConfig{
					Enabled:  false,
					Params:   defaultParams(768),
					Thinking: ThinkingConfig{Type: "disabled"},
				},
				Mode:          "rule_only",
				RuntimeMode:   "rule_only",
				FallbackMode:  "rule_only",
				TimeoutMS:     1500,
				ScorerVersion: "query_analysis_scorer_v1",
				RouterVersion: "semantic_router_v1",
				Thresholds: QueryAnalysisThresholdsConfig{
					MinRuleFit:                  0.66,
					MinAnchorReadiness:          0.45,
					SemanticNeedThreshold:       0.58,
					MinComplexityForSemantic:    0.50,
					FullSemanticComplexity:      0.72,
					DecomposeSemanticComplexity: 0.80,
					MinSemanticFieldConfidence:  0.70,
					MinOverrideMargin:           0.08,
					HighSafetyRiskThreshold:     0.80,
				},
				Budget: QueryAnalysisBudgetConfig{
					MaxSemanticCallsPerSession:         8,
					MaxSemanticCallsPerSessionWindowMS: 30 * 60 * 1000,
					MaxSemanticCallsPer1000Queries:     250,
					MaxSemanticLatencyMS:               1500,
				},
				Diagnostics: QueryAnalysisDiagnosticsConfig{
					IncludeScoreBreakdown: true,
					IncludeReasonCodes:    true,
					SampleRate:            1.0,
				},
				MinConfidenceToOverride:     0.72,
				MinEntitySemanticConfidence: 0.70,
				MaxQueryRewrites:            5,
				MaxSemanticAnchors:          8,
				SemanticTotalEnergyCap:      5.0,
				MaxGeneratedDenseWeightSum:  3.0,
			},
			Embedding: EmbeddingPipelineConfig{
				Enabled:    false,
				ProviderID: "default_embedder",
				BatchSize:  32,
			},
			Rerank: RerankPipelineConfig{
				Enabled:    false,
				ProviderID: "default_reranker",
				TopK:       20,
			},
			NarrativeInsight: LLMPipelineConfig{
				Enabled:  false,
				Params:   defaultParams(1200),
				Thinking: ThinkingConfig{Type: "disabled"},
			},
		},
		WritePolicy: WritePolicyConfig{
			Triggers: WriteTriggersConfig{
				IdleDetect:    IdleDetectTriggerConfig{Enabled: true, MinIdleMS: 8000},
				SessionEnd:    TriggerConfig{Enabled: true},
				ManualPin:     TriggerConfig{Enabled: true},
				ManualForget:  TriggerConfig{Enabled: true},
				WorkCandidate: TriggerConfig{Enabled: true},
			},
			Extraction: WriteExtractionConfig{
				AllowInference:             true,
				AllowSensitiveExtraction:   false,
				MaxFactsPerRequest:         12,
				MaxLinksPerRequest:         20,
				SensitiveReasoningMaxChars: 0,
			},
			Prefilter: WritePrefilterConfig{
				MinMemoryWorthiness:     0.55,
				MinLongTermValue:        0.45,
				KeepManualPinAlways:     true,
				RouteManualForgetAlways: true,
			},
		},
		Retrieval: RetrievalConfig{
			UseFTS:                true,
			UseMirror:             false,
			AllowHistorical:       false,
			AllowDeepArchive:      false,
			SensitivityPermission: memorycore.SensitivityNormal,
			FinalMemoryCount:      8,
			ContextBudgetTokens:   1200,
			Anchor: RetrievalAnchorConfig{
				MaxEntityAnchors:     20,
				MaxSparseAnchors:     30,
				MaxDenseAnchors:      30,
				MaxPinnedCoreAnchors: 10,
				MaxRecentAnchors:     10,
				MaxNarrativeAnchors:  10,
				EntityExactMinEnergy: 0.75,
				PinnedCoreMinEnergy:  0.70,
				SeedEnergyCapPerNode: 1.0,
			},
			Activation: RetrievalActivationConfig{
				MaxHops:                      2,
				MaxHopsForHistoricalOrCausal: 3,
				HopDecay:                     0.75,
				TeleportAlpha:                0.15,
				MinEnergyThreshold:           0.015,
				MaxActiveNodes:               1000,
				HubPower:                     0.55,
				AllowNegativeEdges:           true,
			},
			Fatigue: RetrievalFatigueConfig{
				WindowTurns:       5,
				Factor:            0.35,
				FactorForRepeated: 0.20,
			},
			Cooccurrence: RetrievalCooccurrenceConfig{Enabled: true, MaxPairs: 100},
			Ranking: RetrievalRankingConfig{
				CandidatePoolSize:    80,
				MinFinalScore:        0.20,
				AgentAffectWeightCap: 0.03,
			},
			MMR: RetrievalMMRConfig{
				Enabled:            true,
				Lambda:             0.72,
				DuplicateThreshold: 0.88,
			},
			Prompt: RetrievalPromptConfig{MaxSourceEpisodeQuotes: 2, QuoteByDefault: false},
		},
		Sidecar: SidecarConfig{
			Enabled:             false,
			URL:                 "",
			Adapter:             "trivium",
			TotalTimeoutMS:      2500,
			MirrorTimeoutMS:     1200,
			ActivationTimeoutMS: 1500,
			RerankTimeoutMS:     1200,
			CircuitBreaker: SidecarCircuitBreakerConfig{
				Enabled:          true,
				Window:           20,
				FailureThreshold: 3,
				OpenMS:           60000,
			},
			ActivationBudget: SidecarActivationBudgetConfig{
				MaxEdgesScannedPerRequest: 10000,
				MaxNeighborsPerNode:       100,
				MaxWallMS:                 120,
			},
		},
		Mirror: MirrorConfig{
			Enabled:             false,
			SyncLimit:           100,
			RebuildOnStart:      false,
			StaleLagThresholdMS: 30000,
		},
		Retention: RetentionConfig{
			Jobs: RetentionJobsConfig{
				LazyDecay:            true,
				DailyTTLExpiry:       true,
				DailyStateTransition: true,
				WeeklyCompression:    false,
				MonthlyArchive:       false,
				MirrorCompaction:     true,
			},
			Thresholds: RetentionThresholdsConfig{
				ActiveToDormant:         0.35,
				DormantToArchived:       0.20,
				ArchivedToDeepThreshold: 0.18,
				DeepArchiveAfterDays:    0,
			},
			AutoDelete: false,
		},
		ForgettingPrivacy: ForgettingPrivacyConfig{
			DefaultForgetLevel:               "soft_forget",
			RequireConfirmationForPurge:      true,
			RequireConfirmationForTopicScope: true,
			Cleanup: ForgettingCleanupConfig{
				DeleteTriviumNodes:          true,
				DeleteSQLiteSearchDocuments: true,
				CleanAgentAffectRefs:        true,
				RecomputeDerived:            true,
				VerifyAfterDelete:           true,
			},
		},
		AgentAffect: AgentAffectConfig{
			Enabled:         false,
			StorageEnabled:  true,
			ServiceMode:     "local_stub",
			DefaultProfile:  "default",
			NeutralFallback: true,
			Safety: AgentAffectSafetyConfig{
				AllowUserFactWrites:    false,
				AllowSensitivityBypass: false,
				AllowForgetBypass:      false,
				MoodSafety:             "conservative",
			},
			Retrieval: AgentAffectRetrievalConfig{
				WeightCap:              0.03,
				SensitiveRecall:        "disallow",
				NegativeRetentionBoost: false,
			},
		},
		Observability: ObservabilityConfig{
			MetricsEnabled:         true,
			IncludeScoreBreakdown:  false,
			IncludeActivationPaths: false,
			LogSanitizedDebug:      true,
		},
		Eval: &EvalConfig{Enabled: false},
	}
}

func Default() Config {
	return DefaultConfig()
}

func defaultParams(maxOutputTokens int) ModelParamsConfig {
	return ModelParamsConfig{
		Temperature:     0,
		TopP:            1,
		MaxOutputTokens: maxOutputTokens,
		ResponseFormat:  "json_schema",
	}
}

func (c Config) Validate() error {
	if c.SchemaVersion != "" && c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q", SchemaVersion)
	}
	if c.Enabled && strings.TrimSpace(c.Core.DBPath) == "" {
		return fmt.Errorf("core.db_path is required when enabled=true")
	}
	if c.Retrieval.FinalMemoryCount <= 0 {
		return fmt.Errorf("retrieval.final_memory_count must be > 0")
	}
	if c.Retrieval.ContextBudgetTokens <= 0 {
		return fmt.Errorf("retrieval.context_budget_tokens must be > 0")
	}
	switch c.Retrieval.SensitivityPermission {
	case memorycore.SensitivityNormal, memorycore.SensitivitySensitive, memorycore.SensitivityHighlySensitive:
	default:
		return fmt.Errorf("retrieval.sensitivity_permission must be one of normal|sensitive|highly_sensitive")
	}
	if c.Retrieval.Ranking.AgentAffectWeightCap > 0.03 {
		return fmt.Errorf("retrieval.ranking.agent_affect_weight_cap must be <= 0.03")
	}
	if c.Retrieval.MMR.Lambda < 0 || c.Retrieval.MMR.Lambda > 1 {
		return fmt.Errorf("retrieval.mmr.lambda must be within [0, 1]")
	}
	if err := c.validateProviders(); err != nil {
		return err
	}
	if err := c.validatePipelines(); err != nil {
		return err
	}
	if c.WritePolicy.Extraction.MaxFactsPerRequest <= 0 {
		return fmt.Errorf("write_policy.extraction.max_facts_per_request must be > 0")
	}
	if c.WritePolicy.Extraction.MaxLinksPerRequest <= 0 {
		return fmt.Errorf("write_policy.extraction.max_links_per_request must be > 0")
	}
	if err := c.validateSidecar(); err != nil {
		return err
	}
	if c.Mirror.SyncLimit <= 0 {
		return fmt.Errorf("mirror.sync_limit must be > 0")
	}
	if c.Retention.Jobs.MonthlyArchive && c.Retention.Thresholds.DeepArchiveAfterDays <= 0 {
		return fmt.Errorf("retention.thresholds.deep_archive_after_days must be > 0 when retention.jobs.monthly_archive=true")
	}
	if c.Retention.AutoDelete {
		return fmt.Errorf("retention.auto_delete cannot be true in this phase")
	}
	if err := c.validateAgentAffect(); err != nil {
		return err
	}
	return nil
}

func (c Config) validateProviders() error {
	ids := map[string]bool{}
	for _, group := range []struct {
		name      string
		providers []ProviderConfig
	}{
		{"providers.llm", c.Providers.LLM},
		{"providers.embedder", c.Providers.Embedder},
		{"providers.reranker", c.Providers.Reranker},
	} {
		for idx, provider := range group.providers {
			path := fmt.Sprintf("%s[%d]", group.name, idx)
			if strings.TrimSpace(provider.ID) == "" {
				return fmt.Errorf("%s.id is required", path)
			}
			if ids[provider.ID] {
				return fmt.Errorf("provider id %q is duplicated", provider.ID)
			}
			ids[provider.ID] = true
			if provider.TimeoutMS < 0 {
				return fmt.Errorf("%s.timeout_ms must be >= 0", path)
			}
			if provider.Retry.MaxAttempts < 0 || provider.Retry.BackoffMS < 0 {
				return fmt.Errorf("%s.retry values must be >= 0", path)
			}
			if err := rejectSecretMap(path+".extra", provider.Extra); err != nil {
				return err
			}
			if err := rejectSecretMap(path+".config", provider.Config); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c Config) validatePipelines() error {
	if err := validateLLMPipeline("pipelines.prefilter", c.Pipelines.Prefilter, c, false); err != nil {
		return err
	}
	if err := validateLLMPipeline("pipelines.extraction", c.Pipelines.Extraction.LLMPipelineConfig, c, false); err != nil {
		return err
	}
	if _, err := normalizeExtractionConfigMode(c.Pipelines.Extraction.Mode); err != nil {
		return err
	}
	if c.Pipelines.Extraction.RawLog.Enabled && strings.TrimSpace(c.Pipelines.Extraction.RawLog.Directory) == "" {
		return fmt.Errorf("pipelines.extraction.raw_log.directory is required when raw_log.enabled is true")
	}
	if err := validateLLMPipeline("pipelines.extraction_repair", c.Pipelines.ExtractionRepair, c, false); err != nil {
		return err
	}
	if err := validateLLMPipeline("pipelines.narrative_insight", c.Pipelines.NarrativeInsight, c, false); err != nil {
		return err
	}
	if err := validateQueryAnalysisPipeline(c.Pipelines.QueryAnalysis, c); err != nil {
		return err
	}
	if c.Pipelines.Embedding.Enabled && c.ProviderByID(c.Pipelines.Embedding.ProviderID) == nil {
		return fmt.Errorf("pipelines.embedding.provider_id %q does not exist", c.Pipelines.Embedding.ProviderID)
	}
	if c.Pipelines.Embedding.BatchSize < 0 {
		return fmt.Errorf("pipelines.embedding.batch_size must be >= 0")
	}
	if c.Pipelines.Rerank.Enabled && c.ProviderByID(c.Pipelines.Rerank.ProviderID) == nil {
		return fmt.Errorf("pipelines.rerank.provider_id %q does not exist", c.Pipelines.Rerank.ProviderID)
	}
	if c.Pipelines.Rerank.TopK < 0 {
		return fmt.Errorf("pipelines.rerank.top_k must be >= 0")
	}
	return nil
}

func validateLLMPipeline(path string, pipeline LLMPipelineConfig, cfg Config, requireProvider bool) error {
	if pipeline.Enabled || requireProvider {
		if strings.TrimSpace(pipeline.ProviderID) == "" {
			return fmt.Errorf("%s.provider_id is required when enabled=true", path)
		}
		provider := cfg.ProviderByID(pipeline.ProviderID)
		if provider == nil {
			return fmt.Errorf("%s.provider_id %q does not exist", path, pipeline.ProviderID)
		}
		if !provider.Enabled {
			return fmt.Errorf("%s.provider_id %q references a disabled provider", path, pipeline.ProviderID)
		}
	}
	if err := validateModelParams(path+".params", pipeline.Params); err != nil {
		return err
	}
	switch pipeline.Thinking.Type {
	case "", "enabled", "disabled":
	default:
		return fmt.Errorf("%s.thinking.type must be enabled or disabled", path)
	}
	if pipeline.RetryOnSchemaFailure < 0 {
		return fmt.Errorf("%s.retry_on_schema_failure must be >= 0", path)
	}
	return rejectSecretMap(path+".config", pipeline.Config)
}

func validateQueryAnalysisPipeline(pipeline QueryAnalysisPipeline, cfg Config) error {
	if err := validateLLMPipeline("pipelines.query_analysis", pipeline.LLMPipelineConfig, cfg, pipeline.Mode != "rule_only" && pipeline.Mode != ""); err != nil {
		return err
	}
	switch pipeline.Mode {
	case "", "rule_only", "rule_then_llm", "llm_only", "sidecar":
	default:
		return fmt.Errorf("pipelines.query_analysis.mode must be one of rule_only|rule_then_llm|llm_only|sidecar")
	}
	if pipeline.FallbackMode != "rule_only" {
		return fmt.Errorf("pipelines.query_analysis.fallback_mode must be rule_only")
	}
	switch runtimeQueryAnalysisMode(pipeline) {
	case "rule_only", "semantic_always", "semantic_on_low_confidence", "semantic_rewrite_only",
		"legacy_only", "shadow_adaptive", "adaptive", "adaptive_safe", "adaptive_full":
	default:
		return fmt.Errorf("pipelines.query_analysis.runtime_mode is invalid")
	}
	if pipeline.TimeoutMS <= 0 {
		return fmt.Errorf("pipelines.query_analysis.timeout_ms must be > 0")
	}
	if strings.TrimSpace(pipeline.ScorerVersion) == "" {
		return fmt.Errorf("pipelines.query_analysis.scorer_version is required")
	}
	if strings.TrimSpace(pipeline.RouterVersion) == "" {
		return fmt.Errorf("pipelines.query_analysis.router_version is required")
	}
	if err := validateQueryAnalysisThresholds(pipeline.Thresholds); err != nil {
		return err
	}
	if err := validateQueryAnalysisBudget(pipeline.Budget); err != nil {
		return err
	}
	if pipeline.Diagnostics.SampleRate < 0 || pipeline.Diagnostics.SampleRate > 1 {
		return fmt.Errorf("pipelines.query_analysis.diagnostics.sample_rate must be within [0, 1]")
	}
	if pipeline.MinConfidenceToOverride <= 0 || pipeline.MinConfidenceToOverride > 1 {
		return fmt.Errorf("pipelines.query_analysis.min_confidence_to_override must be within (0, 1]")
	}
	if pipeline.MinEntitySemanticConfidence <= 0 || pipeline.MinEntitySemanticConfidence > 1 {
		return fmt.Errorf("pipelines.query_analysis.min_entity_semantic_confidence must be within (0, 1]")
	}
	if pipeline.MaxQueryRewrites <= 0 {
		return fmt.Errorf("pipelines.query_analysis.max_query_rewrites must be > 0")
	}
	if pipeline.MaxSemanticAnchors <= 0 {
		return fmt.Errorf("pipelines.query_analysis.max_semantic_anchors must be > 0")
	}
	if pipeline.SemanticTotalEnergyCap <= 0 {
		return fmt.Errorf("pipelines.query_analysis.semantic_total_energy_cap must be > 0")
	}
	if pipeline.MaxGeneratedDenseWeightSum <= 0 {
		return fmt.Errorf("pipelines.query_analysis.max_generated_dense_weight_sum must be > 0")
	}
	return nil
}

func validateModelParams(path string, params ModelParamsConfig) error {
	if params.TopP < 0 || params.TopP > 1 {
		return fmt.Errorf("%s.top_p must be within [0, 1]", path)
	}
	if params.Temperature < 0 {
		return fmt.Errorf("%s.temperature must be >= 0", path)
	}
	if params.MaxOutputTokens < 0 {
		return fmt.Errorf("%s.max_output_tokens must be >= 0", path)
	}
	if params.TimeoutMS < 0 {
		return fmt.Errorf("%s.timeout_ms must be >= 0", path)
	}
	return nil
}

func validateUnitInterval(name string, value float64) error {
	if value <= 0 || value > 1 {
		return fmt.Errorf("%s must be within (0, 1]", name)
	}
	return nil
}

func validateQueryAnalysisThresholds(thresholds QueryAnalysisThresholdsConfig) error {
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.min_rule_fit", thresholds.MinRuleFit); err != nil {
		return err
	}
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.min_anchor_readiness", thresholds.MinAnchorReadiness); err != nil {
		return err
	}
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.semantic_need", thresholds.SemanticNeedThreshold); err != nil {
		return err
	}
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.min_complexity_for_semantic", thresholds.MinComplexityForSemantic); err != nil {
		return err
	}
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.full_semantic_complexity", thresholds.FullSemanticComplexity); err != nil {
		return err
	}
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.decompose_complexity", thresholds.DecomposeSemanticComplexity); err != nil {
		return err
	}
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.min_semantic_field_confidence", thresholds.MinSemanticFieldConfidence); err != nil {
		return err
	}
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.min_override_margin", thresholds.MinOverrideMargin); err != nil {
		return err
	}
	if err := validateUnitInterval("pipelines.query_analysis.thresholds.high_safety_risk", thresholds.HighSafetyRiskThreshold); err != nil {
		return err
	}
	return nil
}

func validateQueryAnalysisBudget(budget QueryAnalysisBudgetConfig) error {
	if budget.MaxSemanticCallsPerSession <= 0 {
		return fmt.Errorf("pipelines.query_analysis.budget.max_semantic_calls_per_session must be > 0")
	}
	if budget.MaxSemanticCallsPerSessionWindowMS <= 0 {
		return fmt.Errorf("pipelines.query_analysis.budget.max_semantic_calls_per_session_window_ms must be > 0")
	}
	if budget.MaxSemanticCallsPer1000Queries <= 0 || budget.MaxSemanticCallsPer1000Queries > 1000 {
		return fmt.Errorf("pipelines.query_analysis.budget.max_semantic_calls_per_1000_queries must be within [1, 1000]")
	}
	if budget.MaxSemanticLatencyMS <= 0 {
		return fmt.Errorf("pipelines.query_analysis.budget.max_semantic_latency_ms must be > 0")
	}
	return nil
}

func (c Config) validateSidecar() error {
	switch c.Sidecar.Adapter {
	case "fake", "trivium":
	default:
		return fmt.Errorf("sidecar.adapter must be one of fake|trivium")
	}
	if c.Sidecar.Enabled && c.Sidecar.Adapter == "trivium" && strings.TrimSpace(c.Sidecar.URL) == "" {
		return fmt.Errorf("sidecar.url is required when sidecar.enabled=true and sidecar.adapter=trivium")
	}
	if strings.TrimSpace(c.Sidecar.URL) != "" {
		if err := memorycore.ValidateSidecarLoopbackURL(c.Sidecar.URL); err != nil {
			return fmt.Errorf("sidecar.url must be a loopback HTTP URL: %w", err)
		}
	}
	if c.Sidecar.TotalTimeoutMS <= 0 {
		return fmt.Errorf("sidecar.total_timeout_ms must be > 0")
	}
	if c.Sidecar.MirrorTimeoutMS <= 0 {
		return fmt.Errorf("sidecar.mirror_timeout_ms must be > 0")
	}
	if c.Sidecar.ActivationTimeoutMS <= 0 {
		return fmt.Errorf("sidecar.activation_timeout_ms must be > 0")
	}
	if c.Sidecar.RerankTimeoutMS <= 0 {
		return fmt.Errorf("sidecar.rerank_timeout_ms must be > 0")
	}
	if c.Sidecar.CircuitBreaker.Enabled {
		if c.Sidecar.CircuitBreaker.Window <= 0 {
			return fmt.Errorf("sidecar.circuit_breaker.window must be > 0")
		}
		if c.Sidecar.CircuitBreaker.FailureThreshold <= 0 {
			return fmt.Errorf("sidecar.circuit_breaker.failure_threshold must be > 0")
		}
		if c.Sidecar.CircuitBreaker.OpenMS <= 0 {
			return fmt.Errorf("sidecar.circuit_breaker.open_ms must be > 0")
		}
	}
	if c.Sidecar.ActivationBudget.MaxEdgesScannedPerRequest <= 0 {
		return fmt.Errorf("sidecar.activation_budget.max_edges_scanned_per_request must be > 0")
	}
	if c.Sidecar.ActivationBudget.MaxNeighborsPerNode <= 0 {
		return fmt.Errorf("sidecar.activation_budget.max_neighbors_per_node must be > 0")
	}
	if c.Sidecar.ActivationBudget.MaxWallMS <= 0 {
		return fmt.Errorf("sidecar.activation_budget.max_wall_ms must be > 0")
	}
	return nil
}

func (c Config) validateAgentAffect() error {
	if c.AgentAffect.Safety.AllowUserFactWrites {
		return fmt.Errorf("agent_affect.safety.allow_user_fact_writes cannot be true")
	}
	if c.AgentAffect.Safety.AllowSensitivityBypass {
		return fmt.Errorf("agent_affect.safety.allow_sensitivity_bypass cannot be true")
	}
	if c.AgentAffect.Safety.AllowForgetBypass {
		return fmt.Errorf("agent_affect.safety.allow_forget_bypass cannot be true")
	}
	if c.AgentAffect.Retrieval.WeightCap < 0 || c.AgentAffect.Retrieval.WeightCap > 0.03 {
		return fmt.Errorf("agent_affect.retrieval.weight_cap must be within [0, 0.03]")
	}
	if c.AgentAffect.Retrieval.SensitiveRecall != "disallow" {
		return fmt.Errorf("agent_affect.retrieval.sensitive_recall must be disallow")
	}
	if c.AgentAffect.Retrieval.NegativeRetentionBoost {
		return fmt.Errorf("agent_affect.retrieval.negative_retention_boost cannot be true")
	}
	return nil
}

func rejectSecretMap(path string, values map[string]any) error {
	for key, value := range values {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "api_key") || strings.Contains(lower, "secret") || lower == "token" || strings.HasSuffix(lower, "_token") {
			return fmt.Errorf("%s.%s must not contain plaintext secrets; use api_key_env", path, key)
		}
		if nested, ok := value.(map[string]any); ok {
			if err := rejectSecretMap(path+"."+key, nested); err != nil {
				return err
			}
		}
		if nested, ok := value.(map[any]any); ok {
			converted := map[string]any{}
			for nestedKey, nestedValue := range nested {
				converted[fmt.Sprint(nestedKey)] = nestedValue
			}
			if err := rejectSecretMap(path+"."+key, converted); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c Config) ValidateRuntime(opts RuntimeValidationOptions) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if !opts.CheckEnv {
		return nil
	}
	env := opts.Env
	if env == nil {
		env = os.Getenv
	}
	for _, provider := range c.allProviders() {
		if provider.Enabled && strings.TrimSpace(provider.APIKeyEnv) != "" && strings.TrimSpace(env(provider.APIKeyEnv)) == "" {
			return fmt.Errorf("provider %q requires env %s", provider.ID, provider.APIKeyEnv)
		}
	}
	return nil
}

func (c Config) ToOptions() (memorycore.Options, error) {
	adapter, err := c.NewMirrorAdapter()
	if err != nil {
		return memorycore.Options{}, err
	}
	breakerMode := memorycore.SidecarBreakerModeEnabled
	if !c.Sidecar.CircuitBreaker.Enabled {
		breakerMode = memorycore.SidecarBreakerModeDisabled
	}
	qa := c.Pipelines.QueryAnalysis
	provider := memorycore.QueryAnalysisProviderNone
	if qa.Enabled && qa.Mode != "rule_only" && c.Sidecar.Enabled {
		provider = memorycore.QueryAnalysisProviderSidecar
	}
	return memorycore.Options{
		DBPath:        c.Core.DBPath,
		PersonaID:     c.Core.PersonaID,
		AutoMigrate:   c.Core.AutoMigrate,
		EnableFTS:     c.Core.EnableFTS,
		MirrorAdapter: adapter,
		Extraction:    c.ExtractionOptions(),
		QueryAnalysis: memorycore.QueryAnalysisOptions{
			Provider:                         provider,
			Mode:                             memorycore.QueryAnalysisMode(runtimeQueryAnalysisMode(qa)),
			SidecarURL:                       c.Sidecar.URL,
			Timeout:                          time.Duration(qa.TimeoutMS) * time.Millisecond,
			ScorerVersion:                    qa.ScorerVersion,
			RouterVersion:                    qa.RouterVersion,
			MinConfidenceToOverride:          qa.MinConfidenceToOverride,
			MinEntitySemanticConfidence:      qa.MinEntitySemanticConfidence,
			MinRuleFit:                       qa.Thresholds.MinRuleFit,
			MinAnchorReadiness:               qa.Thresholds.MinAnchorReadiness,
			SemanticNeedThreshold:            qa.Thresholds.SemanticNeedThreshold,
			MinComplexityForSemantic:         qa.Thresholds.MinComplexityForSemantic,
			FullSemanticComplexity:           qa.Thresholds.FullSemanticComplexity,
			DecomposeSemanticComplexity:      qa.Thresholds.DecomposeSemanticComplexity,
			MinSemanticFieldConfidence:       qa.Thresholds.MinSemanticFieldConfidence,
			MinOverrideMargin:                qa.Thresholds.MinOverrideMargin,
			HighSafetyRiskThreshold:          qa.Thresholds.HighSafetyRiskThreshold,
			MaxSemanticCallsPerSession:       qa.Budget.MaxSemanticCallsPerSession,
			MaxSemanticCallsPerSessionWindow: time.Duration(qa.Budget.MaxSemanticCallsPerSessionWindowMS) * time.Millisecond,
			MaxSemanticCallsPer1000Queries:   qa.Budget.MaxSemanticCallsPer1000Queries,
			MaxSemanticLatency:               time.Duration(qa.Budget.MaxSemanticLatencyMS) * time.Millisecond,
			DiagnosticsConfigured:            true,
			DiagnosticsIncludeScoreBreakdown: qa.Diagnostics.IncludeScoreBreakdown,
			DiagnosticsIncludeReasonCodes:    qa.Diagnostics.IncludeReasonCodes,
			DiagnosticsSampleRate:            qa.Diagnostics.SampleRate,
			MaxQueryRewrites:                 qa.MaxQueryRewrites,
			MaxSemanticAnchors:               qa.MaxSemanticAnchors,
			SemanticTotalEnergyCap:           qa.SemanticTotalEnergyCap,
			MaxGeneratedDenseWeightSum:       qa.MaxGeneratedDenseWeightSum,
			IncludeRationaleSummary:          qa.IncludeRationaleSummary,
			DisableGeneratedDense:            qa.DisableGeneratedDense,
		},
		SidecarResilience: memorycore.SidecarResilienceOptions{
			Timeouts: memorycore.SidecarStageTimeouts{
				Total:      time.Duration(c.Sidecar.TotalTimeoutMS) * time.Millisecond,
				Mirror:     time.Duration(c.Sidecar.MirrorTimeoutMS) * time.Millisecond,
				Activation: time.Duration(c.Sidecar.ActivationTimeoutMS) * time.Millisecond,
				Rerank:     time.Duration(c.Sidecar.RerankTimeoutMS) * time.Millisecond,
			},
			Breaker: memorycore.SidecarBreakerOptions{
				Mode:             breakerMode,
				Window:           c.Sidecar.CircuitBreaker.Window,
				FailureThreshold: c.Sidecar.CircuitBreaker.FailureThreshold,
				OpenFor:          time.Duration(c.Sidecar.CircuitBreaker.OpenMS) * time.Millisecond,
			},
			ActivationBudget: memorycore.SidecarActivationBudgetOptions{
				MaxEdgesScannedPerRequest: c.Sidecar.ActivationBudget.MaxEdgesScannedPerRequest,
				MaxNeighborsPerNode:       c.Sidecar.ActivationBudget.MaxNeighborsPerNode,
				MaxActivationWall:         time.Duration(c.Sidecar.ActivationBudget.MaxWallMS) * time.Millisecond,
			},
		},
	}, nil
}

func runtimeQueryAnalysisMode(qa QueryAnalysisPipeline) string {
	if strings.TrimSpace(qa.RuntimeMode) != "" {
		return qa.RuntimeMode
	}
	switch qa.Mode {
	case "rule_then_llm", "sidecar":
		return "adaptive_safe"
	case "llm_only":
		return "semantic_always"
	default:
		return "rule_only"
	}
}

func (c Config) ExtractionOptions() memorycore.ExtractionOptions {
	pipeline := c.Pipelines.Extraction
	repair := c.Pipelines.ExtractionRepair
	provider := c.ProviderByID(pipeline.ProviderID)
	providerOpts := memorycore.ExtractionProviderOptions{
		Kind:           memorycore.ExtractionProviderDisabled,
		ID:             pipeline.ProviderID,
		Model:          pipeline.Model,
		Temperature:    pipeline.Params.Temperature,
		MaxTokens:      pipeline.Params.MaxOutputTokens,
		ResponseFormat: memorycore.ExtractionResponseFormat(pipeline.Params.ResponseFormat),
		Thinking:       &memorycore.OpenAICompatibleThinkingOptions{Type: pipeline.Thinking.Type},
	}
	if provider != nil {
		providerOpts.Kind = extractionProviderKind(*provider)
		providerOpts.BaseURL = provider.BaseURL
		providerOpts.APIKeyEnv = provider.APIKeyEnv
		if provider.TimeoutMS > 0 {
			providerOpts.Timeout = time.Duration(provider.TimeoutMS) * time.Millisecond
		}
	}
	return memorycore.ExtractionOptions{
		Enabled:  pipeline.Enabled,
		Provider: providerOpts,
		Defaults: memorycore.ExtractionDefaults{
			Configured:               true,
			Mode:                     extractionConfigRunMode(pipeline.Mode),
			Timezone:                 firstNonEmptyString(c.Core.Timezone, "Asia/Singapore"),
			AllowSensitiveExtraction: c.WritePolicy.Extraction.AllowSensitiveExtraction,
			AllowInference:           c.WritePolicy.Extraction.AllowInference,
			MaxFacts:                 c.WritePolicy.Extraction.MaxFactsPerRequest,
			MaxLinks:                 c.WritePolicy.Extraction.MaxLinksPerRequest,
			RequireCleanGate:         false,
			ApplyAcceptedFacts:       true,
			ExecuteDeletionIntents:   true,
		},
		Runtime: memorycore.ExtractionRuntimeOptions{
			Configured:    true,
			UsePreFilter:  c.Pipelines.Prefilter.Enabled,
			RepairEnabled: repair.Enabled || repair.RetryOnSchemaFailure > 0 || pipeline.RetryOnSchemaFailure > 0,
		},
		Audit: memorycore.ExtractionAuditOptions{
			Configured: true,
			Enabled:    pipeline.Audit.Enabled,
			Force:      pipeline.Audit.Force,
		},
		RawLog: memorycore.ExtractionRawLogOptions{
			Enabled:   pipeline.RawLog.Enabled,
			Directory: pipeline.RawLog.Directory,
		},
	}
}

func extractionConfigRunMode(mode string) memorycore.ExtractionRunMode {
	normalized, err := normalizeExtractionConfigMode(mode)
	if err != nil {
		return memorycore.ExtractionRunModeDryRun
	}
	return normalized
}

func normalizeExtractionConfigMode(mode string) (memorycore.ExtractionRunMode, error) {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "", string(memorycore.ExtractionRunModeDryRun), "dry_run":
		return memorycore.ExtractionRunModeDryRun, nil
	case string(memorycore.ExtractionRunModeValidate):
		return memorycore.ExtractionRunModeValidate, nil
	case string(memorycore.ExtractionRunModeApply):
		return memorycore.ExtractionRunModeApply, nil
	default:
		return "", fmt.Errorf("pipelines.extraction.mode must be one of validate|dry-run|dry_run|apply")
	}
}

func extractionProviderKind(provider ProviderConfig) string {
	switch strings.TrimSpace(strings.ToLower(strings.ReplaceAll(provider.Provider, "_", "-"))) {
	case "mock":
		return memorycore.ExtractionProviderMock
	case "openai-compatible", "openai":
		return memorycore.ExtractionProviderOpenAICompatible
	case "disabled", "":
		if strings.EqualFold(provider.Protocol, "openai_compatible") && provider.Enabled {
			return memorycore.ExtractionProviderOpenAICompatible
		}
		return memorycore.ExtractionProviderDisabled
	default:
		if strings.EqualFold(provider.Protocol, "openai_compatible") {
			return memorycore.ExtractionProviderOpenAICompatible
		}
		return provider.Provider
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c Config) RetrievalPolicy() memorycore.RetrievalPolicy {
	return memorycore.RetrievalPolicy{
		SensitivityPermission: c.Retrieval.SensitivityPermission,
		AllowHistorical:       c.Retrieval.AllowHistorical,
		AllowDeepArchive:      c.Retrieval.AllowDeepArchive,
		FinalMemoryCount:      c.Retrieval.FinalMemoryCount,
		ContextBudgetTokens:   c.Retrieval.ContextBudgetTokens,
		UseFTS:                c.Retrieval.UseFTS,
		UseMirror:             c.Retrieval.UseMirror,
	}
}

func (c Config) RetentionJobs() []memorycore.RetentionJobName {
	jobs := []memorycore.RetentionJobName{}
	if c.Retention.Jobs.DailyTTLExpiry {
		jobs = append(jobs, memorycore.RetentionJobDailyTTLExpiry)
	}
	if c.Retention.Jobs.MonthlyArchive {
		jobs = append(jobs, memorycore.RetentionJobMonthlyDeepArchive)
	}
	return jobs
}

func (c Config) NewMirrorAdapter() (memorycore.MirrorAdapter, error) {
	if !c.Sidecar.Enabled {
		return nil, nil
	}
	switch c.Sidecar.Adapter {
	case "fake":
		return memorycore.NewFakeMirrorAdapter(), nil
	case "trivium":
		if err := memorycore.ValidateSidecarLoopbackURL(c.Sidecar.URL); err != nil {
			return nil, fmt.Errorf("sidecar.url must be a loopback HTTP URL: %w", err)
		}
		return memorycore.NewSidecarMirrorAdapter(c.Sidecar.URL), nil
	default:
		return nil, fmt.Errorf("sidecar.adapter must be one of fake|trivium")
	}
}

func (c Config) ProviderByID(id string) *ProviderConfig {
	for idx := range c.Providers.LLM {
		if c.Providers.LLM[idx].ID == id {
			return &c.Providers.LLM[idx]
		}
	}
	for idx := range c.Providers.Embedder {
		if c.Providers.Embedder[idx].ID == id {
			return &c.Providers.Embedder[idx]
		}
	}
	for idx := range c.Providers.Reranker {
		if c.Providers.Reranker[idx].ID == id {
			return &c.Providers.Reranker[idx]
		}
	}
	return nil
}

func (c Config) allProviders() []ProviderConfig {
	providers := make([]ProviderConfig, 0, len(c.Providers.LLM)+len(c.Providers.Embedder)+len(c.Providers.Reranker))
	providers = append(providers, c.Providers.LLM...)
	providers = append(providers, c.Providers.Embedder...)
	providers = append(providers, c.Providers.Reranker...)
	return providers
}

func (c *Config) ApplyOverrides(overrides ConfigOverrides) {
	if overrides.Enabled != nil {
		c.Enabled = *overrides.Enabled
	}
	if overrides.Core != nil {
		if overrides.Core.DBPath != nil {
			c.Core.DBPath = *overrides.Core.DBPath
		}
		if overrides.Core.PersonaID != nil {
			c.Core.PersonaID = *overrides.Core.PersonaID
		}
	}
	if overrides.Retrieval != nil {
		if overrides.Retrieval.FinalMemoryCount != nil {
			c.Retrieval.FinalMemoryCount = *overrides.Retrieval.FinalMemoryCount
		}
		if overrides.Retrieval.ContextBudgetTokens != nil {
			c.Retrieval.ContextBudgetTokens = *overrides.Retrieval.ContextBudgetTokens
		}
		if overrides.Retrieval.UseFTS != nil {
			c.Retrieval.UseFTS = *overrides.Retrieval.UseFTS
		}
		if overrides.Retrieval.UseMirror != nil {
			c.Retrieval.UseMirror = *overrides.Retrieval.UseMirror
		}
	}
	if overrides.Pipelines != nil {
		if overrides.Pipelines.Extraction != nil {
			applyLLMPipelineOverrides(&c.Pipelines.Extraction.LLMPipelineConfig, *overrides.Pipelines.Extraction)
		}
		if overrides.Pipelines.QueryAnalysis != nil {
			applyLLMPipelineOverrides(&c.Pipelines.QueryAnalysis.LLMPipelineConfig, *overrides.Pipelines.QueryAnalysis)
		}
	}
	if overrides.Sidecar != nil {
		if overrides.Sidecar.Enabled != nil {
			c.Sidecar.Enabled = *overrides.Sidecar.Enabled
		}
		if overrides.Sidecar.URL != nil {
			c.Sidecar.URL = *overrides.Sidecar.URL
		}
		if overrides.Sidecar.Adapter != nil {
			c.Sidecar.Adapter = *overrides.Sidecar.Adapter
		}
	}
	if overrides.Mirror != nil {
		if overrides.Mirror.Enabled != nil {
			c.Mirror.Enabled = *overrides.Mirror.Enabled
		}
		if overrides.Mirror.SyncLimit != nil {
			c.Mirror.SyncLimit = *overrides.Mirror.SyncLimit
		}
	}
	if overrides.Retention != nil && overrides.Retention.DeepArchiveAfterDays != nil {
		c.Retention.Thresholds.DeepArchiveAfterDays = *overrides.Retention.DeepArchiveAfterDays
	}
}

func applyLLMPipelineOverrides(pipeline *LLMPipelineConfig, overrides LLMPipelineOverrides) {
	if overrides.ProviderID != nil {
		pipeline.ProviderID = *overrides.ProviderID
	}
	if overrides.Model != nil {
		pipeline.Model = *overrides.Model
	}
}

func (c *Config) ApplyProviderRegistry(registry ProviderRegistry) {
	if registry.LLM != nil {
		c.Providers.LLM = providerMappingsToConfigs(registry.LLM)
	}
	if registry.Embedder != nil {
		c.Providers.Embedder = providerMappingsToConfigs(registry.Embedder)
	}
	if registry.Reranker != nil {
		c.Providers.Reranker = providerMappingsToConfigs(registry.Reranker)
	}
}

func providerMappingsToConfigs(mappings []ProviderMapping) []ProviderConfig {
	providers := make([]ProviderConfig, 0, len(mappings))
	for _, mapping := range mappings {
		providers = append(providers, ProviderConfig{
			ID:        mapping.ID,
			Provider:  mapping.Provider,
			Protocol:  mapping.Protocol,
			BaseURL:   mapping.BaseURL,
			APIKeyEnv: mapping.APIKeyEnv,
			Enabled:   mapping.Enabled,
			TimeoutMS: mapping.TimeoutMS,
		})
	}
	return providers
}

func LoadEffective(opts LoadEffectiveOptions) (Config, error) {
	var cfg Config
	var err error
	if strings.TrimSpace(opts.ConfigPath) == "" {
		cfg = DefaultConfig()
	} else if strings.EqualFold(filepathExt(opts.ConfigPath), ".json") {
		cfg, err = LoadJSONWithOptions(opts.ConfigPath, LoadOptions{SkipValidate: true})
	} else {
		cfg, err = LoadYAMLWithOptions(opts.ConfigPath, LoadOptions{SkipValidate: true})
	}
	if err != nil {
		return Config{}, err
	}
	cfg.ApplyProviderRegistry(opts.ProviderRegistry)
	cfg.ApplyOverrides(opts.Overrides)
	if err := cfg.ValidateRuntime(opts.Runtime); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Runtime() (ConfiguredRuntime, error) {
	opts, err := c.ToOptions()
	if err != nil {
		return ConfiguredRuntime{}, err
	}
	return ConfiguredRuntime{
		Config:          c,
		Options:         opts,
		RetrievalPolicy: c.RetrievalPolicy(),
		RetentionJobs:   c.RetentionJobs(),
		MirrorSyncLimit: c.Mirror.SyncLimit,
	}, nil
}

func Open(ctx context.Context, opts ConfigOpenOptions) (*ConfiguredService, error) {
	cfg, err := LoadEffective(LoadEffectiveOptions{
		ConfigPath:       opts.ConfigPath,
		Overrides:        opts.Overrides,
		ProviderRegistry: opts.ProviderRegistry,
		Runtime:          opts.Runtime,
	})
	if err != nil {
		return nil, err
	}
	runtime, err := cfg.Runtime()
	if err != nil {
		return nil, err
	}
	openOpts := runtime.Options
	openOpts.Now = opts.Now
	svc, err := memorycore.Open(ctx, openOpts)
	if err != nil {
		return nil, err
	}
	return &ConfiguredService{
		Service:         svc,
		Config:          cfg,
		RetrievalPolicy: runtime.RetrievalPolicy,
		RetentionJobs:   runtime.RetentionJobs,
		MirrorSyncLimit: runtime.MirrorSyncLimit,
	}, nil
}

func filepathExt(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return ""
	}
	return path[idx:]
}

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	if err := rejectUnknownYAMLFields(value, yamlFieldsForType(reflect.TypeOf(Config{})), ""); err != nil {
		return err
	}
	type plain Config
	cfg := plain(DefaultConfig())
	if err := value.Decode(&cfg); err != nil {
		return err
	}
	*c = Config(cfg)
	return nil
}

func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	cfg := plain(DefaultConfig())
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return err
	}
	*c = Config(cfg)
	return nil
}

type yamlFieldSet map[string]yamlFieldSet

func yamlFieldsForType(t reflect.Type) yamlFieldSet {
	t = derefType(t)
	if t.Kind() == reflect.Map {
		return nil
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		return yamlFieldsForType(t.Elem())
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	fields := yamlFieldSet{}
	for idx := 0; idx < t.NumField(); idx++ {
		field := t.Field(idx)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		rawTag := field.Tag.Get("yaml")
		tagParts := strings.Split(rawTag, ",")
		tag := tagParts[0]
		if tag == "-" {
			continue
		}
		if field.Anonymous && containsTagOption(tagParts[1:], "inline") {
			for key, value := range yamlFieldsForType(field.Type) {
				fields[key] = value
			}
			continue
		}
		if tag == "" {
			tag = strings.ToLower(field.Name)
		}
		fields[tag] = yamlFieldsForType(field.Type)
	}
	return fields
}

func containsTagOption(options []string, want string) bool {
	for _, option := range options {
		if option == want {
			return true
		}
	}
	return false
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func rejectUnknownYAMLFields(node *yaml.Node, allowed yamlFieldSet, prefix string) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil
		}
		return rejectUnknownYAMLFields(node.Content[0], allowed, prefix)
	}
	if node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			if err := rejectUnknownYAMLFields(child, allowed, prefix); err != nil {
				return err
			}
		}
		return nil
	}
	if node.Kind != yaml.MappingNode || allowed == nil {
		return nil
	}
	for idx := 0; idx+1 < len(node.Content); idx += 2 {
		key := node.Content[idx].Value
		childFields, ok := allowed[key]
		fieldPath := joinFieldPath(prefix, key)
		if !ok {
			return fmt.Errorf("unknown config field %s", fieldPath)
		}
		if err := rejectUnknownYAMLFields(node.Content[idx+1], childFields, fieldPath); err != nil {
			return err
		}
	}
	return nil
}

func joinFieldPath(prefix string, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
