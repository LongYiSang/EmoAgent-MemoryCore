package memorycore

import "time"

const (
	ExtractionRequestSchemaVersion  = "memory_extraction_protocol.v0.1.request"
	ExtractionResponseSchemaVersion = "memory_extraction_protocol.v0.1"

	ExtractionTriggerIdleDetect    = "idle_detect"
	ExtractionTriggerSessionEnd    = "session_end"
	ExtractionTriggerManualPin     = "manual_pin"
	ExtractionTriggerManualForget  = "manual_forget"
	ExtractionTriggerWorkCandidate = "work_candidate"
	ExtractionTriggerReprocess     = "reprocess"
)

type ExtractionRequest struct {
	SchemaVersion          string                      `json:"schema_version"`
	RequestID              string                      `json:"request_id"`
	PersonaID              string                      `json:"persona_id"`
	SessionID              *string                     `json:"session_id"`
	Trigger                string                      `json:"trigger"`
	Now                    time.Time                   `json:"now"`
	Timezone               string                      `json:"timezone"`
	Episodes               []ExtractionEpisode         `json:"episodes"`
	ApprovedWorkCandidates []ExtractionWorkCandidate   `json:"approved_work_candidates"`
	KnownEntities          []ExtractionKnownEntity     `json:"known_entities"`
	PredicateSchemas       []ExtractionPredicateSchema `json:"predicate_schemas"`
	Policy                 ExtractionPolicy            `json:"policy"`
}

type ExtractionEpisode struct {
	EpisodeID        string    `json:"episode_id"`
	Role             string    `json:"role"`
	Content          string    `json:"content"`
	OccurredAt       time.Time `json:"occurred_at"`
	SourceType       string    `json:"source_type"`
	PrevEpisodeID    *string   `json:"prev_episode_id"`
	NextEpisodeID    *string   `json:"next_episode_id"`
	VisibilityStatus string    `json:"visibility_status"`
	SensitivityLevel string    `json:"sensitivity_level"`
}

type ExtractionWorkCandidate struct {
	CandidateID string `json:"candidate_id"`
	Summary     string `json:"summary"`
}

type ExtractionKnownEntity struct {
	EntityID         string                  `json:"entity_id"`
	CanonicalName    string                  `json:"canonical_name"`
	EntityType       string                  `json:"entity_type"`
	Aliases          []ExtractionEntityAlias `json:"aliases"`
	Description      *string                 `json:"description"`
	VisibilityStatus string                  `json:"visibility_status"`
	SensitivityLevel string                  `json:"sensitivity_level"`
}

type ExtractionEntityAlias struct {
	Alias           string  `json:"alias"`
	AliasType       string  `json:"alias_type"`
	Confidence      float64 `json:"confidence"`
	SourceEpisodeID *string `json:"source_episode_id"`
}

type ExtractionPredicateSchema struct {
	Predicate          string   `json:"predicate"`
	CanonicalLabel     *string  `json:"canonical_label"`
	DefaultFactType    *string  `json:"default_fact_type"`
	Cardinality        string   `json:"cardinality"`
	ConflictPolicy     string   `json:"conflict_policy"`
	TemporalBehavior   string   `json:"temporal_behavior"`
	ObjectKind         string   `json:"object_kind"`
	DefaultTauDays     *float64 `json:"default_tau_days"`
	DefaultImportance  float64  `json:"default_importance"`
	AllowInference     bool     `json:"allow_inference"`
	SensitiveByDefault bool     `json:"sensitive_by_default"`
}

type ExtractionPolicy struct {
	AllowSensitiveExtraction bool `json:"allow_sensitive_extraction"`
	AllowInference           bool `json:"allow_inference"`
	ManualPin                bool `json:"manual_pin"`
	ManualForget             bool `json:"manual_forget"`
	MaxFacts                 int  `json:"max_facts"`
	MaxLinks                 int  `json:"max_links"`
}

type ExtractionResponse struct {
	SchemaVersion      string                          `json:"schema_version"`
	RequestID          string                          `json:"request_id"`
	PersonaID          string                          `json:"persona_id"`
	SessionID          *string                         `json:"session_id"`
	Trigger            string                          `json:"trigger"`
	SourceWindow       ExtractionSourceWindow          `json:"source_window"`
	Entities           []ExtractedEntityCandidate      `json:"entities"`
	Facts              []ExtractedFactCandidate        `json:"facts"`
	Links              []ExtractedLinkCandidate        `json:"links"`
	AffectEvents       []ExtractedAffectEventCandidate `json:"affect_events"`
	DeletionIntents    []ExtractedDeletionIntent       `json:"deletion_intents"`
	PinIntents         []ExtractedPinIntent            `json:"pin_intents"`
	CorrectionHints    []ExtractedCorrectionHint       `json:"correction_hints"`
	RejectedCandidates []ExtractedRejectedCandidate    `json:"rejected_candidates"`
	QualityFlags       []string                        `json:"quality_flags"`
	GateSummary        ExtractionGateSummary           `json:"gate_summary"`
}

type ExtractionSourceWindow struct {
	EpisodeIDs []string   `json:"episode_ids"`
	StartedAt  *time.Time `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at"`
}

type ExtractedEntityCandidate struct {
	CandidateID      string   `json:"candidate_id"`
	CanonicalName    string   `json:"canonical_name"`
	EntityType       string   `json:"entity_type"`
	Aliases          []string `json:"aliases"`
	Description      *string  `json:"description"`
	Confidence       float64  `json:"confidence"`
	SourceEpisodeIDs []string `json:"source_episode_ids"`
	MergeHint        string   `json:"merge_hint"`
	KnownEntityID    *string  `json:"known_entity_id"`
	SensitivityLevel string   `json:"sensitivity_level"`
	Reasoning        *string  `json:"reasoning"`
}

type ExtractedFactCandidate struct {
	CandidateID               string     `json:"candidate_id"`
	SubjectEntityCandidateID  string     `json:"subject_entity_candidate_id"`
	Predicate                 string     `json:"predicate"`
	ObjectEntityCandidateID   *string    `json:"object_entity_candidate_id"`
	ObjectLiteral             *string    `json:"object_literal"`
	ContentSummary            string     `json:"content_summary"`
	FactType                  string     `json:"fact_type"`
	ValidFrom                 *time.Time `json:"valid_from"`
	ValidTo                   *time.Time `json:"valid_to"`
	TemporalPrecision         string     `json:"temporal_precision"`
	ExtractionConfidence      string     `json:"extraction_confidence"`
	ExtractionConfidenceScore float64    `json:"extraction_confidence_score"`
	Importance                float64    `json:"importance"`
	Valence                   float64    `json:"valence"`
	Arousal                   float64    `json:"arousal"`
	SensitivityLevel          string     `json:"sensitivity_level"`
	SourceEpisodeIDs          []string   `json:"source_episode_ids"`
	EvidenceNotes             *string    `json:"evidence_notes"`
	Reasoning                 *string    `json:"reasoning"`
	OperationHint             string     `json:"operation_hint"`
	Pinned                    bool       `json:"pinned"`
	UserRequested             bool       `json:"user_requested"`
	SearchableHint            bool       `json:"searchable_hint"`
	QualityDecision           string     `json:"quality_decision"`
	QualityReasons            []string   `json:"quality_reasons"`
}

type ExtractedLinkCandidate struct {
	CandidateID      string   `json:"candidate_id"`
	LinkType         string   `json:"link_type"`
	FromCandidateID  string   `json:"from_candidate_id"`
	ToCandidateID    string   `json:"to_candidate_id"`
	SourceEpisodeIDs []string `json:"source_episode_ids"`
	Confidence       float64  `json:"confidence"`
	Reasoning        *string  `json:"reasoning"`
}

type ExtractedAffectEventCandidate struct {
	CandidateID      string   `json:"candidate_id"`
	Scope            string   `json:"scope"`
	Label            string   `json:"label"`
	Valence          float64  `json:"valence"`
	Arousal          float64  `json:"arousal"`
	SourceEpisodeIDs []string `json:"source_episode_ids"`
	Confidence       float64  `json:"confidence"`
	Reasoning        *string  `json:"reasoning"`
}

type ExtractedDeletionIntent struct {
	CandidateID          string  `json:"candidate_id"`
	ForgetLevel          string  `json:"forget_level"`
	TargetDescription    string  `json:"target_description"`
	TargetNodeTypeHint   *string `json:"target_node_type_hint"`
	SourceEpisodeID      string  `json:"source_episode_id"`
	Confidence           float64 `json:"confidence"`
	Reasoning            *string `json:"reasoning"`
	RequiresConfirmation bool    `json:"requires_confirmation"`
}

type ExtractedPinIntent struct {
	CandidateID       string   `json:"candidate_id"`
	TargetCandidateID *string  `json:"target_candidate_id"`
	ContentSummary    string   `json:"content_summary"`
	SourceEpisodeIDs  []string `json:"source_episode_ids"`
	PinReason         string   `json:"pin_reason"`
	Confidence        float64  `json:"confidence"`
}

type ExtractedCorrectionHint struct {
	CandidateID    string  `json:"candidate_id"`
	CorrectedTopic string  `json:"corrected_topic"`
	NewCandidateID *string `json:"new_candidate_id"`
	OldMemoryRef   *string `json:"old_memory_ref"`
	Confidence     float64 `json:"confidence"`
	Reasoning      *string `json:"reasoning"`
}

type ExtractedRejectedCandidate struct {
	CandidateID string   `json:"candidate_id"`
	Kind        string   `json:"kind"`
	Reasons     []string `json:"reasons"`
}

type ExtractionGateSummary struct {
	AcceptedFactCount   int    `json:"accepted_fact_count"`
	NeedsReviewCount    int    `json:"needs_review_count"`
	RejectedCount       int    `json:"rejected_count"`
	HasDeletionIntent   bool   `json:"has_deletion_intent"`
	HasPinIntent        bool   `json:"has_pin_intent"`
	RequiresHumanReview bool   `json:"requires_human_review"`
	Notes               string `json:"notes"`
	RoutedCount         int    `json:"routed_count,omitempty"`
	NotAppliedCount     int    `json:"not_applied_count,omitempty"`
}

type ExtractionGateResult struct {
	RequestID               string                  `json:"request_id"`
	PersonaID               string                  `json:"persona_id"`
	Status                  string                  `json:"status"`
	ResponseDecisions       []CandidateGateDecision `json:"response_decisions"`
	FactDecisions           []CandidateGateDecision `json:"fact_decisions"`
	EntityDecisions         []CandidateGateDecision `json:"entity_decisions"`
	LinkDecisions           []CandidateGateDecision `json:"link_decisions"`
	AffectEventDecisions    []CandidateGateDecision `json:"affect_event_decisions"`
	DeletionIntentDecisions []CandidateGateDecision `json:"deletion_intent_decisions"`
	PinIntentDecisions      []CandidateGateDecision `json:"pin_intent_decisions"`
	CorrectionHintDecisions []CandidateGateDecision `json:"correction_hint_decisions"`
	Summary                 ExtractionGateSummary   `json:"summary"`
}

type CandidateGateDecision struct {
	CandidateID string   `json:"candidate_id"`
	Kind        string   `json:"kind"`
	Decision    string   `json:"decision"`
	ReasonCodes []string `json:"reason_codes"`
	Notes       string   `json:"notes"`
}

type ExtractionDryRunResult struct {
	RequestID              string                 `json:"request_id"`
	PersonaID              string                 `json:"persona_id"`
	GateResult             ExtractionGateResult   `json:"gate_result"`
	EntityPreview          []EntityApplyPreview   `json:"entity_preview"`
	FactPreview            []FactApplyPreview     `json:"fact_preview"`
	RoutedDeletionIntents  []DeletionIntentRoute  `json:"routed_deletion_intents"`
	RoutedPinIntents       []PinIntentRoute       `json:"routed_pin_intents"`
	NotAppliedLinks        []LinkCandidatePreview `json:"not_applied_links"`
	NotAppliedAffectEvents []AffectEventPreview   `json:"not_applied_affect_events"`
	Summary                DryRunSummary          `json:"summary"`
}

type EntityApplyPreview struct {
	CandidateID string `json:"candidate_id"`
	Action      string `json:"action"`
	EntityID    string `json:"entity_id,omitempty"`
	Decision    string `json:"decision"`
}

type FactApplyPreview struct {
	CandidateID   string   `json:"candidate_id"`
	Predicate     string   `json:"predicate"`
	Decision      string   `json:"decision"`
	ReasonCodes   []string `json:"reason_codes"`
	Pinned        bool     `json:"pinned"`
	UserRequested bool     `json:"user_requested"`
}

type DeletionIntentRoute struct {
	CandidateID string `json:"candidate_id"`
	RouteTo     string `json:"route_to"`
	Decision    string `json:"decision"`
}

type PinIntentRoute struct {
	CandidateID       string  `json:"candidate_id"`
	TargetCandidateID *string `json:"target_candidate_id"`
	Decision          string  `json:"decision"`
}

type LinkCandidatePreview struct {
	CandidateID string `json:"candidate_id"`
	LinkType    string `json:"link_type"`
	Decision    string `json:"decision"`
}

type AffectEventPreview struct {
	CandidateID string `json:"candidate_id"`
	Scope       string `json:"scope"`
	Decision    string `json:"decision"`
}

type DryRunSummary struct {
	AcceptedFacts int `json:"accepted_facts"`
	NeedsReview   int `json:"needs_review"`
	Rejected      int `json:"rejected"`
	Routed        int `json:"routed"`
	NotApplied    int `json:"not_applied"`
}

type ExtractionApplyResult struct {
	RequestID    string             `json:"request_id"`
	PersonaID    string             `json:"persona_id"`
	Status       string             `json:"status"`
	AppliedCount int                `json:"applied_count"`
	Results      []FactApplyResult  `json:"results"`
	Failures     []FactApplyFailure `json:"failures"`
}

type FactApplyResult struct {
	CandidateID string               `json:"candidate_id"`
	Status      string               `json:"status"`
	Result      *ConsolidationResult `json:"result,omitempty"`
}

type FactApplyFailure struct {
	CandidateID string `json:"candidate_id"`
	Reason      string `json:"reason"`
}
