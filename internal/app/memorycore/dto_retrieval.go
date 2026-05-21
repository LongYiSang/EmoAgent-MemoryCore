package memorycore

import (
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	MemoryBlockTypeFacts                      = "facts"
	MemoryBlockTypeRelevantCausalMemory       = "relevant_causal_memory"
	MemoryBlockTypeHistoricalTransitionMemory = "historical_transition_memory"
	MemoryBlockTypeProvenanceMemory           = "provenance_memory"
	MemoryBlockTypePremiseCheckMemory         = "premise_check_memory"
	MemoryBlockTypeRelationshipArcMemory      = "relationship_arc_memory"
	MemoryBlockTypeSupportiveMemory           = "supportive_memory"
	MemoryBlockTypeExperienceContext          = "experience_context"

	MemoryBlockTypeCausalContext     = MemoryBlockTypeRelevantCausalMemory
	MemoryBlockTypeHistoricalContext = MemoryBlockTypeHistoricalTransitionMemory
	MemoryBlockTypeProvenanceContext = MemoryBlockTypeProvenanceMemory
	MemoryBlockTypeSupportiveContext = MemoryBlockTypeSupportiveMemory

	MemoryHistoricalStatusCurrent    = "current"
	MemoryHistoricalStatusHistorical = "historical"
	MemoryHistoricalStatusSuperseded = "superseded"

	MemorySuppressionReasonFatigue       = core.MemorySuppressionReasonFatigue
	MemorySuppressionReasonMMRDuplicate  = core.MemorySuppressionReasonMMRDuplicate
	MemorySuppressionReasonContextBudget = core.MemorySuppressionReasonContextBudget
)

type QueryTimeMode string
type QuerySignal string
type MemoryDomain string
type MemoryAbility string
type EvidenceNeed string
type QueryEntityMentionKind string
type QueryAnalysisSource string

const (
	QueryTimeModeCurrent         QueryTimeMode = "current"
	QueryTimeModeHistorical      QueryTimeMode = "historical"
	QueryTimeModeBitemporalCheck QueryTimeMode = "bitemporal_check"

	QuerySignalCausal                QuerySignal = "causal"
	QuerySignalHistorical            QuerySignal = "historical"
	QuerySignalProvenance            QuerySignal = "provenance"
	QuerySignalSensitivity           QuerySignal = "sensitivity"
	QuerySignalDebug                 QuerySignal = "debug"
	QuerySignalPremiseCheck          QuerySignal = "premise_check"
	QuerySignalRelationshipArc       QuerySignal = "relationship_arc"
	QuerySignalForgetDelete          QuerySignal = "forget_delete"
	QuerySignalPastEventDirectFact   QuerySignal = "past_event_direct_fact"
	QuerySignalStateTransition       QuerySignal = "state_transition"
	QuerySignalProvenanceSource      QuerySignal = "provenance_source"
	QuerySignalCausalChain           QuerySignal = "causal_chain"
	QuerySignalPremiseCounterexample QuerySignal = "premise_counterexample"
	QuerySignalEventBundle           QuerySignal = "event_bundle"
	QuerySignalReflectionSummary     QuerySignal = "reflection_summary"
	QuerySignalExactFact             QuerySignal = "exact_fact"

	MemoryDomainRelationship          MemoryDomain = "relationship_memory"
	MemoryDomainUserProfile           MemoryDomain = "user_profile_memory"
	MemoryDomainWorkExperience        MemoryDomain = "work_experience_memory"
	MemoryDomainEnvironmentExperience MemoryDomain = "environment_experience_memory"

	MemoryAbilityDirectFact      MemoryAbility = "direct_fact"
	MemoryAbilityCausalExplain   MemoryAbility = "causal_explain"
	MemoryAbilityHistorical      MemoryAbility = "historical"
	MemoryAbilityProvenance      MemoryAbility = "provenance"
	MemoryAbilityBoundary        MemoryAbility = "boundary"
	MemoryAbilitySupportive      MemoryAbility = "supportive"
	MemoryAbilityPlanning        MemoryAbility = "planning"
	MemoryAbilityStaticState     MemoryAbility = "static_state"
	MemoryAbilityDynamicState    MemoryAbility = "dynamic_state"
	MemoryAbilityWorkflow        MemoryAbility = "workflow"
	MemoryAbilityGotcha          MemoryAbility = "gotcha"
	MemoryAbilityPremiseCheck    MemoryAbility = "premise_check"
	MemoryAbilityRelationshipArc MemoryAbility = "relationship_arc"

	EvidenceNeedExactObservation      EvidenceNeed = "exact_observation"
	EvidenceNeedStateTransition       EvidenceNeed = "state_transition"
	EvidenceNeedProcedureNote         EvidenceNeed = "procedure_note"
	EvidenceNeedGotchaNote            EvidenceNeed = "gotcha_note"
	EvidenceNeedPremiseCounterexample EvidenceNeed = "premise_counterexample"
	EvidenceNeedProvenanceSource      EvidenceNeed = "provenance_source"
	EvidenceNeedRelationshipTimeline  EvidenceNeed = "relationship_timeline"

	QueryEntityMentionKindCanonical QueryEntityMentionKind = "canonical_name"
	QueryEntityMentionKindAlias     QueryEntityMentionKind = "entity_alias"

	QueryAnalysisSourceRuleOnly         QueryAnalysisSource = "rule_only"
	QueryAnalysisSourceSemantic         QueryAnalysisSource = "semantic"
	QueryAnalysisSourceMerged           QueryAnalysisSource = "merged"
	QueryAnalysisSourceSemanticFallback QueryAnalysisSource = "semantic_failed_rule_fallback"
)

type RetrievalRequest struct {
	PersonaID string
	SessionID *string
	QueryText string
	Now       time.Time
	Policy    RetrievalPolicy
	Context   RetrievalAffectContext
}

type RetrievalPolicy struct {
	SensitivityPermission string
	AllowHistorical       bool
	AllowDeepArchive      bool
	FinalMemoryCount      int
	ContextBudgetTokens   int
	UseFTS                bool
	UseMirror             bool
}

type RetrievalAffectContext struct {
	UserMoodLabel         string
	RelationshipMoodLabel string
}

type MemoryContext struct {
	Blocks          []MemoryBlock
	DoNotMention    []MemorySuppression
	TokenEstimate   int
	Mirror          *MirrorRetrievalDiagnostics
	GraphActivation *GraphActivationDiagnostics `json:"graph_activation,omitempty"`
	Rerank          *RerankDiagnostics          `json:"rerank,omitempty"`
	QueryAnalysis   *QueryAnalysis
	AnchorFusion    *AnchorFusionDiagnostics `json:"anchor_fusion,omitempty"`
}

type QueryAnalysis struct {
	Raw               string
	Normalized        string
	Terms             []string
	EntityMentions    []QueryEntityMention
	TimeMode          QueryTimeMode
	Signals           []QuerySignal
	MemoryDomain      MemoryDomain
	MemoryAbility     MemoryAbility
	EvidenceNeed      EvidenceNeed
	Source            QueryAnalysisSource
	Confidence        float64
	FieldConfidence   QueryAnalysisConfidence
	Scores            QueryAnalysisScores
	Probes            QueryAnchorProbe
	Decision          QueryAnalysisDecision
	Evidence          []QueryAnalysisEvidence    `json:",omitempty"`
	Alternatives      []QueryAnalysisAlternative `json:",omitempty"`
	QueryRewrites     []QueryRewrite             `json:",omitempty"`
	SemanticAnchors   []SemanticAnchor           `json:",omitempty"`
	ContextBlockHints []string                   `json:",omitempty"`
	PolicyHints       QueryPolicyHints
	Diagnostics       *QueryAnalysisDiagnostics `json:",omitempty"`
}

type QueryEntityMention struct {
	EntityID      string
	CanonicalName string
	Alias         string
	MatchText     string
	MatchKind     QueryEntityMentionKind
}

type QueryRewrite struct {
	Text    string
	Purpose string
	Weight  float64
}

type SemanticAnchor struct {
	Text       string
	AnchorType string
	EntityID   string
	Weight     float64
	Confidence float64
}

type QueryAnalysisConfidence struct {
	Overall          float64
	TimeMode         float64
	MemoryAbility    float64
	MemoryDomain     float64
	EvidenceNeed     float64
	EntityResolution float64
}

type QueryAnalysisScores struct {
	RuleFit                     float64
	AnchorReadiness             float64
	ExpectedRetrievalConfidence float64
	SemanticNeed                float64
	Complexity                  float64
	Ambiguity                   float64
	Specificity                 float64
	SafetyRisk                  float64
	IntentEvidence              float64
	TimeEvidence                float64
	DomainEvidence              float64
	EvidenceNeedEvidence        float64
	EntityResolution            float64
	FieldConsistency            float64
	DefaultFallbackPenalty      float64
	MultiIntentConflictPenalty  float64
	SensitivityPenalty          float64
}

type QueryAnchorProbe struct {
	EntityExactConf        float64
	EntityAmbiguity        float64
	SparseProbeConf        float64
	PredicateProbeConf     float64
	RecentProbeConf        float64
	PinnedCoreProbeConf    float64
	NarrativeProbeConf     float64
	FallbackSearchHitCount int
	Top1Score              float64
	Top2Score              float64
	Top1Margin             float64
	Breakdown              []QueryAnchorProbeBreakdown
}

type QueryAnchorProbeBreakdown struct {
	Source      string
	Confidence  float64
	HitCount    int
	TopScore    float64
	SecondScore float64
	Reason      string
	Error       string
}

type QueryAnalysisDecision struct {
	UseSemantic      bool
	SemanticMode     string
	RetrievalMode    string
	ReasonCodes      []string
	ThresholdVersion string
	ScorerVersion    string
}

type QueryAnalysisEvidence struct {
	Field     string
	Signal    string
	MatchText string
	SpanStart int
	SpanEnd   int
	Weight    float64
	Detector  string
}

type QueryAnalysisAlternative struct {
	Field       string
	Value       string
	Confidence  float64
	ReasonCodes []string
	Detector    string
}

type QueryPolicyHints struct {
	PreferEvidencedByLinks bool
	PreferSupersedesLinks  bool
	PreferCausalLinks      bool
	PreferCounterexamples  bool
	PreferNarratives       bool
	MaxHopsHint            int
}

type QueryAnalysisDiagnostics struct {
	ScorerVersion           string
	RuleConfidenceLegacy    float64
	RuleConfidenceReason    string
	SemanticDecisionLegacy  bool
	MinConfidenceToOverride float64
	Signals                 []string
	EntityMentionCount      int
	Scores                  QueryAnalysisScores
	FieldConfidence         QueryAnalysisConfidence
	RuleDecision            QueryAnalysisDecision
	AdaptiveDecision        QueryAnalysisDecision
	RuleEvidence            []QueryAnalysisEvidence
	RuleAlternatives        []QueryAnalysisAlternative
	SemanticStatus          string
	SemanticProvider        string
	SemanticModel           string
	PromptVersion           string
	SemanticLatencyMs       int64
	FallbackReason          string
	RewriteCount            int
	SemanticAnchorCount     int
	DroppedRewriteCount     int
	DroppedRewriteReasons   []string
	DroppedSemanticAnchorCount   int
	DroppedSemanticAnchorReasons []string
	EnglishRewriteCount     int
	SemanticDriftCount      int
	FieldMergeDecisions     []FieldMergeDecision
	SemanticAnalysis        *SemanticQueryAnalysisDiagnostics `json:"semantic_analysis,omitempty"`
}

type SemanticQueryAnalysisDiagnostics struct {
	TimeMode          string                                  `json:"time_mode,omitempty"`
	SemanticMode      string                                  `json:"semantic_mode,omitempty"`
	Signals           []string                                `json:"signals,omitempty"`
	MemoryDomain      string                                  `json:"memory_domain,omitempty"`
	MemoryAbility     string                                  `json:"memory_ability,omitempty"`
	EvidenceNeed      string                                  `json:"evidence_need,omitempty"`
	Confidence        float64                                 `json:"confidence,omitempty"`
	FieldConfidence   QueryAnalysisConfidence                 `json:"field_confidence,omitempty"`
	FieldProposals    map[string]SemanticFieldProposal        `json:"field_proposals,omitempty"`
	Scores            QueryAnalysisScores                     `json:"scores,omitempty"`
	Probes            QueryAnchorProbe                        `json:"probes,omitempty"`
	Decision          QueryAnalysisDecision                   `json:"decision,omitempty"`
	Evidence          []QueryAnalysisEvidence                 `json:"evidence,omitempty"`
	Alternatives      []QueryAnalysisAlternative              `json:"alternatives,omitempty"`
	EntityMentions    []SemanticQueryEntityMentionDiagnostics `json:"entity_mentions,omitempty"`
	QueryRewrites     []QueryRewrite                          `json:"query_rewrites,omitempty"`
	SemanticAnchors   []SemanticAnchor                        `json:"semantic_anchors,omitempty"`
	Subqueries        []string                                `json:"subqueries,omitempty"`
	SafetyNotes       []string                                `json:"safety_notes,omitempty"`
	ContextBlockHints []string                                `json:"context_block_hints,omitempty"`
	PolicyHints       QueryPolicyHints                        `json:"policy_hints,omitempty"`
}

type SemanticFieldProposal struct {
	Value      string   `json:"value,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
}

type FieldMergeDecision struct {
	Field              string   `json:"field,omitempty"`
	RuleValue          string   `json:"rule_value,omitempty"`
	SemanticValue      string   `json:"semantic_value,omitempty"`
	RuleConfidence     float64  `json:"rule_confidence,omitempty"`
	SemanticConfidence float64  `json:"semantic_confidence,omitempty"`
	Reason             string   `json:"reason,omitempty"`
	Evidence           []string `json:"evidence,omitempty"`
	UseSemantic        bool     `json:"use_semantic,omitempty"`
}

type SemanticQueryEntityMentionDiagnostics struct {
	EntityID      string  `json:"entity_id,omitempty"`
	CanonicalName string  `json:"canonical_name,omitempty"`
	Alias         string  `json:"alias,omitempty"`
	MatchText     string  `json:"match_text,omitempty"`
	MatchKind     string  `json:"match_kind,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

type MemoryBlock struct {
	BlockType string
	Items     []MemoryContextItem
}

type MemoryContextItem struct {
	NodeType         string
	NodeID           string
	Summary          string
	Confidence       float64
	UsageGuidance    string
	HistoricalStatus string                 `json:",omitempty"`
	ValidFrom        *time.Time             `json:",omitempty"`
	ValidTo          *time.Time             `json:",omitempty"`
	SourceRefs       []MemorySourceRef      `json:",omitempty"`
	RelatedFacts     []MemoryRelatedFactRef `json:",omitempty"`
	DoNotOverstate   bool                   `json:",omitempty"`
}

type MemorySourceRef struct {
	EpisodeID     string
	SessionID     string
	SessionTitle  string
	OccurredAt    time.Time
	SourceStatus  string
	EvidenceCount int
	QuoteAllowed  bool
}

type MemoryRelatedFactRef struct {
	NodeType         string
	NodeID           string
	Summary          string
	LinkType         string
	Direction        string
	HistoricalStatus string
}

type MemorySuppression struct {
	NodeType string
	NodeID   string
	Reason   string
}

type AnchorFusionDiagnostics struct {
	Seeds []FusedAnchor `json:"seeds,omitempty"`
}

type FusedAnchor struct {
	NodeID           string                  `json:"node_id"`
	NodeType         string                  `json:"node_type"`
	FusedAnchorScore float64                 `json:"fused_anchor_score"`
	SeedEnergy       float64                 `json:"seed_energy"`
	SourceBreakdown  []AnchorSourceBreakdown `json:"source_breakdown,omitempty"`
}

type AnchorSourceBreakdown struct {
	Source          string  `json:"source"`
	Rank            int     `json:"rank"`
	RawScore        float64 `json:"raw_score"`
	Weight          float64 `json:"weight"`
	RRFContribution float64 `json:"rrf_contribution"`
	DebugReason     string  `json:"debug_reason,omitempty"`
}

type MirrorRetrievalDiagnostics struct {
	Status                       string                               `json:"status"`
	Degraded                     bool                                 `json:"degraded"`
	FallbackReason               string                               `json:"fallback_reason,omitempty"`
	LatencyMs                    int64                                `json:"latency_ms,omitempty"`
	SidecarCandidateCount        int                                  `json:"sidecar_candidate_count"`
	MappedCandidateCount         int                                  `json:"mapped_candidate_count"`
	DroppedCandidateCount        int                                  `json:"dropped_candidate_count"`
	EmbeddingCacheHits           int                                  `json:"embedding_cache_hits,omitempty"`
	EmbeddingCacheMisses         int                                  `json:"embedding_cache_misses,omitempty"`
	EmbeddingLiveCallCount       int                                  `json:"embedding_live_call_count,omitempty"`
	QueryCount                   int                                  `json:"query_count,omitempty"`
	RawQueryCount                int                                  `json:"raw_query_count,omitempty"`
	RewriteQueryCount            int                                  `json:"rewrite_query_count,omitempty"`
	AnchorQueryCount             int                                  `json:"anchor_query_count,omitempty"`
	MergedCandidateCount         int                                  `json:"merged_candidate_count,omitempty"`
	QueryTrimCount               int                                  `json:"query_trim_count,omitempty"`
	DenseEmbeddingWallLatencyMs  int64                                `json:"dense_embedding_wall_latency_ms,omitempty"`
	DenseEmbeddingBatchLatencyMs int64                                `json:"dense_embedding_batch_latency_ms,omitempty"`
	DenseSearchTotalLatencyMs    int64                                `json:"dense_search_total_latency_ms,omitempty"`
	QueryCountTrimmedByBudget    int                                  `json:"query_count_trimmed_by_budget,omitempty"`
	PerQuery                     []MirrorCandidatePerQueryDiagnostics `json:"per_query,omitempty"`
	Candidates                   []MirrorCandidateDiagnostics         `json:"candidates,omitempty"`
}

type MirrorCandidateDiagnostics struct {
	TriviumNodeID  int64   `json:"trivium_node_id,omitempty"`
	SQLiteFactID   string  `json:"sqlite_fact_id,omitempty"`
	Score          float64 `json:"score,omitempty"`
	Source         string  `json:"source,omitempty"`
	PrimaryPurpose string  `json:"primary_purpose,omitempty"`
	Rank           int     `json:"rank,omitempty"`
	HitCount       int     `json:"hit_count,omitempty"`
	DropReason     string  `json:"drop_reason,omitempty"`
}

type MirrorCandidatePerQueryDiagnostics struct {
	Source    string `json:"source,omitempty"`
	Purpose   string `json:"purpose,omitempty"`
	Count     int    `json:"count,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
}

type GraphActivationDiagnostics struct {
	Status                string                                `json:"status"`
	Degraded              bool                                  `json:"degraded"`
	FallbackReason        string                                `json:"fallback_reason,omitempty"`
	LatencyMs             int64                                 `json:"latency_ms,omitempty"`
	SidecarCandidateCount int                                   `json:"sidecar_candidate_count"`
	MappedCandidateCount  int                                   `json:"mapped_candidate_count"`
	DroppedCandidateCount int                                   `json:"dropped_candidate_count"`
	Candidates            []GraphActivationCandidateDiagnostics `json:"candidates,omitempty"`
}

type GraphActivationCandidateDiagnostics struct {
	TriviumNodeID int64                 `json:"trivium_node_id,omitempty"`
	SQLiteNodeID  string                `json:"sqlite_node_id,omitempty"`
	NodeType      string                `json:"node_type,omitempty"`
	Score         float64               `json:"score,omitempty"`
	Source        string                `json:"source,omitempty"`
	Rank          int                   `json:"rank,omitempty"`
	DropReason    string                `json:"drop_reason,omitempty"`
	Paths         []GraphActivationPath `json:"paths,omitempty"`
}

type GraphActivationPath struct {
	TriviumNodeIDs []int64  `json:"trivium_node_ids,omitempty"`
	LinkTypes      []string `json:"link_types,omitempty"`
}

type RerankDiagnostics struct {
	Status             string `json:"status"`
	SkippedReason      string `json:"skipped_reason,omitempty"`
	InputCount         int    `json:"input_count,omitempty"`
	SafeCandidateCount int    `json:"safe_candidate_count"`
	ResultCount        int    `json:"result_count"`
	Degraded           bool   `json:"degraded"`
	FallbackReason     string `json:"fallback_reason,omitempty"`
	LatencyMs          int64  `json:"latency_ms,omitempty"`
}

type RebuildSearchDocumentsRequest struct {
	PersonaID string
}

type RebuildSearchDocumentsResult struct {
	Upserted int
}
