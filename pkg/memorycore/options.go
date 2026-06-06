package memorycore

import "time"

type Options struct {
	DBPath            string
	PersonaID         string
	AutoMigrate       bool
	EnableFTS         bool
	Timezone          string
	Now               func() time.Time
	MirrorBackend     MirrorBackend
	QueryAnalysis     QueryAnalysisOptions
	SidecarResilience SidecarResilienceOptions
	Extraction        ExtractionOptions
	SemanticOps       SemanticOpsOptions
	NaturalMemory     NaturalMemoryOptions
}

type SemanticOpsOptions struct {
	SemanticMirrorMetaEnabled       bool
	SemanticSidecarAuthTokenEnabled bool
	Dedup                           SemanticDedupOptions
	Forget                          SemanticForgetOptions
	Curation                        SemanticCurationOptions
}

type SemanticDedupOptions struct {
	Enabled          bool   `json:"enabled,omitempty"`
	Shadow           bool   `json:"shadow,omitempty"`
	Enforce          bool   `json:"enforce,omitempty"`
	CandidateLimit   int    `json:"candidate_limit,omitempty"`
	ThresholdProfile string `json:"threshold_profile,omitempty"`
}

type SemanticForgetOptions struct {
	PreviewEnabled bool
	ExecuteEnabled bool
}

type SemanticCurationOptions struct {
	Enabled                bool
	Mode                   string
	MaxNewFactsPerRun      int
	CandidateLimitPerFact  int
	MaxFactsPerGroup       int
	MinAutoApplyConfidence float64
	CandidateRetrieval     CurationCandidateRetrievalOptions
	IncludeFactTypes       []string
	ExcludeFactTypes       []string
	RawLog                 CurationRawLogOptions
	LLM                    CurationLLMOptions
}

type CurationCandidateRetrievalOptions struct {
	Mode                string  `json:"mode,omitempty"`
	MirrorMinSimilarity float64 `json:"mirror_min_similarity,omitempty"`
}

type CurationLLMOptions struct {
	Provider       ExtractionProviderOptions
	ProviderID     string
	ProviderKind   string
	Model          string
	Temperature    float64
	MaxTokens      int
	ResponseFormat ExtractionResponseFormat
	Timeout        time.Duration
	Thinking       *OpenAICompatibleThinkingOptions
}

type ExtractionOptions struct {
	Enabled        bool
	Provider       ExtractionProviderOptions
	Defaults       ExtractionDefaults
	Runtime        ExtractionRuntimeOptions
	Audit          ExtractionAuditOptions
	RawLog         ExtractionRawLogOptions
	PromptVersions ExtractionPromptVersionOptions
}

type ExtractionProviderOptions struct {
	Kind           string
	ID             string
	BaseURL        string
	APIKeyEnv      string
	Model          string
	Temperature    float64
	MaxTokens      int
	Timeout        time.Duration
	ResponseFormat ExtractionResponseFormat
	Thinking       *OpenAICompatibleThinkingOptions
}

const (
	ExtractionProviderDisabled         = "disabled"
	ExtractionProviderMock             = "mock"
	ExtractionProviderOpenAICompatible = "openai-compatible"
)

type ExtractionDefaults struct {
	Configured               bool
	Mode                     ExtractionRunMode
	Timezone                 string
	AllowSensitiveExtraction bool
	AllowInference           bool
	MaxFacts                 int
	MaxLinks                 int
	RequireCleanGate         bool
	ApplyAcceptedFacts       bool
	ExecuteDeletionIntents   bool
}

type ExtractionRuntimeOptions struct {
	Configured    bool
	UsePreFilter  bool
	RepairEnabled bool
}

type ExtractionAuditOptions struct {
	Configured bool
	Enabled    bool
	Force      bool
}

type ExtractionPromptVersionOptions struct {
	Extraction string
	PreFilter  string
	Repair     string
}

type QueryAnalysisProvider string

const (
	QueryAnalysisProviderNone    QueryAnalysisProvider = ""
	QueryAnalysisProviderSidecar QueryAnalysisProvider = "sidecar"
)

type QueryAnalysisMode string

const (
	QueryAnalysisModeRuleOnly                QueryAnalysisMode = ""
	QueryAnalysisModeRuleOnlyExplicit        QueryAnalysisMode = "rule_only"
	QueryAnalysisModeSemanticAlways          QueryAnalysisMode = "semantic_always"
	QueryAnalysisModeSemanticOnLowConfidence QueryAnalysisMode = "semantic_on_low_confidence"
	QueryAnalysisModeSemanticRewriteOnly     QueryAnalysisMode = "semantic_rewrite_only"
	QueryAnalysisModeLegacyOnly              QueryAnalysisMode = "legacy_only"
	QueryAnalysisModeShadowAdaptive          QueryAnalysisMode = "shadow_adaptive"
	QueryAnalysisModeAdaptive                QueryAnalysisMode = "adaptive"
	QueryAnalysisModeAdaptiveSafe            QueryAnalysisMode = "adaptive_safe"
	QueryAnalysisModeAdaptiveFull            QueryAnalysisMode = "adaptive_full"
)

type QueryAnalysisOptions struct {
	Provider                         QueryAnalysisProvider
	Mode                             QueryAnalysisMode
	SidecarURL                       string
	Timeout                          time.Duration
	SoftJoinTimeout                  time.Duration
	Cache                            *QueryAnalysisCache
	ScorerVersion                    string
	RouterVersion                    string
	MinConfidenceToOverride          float64
	MinEntitySemanticConfidence      float64
	MinRuleFit                       float64
	MinAnchorReadiness               float64
	SemanticNeedThreshold            float64
	MinComplexityForSemantic         float64
	FullSemanticComplexity           float64
	DecomposeSemanticComplexity      float64
	MinSemanticFieldConfidence       float64
	MinOverrideMargin                float64
	HighSafetyRiskThreshold          float64
	MaxSemanticCallsPerSession       int
	MaxSemanticCallsPerSessionWindow time.Duration
	MaxSemanticCallsPer1000Queries   int
	MaxSemanticLatency               time.Duration
	DiagnosticsConfigured            bool
	DiagnosticsIncludeScoreBreakdown bool
	DiagnosticsIncludeReasonCodes    bool
	DiagnosticsSampleRate            float64
	MaxQueryRewrites                 int
	MaxSemanticAnchors               int
	SemanticTotalEnergyCap           float64
	MaxGeneratedDenseWeightSum       float64
	IncludeRationaleSummary          bool
	DisableGeneratedDense            bool
}

type SidecarBreakerMode string

const (
	SidecarBreakerModeDefault  SidecarBreakerMode = ""
	SidecarBreakerModeEnabled  SidecarBreakerMode = "enabled"
	SidecarBreakerModeDisabled SidecarBreakerMode = "disabled"
)

type SidecarStageTimeouts struct {
	Total      time.Duration
	Mirror     time.Duration
	Activation time.Duration
	Rerank     time.Duration
}

type SidecarBreakerOptions struct {
	Mode             SidecarBreakerMode
	Window           int
	FailureThreshold int
	OpenFor          time.Duration
}

type SidecarActivationBudgetOptions struct {
	MaxEdgesScannedPerRequest int
	MaxNeighborsPerNode       int
	MaxActivationWall         time.Duration
}

type SidecarResilienceOptions struct {
	Timeouts         SidecarStageTimeouts
	Breaker          SidecarBreakerOptions
	ActivationBudget SidecarActivationBudgetOptions
}
