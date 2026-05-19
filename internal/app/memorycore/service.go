package memorycore

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
	"strings"
	"time"
)

const defaultPersonaID = "default"

type Service interface {
	Close() error
	StartSession(ctx context.Context, req StartSessionRequest) (*Session, error)
	EndSession(ctx context.Context, req EndSessionRequest) (*Session, error)
	AppendEpisode(ctx context.Context, req AppendEpisodeRequest) (*Episode, error)
	EnsureEntity(ctx context.Context, req EnsureEntityRequest) (*Entity, error)
	AddEntityAlias(ctx context.Context, req AddEntityAliasRequest) (*EntityAlias, error)
	ConsolidateCandidate(ctx context.Context, req ConsolidateCandidateRequest) (*ConsolidationResult, error)
	Retrieve(ctx context.Context, req RetrievalRequest) (*MemoryContext, error)
	RebuildSearchDocuments(ctx context.Context, req RebuildSearchDocumentsRequest) (*RebuildSearchDocumentsResult, error)
	RunRetention(ctx context.Context, req RunRetentionRequest) (*RunRetentionResult, error)
	RunRetentionJobs(ctx context.Context, req RunRetentionJobsRequest) (*RunRetentionJobsResult, error)
	ApplyCompression(ctx context.Context, req ApplyCompressionRequest) (*ApplyCompressionResult, error)
	Forget(ctx context.Context, req ForgetRequest) (*ForgetResult, error)
	RunMirrorSync(ctx context.Context, req RunMirrorSyncRequest) (*RunMirrorSyncResult, error)
	RebuildMirror(ctx context.Context, req RebuildMirrorRequest) (*RebuildMirrorResult, error)
}

type service struct {
	db                *memsqlite.DB
	sqlDB             *sql.DB
	store             *memsqlite.Store
	episodes          *memsqlite.EpisodeRepository
	entities          *memsqlite.EntityRepository
	facts             *memsqlite.ConsolidationRepository
	search            *memsqlite.SearchRepository
	retrieve          *memsqlite.RetrievalRepository
	queryAnalyzer     QueryAnalyzer
	retention         *memsqlite.RetentionRepository
	compress          *memsqlite.CompressionRepository
	forget            *memsqlite.ForgetRepository
	mirrorAdapter     MirrorAdapter
	mirrorQueue       *memsqlite.MirrorQueueRepository
	mirrorPayload     *memsqlite.MirrorPayloadRepository
	mirrorIndex       *memsqlite.MirrorIndexRepository
	mirrorMap         *memsqlite.MirrorCandidateRepository
	mirrorState       *memsqlite.MirrorPersonaStateRepository
	persona           string
	now               func() time.Time
	sidecarResilience SidecarResilienceOptions
	sidecarBreaker    *sidecarCircuitBreaker
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
	resilience := normalizeSidecarResilienceOptions(opts.SidecarResilience)
	sqlDB := db.SQLDB()
	retrieve := memsqlite.NewRetrievalRepository(sqlDB, uuid.NewString, now)
	return &service{
		db:                db,
		sqlDB:             sqlDB,
		store:             memsqlite.NewStore(sqlDB),
		episodes:          memsqlite.NewEpisodeRepository(sqlDB),
		entities:          memsqlite.NewEntityRepository(sqlDB),
		facts:             memsqlite.NewConsolidationRepository(sqlDB, uuid.NewString, now),
		search:            memsqlite.NewSearchRepository(sqlDB),
		retrieve:          retrieve,
		queryAnalyzer:     newQueryAnalysisPipeline(storeRuleQueryAnalyzer{repo: retrieve}, newSemanticQueryAnalyzerFromOptions(opts.QueryAnalysis), opts.QueryAnalysis),
		retention:         memsqlite.NewRetentionRepository(sqlDB, uuid.NewString, now),
		compress:          memsqlite.NewCompressionRepository(sqlDB, uuid.NewString, now),
		forget:            memsqlite.NewForgetRepository(sqlDB, uuid.NewString, now),
		mirrorAdapter:     opts.MirrorAdapter,
		mirrorQueue:       memsqlite.NewMirrorQueueRepository(sqlDB),
		mirrorPayload:     memsqlite.NewMirrorPayloadRepository(sqlDB),
		mirrorIndex:       memsqlite.NewMirrorIndexRepository(sqlDB, uuid.NewString),
		mirrorMap:         memsqlite.NewMirrorCandidateRepository(sqlDB),
		mirrorState:       memsqlite.NewMirrorPersonaStateRepository(sqlDB),
		persona:           defaultString(opts.PersonaID, defaultPersonaID),
		now:               now,
		sidecarResilience: resilience,
		sidecarBreaker:    newSidecarCircuitBreaker(resilience.Breaker, now),
	}, nil
}

func (s *service) Close() error {
	if s == nil {
		return nil
	}
	return s.db.Close()
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
