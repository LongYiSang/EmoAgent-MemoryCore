package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

const (
	MirrorPersonaStateReady      = "ready"
	MirrorPersonaStateRebuilding = "rebuilding"
	MirrorPersonaStateDegraded   = "degraded"
)

type MirrorPersonaStateRepository struct {
	db *sql.DB
}

func NewMirrorPersonaStateRepository(db *sql.DB) *MirrorPersonaStateRepository {
	return &MirrorPersonaStateRepository{db: db}
}

func (r *MirrorPersonaStateRepository) MarkRebuilding(ctx context.Context, personaID string) error {
	return r.mark(ctx, personaID, MirrorPersonaStateRebuilding, "")
}

func (r *MirrorPersonaStateRepository) MarkReady(ctx context.Context, personaID string) error {
	return r.mark(ctx, personaID, MirrorPersonaStateReady, "")
}

func (r *MirrorPersonaStateRepository) MarkDegraded(ctx context.Context, personaID string, reason string) error {
	return r.mark(ctx, personaID, MirrorPersonaStateDegraded, reason)
}

func (r *MirrorPersonaStateRepository) IsReady(ctx context.Context, personaID string) (bool, error) {
	var state string
	err := r.db.QueryRowContext(ctx, `
SELECT state
FROM mirror_persona_state
WHERE persona_id = ?`, personaID).Scan(&state)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return state == MirrorPersonaStateReady, nil
}

func (r *MirrorPersonaStateRepository) mark(ctx context.Context, personaID string, state string, reason string) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO mirror_persona_state (persona_id, state, reason, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(persona_id) DO UPDATE SET
    state = excluded.state,
    reason = excluded.reason,
    updated_at = excluded.updated_at`,
		personaID,
		state,
		nullableMirrorPersonaStateReason(reason),
		formatTime(time.Now().UTC()),
	)
	return err
}

func nullableMirrorPersonaStateReason(reason string) sql.NullString {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: reason, Valid: true}
}
