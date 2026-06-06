package memorycore

const (
	EntityTypeUser       = "user"
	EntityTypeAgent      = "agent"
	EntityTypePerson     = "person"
	EntityTypePlace      = "place"
	EntityTypeOrg        = "org"
	EntityTypeConcept    = "concept"
	EntityTypeObject     = "object"
	EntityTypeEventTopic = "event_topic"

	AliasTypeSurface      = "surface"
	AliasTypeNickname     = "nickname"
	AliasTypeTranslation  = "translation"
	AliasTypeAbbreviation = "abbreviation"
)

type EntityAliasInput struct {
	ID              string
	Alias           string
	AliasType       string
	Confidence      float64
	SourceEpisodeID *string
}

type EnsureEntityRequest struct {
	ID               string
	PersonaID        string
	CanonicalName    string
	EntityType       string
	Description      *string
	VisibilityStatus string
	SensitivityLevel string
	Searchable       *bool
	Aliases          []EntityAliasInput
}

type AddEntityAliasRequest struct {
	ID              string
	PersonaID       string
	EntityID        string
	Alias           string
	AliasType       string
	Confidence      float64
	SourceEpisodeID *string
}

type Entity struct {
	ID               string
	PersonaID        string
	CanonicalName    string
	EntityType       string
	Description      *string
	VisibilityStatus string
	SensitivityLevel string
	Searchable       bool
	Aliases          []EntityAlias
}

type EntityAlias struct {
	ID              string
	PersonaID       string
	EntityID        string
	Alias           string
	AliasType       string
	Confidence      float64
	SourceEpisodeID *string
}
