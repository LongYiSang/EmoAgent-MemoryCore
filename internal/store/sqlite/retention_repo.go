package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type RetentionRepository struct {
	db    *sql.DB
	newID func() string
	now   func() time.Time
}

type RetentionRequest struct {
	PersonaID string
	Now       time.Time
	DryRun    bool
}

type RetentionResult struct {
	EvaluatedFacts        int
	ExpiredFacts          int
	ArchivedFacts         int
	SearchDocumentsSynced int
	MirrorUpdatesEnqueued int
}

type retentionFact struct {
	ID              string
	Pinned          bool
	LifecycleStatus core.LifecycleStatus
	FactType        core.FactType
}

func NewRetentionRepository(db *sql.DB, newID func() string, now func() time.Time) *RetentionRepository {
	if newID == nil {
		counter := 0
		newID = func() string {
			counter++
			return fmt.Sprintf("retention_id_%d", counter)
		}
	}
	if now == nil {
		now = time.Now
	}
	return &RetentionRepository{db: db, newID: newID, now: now}
}

func (r *RetentionRepository) Run(ctx context.Context, req RetentionRequest) (RetentionResult, error) {
	if strings.TrimSpace(req.PersonaID) == "" {
		return RetentionResult{}, errors.New("persona_id is required")
	}
	now := req.Now
	if now.IsZero() {
		now = r.now()
	}

	facts, err := r.expiredFacts(ctx, req.PersonaID, now)
	if err != nil {
		return RetentionResult{}, err
	}
	result := retentionCounts(facts)
	if req.DryRun || len(facts) == 0 {
		return result, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RetentionResult{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result.SearchDocumentsSynced = 0
	result.MirrorUpdatesEnqueued = 0
	updatedAt := formatTime(now)
	for _, fact := range facts {
		var updated bool
		updated, err = updateRetainedFactTx(ctx, tx, req.PersonaID, fact, updatedAt)
		if err != nil {
			return RetentionResult{}, err
		}
		if !updated {
			continue
		}
		if err = upsertFactSearchDocumentTx(ctx, tx, req.PersonaID, fact.ID); err != nil {
			return RetentionResult{}, err
		}
		result.SearchDocumentsSynced++

		mapped, err := factIndexMapExistsTx(ctx, tx, req.PersonaID, fact.ID)
		if err != nil {
			return RetentionResult{}, err
		}
		if mapped {
			if err = enqueueRetentionIndexSyncTx(ctx, tx, r.newID(), req.PersonaID, fact.ID); err != nil {
				return RetentionResult{}, err
			}
			result.MirrorUpdatesEnqueued++
		}
	}
	if err = tx.Commit(); err != nil {
		return RetentionResult{}, err
	}
	return result, nil
}

func (r *RetentionRepository) expiredFacts(ctx context.Context, personaID string, now time.Time) ([]retentionFact, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, pinned, lifecycle_status, fact_type
FROM facts
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND validity_status = 'valid'
  AND valid_to IS NOT NULL
  AND valid_to <= ?
  AND lifecycle_status IN ('active', 'dormant', 'consolidated', 'archived')
ORDER BY valid_to ASC, id ASC`, personaID, formatTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var facts []retentionFact
	for rows.Next() {
		var fact retentionFact
		var pinned int
		if err := rows.Scan(&fact.ID, &pinned, &fact.LifecycleStatus, &fact.FactType); err != nil {
			return nil, err
		}
		fact.Pinned = intBool(pinned)
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return facts, nil
}

func retentionCounts(facts []retentionFact) RetentionResult {
	result := RetentionResult{
		EvaluatedFacts: len(facts),
		ExpiredFacts:   len(facts),
	}
	for _, fact := range facts {
		if shouldArchiveExpiredFact(fact) {
			result.ArchivedFacts++
		}
	}
	return result
}

func updateRetainedFactTx(ctx context.Context, tx *sql.Tx, personaID string, fact retentionFact, updatedAt string) (bool, error) {
	lifecycle := fact.LifecycleStatus
	if shouldArchiveExpiredFact(fact) {
		lifecycle = core.LifecycleArchived
	}
	result, err := tx.ExecContext(ctx, `
UPDATE facts
SET validity_status = 'invalidated',
    lifecycle_status = ?,
    updated_at = ?
WHERE persona_id = ?
  AND id = ?
  AND validity_status = 'valid'`,
		string(lifecycle),
		updatedAt,
		personaID,
		fact.ID,
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

func shouldArchiveExpiredFact(fact retentionFact) bool {
	if fact.Pinned {
		return false
	}
	switch fact.FactType {
	case core.FactTypeCoreIdentity, core.FactTypeCommitment:
		return false
	default:
		return true
	}
}

func factIndexMapExistsTx(ctx context.Context, tx *sql.Tx, personaID string, factID string) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_index_map
WHERE persona_id = ?
  AND node_type = 'fact'
  AND node_id = ?`, personaID, factID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func enqueueRetentionIndexSyncTx(ctx context.Context, tx *sql.Tx, id string, personaID string, factID string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (?, ?, 'fact', ?, 'upsert_node')`, id, personaID, factID)
	return err
}
