package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type Store struct {
	db   *sql.DB
	time timeFormatter
}

func NewStore(db *sql.DB) *Store {
	return NewStoreWithOptions(db, StoreOptions{})
}

func NewStoreWithOptions(db *sql.DB, opts StoreOptions) *Store {
	return &Store{db: db, time: newTimeFormatter(opts)}
}

func (s *Store) EnsurePersona(ctx context.Context, persona core.Persona) error {
	now := s.time.nowText()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO personas(id, display_name, description, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    display_name = excluded.display_name,
    description = excluded.description,
    updated_at = excluded.updated_at`,
		persona.ID,
		persona.DisplayName,
		nullableString(persona.Description),
		now,
		now,
	)
	return err
}

func (s *Store) EnsureSession(ctx context.Context, session core.Session) error {
	channel := session.Channel
	if channel == "" {
		channel = core.ChannelCLI
	}

	now := s.time.nowText()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sessions(id, persona_id, channel, title, summary, started_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    channel = excluded.channel,
    title = excluded.title,
    summary = COALESCE(excluded.summary, sessions.summary),
    updated_at = excluded.updated_at`,
		session.ID,
		session.PersonaID,
		string(channel),
		nullableString(session.Title),
		nullableString(session.Summary),
		s.time.formatTime(session.StartedAt),
		now,
		now,
	)
	return err
}

func (s *Store) GetSession(ctx context.Context, personaID string, sessionID string) (core.Session, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, persona_id, channel, title, summary, started_at, ended_at
FROM sessions
WHERE persona_id = ? AND id = ?`, personaID, sessionID)
	return scanSession(row)
}

func (s *Store) EndSession(ctx context.Context, session core.Session) (core.Session, error) {
	if session.EndedAt == nil {
		return core.Session{}, fmt.Errorf("ended_at is required")
	}

	result, err := s.db.ExecContext(ctx, `
UPDATE sessions
SET ended_at = ?,
    summary = COALESCE(?, summary),
    updated_at = ?
WHERE persona_id = ? AND id = ?`,
		s.time.nullableTime(session.EndedAt),
		nullableString(session.Summary),
		s.time.nowText(),
		session.PersonaID,
		session.ID,
	)
	if err != nil {
		return core.Session{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return core.Session{}, err
	}
	if affected == 0 {
		return core.Session{}, sql.ErrNoRows
	}
	return s.GetSession(ctx, session.PersonaID, session.ID)
}

type sessionScanner interface {
	Scan(dest ...any) error
}

func scanSession(row sessionScanner) (core.Session, error) {
	var session core.Session
	var title, summary, endedAt sql.NullString
	var startedAt string
	if err := row.Scan(
		&session.ID,
		&session.PersonaID,
		&session.Channel,
		&title,
		&summary,
		&startedAt,
		&endedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return core.Session{}, sql.ErrNoRows
		}
		return core.Session{}, err
	}
	session.Title = stringPtr(title)
	session.Summary = stringPtr(summary)
	session.StartedAt = parseTime(startedAt)
	session.EndedAt = timePtr(endedAt)
	return session, nil
}
