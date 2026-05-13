package memorycore

import "time"

const (
	MemoryBlockTypeFacts = "facts"

	MemorySuppressionReasonFatigue = "fatigue"
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
	Blocks        []MemoryBlock
	DoNotMention  []MemorySuppression
	TokenEstimate int
	Mirror        *MirrorRetrievalDiagnostics
}

type MemoryBlock struct {
	BlockType string
	Items     []MemoryContextItem
}

type MemoryContextItem struct {
	NodeType      string
	NodeID        string
	Summary       string
	Confidence    float64
	UsageGuidance string
}

type MemorySuppression struct {
	NodeType string
	NodeID   string
	Reason   string
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
	DropReason    string  `json:"drop_reason,omitempty"`
}

type RebuildSearchDocumentsRequest struct {
	PersonaID string
}

type RebuildSearchDocumentsResult struct {
	Upserted int
}
