package core

type Entity struct {
	ID               string
	PersonaID        string
	CanonicalName    string
	EntityType       EntityType
	Description      *string
	VisibilityStatus VisibilityStatus
	SensitivityLevel SensitivityLevel
	Searchable       bool
}

type EntityAlias struct {
	ID              string
	PersonaID       string
	EntityID        string
	Alias           string
	AliasType       AliasType
	Confidence      float64
	SourceEpisodeID *string
}
