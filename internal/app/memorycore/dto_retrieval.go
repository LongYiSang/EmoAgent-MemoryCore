package memorycore

import (
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	MemoryBlockTypeFacts             = "facts"
	MemoryBlockTypeCausalContext     = "causal_context"
	MemoryBlockTypeHistoricalContext = "historical_context"
	MemoryBlockTypeProvenanceContext = "provenance_context"
	MemoryBlockTypeSupportiveContext = "supportive_context"
	MemoryBlockTypeExperienceContext = "experience_context"

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

const (
	QueryTimeModeCurrent         QueryTimeMode = "current"
	QueryTimeModeHistorical      QueryTimeMode = "historical"
	QueryTimeModeBitemporalCheck QueryTimeMode = "bitemporal_check"

	QuerySignalCausal      QuerySignal = "causal"
	QuerySignalHistorical  QuerySignal = "historical"
	QuerySignalProvenance  QuerySignal = "provenance"
	QuerySignalSensitivity QuerySignal = "sensitivity"
	QuerySignalDebug       QuerySignal = "debug"

	MemoryDomainRelationship          MemoryDomain = "relationship_memory"
	MemoryDomainUserProfile           MemoryDomain = "user_profile_memory"
	MemoryDomainWorkExperience        MemoryDomain = "work_experience_memory"
	MemoryDomainEnvironmentExperience MemoryDomain = "environment_experience_memory"

	MemoryAbilityDirectFact    MemoryAbility = "direct_fact"
	MemoryAbilityCausalExplain MemoryAbility = "causal_explain"
	MemoryAbilityHistorical    MemoryAbility = "historical"
	MemoryAbilityProvenance    MemoryAbility = "provenance"
	MemoryAbilityBoundary      MemoryAbility = "boundary"
	MemoryAbilitySupportive    MemoryAbility = "supportive"
	MemoryAbilityPlanning      MemoryAbility = "planning"
	MemoryAbilityStaticState   MemoryAbility = "static_state"
	MemoryAbilityDynamicState  MemoryAbility = "dynamic_state"
	MemoryAbilityWorkflow      MemoryAbility = "workflow"
	MemoryAbilityGotcha        MemoryAbility = "gotcha"
	MemoryAbilityPremiseCheck  MemoryAbility = "premise_check"

	EvidenceNeedExactObservation      EvidenceNeed = "exact_observation"
	EvidenceNeedStateTransition       EvidenceNeed = "state_transition"
	EvidenceNeedProcedureNote         EvidenceNeed = "procedure_note"
	EvidenceNeedGotchaNote            EvidenceNeed = "gotcha_note"
	EvidenceNeedPremiseCounterexample EvidenceNeed = "premise_counterexample"
	EvidenceNeedProvenanceSource      EvidenceNeed = "provenance_source"

	QueryEntityMentionKindCanonical QueryEntityMentionKind = "canonical_name"
	QueryEntityMentionKindAlias     QueryEntityMentionKind = "entity_alias"
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
	Raw            string
	Normalized     string
	Terms          []string
	EntityMentions []QueryEntityMention
	TimeMode       QueryTimeMode
	Signals        []QuerySignal
	MemoryDomain   MemoryDomain
	MemoryAbility  MemoryAbility
	EvidenceNeed   EvidenceNeed
}

type QueryEntityMention struct {
	EntityID      string
	CanonicalName string
	Alias         string
	MatchText     string
	MatchKind     QueryEntityMentionKind
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
	Status                string                       `json:"status"`
	SidecarCandidateCount int                          `json:"sidecar_candidate_count"`
	MappedCandidateCount  int                          `json:"mapped_candidate_count"`
	DroppedCandidateCount int                          `json:"dropped_candidate_count"`
	Candidates            []MirrorCandidateDiagnostics `json:"candidates,omitempty"`
}

type MirrorCandidateDiagnostics struct {
	TriviumNodeID int64   `json:"trivium_node_id,omitempty"`
	SQLiteFactID  string  `json:"sqlite_fact_id,omitempty"`
	Score         float64 `json:"score,omitempty"`
	Source        string  `json:"source,omitempty"`
	Rank          int     `json:"rank,omitempty"`
	DropReason    string  `json:"drop_reason,omitempty"`
}

type GraphActivationDiagnostics struct {
	Status                string                                `json:"status"`
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
	SafeCandidateCount int    `json:"safe_candidate_count"`
	ResultCount        int    `json:"result_count"`
	Degraded           bool   `json:"degraded"`
	FallbackReason     string `json:"fallback_reason,omitempty"`
}

type RebuildSearchDocumentsRequest struct {
	PersonaID string
}

type RebuildSearchDocumentsResult struct {
	Upserted int
}
