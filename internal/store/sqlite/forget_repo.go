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

const (
	ForgetScopeExactNode = "exact_node"

	ForgetActorUser   = "user"
	ForgetActorSystem = "system"
	ForgetActorAdmin  = "admin"

	ForgetReasonUserRequested   = "user_requested"
	ForgetReasonRetentionPolicy = "retention_policy"
	ForgetReasonSafety          = "safety"
	ForgetReasonAdminPolicy     = "admin_policy"

	ForgetLevelSoft         = "soft_forget"
	ForgetLevelHard         = "hard_forget"
	ForgetLevelSourceRedact = "source_redact"
	ForgetLevelPurge        = "purge"

	ForgottenPlaceholder = "[forgotten]"
	RedactedPlaceholder  = "[redacted]"
)

type ForgetRepository struct {
	db    *sql.DB
	newID func() string
	now   func() time.Time
}

type ForgetRequest struct {
	PersonaID  string
	Actor      string
	ReasonCode string
	Level      string
	Target     ForgetTarget
}

type ForgetTarget struct {
	ScopeMode string
	NodeType  core.NodeType
	NodeID    string
}

type ForgetResult struct {
	DeletionEventID        string
	TargetNodeType         core.NodeType
	TargetNodeID           string
	SearchDocumentsDeleted int64
	FTSRowsDeleted         int64
	MirrorDeletesEnqueued  int64
	LinksScrubbed          int64
}

type forgetCounts struct {
	SearchDocumentsDeleted int64 `json:"search_documents_deleted"`
	FTSRowsDeleted         int64 `json:"fts_rows_deleted"`
	MirrorDeletesEnqueued  int64 `json:"mirror_deletes_enqueued"`
	LinksScrubbed          int64 `json:"links_scrubbed,omitempty"`
	TombstonesWritten      int64 `json:"tombstones_written,omitempty"`
}

func NewForgetRepository(db *sql.DB, newID func() string, now func() time.Time) *ForgetRepository {
	if newID == nil {
		counter := 0
		newID = func() string {
			counter++
			return "forget_" + formatInt(counter)
		}
	}
	if now == nil {
		now = time.Now
	}
	return &ForgetRepository{db: db, newID: newID, now: now}
}

func (r *ForgetRepository) Forget(ctx context.Context, req ForgetRequest) (ForgetResult, error) {
	if err := validateSQLiteForgetRequest(req); err != nil {
		return ForgetResult{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ForgetResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var result ForgetResult
	switch req.Level {
	case ForgetLevelSoft:
		result, err = r.softForgetFactTx(ctx, tx, req)
	case ForgetLevelHard:
		result, err = r.hardForgetFactTx(ctx, tx, req)
	case ForgetLevelSourceRedact:
		result, err = r.sourceRedactEpisodeTx(ctx, tx, req)
	case ForgetLevelPurge:
		switch req.Target.NodeType {
		case core.NodeTypeFact:
			result, err = r.purgeFactTx(ctx, tx, req)
		case core.NodeTypeEpisode:
			result, err = r.purgeEpisodeTx(ctx, tx, req)
		default:
			err = fmt.Errorf("purge only supports fact or episode targets")
		}
	default:
		err = fmt.Errorf("unsupported forget level %q", req.Level)
	}
	if err != nil {
		return ForgetResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return ForgetResult{}, err
	}
	committed = true
	return result, nil
}

func (r *ForgetRepository) softForgetFactTx(ctx context.Context, tx *sql.Tx, req ForgetRequest) (ForgetResult, error) {
	if err := requireFactExistsTx(ctx, tx, req.PersonaID, req.Target.NodeID); err != nil {
		return ForgetResult{}, err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE facts
SET visibility_status = 'hidden',
    searchable = 0,
    pinned = 0,
    pin_reason = NULL,
    pin_actor = NULL,
    pinned_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE persona_id = ? AND id = ?`, req.PersonaID, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}

	counts, err := r.removeSearchAndMirrorTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}
	edgeDeletes, err := r.enqueueMirrorEdgeDeletesForNodeTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID, nil)
	if err != nil {
		return ForgetResult{}, err
	}
	counts.MirrorDeletesEnqueued += edgeDeletes
	return r.completeForgetTx(ctx, tx, req, counts)
}

func (r *ForgetRepository) hardForgetFactTx(ctx context.Context, tx *sql.Tx, req ForgetRequest) (ForgetResult, error) {
	if err := requireFactExistsTx(ctx, tx, req.PersonaID, req.Target.NodeID); err != nil {
		return ForgetResult{}, err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE facts
SET subject_entity_id = NULL,
    predicate = ?,
    object_entity_id = NULL,
    object_literal = NULL,
    content_summary = ?,
    extraction_reasoning = NULL,
    visibility_status = 'forgotten',
    searchable = 0,
    pinned = 0,
    pin_reason = NULL,
    pin_actor = NULL,
    pinned_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE persona_id = ? AND id = ?`,
		ForgottenPlaceholder,
		ForgottenPlaceholder,
		req.PersonaID,
		req.Target.NodeID,
	)
	if err != nil {
		return ForgetResult{}, err
	}
	linkResult, err := tx.ExecContext(ctx, `
UPDATE memory_links
SET reasoning = NULL
WHERE persona_id = ?
  AND reasoning IS NOT NULL
  AND (
      (from_node_type = 'fact' AND from_node_id = ?)
      OR (to_node_type = 'fact' AND to_node_id = ?)
  )`, req.PersonaID, req.Target.NodeID, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}

	counts, err := r.removeSearchAndMirrorTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}
	edgeDeletes, err := r.enqueueMirrorEdgeDeletesForNodeTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID, nil)
	if err != nil {
		return ForgetResult{}, err
	}
	counts.MirrorDeletesEnqueued += edgeDeletes
	counts.LinksScrubbed = rowsAffected(linkResult)
	return r.completeForgetTx(ctx, tx, req, counts)
}

func (r *ForgetRepository) sourceRedactEpisodeTx(ctx context.Context, tx *sql.Tx, req ForgetRequest) (ForgetResult, error) {
	episode, err := loadEpisodeRedactionAnchorTx(ctx, tx, req.PersonaID, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}
	sourceRefHash := sql.NullString{}
	if episode.SourceRef.Valid {
		sourceRefHash = sql.NullString{String: hashContent(episode.SourceRef.String), Valid: true}
	}
	tombstone, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO episode_tombstones (
    episode_id, persona_id, session_id, occurred_at, redacted_at,
    redaction_level, redaction_actor, redaction_reason_code,
    source_type, source_ref_hash, content_hash_before_redaction
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Target.NodeID,
		req.PersonaID,
		nullableStringValue(episode.SessionID),
		nullableStringValue(episode.OccurredAt),
		formatTime(r.now()),
		req.Level,
		req.Actor,
		req.ReasonCode,
		nullableStringValue(episode.SourceType),
		sourceRefHash,
		nullableStringValue(episode.ContentHash),
	)
	if err != nil {
		return ForgetResult{}, err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE episodes
SET content = ?,
    content_hash = ?,
    visibility_status = 'redacted',
    searchable = 0
WHERE persona_id = ? AND id = ?`,
		RedactedPlaceholder,
		hashContent(RedactedPlaceholder),
		req.PersonaID,
		req.Target.NodeID,
	)
	if err != nil {
		return ForgetResult{}, err
	}

	counts, err := r.removeSearchAndMirrorTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}
	edgeDeleteSeen := map[string]struct{}{}
	edgeDeletes, err := r.enqueueMirrorEdgeDeletesForNodeTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID, edgeDeleteSeen)
	if err != nil {
		return ForgetResult{}, err
	}
	counts.MirrorDeletesEnqueued += edgeDeletes
	dependentCounts, err := r.removeBlockedFactSearchAndMirrorForEpisodeTx(ctx, tx, req.PersonaID, req.Target.NodeID, edgeDeleteSeen)
	if err != nil {
		return ForgetResult{}, err
	}
	counts.SearchDocumentsDeleted += dependentCounts.SearchDocumentsDeleted
	counts.FTSRowsDeleted += dependentCounts.FTSRowsDeleted
	counts.MirrorDeletesEnqueued += dependentCounts.MirrorDeletesEnqueued
	counts.TombstonesWritten = rowsAffected(tombstone)
	return r.completeForgetTx(ctx, tx, req, counts)
}

func (r *ForgetRepository) purgeFactTx(ctx context.Context, tx *sql.Tx, req ForgetRequest) (ForgetResult, error) {
	if err := requireFactExistsTx(ctx, tx, req.PersonaID, req.Target.NodeID); err != nil {
		return ForgetResult{}, err
	}
	_, err := tx.ExecContext(ctx, `
UPDATE facts
SET subject_entity_id = NULL,
    predicate = ?,
    object_entity_id = NULL,
    object_literal = NULL,
    content_summary = ?,
    extraction_reasoning = NULL,
    visibility_status = 'purged',
    searchable = 0,
    pinned = 0,
    pin_reason = NULL,
    pin_actor = NULL,
    pinned_at = NULL,
    updated_at = CURRENT_TIMESTAMP
WHERE persona_id = ? AND id = ?`,
		ForgottenPlaceholder,
		ForgottenPlaceholder,
		req.PersonaID,
		req.Target.NodeID,
	)
	if err != nil {
		return ForgetResult{}, err
	}
	linkResult, err := tx.ExecContext(ctx, `
UPDATE memory_links
SET reasoning = NULL,
    visibility_status = CASE
        WHEN visibility_status = 'visible' THEN 'purged'
        ELSE visibility_status
    END,
    searchable = 0
WHERE persona_id = ?
  AND (
      (from_node_type = 'fact' AND from_node_id = ?)
      OR (to_node_type = 'fact' AND to_node_id = ?)
  )`, req.PersonaID, req.Target.NodeID, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}

	counts, err := r.removeSearchAndMirrorTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}
	edgeDeletes, err := r.enqueueMirrorEdgeDeletesForNodeTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID, nil)
	if err != nil {
		return ForgetResult{}, err
	}
	counts.MirrorDeletesEnqueued += edgeDeletes
	counts.LinksScrubbed = rowsAffected(linkResult)
	return r.completeForgetTx(ctx, tx, req, counts)
}

func (r *ForgetRepository) purgeEpisodeTx(ctx context.Context, tx *sql.Tx, req ForgetRequest) (ForgetResult, error) {
	episode, err := loadEpisodeRedactionAnchorTx(ctx, tx, req.PersonaID, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}
	sourceRefHash := sql.NullString{}
	if episode.SourceRef.Valid {
		sourceRefHash = sql.NullString{String: hashContent(episode.SourceRef.String), Valid: true}
	}
	tombstone, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO episode_tombstones (
    episode_id, persona_id, session_id, occurred_at, redacted_at,
    redaction_level, redaction_actor, redaction_reason_code,
    source_type, source_ref_hash, content_hash_before_redaction
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Target.NodeID,
		req.PersonaID,
		nullableStringValue(episode.SessionID),
		nullableStringValue(episode.OccurredAt),
		formatTime(r.now()),
		req.Level,
		req.Actor,
		req.ReasonCode,
		nullableStringValue(episode.SourceType),
		sourceRefHash,
		nullableStringValue(episode.ContentHash),
	)
	if err != nil {
		return ForgetResult{}, err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE episodes
SET content = ?,
    content_hash = ?,
    source_ref = NULL,
    visibility_status = 'purged',
    searchable = 0
WHERE persona_id = ? AND id = ?`,
		RedactedPlaceholder,
		hashContent(RedactedPlaceholder),
		req.PersonaID,
		req.Target.NodeID,
	)
	if err != nil {
		return ForgetResult{}, err
	}
	linkResult, err := tx.ExecContext(ctx, `
UPDATE memory_links
SET reasoning = NULL,
    visibility_status = CASE
        WHEN visibility_status = 'visible' THEN 'purged'
        ELSE visibility_status
    END,
    searchable = 0
WHERE persona_id = ?
  AND (
      (from_node_type = 'episode' AND from_node_id = ?)
      OR (to_node_type = 'episode' AND to_node_id = ?)
  )`, req.PersonaID, req.Target.NodeID, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}
	counts, err := r.removeSearchAndMirrorTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID)
	if err != nil {
		return ForgetResult{}, err
	}
	edgeDeleteSeen := map[string]struct{}{}
	edgeDeletes, err := r.enqueueMirrorEdgeDeletesForNodeTx(ctx, tx, req.PersonaID, req.Target.NodeType, req.Target.NodeID, edgeDeleteSeen)
	if err != nil {
		return ForgetResult{}, err
	}
	counts.MirrorDeletesEnqueued += edgeDeletes
	dependentCounts, err := r.removeBlockedFactSearchAndMirrorForEpisodeTx(ctx, tx, req.PersonaID, req.Target.NodeID, edgeDeleteSeen)
	if err != nil {
		return ForgetResult{}, err
	}
	counts.SearchDocumentsDeleted += dependentCounts.SearchDocumentsDeleted
	counts.FTSRowsDeleted += dependentCounts.FTSRowsDeleted
	counts.MirrorDeletesEnqueued += dependentCounts.MirrorDeletesEnqueued
	counts.TombstonesWritten = rowsAffected(tombstone)
	counts.LinksScrubbed = rowsAffected(linkResult)
	return r.completeForgetTx(ctx, tx, req, counts)
}

func (r *ForgetRepository) removeBlockedFactSearchAndMirrorForEpisodeTx(ctx context.Context, tx *sql.Tx, personaID string, episodeID string, edgeDeleteSeen map[string]struct{}) (forgetCounts, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT DISTINCT l.from_node_id
FROM memory_links l
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.link_type = 'EVIDENCED_BY'
  AND l.to_node_type = 'episode'
  AND l.to_node_id = ?
  AND NOT EXISTS (
      SELECT 1
      FROM memory_links visible_l
      JOIN episodes e
        ON e.persona_id = visible_l.persona_id
       AND e.id = visible_l.to_node_id
      WHERE visible_l.persona_id = l.persona_id
        AND visible_l.from_node_type = 'fact'
        AND visible_l.from_node_id = l.from_node_id
        AND visible_l.link_type = 'EVIDENCED_BY'
        AND visible_l.to_node_type = 'episode'
        AND e.visibility_status = 'visible'
        AND e.searchable = 1
  )`, personaID, episodeID)
	if err != nil {
		return forgetCounts{}, err
	}
	defer rows.Close()

	var factIDs []string
	for rows.Next() {
		var factID string
		if err := rows.Scan(&factID); err != nil {
			return forgetCounts{}, err
		}
		factIDs = append(factIDs, factID)
	}
	if err := rows.Err(); err != nil {
		return forgetCounts{}, err
	}

	var counts forgetCounts
	for _, factID := range factIDs {
		searchRows, err := countSearchDocumentsTx(ctx, tx, personaID, core.NodeTypeFact, factID)
		if err != nil {
			return forgetCounts{}, err
		}
		ftsRows, err := countSearchFTSRowsTx(ctx, tx, personaID, core.NodeTypeFact, factID)
		if err != nil {
			return forgetCounts{}, err
		}
		if err := deleteSearchDocument(ctx, tx, personaID, core.NodeTypeFact, factID); err != nil {
			return forgetCounts{}, err
		}
		mirrorDeletes, err := r.enqueueMirrorDeleteIfMappedTx(ctx, tx, personaID, core.NodeTypeFact, factID)
		if err != nil {
			return forgetCounts{}, err
		}
		edgeDeletes, err := r.enqueueMirrorEdgeDeletesForNodeTx(ctx, tx, personaID, core.NodeTypeFact, factID, edgeDeleteSeen)
		if err != nil {
			return forgetCounts{}, err
		}
		counts.SearchDocumentsDeleted += searchRows
		counts.FTSRowsDeleted += ftsRows
		counts.MirrorDeletesEnqueued += mirrorDeletes + edgeDeletes
	}
	return counts, nil
}

func (r *ForgetRepository) removeSearchAndMirrorTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType core.NodeType, nodeID string) (forgetCounts, error) {
	searchRows, err := countSearchDocumentsTx(ctx, tx, personaID, nodeType, nodeID)
	if err != nil {
		return forgetCounts{}, err
	}
	ftsRows, err := countSearchFTSRowsTx(ctx, tx, personaID, nodeType, nodeID)
	if err != nil {
		return forgetCounts{}, err
	}
	if err := deleteSearchDocument(ctx, tx, personaID, nodeType, nodeID); err != nil {
		return forgetCounts{}, err
	}
	mirrorDeletes, err := r.enqueueMirrorDeleteIfMappedTx(ctx, tx, personaID, nodeType, nodeID)
	if err != nil {
		return forgetCounts{}, err
	}
	return forgetCounts{
		SearchDocumentsDeleted: searchRows,
		FTSRowsDeleted:         ftsRows,
		MirrorDeletesEnqueued:  mirrorDeletes,
	}, nil
}

func (r *ForgetRepository) completeForgetTx(ctx context.Context, tx *sql.Tx, req ForgetRequest, counts forgetCounts) (ForgetResult, error) {
	eventID, err := r.writeDeletionEventTx(ctx, tx, req, counts)
	if err != nil {
		return ForgetResult{}, err
	}
	return ForgetResult{
		DeletionEventID:        eventID,
		TargetNodeType:         req.Target.NodeType,
		TargetNodeID:           req.Target.NodeID,
		SearchDocumentsDeleted: counts.SearchDocumentsDeleted,
		FTSRowsDeleted:         counts.FTSRowsDeleted,
		MirrorDeletesEnqueued:  counts.MirrorDeletesEnqueued,
		LinksScrubbed:          counts.LinksScrubbed,
	}, nil
}

func (r *ForgetRepository) writeDeletionEventTx(ctx context.Context, tx *sql.Tx, req ForgetRequest, counts forgetCounts) (string, error) {
	eventID := r.newID()
	scopeJSON, err := json.Marshal(map[string]string{"scope_mode": req.Target.ScopeMode})
	if err != nil {
		return "", err
	}
	cascadeJSON, err := json.Marshal(counts)
	if err != nil {
		return "", err
	}
	now := formatTime(r.now())
	_, err = tx.ExecContext(ctx, `
INSERT INTO deletion_events (
    id, persona_id, deletion_level, target_node_type, target_node_id,
    actor, reason_code, scope_json, cascade_summary_json,
    status, created_at, completed_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'completed', ?, ?)`,
		eventID,
		req.PersonaID,
		req.Level,
		string(req.Target.NodeType),
		req.Target.NodeID,
		req.Actor,
		req.ReasonCode,
		string(scopeJSON),
		string(cascadeJSON),
		now,
		now,
	)
	if err != nil {
		return "", err
	}
	return eventID, nil
}

func (r *ForgetRepository) enqueueMirrorDeleteIfMappedTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType core.NodeType, nodeID string) (int64, error) {
	var mapped int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_index_map
WHERE persona_id = ? AND node_type = ? AND node_id = ?`,
		personaID,
		string(nodeType),
		nodeID,
	).Scan(&mapped); err != nil {
		return 0, err
	}
	if mapped == 0 {
		return 0, nil
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (?, ?, ?, ?, 'delete_node')`,
		r.newID(),
		personaID,
		string(nodeType),
		nodeID,
	)
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func (r *ForgetRepository) enqueueMirrorEdgeDeletesForNodeTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType core.NodeType, nodeID string, seen map[string]struct{}) (int64, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id
FROM memory_links
WHERE persona_id = ?
  AND (
      (from_node_type = ? AND from_node_id = ?)
      OR (to_node_type = ? AND to_node_id = ?)
  )
ORDER BY id`,
		personaID,
		string(nodeType),
		nodeID,
		string(nodeType),
		nodeID,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var linkIDs []string
	for rows.Next() {
		var linkID string
		if err := rows.Scan(&linkID); err != nil {
			return 0, err
		}
		linkIDs = append(linkIDs, linkID)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return r.enqueueMirrorEdgeDeletesTx(ctx, tx, personaID, linkIDs, seen)
}

func (r *ForgetRepository) enqueueMirrorEdgeDeletesTx(ctx context.Context, tx *sql.Tx, personaID string, linkIDs []string, seen map[string]struct{}) (int64, error) {
	if seen == nil {
		seen = map[string]struct{}{}
	}
	var enqueued int64
	for _, linkID := range linkIDs {
		if _, ok := seen[linkID]; ok {
			continue
		}
		seen[linkID] = struct{}{}
		_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (?, ?, 'memory_link', ?, 'delete_edge')`,
			r.newID(),
			personaID,
			linkID,
		)
		if err != nil {
			return 0, err
		}
		enqueued++
	}
	return enqueued, nil
}

func validateSQLiteForgetRequest(req ForgetRequest) error {
	if strings.TrimSpace(req.PersonaID) == "" {
		return errors.New("persona_id is required")
	}
	if req.Target.ScopeMode != ForgetScopeExactNode {
		return errors.New("scope_mode must be exact_node")
	}
	if strings.TrimSpace(req.Target.NodeID) == "" {
		return errors.New("node_id is required")
	}
	switch req.Actor {
	case ForgetActorUser, ForgetActorSystem, ForgetActorAdmin:
	default:
		return fmt.Errorf("invalid actor %q", req.Actor)
	}
	switch req.ReasonCode {
	case ForgetReasonUserRequested, ForgetReasonRetentionPolicy, ForgetReasonSafety, ForgetReasonAdminPolicy:
	default:
		return fmt.Errorf("invalid reason_code %q", req.ReasonCode)
	}
	switch req.Level {
	case ForgetLevelSoft, ForgetLevelHard:
		if req.Target.NodeType != core.NodeTypeFact {
			return fmt.Errorf("%s only supports fact targets", req.Level)
		}
	case ForgetLevelSourceRedact:
		if req.Target.NodeType != core.NodeTypeEpisode {
			return errors.New("source_redact only supports episode targets")
		}
	case ForgetLevelPurge:
		if req.Target.NodeType != core.NodeTypeFact && req.Target.NodeType != core.NodeTypeEpisode {
			return errors.New("purge only supports fact or episode targets")
		}
	default:
		return fmt.Errorf("invalid forget level %q", req.Level)
	}
	return nil
}

func requireFactExistsTx(ctx context.Context, tx *sql.Tx, personaID string, factID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM facts
WHERE persona_id = ? AND id = ?`, personaID, factID).Scan(&id)
	return err
}

type episodeRedactionAnchor struct {
	SessionID   string
	OccurredAt  string
	SourceType  string
	SourceRef   sql.NullString
	ContentHash string
}

func loadEpisodeRedactionAnchorTx(ctx context.Context, tx *sql.Tx, personaID string, episodeID string) (episodeRedactionAnchor, error) {
	var anchor episodeRedactionAnchor
	err := tx.QueryRowContext(ctx, `
SELECT session_id, occurred_at, source_type, source_ref, content_hash
FROM episodes
WHERE persona_id = ? AND id = ?`, personaID, episodeID).Scan(
		&anchor.SessionID,
		&anchor.OccurredAt,
		&anchor.SourceType,
		&anchor.SourceRef,
		&anchor.ContentHash,
	)
	return anchor, err
}

func countSearchDocumentsTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType core.NodeType, nodeID string) (int64, error) {
	var count int64
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_search_documents
WHERE persona_id = ? AND node_type = ? AND node_id = ?`,
		personaID,
		string(nodeType),
		nodeID,
	).Scan(&count)
	return count, err
}

func countSearchFTSRowsTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType core.NodeType, nodeID string) (int64, error) {
	ok, err := searchFTSExists(ctx, tx)
	if err != nil || !ok {
		return 0, err
	}
	var count int64
	err = tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_search_fts
WHERE persona_id = ? AND node_type = ? AND node_id = ?`,
		personaID,
		string(nodeType),
		nodeID,
	).Scan(&count)
	return count, err
}

func rowsAffected(result sql.Result) int64 {
	if result == nil {
		return 0
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0
	}
	return rows
}

func nullableStringValue(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
