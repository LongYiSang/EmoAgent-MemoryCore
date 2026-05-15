package memorycore

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/internal/core"
	"strings"
)

func (s *service) AppendEpisode(ctx context.Context, req AppendEpisodeRequest) (*Episode, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("%w: SessionID is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Content) == "" {
		return nil, fmt.Errorf("%w: Content is required", ErrInvalidRequest)
	}

	personaID := defaultString(req.PersonaID, s.persona)
	if err := s.requireSession(ctx, personaID, req.SessionID); err != nil {
		return nil, err
	}

	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = s.now()
	}
	searchable := true
	if req.Searchable != nil {
		searchable = *req.Searchable
	}

	episode := core.Episode{
		ID:               defaultString(req.ID, uuid.NewString()),
		PersonaID:        personaID,
		SessionID:        req.SessionID,
		Role:             core.Role(defaultString(req.Role, RoleUser)),
		Content:          req.Content,
		OccurredAt:       occurredAt,
		SourceType:       core.SourceType(defaultString(req.SourceType, SourceTypeChat)),
		SourceRef:        req.SourceRef,
		VisibilityStatus: core.VisibilityStatus(defaultString(req.VisibilityStatus, VisibilityVisible)),
		SensitivityLevel: core.SensitivityLevel(defaultString(req.SensitivityLevel, SensitivityNormal)),
		Searchable:       searchable,
	}
	if err := s.episodes.Append(ctx, episode); err != nil {
		return nil, err
	}

	stored, err := s.episodes.Get(ctx, personaID, episode.ID)
	if err != nil {
		return nil, err
	}
	return episodeFromCore(stored), nil
}
