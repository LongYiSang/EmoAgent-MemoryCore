package sqlite

import (
	"context"
	"database/sql"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type EntityRepository struct {
	db *sql.DB
}

func NewEntityRepository(db *sql.DB) *EntityRepository {
	return &EntityRepository{db: db}
}

func (r *EntityRepository) Upsert(ctx context.Context, entity core.Entity) error {
	entity = normalizeEntity(entity)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO entities (
    id, persona_id, canonical_name, entity_type, description,
    visibility_status, sensitivity_level, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    canonical_name = excluded.canonical_name,
    entity_type = excluded.entity_type,
    description = excluded.description,
    visibility_status = excluded.visibility_status,
    sensitivity_level = excluded.sensitivity_level,
    searchable = excluded.searchable,
    updated_at = CURRENT_TIMESTAMP`,
		entity.ID,
		entity.PersonaID,
		entity.CanonicalName,
		string(entity.EntityType),
		nullableString(entity.Description),
		string(entity.VisibilityStatus),
		string(entity.SensitivityLevel),
		boolInt(entity.Searchable),
	)
	return err
}

func (r *EntityRepository) AddAlias(ctx context.Context, alias core.EntityAlias) error {
	if alias.AliasType == "" {
		alias.AliasType = core.AliasTypeSurface
	}
	if alias.Confidence == 0 {
		alias.Confidence = 1.0
	}
	_, err := r.db.ExecContext(ctx, `
INSERT OR IGNORE INTO entity_aliases (
    id, persona_id, entity_id, alias, alias_type, confidence, source_episode_id
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		alias.ID,
		alias.PersonaID,
		alias.EntityID,
		alias.Alias,
		string(alias.AliasType),
		alias.Confidence,
		nullableString(alias.SourceEpisodeID),
	)
	return err
}

func (r *EntityRepository) ResolveByAlias(ctx context.Context, personaID string, alias string) (core.Entity, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT e.id, e.persona_id, e.canonical_name, e.entity_type, e.description,
       e.visibility_status, e.sensitivity_level, e.searchable
FROM entity_aliases a
JOIN entities e ON e.id = a.entity_id AND e.persona_id = a.persona_id
WHERE a.persona_id = ?
  AND a.alias = ?
  AND e.visibility_status = 'visible'
  AND e.searchable = 1
ORDER BY a.confidence DESC, a.created_at DESC
LIMIT 1`, personaID, alias)
	return scanEntity(row)
}

type entityScanner interface {
	Scan(dest ...any) error
}

func scanEntity(row entityScanner) (core.Entity, error) {
	var entity core.Entity
	var description sql.NullString
	var searchable int
	if err := row.Scan(
		&entity.ID,
		&entity.PersonaID,
		&entity.CanonicalName,
		&entity.EntityType,
		&description,
		&entity.VisibilityStatus,
		&entity.SensitivityLevel,
		&searchable,
	); err != nil {
		return core.Entity{}, err
	}
	entity.Description = stringPtr(description)
	entity.Searchable = intBool(searchable)
	return entity, nil
}

func normalizeEntity(entity core.Entity) core.Entity {
	if entity.EntityType == "" {
		entity.EntityType = core.EntityTypeConcept
	}
	if entity.VisibilityStatus == "" {
		entity.VisibilityStatus = core.VisibilityVisible
		entity.Searchable = true
	}
	entity.SensitivityLevel = defaultSensitivity(entity.SensitivityLevel)
	return entity
}
