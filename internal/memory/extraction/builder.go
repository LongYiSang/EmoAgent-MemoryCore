package extraction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

const defaultPersonaID = "default"

type BuildRequestOptions struct {
	PersonaID                string
	SessionID                *string
	EpisodeIDs               []string
	Trigger                  string
	Limit                    int
	Since                    *time.Time
	Until                    *time.Time
	Timezone                 string
	AllowSensitiveExtraction bool
	AllowInference           bool
	ManualPin                bool
	ManualForget             bool
	MaxFacts                 int
	MaxLinks                 int
	Now                      time.Time
}

func BuildRequest(ctx context.Context, db *sql.DB, opts BuildRequestOptions) (memorycore.ExtractionRequest, error) {
	if db == nil {
		return memorycore.ExtractionRequest{}, fmt.Errorf("db is required")
	}
	personaID := defaultString(opts.PersonaID, defaultPersonaID)
	trigger := defaultString(opts.Trigger, memorycore.ExtractionTriggerSessionEnd)
	if !validExtractionTrigger(trigger) {
		return memorycore.ExtractionRequest{}, fmt.Errorf("trigger must be one of idle_detect|session_end|manual_pin|manual_forget|work_candidate|reprocess")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	timezone := defaultString(opts.Timezone, "Asia/Singapore")
	maxFacts := opts.MaxFacts
	if maxFacts == 0 {
		maxFacts = 12
	}
	maxLinks := opts.MaxLinks
	if maxLinks == 0 {
		maxLinks = 20
	}

	episodes, err := loadRequestEpisodes(ctx, db, personaID, opts)
	if err != nil {
		return memorycore.ExtractionRequest{}, err
	}
	if len(episodes) == 0 {
		return memorycore.ExtractionRequest{}, fmt.Errorf("no visible/searchable episodes")
	}
	entities, err := loadKnownEntities(ctx, db, personaID)
	if err != nil {
		return memorycore.ExtractionRequest{}, err
	}
	predicates, err := loadPredicateSchemas(ctx, db)
	if err != nil {
		return memorycore.ExtractionRequest{}, err
	}

	return memorycore.ExtractionRequest{
		SchemaVersion:          memorycore.ExtractionRequestSchemaVersion,
		RequestID:              uuid.NewString(),
		PersonaID:              personaID,
		SessionID:              opts.SessionID,
		Trigger:                trigger,
		Now:                    now,
		Timezone:               timezone,
		Episodes:               episodes,
		ApprovedWorkCandidates: []memorycore.ExtractionWorkCandidate{},
		KnownEntities:          entities,
		PredicateSchemas:       predicates,
		Policy: memorycore.ExtractionPolicy{
			AllowSensitiveExtraction: opts.AllowSensitiveExtraction,
			AllowInference:           opts.AllowInference,
			ManualPin:                opts.ManualPin || trigger == memorycore.ExtractionTriggerManualPin,
			ManualForget:             opts.ManualForget || trigger == memorycore.ExtractionTriggerManualForget,
			MaxFacts:                 maxFacts,
			MaxLinks:                 maxLinks,
		},
	}, nil
}

func loadRequestEpisodes(ctx context.Context, db *sql.DB, personaID string, opts BuildRequestOptions) ([]memorycore.ExtractionEpisode, error) {
	if len(opts.EpisodeIDs) > 0 {
		episodes := make([]memorycore.ExtractionEpisode, 0, len(opts.EpisodeIDs))
		for _, id := range opts.EpisodeIDs {
			episode, err := loadVisibleSearchableEpisode(ctx, db, personaID, id)
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("episode %s is missing or ineligible", id)
			}
			if err != nil {
				return nil, err
			}
			episodes = append(episodes, episode)
		}
		return episodes, nil
	}

	limit := opts.Limit
	if limit == 0 {
		limit = 50
	}
	query := `
SELECT id, role, content, occurred_at, source_type, prev_episode_id, next_episode_id,
       visibility_status, sensitivity_level
FROM episodes
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`
	args := []any{personaID}
	if opts.SessionID != nil && strings.TrimSpace(*opts.SessionID) != "" {
		query += ` AND session_id = ?`
		args = append(args, strings.TrimSpace(*opts.SessionID))
	}
	if opts.Since != nil && !opts.Since.IsZero() {
		query += ` AND occurred_at >= ?`
		args = append(args, formatTime(*opts.Since))
	}
	if opts.Until != nil && !opts.Until.IsZero() {
		query += ` AND occurred_at <= ?`
		args = append(args, formatTime(*opts.Until))
	}
	query += ` ORDER BY occurred_at ASC, ingested_at ASC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExtractionEpisodes(rows)
}

func loadVisibleSearchableEpisode(ctx context.Context, db *sql.DB, personaID string, episodeID string) (memorycore.ExtractionEpisode, error) {
	row := db.QueryRowContext(ctx, `
SELECT id, role, content, occurred_at, source_type, prev_episode_id, next_episode_id,
       visibility_status, sensitivity_level
FROM episodes
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`, personaID, episodeID)
	return scanExtractionEpisode(row)
}

func scanExtractionEpisodes(rows *sql.Rows) ([]memorycore.ExtractionEpisode, error) {
	var episodes []memorycore.ExtractionEpisode
	for rows.Next() {
		episode, err := scanExtractionEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return episodes, nil
}

type extractionEpisodeScanner interface {
	Scan(dest ...any) error
}

func scanExtractionEpisode(row extractionEpisodeScanner) (memorycore.ExtractionEpisode, error) {
	var episode memorycore.ExtractionEpisode
	var occurredAt string
	var prevID, nextID sql.NullString
	if err := row.Scan(
		&episode.EpisodeID,
		&episode.Role,
		&episode.Content,
		&occurredAt,
		&episode.SourceType,
		&prevID,
		&nextID,
		&episode.VisibilityStatus,
		&episode.SensitivityLevel,
	); err != nil {
		return memorycore.ExtractionEpisode{}, err
	}
	episode.OccurredAt = parseTime(occurredAt)
	episode.PrevEpisodeID = stringPtr(prevID)
	episode.NextEpisodeID = stringPtr(nextID)
	return episode, nil
}

func loadKnownEntities(ctx context.Context, db *sql.DB, personaID string) ([]memorycore.ExtractionKnownEntity, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, canonical_name, entity_type, description, visibility_status, sensitivity_level
FROM entities
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY entity_type, canonical_name`, personaID)
	if err != nil {
		return nil, err
	}

	var entities []memorycore.ExtractionKnownEntity
	for rows.Next() {
		var entity memorycore.ExtractionKnownEntity
		var description sql.NullString
		if err := rows.Scan(
			&entity.EntityID,
			&entity.CanonicalName,
			&entity.EntityType,
			&description,
			&entity.VisibilityStatus,
			&entity.SensitivityLevel,
		); err != nil {
			return nil, err
		}
		entity.Description = stringPtr(description)
		entities = append(entities, entity)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for i := range entities {
		aliases, err := loadEntityAliases(ctx, db, personaID, entities[i].EntityID)
		if err != nil {
			return nil, err
		}
		entities[i].Aliases = aliases
	}
	return entities, nil
}

func loadEntityAliases(ctx context.Context, db *sql.DB, personaID string, entityID string) ([]memorycore.ExtractionEntityAlias, error) {
	rows, err := db.QueryContext(ctx, `
SELECT alias, alias_type, confidence, source_episode_id
FROM entity_aliases
WHERE persona_id = ? AND entity_id = ?
ORDER BY created_at ASC`, personaID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []memorycore.ExtractionEntityAlias
	for rows.Next() {
		var alias memorycore.ExtractionEntityAlias
		var sourceID sql.NullString
		if err := rows.Scan(&alias.Alias, &alias.AliasType, &alias.Confidence, &sourceID); err != nil {
			return nil, err
		}
		alias.SourceEpisodeID = stringPtr(sourceID)
		aliases = append(aliases, alias)
	}
	return aliases, rows.Err()
}

func loadPredicateSchemas(ctx context.Context, db *sql.DB) ([]memorycore.ExtractionPredicateSchema, error) {
	rows, err := db.QueryContext(ctx, `
SELECT predicate, canonical_label, default_fact_type, cardinality, conflict_policy,
       temporal_behavior, object_kind, default_tau_days, default_importance,
       allow_inference, sensitive_by_default
FROM predicate_schemas
ORDER BY predicate ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []memorycore.ExtractionPredicateSchema
	for rows.Next() {
		var schema memorycore.ExtractionPredicateSchema
		var label, defaultFactType sql.NullString
		var tau sql.NullFloat64
		var allowInference, sensitiveByDefault int
		if err := rows.Scan(
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
			return nil, err
		}
		schema.CanonicalLabel = stringPtr(label)
		schema.DefaultFactType = stringPtr(defaultFactType)
		if tau.Valid {
			schema.DefaultTauDays = &tau.Float64
		}
		schema.AllowInference = allowInference != 0
		schema.SensitiveByDefault = sensitiveByDefault != 0
		schemas = append(schemas, schema)
	}
	return schemas, rows.Err()
}

func validExtractionTrigger(trigger string) bool {
	switch trigger {
	case memorycore.ExtractionTriggerIdleDetect,
		memorycore.ExtractionTriggerSessionEnd,
		memorycore.ExtractionTriggerManualPin,
		memorycore.ExtractionTriggerManualForget,
		memorycore.ExtractionTriggerWorkCandidate,
		memorycore.ExtractionTriggerReprocess:
		return true
	default:
		return false
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed
	}
	parsed, _ = time.Parse(time.RFC3339, value)
	return parsed
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
