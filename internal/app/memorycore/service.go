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
	RunNaturalMemoryCycle(ctx context.Context, req RunNaturalMemoryCycleRequest) (*RunNaturalMemoryCycleResult, error)
	RunNaturalMemoryTick(ctx context.Context, req RunNaturalMemoryTickRequest) (*RunNaturalMemoryCycleResult, error)
	ApplyCompression(ctx context.Context, req ApplyCompressionRequest) (*ApplyCompressionResult, error)
	RunCuration(ctx context.Context, req RunCurationRequest) (*RunCurationResult, error)
	Forget(ctx context.Context, req ForgetRequest) (*ForgetResult, error)
	PreviewForget(ctx context.Context, req ForgetPreviewRequest) (*ForgetPreviewResult, error)
	ExecuteForget(ctx context.Context, req ForgetExecuteRequest) (*ForgetExecuteResult, error)
	GetPendingManualForgetOperation(ctx context.Context, req GetPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error)
	CancelPendingManualForgetOperation(ctx context.Context, req CancelPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error)
	VerifyForget(ctx context.Context, req ForgetVerifyRequest) (*ForgetVerifyResult, error)
	RunExtraction(ctx context.Context, req RunExtractionRequest) (*ExtractionRunResult, error)
	RunExtractionBatch(ctx context.Context, req ExtractionBatchRequest) (*ExtractionBatchResult, error)
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
	queryPipeline     queryAnalysisPipeline
	retention         *memsqlite.RetentionRepository
	natural           *memsqlite.NaturalMemoryRepository
	compress          *memsqlite.CompressionRepository
	curation          *memsqlite.CurationRepository
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
	extraction        ExtractionOptions
	semanticOps       SemanticOpsOptions
	naturalOptions    NaturalMemoryOptions
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
	timezone := strings.TrimSpace(opts.Timezone)
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%w: Timezone must be a valid IANA timezone: %v", ErrInvalidOptions, err)
	}
	baseNow := now
	now = func() time.Time {
		return baseNow().In(loc)
	}
	storeOptions := memsqlite.StoreOptions{Timezone: timezone, Now: now}
	resilience := normalizeSidecarResilienceOptions(opts.SidecarResilience)
	extraction := normalizeExtractionOptions(opts.Extraction)
	sqlDB := db.SQLDB()
	retrieve := memsqlite.NewRetrievalRepository(sqlDB, uuid.NewString, now)
	queryPipeline := newQueryAnalysisPipeline(storeRuleQueryAnalyzer{repo: retrieve}, newSemanticQueryAnalyzerFromOptions(opts.QueryAnalysis), opts.QueryAnalysis)
	naturalOptions := normalizeNaturalMemoryOptions(opts.NaturalMemory)
	if strings.TrimSpace(naturalOptions.SleepCycle.Timezone) == "" {
		naturalOptions.SleepCycle.Timezone = timezone
	}
	return &service{
		db:                db,
		sqlDB:             sqlDB,
		store:             memsqlite.NewStoreWithOptions(sqlDB, storeOptions),
		episodes:          memsqlite.NewEpisodeRepositoryWithOptions(sqlDB, storeOptions),
		entities:          memsqlite.NewEntityRepositoryWithOptions(sqlDB, storeOptions),
		facts:             memsqlite.NewConsolidationRepositoryWithOptions(sqlDB, uuid.NewString, now, storeOptions),
		search:            memsqlite.NewSearchRepositoryWithOptions(sqlDB, storeOptions),
		retrieve:          retrieve,
		queryAnalyzer:     queryPipeline,
		queryPipeline:     queryPipeline,
		retention:         memsqlite.NewRetentionRepositoryWithOptions(sqlDB, uuid.NewString, now, storeOptions),
		natural:           memsqlite.NewNaturalMemoryRepositoryWithOptions(sqlDB, uuid.NewString, now, storeOptions),
		compress:          memsqlite.NewCompressionRepositoryWithOptions(sqlDB, uuid.NewString, now, storeOptions),
		curation:          memsqlite.NewCurationRepositoryWithOptions(sqlDB, uuid.NewString, now, storeOptions),
		forget:            memsqlite.NewForgetRepositoryWithOptions(sqlDB, uuid.NewString, now, storeOptions),
		mirrorAdapter:     opts.MirrorAdapter,
		mirrorQueue:       memsqlite.NewMirrorQueueRepositoryWithOptions(sqlDB, storeOptions),
		mirrorPayload:     memsqlite.NewMirrorPayloadRepository(sqlDB),
		mirrorIndex:       memsqlite.NewMirrorIndexRepositoryWithOptions(sqlDB, uuid.NewString, storeOptions),
		mirrorMap:         memsqlite.NewMirrorCandidateRepository(sqlDB),
		mirrorState:       memsqlite.NewMirrorPersonaStateRepositoryWithOptions(sqlDB, storeOptions),
		persona:           defaultString(opts.PersonaID, defaultPersonaID),
		now:               now,
		sidecarResilience: resilience,
		sidecarBreaker:    newSidecarCircuitBreaker(resilience.Breaker, now),
		extraction:        extraction,
		semanticOps:       normalizeSemanticOpsOptions(opts.SemanticOps),
		naturalOptions:    naturalOptions,
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
