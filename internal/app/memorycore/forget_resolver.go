package memorycore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"
)

func (s *service) PreviewForget(ctx context.Context, req ForgetPreviewRequest) (*ForgetPreviewResult, error) {
	result, err := s.resolveForgetPreview(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.recordSemanticDecisionAudit(ctx, semanticDecisionAuditRecord{
		RequestID:       result.RequestID,
		PersonaID:       result.PersonaID,
		DecisionType:    "forget_preview",
		Actor:           defaultString(req.Actor, ForgetActorUser),
		ReasonCode:      defaultString(req.RequestedLevel, req.ScopeMode),
		CandidateHash:   forgetPreviewCandidateHash(result.Targets),
		SelectedNodeIDs: exactRefsFromResolvedTargets(result.Targets),
		PreviewHash:     result.PreviewHash,
		PolicySnapshot: map[string]any{
			"scope_mode":           result.ScopeMode,
			"requested_level":      strings.TrimSpace(req.RequestedLevel),
			"require_confirmation": req.RequireConfirmation,
			"risk_flags":           append([]string(nil), result.RiskFlags...),
		},
		SidecarStatus:   result.SidecarStatus,
		DiagnosticsJSON: map[string]any{"status": result.Status},
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *service) resolveForgetPreview(ctx context.Context, req ForgetPreviewRequest) (*ForgetPreviewResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	scope := defaultString(req.ScopeMode, ForgetScopeExactNode)
	result := &ForgetPreviewResult{
		PersonaID:      personaID,
		RequestID:      defaultString(req.RequestID, "forget_preview_"+uuid.NewString()),
		RequestedLevel: strings.TrimSpace(req.RequestedLevel),
		ScopeMode:      scope,
		Status:         "resolved",
		SidecarStatus:  "skipped",
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
	case ForgetScopeSemanticQuery:
		targets, sidecarStatus, err := s.previewSemanticForgetQuery(ctx, personaID, req)
		if err != nil {
			return nil, err
		}
		result.Targets = targets
		result.SidecarStatus = sidecarStatus
		result.RequiresConfirmation = true
		result.Reason = "semantic_query_requires_confirmation"
	default:
		return nil, fmt.Errorf("%w: unsupported forget scope %s", ErrInvalidRequest, scope)
	}
	if len(result.Targets) == 0 {
		result.Status = "no_match"
	}
	if req.RequireConfirmation {
		result.RequiresConfirmation = true
	}
	result.RiskFlags = forgetPreviewRiskFlags(req, *result)
	result.PreviewHash = forgetPreviewHash(*result)
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
	executeLevel := strings.TrimSpace(req.Level)
	if executeLevel == "" {
		executeLevel = strings.TrimSpace(previewReq.RequestedLevel)
	}
	if strings.TrimSpace(previewReq.RequestedLevel) == "" {
		previewReq.RequestedLevel = executeLevel
	}
	if strings.TrimSpace(previewReq.RequestedLevel) == "" {
		return nil, fmt.Errorf("%w: execute requires preview requested level", ErrInvalidRequest)
	}
	if executeLevel != strings.TrimSpace(previewReq.RequestedLevel) {
		return nil, fmt.Errorf("%w: preview_level_mismatch", ErrInvalidRequest)
	}
	preview, err := s.resolveForgetPreview(ctx, previewReq)
	if err != nil {
		return nil, err
	}
	expectedHash := strings.TrimSpace(req.PreviewHash)
	if expectedHash == "" {
		expectedHash = strings.TrimSpace(req.Preview.PreviewHash)
	}
	if expectedHash == "" {
		return nil, fmt.Errorf("%w: execute requires preview hash", ErrInvalidRequest)
	}
	if expectedHash != "" && expectedHash != preview.PreviewHash {
		return nil, fmt.Errorf("%w: preview_changed", ErrInvalidRequest)
	}
	if preview.RequiresConfirmation && !req.Confirmed {
		return nil, fmt.Errorf("%w: forget preview requires confirmation", ErrInvalidRequest)
	}
	targets, err := confirmedForgetTargets(req, *preview)
	if err != nil {
		return nil, err
	}
	actor := defaultString(req.Actor, ForgetActorUser)
	reason := defaultString(req.ReasonCode, ForgetReasonUserRequested)
	result := &ForgetExecuteResult{PersonaID: personaID, PreviewHash: preview.PreviewHash}
	for _, target := range targets {
		forget, err := s.Forget(ctx, ForgetRequest{
			PersonaID:  personaID,
			Actor:      actor,
			ReasonCode: reason,
			Level:      executeLevel,
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
	if err := s.recordSemanticDecisionAudit(ctx, semanticDecisionAuditRecord{
		RequestID:       defaultString(previewReq.RequestID, preview.RequestID),
		PersonaID:       personaID,
		DecisionType:    "forget_execute",
		Actor:           actor,
		ReasonCode:      reason,
		CandidateHash:   forgetPreviewCandidateHash(preview.Targets),
		SelectedNodeIDs: exactRefsFromResolvedTargets(targets),
		PreviewHash:     preview.PreviewHash,
		PolicySnapshot: map[string]any{
			"level":           executeLevel,
			"requested_level": strings.TrimSpace(preview.RequestedLevel),
			"scope_mode":      preview.ScopeMode,
		},
		SidecarStatus:   preview.SidecarStatus,
		DiagnosticsJSON: map[string]any{"executed": result.Executed},
	}); err != nil {
		return result, err
	}
	return result, nil
}

func confirmedForgetTargets(req ForgetExecuteRequest, preview ForgetPreviewResult) ([]ForgetResolvedTarget, error) {
	if len(req.ConfirmedTargets) == 0 {
		return nil, fmt.Errorf("%w: execute requires confirmed exact targets", ErrInvalidRequest)
	}
	allowed := map[string]ForgetResolvedTarget{}
	for _, target := range preview.Targets {
		if strings.TrimSpace(target.NodeType) == "" || strings.TrimSpace(target.NodeID) == "" {
			continue
		}
		allowed[exactNodeKey(target.NodeType, target.NodeID)] = target
	}
	selected := make([]ForgetResolvedTarget, 0, len(req.ConfirmedTargets))
	seen := map[string]struct{}{}
	for _, target := range req.ConfirmedTargets {
		nodeType := strings.TrimSpace(target.NodeType)
		nodeID := strings.TrimSpace(target.NodeID)
		if nodeType == "" || nodeID == "" {
			return nil, fmt.Errorf("%w: confirmed target requires node type and node id", ErrInvalidRequest)
		}
		key := exactNodeKey(nodeType, nodeID)
		if _, ok := seen[key]; ok {
			continue
		}
		resolved, ok := allowed[key]
		if !ok {
			return nil, fmt.Errorf("%w: confirmed target is not in preview", ErrInvalidRequest)
		}
		seen[key] = struct{}{}
		selected = append(selected, resolved)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: execute requires confirmed exact targets", ErrInvalidRequest)
	}
	return selected, nil
}

func exactNodeKey(nodeType string, nodeID string) string {
	return strings.TrimSpace(nodeType) + "\x1f" + strings.TrimSpace(nodeID)
}

func forgetPreviewHash(preview ForgetPreviewResult) string {
	payload := map[string]any{
		"persona_id":      preview.PersonaID,
		"requested_level": strings.TrimSpace(preview.RequestedLevel),
		"risk_flags":      sortedStrings(preview.RiskFlags),
		"scope_mode":      preview.ScopeMode,
		"targets":         exactRefsFromResolvedTargets(preview.Targets),
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func sortedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func forgetPreviewCandidateHash(targets []ForgetResolvedTarget) string {
	data, _ := json.Marshal(exactRefsFromResolvedTargets(targets))
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func exactRefsFromResolvedTargets(targets []ForgetResolvedTarget) []ExactNodeRef {
	refs := make([]ExactNodeRef, 0, len(targets))
	for _, target := range targets {
		nodeType := strings.TrimSpace(target.NodeType)
		nodeID := strings.TrimSpace(target.NodeID)
		if nodeType == "" || nodeID == "" {
			continue
		}
		refs = append(refs, ExactNodeRef{NodeType: nodeType, NodeID: nodeID})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].NodeType == refs[j].NodeType {
			return refs[i].NodeID < refs[j].NodeID
		}
		return refs[i].NodeType < refs[j].NodeType
	})
	return refs
}

func forgetPreviewRiskFlags(req ForgetPreviewRequest, result ForgetPreviewResult) []string {
	flags := map[string]struct{}{}
	switch result.ScopeMode {
	case ForgetScopeBroadTopic:
		flags["broad_topic"] = struct{}{}
	case ForgetScopeEntity:
		flags["entity_scope"] = struct{}{}
	case ForgetScopeSemanticQuery:
		flags["semantic_query"] = struct{}{}
	case ForgetScopeRecentEpisodeWindow:
		flags["recent_episode_window"] = struct{}{}
	}
	if len(result.Targets) > 1 {
		flags["batch"] = struct{}{}
	}
	if strings.TrimSpace(req.RequestedLevel) == ForgetLevelPurge {
		flags["purge"] = struct{}{}
	}
	out := make([]string, 0, len(flags))
	for flag := range flags {
		out = append(out, flag)
	}
	sort.Strings(out)
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

func (s *service) previewSemanticForgetQuery(ctx context.Context, personaID string, req ForgetPreviewRequest) ([]ForgetResolvedTarget, string, error) {
	query := strings.TrimSpace(derefString(req.SemanticQuery))
	if query == "" {
		query = strings.TrimSpace(req.Topic)
	}
	if query == "" {
		return nil, "skipped", fmt.Errorf("%w: semantic_query requires query text", ErrInvalidRequest)
	}
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	targets := []ForgetResolvedTarget{}
	sidecarStatus := "skipped"
	if s.semanticOps.Forget.PreviewEnabled {
		if adapter, ok := s.mirrorAdapter.(MirrorDeleteCandidatesAdapter); ok {
			result, err := adapter.DeleteCandidates(ctx, MirrorDeleteCandidatesRequest{
				RequestID: req.RequestID,
				PersonaID: personaID,
				Intent: MirrorDeleteCandidateIntent{
					RawText:             query,
					OperationPurpose:    "forget_delete",
					OperationTargetOnly: true,
				},
				Scope: MirrorDeleteCandidateScope{
					SessionID: strings.TrimSpace(req.SessionID),
				},
				Policy: MirrorDeleteCandidatePolicy{
					Limit:                  limit,
					AllowEpisodeCandidates: true,
					AllowFactCandidates:    true,
					IncludeSafeSummary:     true,
				},
			})
			switch {
			case err != nil:
				sidecarStatus = "failed"
			case result.Degraded:
				sidecarStatus = "degraded"
			default:
				sidecarStatus = "ok"
			}
			if err == nil {
				targets = s.authorityFilterForgetCandidates(ctx, personaID, result.Candidates)
			}
		}
	}
	if len(targets) > 0 {
		return targets, sidecarStatus, nil
	}
	fallback, err := s.previewSemanticSQLiteFallback(ctx, personaID, query, limit)
	if err != nil {
		return nil, sidecarStatus, err
	}
	return fallback, sidecarStatus, nil
}

func (s *service) authorityFilterForgetCandidates(ctx context.Context, personaID string, candidates []MirrorDeleteCandidate) []ForgetResolvedTarget {
	targets := make([]ForgetResolvedTarget, 0, len(candidates))
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		nodeType := strings.TrimSpace(candidate.NodeType)
		nodeID := strings.TrimSpace(candidate.NodeID)
		if nodeType == "" || nodeID == "" {
			continue
		}
		if _, ok := seen[exactNodeKey(nodeType, nodeID)]; ok {
			continue
		}
		target, ok := s.previewAuthorityForgetTarget(ctx, personaID, nodeType, nodeID, candidate.SafeSummary)
		if !ok {
			continue
		}
		targets = append(targets, target)
		seen[exactNodeKey(nodeType, nodeID)] = struct{}{}
	}
	return targets
}

func (s *service) previewAuthorityForgetTarget(ctx context.Context, personaID string, nodeType string, nodeID string, safeSummary string) (ForgetResolvedTarget, bool) {
	var exists int
	var err error
	switch nodeType {
	case ForgetNodeFact:
		err = s.sqlDB.QueryRowContext(ctx, `
SELECT 1
FROM facts
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND validity_status = 'valid'
  AND lifecycle_status = 'active'`, personaID, nodeID).Scan(&exists)
	case ForgetNodeEpisode:
		err = s.sqlDB.QueryRowContext(ctx, `
SELECT 1
FROM episodes
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`, personaID, nodeID).Scan(&exists)
	default:
		return ForgetResolvedTarget{}, false
	}
	if err != nil {
		return ForgetResolvedTarget{}, false
	}
	target := safeForgetTarget(nodeType, nodeID)
	if strings.TrimSpace(safeSummary) != "" {
		target.Summary = strings.TrimSpace(safeSummary)
		target.SafeSummary = strings.TrimSpace(safeSummary)
	}
	return target, true
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

func (s *service) previewSemanticSQLiteFallback(ctx context.Context, personaID string, queryText string, limit int) ([]ForgetResolvedTarget, error) {
	patterns := semanticForgetFallbackPatterns(queryText)
	if len(patterns) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	clauses := make([]string, 0, len(patterns))
	args := []any{personaID}
	for _, pattern := range patterns {
		clauses = append(clauses, "content_summary LIKE ?")
		args = append(args, "%"+pattern+"%")
	}
	args = append(args, limit)
	rows, err := s.sqlDB.QueryContext(ctx, `
SELECT id, content_summary
FROM facts
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND validity_status = 'valid'
  AND lifecycle_status = 'active'
  AND (`+strings.Join(clauses, " OR ")+`)
ORDER BY updated_at DESC, created_at DESC
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanForgetFactSummaryRows(rows)
}

func semanticForgetFallbackPatterns(queryText string) []string {
	base := normalizeSemanticForgetFallbackQuery(queryText)
	if base == "" {
		return nil
	}
	var patterns []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if !usefulSemanticForgetFallbackPattern(value) {
			return
		}
		for _, existing := range patterns {
			if existing == value {
				return
			}
		}
		patterns = append(patterns, value)
	}
	add(base)
	add(normalizeUserPronounForForgetFallback(base))
	return patterns
}

func normalizeSemanticForgetFallbackQuery(value string) string {
	value = strings.Trim(strings.TrimSpace(value), " \t\r\n。！？!?.,，；;：:\"'“”‘’")
	if value == "" {
		return ""
	}
	for _, token := range []string{"不要再提", "别再提", "忘记", "忘掉", "删除"} {
		value = strings.ReplaceAll(value, token, "")
	}
	return strings.Trim(strings.TrimSpace(value), " \t\r\n。！？!?.,，；;：:\"'“”‘’")
}

func normalizeUserPronounForForgetFallback(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "我的", "用户的")
	value = strings.ReplaceAll(value, "我", "用户")
	return strings.TrimSpace(value)
}

func usefulSemanticForgetFallbackPattern(value string) bool {
	switch strings.TrimSpace(value) {
	case "", "我", "我的", "用户", "用户的":
		return false
	default:
		return true
	}
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

func scanForgetFactSummaryRows(rows *sql.Rows) ([]ForgetResolvedTarget, error) {
	var targets []ForgetResolvedTarget
	for rows.Next() {
		var nodeID string
		var safeSummary string
		if err := rows.Scan(&nodeID, &safeSummary); err != nil {
			return nil, err
		}
		target := safeForgetTarget(ForgetNodeFact, nodeID)
		safeSummary = strings.TrimSpace(safeSummary)
		if safeSummary != "" {
			target.Summary = safeSummary
			target.SafeSummary = safeSummary
		}
		targets = append(targets, target)
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

type semanticDecisionAuditRecord struct {
	RequestID        string
	PersonaID        string
	DecisionType     string
	Actor            string
	ReasonCode       string
	CandidateHash    string
	SelectedNodeIDs  []ExactNodeRef
	PreviewHash      string
	PolicySnapshot   map[string]any
	SimilarityScores map[string]any
	SidecarStatus    string
	DiagnosticsJSON  map[string]any
}

func (s *service) recordSemanticDecisionAudit(ctx context.Context, rec semanticDecisionAuditRecord) error {
	if s == nil || s.sqlDB == nil {
		return nil
	}
	exists, err := forgetTableExists(ctx, s.sqlDB, "semantic_decision_audit")
	if err != nil || !exists {
		return err
	}
	selected, err := json.Marshal(rec.SelectedNodeIDs)
	if err != nil {
		return err
	}
	policy, err := nullableJSON(rec.PolicySnapshot)
	if err != nil {
		return err
	}
	scores, err := nullableJSON(rec.SimilarityScores)
	if err != nil {
		return err
	}
	diagnostics, err := nullableJSON(rec.DiagnosticsJSON)
	if err != nil {
		return err
	}
	_, err = s.sqlDB.ExecContext(ctx, `
INSERT INTO semantic_decision_audit (
    id, request_id, persona_id, decision_type, actor, reason_code,
    candidate_hash, selected_node_ids, preview_hash, policy_snapshot,
    similarity_scores, sidecar_status, diagnostics_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"semantic_audit_"+uuid.NewString(),
		defaultString(rec.RequestID, "semantic_"+uuid.NewString()),
		defaultString(rec.PersonaID, s.persona),
		strings.TrimSpace(rec.DecisionType),
		defaultString(rec.Actor, ForgetActorSystem),
		semanticNullableString(rec.ReasonCode),
		semanticNullableString(rec.CandidateHash),
		string(selected),
		semanticNullableString(rec.PreviewHash),
		policy,
		scores,
		defaultString(rec.SidecarStatus, "skipped"),
		diagnostics,
	)
	return err
}

func nullableJSON(value map[string]any) (any, error) {
	if len(value) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func semanticNullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
