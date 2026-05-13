package sqlite

import (
	"context"
	"database/sql"
	"errors"

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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	_, err = tx.ExecContext(ctx, `
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
	if err != nil {
		return err
	}
	if entity.VisibilityStatus == core.VisibilityVisible && entity.Searchable {
		if err = enqueueEntityUpsertTx(ctx, tx, entity.PersonaID, entity.ID); err != nil {
			return err
		}
	} else {
		if err = enqueueEntityDeleteTx(ctx, tx, entity.PersonaID, entity.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *EntityRepository) EnsureByCanonical(ctx context.Context, entity core.Entity) (core.Entity, error) {
	entity = normalizeEntity(entity)
	existing, err := r.GetByCanonical(ctx, entity.PersonaID, entity.CanonicalName, entity.EntityType)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return core.Entity{}, err
	}
	if err := r.Upsert(ctx, entity); err != nil {
		return core.Entity{}, err
	}
	return r.GetByCanonical(ctx, entity.PersonaID, entity.CanonicalName, entity.EntityType)
}

func (r *EntityRepository) Get(ctx context.Context, personaID string, entityID string) (core.Entity, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, persona_id, canonical_name, entity_type, description,
       visibility_status, sensitivity_level, searchable
FROM entities
WHERE persona_id = ? AND id = ?`, personaID, entityID)
	return scanEntity(row)
}

func (r *EntityRepository) GetByCanonical(ctx context.Context, personaID string, canonicalName string, entityType core.EntityType) (core.Entity, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, persona_id, canonical_name, entity_type, description,
       visibility_status, sensitivity_level, searchable
FROM entities
WHERE persona_id = ?
  AND canonical_name = ?
  AND entity_type = ?
  AND visibility_status = 'visible'
ORDER BY created_at ASC
LIMIT 1`, personaID, canonicalName, string(entityType))
	return scanEntity(row)
}

func (r *EntityRepository) AddAlias(ctx context.Context, alias core.EntityAlias) error {
	_, err := r.EnsureAlias(ctx, alias)
	return err
}

func (r *EntityRepository) EnsureAlias(ctx context.Context, alias core.EntityAlias) (core.EntityAlias, error) {
	if alias.AliasType == "" {
		alias.AliasType = core.AliasTypeSurface
	}
	if alias.Confidence == 0 {
		alias.Confidence = 1.0
	}
	existing, err := r.GetAlias(ctx, alias.PersonaID, alias.EntityID, alias.Alias, alias.AliasType)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return core.EntityAlias{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return core.EntityAlias{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	result, err := tx.ExecContext(ctx, `
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
	if err != nil {
		return core.EntityAlias{}, err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil {
		return core.EntityAlias{}, rowsErr
	} else if rows > 0 {
		mirrorable, mirrorErr := entityMirrorEligibleTx(ctx, tx, alias.PersonaID, alias.EntityID)
		if mirrorErr != nil {
			return core.EntityAlias{}, mirrorErr
		}
		if mirrorable {
			if err = enqueueEntityUpsertTx(ctx, tx, alias.PersonaID, alias.EntityID); err != nil {
				return core.EntityAlias{}, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return core.EntityAlias{}, err
	}
	return r.GetAlias(ctx, alias.PersonaID, alias.EntityID, alias.Alias, alias.AliasType)
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

func (r *EntityRepository) GetAlias(ctx context.Context, personaID string, entityID string, alias string, aliasType core.AliasType) (core.EntityAlias, error) {
	if aliasType == "" {
		aliasType = core.AliasTypeSurface
	}
	row := r.db.QueryRowContext(ctx, `
SELECT id, persona_id, entity_id, alias, alias_type, confidence, source_episode_id
FROM entity_aliases
WHERE persona_id = ?
  AND entity_id = ?
  AND alias = ?
  AND alias_type = ?
ORDER BY created_at ASC
LIMIT 1`, personaID, entityID, alias, string(aliasType))
	return scanEntityAlias(row)
}

func (r *EntityRepository) ListAliases(ctx context.Context, personaID string, entityID string) ([]core.EntityAlias, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, persona_id, entity_id, alias, alias_type, confidence, source_episode_id
FROM entity_aliases
WHERE persona_id = ? AND entity_id = ?
ORDER BY created_at ASC`, personaID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []core.EntityAlias
	for rows.Next() {
		alias, err := scanEntityAlias(rows)
		if err != nil {
			return nil, err
		}
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return aliases, nil
}

type entityScanner interface {
	Scan(dest ...any) error
}

type entityAliasScanner interface {
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

func scanEntityAlias(row entityAliasScanner) (core.EntityAlias, error) {
	var alias core.EntityAlias
	var sourceEpisodeID sql.NullString
	if err := row.Scan(
		&alias.ID,
		&alias.PersonaID,
		&alias.EntityID,
		&alias.Alias,
		&alias.AliasType,
		&alias.Confidence,
		&sourceEpisodeID,
	); err != nil {
		return core.EntityAlias{}, err
	}
	alias.SourceEpisodeID = stringPtr(sourceEpisodeID)
	return alias, nil
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

func entityMirrorEligibleTx(ctx context.Context, tx *sql.Tx, personaID string, entityID string) (bool, error) {
	var visibility string
	var searchable int
	err := tx.QueryRowContext(ctx, `
SELECT visibility_status, searchable
FROM entities
WHERE persona_id = ? AND id = ?`, personaID, entityID).Scan(&visibility, &searchable)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return visibility == string(core.VisibilityVisible) && searchable == 1, nil
}

func enqueueEntityUpsertTx(ctx context.Context, tx *sql.Tx, personaID string, entityID string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (lower(hex(randomblob(16))), ?, 'entity', ?, 'upsert_node')`,
		personaID,
		entityID,
	)
	return err
}

func enqueueEntityDeleteTx(ctx context.Context, tx *sql.Tx, personaID string, entityID string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (lower(hex(randomblob(16))), ?, 'entity', ?, 'delete_node')`,
		personaID,
		entityID,
	)
	return err
}
