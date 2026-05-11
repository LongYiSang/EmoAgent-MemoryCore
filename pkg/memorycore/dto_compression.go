package memorycore

import "time"

type ApplyCompressionRequest struct {
	PersonaID     string
	SourceFactIDs []string
	Narrative     *NarrativeDraft
	Insights      []InsightDraft
	Now           time.Time
	DryRun        bool
}

type NarrativeDraft struct {
	ID               string
	Scope            string
	ScopeRef         string
	Summary          string
	EmotionalTone    string
	ValenceAvg       *float64
	ArousalAvg       *float64
	Importance       float64
	ValidFrom        *time.Time
	ValidTo          *time.Time
	SensitivityLevel string
}

type InsightDraft struct {
	ID               string
	InsightType      string
	Content          string
	Confidence       float64
	Importance       float64
	Valence          float64
	Arousal          float64
	SensitivityLevel string
}

type ApplyCompressionResult struct {
	NarrativeID             string
	InsightIDs              []string
	SourceFactsConsolidated int
	DerivedLinkIDs          []string
	SearchDocumentsSynced   int
	MirrorUpdatesEnqueued   int
	DryRun                  bool
}
