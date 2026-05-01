package sqlite

import (
	"context"
	"database/sql"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type PredicateRepository struct {
	db *sql.DB
}

func NewPredicateRepository(db *sql.DB) *PredicateRepository {
	return &PredicateRepository{db: db}
}

func (r *PredicateRepository) Get(ctx context.Context, predicate string) (core.PredicateSchema, error) {
	var schema core.PredicateSchema
	var label, defaultFactType sql.NullString
	var tau sql.NullFloat64
	var allowInference, sensitiveByDefault int
	if err := r.db.QueryRowContext(ctx, `
SELECT predicate, canonical_label, default_fact_type, cardinality, conflict_policy,
       temporal_behavior, object_kind, default_tau_days, default_importance,
       allow_inference, sensitive_by_default
FROM predicate_schemas
WHERE predicate = ?`, predicate).Scan(
		&schema.Predicate,
		&label,
		&defaultFactType,
		&schema.Cardinality,
		&schema.ConflictPolicy,
		&schema.TemporalBehavior,
		&schema.ObjectKind,
		&tau,
		&schema.DefaultImportance,
		&allowInference,
		&sensitiveByDefault,
	); err != nil {
		return core.PredicateSchema{}, err
	}

	schema.CanonicalLabel = stringPtr(label)
	if defaultFactType.Valid {
		value := core.FactType(defaultFactType.String)
		schema.DefaultFactType = &value
	}
	if tau.Valid {
		schema.DefaultTauDays = &tau.Float64
	}
	schema.AllowInference = intBool(allowInference)
	schema.SensitiveByDefault = intBool(sensitiveByDefault)
	return schema, nil
}
