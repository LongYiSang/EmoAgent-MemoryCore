package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type NaturalMemoryRepository struct {
	db    *sql.DB
	newID func() string
	now   func() time.Time
	time  timeFormatter
}

type NaturalMemoryRunRow struct {
	ID                          string
	PersonaID                   string
	RunKind                     string
	AlgorithmVersion            string
	LocalDate                   string
	LocalTime                   string
	Timezone                    string
	DryRun                      bool
	Force                       bool
	MarkSleepCycle              bool
	Status                      string
	EvaluatedNodes              int
	ScoredNodes                 int
	ProtectedNodes              int
	DecayedNodes                int
	ReactivatedNodes            int
	FirstSleepConsolidatedNodes int
	SearchTierUpdates           int
	SearchDocumentsCreated      int
	MirrorUpdatesEnqueued       int
	CompressionCandidates       int
	NarrativesCreated           int
	InsightsCreated             int
}

type NaturalMemoryCandidateRow struct {
	PersonaID                  string
	NodeType                   core.NodeType
	NodeID                     string
	FactType                   core.FactType
	Importance                 float64
	Confidence                 float64
	SensitivityLevel           core.SensitivityLevel
	ValidityStatus             core.ValidityStatus
	VisibilityStatus           core.VisibilityStatus
	LifecycleStatus            core.LifecycleStatus
	Pinned                     bool
	Searchable                 bool
	AccessCount                int
	ReinforcementCount         int
	CongruentAccessCount       int
	RecentAccessEventCount     int
	RecentPromptInjectedCount  int
	RecentReinforcedEventCount int
	StructuralAssociationCount int
	CreatedAt                  time.Time
	UpdatedAt                  *time.Time
	IngestedAt                 *time.Time
	LastAccessedAt             *time.Time
	ValidTo                    *time.Time
	CurrentSearchTier          core.SearchTier
	PreviousNaturalState       string
	FirstSleepConsolidated     bool
	EmotionSalienceHint        float64
	EmotionPersistenceHint     float64
	ReactivationCount          int
	LastReactivatedAt          *time.Time
	LastStrengthenedAt         *time.Time
	ClusterKey                 string
}

type NaturalMemoryStateWrite struct {
	RunID                      string
	PersonaID                  string
	NodeType                   core.NodeType
	NodeID                     string
	AlgorithmVersion           string
	NaturalStrength            float64
	Retrievability             float64
	StabilityDays              float64
	DecayExponent              float64
	NaturalState               string
	LastSimulatedAt            time.Time
	LastReactivatedAt          *time.Time
	LastStrengthenedAt         *time.Time
	FirstSleepConsolidated     bool
	ReactivationCountIncrement bool
	ProtectedReason            string
	EmotionSalienceHint        float64
	EmotionPersistenceHint     float64
	ScoreBreakdownJSON         string
}

type NaturalMemoryEventWrite struct {
	RunID             string
	PersonaID         string
	NodeType          core.NodeType
	NodeID            string
	EventType         string
	FromNaturalState  string
	ToNaturalState    string
	FromSearchTier    core.SearchTier
	ToSearchTier      core.SearchTier
	NaturalStrength   float64
	Retrievability    float64
	StabilityDays     float64
	DecayExponent     float64
	ReactivationScore float64
	ReasonCodes       []string
	SafeReasonSummary string
}

type NaturalCompressionCandidateWrite struct {
	RunID             string
	PersonaID         string
	ClusterKey        string
	TargetNodeType    core.NodeType
	SourceRefs        []string
	CandidateSummary  string
	AvgRetrievability float64
	AvgImportance     float64
	MinConfidence     float64
	ReasonCodes       []string
}

func NewNaturalMemoryRepository(db *sql.DB, newID func() string, now func() time.Time) *NaturalMemoryRepository {
	return NewNaturalMemoryRepositoryWithOptions(db, newID, now, StoreOptions{})
}

func NewNaturalMemoryRepositoryWithOptions(db *sql.DB, newID func() string, now func() time.Time, opts StoreOptions) *NaturalMemoryRepository {
	if newID == nil {
		counter := 0
		newID = func() string {
			counter++
			return fmt.Sprintf("natural_id_%d", counter)
		}
	}
	if now == nil {
		now = time.Now
	}
	if opts.Now == nil {
		opts.Now = now
	}
	return &NaturalMemoryRepository{db: db, newID: newID, now: now, time: newTimeFormatter(opts)}
}

func (r *NaturalMemoryRepository) NewID() string {
	return r.newID()
}

func (r *NaturalMemoryRepository) ListCandidates(ctx context.Context, personaID string, limit int, now time.Time) ([]NaturalMemoryCandidateRow, int, error) {
	if strings.TrimSpace(personaID) == "" {
		return nil, 0, errors.New("persona_id is required")
	}
	if now.IsZero() {
		now = r.now()
	}
	if limit <= 0 {
		limit = 5000
	}
	candidates, skipped, err := r.listFactCandidates(ctx, personaID, limit)
	if err != nil {
		return nil, 0, err
	}
	remaining := limit - len(candidates)
	if remaining > 0 {
		narratives, skippedNarratives, err := r.listNarrativeCandidates(ctx, personaID, remaining)
		if err != nil {
			return nil, 0, err
		}
		candidates = append(candidates, narratives...)
		skipped += skippedNarratives
	}
	remaining = limit - len(candidates)
	if remaining > 0 {
		insights, skippedInsights, err := r.listInsightCandidates(ctx, personaID, remaining)
		if err != nil {
			return nil, 0, err
		}
		candidates = append(candidates, insights...)
		skipped += skippedInsights
	}
	if err := r.hydrateCandidateReactivationSignals(ctx, candidates, now); err != nil {
		return nil, 0, err
	}
	return candidates, skipped, nil
}

func (r *NaturalMemoryRepository) listFactCandidates(ctx context.Context, personaID string, limit int) ([]NaturalMemoryCandidateRow, int, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT f.persona_id, 'fact', f.id, f.fact_type, f.importance, f.extraction_confidence_score,
       f.sensitivity_level, f.validity_status, f.visibility_status, f.lifecycle_status,
       f.pinned, f.searchable, f.access_count, f.reinforcement_count, f.congruent_access_count,
       f.created_at, COALESCE(f.updated_at, ''), COALESCE(f.ingested_at, ''),
       COALESCE(f.last_accessed_at, ''), COALESCE(f.valid_to, ''),
       COALESCE(d.search_tier, 'hot'), COALESCE(s.natural_state, ''),
       COALESCE(s.first_sleep_consolidated, 0), COALESCE(s.emotion_salience_hint, 0),
       COALESCE(s.emotion_persistence_hint, 0), COALESCE(s.reactivation_count, 0),
       COALESCE(s.last_reactivated_at, ''), COALESCE(s.last_strengthened_at, ''),
       f.predicate
FROM facts f
LEFT JOIN memory_search_documents d
  ON d.persona_id = f.persona_id
 AND d.node_type = 'fact'
 AND d.node_id = f.id
LEFT JOIN memory_natural_states s
  ON s.persona_id = f.persona_id
 AND s.node_type = 'fact'
 AND s.node_id = f.id
WHERE f.persona_id = ?
  AND f.visibility_status = 'visible'
  AND f.validity_status IN ('valid', 'uncertain')
  AND f.searchable = 1
  AND f.lifecycle_status != 'deep_archived'
ORDER BY f.created_at ASC, f.id ASC
LIMIT ?`, personaID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	return scanNaturalCandidateRows(rows)
}

func (r *NaturalMemoryRepository) listNarrativeCandidates(ctx context.Context, personaID string, limit int) ([]NaturalMemoryCandidateRow, int, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT n.persona_id, 'narrative', n.id, '', n.importance, 0.7,
       n.sensitivity_level, 'valid', n.visibility_status, n.lifecycle_status,
       0, n.searchable, 0, 0, 0,
       n.generated_at, '', '', '', COALESCE(n.valid_to, ''),
       COALESCE(d.search_tier, 'hot'), COALESCE(s.natural_state, ''),
       COALESCE(s.first_sleep_consolidated, 0), COALESCE(s.emotion_salience_hint, 0),
       COALESCE(s.emotion_persistence_hint, 0), COALESCE(s.reactivation_count, 0),
       COALESCE(s.last_reactivated_at, ''), COALESCE(s.last_strengthened_at, ''),
       n.scope
FROM narratives n
LEFT JOIN memory_search_documents d
  ON d.persona_id = n.persona_id
 AND d.node_type = 'narrative'
 AND d.node_id = n.id
LEFT JOIN memory_natural_states s
  ON s.persona_id = n.persona_id
 AND s.node_type = 'narrative'
 AND s.node_id = n.id
WHERE n.persona_id = ?
  AND n.visibility_status = 'visible'
  AND n.searchable = 1
  AND n.lifecycle_status != 'deep_archived'
ORDER BY n.generated_at ASC, n.id ASC
LIMIT ?`, personaID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	return scanNaturalCandidateRows(rows)
}

func (r *NaturalMemoryRepository) listInsightCandidates(ctx context.Context, personaID string, limit int) ([]NaturalMemoryCandidateRow, int, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT i.persona_id, 'insight', i.id, '', i.importance, i.confidence,
       i.sensitivity_level, 'valid', i.visibility_status, i.lifecycle_status,
       0, i.searchable, 0, 0, 0,
       i.created_at, COALESCE(i.updated_at, ''), '', '', '',
       COALESCE(d.search_tier, 'hot'), COALESCE(s.natural_state, ''),
       COALESCE(s.first_sleep_consolidated, 0), COALESCE(s.emotion_salience_hint, 0),
       COALESCE(s.emotion_persistence_hint, 0), COALESCE(s.reactivation_count, 0),
       COALESCE(s.last_reactivated_at, ''), COALESCE(s.last_strengthened_at, ''),
       i.insight_type
FROM insights i
LEFT JOIN memory_search_documents d
  ON d.persona_id = i.persona_id
 AND d.node_type = 'insight'
 AND d.node_id = i.id
LEFT JOIN memory_natural_states s
  ON s.persona_id = i.persona_id
 AND s.node_type = 'insight'
 AND s.node_id = i.id
WHERE i.persona_id = ?
  AND i.visibility_status = 'visible'
  AND i.searchable = 1
  AND i.lifecycle_status != 'deep_archived'
ORDER BY i.created_at ASC, i.id ASC
LIMIT ?`, personaID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	return scanNaturalCandidateRows(rows)
}

func scanNaturalCandidateRows(rows *sql.Rows) ([]NaturalMemoryCandidateRow, int, error) {
	candidates := make([]NaturalMemoryCandidateRow, 0)
	skipped := 0
	for rows.Next() {
		row, err := scanNaturalMemoryCandidate(rows)
		if err != nil {
			return nil, 0, err
		}
		if !naturalCandidateEligible(row) {
			skipped++
			continue
		}
		candidates = append(candidates, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return candidates, skipped, nil
}

func (r *NaturalMemoryRepository) hydrateCandidateReactivationSignals(ctx context.Context, candidates []NaturalMemoryCandidateRow, now time.Time) error {
	cutoff := r.time.formatTime(now.Add(-7 * 24 * time.Hour))
	for i := range candidates {
		access, promptInjected, reinforced, err := r.countRecentAccessSignals(ctx, candidates[i], cutoff)
		if err != nil {
			return err
		}
		structural, err := r.countStructuralAssociations(ctx, candidates[i], cutoff)
		if err != nil {
			return err
		}
		candidates[i].RecentAccessEventCount = access
		candidates[i].RecentPromptInjectedCount = promptInjected
		candidates[i].RecentReinforcedEventCount = reinforced
		candidates[i].StructuralAssociationCount = structural
	}
	return nil
}

func (r *NaturalMemoryRepository) countRecentAccessSignals(ctx context.Context, row NaturalMemoryCandidateRow, cutoff string) (int, int, int, error) {
	var access, promptInjected, reinforced int
	err := r.db.QueryRowContext(ctx, `
SELECT
    COALESCE(SUM(CASE WHEN access_type IN ('retrieved', 'prompt_injected', 'reinforced') THEN 1 ELSE 0 END), 0),
    COALESCE(SUM(CASE WHEN access_type = 'prompt_injected' THEN 1 ELSE 0 END), 0),
    COALESCE(SUM(CASE WHEN access_type = 'reinforced' THEN 1 ELSE 0 END), 0)
FROM memory_access_events
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?
  AND datetime(created_at) >= datetime(?)`,
		row.PersonaID,
		string(row.NodeType),
		row.NodeID,
		cutoff,
	).Scan(&access, &promptInjected, &reinforced)
	if err != nil {
		return 0, 0, 0, err
	}
	return access, promptInjected, reinforced, nil
}

func (r *NaturalMemoryRepository) countStructuralAssociations(ctx context.Context, row NaturalMemoryCandidateRow, cutoff string) (int, error) {
	linked, err := r.countLinkedActiveAccesses(ctx, row, cutoff)
	if err != nil {
		return 0, err
	}
	if row.NodeType != core.NodeTypeFact || strings.TrimSpace(row.ClusterKey) == "" {
		return linked, nil
	}
	var sharedPredicate int
	err = r.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT e.node_id)
FROM memory_access_events e
JOIN facts f
  ON f.persona_id = e.persona_id
 AND f.id = e.node_id
WHERE e.persona_id = ?
  AND e.node_type = 'fact'
  AND e.node_id != ?
  AND e.access_type IN ('retrieved', 'prompt_injected', 'reinforced')
  AND datetime(e.created_at) >= datetime(?)
  AND f.visibility_status = 'visible'
  AND f.searchable = 1
  AND f.predicate = ?`,
		row.PersonaID,
		row.NodeID,
		cutoff,
		row.ClusterKey,
	).Scan(&sharedPredicate)
	if err != nil {
		return 0, err
	}
	return linked + sharedPredicate, nil
}

func (r *NaturalMemoryRepository) countLinkedActiveAccesses(ctx context.Context, row NaturalMemoryCandidateRow, cutoff string) (int, error) {
	var linked int
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(DISTINCT e.node_type || ':' || e.node_id)
FROM memory_links l
JOIN memory_access_events e
  ON e.persona_id = l.persona_id
 AND e.access_type IN ('retrieved', 'prompt_injected', 'reinforced')
 AND datetime(e.created_at) >= datetime(?)
 AND (
      (l.from_node_type = ? AND l.from_node_id = ? AND e.node_type = l.to_node_type AND e.node_id = l.to_node_id)
   OR (l.to_node_type = ? AND l.to_node_id = ? AND e.node_type = l.from_node_type AND e.node_id = l.from_node_id)
 )
WHERE l.persona_id = ?
  AND l.visibility_status = 'visible'
  AND l.searchable = 1`,
		cutoff,
		string(row.NodeType),
		row.NodeID,
		string(row.NodeType),
		row.NodeID,
		row.PersonaID,
	).Scan(&linked)
	if err != nil {
		return 0, err
	}
	return linked, nil
}

func naturalCandidateEligible(row NaturalMemoryCandidateRow) bool {
	return row.VisibilityStatus == core.VisibilityVisible &&
		row.ValidityStatus != core.ValidityInvalidated &&
		row.Searchable &&
		row.LifecycleStatus != core.LifecycleDeepArchived
}

func scanNaturalMemoryCandidate(row interface{ Scan(dest ...any) error }) (NaturalMemoryCandidateRow, error) {
	var item NaturalMemoryCandidateRow
	var pinned, searchable, firstSleep int
	var createdAt, updatedAt, ingestedAt, lastAccessedAt, validTo, lastReactivatedAt, lastStrengthenedAt string
	if err := row.Scan(
		&item.PersonaID,
		&item.NodeType,
		&item.NodeID,
		&item.FactType,
		&item.Importance,
		&item.Confidence,
		&item.SensitivityLevel,
		&item.ValidityStatus,
		&item.VisibilityStatus,
		&item.LifecycleStatus,
		&pinned,
		&searchable,
		&item.AccessCount,
		&item.ReinforcementCount,
		&item.CongruentAccessCount,
		&createdAt,
		&updatedAt,
		&ingestedAt,
		&lastAccessedAt,
		&validTo,
		&item.CurrentSearchTier,
		&item.PreviousNaturalState,
		&firstSleep,
		&item.EmotionSalienceHint,
		&item.EmotionPersistenceHint,
		&item.ReactivationCount,
		&lastReactivatedAt,
		&lastStrengthenedAt,
		&item.ClusterKey,
	); err != nil {
		return NaturalMemoryCandidateRow{}, err
	}
	item.Pinned = intBool(pinned)
	item.Searchable = intBool(searchable)
	item.FirstSleepConsolidated = intBool(firstSleep)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = nullableParsedTime(updatedAt)
	item.IngestedAt = nullableParsedTime(ingestedAt)
	item.LastAccessedAt = nullableParsedTime(lastAccessedAt)
	item.ValidTo = nullableParsedTime(validTo)
	item.LastReactivatedAt = nullableParsedTime(lastReactivatedAt)
	item.LastStrengthenedAt = nullableParsedTime(lastStrengthenedAt)
	return item, nil
}

func (r *NaturalMemoryRepository) SleepCycleCompletedForDate(ctx context.Context, personaID string, localDate string) (bool, error) {
	return rowExists(ctx, r.db, `
SELECT COUNT(*)
FROM memory_natural_runs
WHERE persona_id = ?
  AND (run_kind = 'sleep_cycle' OR mark_sleep_cycle = 1)
  AND local_date = ?
  AND status = 'completed'
  AND force = 0`, personaID, localDate)
}

func (r *NaturalMemoryRepository) LastCompletedSleepCycle(ctx context.Context, personaID string) (time.Time, bool, error) {
	var completedAt string
	err := r.db.QueryRowContext(ctx, `
SELECT completed_at
FROM memory_natural_runs
WHERE persona_id = ?
  AND (run_kind = 'sleep_cycle' OR mark_sleep_cycle = 1)
  AND status = 'completed'
  AND force = 0
  AND completed_at IS NOT NULL
ORDER BY datetime(replace(replace(completed_at, 'T', ' '), 'Z', '')) DESC
LIMIT 1`, personaID).Scan(&completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return parseTime(completedAt), true, nil
}

func (r *NaturalMemoryRepository) PersistRun(ctx context.Context, run NaturalMemoryRunRow) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO memory_natural_runs (
    id, persona_id, run_kind, algorithm_version, local_date, local_time, timezone,
    dry_run, force, mark_sleep_cycle, started_at, completed_at, status,
    evaluated_nodes, scored_nodes, protected_nodes, decayed_nodes, reactivated_nodes,
    first_sleep_consolidated_nodes, search_tier_updates, search_documents_created,
    mirror_updates_enqueued, compression_candidates, narratives_created, insights_created
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.PersonaID,
		run.RunKind,
		run.AlgorithmVersion,
		nullableText(run.LocalDate),
		nullableText(run.LocalTime),
		nullableText(run.Timezone),
		boolInt(run.DryRun),
		boolInt(run.Force),
		boolInt(run.MarkSleepCycle),
		r.time.nowText(),
		r.time.nowText(),
		run.Status,
		run.EvaluatedNodes,
		run.ScoredNodes,
		run.ProtectedNodes,
		run.DecayedNodes,
		run.ReactivatedNodes,
		run.FirstSleepConsolidatedNodes,
		run.SearchTierUpdates,
		run.SearchDocumentsCreated,
		run.MirrorUpdatesEnqueued,
		run.CompressionCandidates,
		run.NarrativesCreated,
		run.InsightsCreated,
	)
	return err
}

func (r *NaturalMemoryRepository) ApplyNodeWrites(ctx context.Context, state NaturalMemoryStateWrite, events []NaturalMemoryEventWrite, targetTier core.SearchTier, writeSearchDocument bool, enqueueMirrorUpsert bool) (bool, bool, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, false, err
	}
	defer rollbackUnlessCommitted(tx)

	if err := r.upsertStateTx(ctx, tx, state); err != nil {
		return false, false, false, err
	}
	for _, event := range events {
		if err := r.insertEventTx(ctx, tx, event); err != nil {
			return false, false, false, err
		}
	}
	documentCreated := false
	tierChanged := false
	mirrorEnqueued := false
	if writeSearchDocument {
		documentCreated, err = r.ensureSearchDocumentTx(ctx, tx, state.PersonaID, state.NodeType, state.NodeID)
		if err != nil {
			return false, false, false, err
		}
		tierChanged, err = r.updateSearchTierOnlyTx(ctx, tx, state.PersonaID, state.NodeType, state.NodeID, targetTier)
		if err != nil {
			return false, false, false, err
		}
		if tierChanged && enqueueMirrorUpsert {
			mapped, err := naturalIndexMapExistsTx(ctx, tx, state.PersonaID, state.NodeType, state.NodeID)
			if err != nil {
				return false, false, false, err
			}
			if mapped {
				if err := enqueueNaturalIndexSyncTx(ctx, tx, r.newID(), state.PersonaID, state.NodeType, state.NodeID); err != nil {
					return false, false, false, err
				}
				mirrorEnqueued = true
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return false, false, false, err
	}
	tx = nil
	return tierChanged, mirrorEnqueued, documentCreated, nil
}

func (r *NaturalMemoryRepository) EmitCompressionCandidate(ctx context.Context, candidate NaturalCompressionCandidateWrite, events []NaturalMemoryEventWrite) error {
	sourceRefs, err := json.Marshal(candidate.SourceRefs)
	if err != nil {
		return err
	}
	reasonCodes, err := json.Marshal(candidate.ReasonCodes)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)

	_, err = tx.ExecContext(ctx, `
INSERT INTO memory_natural_compression_candidates (
    id, run_id, persona_id, cluster_key, target_node_type, source_refs_json,
    candidate_summary, avg_retrievability, avg_importance, min_confidence,
    status, reason_codes_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'emitted', ?, ?, ?)`,
		r.newID(),
		nullableText(candidate.RunID),
		candidate.PersonaID,
		candidate.ClusterKey,
		string(candidate.TargetNodeType),
		string(sourceRefs),
		nullableText(candidate.CandidateSummary),
		candidate.AvgRetrievability,
		candidate.AvgImportance,
		candidate.MinConfidence,
		string(reasonCodes),
		r.time.nowText(),
		r.time.nowText(),
	)
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := r.insertEventTx(ctx, tx, event); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func (r *NaturalMemoryRepository) upsertStateTx(ctx context.Context, tx *sql.Tx, state NaturalMemoryStateWrite) error {
	reactivationIncrement := 0
	if state.ReactivationCountIncrement {
		reactivationIncrement = 1
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO memory_natural_states (
    persona_id, node_type, node_id, algorithm_version, natural_strength,
    retrievability, stability_days, decay_exponent, natural_state,
    last_simulated_at, last_reactivated_at, last_strengthened_at,
    first_sleep_consolidated, reactivation_count, protected_reason,
    emotion_salience_hint, emotion_persistence_hint, score_breakdown_json, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(persona_id, node_type, node_id) DO UPDATE SET
    algorithm_version = excluded.algorithm_version,
    natural_strength = excluded.natural_strength,
    retrievability = excluded.retrievability,
    stability_days = excluded.stability_days,
    decay_exponent = excluded.decay_exponent,
    natural_state = excluded.natural_state,
    last_simulated_at = excluded.last_simulated_at,
    last_reactivated_at = COALESCE(excluded.last_reactivated_at, memory_natural_states.last_reactivated_at),
    last_strengthened_at = COALESCE(excluded.last_strengthened_at, memory_natural_states.last_strengthened_at),
    first_sleep_consolidated = CASE
        WHEN excluded.first_sleep_consolidated = 1 THEN 1
        ELSE memory_natural_states.first_sleep_consolidated
    END,
    reactivation_count = memory_natural_states.reactivation_count + ?,
    protected_reason = excluded.protected_reason,
    emotion_salience_hint = excluded.emotion_salience_hint,
    emotion_persistence_hint = excluded.emotion_persistence_hint,
    score_breakdown_json = excluded.score_breakdown_json,
    updated_at = excluded.updated_at`,
		state.PersonaID,
		string(state.NodeType),
		state.NodeID,
		state.AlgorithmVersion,
		state.NaturalStrength,
		state.Retrievability,
		state.StabilityDays,
		state.DecayExponent,
		state.NaturalState,
		r.time.formatTime(state.LastSimulatedAt),
		nullableTimeText(r.time, state.LastReactivatedAt),
		nullableTimeText(r.time, state.LastStrengthenedAt),
		boolInt(state.FirstSleepConsolidated),
		reactivationIncrement,
		nullableText(state.ProtectedReason),
		state.EmotionSalienceHint,
		state.EmotionPersistenceHint,
		nullableText(state.ScoreBreakdownJSON),
		r.time.nowText(),
		reactivationIncrement,
	)
	return err
}

func (r *NaturalMemoryRepository) insertEventTx(ctx context.Context, tx *sql.Tx, event NaturalMemoryEventWrite) error {
	reasonCodes, err := json.Marshal(event.ReasonCodes)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO memory_natural_events (
    id, run_id, persona_id, node_type, node_id, event_type,
    from_natural_state, to_natural_state, from_search_tier, to_search_tier,
    natural_strength, retrievability, stability_days, decay_exponent,
    reactivation_score, reason_codes_json, safe_reason_summary, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.newID(),
		nullableText(event.RunID),
		event.PersonaID,
		string(event.NodeType),
		event.NodeID,
		event.EventType,
		nullableText(event.FromNaturalState),
		nullableText(event.ToNaturalState),
		nullableText(string(event.FromSearchTier)),
		nullableText(string(event.ToSearchTier)),
		event.NaturalStrength,
		event.Retrievability,
		event.StabilityDays,
		event.DecayExponent,
		event.ReactivationScore,
		string(reasonCodes),
		nullableText(event.SafeReasonSummary),
		r.time.nowText(),
	)
	return err
}

func (r *NaturalMemoryRepository) updateSearchTierOnlyTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType core.NodeType, nodeID string, tier core.SearchTier) (bool, error) {
	result, err := tx.ExecContext(ctx, `
UPDATE memory_search_documents
SET search_tier = ?,
    updated_at = ?
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?
  AND search_tier != ?`,
		string(tier),
		r.time.nowText(),
		personaID,
		string(nodeType),
		nodeID,
		string(tier),
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (r *NaturalMemoryRepository) ensureSearchDocumentTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType core.NodeType, nodeID string) (bool, error) {
	exists, err := rowExists(ctx, tx, `
SELECT COUNT(*)
FROM memory_search_documents
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?`, personaID, string(nodeType), nodeID)
	if err != nil || exists {
		return false, err
	}
	switch nodeType {
	case core.NodeTypeFact:
		if err := upsertFactSearchDocumentTxWithFormatter(ctx, tx, personaID, nodeID, r.time); err != nil {
			return false, err
		}
	case core.NodeTypeNarrative:
		if err := upsertNarrativeSearchDocumentTxWithFormatter(ctx, tx, personaID, nodeID, r.time); err != nil {
			return false, err
		}
	case core.NodeTypeInsight:
		if err := upsertInsightSearchDocumentTxWithFormatter(ctx, tx, personaID, nodeID, r.time); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("unsupported natural memory node_type %q", nodeType)
	}
	return true, nil
}

func naturalIndexMapExistsTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType core.NodeType, nodeID string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_index_map
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?`, personaID, string(nodeType), nodeID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func enqueueNaturalIndexSyncTx(ctx context.Context, tx *sql.Tx, id string, personaID string, nodeType core.NodeType, nodeID string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (?, ?, ?, ?, 'upsert_node')`, id, personaID, string(nodeType), nodeID)
	return err
}

func nullableParsedTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed := parseTime(value)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func nullableTimeText(formatter timeFormatter, value *time.Time) sql.NullString {
	if value == nil || value.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: formatter.formatTime(*value), Valid: true}
}
