package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type EpisodeRepository struct {
	db *sql.DB
}

func NewEpisodeRepository(db *sql.DB) *EpisodeRepository {
	return &EpisodeRepository{db: db}
}

func (r *EpisodeRepository) Append(ctx context.Context, episode core.Episode) error {
	episode = normalizeEpisode(episode)

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if episode.PrevEpisodeID == nil {
		prevID, prevErr := latestEpisodeID(ctx, tx, episode.PersonaID, episode.SessionID)
		if prevErr != nil {
			err = prevErr
			return err
		}
		episode.PrevEpisodeID = prevID
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO episodes (
    id, persona_id, session_id, role, content, content_hash, occurred_at,
    source_type, source_ref, prev_episode_id, next_episode_id,
    visibility_status, sensitivity_level, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		episode.ID,
		episode.PersonaID,
		episode.SessionID,
		string(episode.Role),
		episode.Content,
		episode.ContentHash,
		formatTime(episode.OccurredAt),
		string(episode.SourceType),
		nullableString(episode.SourceRef),
		nullableString(episode.PrevEpisodeID),
		nullableString(episode.NextEpisodeID),
		string(episode.VisibilityStatus),
		string(episode.SensitivityLevel),
		boolInt(episode.Searchable),
	)
	if err != nil {
		return err
	}

	if episode.PrevEpisodeID != nil {
		_, err = tx.ExecContext(ctx, `
UPDATE episodes
SET next_episode_id = ?
WHERE persona_id = ? AND id = ?`,
			episode.ID,
			episode.PersonaID,
			*episode.PrevEpisodeID,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *EpisodeRepository) Get(ctx context.Context, personaID string, episodeID string) (core.Episode, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, persona_id, session_id, role, content, content_hash, occurred_at,
       source_type, source_ref, prev_episode_id, next_episode_id,
       visibility_status, sensitivity_level, searchable
FROM episodes
WHERE persona_id = ? AND id = ?`, personaID, episodeID)
	return scanEpisode(row)
}

func (r *EpisodeRepository) ListExtractionCandidates(ctx context.Context, personaID string, sessionID string) ([]core.Episode, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, persona_id, session_id, role, content, content_hash, occurred_at,
       source_type, source_ref, prev_episode_id, next_episode_id,
       visibility_status, sensitivity_level, searchable
FROM episodes
WHERE persona_id = ?
  AND session_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY occurred_at ASC, ingested_at ASC`, personaID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var episodes []core.Episode
	for rows.Next() {
		episode, err := scanEpisode(rows)
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

func latestEpisodeID(ctx context.Context, tx *sql.Tx, personaID string, sessionID string) (*string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM episodes
WHERE persona_id = ? AND session_id = ? AND next_episode_id IS NULL
ORDER BY occurred_at DESC, ingested_at DESC
LIMIT 1`, personaID, sessionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find previous episode: %w", err)
	}
	return &id, nil
}

type episodeScanner interface {
	Scan(dest ...any) error
}

func scanEpisode(row episodeScanner) (core.Episode, error) {
	var episode core.Episode
	var occurredAt string
	var sourceRef, prevID, nextID sql.NullString
	var searchable int
	if err := row.Scan(
		&episode.ID,
		&episode.PersonaID,
		&episode.SessionID,
		&episode.Role,
		&episode.Content,
		&episode.ContentHash,
		&occurredAt,
		&episode.SourceType,
		&sourceRef,
		&prevID,
		&nextID,
		&episode.VisibilityStatus,
		&episode.SensitivityLevel,
		&searchable,
	); err != nil {
		return core.Episode{}, err
	}
	episode.OccurredAt = parseTime(occurredAt)
	episode.SourceRef = stringPtr(sourceRef)
	episode.PrevEpisodeID = stringPtr(prevID)
	episode.NextEpisodeID = stringPtr(nextID)
	episode.Searchable = intBool(searchable)
	return episode, nil
}

func normalizeEpisode(episode core.Episode) core.Episode {
	if episode.Role == "" {
		episode.Role = core.RoleUser
	}
	if episode.SourceType == "" {
		episode.SourceType = core.SourceTypeChat
	}
	if episode.ContentHash == "" {
		episode.ContentHash = hashContent(episode.Content)
	}
	if episode.VisibilityStatus == "" {
		episode.VisibilityStatus = core.VisibilityVisible
		episode.Searchable = true
	}
	if episode.VisibilityStatus != core.VisibilityVisible {
		episode.Searchable = false
	}
	episode.SensitivityLevel = defaultSensitivity(episode.SensitivityLevel)
	return episode
}
