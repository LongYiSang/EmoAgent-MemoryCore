package memorycore

import "time"

const (
	RoleUser        = "user"
	RoleAssistant   = "assistant"
	RoleSystem      = "system"
	RoleToolSummary = "tool_summary"
	RoleWorkReport  = "work_report"

	SourceTypeChat          = "chat"
	SourceTypeWorkCandidate = "work_candidate"
	SourceTypePlugin        = "plugin"
	SourceTypeSystem        = "system"
	SourceTypeImported      = "imported"

	VisibilityVisible   = "visible"
	VisibilityHidden    = "hidden"
	VisibilityRedacted  = "redacted"
	VisibilityForgotten = "forgotten"
	VisibilityPurged    = "purged"

	SensitivityNormal          = "normal"
	SensitivitySensitive       = "sensitive"
	SensitivityHighlySensitive = "highly_sensitive"
)

type AppendEpisodeRequest struct {
	ID               string
	PersonaID        string
	SessionID        string
	Role             string
	Content          string
	OccurredAt       time.Time
	SourceType       string
	SourceRef        *string
	VisibilityStatus string
	SensitivityLevel string
	Searchable       *bool
}

type Episode struct {
	ID               string
	PersonaID        string
	SessionID        string
	Role             string
	Content          string
	ContentHash      string
	OccurredAt       time.Time
	SourceType       string
	SourceRef        *string
	PrevEpisodeID    *string
	NextEpisodeID    *string
	VisibilityStatus string
	SensitivityLevel string
	Searchable       bool
}
