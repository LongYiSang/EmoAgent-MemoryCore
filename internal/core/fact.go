package core

import "time"

type Fact struct {
	ID                        string
	PersonaID                 string
	SubjectEntityID           *string
	Predicate                 string
	ObjectEntityID            *string
	ObjectLiteral             *string
	ContentSummary            string
	FactType                  FactType
	ValidFrom                 *time.Time
	ValidTo                   *time.Time
	ExtractionConfidence      ExtractionConfidence
	ExtractionConfidenceScore float64
	ExtractionReasoning       *string
	Importance                float64
	Valence                   float64
	Arousal                   float64
	SensitivityLevel          SensitivityLevel
	ValidityStatus            ValidityStatus
	VisibilityStatus          VisibilityStatus
	LifecycleStatus           LifecycleStatus
	Pinned                    bool
	PinReason                 *string
	PinActor                  *string
	Searchable                bool
	CreatedAt                 time.Time
	UpdatedAt                 *time.Time
}

type PredicateSchema struct {
	Predicate          string
	CanonicalLabel     *string
	DefaultFactType    *FactType
	Cardinality        string
	ConflictPolicy     ConflictPolicy
	TemporalBehavior   string
	ObjectKind         string
	DefaultTauDays     *float64
	DefaultImportance  float64
	AllowInference     bool
	SensitiveByDefault bool
}
