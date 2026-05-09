package core

import "time"

type Persona struct {
	ID          string
	DisplayName string
	Description *string
}

type Session struct {
	ID        string
	PersonaID string
	Channel   Channel
	Title     *string
	Summary   *string
	StartedAt time.Time
	EndedAt   *time.Time
}

type Episode struct {
	ID               string
	PersonaID        string
	SessionID        string
	Role             Role
	Content          string
	ContentHash      string
	OccurredAt       time.Time
	SourceType       SourceType
	SourceRef        *string
	PrevEpisodeID    *string
	NextEpisodeID    *string
	VisibilityStatus VisibilityStatus
	SensitivityLevel SensitivityLevel
	Searchable       bool
}
