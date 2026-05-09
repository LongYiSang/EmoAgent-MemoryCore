package memorycore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const defaultPersonaID = "default"

type Service interface {
	Close() error
	StartSession(ctx context.Context, req StartSessionRequest) (*Session, error)
	EndSession(ctx context.Context, req EndSessionRequest) (*Session, error)
	AppendEpisode(ctx context.Context, req AppendEpisodeRequest) (*Episode, error)
	EnsureEntity(ctx context.Context, req EnsureEntityRequest) (*Entity, error)
	AddEntityAlias(ctx context.Context, req AddEntityAliasRequest) (*EntityAlias, error)
}

type service struct {
	db       *memsqlite.DB
	sqlDB    *sql.DB
	store    *memsqlite.Store
	episodes *memsqlite.EpisodeRepository
	entities *memsqlite.EntityRepository
	persona  string
	now      func() time.Time
}

func Open(ctx context.Context, opts Options) (Service, error) {
	if strings.TrimSpace(opts.DBPath) == "" {
		return nil, fmt.Errorf("%w: DBPath is required", ErrInvalidOptions)
	}

	db, err := memsqlite.Open(ctx, opts.DBPath)
	if err != nil {
		return nil, err
	}
	if opts.AutoMigrate {
		if err := db.MigrateWithOptions(ctx, memsqlite.MigrateOptions{EnableFTS: opts.EnableFTS}); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sqlDB := db.SQLDB()
	return &service{
		db:       db,
		sqlDB:    sqlDB,
		store:    memsqlite.NewStore(sqlDB),
		episodes: memsqlite.NewEpisodeRepository(sqlDB),
		entities: memsqlite.NewEntityRepository(sqlDB),
		persona:  defaultString(opts.PersonaID, defaultPersonaID),
		now:      now,
	}, nil
}

func (s *service) Close() error {
	if s == nil {
		return nil
	}
	return s.db.Close()
}

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

func (s *service) ensurePersona(ctx context.Context, personaID string) error {
	return s.store.EnsurePersona(ctx, core.Persona{
		ID:          personaID,
		DisplayName: displayNameForPersona(personaID),
	})
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func displayNameForPersona(personaID string) string {
	if personaID == defaultPersonaID {
		return "Default"
	}
	return personaID
}

func sessionFromCore(session core.Session) *Session {
	return &Session{
		ID:        session.ID,
		PersonaID: session.PersonaID,
		Channel:   string(session.Channel),
		Title:     session.Title,
		Summary:   session.Summary,
		StartedAt: session.StartedAt,
		EndedAt:   session.EndedAt,
	}
}

func episodeFromCore(episode core.Episode) *Episode {
	return &Episode{
		ID:               episode.ID,
		PersonaID:        episode.PersonaID,
		SessionID:        episode.SessionID,
		Role:             string(episode.Role),
		Content:          episode.Content,
		ContentHash:      episode.ContentHash,
		OccurredAt:       episode.OccurredAt,
		SourceType:       string(episode.SourceType),
		SourceRef:        episode.SourceRef,
		PrevEpisodeID:    episode.PrevEpisodeID,
		NextEpisodeID:    episode.NextEpisodeID,
		VisibilityStatus: string(episode.VisibilityStatus),
		SensitivityLevel: string(episode.SensitivityLevel),
		Searchable:       episode.Searchable,
	}
}

func entityFromCore(entity core.Entity, aliases []core.EntityAlias) *Entity {
	result := &Entity{
		ID:               entity.ID,
		PersonaID:        entity.PersonaID,
		CanonicalName:    entity.CanonicalName,
		EntityType:       string(entity.EntityType),
		Description:      entity.Description,
		VisibilityStatus: string(entity.VisibilityStatus),
		SensitivityLevel: string(entity.SensitivityLevel),
		Searchable:       entity.Searchable,
		Aliases:          make([]EntityAlias, 0, len(aliases)),
	}
	for _, alias := range aliases {
		result.Aliases = append(result.Aliases, *entityAliasFromCore(alias))
	}
	return result
}

func entityAliasFromCore(alias core.EntityAlias) *EntityAlias {
	return &EntityAlias{
		ID:              alias.ID,
		PersonaID:       alias.PersonaID,
		EntityID:        alias.EntityID,
		Alias:           alias.Alias,
		AliasType:       string(alias.AliasType),
		Confidence:      alias.Confidence,
		SourceEpisodeID: alias.SourceEpisodeID,
	}
}
