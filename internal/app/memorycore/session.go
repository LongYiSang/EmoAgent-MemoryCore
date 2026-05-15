package memorycore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/internal/core"
	"strings"
)

func (s *service) StartSession(ctx context.Context, req StartSessionRequest) (*Session, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	sessionID := defaultString(req.ID, uuid.NewString())
	channel := defaultString(req.Channel, ChannelAPI)
	startedAt := req.StartedAt
	if startedAt.IsZero() {
		startedAt = s.now()
	}

	if err := s.store.EnsurePersona(ctx, core.Persona{
		ID:          personaID,
		DisplayName: displayNameForPersona(personaID),
	}); err != nil {
		return nil, err
	}

	session := core.Session{
		ID:        sessionID,
		PersonaID: personaID,
		Channel:   core.Channel(channel),
		Title:     req.Title,
		StartedAt: startedAt,
	}
	if err := s.store.EnsureSession(ctx, session); err != nil {
		return nil, err
	}
	stored, err := s.store.GetSession(ctx, personaID, sessionID)
	if err != nil {
		return nil, err
	}
	return sessionFromCore(stored), nil
}

func (s *service) EndSession(ctx context.Context, req EndSessionRequest) (*Session, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("%w: SessionID is required", ErrInvalidRequest)
	}

	personaID := defaultString(req.PersonaID, s.persona)
	endedAt := req.EndedAt
	if endedAt.IsZero() {
		endedAt = s.now()
	}
	ended, err := s.store.EndSession(ctx, core.Session{
		ID:        req.SessionID,
		PersonaID: personaID,
		EndedAt:   &endedAt,
		Summary:   req.Summary,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: session %s", ErrNotFound, req.SessionID)
	}
	if err != nil {
		return nil, err
	}
	return sessionFromCore(ended), nil
}

func (s *service) requireSession(ctx context.Context, personaID string, sessionID string) error {
	var id string
	err := s.sqlDB.QueryRowContext(ctx, `
SELECT id
FROM sessions
WHERE persona_id = ? AND id = ?`, personaID, sessionID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: session %s", ErrNotFound, sessionID)
	}
	return err
}
