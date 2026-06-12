package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const (
	ObservabilityStatusOK       = "ok"
	ObservabilityStatusDegraded = "degraded"
)

type ObservabilitySnapshotRequest struct {
	PersonaID        string    `json:"persona_id,omitempty"`
	Since            time.Time `json:"since,omitempty"`
	IncludeDebug     bool      `json:"include_debug,omitempty"`
	MirrorConfigured bool      `json:"-"`
}

type ObservabilitySnapshot struct {
	PersonaID     string                     `json:"persona_id"`
	GeneratedAt   time.Time                  `json:"generated_at"`
	Status        string                     `json:"status"`
	Warnings      []string                   `json:"warnings,omitempty"`
	Store         StoreObservability         `json:"store"`
	Retrieval     RetrievalObservability     `json:"retrieval"`
	Extraction    ExtractionObservability    `json:"extraction"`
	Forgetting    ForgettingObservability    `json:"forgetting"`
	Retention     RetentionObservability     `json:"retention"`
	Mirror        MirrorObservability        `json:"mirror"`
	NaturalMemory NaturalMemoryObservability `json:"natural_memory"`
}

type StoreObservability struct {
	PersonaCount         int64            `json:"persona_count"`
	SessionCount         int64            `json:"session_count"`
	EpisodeByVisibility  map[string]int64 `json:"episode_by_visibility"`
	FactByVisibility     map[string]int64 `json:"fact_by_visibility"`
	FactByValidity       map[string]int64 `json:"fact_by_validity"`
	FactByLifecycle      map[string]int64 `json:"fact_by_lifecycle"`
	FactBySensitivity    map[string]int64 `json:"fact_by_sensitivity"`
	NarrativeCount       int64            `json:"narrative_count"`
	InsightCount         int64            `json:"insight_count"`
	SearchDocumentCount  int64            `json:"search_document_count"`
	SearchDocumentByTier map[string]int64 `json:"search_document_by_tier"`
	FTSAvailable         bool             `json:"fts_available"`
}

type RetrievalObservability struct {
	AccessEventsByType    map[string]int64 `json:"access_events_by_type"`
	RecentRetrievedCount  int64            `json:"recent_retrieved_count"`
	RecentInjectedCount   int64            `json:"recent_prompt_injected_count"`
	RecentSuppressedCount int64            `json:"recent_suppressed_count"`
}

type ExtractionObservability struct {
	RunsByStatus      map[string]int64 `json:"runs_by_status"`
	RecentRunCount    int64            `json:"recent_run_count"`
	RecentFailedCount int64            `json:"recent_failed_count"`
}

type ForgettingObservability struct {
	DeletionEventsByLevel map[string]int64 `json:"deletion_events_by_level"`
	RecentDeletionCount   int64            `json:"recent_deletion_count"`
	PendingManualCount    int64            `json:"pending_manual_count"`
}

type RetentionObservability struct {
	JobsByStatus      map[string]int64 `json:"jobs_by_status"`
	RecentRunCount    int64            `json:"recent_run_count"`
	RecentFailedCount int64            `json:"recent_failed_count"`
}

type MirrorObservability struct {
	EnabledOrConfigured bool             `json:"enabled_or_configured"`
	QueueByOperation    map[string]int64 `json:"queue_by_operation"`
	QueueByStatus       map[string]int64 `json:"queue_by_status"`
	PendingCount        int64            `json:"pending_count"`
	FailedCount         int64            `json:"failed_count"`
	PersonaReady        bool             `json:"persona_ready"`
	PersonaDegraded     bool             `json:"persona_degraded"`
	LastSyncAt          *time.Time       `json:"last_sync_at,omitempty"`
}

type NaturalMemoryObservability struct {
	RunsByStatus         map[string]int64 `json:"runs_by_status"`
	RecentRunCount       int64            `json:"recent_run_count"`
	RecentDecayedNodes   int64            `json:"recent_decayed_nodes"`
	RecentProtectedNodes int64            `json:"recent_protected_nodes"`
}

type ObservabilityRepository struct {
	db   *sql.DB
	time timeFormatter
}

func NewObservabilityRepository(db *sql.DB) *ObservabilityRepository {
	return NewObservabilityRepositoryWithOptions(db, StoreOptions{})
}

func NewObservabilityRepositoryWithOptions(db *sql.DB, opts StoreOptions) *ObservabilityRepository {
	return &ObservabilityRepository{db: db, time: newTimeFormatter(opts)}
}

func (r *ObservabilityRepository) Snapshot(ctx context.Context, req ObservabilitySnapshotRequest) (*ObservabilitySnapshot, error) {
	personaID := strings.TrimSpace(req.PersonaID)
	if personaID == "" {
		personaID = "default"
	}
	now := r.time.nowTime()
	since := req.Since
	if since.IsZero() {
		since = now.Add(-24 * time.Hour)
	}
	snapshot := &ObservabilitySnapshot{
		PersonaID:   personaID,
		GeneratedAt: now,
		Status:      ObservabilityStatusOK,
		Store: StoreObservability{
			EpisodeByVisibility:  map[string]int64{},
			FactByVisibility:     map[string]int64{},
			FactByValidity:       map[string]int64{},
			FactByLifecycle:      map[string]int64{},
			FactBySensitivity:    map[string]int64{},
			SearchDocumentByTier: map[string]int64{},
		},
		Retrieval: RetrievalObservability{
			AccessEventsByType: map[string]int64{},
		},
		Extraction: ExtractionObservability{
			RunsByStatus: map[string]int64{},
		},
		Forgetting: ForgettingObservability{
			DeletionEventsByLevel: map[string]int64{},
		},
		Retention: RetentionObservability{
			JobsByStatus: map[string]int64{},
		},
		Mirror: MirrorObservability{
			QueueByOperation: map[string]int64{},
			QueueByStatus:    map[string]int64{},
			PersonaReady:     true,
		},
		NaturalMemory: NaturalMemoryObservability{
			RunsByStatus: map[string]int64{},
		},
	}

	collector := observabilityCollector{repo: r, snapshot: snapshot, personaID: personaID, since: since}
	collector.collectStore(ctx)
	collector.collectRetrieval(ctx)
	collector.collectExtraction(ctx)
	collector.collectForgetting(ctx)
	collector.collectRetention(req.IncludeDebug)
	collector.collectMirror(ctx, req.MirrorConfigured)
	collector.collectNaturalMemory(ctx)
	return snapshot, nil
}

type observabilityCollector struct {
	repo      *ObservabilityRepository
	snapshot  *ObservabilitySnapshot
	personaID string
	since     time.Time
}

func (c observabilityCollector) collectStore(ctx context.Context) {
	c.count(ctx, "store.persona_count", &c.snapshot.Store.PersonaCount, `
SELECT COUNT(*) FROM personas WHERE id = ?`, c.personaID)
	c.count(ctx, "store.session_count", &c.snapshot.Store.SessionCount, `
SELECT COUNT(*) FROM sessions WHERE persona_id = ?`, c.personaID)
	c.group(ctx, "store.episode_by_visibility", &c.snapshot.Store.EpisodeByVisibility, `
SELECT visibility_status, COUNT(*) FROM episodes WHERE persona_id = ? GROUP BY visibility_status`, c.personaID)
	c.group(ctx, "store.fact_by_visibility", &c.snapshot.Store.FactByVisibility, `
SELECT visibility_status, COUNT(*) FROM facts WHERE persona_id = ? GROUP BY visibility_status`, c.personaID)
	c.group(ctx, "store.fact_by_validity", &c.snapshot.Store.FactByValidity, `
SELECT validity_status, COUNT(*) FROM facts WHERE persona_id = ? GROUP BY validity_status`, c.personaID)
	c.group(ctx, "store.fact_by_lifecycle", &c.snapshot.Store.FactByLifecycle, `
SELECT lifecycle_status, COUNT(*) FROM facts WHERE persona_id = ? GROUP BY lifecycle_status`, c.personaID)
	c.group(ctx, "store.fact_by_sensitivity", &c.snapshot.Store.FactBySensitivity, `
SELECT sensitivity_level, COUNT(*) FROM facts WHERE persona_id = ? GROUP BY sensitivity_level`, c.personaID)
	c.count(ctx, "store.narrative_count", &c.snapshot.Store.NarrativeCount, `
SELECT COUNT(*) FROM narratives WHERE persona_id = ?`, c.personaID)
	c.count(ctx, "store.insight_count", &c.snapshot.Store.InsightCount, `
SELECT COUNT(*) FROM insights WHERE persona_id = ?`, c.personaID)
	c.count(ctx, "store.search_document_count", &c.snapshot.Store.SearchDocumentCount, `
SELECT COUNT(*) FROM memory_search_documents WHERE persona_id = ?`, c.personaID)
	c.group(ctx, "store.search_document_by_tier", &c.snapshot.Store.SearchDocumentByTier, `
SELECT search_tier, COUNT(*) FROM memory_search_documents WHERE persona_id = ? GROUP BY search_tier`, c.personaID)

	var err error
	c.snapshot.Store.FTSAvailable, err = c.repo.tableExists(ctx, "memory_search_fts")
	if err != nil {
		c.warn("store.fts_available", err)
	}
}

func (c observabilityCollector) collectRetrieval(ctx context.Context) {
	sinceText := c.repo.time.formatTime(c.since)
	c.group(ctx, "retrieval.access_events_by_type", &c.snapshot.Retrieval.AccessEventsByType, `
SELECT access_type, COUNT(*) FROM memory_access_events WHERE persona_id = ? GROUP BY access_type`, c.personaID)
	c.count(ctx, "retrieval.recent_retrieved_count", &c.snapshot.Retrieval.RecentRetrievedCount, `
SELECT COUNT(*) FROM memory_access_events WHERE persona_id = ? AND access_type = 'retrieved' AND created_at >= ?`, c.personaID, sinceText)
	c.count(ctx, "retrieval.recent_prompt_injected_count", &c.snapshot.Retrieval.RecentInjectedCount, `
SELECT COUNT(*) FROM memory_access_events WHERE persona_id = ? AND access_type = 'prompt_injected' AND created_at >= ?`, c.personaID, sinceText)
	c.count(ctx, "retrieval.recent_suppressed_count", &c.snapshot.Retrieval.RecentSuppressedCount, `
SELECT COUNT(*) FROM memory_access_events WHERE persona_id = ? AND access_type = 'suppressed' AND created_at >= ?`, c.personaID, sinceText)
}

func (c observabilityCollector) collectExtraction(ctx context.Context) {
	sinceText := c.repo.time.formatTime(c.since)
	c.group(ctx, "extraction.runs_by_status", &c.snapshot.Extraction.RunsByStatus, `
SELECT status, COUNT(*) FROM extraction_runs WHERE persona_id = ? GROUP BY status`, c.personaID)
	c.count(ctx, "extraction.recent_run_count", &c.snapshot.Extraction.RecentRunCount, `
SELECT COUNT(*) FROM extraction_runs WHERE persona_id = ? AND created_at >= ?`, c.personaID, sinceText)
	c.count(ctx, "extraction.recent_failed_count", &c.snapshot.Extraction.RecentFailedCount, `
SELECT COUNT(*) FROM extraction_runs WHERE persona_id = ? AND status IN ('failed', 'partially_failed') AND created_at >= ?`, c.personaID, sinceText)
}

func (c observabilityCollector) collectForgetting(ctx context.Context) {
	sinceText := c.repo.time.formatTime(c.since)
	c.group(ctx, "forgetting.deletion_events_by_level", &c.snapshot.Forgetting.DeletionEventsByLevel, `
SELECT deletion_level, COUNT(*) FROM deletion_events WHERE persona_id = ? GROUP BY deletion_level`, c.personaID)
	c.count(ctx, "forgetting.recent_deletion_count", &c.snapshot.Forgetting.RecentDeletionCount, `
SELECT COUNT(*) FROM deletion_events WHERE persona_id = ? AND created_at >= ?`, c.personaID, sinceText)
	c.count(ctx, "forgetting.pending_manual_count", &c.snapshot.Forgetting.PendingManualCount, `
SELECT COUNT(*) FROM pending_manual_forget_operations WHERE persona_id = ? AND status = 'pending_confirmation'`, c.personaID)
}

func (c observabilityCollector) collectRetention(includeDebug bool) {
	if includeDebug {
		c.snapshot.Warnings = append(c.snapshot.Warnings, "retention run history is not persisted in v0.1")
	}
}

func (c observabilityCollector) collectMirror(ctx context.Context, configured bool) {
	c.group(ctx, "mirror.queue_by_operation", &c.snapshot.Mirror.QueueByOperation, `
SELECT operation, COUNT(*) FROM index_sync_queue WHERE persona_id = ? GROUP BY operation`, c.personaID)
	c.group(ctx, "mirror.queue_by_status", &c.snapshot.Mirror.QueueByStatus, `
SELECT status, COUNT(*) FROM index_sync_queue WHERE persona_id = ? GROUP BY status`, c.personaID)
	c.count(ctx, "mirror.pending_count", &c.snapshot.Mirror.PendingCount, `
SELECT COUNT(*) FROM index_sync_queue WHERE persona_id = ? AND status = 'pending'`, c.personaID)
	c.count(ctx, "mirror.failed_count", &c.snapshot.Mirror.FailedCount, `
SELECT COUNT(*) FROM index_sync_queue WHERE persona_id = ? AND status = 'failed'`, c.personaID)

	var state string
	stateExists := false
	err := c.repo.db.QueryRowContext(ctx, `
SELECT state FROM mirror_persona_state WHERE persona_id = ?`, c.personaID).Scan(&state)
	switch {
	case err == nil:
		stateExists = true
		c.snapshot.Mirror.PersonaReady = state == "ready"
		c.snapshot.Mirror.PersonaDegraded = state == "degraded"
	case err == sql.ErrNoRows:
		c.snapshot.Mirror.PersonaReady = true
	default:
		c.warn("mirror.persona_state", err)
	}

	var lastSync sql.NullString
	err = c.repo.db.QueryRowContext(ctx, `
SELECT MAX(indexed_at)
FROM memory_index_map
WHERE persona_id = ? AND index_status = 'indexed'`, c.personaID).Scan(&lastSync)
	if err != nil {
		c.warn("mirror.last_sync_at", err)
	} else if lastSync.Valid && strings.TrimSpace(lastSync.String) != "" {
		parsed := parseTime(lastSync.String)
		c.snapshot.Mirror.LastSyncAt = &parsed
	}

	var indexedCount int64
	c.count(ctx, "mirror.indexed_count", &indexedCount, `
SELECT COUNT(*) FROM memory_index_map WHERE persona_id = ?`, c.personaID)
	c.snapshot.Mirror.EnabledOrConfigured = configured ||
		stateExists ||
		indexedCount > 0 ||
		c.snapshot.Mirror.PendingCount > 0 ||
		c.snapshot.Mirror.FailedCount > 0 ||
		len(c.snapshot.Mirror.QueueByOperation) > 0 ||
		!c.snapshot.Mirror.PersonaReady ||
		c.snapshot.Mirror.PersonaDegraded
}

func (c observabilityCollector) collectNaturalMemory(ctx context.Context) {
	sinceText := c.repo.time.formatTime(c.since)
	c.group(ctx, "natural_memory.runs_by_status", &c.snapshot.NaturalMemory.RunsByStatus, `
SELECT status, COUNT(*) FROM memory_natural_runs WHERE persona_id = ? GROUP BY status`, c.personaID)
	c.count(ctx, "natural_memory.recent_run_count", &c.snapshot.NaturalMemory.RecentRunCount, `
SELECT COUNT(*) FROM memory_natural_runs WHERE persona_id = ? AND started_at >= ?`, c.personaID, sinceText)
	c.count(ctx, "natural_memory.recent_decayed_nodes", &c.snapshot.NaturalMemory.RecentDecayedNodes, `
SELECT COALESCE(SUM(decayed_nodes), 0) FROM memory_natural_runs WHERE persona_id = ? AND started_at >= ?`, c.personaID, sinceText)
	c.count(ctx, "natural_memory.recent_protected_nodes", &c.snapshot.NaturalMemory.RecentProtectedNodes, `
SELECT COALESCE(SUM(protected_nodes), 0) FROM memory_natural_runs WHERE persona_id = ? AND started_at >= ?`, c.personaID, sinceText)
}

func (c observabilityCollector) count(ctx context.Context, name string, target *int64, query string, args ...any) {
	value, err := c.repo.countRows(ctx, query, args...)
	if err != nil {
		c.warn(name, err)
		return
	}
	*target = value
}

func (c observabilityCollector) group(ctx context.Context, name string, target *map[string]int64, query string, args ...any) {
	value, err := c.repo.groupCounts(ctx, query, args...)
	if err != nil {
		c.warn(name, err)
		return
	}
	*target = value
}

func (c observabilityCollector) warn(name string, err error) {
	c.snapshot.Status = ObservabilityStatusDegraded
	c.snapshot.Warnings = append(c.snapshot.Warnings, fmt.Sprintf("%s: %v", name, err))
}

func (r *ObservabilityRepository) countRows(ctx context.Context, query string, args ...any) (int64, error) {
	var count int64
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *ObservabilityRepository) groupCounts(ctx context.Context, query string, args ...any) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int64{}
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return counts, nil
}

func (r *ObservabilityRepository) tableExists(ctx context.Context, table string) (bool, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_master
WHERE type IN ('table', 'virtual table') AND name = ?`, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
