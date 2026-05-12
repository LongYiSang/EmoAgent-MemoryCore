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

type RebuildSearchDocumentsRequest struct {
	PersonaID string
}

type RebuildSearchDocumentsResult struct {
	Upserted int
}
