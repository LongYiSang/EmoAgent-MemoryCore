package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"gopkg.in/yaml.v3"
)

const SchemaVersion = "memorycore.config.v0.1"

type Config struct {
	SchemaVersion string              `yaml:"schema_version" json:"schema_version"`
	Enabled       bool                `yaml:"enabled" json:"enabled"`
	Core          CoreConfig          `yaml:"core" json:"core"`
	Retrieval     RetrievalConfig     `yaml:"retrieval" json:"retrieval"`
	QueryAnalysis QueryAnalysisConfig `yaml:"query_analysis" json:"query_analysis"`
	Sidecar       SidecarConfig       `yaml:"sidecar" json:"sidecar"`
	Retention     RetentionConfig     `yaml:"retention" json:"retention"`
	Mirror        MirrorConfig        `yaml:"mirror" json:"mirror"`
}

type CoreConfig struct {
	DBPath      string `yaml:"db_path" json:"db_path"`
	PersonaID   string `yaml:"persona_id" json:"persona_id"`
	AutoMigrate bool   `yaml:"auto_migrate" json:"auto_migrate"`
	EnableFTS   bool   `yaml:"enable_fts" json:"enable_fts"`
}

type RetrievalConfig struct {
	UseFTS                bool   `yaml:"use_fts" json:"use_fts"`
	UseMirror             bool   `yaml:"use_mirror" json:"use_mirror"`
	FinalMemoryCount      int    `yaml:"final_memory_count" json:"final_memory_count"`
	ContextBudgetTokens   int    `yaml:"context_budget_tokens" json:"context_budget_tokens"`
	AllowHistorical       bool   `yaml:"allow_historical" json:"allow_historical"`
	AllowDeepArchive      bool   `yaml:"allow_deep_archive" json:"allow_deep_archive"`
	SensitivityPermission string `yaml:"sensitivity_permission" json:"sensitivity_permission"`
}

type QueryAnalysisConfig struct {
	Provider                    string                         `yaml:"provider" json:"provider"`
	Mode                        string                         `yaml:"mode" json:"mode"`
	SidecarURL                  string                         `yaml:"sidecar_url" json:"sidecar_url"`
	TimeoutMS                   int                            `yaml:"timeout_ms" json:"timeout_ms"`
	ScorerVersion               string                         `yaml:"scorer_version" json:"scorer_version"`
	RouterVersion               string                         `yaml:"router_version" json:"router_version"`
	Thresholds                  QueryAnalysisThresholdsConfig  `yaml:"thresholds" json:"thresholds"`
	Budget                      QueryAnalysisBudgetConfig      `yaml:"budget" json:"budget"`
	Diagnostics                 QueryAnalysisDiagnosticsConfig `yaml:"diagnostics" json:"diagnostics"`
	MinConfidenceToOverride     float64                        `yaml:"min_confidence_to_override" json:"min_confidence_to_override"`
	MinEntitySemanticConfidence float64                        `yaml:"min_entity_semantic_confidence" json:"min_entity_semantic_confidence"`
	MinRuleFit                  float64                        `yaml:"min_rule_fit" json:"min_rule_fit"`
	MinAnchorReadiness          float64                        `yaml:"min_anchor_readiness" json:"min_anchor_readiness"`
	SemanticNeedThreshold       float64                        `yaml:"semantic_need" json:"semantic_need"`
	MinComplexityForSemantic    float64                        `yaml:"min_complexity_for_semantic" json:"min_complexity_for_semantic"`
	FullSemanticComplexity      float64                        `yaml:"full_semantic_complexity" json:"full_semantic_complexity"`
	DecomposeSemanticComplexity float64                        `yaml:"decompose_complexity" json:"decompose_complexity"`
	MinSemanticFieldConfidence  float64                        `yaml:"min_semantic_field_confidence" json:"min_semantic_field_confidence"`
	MinOverrideMargin           float64                        `yaml:"min_override_margin" json:"min_override_margin"`
	HighSafetyRiskThreshold     float64                        `yaml:"high_safety_risk" json:"high_safety_risk"`
	MaxQueryRewrites            int                            `yaml:"max_query_rewrites" json:"max_query_rewrites"`
	MaxSemanticAnchors          int                            `yaml:"max_semantic_anchors" json:"max_semantic_anchors"`
	SemanticTotalEnergyCap      float64                        `yaml:"semantic_total_energy_cap" json:"semantic_total_energy_cap"`
	MaxGeneratedDenseWeightSum  float64                        `yaml:"max_generated_dense_weight_sum" json:"max_generated_dense_weight_sum"`
	IncludeRationaleSummary     bool                           `yaml:"include_rationale_summary" json:"include_rationale_summary"`
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
	MaxSemanticCallsPerSession     int `yaml:"max_semantic_calls_per_session" json:"max_semantic_calls_per_session"`
	MaxSemanticCallsPer1000Queries int `yaml:"max_semantic_calls_per_1000_queries" json:"max_semantic_calls_per_1000_queries"`
	MaxSemanticLatencyMS           int `yaml:"max_semantic_latency_ms" json:"max_semantic_latency_ms"`
}

type QueryAnalysisDiagnosticsConfig struct {
	IncludeScoreBreakdown bool    `yaml:"include_score_breakdown" json:"include_score_breakdown"`
	IncludeReasonCodes    bool    `yaml:"include_reason_codes" json:"include_reason_codes"`
	SampleRate            float64 `yaml:"sample_rate" json:"sample_rate"`
}

type SidecarConfig struct {
	Enabled                             bool   `yaml:"enabled" json:"enabled"`
	URL                                 string `yaml:"url" json:"url"`
	Adapter                             string `yaml:"adapter" json:"adapter"`
	TotalTimeoutMS                      int    `yaml:"total_timeout_ms" json:"total_timeout_ms"`
	MirrorTimeoutMS                     int    `yaml:"mirror_timeout_ms" json:"mirror_timeout_ms"`
	ActivationTimeoutMS                 int    `yaml:"activation_timeout_ms" json:"activation_timeout_ms"`
	RerankTimeoutMS                     int    `yaml:"rerank_timeout_ms" json:"rerank_timeout_ms"`
	BreakerEnabled                      bool   `yaml:"breaker_enabled" json:"breaker_enabled"`
	BreakerWindow                       int    `yaml:"breaker_window" json:"breaker_window"`
	BreakerFailureThreshold             int    `yaml:"breaker_failure_threshold" json:"breaker_failure_threshold"`
	BreakerOpenMS                       int    `yaml:"breaker_open_ms" json:"breaker_open_ms"`
	ActivationMaxEdgesScannedPerRequest int    `yaml:"activation_max_edges_scanned_per_request" json:"activation_max_edges_scanned_per_request"`
	ActivationMaxNeighborsPerNode       int    `yaml:"activation_max_neighbors_per_node" json:"activation_max_neighbors_per_node"`
	ActivationMaxWallMS                 int    `yaml:"activation_max_wall_ms" json:"activation_max_wall_ms"`
}

type RetentionConfig struct {
	Jobs                 []string `yaml:"jobs" json:"jobs"`
	DeepArchiveAfterDays int      `yaml:"deep_archive_after_days" json:"deep_archive_after_days"`
}

type MirrorConfig struct {
	SyncLimit int `yaml:"sync_limit" json:"sync_limit"`
}

type RuntimeValidationOptions struct {
	CheckEnv bool
	Env      func(string) string
}

func Default() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Enabled:       false,
		Core: CoreConfig{
			DBPath:      "./data/memory.db",
			PersonaID:   "default",
			AutoMigrate: true,
			EnableFTS:   true,
		},
		Retrieval: RetrievalConfig{
			UseFTS:                true,
			UseMirror:             false,
			FinalMemoryCount:      8,
			ContextBudgetTokens:   1200,
			AllowHistorical:       false,
			AllowDeepArchive:      false,
			SensitivityPermission: memorycore.SensitivityNormal,
		},
		QueryAnalysis: QueryAnalysisConfig{
			Provider:      "none",
			Mode:          "rule_only",
			SidecarURL:    "http://127.0.0.1:8765",
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
				MaxSemanticCallsPerSession:     8,
				MaxSemanticCallsPer1000Queries: 250,
				MaxSemanticLatencyMS:           1500,
			},
			Diagnostics: QueryAnalysisDiagnosticsConfig{
				IncludeScoreBreakdown: true,
				IncludeReasonCodes:    true,
				SampleRate:            1.0,
			},
			MinConfidenceToOverride:     0.72,
			MinEntitySemanticConfidence: 0.70,
			MinRuleFit:                  0.66,
			MinAnchorReadiness:          0.45,
			SemanticNeedThreshold:       0.58,
			MinComplexityForSemantic:    0.50,
			FullSemanticComplexity:      0.72,
			DecomposeSemanticComplexity: 0.80,
			MinSemanticFieldConfidence:  0.70,
			MinOverrideMargin:           0.08,
			HighSafetyRiskThreshold:     0.80,
			MaxQueryRewrites:            5,
			MaxSemanticAnchors:          8,
			SemanticTotalEnergyCap:      5.0,
			MaxGeneratedDenseWeightSum:  3.0,
			IncludeRationaleSummary:     false,
		},
		Sidecar: SidecarConfig{
			Enabled:                             false,
			URL:                                 "",
			Adapter:                             "trivium",
			TotalTimeoutMS:                      400,
			MirrorTimeoutMS:                     80,
			ActivationTimeoutMS:                 150,
			RerankTimeoutMS:                     100,
			BreakerEnabled:                      true,
			BreakerWindow:                       20,
			BreakerFailureThreshold:             3,
			BreakerOpenMS:                       60000,
			ActivationMaxEdgesScannedPerRequest: 10000,
			ActivationMaxNeighborsPerNode:       100,
			ActivationMaxWallMS:                 120,
		},
		Retention: RetentionConfig{
			Jobs:                 []string{string(memorycore.RetentionJobDailyTTLExpiry)},
			DeepArchiveAfterDays: 0,
		},
		Mirror: MirrorConfig{
			SyncLimit: 100,
		},
	}
}

func (c *Config) ApplyDefaults() {
	defaults := Default()
	if c.SchemaVersion == "" {
		c.SchemaVersion = defaults.SchemaVersion
	}
	if strings.TrimSpace(c.Core.DBPath) == "" {
		c.Core.DBPath = defaults.Core.DBPath
	}
	if strings.TrimSpace(c.Core.PersonaID) == "" {
		c.Core.PersonaID = defaults.Core.PersonaID
	}
	if c.Retrieval.FinalMemoryCount == 0 {
		c.Retrieval.FinalMemoryCount = defaults.Retrieval.FinalMemoryCount
	}
	if c.Retrieval.ContextBudgetTokens == 0 {
		c.Retrieval.ContextBudgetTokens = defaults.Retrieval.ContextBudgetTokens
	}
	if strings.TrimSpace(c.Retrieval.SensitivityPermission) == "" {
		c.Retrieval.SensitivityPermission = defaults.Retrieval.SensitivityPermission
	}
	if strings.TrimSpace(c.QueryAnalysis.Provider) == "" {
		c.QueryAnalysis.Provider = defaults.QueryAnalysis.Provider
	}
	if strings.TrimSpace(c.QueryAnalysis.Mode) == "" {
		c.QueryAnalysis.Mode = defaults.QueryAnalysis.Mode
	}
	if strings.TrimSpace(c.QueryAnalysis.SidecarURL) == "" {
		c.QueryAnalysis.SidecarURL = defaults.QueryAnalysis.SidecarURL
	}
	if c.QueryAnalysis.TimeoutMS == 0 {
		c.QueryAnalysis.TimeoutMS = defaults.QueryAnalysis.TimeoutMS
	}
	if strings.TrimSpace(c.QueryAnalysis.ScorerVersion) == "" {
		c.QueryAnalysis.ScorerVersion = defaults.QueryAnalysis.ScorerVersion
	}
	if strings.TrimSpace(c.QueryAnalysis.RouterVersion) == "" {
		c.QueryAnalysis.RouterVersion = defaults.QueryAnalysis.RouterVersion
	}
	if c.QueryAnalysis.MinConfidenceToOverride == 0 {
		c.QueryAnalysis.MinConfidenceToOverride = defaults.QueryAnalysis.MinConfidenceToOverride
	}
	if c.QueryAnalysis.MinEntitySemanticConfidence == 0 {
		c.QueryAnalysis.MinEntitySemanticConfidence = defaults.QueryAnalysis.MinEntitySemanticConfidence
	}
	if c.QueryAnalysis.MinRuleFit == 0 {
		c.QueryAnalysis.MinRuleFit = defaults.QueryAnalysis.MinRuleFit
	}
	if c.QueryAnalysis.MinAnchorReadiness == 0 {
		c.QueryAnalysis.MinAnchorReadiness = defaults.QueryAnalysis.MinAnchorReadiness
	}
	if c.QueryAnalysis.SemanticNeedThreshold == 0 {
		c.QueryAnalysis.SemanticNeedThreshold = defaults.QueryAnalysis.SemanticNeedThreshold
	}
	if c.QueryAnalysis.MinComplexityForSemantic == 0 {
		c.QueryAnalysis.MinComplexityForSemantic = defaults.QueryAnalysis.MinComplexityForSemantic
	}
	if c.QueryAnalysis.FullSemanticComplexity == 0 {
		c.QueryAnalysis.FullSemanticComplexity = defaults.QueryAnalysis.FullSemanticComplexity
	}
	if c.QueryAnalysis.DecomposeSemanticComplexity == 0 {
		c.QueryAnalysis.DecomposeSemanticComplexity = defaults.QueryAnalysis.DecomposeSemanticComplexity
	}
	if c.QueryAnalysis.MinSemanticFieldConfidence == 0 {
		c.QueryAnalysis.MinSemanticFieldConfidence = defaults.QueryAnalysis.MinSemanticFieldConfidence
	}
	if c.QueryAnalysis.MinOverrideMargin == 0 {
		c.QueryAnalysis.MinOverrideMargin = defaults.QueryAnalysis.MinOverrideMargin
	}
	if c.QueryAnalysis.HighSafetyRiskThreshold == 0 {
		c.QueryAnalysis.HighSafetyRiskThreshold = defaults.QueryAnalysis.HighSafetyRiskThreshold
	}
	if c.QueryAnalysis.MaxQueryRewrites == 0 {
		c.QueryAnalysis.MaxQueryRewrites = defaults.QueryAnalysis.MaxQueryRewrites
	}
	if c.QueryAnalysis.MaxSemanticAnchors == 0 {
		c.QueryAnalysis.MaxSemanticAnchors = defaults.QueryAnalysis.MaxSemanticAnchors
	}
	if c.QueryAnalysis.SemanticTotalEnergyCap == 0 {
		c.QueryAnalysis.SemanticTotalEnergyCap = defaults.QueryAnalysis.SemanticTotalEnergyCap
	}
	if c.QueryAnalysis.MaxGeneratedDenseWeightSum == 0 {
		c.QueryAnalysis.MaxGeneratedDenseWeightSum = defaults.QueryAnalysis.MaxGeneratedDenseWeightSum
	}
	applyQueryAnalysisThresholdDefaults(&c.QueryAnalysis.Thresholds, defaults.QueryAnalysis.Thresholds)
	applyQueryAnalysisBudgetDefaults(&c.QueryAnalysis.Budget, defaults.QueryAnalysis.Budget)
	if c.QueryAnalysis.Diagnostics.SampleRate == 0 &&
		!c.QueryAnalysis.Diagnostics.IncludeScoreBreakdown &&
		!c.QueryAnalysis.Diagnostics.IncludeReasonCodes {
		c.QueryAnalysis.Diagnostics = defaults.QueryAnalysis.Diagnostics
	} else if c.QueryAnalysis.Diagnostics.SampleRate == 0 {
		c.QueryAnalysis.Diagnostics.SampleRate = defaults.QueryAnalysis.Diagnostics.SampleRate
	}
	if strings.TrimSpace(c.Sidecar.Adapter) == "" {
		c.Sidecar.Adapter = defaults.Sidecar.Adapter
	}
	if c.Sidecar.TotalTimeoutMS == 0 {
		c.Sidecar.TotalTimeoutMS = defaults.Sidecar.TotalTimeoutMS
	}
	if c.Sidecar.MirrorTimeoutMS == 0 {
		c.Sidecar.MirrorTimeoutMS = defaults.Sidecar.MirrorTimeoutMS
	}
	if c.Sidecar.ActivationTimeoutMS == 0 {
		c.Sidecar.ActivationTimeoutMS = defaults.Sidecar.ActivationTimeoutMS
	}
	if c.Sidecar.RerankTimeoutMS == 0 {
		c.Sidecar.RerankTimeoutMS = defaults.Sidecar.RerankTimeoutMS
	}
	if c.Sidecar.BreakerWindow == 0 {
		c.Sidecar.BreakerWindow = defaults.Sidecar.BreakerWindow
	}
	if c.Sidecar.BreakerFailureThreshold == 0 {
		c.Sidecar.BreakerFailureThreshold = defaults.Sidecar.BreakerFailureThreshold
	}
	if c.Sidecar.BreakerOpenMS == 0 {
		c.Sidecar.BreakerOpenMS = defaults.Sidecar.BreakerOpenMS
	}
	if c.Sidecar.ActivationMaxEdgesScannedPerRequest == 0 {
		c.Sidecar.ActivationMaxEdgesScannedPerRequest = defaults.Sidecar.ActivationMaxEdgesScannedPerRequest
	}
	if c.Sidecar.ActivationMaxNeighborsPerNode == 0 {
		c.Sidecar.ActivationMaxNeighborsPerNode = defaults.Sidecar.ActivationMaxNeighborsPerNode
	}
	if c.Sidecar.ActivationMaxWallMS == 0 {
		c.Sidecar.ActivationMaxWallMS = defaults.Sidecar.ActivationMaxWallMS
	}
	if c.Retention.Jobs == nil {
		c.Retention.Jobs = append([]string(nil), defaults.Retention.Jobs...)
	}
	if c.Mirror.SyncLimit == 0 {
		c.Mirror.SyncLimit = defaults.Mirror.SyncLimit
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
	switch c.QueryAnalysis.Provider {
	case "none", "sidecar":
	default:
		return fmt.Errorf("query_analysis.provider must be one of none|sidecar")
	}
	switch c.QueryAnalysis.Mode {
	case "rule_only", "semantic_always", "semantic_on_low_confidence", "semantic_rewrite_only",
		"legacy_only", "shadow_adaptive", "adaptive", "adaptive_safe", "adaptive_full":
	default:
		return fmt.Errorf("query_analysis.mode must be one of rule_only|semantic_always|semantic_on_low_confidence|semantic_rewrite_only|legacy_only|shadow_adaptive|adaptive|adaptive_safe|adaptive_full")
	}
	if c.QueryAnalysis.Provider == "none" &&
		c.QueryAnalysis.Mode != "rule_only" &&
		c.QueryAnalysis.Mode != "legacy_only" &&
		c.QueryAnalysis.Mode != "shadow_adaptive" {
		return fmt.Errorf("query_analysis.mode must be rule_only, legacy_only, or shadow_adaptive when query_analysis.provider=none")
	}
	if c.QueryAnalysis.TimeoutMS <= 0 {
		return fmt.Errorf("query_analysis.timeout_ms must be > 0")
	}
	if strings.TrimSpace(c.QueryAnalysis.ScorerVersion) == "" {
		return fmt.Errorf("query_analysis.scorer_version is required")
	}
	if strings.TrimSpace(c.QueryAnalysis.RouterVersion) == "" {
		return fmt.Errorf("query_analysis.router_version is required")
	}
	if c.QueryAnalysis.MinConfidenceToOverride <= 0 || c.QueryAnalysis.MinConfidenceToOverride > 1 {
		return fmt.Errorf("query_analysis.min_confidence_to_override must be within (0, 1]")
	}
	if c.QueryAnalysis.MinEntitySemanticConfidence <= 0 || c.QueryAnalysis.MinEntitySemanticConfidence > 1 {
		return fmt.Errorf("query_analysis.min_entity_semantic_confidence must be within (0, 1]")
	}
	if err := validateUnitInterval("query_analysis.min_rule_fit", c.QueryAnalysis.MinRuleFit); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.min_anchor_readiness", c.QueryAnalysis.MinAnchorReadiness); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.semantic_need", c.QueryAnalysis.SemanticNeedThreshold); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.min_complexity_for_semantic", c.QueryAnalysis.MinComplexityForSemantic); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.full_semantic_complexity", c.QueryAnalysis.FullSemanticComplexity); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.decompose_complexity", c.QueryAnalysis.DecomposeSemanticComplexity); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.min_semantic_field_confidence", c.QueryAnalysis.MinSemanticFieldConfidence); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.min_override_margin", c.QueryAnalysis.MinOverrideMargin); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.high_safety_risk", c.QueryAnalysis.HighSafetyRiskThreshold); err != nil {
		return err
	}
	if err := validateQueryAnalysisThresholds(c.QueryAnalysis.Thresholds); err != nil {
		return err
	}
	if err := validateQueryAnalysisBudget(c.QueryAnalysis.Budget); err != nil {
		return err
	}
	if c.QueryAnalysis.Diagnostics.SampleRate < 0 || c.QueryAnalysis.Diagnostics.SampleRate > 1 {
		return fmt.Errorf("query_analysis.diagnostics.sample_rate must be within [0, 1]")
	}
	if c.QueryAnalysis.MaxQueryRewrites <= 0 {
		return fmt.Errorf("query_analysis.max_query_rewrites must be > 0")
	}
	if c.QueryAnalysis.MaxSemanticAnchors <= 0 {
		return fmt.Errorf("query_analysis.max_semantic_anchors must be > 0")
	}
	if c.QueryAnalysis.SemanticTotalEnergyCap <= 0 {
		return fmt.Errorf("query_analysis.semantic_total_energy_cap must be > 0")
	}
	if c.QueryAnalysis.MaxGeneratedDenseWeightSum <= 0 {
		return fmt.Errorf("query_analysis.max_generated_dense_weight_sum must be > 0")
	}
	if c.QueryAnalysis.Provider == "sidecar" {
		if err := memorycore.ValidateSidecarLoopbackURL(c.QueryAnalysis.SidecarURL); err != nil {
			return fmt.Errorf("query_analysis.sidecar_url must be a loopback HTTP URL: %w", err)
		}
	}
	if c.Retrieval.UseMirror {
		if !c.Sidecar.Enabled {
			return fmt.Errorf("sidecar.enabled must be true when retrieval.use_mirror=true")
		}
		if c.Sidecar.Adapter != "fake" && strings.TrimSpace(c.Sidecar.URL) == "" {
			return fmt.Errorf("sidecar.url is required when retrieval.use_mirror=true and sidecar.adapter=%q", c.Sidecar.Adapter)
		}
	}
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
	if c.Sidecar.Enabled || c.Retrieval.UseMirror {
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
		if c.Sidecar.ActivationMaxEdgesScannedPerRequest <= 0 {
			return fmt.Errorf("sidecar.activation_max_edges_scanned_per_request must be > 0")
		}
		if c.Sidecar.ActivationMaxNeighborsPerNode <= 0 {
			return fmt.Errorf("sidecar.activation_max_neighbors_per_node must be > 0")
		}
		if c.Sidecar.ActivationMaxWallMS <= 0 {
			return fmt.Errorf("sidecar.activation_max_wall_ms must be > 0")
		}
		if c.Sidecar.BreakerEnabled {
			if c.Sidecar.BreakerWindow <= 0 {
				return fmt.Errorf("sidecar.breaker_window must be > 0 when sidecar.breaker_enabled=true")
			}
			if c.Sidecar.BreakerFailureThreshold <= 0 {
				return fmt.Errorf("sidecar.breaker_failure_threshold must be > 0 when sidecar.breaker_enabled=true")
			}
			if c.Sidecar.BreakerOpenMS <= 0 {
				return fmt.Errorf("sidecar.breaker_open_ms must be > 0 when sidecar.breaker_enabled=true")
			}
		}
	}
	for _, job := range c.Retention.Jobs {
		switch memorycore.RetentionJobName(job) {
		case memorycore.RetentionJobDailyTTLExpiry:
		case memorycore.RetentionJobMonthlyDeepArchive:
			if c.Retention.DeepArchiveAfterDays <= 0 {
				return fmt.Errorf("retention.deep_archive_after_days must be > 0 when monthly_deep_archive is enabled")
			}
		default:
			return fmt.Errorf("retention.jobs contains unknown job %q", job)
		}
	}
	if c.Mirror.SyncLimit <= 0 {
		return fmt.Errorf("mirror.sync_limit must be > 0")
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
	if err := validateUnitInterval("query_analysis.thresholds.min_rule_fit", thresholds.MinRuleFit); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.thresholds.min_anchor_readiness", thresholds.MinAnchorReadiness); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.thresholds.semantic_need", thresholds.SemanticNeedThreshold); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.thresholds.min_complexity_for_semantic", thresholds.MinComplexityForSemantic); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.thresholds.full_semantic_complexity", thresholds.FullSemanticComplexity); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.thresholds.decompose_complexity", thresholds.DecomposeSemanticComplexity); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.thresholds.min_semantic_field_confidence", thresholds.MinSemanticFieldConfidence); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.thresholds.min_override_margin", thresholds.MinOverrideMargin); err != nil {
		return err
	}
	if err := validateUnitInterval("query_analysis.thresholds.high_safety_risk", thresholds.HighSafetyRiskThreshold); err != nil {
		return err
	}
	return nil
}

func validateQueryAnalysisBudget(budget QueryAnalysisBudgetConfig) error {
	if budget.MaxSemanticCallsPerSession <= 0 {
		return fmt.Errorf("query_analysis.budget.max_semantic_calls_per_session must be > 0")
	}
	if budget.MaxSemanticCallsPer1000Queries <= 0 {
		return fmt.Errorf("query_analysis.budget.max_semantic_calls_per_1000_queries must be > 0")
	}
	if budget.MaxSemanticCallsPer1000Queries > 1000 {
		return fmt.Errorf("query_analysis.budget.max_semantic_calls_per_1000_queries must be <= 1000")
	}
	if budget.MaxSemanticLatencyMS <= 0 {
		return fmt.Errorf("query_analysis.budget.max_semantic_latency_ms must be > 0")
	}
	return nil
}

func (c Config) ValidateRuntime(opts RuntimeValidationOptions) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if opts.CheckEnv && opts.Env == nil {
		_ = opts.Env
	}
	return nil
}

func (c Config) ToOptions() (memorycore.Options, error) {
	adapter, err := c.NewMirrorAdapter()
	if err != nil {
		return memorycore.Options{}, err
	}
	breakerMode := memorycore.SidecarBreakerModeEnabled
	if !c.Sidecar.BreakerEnabled {
		breakerMode = memorycore.SidecarBreakerModeDisabled
	}
	return memorycore.Options{
		DBPath:        c.Core.DBPath,
		PersonaID:     c.Core.PersonaID,
		AutoMigrate:   c.Core.AutoMigrate,
		EnableFTS:     c.Core.EnableFTS,
		MirrorAdapter: adapter,
		QueryAnalysis: memorycore.QueryAnalysisOptions{
			Provider:                         memorycore.QueryAnalysisProvider(c.QueryAnalysis.Provider),
			Mode:                             memorycore.QueryAnalysisMode(c.QueryAnalysis.Mode),
			SidecarURL:                       c.QueryAnalysis.SidecarURL,
			Timeout:                          time.Duration(c.QueryAnalysis.TimeoutMS) * time.Millisecond,
			ScorerVersion:                    c.QueryAnalysis.ScorerVersion,
			RouterVersion:                    c.QueryAnalysis.RouterVersion,
			MinConfidenceToOverride:          c.QueryAnalysis.MinConfidenceToOverride,
			MinEntitySemanticConfidence:      c.QueryAnalysis.MinEntitySemanticConfidence,
			MinRuleFit:                       c.QueryAnalysis.Thresholds.MinRuleFit,
			MinAnchorReadiness:               c.QueryAnalysis.Thresholds.MinAnchorReadiness,
			SemanticNeedThreshold:            c.QueryAnalysis.Thresholds.SemanticNeedThreshold,
			MinComplexityForSemantic:         c.QueryAnalysis.Thresholds.MinComplexityForSemantic,
			FullSemanticComplexity:           c.QueryAnalysis.Thresholds.FullSemanticComplexity,
			DecomposeSemanticComplexity:      c.QueryAnalysis.Thresholds.DecomposeSemanticComplexity,
			MinSemanticFieldConfidence:       c.QueryAnalysis.Thresholds.MinSemanticFieldConfidence,
			MinOverrideMargin:                c.QueryAnalysis.Thresholds.MinOverrideMargin,
			HighSafetyRiskThreshold:          c.QueryAnalysis.Thresholds.HighSafetyRiskThreshold,
			MaxSemanticCallsPerSession:       c.QueryAnalysis.Budget.MaxSemanticCallsPerSession,
			MaxSemanticCallsPer1000Queries:   c.QueryAnalysis.Budget.MaxSemanticCallsPer1000Queries,
			MaxSemanticLatency:               time.Duration(c.QueryAnalysis.Budget.MaxSemanticLatencyMS) * time.Millisecond,
			DiagnosticsConfigured:            true,
			DiagnosticsIncludeScoreBreakdown: c.QueryAnalysis.Diagnostics.IncludeScoreBreakdown,
			DiagnosticsIncludeReasonCodes:    c.QueryAnalysis.Diagnostics.IncludeReasonCodes,
			DiagnosticsSampleRate:            c.QueryAnalysis.Diagnostics.SampleRate,
			MaxQueryRewrites:                 c.QueryAnalysis.MaxQueryRewrites,
			MaxSemanticAnchors:               c.QueryAnalysis.MaxSemanticAnchors,
			SemanticTotalEnergyCap:           c.QueryAnalysis.SemanticTotalEnergyCap,
			MaxGeneratedDenseWeightSum:       c.QueryAnalysis.MaxGeneratedDenseWeightSum,
			IncludeRationaleSummary:          c.QueryAnalysis.IncludeRationaleSummary,
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
				Window:           c.Sidecar.BreakerWindow,
				FailureThreshold: c.Sidecar.BreakerFailureThreshold,
				OpenFor:          time.Duration(c.Sidecar.BreakerOpenMS) * time.Millisecond,
			},
			ActivationBudget: memorycore.SidecarActivationBudgetOptions{
				MaxEdgesScannedPerRequest: c.Sidecar.ActivationMaxEdgesScannedPerRequest,
				MaxNeighborsPerNode:       c.Sidecar.ActivationMaxNeighborsPerNode,
				MaxActivationWall:         time.Duration(c.Sidecar.ActivationMaxWallMS) * time.Millisecond,
			},
		},
	}, nil
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
	jobs := make([]memorycore.RetentionJobName, 0, len(c.Retention.Jobs))
	for _, job := range c.Retention.Jobs {
		jobs = append(jobs, memorycore.RetentionJobName(job))
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

func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	if err := rejectUnknownYAMLFields(value, configYAMLFields, ""); err != nil {
		return err
	}
	var patch configPatch
	if err := value.Decode(&patch); err != nil {
		return err
	}
	if err := validateConfigPatchMigration(patch); err != nil {
		return err
	}
	cfg := Default()
	applyConfigPatch(&cfg, patch)
	*c = cfg
	return nil
}

func (c *Config) UnmarshalJSON(data []byte) error {
	var patch configPatch
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		return err
	}
	if err := validateConfigPatchMigration(patch); err != nil {
		return err
	}
	cfg := Default()
	applyConfigPatch(&cfg, patch)
	*c = cfg
	return nil
}

type configPatch struct {
	SchemaVersion *string             `yaml:"schema_version" json:"schema_version"`
	Enabled       *bool               `yaml:"enabled" json:"enabled"`
	Core          *corePatch          `yaml:"core" json:"core"`
	Retrieval     *retrievalPatch     `yaml:"retrieval" json:"retrieval"`
	QueryAnalysis *queryAnalysisPatch `yaml:"query_analysis" json:"query_analysis"`
	Sidecar       *sidecarPatch       `yaml:"sidecar" json:"sidecar"`
	Retention     *retentionPatch     `yaml:"retention" json:"retention"`
	Mirror        *mirrorPatch        `yaml:"mirror" json:"mirror"`
}

type corePatch struct {
	DBPath      *string `yaml:"db_path" json:"db_path"`
	PersonaID   *string `yaml:"persona_id" json:"persona_id"`
	AutoMigrate *bool   `yaml:"auto_migrate" json:"auto_migrate"`
	EnableFTS   *bool   `yaml:"enable_fts" json:"enable_fts"`
}

type retrievalPatch struct {
	UseFTS                *bool   `yaml:"use_fts" json:"use_fts"`
	UseMirror             *bool   `yaml:"use_mirror" json:"use_mirror"`
	FinalMemoryCount      *int    `yaml:"final_memory_count" json:"final_memory_count"`
	ContextBudgetTokens   *int    `yaml:"context_budget_tokens" json:"context_budget_tokens"`
	AllowHistorical       *bool   `yaml:"allow_historical" json:"allow_historical"`
	AllowDeepArchive      *bool   `yaml:"allow_deep_archive" json:"allow_deep_archive"`
	SensitivityPermission *string `yaml:"sensitivity_permission" json:"sensitivity_permission"`
}

type queryAnalysisPatch struct {
	Provider                    *string                        `yaml:"provider" json:"provider"`
	Mode                        *string                        `yaml:"mode" json:"mode"`
	SidecarURL                  *string                        `yaml:"sidecar_url" json:"sidecar_url"`
	TimeoutMS                   *int                           `yaml:"timeout_ms" json:"timeout_ms"`
	ScorerVersion               *string                        `yaml:"scorer_version" json:"scorer_version"`
	RouterVersion               *string                        `yaml:"router_version" json:"router_version"`
	Thresholds                  *queryAnalysisThresholdsPatch  `yaml:"thresholds" json:"thresholds"`
	Budget                      *queryAnalysisBudgetPatch      `yaml:"budget" json:"budget"`
	Diagnostics                 *queryAnalysisDiagnosticsPatch `yaml:"diagnostics" json:"diagnostics"`
	MinConfidenceToOverride     *float64                       `yaml:"min_confidence_to_override" json:"min_confidence_to_override"`
	MinEntitySemanticConfidence *float64                       `yaml:"min_entity_semantic_confidence" json:"min_entity_semantic_confidence"`
	MinRuleFit                  *float64                       `yaml:"min_rule_fit" json:"min_rule_fit"`
	MinAnchorReadiness          *float64                       `yaml:"min_anchor_readiness" json:"min_anchor_readiness"`
	SemanticNeedThreshold       *float64                       `yaml:"semantic_need" json:"semantic_need"`
	MinComplexityForSemantic    *float64                       `yaml:"min_complexity_for_semantic" json:"min_complexity_for_semantic"`
	FullSemanticComplexity      *float64                       `yaml:"full_semantic_complexity" json:"full_semantic_complexity"`
	DecomposeSemanticComplexity *float64                       `yaml:"decompose_complexity" json:"decompose_complexity"`
	MinSemanticFieldConfidence  *float64                       `yaml:"min_semantic_field_confidence" json:"min_semantic_field_confidence"`
	MinOverrideMargin           *float64                       `yaml:"min_override_margin" json:"min_override_margin"`
	HighSafetyRiskThreshold     *float64                       `yaml:"high_safety_risk" json:"high_safety_risk"`
	MaxQueryRewrites            *int                           `yaml:"max_query_rewrites" json:"max_query_rewrites"`
	MaxSemanticAnchors          *int                           `yaml:"max_semantic_anchors" json:"max_semantic_anchors"`
	SemanticTotalEnergyCap      *float64                       `yaml:"semantic_total_energy_cap" json:"semantic_total_energy_cap"`
	MaxGeneratedDenseWeightSum  *float64                       `yaml:"max_generated_dense_weight_sum" json:"max_generated_dense_weight_sum"`
	IncludeRationaleSummary     *bool                          `yaml:"include_rationale_summary" json:"include_rationale_summary"`
}

type queryAnalysisThresholdsPatch struct {
	MinRuleFit                  *float64 `yaml:"min_rule_fit" json:"min_rule_fit"`
	MinAnchorReadiness          *float64 `yaml:"min_anchor_readiness" json:"min_anchor_readiness"`
	SemanticNeedThreshold       *float64 `yaml:"semantic_need" json:"semantic_need"`
	MinComplexityForSemantic    *float64 `yaml:"min_complexity_for_semantic" json:"min_complexity_for_semantic"`
	FullSemanticComplexity      *float64 `yaml:"full_semantic_complexity" json:"full_semantic_complexity"`
	DecomposeSemanticComplexity *float64 `yaml:"decompose_complexity" json:"decompose_complexity"`
	MinSemanticFieldConfidence  *float64 `yaml:"min_semantic_field_confidence" json:"min_semantic_field_confidence"`
	MinOverrideMargin           *float64 `yaml:"min_override_margin" json:"min_override_margin"`
	HighSafetyRiskThreshold     *float64 `yaml:"high_safety_risk" json:"high_safety_risk"`
}

type queryAnalysisBudgetPatch struct {
	MaxSemanticCallsPerSession     *int `yaml:"max_semantic_calls_per_session" json:"max_semantic_calls_per_session"`
	MaxSemanticCallsPer1000Queries *int `yaml:"max_semantic_calls_per_1000_queries" json:"max_semantic_calls_per_1000_queries"`
	MaxSemanticLatencyMS           *int `yaml:"max_semantic_latency_ms" json:"max_semantic_latency_ms"`
}

type queryAnalysisDiagnosticsPatch struct {
	IncludeScoreBreakdown *bool    `yaml:"include_score_breakdown" json:"include_score_breakdown"`
	IncludeReasonCodes    *bool    `yaml:"include_reason_codes" json:"include_reason_codes"`
	SampleRate            *float64 `yaml:"sample_rate" json:"sample_rate"`
}

func validateConfigPatchMigration(patch configPatch) error {
	if patch.QueryAnalysis == nil {
		return nil
	}
	query := *patch.QueryAnalysis
	if query.Thresholds != nil && query.hasLegacyThresholdPatch() {
		return fmt.Errorf("query_analysis.thresholds cannot be mixed with legacy query_analysis flat threshold fields; move legacy fields under query_analysis.thresholds")
	}
	return nil
}

func (p queryAnalysisPatch) hasLegacyThresholdPatch() bool {
	return p.MinRuleFit != nil ||
		p.MinAnchorReadiness != nil ||
		p.SemanticNeedThreshold != nil ||
		p.MinComplexityForSemantic != nil ||
		p.FullSemanticComplexity != nil ||
		p.DecomposeSemanticComplexity != nil ||
		p.MinSemanticFieldConfidence != nil ||
		p.MinOverrideMargin != nil ||
		p.HighSafetyRiskThreshold != nil
}

type sidecarPatch struct {
	Enabled                             *bool   `yaml:"enabled" json:"enabled"`
	URL                                 *string `yaml:"url" json:"url"`
	Adapter                             *string `yaml:"adapter" json:"adapter"`
	TotalTimeoutMS                      *int    `yaml:"total_timeout_ms" json:"total_timeout_ms"`
	MirrorTimeoutMS                     *int    `yaml:"mirror_timeout_ms" json:"mirror_timeout_ms"`
	ActivationTimeoutMS                 *int    `yaml:"activation_timeout_ms" json:"activation_timeout_ms"`
	RerankTimeoutMS                     *int    `yaml:"rerank_timeout_ms" json:"rerank_timeout_ms"`
	BreakerEnabled                      *bool   `yaml:"breaker_enabled" json:"breaker_enabled"`
	BreakerWindow                       *int    `yaml:"breaker_window" json:"breaker_window"`
	BreakerFailureThreshold             *int    `yaml:"breaker_failure_threshold" json:"breaker_failure_threshold"`
	BreakerOpenMS                       *int    `yaml:"breaker_open_ms" json:"breaker_open_ms"`
	ActivationMaxEdgesScannedPerRequest *int    `yaml:"activation_max_edges_scanned_per_request" json:"activation_max_edges_scanned_per_request"`
	ActivationMaxNeighborsPerNode       *int    `yaml:"activation_max_neighbors_per_node" json:"activation_max_neighbors_per_node"`
	ActivationMaxWallMS                 *int    `yaml:"activation_max_wall_ms" json:"activation_max_wall_ms"`
}

type retentionPatch struct {
	Jobs                 *[]string `yaml:"jobs" json:"jobs"`
	DeepArchiveAfterDays *int      `yaml:"deep_archive_after_days" json:"deep_archive_after_days"`
}

type mirrorPatch struct {
	SyncLimit *int `yaml:"sync_limit" json:"sync_limit"`
}

func applyConfigPatch(cfg *Config, patch configPatch) {
	if patch.SchemaVersion != nil {
		cfg.SchemaVersion = *patch.SchemaVersion
	}
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.Core != nil {
		applyCorePatch(&cfg.Core, *patch.Core)
	}
	if patch.Retrieval != nil {
		applyRetrievalPatch(&cfg.Retrieval, *patch.Retrieval)
	}
	if patch.QueryAnalysis != nil {
		applyQueryAnalysisPatch(&cfg.QueryAnalysis, *patch.QueryAnalysis)
	}
	if patch.Sidecar != nil {
		applySidecarPatch(&cfg.Sidecar, *patch.Sidecar)
	}
	if patch.Retention != nil {
		applyRetentionPatch(&cfg.Retention, *patch.Retention)
	}
	if patch.Mirror != nil {
		applyMirrorPatch(&cfg.Mirror, *patch.Mirror)
	}
}

func applyQueryAnalysisPatch(cfg *QueryAnalysisConfig, patch queryAnalysisPatch) {
	if patch.Provider != nil {
		cfg.Provider = *patch.Provider
	}
	if patch.Mode != nil {
		cfg.Mode = *patch.Mode
	}
	if patch.SidecarURL != nil {
		cfg.SidecarURL = *patch.SidecarURL
	}
	if patch.TimeoutMS != nil {
		cfg.TimeoutMS = *patch.TimeoutMS
	}
	if patch.ScorerVersion != nil {
		cfg.ScorerVersion = *patch.ScorerVersion
	}
	if patch.RouterVersion != nil {
		cfg.RouterVersion = *patch.RouterVersion
	}
	if patch.Thresholds != nil {
		applyQueryAnalysisThresholdsPatch(&cfg.Thresholds, *patch.Thresholds)
		syncQueryAnalysisFlatThresholdsFromNested(cfg)
	}
	if patch.Budget != nil {
		applyQueryAnalysisBudgetPatch(&cfg.Budget, *patch.Budget)
	}
	if patch.Diagnostics != nil {
		applyQueryAnalysisDiagnosticsPatch(&cfg.Diagnostics, *patch.Diagnostics)
	}
	if patch.MinConfidenceToOverride != nil {
		cfg.MinConfidenceToOverride = *patch.MinConfidenceToOverride
	}
	if patch.MinEntitySemanticConfidence != nil {
		cfg.MinEntitySemanticConfidence = *patch.MinEntitySemanticConfidence
	}
	if patch.MinRuleFit != nil {
		cfg.MinRuleFit = *patch.MinRuleFit
		cfg.Thresholds.MinRuleFit = *patch.MinRuleFit
	}
	if patch.MinAnchorReadiness != nil {
		cfg.MinAnchorReadiness = *patch.MinAnchorReadiness
		cfg.Thresholds.MinAnchorReadiness = *patch.MinAnchorReadiness
	}
	if patch.SemanticNeedThreshold != nil {
		cfg.SemanticNeedThreshold = *patch.SemanticNeedThreshold
		cfg.Thresholds.SemanticNeedThreshold = *patch.SemanticNeedThreshold
	}
	if patch.MinComplexityForSemantic != nil {
		cfg.MinComplexityForSemantic = *patch.MinComplexityForSemantic
		cfg.Thresholds.MinComplexityForSemantic = *patch.MinComplexityForSemantic
	}
	if patch.FullSemanticComplexity != nil {
		cfg.FullSemanticComplexity = *patch.FullSemanticComplexity
		cfg.Thresholds.FullSemanticComplexity = *patch.FullSemanticComplexity
	}
	if patch.DecomposeSemanticComplexity != nil {
		cfg.DecomposeSemanticComplexity = *patch.DecomposeSemanticComplexity
		cfg.Thresholds.DecomposeSemanticComplexity = *patch.DecomposeSemanticComplexity
	}
	if patch.MinSemanticFieldConfidence != nil {
		cfg.MinSemanticFieldConfidence = *patch.MinSemanticFieldConfidence
		cfg.Thresholds.MinSemanticFieldConfidence = *patch.MinSemanticFieldConfidence
	}
	if patch.MinOverrideMargin != nil {
		cfg.MinOverrideMargin = *patch.MinOverrideMargin
		cfg.Thresholds.MinOverrideMargin = *patch.MinOverrideMargin
	}
	if patch.HighSafetyRiskThreshold != nil {
		cfg.HighSafetyRiskThreshold = *patch.HighSafetyRiskThreshold
		cfg.Thresholds.HighSafetyRiskThreshold = *patch.HighSafetyRiskThreshold
	}
	if patch.MaxQueryRewrites != nil {
		cfg.MaxQueryRewrites = *patch.MaxQueryRewrites
	}
	if patch.MaxSemanticAnchors != nil {
		cfg.MaxSemanticAnchors = *patch.MaxSemanticAnchors
	}
	if patch.SemanticTotalEnergyCap != nil {
		cfg.SemanticTotalEnergyCap = *patch.SemanticTotalEnergyCap
	}
	if patch.MaxGeneratedDenseWeightSum != nil {
		cfg.MaxGeneratedDenseWeightSum = *patch.MaxGeneratedDenseWeightSum
	}
	if patch.IncludeRationaleSummary != nil {
		cfg.IncludeRationaleSummary = *patch.IncludeRationaleSummary
	}
}

func applyQueryAnalysisThresholdsPatch(cfg *QueryAnalysisThresholdsConfig, patch queryAnalysisThresholdsPatch) {
	if patch.MinRuleFit != nil {
		cfg.MinRuleFit = *patch.MinRuleFit
	}
	if patch.MinAnchorReadiness != nil {
		cfg.MinAnchorReadiness = *patch.MinAnchorReadiness
	}
	if patch.SemanticNeedThreshold != nil {
		cfg.SemanticNeedThreshold = *patch.SemanticNeedThreshold
	}
	if patch.MinComplexityForSemantic != nil {
		cfg.MinComplexityForSemantic = *patch.MinComplexityForSemantic
	}
	if patch.FullSemanticComplexity != nil {
		cfg.FullSemanticComplexity = *patch.FullSemanticComplexity
	}
	if patch.DecomposeSemanticComplexity != nil {
		cfg.DecomposeSemanticComplexity = *patch.DecomposeSemanticComplexity
	}
	if patch.MinSemanticFieldConfidence != nil {
		cfg.MinSemanticFieldConfidence = *patch.MinSemanticFieldConfidence
	}
	if patch.MinOverrideMargin != nil {
		cfg.MinOverrideMargin = *patch.MinOverrideMargin
	}
	if patch.HighSafetyRiskThreshold != nil {
		cfg.HighSafetyRiskThreshold = *patch.HighSafetyRiskThreshold
	}
}

func applyQueryAnalysisBudgetPatch(cfg *QueryAnalysisBudgetConfig, patch queryAnalysisBudgetPatch) {
	if patch.MaxSemanticCallsPerSession != nil {
		cfg.MaxSemanticCallsPerSession = *patch.MaxSemanticCallsPerSession
	}
	if patch.MaxSemanticCallsPer1000Queries != nil {
		cfg.MaxSemanticCallsPer1000Queries = *patch.MaxSemanticCallsPer1000Queries
	}
	if patch.MaxSemanticLatencyMS != nil {
		cfg.MaxSemanticLatencyMS = *patch.MaxSemanticLatencyMS
	}
}

func applyQueryAnalysisDiagnosticsPatch(cfg *QueryAnalysisDiagnosticsConfig, patch queryAnalysisDiagnosticsPatch) {
	if patch.IncludeScoreBreakdown != nil {
		cfg.IncludeScoreBreakdown = *patch.IncludeScoreBreakdown
	}
	if patch.IncludeReasonCodes != nil {
		cfg.IncludeReasonCodes = *patch.IncludeReasonCodes
	}
	if patch.SampleRate != nil {
		cfg.SampleRate = *patch.SampleRate
	}
}

func applyQueryAnalysisThresholdDefaults(cfg *QueryAnalysisThresholdsConfig, defaults QueryAnalysisThresholdsConfig) {
	if cfg.MinRuleFit == 0 {
		cfg.MinRuleFit = defaults.MinRuleFit
	}
	if cfg.MinAnchorReadiness == 0 {
		cfg.MinAnchorReadiness = defaults.MinAnchorReadiness
	}
	if cfg.SemanticNeedThreshold == 0 {
		cfg.SemanticNeedThreshold = defaults.SemanticNeedThreshold
	}
	if cfg.MinComplexityForSemantic == 0 {
		cfg.MinComplexityForSemantic = defaults.MinComplexityForSemantic
	}
	if cfg.FullSemanticComplexity == 0 {
		cfg.FullSemanticComplexity = defaults.FullSemanticComplexity
	}
	if cfg.DecomposeSemanticComplexity == 0 {
		cfg.DecomposeSemanticComplexity = defaults.DecomposeSemanticComplexity
	}
	if cfg.MinSemanticFieldConfidence == 0 {
		cfg.MinSemanticFieldConfidence = defaults.MinSemanticFieldConfidence
	}
	if cfg.MinOverrideMargin == 0 {
		cfg.MinOverrideMargin = defaults.MinOverrideMargin
	}
	if cfg.HighSafetyRiskThreshold == 0 {
		cfg.HighSafetyRiskThreshold = defaults.HighSafetyRiskThreshold
	}
}

func applyQueryAnalysisBudgetDefaults(cfg *QueryAnalysisBudgetConfig, defaults QueryAnalysisBudgetConfig) {
	if cfg.MaxSemanticCallsPerSession == 0 {
		cfg.MaxSemanticCallsPerSession = defaults.MaxSemanticCallsPerSession
	}
	if cfg.MaxSemanticCallsPer1000Queries == 0 {
		cfg.MaxSemanticCallsPer1000Queries = defaults.MaxSemanticCallsPer1000Queries
	}
	if cfg.MaxSemanticLatencyMS == 0 {
		cfg.MaxSemanticLatencyMS = defaults.MaxSemanticLatencyMS
	}
}

func syncQueryAnalysisFlatThresholdsFromNested(cfg *QueryAnalysisConfig) {
	cfg.MinRuleFit = cfg.Thresholds.MinRuleFit
	cfg.MinAnchorReadiness = cfg.Thresholds.MinAnchorReadiness
	cfg.SemanticNeedThreshold = cfg.Thresholds.SemanticNeedThreshold
	cfg.MinComplexityForSemantic = cfg.Thresholds.MinComplexityForSemantic
	cfg.FullSemanticComplexity = cfg.Thresholds.FullSemanticComplexity
	cfg.DecomposeSemanticComplexity = cfg.Thresholds.DecomposeSemanticComplexity
	cfg.MinSemanticFieldConfidence = cfg.Thresholds.MinSemanticFieldConfidence
	cfg.MinOverrideMargin = cfg.Thresholds.MinOverrideMargin
	cfg.HighSafetyRiskThreshold = cfg.Thresholds.HighSafetyRiskThreshold
}

func applyCorePatch(cfg *CoreConfig, patch corePatch) {
	if patch.DBPath != nil {
		cfg.DBPath = *patch.DBPath
	}
	if patch.PersonaID != nil {
		cfg.PersonaID = *patch.PersonaID
	}
	if patch.AutoMigrate != nil {
		cfg.AutoMigrate = *patch.AutoMigrate
	}
	if patch.EnableFTS != nil {
		cfg.EnableFTS = *patch.EnableFTS
	}
}

func applyRetrievalPatch(cfg *RetrievalConfig, patch retrievalPatch) {
	if patch.UseFTS != nil {
		cfg.UseFTS = *patch.UseFTS
	}
	if patch.UseMirror != nil {
		cfg.UseMirror = *patch.UseMirror
	}
	if patch.FinalMemoryCount != nil {
		cfg.FinalMemoryCount = *patch.FinalMemoryCount
	}
	if patch.ContextBudgetTokens != nil {
		cfg.ContextBudgetTokens = *patch.ContextBudgetTokens
	}
	if patch.AllowHistorical != nil {
		cfg.AllowHistorical = *patch.AllowHistorical
	}
	if patch.AllowDeepArchive != nil {
		cfg.AllowDeepArchive = *patch.AllowDeepArchive
	}
	if patch.SensitivityPermission != nil {
		cfg.SensitivityPermission = *patch.SensitivityPermission
	}
}

func applySidecarPatch(cfg *SidecarConfig, patch sidecarPatch) {
	if patch.Enabled != nil {
		cfg.Enabled = *patch.Enabled
	}
	if patch.URL != nil {
		cfg.URL = *patch.URL
	}
	if patch.Adapter != nil {
		cfg.Adapter = *patch.Adapter
	}
	if patch.TotalTimeoutMS != nil {
		cfg.TotalTimeoutMS = *patch.TotalTimeoutMS
	}
	if patch.MirrorTimeoutMS != nil {
		cfg.MirrorTimeoutMS = *patch.MirrorTimeoutMS
	}
	if patch.ActivationTimeoutMS != nil {
		cfg.ActivationTimeoutMS = *patch.ActivationTimeoutMS
	}
	if patch.RerankTimeoutMS != nil {
		cfg.RerankTimeoutMS = *patch.RerankTimeoutMS
	}
	if patch.BreakerEnabled != nil {
		cfg.BreakerEnabled = *patch.BreakerEnabled
	}
	if patch.BreakerWindow != nil {
		cfg.BreakerWindow = *patch.BreakerWindow
	}
	if patch.BreakerFailureThreshold != nil {
		cfg.BreakerFailureThreshold = *patch.BreakerFailureThreshold
	}
	if patch.BreakerOpenMS != nil {
		cfg.BreakerOpenMS = *patch.BreakerOpenMS
	}
	if patch.ActivationMaxEdgesScannedPerRequest != nil {
		cfg.ActivationMaxEdgesScannedPerRequest = *patch.ActivationMaxEdgesScannedPerRequest
	}
	if patch.ActivationMaxNeighborsPerNode != nil {
		cfg.ActivationMaxNeighborsPerNode = *patch.ActivationMaxNeighborsPerNode
	}
	if patch.ActivationMaxWallMS != nil {
		cfg.ActivationMaxWallMS = *patch.ActivationMaxWallMS
	}
}

func applyRetentionPatch(cfg *RetentionConfig, patch retentionPatch) {
	if patch.Jobs != nil {
		cfg.Jobs = append([]string(nil), (*patch.Jobs)...)
	}
	if patch.DeepArchiveAfterDays != nil {
		cfg.DeepArchiveAfterDays = *patch.DeepArchiveAfterDays
	}
}

func applyMirrorPatch(cfg *MirrorConfig, patch mirrorPatch) {
	if patch.SyncLimit != nil {
		cfg.SyncLimit = *patch.SyncLimit
	}
}

type yamlFieldSet map[string]yamlFieldSet

var configYAMLFields = yamlFieldSet{
	"schema_version": nil,
	"enabled":        nil,
	"core": {
		"db_path":      nil,
		"persona_id":   nil,
		"auto_migrate": nil,
		"enable_fts":   nil,
	},
	"retrieval": {
		"use_fts":                nil,
		"use_mirror":             nil,
		"final_memory_count":     nil,
		"context_budget_tokens":  nil,
		"allow_historical":       nil,
		"allow_deep_archive":     nil,
		"sensitivity_permission": nil,
	},
	"query_analysis": {
		"provider":       nil,
		"mode":           nil,
		"sidecar_url":    nil,
		"timeout_ms":     nil,
		"scorer_version": nil,
		"router_version": nil,
		"thresholds": {
			"min_rule_fit":                  nil,
			"min_anchor_readiness":          nil,
			"semantic_need":                 nil,
			"min_complexity_for_semantic":   nil,
			"full_semantic_complexity":      nil,
			"decompose_complexity":          nil,
			"min_semantic_field_confidence": nil,
			"min_override_margin":           nil,
			"high_safety_risk":              nil,
		},
		"budget": {
			"max_semantic_calls_per_session":      nil,
			"max_semantic_calls_per_1000_queries": nil,
			"max_semantic_latency_ms":             nil,
		},
		"diagnostics": {
			"include_score_breakdown": nil,
			"include_reason_codes":    nil,
			"sample_rate":             nil,
		},
		"min_confidence_to_override":     nil,
		"min_entity_semantic_confidence": nil,
		"min_rule_fit":                   nil,
		"min_anchor_readiness":           nil,
		"semantic_need":                  nil,
		"min_complexity_for_semantic":    nil,
		"full_semantic_complexity":       nil,
		"decompose_complexity":           nil,
		"min_semantic_field_confidence":  nil,
		"min_override_margin":            nil,
		"high_safety_risk":               nil,
		"max_query_rewrites":             nil,
		"max_semantic_anchors":           nil,
		"semantic_total_energy_cap":      nil,
		"max_generated_dense_weight_sum": nil,
		"include_rationale_summary":      nil,
	},
	"sidecar": {
		"enabled":                   nil,
		"url":                       nil,
		"adapter":                   nil,
		"total_timeout_ms":          nil,
		"mirror_timeout_ms":         nil,
		"activation_timeout_ms":     nil,
		"rerank_timeout_ms":         nil,
		"breaker_enabled":           nil,
		"breaker_window":            nil,
		"breaker_failure_threshold": nil,
		"breaker_open_ms":           nil,
		"activation_max_edges_scanned_per_request": nil,
		"activation_max_neighbors_per_node":        nil,
		"activation_max_wall_ms":                   nil,
	},
	"retention": {
		"jobs":                    nil,
		"deep_archive_after_days": nil,
	},
	"mirror": {
		"sync_limit": nil,
	},
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
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for idx := 0; idx+1 < len(node.Content); idx += 2 {
		key := node.Content[idx].Value
		childFields, ok := allowed[key]
		fieldPath := joinFieldPath(prefix, key)
		if !ok {
			return fmt.Errorf("unknown config field %s", fieldPath)
		}
		if childFields != nil {
			if err := rejectUnknownYAMLFields(node.Content[idx+1], childFields, fieldPath); err != nil {
				return err
			}
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
