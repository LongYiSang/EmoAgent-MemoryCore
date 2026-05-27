package memorycore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *service) PreviewForget(ctx context.Context, req ForgetPreviewRequest) (*ForgetPreviewResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	scope := defaultString(req.ScopeMode, ForgetScopeExactNode)
	result := &ForgetPreviewResult{
		PersonaID: personaID,
		ScopeMode: scope,
		Status:    "resolved",
	}
	switch scope {
	case ForgetScopeExactNode:
		target, err := s.previewExactForgetTarget(ctx, personaID, req.NodeType, req.NodeID)
		if err != nil {
			return nil, err
		}
		result.Targets = []ForgetResolvedTarget{target}
	case ForgetScopeRecentPromptItem:
		target, err := previewRecentPromptItem(req)
		if err != nil {
			return nil, err
		}
		result.Targets = []ForgetResolvedTarget{target}
	case ForgetScopeRecentEpisodeWindow:
		targets, err := s.previewRecentEpisodeWindow(ctx, personaID, req)
		if err != nil {
			return nil, err
		}
		result.Targets = targets
		result.RequiresConfirmation = true
		result.Reason = "recent_episode_window_requires_confirmation"
	case ForgetScopeEntity:
		targets, err := s.previewEntityScope(ctx, personaID, req.EntityID, req.Limit)
		if err != nil {
			return nil, err
		}
		result.Targets = targets
		result.RequiresConfirmation = true
		result.Reason = "entity_scope_requires_confirmation"
	case ForgetScopeBroadTopic:
		targets, err := s.previewBroadTopic(ctx, personaID, req.Topic, req.Limit)
		if err != nil {
			return nil, err
		}
		result.Targets = targets
		result.RequiresConfirmation = true
		result.Reason = "broad_topic_requires_confirmation"
	default:
		return nil, fmt.Errorf("%w: unsupported forget scope %s", ErrInvalidRequest, scope)
	}
	if len(result.Targets) == 0 {
		result.Status = "no_match"
	}
	return result, nil
}

func (s *service) ExecuteForget(ctx context.Context, req ForgetExecuteRequest) (*ForgetExecuteResult, error) {
	previewReq := req.PreviewRequest
	if strings.TrimSpace(previewReq.ScopeMode) == "" {
		return nil, fmt.Errorf("%w: execute requires preview request", ErrInvalidRequest)
	}
	personaID := defaultString(req.PersonaID, previewReq.PersonaID)
	personaID = defaultString(personaID, s.persona)
	previewReq.PersonaID = personaID
	preview, err := s.PreviewForget(ctx, previewReq)
	if err != nil {
		return nil, err
	}
	if req.Preview.ScopeMode != "" && !sameForgetPreview(req.Preview, *preview) {
		return nil, fmt.Errorf("%w: forget preview changed", ErrInvalidRequest)
	}
	if preview.ScopeMode == ForgetScopeBroadTopic {
		return nil, fmt.Errorf("%w: broad_topic preview is not executable", ErrInvalidRequest)
	}
	if preview.RequiresConfirmation && !req.Confirmed {
		return nil, fmt.Errorf("%w: forget preview requires confirmation", ErrInvalidRequest)
	}
	actor := defaultString(req.Actor, ForgetActorUser)
	reason := defaultString(req.ReasonCode, ForgetReasonUserRequested)
	result := &ForgetExecuteResult{PersonaID: personaID}
	for _, target := range preview.Targets {
		forget, err := s.Forget(ctx, ForgetRequest{
			PersonaID:  personaID,
			Actor:      actor,
			ReasonCode: reason,
			Level:      req.Level,
			Target: ForgetTarget{
				ScopeMode: ForgetScopeExactNode,
				NodeType:  target.NodeType,
				NodeID:    target.NodeID,
			},
		})
		if err != nil {
			return nil, err
		}
		result.Executed++
		result.Results = append(result.Results, *forget)
	}
	return result, nil
}

func sameForgetPreview(left ForgetPreviewResult, right ForgetPreviewResult) bool {
	if left.PersonaID != "" && left.PersonaID != right.PersonaID {
		return false
	}
	if left.ScopeMode != right.ScopeMode || left.RequiresConfirmation != right.RequiresConfirmation || len(left.Targets) != len(right.Targets) {
		return false
	}
	leftTargets := forgetPreviewTargetSet(left.Targets)
	rightTargets := forgetPreviewTargetSet(right.Targets)
	if len(leftTargets) != len(rightTargets) {
		return false
	}
	for key := range leftTargets {
		if _, ok := rightTargets[key]; !ok {
			return false
		}
	}
	return true
}

func forgetPreviewTargetSet(targets []ForgetResolvedTarget) map[string]struct{} {
	out := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		key := strings.TrimSpace(target.NodeType) + "\x1f" + strings.TrimSpace(target.NodeID)
		if strings.TrimSpace(target.NodeType) != "" && strings.TrimSpace(target.NodeID) != "" {
			out[key] = struct{}{}
		}
	}
	return out
}

func (s *service) VerifyForget(ctx context.Context, req ForgetVerifyRequest) (*ForgetVerifyResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	result := &ForgetVerifyResult{PersonaID: personaID, Passed: true}
	for _, target := range req.Targets {
		item, err := s.verifyForgetTarget(ctx, personaID, target)
		if err != nil {
			return nil, err
		}
		if !item.Passed {
			result.Passed = false
		}
		result.Targets = append(result.Targets, item)
	}
	return result, nil
}

func (s *service) previewExactForgetTarget(ctx context.Context, personaID string, nodeType string, nodeID string) (ForgetResolvedTarget, error) {
	nodeType = strings.TrimSpace(nodeType)
	nodeID = strings.TrimSpace(nodeID)
	if nodeType == "" || nodeID == "" {
		return ForgetResolvedTarget{}, fmt.Errorf("%w: exact_node requires node type and node id", ErrInvalidRequest)
	}
	switch nodeType {
	case ForgetNodeFact:
		var exists int
		err := s.sqlDB.QueryRowContext(ctx, `
SELECT 1
FROM facts
WHERE persona_id = ? AND id = ?`, personaID, nodeID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ForgetResolvedTarget{}, fmt.Errorf("%w: fact %s", ErrNotFound, nodeID)
		}
		if err != nil {
			return ForgetResolvedTarget{}, err
		}
		return safeForgetTarget(nodeType, nodeID), nil
	case ForgetNodeEpisode:
		var exists int
		err := s.sqlDB.QueryRowContext(ctx, `
SELECT 1
FROM episodes
WHERE persona_id = ? AND id = ?`, personaID, nodeID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ForgetResolvedTarget{}, fmt.Errorf("%w: episode %s", ErrNotFound, nodeID)
		}
		if err != nil {
			return ForgetResolvedTarget{}, err
		}
		return safeForgetTarget(nodeType, nodeID), nil
	default:
		return ForgetResolvedTarget{}, fmt.Errorf("%w: unsupported node type %s", ErrInvalidRequest, nodeType)
	}
}

func previewRecentPromptItem(req ForgetPreviewRequest) (ForgetResolvedTarget, error) {
	nodeID := strings.TrimSpace(req.NodeID)
	nodeType := strings.TrimSpace(req.NodeType)
	for _, item := range req.RecentPromptItems {
		if item.NodeID != nodeID {
			continue
		}
		if nodeType != "" && item.NodeType != nodeType {
			continue
		}
		return safeForgetTarget(item.NodeType, item.NodeID), nil
	}
	return ForgetResolvedTarget{}, fmt.Errorf("%w: recent prompt item not found", ErrNotFound)
}

func (s *service) previewRecentEpisodeWindow(ctx context.Context, personaID string, req ForgetPreviewRequest) ([]ForgetResolvedTarget, error) {
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	query := `
SELECT id
FROM episodes
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`
	args := []any{personaID}
	if strings.TrimSpace(req.SessionID) != "" {
		query += ` AND session_id = ?`
		args = append(args, strings.TrimSpace(req.SessionID))
	}
	if req.Since != nil && !req.Since.IsZero() {
		query += ` AND occurred_at >= ?`
		args = append(args, req.Since.UTC().Format(time.RFC3339Nano))
	}
	if req.Until != nil && !req.Until.IsZero() {
		query += ` AND occurred_at <= ?`
		args = append(args, req.Until.UTC().Format(time.RFC3339Nano))
	}
	query += ` ORDER BY occurred_at DESC, ingested_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.sqlDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanForgetTargetIDs(rows, ForgetNodeEpisode)
}

func (s *service) previewEntityScope(ctx context.Context, personaID string, entityID string, limit int) ([]ForgetResolvedTarget, error) {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return nil, fmt.Errorf("%w: entity_scope requires entity id", ErrInvalidRequest)
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
SELECT id
FROM facts
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND (subject_entity_id = ? OR object_entity_id = ?)
ORDER BY updated_at DESC, created_at DESC
LIMIT ?`, personaID, entityID, entityID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanForgetTargetIDs(rows, ForgetNodeFact)
}

func (s *service) previewBroadTopic(ctx context.Context, personaID string, topic string, limit int) ([]ForgetResolvedTarget, error) {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return nil, fmt.Errorf("%w: broad_topic requires topic", ErrInvalidRequest)
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
SELECT id
FROM facts
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND content_summary LIKE ?
ORDER BY updated_at DESC, created_at DESC
LIMIT ?`, personaID, "%"+topic+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanForgetTargetIDs(rows, ForgetNodeFact)
}

func scanForgetTargetIDs(rows *sql.Rows, nodeType string) ([]ForgetResolvedTarget, error) {
	var targets []ForgetResolvedTarget
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return nil, err
		}
		targets = append(targets, safeForgetTarget(nodeType, nodeID))
	}
	return targets, rows.Err()
}

func safeForgetTarget(nodeType string, nodeID string) ForgetResolvedTarget {
	summary := safeForgetSummary(nodeType, nodeID)
	return ForgetResolvedTarget{
		NodeType:    nodeType,
		NodeID:      nodeID,
		Summary:     summary,
		SafeSummary: summary,
	}
}

func safeForgetSummary(nodeType string, nodeID string) string {
	return nodeType + ":" + nodeID
}

func (s *service) verifyForgetTarget(ctx context.Context, personaID string, target ForgetResolvedTarget) (ForgetVerifyTargetResult, error) {
	result := ForgetVerifyTargetResult{NodeType: target.NodeType, NodeID: target.NodeID}
	query, err := forgetVerifyNodeQuery(target.NodeType)
	if err != nil {
		return ForgetVerifyTargetResult{}, err
	}
	var searchable int
	err = s.sqlDB.QueryRowContext(ctx, query, personaID, target.NodeID).Scan(&result.VisibilityStatus, &searchable)
	if errors.Is(err, sql.ErrNoRows) {
		result.Passed = true
		return result, nil
	}
	if err != nil {
		return ForgetVerifyTargetResult{}, err
	}
	result.Searchable = searchable == 1
	result.SearchDocumentsFound, err = countForgetSearchRows(ctx, s.sqlDB, "memory_search_documents", personaID, target.NodeType, target.NodeID)
	if err != nil {
		return ForgetVerifyTargetResult{}, err
	}
	result.FTSRowsFound, err = countForgetSearchRows(ctx, s.sqlDB, "memory_search_fts", personaID, target.NodeType, target.NodeID)
	if err != nil {
		return ForgetVerifyTargetResult{}, err
	}
	result.Passed = result.VisibilityStatus != VisibilityVisible &&
		!result.Searchable &&
		result.SearchDocumentsFound == 0 &&
		result.FTSRowsFound == 0
	return result, nil
}

func forgetVerifyNodeQuery(nodeType string) (string, error) {
	switch nodeType {
	case ForgetNodeFact:
		return `SELECT visibility_status, searchable FROM facts WHERE persona_id = ? AND id = ?`, nil
	case ForgetNodeEpisode:
		return `SELECT visibility_status, searchable FROM episodes WHERE persona_id = ? AND id = ?`, nil
	default:
		return "", fmt.Errorf("%w: unsupported node type %s", ErrInvalidRequest, nodeType)
	}
}

func countForgetSearchRows(ctx context.Context, db *sql.DB, table string, personaID string, nodeType string, nodeID string) (int, error) {
	exists, err := forgetTableExists(ctx, db, table)
	if err != nil || !exists {
		return 0, err
	}
	var count int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE persona_id = ? AND node_type = ? AND node_id = ?", personaID, nodeType, nodeID).Scan(&count)
	return count, err
}

func forgetTableExists(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_master
WHERE name = ?`, table).Scan(&count)
	return count > 0, err
}
