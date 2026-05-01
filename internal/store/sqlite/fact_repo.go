package sqlite

import (
	"context"
	"database/sql"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type FactRepository struct {
	db *sql.DB
}

func NewFactRepository(db *sql.DB) *FactRepository {
	return &FactRepository{db: db}
}

func (r *FactRepository) Insert(ctx context.Context, fact core.Fact) error {
	fact = normalizeFact(fact)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO facts (
    id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
    content_summary, fact_type, valid_from, valid_to,
    extraction_confidence, extraction_confidence_score, extraction_reasoning,
    importance, valence, arousal, sensitivity_level,
    validity_status, visibility_status, lifecycle_status,
    pinned, pin_reason, pin_actor, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fact.ID,
		fact.PersonaID,
		nullableString(fact.SubjectEntityID),
		fact.Predicate,
		nullableString(fact.ObjectEntityID),
		nullableString(fact.ObjectLiteral),
		fact.ContentSummary,
		string(fact.FactType),
		nullableTime(fact.ValidFrom),
		nullableTime(fact.ValidTo),
		string(fact.ExtractionConfidence),
		fact.ExtractionConfidenceScore,
		nullableString(fact.ExtractionReasoning),
		fact.Importance,
		fact.Valence,
		fact.Arousal,
		string(fact.SensitivityLevel),
		string(fact.ValidityStatus),
		string(fact.VisibilityStatus),
		string(fact.LifecycleStatus),
		boolInt(fact.Pinned),
		nullableString(fact.PinReason),
		nullableString(fact.PinActor),
		boolInt(fact.Searchable),
	)
	return err
}

func (r *FactRepository) Get(ctx context.Context, personaID string, factID string) (core.Fact, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
       content_summary, fact_type, valid_from, valid_to,
       extraction_confidence, extraction_confidence_score, extraction_reasoning,
       importance, valence, arousal, sensitivity_level,
       validity_status, visibility_status, lifecycle_status,
       pinned, pin_reason, pin_actor, searchable, created_at, updated_at
FROM facts
WHERE persona_id = ? AND id = ?`, personaID, factID)
	return scanFact(row)
}

type factScanner interface {
	Scan(dest ...any) error
}

func scanFact(row factScanner) (core.Fact, error) {
	var fact core.Fact
	var subjectEntityID, objectEntityID, objectLiteral sql.NullString
	var validFrom, validTo, reasoning, pinReason, pinActor, updatedAt sql.NullString
	var createdAt string
	var pinned, searchable int
	if err := row.Scan(
		&fact.ID,
		&fact.PersonaID,
		&subjectEntityID,
		&fact.Predicate,
		&objectEntityID,
		&objectLiteral,
		&fact.ContentSummary,
		&fact.FactType,
		&validFrom,
		&validTo,
		&fact.ExtractionConfidence,
		&fact.ExtractionConfidenceScore,
		&reasoning,
		&fact.Importance,
		&fact.Valence,
		&fact.Arousal,
		&fact.SensitivityLevel,
		&fact.ValidityStatus,
		&fact.VisibilityStatus,
		&fact.LifecycleStatus,
		&pinned,
		&pinReason,
		&pinActor,
		&searchable,
		&createdAt,
		&updatedAt,
	); err != nil {
		return core.Fact{}, err
	}
	fact.SubjectEntityID = stringPtr(subjectEntityID)
	fact.ObjectEntityID = stringPtr(objectEntityID)
	fact.ObjectLiteral = stringPtr(objectLiteral)
	fact.ValidFrom = timePtr(validFrom)
	fact.ValidTo = timePtr(validTo)
	fact.ExtractionReasoning = stringPtr(reasoning)
	fact.Pinned = intBool(pinned)
	fact.PinReason = stringPtr(pinReason)
	fact.PinActor = stringPtr(pinActor)
	fact.Searchable = intBool(searchable)
	fact.CreatedAt = parseTime(createdAt)
	fact.UpdatedAt = timePtr(updatedAt)
	return fact, nil
}

func normalizeFact(fact core.Fact) core.Fact {
	if fact.FactType == "" {
		fact.FactType = core.FactTypeStablePreference
	}
	if fact.ExtractionConfidence == "" {
		fact.ExtractionConfidence = core.ExtractionConfidenceExplicit
	}
	if fact.ExtractionConfidenceScore == 0 {
		fact.ExtractionConfidenceScore = 0.5
	}
	if fact.Importance == 0 {
		fact.Importance = 0.5
	}
	fact.SensitivityLevel = defaultSensitivity(fact.SensitivityLevel)
	fact.ValidityStatus = defaultValidity(fact.ValidityStatus)
	if fact.VisibilityStatus == "" {
		fact.VisibilityStatus = core.VisibilityVisible
		fact.Searchable = true
	}
	fact.LifecycleStatus = defaultLifecycle(fact.LifecycleStatus)
	return fact
}
