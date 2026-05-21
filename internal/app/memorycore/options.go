package memorycore

import "time"

type Options struct {
	DBPath            string
	PersonaID         string
	AutoMigrate       bool
	EnableFTS         bool
	Now               func() time.Time
	MirrorAdapter     MirrorAdapter
	QueryAnalysis     QueryAnalysisOptions
	SidecarResilience SidecarResilienceOptions
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
	Provider                    QueryAnalysisProvider
	Mode                        QueryAnalysisMode
	SidecarURL                  string
	Timeout                     time.Duration
	SoftJoinTimeout             time.Duration
	Cache                       *QueryAnalysisCache
	MinConfidenceToOverride     float64
	MinEntitySemanticConfidence float64
	MinRuleFit                  float64
	MinAnchorReadiness          float64
	SemanticNeedThreshold       float64
	MinComplexityForSemantic    float64
	FullSemanticComplexity      float64
	DecomposeSemanticComplexity float64
	MinSemanticFieldConfidence  float64
	MinOverrideMargin           float64
	HighSafetyRiskThreshold     float64
	MaxQueryRewrites            int
	MaxSemanticAnchors          int
	SemanticTotalEnergyCap      float64
	MaxGeneratedDenseWeightSum  float64
	IncludeRationaleSummary     bool
	DisableGeneratedDense       bool
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
