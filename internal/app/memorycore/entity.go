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

func (s *service) EnsureEntity(ctx context.Context, req EnsureEntityRequest) (*Entity, error) {
	if strings.TrimSpace(req.CanonicalName) == "" {
		return nil, fmt.Errorf("%w: CanonicalName is required", ErrInvalidRequest)
	}

	personaID := defaultString(req.PersonaID, s.persona)
	if err := s.ensurePersona(ctx, personaID); err != nil {
		return nil, err
	}
	searchable := true
	if req.Searchable != nil {
		searchable = *req.Searchable
	}
	entity, err := s.entities.EnsureByCanonical(ctx, core.Entity{
		ID:               defaultString(req.ID, uuid.NewString()),
		PersonaID:        personaID,
		CanonicalName:    req.CanonicalName,
		EntityType:       core.EntityType(defaultString(req.EntityType, EntityTypeConcept)),
		Description:      req.Description,
		VisibilityStatus: core.VisibilityStatus(defaultString(req.VisibilityStatus, VisibilityVisible)),
		SensitivityLevel: core.SensitivityLevel(defaultString(req.SensitivityLevel, SensitivityNormal)),
		Searchable:       searchable,
	})
	if err != nil {
		return nil, err
	}

	for _, alias := range req.Aliases {
		if strings.TrimSpace(alias.Alias) == "" {
			return nil, fmt.Errorf("%w: alias is required", ErrInvalidRequest)
		}
		if _, err := s.entities.EnsureAlias(ctx, core.EntityAlias{
			ID:              defaultString(alias.ID, uuid.NewString()),
			PersonaID:       personaID,
			EntityID:        entity.ID,
			Alias:           alias.Alias,
			AliasType:       core.AliasType(defaultString(alias.AliasType, AliasTypeSurface)),
			Confidence:      alias.Confidence,
			SourceEpisodeID: alias.SourceEpisodeID,
		}); err != nil {
			return nil, err
		}
	}
	aliases, err := s.entities.ListAliases(ctx, personaID, entity.ID)
	if err != nil {
		return nil, err
	}
	return entityFromCore(entity, aliases), nil
}

func (s *service) AddEntityAlias(ctx context.Context, req AddEntityAliasRequest) (*EntityAlias, error) {
	if strings.TrimSpace(req.EntityID) == "" {
		return nil, fmt.Errorf("%w: EntityID is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.Alias) == "" {
		return nil, fmt.Errorf("%w: Alias is required", ErrInvalidRequest)
	}

	personaID := defaultString(req.PersonaID, s.persona)
	if _, err := s.entities.Get(ctx, personaID, req.EntityID); errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: entity %s", ErrNotFound, req.EntityID)
	} else if err != nil {
		return nil, err
	}
	alias, err := s.entities.EnsureAlias(ctx, core.EntityAlias{
		ID:              defaultString(req.ID, uuid.NewString()),
		PersonaID:       personaID,
		EntityID:        req.EntityID,
		Alias:           req.Alias,
		AliasType:       core.AliasType(defaultString(req.AliasType, AliasTypeSurface)),
		Confidence:      req.Confidence,
		SourceEpisodeID: req.SourceEpisodeID,
	})
	if err != nil {
		return nil, err
	}
	return entityAliasFromCore(alias), nil
}
