package memorycore

import "time"

const (
	ConsolidationTriggerManual        = "manual"
	ConsolidationTriggerAgentAffect   = "agent_affect"
	ConsolidationTriggerWorkCandidate = "work_candidate"

	ConsolidationActionInsert           = "insert"
	ConsolidationActionDiscardDuplicate = "discard_duplicate"
	ConsolidationActionReinforce        = "reinforce"
	ConsolidationActionSupersede        = "supersede"
	ConsolidationActionCoexist          = "coexist"
	ConsolidationActionMergeBaseline    = "merge_baseline"
	ConsolidationActionReject           = "reject"
	ConsolidationActionNeedsReview      = "needs_review"

	ConsolidationStatusInserted    = "inserted"
	ConsolidationStatusDiscarded   = "discarded"
	ConsolidationStatusReinforced  = "reinforced"
	ConsolidationStatusSuperseded  = "superseded"
	ConsolidationStatusCoexisted   = "coexisted"
	ConsolidationStatusRejected    = "rejected"
	ConsolidationStatusNeedsReview = "needs_review"

	FactTypeCoreIdentity        = "core_identity"
	FactTypeSignificantEvent    = "significant_event"
	FactTypeStablePreference    = "stable_preference"
	FactTypeRelationalState     = "relational_state"
	FactTypeCommitment          = "commitment"
	FactTypeTransientContext    = "transient_context"
	FactTypeTaskRelevantContext = "task_relevant_context"

	ConfidenceExplicit  = "explicit"
	ConfidenceInferred  = "inferred"
	ConfidenceAmbiguous = "ambiguous"

	ValidityValid       = "valid"
	ValidityInvalidated = "invalidated"
	ValidityUncertain   = "uncertain"
)

type ConsolidateCandidateRequest struct {
	PersonaID string
	SessionID *string
	Trigger   string
	Candidate ManualFactCandidate
	Policy    ConsolidationPolicy
}

type ManualFactCandidate struct {
	SubjectEntityID  string
	Predicate        string
	ObjectEntityID   *string
	ObjectLiteral    *string
	ContentSummary   string
	FactType         string
	ValidFrom        *time.Time
	ValidTo          *time.Time
	Confidence       string
	ConfidenceScore  float64
	Importance       float64
	Valence          float64
	Arousal          float64
	Sensitivity      string
	SourceEpisodeIDs []string
	Pinned           bool
	UserRequested    bool
}

type ConsolidationPolicy struct {
	Action                      string
	Approved                    bool
	AllowManualPinWithoutSource bool
}

type ConsolidationResult struct {
	Action            string
	Status            string
	Fact              *Fact
	ExistingFact      *Fact
	SupersededFactIDs []string
	LinkIDs           []string
	RejectedReason    string
	NeedsReviewReason string
}

type Fact struct {
	ID                 string
	PersonaID          string
	SubjectEntityID    *string
	Predicate          string
	ObjectEntityID     *string
	ObjectLiteral      *string
	ContentSummary     string
	FactType           string
	ValidFrom          *time.Time
	ValidTo            *time.Time
	Confidence         string
	ConfidenceScore    float64
	Importance         float64
	Valence            float64
	Arousal            float64
	Sensitivity        string
	ValidityStatus     string
	VisibilityStatus   string
	LifecycleStatus    string
	Pinned             bool
	ReinforcementCount int
	Searchable         bool
	CreatedAt          time.Time
	UpdatedAt          *time.Time
}
