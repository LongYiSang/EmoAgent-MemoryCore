package sqlite

import (
	"context"
	"database/sql"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) EnsurePersona(ctx context.Context, persona core.Persona) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO personas(id, display_name, description)
VALUES (?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    display_name = excluded.display_name,
    description = excluded.description,
    updated_at = CURRENT_TIMESTAMP`,
		persona.ID,
		persona.DisplayName,
		nullableString(persona.Description),
	)
	return err
}

func (s *Store) EnsureSession(ctx context.Context, session core.Session) error {
	channel := session.Channel
	if channel == "" {
		channel = core.ChannelCLI
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions(id, persona_id, channel, title, started_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    channel = excluded.channel,
    title = excluded.title,
    updated_at = CURRENT_TIMESTAMP`,
		session.ID,
		session.PersonaID,
		string(channel),
		nullableString(session.Title),
		formatTime(session.StartedAt),
	)
	return err
}
