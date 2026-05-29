package memorycore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const manualForgetPendingTTL = 24 * time.Hour

var broadForgetTopicPattern = regexp.MustCompile(`(?i)(?:关于|about)\s*([^。.!?？]+)`)
var relatedForgetTopicPattern = regexp.MustCompile(`(?i)(?:跟|和)\s*([^。.!?？]+?)\s*(?:相关|有关)`)

func (s *service) AnalyzeManualMemoryDirective(ctx context.Context, req ManualForgetDirectiveRequest) (*ManualForgetDirectiveResult, error) {
	if llmResult, ok := s.analyzeManualMemoryDirectiveWithLLM(ctx, req); ok {
		return llmResult, nil
	}
	return analyzeManualMemoryDirectiveFallback(req), nil
}

func analyzeManualMemoryDirectiveFallback(req ManualForgetDirectiveRequest) *ManualForgetDirectiveResult {
	text := strings.TrimSpace(req.UserText)
	if text == "" {
		return &ManualForgetDirectiveResult{Intent: ManualForgetIntentNone, Confidence: 0}
	}
	lower := strings.ToLower(text)
	result := &ManualForgetDirectiveResult{
		Intent:      ManualForgetIntentNone,
		Confidence:  0.2,
		ReasonCodes: []string{"fallback_rules"},
	}
	if req.RuleHint != nil && strings.TrimSpace(req.RuleHint.Kind) == ManualRuleHintForget {
		result.Intent = ManualForgetIntentForget
		result.Confidence = 0.72
		result.ReasonCodes = append(result.ReasonCodes, "rule_hint_forget")
	}
	if manualForgetContainsAny(lower, "忘记", "忘掉", "别再提", "不要再提", "删", "删除", "remove", "delete", "forget") ||
		manualForgetContainsAny(text, "别记", "不要记", "不要保留原文", "别长期保留") {
		result.Intent = ManualForgetIntentForget
		if result.Confidence < 0.78 {
			result.Confidence = 0.78
		}
	}
	if result.Intent != ManualForgetIntentForget {
		return result
	}

	result.ForgetLevelHint = ForgetLevelSoft
	result.TargetNodeTypeHint = ForgetNodeFact
	switch {
	case manualForgetContainsAny(text, "彻底删除", "全部删除", "所有记忆", "都删", "purge"):
		result.ForgetLevelHint = ForgetLevelPurge
		result.RequiresLLMConfirm = true
		result.Confidence = maxFloat(result.Confidence, 0.86)
		result.ReasonCodes = append(result.ReasonCodes, "broad_or_purge_language")
	case manualForgetContainsAny(text, "不要保留原文", "别保留原文", "不要留原文", "刚才那段", "source redact"):
		result.ForgetLevelHint = ForgetLevelSourceRedact
		result.TargetNodeTypeHint = ForgetNodeEpisode
		result.Confidence = maxFloat(result.Confidence, 0.86)
		result.ReasonCodes = append(result.ReasonCodes, "source_redact_language")
	case manualForgetContainsAny(text, "别记", "不要记", "不要保存", "别保存", "不想让你记"):
		result.ForgetLevelHint = ForgetLevelHard
		result.Confidence = maxFloat(result.Confidence, 0.84)
		result.ReasonCodes = append(result.ReasonCodes, "hard_forget_language")
	case manualForgetContainsAny(text, "别再提", "不要再提", "不再主动提", "以后别提"):
		result.ForgetLevelHint = ForgetLevelSoft
		result.Confidence = maxFloat(result.Confidence, 0.84)
		result.ReasonCodes = append(result.ReasonCodes, "soft_forget_language")
	}
	result.TargetDescription = safeDirectiveTargetDescription(text)
	return result
}

func (s *service) analyzeManualMemoryDirectiveWithLLM(ctx context.Context, req ManualForgetDirectiveRequest) (*ManualForgetDirectiveResult, bool) {
	llm, provider, ok := s.newMemoryOperationLLM()
	if !ok {
		return nil, false
	}
	payload := map[string]any{
		"schema_version":     "memory_operation_directive_request.v0.1",
		"persona_id":         defaultString(req.PersonaID, s.persona),
		"session_id":         req.SessionID,
		"request_episode_id": req.RequestEpisodeID,
		"user_text":          req.UserText,
		"rule_hint":          req.RuleHint,
		"recent_prompt_refs": len(req.RecentPromptRefs),
	}
	body, _ := json.Marshal(payload)
	resp, err := llm.CompleteJSON(ctx, ExtractionLLMRequest{
		Purpose:         MemoryOperationLLMPurposeDirective,
		ProviderID:      provider.ID,
		ProviderKind:    provider.Kind,
		Model:           provider.Model,
		SystemPrompt:    memoryOperationLLMSystemPrompt(),
		DeveloperPrompt: memoryOperationDirectiveDeveloperPrompt(),
		UserPrompt:      string(body),
		Temperature:     0,
		MaxTokens:       900,
		Timeout:         provider.Timeout,
		ResponseFormat:  ExtractionResponseFormatJSONObject,
		Metadata:        requestMetadata(MemoryOperationLLMPurposeDirective, "", "manual_forget_v1", MemoryOperationContextSchemaVersion),
	})
	if err != nil {
		return nil, false
	}
	var out ManualForgetDirectiveResult
	if err := json.Unmarshal([]byte(resp.Text), &out); err != nil {
		return nil, false
	}
	if !validManualForgetIntent(out.Intent) {
		return nil, false
	}
	out.Intent = strings.TrimSpace(out.Intent)
	out.ForgetLevelHint = normalizeManualForgetLevel(out.ForgetLevelHint)
	if out.Confidence <= 0 {
		out.Confidence = 0.5
	}
	out.ReasonCodes = append(out.ReasonCodes, "operation_llm")
	if out.Intent != ManualForgetIntentForget {
		if analyzeManualMemoryDirectiveFallback(req).Intent == ManualForgetIntentForget {
			return nil, false
		}
		return &out, true
	}
	if out.ForgetLevelHint == "" {
		out.ForgetLevelHint = ForgetLevelSoft
	}
	if strings.TrimSpace(out.TargetDescription) == "" {
		out.TargetDescription = safeDirectiveTargetDescription(req.UserText)
	}
	if strings.TrimSpace(out.TargetNodeTypeHint) == "" {
		out.TargetNodeTypeHint = ForgetNodeFact
	}
	return &out, true
}

func (s *service) PlanManualForget(ctx context.Context, req PlanManualForgetRequest) (*PlanManualForgetResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	level := normalizeManualForgetLevel(req.Directive.ForgetLevelHint)
	if level == "" {
		level = ForgetLevelSoft
	}
	if req.Directive.Intent != ManualForgetIntentForget {
		return &PlanManualForgetResult{
			Status:                 ManualForgetStatusIgnored,
			SuppressOrdinaryMemory: false,
		}, nil
	}

	candidates, scope, err := s.resolveManualForgetCandidates(ctx, personaID, req, level)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return &PlanManualForgetResult{
			Status:                 ManualForgetStatusNoMatch,
			RecommendedAction:      ManualForgetActionNoMatchReply,
			RequestedLevel:         level,
			SuppressOrdinaryMemory: true,
			ResultContext:          noMatchMemoryOperationContext(level),
		}, nil
	}
	candidates = capManualForgetCandidates(candidates, 5)
	requiresConfirmation := manualForgetRequiresConfirmation(level, candidates, req.Directive)
	status := ManualForgetStatusExecutable
	action := ManualForgetActionAutoExecute
	if requiresConfirmation {
		status = ManualForgetStatusNeedsConfirmation
		action = ManualForgetActionAskLLMConfirmation
	}
	operationID := "mfo_" + uuid.NewString()
	contextBlock := manualForgetOperationContext(status, operationID, level, requiresConfirmation, candidates, nil, nil)
	operation := PendingManualForgetOperation{
		ID:                   operationID,
		PersonaID:            personaID,
		SessionID:            cloneStringPtr(req.SessionID),
		ChatSessionID:        strings.TrimSpace(req.ChatSessionID),
		RequestEpisodeID:     strings.TrimSpace(req.RequestEpisodeID),
		Status:               ManualForgetStatusPendingConfirmation,
		RequestedLevel:       level,
		ScopeMode:            scope,
		RequiresConfirmation: requiresConfirmation,
		Candidates:           candidates,
		CreatedAt:            manualForgetNow(s, req.Now),
		UpdatedAt:            manualForgetNow(s, req.Now),
		ExpiresAt:            manualForgetNow(s, req.Now).Add(manualForgetPendingTTL),
	}
	if err := s.insertPendingManualForgetOperation(ctx, operation); err != nil {
		return nil, err
	}
	result := &PlanManualForgetResult{
		Status:                 status,
		OperationID:            operationID,
		RecommendedAction:      action,
		RequestedLevel:         level,
		RequiresConfirmation:   requiresConfirmation,
		SuppressOrdinaryMemory: true,
		Candidates:             candidates,
		SafeSummary:            safePlanSummary(level, len(candidates)),
	}
	if requiresConfirmation {
		result.ConfirmationContext = contextBlock
	} else {
		result.ResultContext = contextBlock
	}
	return result, nil
}

func (s *service) GetPendingManualForgetOperation(ctx context.Context, req GetPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	now := manualForgetNow(s, req.Now)
	query := `
SELECT id, persona_id, session_id, chat_session_id, request_episode_id, status,
       requested_level, scope_mode, requires_confirmation, candidates_json,
       created_at, updated_at, expires_at
FROM pending_manual_forget_operations
WHERE persona_id = ?
  AND status = ?
  AND expires_at > ?`
	args := []any{personaID, ManualForgetStatusPendingConfirmation, formatManualForgetTime(now)}
	if req.SessionID != nil && strings.TrimSpace(*req.SessionID) != "" {
		query += ` AND session_id = ?`
		args = append(args, strings.TrimSpace(*req.SessionID))
	}
	if strings.TrimSpace(req.ChatSessionID) != "" {
		query += ` AND chat_session_id = ?`
		args = append(args, strings.TrimSpace(req.ChatSessionID))
	}
	query += ` ORDER BY updated_at DESC, created_at DESC LIMIT 1`
	op, err := s.scanPendingManualForgetOperation(s.sqlDB.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (s *service) ClassifyForgetConfirmation(ctx context.Context, req ClassifyForgetConfirmationRequest) (*ClassifyForgetConfirmationResult, error) {
	op, err := s.loadManualForgetOperationForConfirmation(ctx, req)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, fmt.Errorf("%w: pending manual forget operation", ErrNotFound)
	}
	reply := strings.TrimSpace(req.UserReply)
	if llmResult, ok := s.classifyForgetConfirmationWithLLM(ctx, req, *op); ok {
		return llmResult, nil
	}
	decision := classifyManualForgetReply(reply, *op)
	return &decision, nil
}

type memoryOperationConfirmationLLMResult struct {
	Decision           string   `json:"decision"`
	Confidence         float64  `json:"confidence"`
	SelectedDisplayIDs []string `json:"selected_display_ids,omitempty"`
	ModifiedTarget     string   `json:"modified_target,omitempty"`
	FollowupHint       string   `json:"followup_hint,omitempty"`
	ReasonCodes        []string `json:"reason_codes,omitempty"`
}

func (s *service) classifyForgetConfirmationWithLLM(ctx context.Context, req ClassifyForgetConfirmationRequest, op PendingManualForgetOperation) (*ClassifyForgetConfirmationResult, bool) {
	llm, provider, ok := s.newMemoryOperationLLM()
	if !ok {
		return nil, false
	}
	safeCandidates := make([]MemoryOperationSafeCandidate, 0, len(op.Candidates))
	for _, candidate := range op.Candidates {
		safeCandidates = append(safeCandidates, MemoryOperationSafeCandidate{
			DisplayID:     candidate.DisplayID,
			SafeSummary:   candidate.SafeSummary,
			NodeTypeLabel: candidate.NodeTypeLabel,
			EffectLabel:   candidate.EffectLabel,
		})
	}
	payload := map[string]any{
		"schema_version":        "memory_operation_confirmation_request.v0.1",
		"persona_id":            defaultString(req.PersonaID, op.PersonaID),
		"session_id":            req.SessionID,
		"operation_status":      op.Status,
		"requested_level":       op.RequestedLevel,
		"requires_confirmation": op.RequiresConfirmation,
		"safe_candidates":       safeCandidates,
		"user_reply":            req.UserReply,
	}
	body, _ := json.Marshal(payload)
	resp, err := llm.CompleteJSON(ctx, ExtractionLLMRequest{
		Purpose:         MemoryOperationLLMPurposeConfirm,
		ProviderID:      provider.ID,
		ProviderKind:    provider.Kind,
		Model:           provider.Model,
		SystemPrompt:    memoryOperationLLMSystemPrompt(),
		DeveloperPrompt: memoryOperationConfirmationDeveloperPrompt(),
		UserPrompt:      string(body),
		Temperature:     0,
		MaxTokens:       700,
		Timeout:         provider.Timeout,
		ResponseFormat:  ExtractionResponseFormatJSONObject,
		Metadata:        requestMetadata(MemoryOperationLLMPurposeConfirm, op.ID, "manual_forget_v1", MemoryOperationContextSchemaVersion),
	})
	if err != nil {
		return nil, false
	}
	var parsed memoryOperationConfirmationLLMResult
	if err := json.Unmarshal([]byte(resp.Text), &parsed); err != nil {
		return nil, false
	}
	if !validForgetConfirmationDecision(parsed.Decision) {
		return nil, false
	}
	if parsed.Confidence <= 0 {
		parsed.Confidence = 0.5
	}
	if op.RequiresConfirmation && (parsed.Decision == ForgetConfirmationConfirm || parsed.Decision == ForgetConfirmationSelect) && parsed.Confidence < 0.8 {
		return nil, false
	}
	if highRiskManualForget(op) && parsed.Decision == ForgetConfirmationConfirm && !explicitHighRiskManualForgetConfirm(req.UserReply) {
		return nil, false
	}
	result := &ClassifyForgetConfirmationResult{
		Decision:       parsed.Decision,
		Confidence:     parsed.Confidence,
		ModifiedTarget: parsed.ModifiedTarget,
		FollowupHint:   parsed.FollowupHint,
		ReasonCodes:    append(parsed.ReasonCodes, "operation_llm"),
	}
	switch parsed.Decision {
	case ForgetConfirmationConfirm:
		result.SelectedTargetIDs = candidateTargetIDs(op.Candidates)
	case ForgetConfirmationSelect:
		result.SelectedTargetIDs = targetIDsForDisplayIDs(parsed.SelectedDisplayIDs, op.Candidates)
		if len(result.SelectedTargetIDs) == 0 {
			return nil, false
		}
	}
	return result, true
}

func (s *service) ExecuteManualForgetOperation(ctx context.Context, req ExecuteManualForgetOperationRequest) (*ExecuteManualForgetOperationResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	op, err := s.getPendingManualForgetOperationByID(ctx, personaID, req.OperationID)
	if err != nil {
		return nil, err
	}
	if !req.Confirmed {
		if err := s.updatePendingManualForgetStatus(ctx, op.ID, ManualForgetStatusCancelled); err != nil {
			return nil, err
		}
		return &ExecuteManualForgetOperationResult{
			Status:               ManualForgetStatusCancelled,
			OperationID:          op.ID,
			Level:                op.RequestedLevel,
			UserFacingLLMContext: manualForgetCancelledContext(op.ID, op.RequestedLevel),
		}, nil
	}

	selected := selectManualForgetCandidates(op.Candidates, req.ConfirmedTargetIDs)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w: no confirmed targets", ErrInvalidRequest)
	}
	actor := defaultString(req.Actor, ForgetActorUser)
	reason := defaultString(req.ReasonCode, ForgetReasonUserRequested)
	result := &ExecuteManualForgetOperationResult{
		Status:        ManualForgetStatusExecuted,
		OperationID:   op.ID,
		DeletedCounts: map[string]int{},
		Level:         op.RequestedLevel,
		VerifyPassed:  true,
	}
	verifyTargets := make([]ForgetResolvedTarget, 0, len(selected))
	for _, candidate := range selected {
		forget, err := s.Forget(ctx, ForgetRequest{
			PersonaID:  personaID,
			Actor:      actor,
			ReasonCode: reason,
			Level:      op.RequestedLevel,
			Target: ForgetTarget{
				ScopeMode: ForgetScopeExactNode,
				NodeType:  candidate.TargetType,
				NodeID:    candidate.TargetID,
			},
		})
		if err != nil {
			_ = s.updatePendingManualForgetStatus(ctx, op.ID, ManualForgetStatusFailed)
			return nil, err
		}
		result.DeletionEventIDs = append(result.DeletionEventIDs, forget.DeletionEventID)
		addManualForgetCounts(result.DeletedCounts, candidate, *forget)
		result.SafeSummaries = append(result.SafeSummaries, candidate.SafeSummary)
		verifyTargets = append(verifyTargets, ForgetResolvedTarget{
			NodeType:    candidate.TargetType,
			NodeID:      candidate.TargetID,
			SafeSummary: candidate.SafeSummary,
			Summary:     candidate.SafeSummary,
		})
	}
	verify, err := s.VerifyForget(ctx, ForgetVerifyRequest{PersonaID: personaID, Targets: verifyTargets})
	if err != nil {
		_ = s.updatePendingManualForgetStatus(ctx, op.ID, ManualForgetStatusFailed)
		return nil, err
	}
	result.VerifyResult = verify
	result.VerifyPassed = verify.Passed
	if !verify.Passed {
		result.Status = ManualForgetStatusFailed
		_ = s.updatePendingManualForgetStatus(ctx, op.ID, ManualForgetStatusFailed)
	} else if err := s.updatePendingManualForgetStatus(ctx, op.ID, ManualForgetStatusExecuted); err != nil {
		return nil, err
	}
	result.UserFacingLLMContext = manualForgetOperationContext(result.Status, op.ID, op.RequestedLevel, false, nil, result.DeletedCounts, verify)
	return result, nil
}

func (s *service) resolveManualForgetCandidates(ctx context.Context, personaID string, req PlanManualForgetRequest, level string) ([]ForgetCandidate, string, error) {
	if level == ForgetLevelSourceRedact {
		episodeID := strings.TrimSpace(req.SourceEpisodeID)
		if episodeID == "" {
			episodeID = strings.TrimSpace(req.RequestEpisodeID)
		}
		if strings.TrimSpace(req.SourceEpisodeID) == "" && strings.TrimSpace(req.RequestEpisodeID) != "" {
			prev, err := s.previousVisibleEpisodeID(ctx, personaID, req.SessionID, req.RequestEpisodeID)
			if err != nil {
				return nil, "", err
			}
			if prev != "" {
				episodeID = prev
			}
		}
		if episodeID == "" {
			return nil, ForgetScopeExactNode, nil
		}
		return []ForgetCandidate{manualForgetCandidate(ForgetNodeEpisode, episodeID, 0.93, "source_episode")}, ForgetScopeExactNode, nil
	}
	if len(req.RecentPromptRefs) > 0 && level != ForgetLevelPurge {
		refs := filterRecentPromptRefsForManualForget(req)
		candidates := make([]ForgetCandidate, 0, len(refs))
		for _, ref := range refs {
			if strings.TrimSpace(ref.NodeType) != ForgetNodeFact || strings.TrimSpace(ref.NodeID) == "" {
				continue
			}
			score := ref.Score
			if score == 0 {
				score = 0.94
			}
			score += float64(manualForgetRecentPromptTargetScore(req.Directive.TargetDescription, ref.Summary)) / 100
			candidates = append(candidates, manualForgetCandidate(ref.NodeType, ref.NodeID, score, "recent_prompt_ref"))
		}
		return dedupeManualForgetCandidates(candidates), ForgetScopeRecentPromptItem, nil
	}
	if level == ForgetLevelHard {
		episodeID := strings.TrimSpace(req.SourceEpisodeID)
		if episodeID == "" {
			episodeID = strings.TrimSpace(req.RequestEpisodeID)
		}
		if strings.TrimSpace(req.SourceEpisodeID) == "" && strings.TrimSpace(req.RequestEpisodeID) != "" {
			prev, err := s.previousVisibleEpisodeID(ctx, personaID, req.SessionID, req.RequestEpisodeID)
			if err != nil {
				return nil, "", err
			}
			if prev != "" {
				episodeID = prev
			}
		}
		candidates, err := s.factsEvidencedByEpisode(ctx, personaID, episodeID, "source_episode")
		if err != nil {
			return nil, "", err
		}
		if len(candidates) > 0 {
			return candidates, ForgetScopeExactNode, nil
		}
	}
	target := strings.TrimSpace(req.Directive.TargetDescription)
	if target == "" {
		target = safeDirectiveTargetDescription(req.UserText)
	}
	candidates, err := s.searchManualForgetFactCandidates(ctx, personaID, target, 10)
	if err != nil {
		return nil, "", err
	}
	if len(candidates) == 0 && target != strings.TrimSpace(req.UserText) {
		candidates, err = s.searchManualForgetFactCandidates(ctx, personaID, strings.TrimSpace(req.UserText), 10)
		if err != nil {
			return nil, "", err
		}
	}
	scope := ForgetScopeBroadTopic
	if len(candidates) == 1 && level != ForgetLevelPurge {
		scope = ForgetScopeExactNode
	}
	return candidates, scope, nil
}

func (s *service) searchManualForgetFactCandidates(ctx context.Context, personaID string, query string, limit int) ([]ForgetCandidate, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	docs, err := s.search.SearchDocuments(ctx, personaID, query, true, limit)
	if err != nil {
		return nil, err
	}
	candidates := make([]ForgetCandidate, 0, len(docs))
	for idx, doc := range docs {
		if doc.NodeType != core.NodeTypeFact || strings.TrimSpace(doc.NodeID) == "" {
			continue
		}
		score := 0.78 - float64(idx)*0.03
		candidates = append(candidates, manualForgetCandidate(ForgetNodeFact, doc.NodeID, score, "search_documents"))
	}
	if len(candidates) > 0 {
		return dedupeManualForgetCandidates(candidates), nil
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
SELECT id
FROM facts
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND content_summary LIKE ?
ORDER BY updated_at DESC, created_at DESC
LIMIT ?`, personaID, "%"+query+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		candidates = append(candidates, manualForgetCandidate(ForgetNodeFact, id, 0.68, "fact_like"))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return dedupeManualForgetCandidates(candidates), nil
}

func (s *service) factsEvidencedByEpisode(ctx context.Context, personaID string, episodeID string, source string) ([]ForgetCandidate, error) {
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return nil, nil
	}
	rows, err := s.sqlDB.QueryContext(ctx, `
SELECT f.id
FROM facts f
JOIN memory_links l
  ON l.persona_id = f.persona_id
 AND l.from_node_type = 'fact'
 AND l.from_node_id = f.id
 AND l.link_type = 'EVIDENCED_BY'
 AND l.to_node_type = 'episode'
WHERE f.persona_id = ?
  AND l.to_node_id = ?
  AND f.visibility_status = 'visible'
  AND f.searchable = 1
ORDER BY f.updated_at DESC, f.created_at DESC`, personaID, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []ForgetCandidate
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		candidates = append(candidates, manualForgetCandidate(ForgetNodeFact, id, 0.9, source))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (s *service) previousVisibleEpisodeID(ctx context.Context, personaID string, sessionID *string, requestEpisodeID string) (string, error) {
	requestEpisodeID = strings.TrimSpace(requestEpisodeID)
	if requestEpisodeID == "" {
		return "", nil
	}
	var occurredAt string
	var sid string
	err := s.sqlDB.QueryRowContext(ctx, `
SELECT occurred_at, session_id
FROM episodes
WHERE persona_id = ? AND id = ?`, personaID, requestEpisodeID).Scan(&occurredAt, &sid)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if sessionID != nil && strings.TrimSpace(*sessionID) != "" {
		sid = strings.TrimSpace(*sessionID)
	}
	var id string
	err = s.sqlDB.QueryRowContext(ctx, `
SELECT id
FROM episodes
WHERE persona_id = ?
  AND session_id = ?
  AND id != ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND occurred_at <= ?
ORDER BY occurred_at DESC, ingested_at DESC
LIMIT 1`, personaID, sid, requestEpisodeID, occurredAt).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func manualForgetCandidate(nodeType string, nodeID string, score float64, source string) ForgetCandidate {
	return ForgetCandidate{
		TargetType:      nodeType,
		TargetID:        nodeID,
		Score:           score,
		Source:          source,
		SafeSummary:     safeManualForgetSummary(nodeType),
		NodeTypeLabel:   manualForgetNodeTypeLabel(nodeType),
		EffectLabel:     manualForgetEffectLabel(nodeType),
		RequiresConfirm: false,
	}
}

func manualForgetRequiresConfirmation(level string, candidates []ForgetCandidate, directive ManualForgetDirectiveResult) bool {
	if level == ForgetLevelPurge {
		return true
	}
	if len(candidates) != 1 {
		return true
	}
	if level == ForgetLevelSourceRedact {
		return false
	}
	if directive.RequiresLLMConfirm && candidates[0].Score < 0.9 {
		return true
	}
	if candidates[0].Score < 0.82 {
		return true
	}
	return false
}

func classifyManualForgetReply(reply string, op PendingManualForgetOperation) ClassifyForgetConfirmationResult {
	lower := strings.ToLower(strings.TrimSpace(reply))
	result := ClassifyForgetConfirmationResult{Decision: ForgetConfirmationUnclear, Confidence: 0.45}
	if lower == "" {
		return result
	}
	if containsAny(lower, "不要", "别删", "先不", "取消", "算了", "不用", "deny", "cancel") {
		result.Decision = ForgetConfirmationDeny
		result.Confidence = 0.9
		result.ReasonCodes = []string{"deny_phrase"}
		return result
	}
	selected := selectDisplayIDs(reply, op.Candidates)
	if len(selected) > 0 {
		result.Decision = ForgetConfirmationSelect
		result.Confidence = 0.86
		result.SelectedTargetIDs = selected
		result.ReasonCodes = []string{"display_id_selection"}
		return result
	}
	strong := explicitHighRiskManualForgetConfirm(lower)
	weak := containsAny(lower, "好的", "可以", "嗯", "好", "是的", "yes", "ok")
	if strong || (weak && !highRiskManualForget(op)) {
		result.Decision = ForgetConfirmationConfirm
		result.Confidence = 0.94
		result.SelectedTargetIDs = candidateTargetIDs(op.Candidates)
		result.ReasonCodes = []string{"confirm_phrase"}
		return result
	}
	if containsAny(lower, "改成", "只删", "不是", "换成") {
		result.Decision = ForgetConfirmationModify
		result.Confidence = 0.72
		result.ModifiedTarget = reply
		result.ReasonCodes = []string{"modify_phrase"}
		return result
	}
	return result
}

func highRiskManualForget(op PendingManualForgetOperation) bool {
	return op.RequiresConfirmation && (op.RequestedLevel == ForgetLevelPurge || len(op.Candidates) > 1)
}

func explicitHighRiskManualForgetConfirm(reply string) bool {
	return containsAny(strings.ToLower(strings.TrimSpace(reply)), "确认删除", "确认删", "确认处理", "全部删除", "删除全部", "删掉这些", "都删", "可以删除", "按刚才那组处理", "delete all", "confirm delete")
}

func (s *service) loadManualForgetOperationForConfirmation(ctx context.Context, req ClassifyForgetConfirmationRequest) (*PendingManualForgetOperation, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	if strings.TrimSpace(req.OperationID) != "" {
		return s.getPendingManualForgetOperationByID(ctx, personaID, req.OperationID)
	}
	return s.GetPendingManualForgetOperation(ctx, GetPendingManualForgetOperationRequest{PersonaID: personaID, SessionID: req.SessionID})
}

func (s *service) insertPendingManualForgetOperation(ctx context.Context, op PendingManualForgetOperation) error {
	candidatesJSON, err := json.Marshal(op.Candidates)
	if err != nil {
		return err
	}
	policyJSON, err := json.Marshal(map[string]any{
		"requires_confirmation": op.RequiresConfirmation,
		"requested_level":       op.RequestedLevel,
		"scope_mode":            op.ScopeMode,
	})
	if err != nil {
		return err
	}
	_, err = s.sqlDB.ExecContext(ctx, `
INSERT INTO pending_manual_forget_operations (
    id, persona_id, session_id, chat_session_id, request_episode_id, status,
    requested_level, scope_mode, requires_confirmation, candidates_json, confirmation_policy_json,
    expires_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		op.ID,
		op.PersonaID,
		nullableManualForgetString(op.SessionID),
		nullableManualForgetValue(op.ChatSessionID),
		nullableManualForgetValue(op.RequestEpisodeID),
		op.Status,
		op.RequestedLevel,
		op.ScopeMode,
		manualForgetBoolInt(op.RequiresConfirmation),
		string(candidatesJSON),
		string(policyJSON),
		formatManualForgetTime(op.ExpiresAt),
		formatManualForgetTime(op.CreatedAt),
		formatManualForgetTime(op.UpdatedAt),
	)
	return err
}

func (s *service) getPendingManualForgetOperationByID(ctx context.Context, personaID string, operationID string) (*PendingManualForgetOperation, error) {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return nil, fmt.Errorf("%w: operation_id is required", ErrInvalidRequest)
	}
	op, err := s.scanPendingManualForgetOperation(s.sqlDB.QueryRowContext(ctx, `
SELECT id, persona_id, session_id, chat_session_id, request_episode_id, status,
       requested_level, scope_mode, requires_confirmation, candidates_json,
       created_at, updated_at, expires_at
FROM pending_manual_forget_operations
WHERE persona_id = ? AND id = ?`, personaID, operationID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: manual forget operation %s", ErrNotFound, operationID)
	}
	if err != nil {
		return nil, err
	}
	if op.Status != ManualForgetStatusPendingConfirmation && op.Status != ManualForgetStatusExecutable {
		return nil, fmt.Errorf("%w: manual forget operation is %s", ErrInvalidRequest, op.Status)
	}
	return &op, nil
}

func (s *service) scanPendingManualForgetOperation(row interface{ Scan(dest ...any) error }) (PendingManualForgetOperation, error) {
	var op PendingManualForgetOperation
	var sessionID, chatSessionID, requestEpisodeID sql.NullString
	var candidatesJSON string
	var requiresConfirmation int
	var createdAt, updatedAt, expiresAt string
	if err := row.Scan(
		&op.ID,
		&op.PersonaID,
		&sessionID,
		&chatSessionID,
		&requestEpisodeID,
		&op.Status,
		&op.RequestedLevel,
		&op.ScopeMode,
		&requiresConfirmation,
		&candidatesJSON,
		&createdAt,
		&updatedAt,
		&expiresAt,
	); err != nil {
		return PendingManualForgetOperation{}, err
	}
	op.SessionID = stringPtrFromNull(sessionID)
	op.ChatSessionID = stringValueFromNull(chatSessionID)
	op.RequestEpisodeID = stringValueFromNull(requestEpisodeID)
	op.RequiresConfirmation = requiresConfirmation == 1
	if err := json.Unmarshal([]byte(candidatesJSON), &op.Candidates); err != nil {
		return PendingManualForgetOperation{}, err
	}
	op.CreatedAt = parseManualForgetTime(createdAt)
	op.UpdatedAt = parseManualForgetTime(updatedAt)
	op.ExpiresAt = parseManualForgetTime(expiresAt)
	return op, nil
}

func (s *service) updatePendingManualForgetStatus(ctx context.Context, operationID string, status string) error {
	_, err := s.sqlDB.ExecContext(ctx, `
UPDATE pending_manual_forget_operations
SET status = ?, updated_at = ?
WHERE id = ?`, status, formatManualForgetTime(s.now()), operationID)
	return err
}

func manualForgetOperationContext(status string, operationID string, level string, requiresConfirmation bool, candidates []ForgetCandidate, counts map[string]int, verify *ForgetVerifyResult) *MemoryOperationLLMContext {
	ctx := &MemoryOperationLLMContext{
		SchemaVersion:      MemoryOperationContextSchemaVersion,
		OperationType:      "manual_forget",
		Status:             status,
		PendingOperationID: operationID,
		RequestedLevel:     level,
		RiskLevel:          manualForgetRiskLevel(level, len(candidates)),
		AssistantGuidance: MemoryOperationAssistantGuidance{
			Tone:  "natural, calm, brief",
			DoNot: []string{"不要复述被删内容", "不要展示内部 ID", "不要说数据库表名", "不要复述敏感原文"},
		},
	}
	if len(candidates) > 0 {
		ctx.SafeCandidateCount = len(candidates)
		for _, candidate := range candidates {
			ctx.SafeCandidates = append(ctx.SafeCandidates, MemoryOperationSafeCandidate{
				DisplayID:     candidate.DisplayID,
				SafeSummary:   candidate.SafeSummary,
				NodeTypeLabel: candidate.NodeTypeLabel,
				EffectLabel:   candidate.EffectLabel,
			})
		}
	}
	if requiresConfirmation {
		ctx.AssistantGuidance.AskConfirmation = true
		ctx.AssistantGuidance.Say = manualForgetConfirmationQuestion(ctx.SafeCandidates)
		ctx.AssistantGuidance.SuggestedUserVisibleQuestion = ctx.AssistantGuidance.Say
		ctx.AssistantGuidance.DoNot = append(ctx.AssistantGuidance.DoNot,
			"不要把待确认状态说成已处理、已删除、记住了或以后不会再提",
			"不要在用户确认前承诺已经改动记忆",
		)
		return ctx
	}
	ctx.AssistantGuidance.ReplyNaturally = true
	if counts != nil {
		ctx.DeletedCounts = counts
		ctx.SafeResultSummary = safeExecutionSummary(counts)
		ctx.AssistantGuidance.Say = suggestedExecutionReply(level)
		searchable := false
		passed := false
		if verify != nil {
			passed = verify.Passed
		}
		ctx.Verify = &MemoryOperationVerifyContext{Passed: passed, SearchableAfter: searchable}
	}
	return ctx
}

func noMatchMemoryOperationContext(level string) *MemoryOperationLLMContext {
	return &MemoryOperationLLMContext{
		SchemaVersion:     MemoryOperationContextSchemaVersion,
		OperationType:     "manual_forget",
		Status:            ManualForgetStatusNoMatch,
		RequestedLevel:    level,
		SafeResultSummary: "没有找到明确对应的已保存记忆。",
		AssistantGuidance: MemoryOperationAssistantGuidance{
			ReplyNaturally: true,
			Say:            "我没有找到对应的已保存记忆；之后也不会主动保存或提起这类内容。",
			DoNot:          []string{"不要复述用户想删除的原文", "不要展示内部 ID", "不要承诺已经删除未找到的内容"},
		},
	}
}

func manualForgetCancelledContext(operationID string, level string) *MemoryOperationLLMContext {
	return &MemoryOperationLLMContext{
		SchemaVersion:      MemoryOperationContextSchemaVersion,
		OperationType:      "manual_forget",
		Status:             ManualForgetStatusCancelled,
		PendingOperationID: operationID,
		RequestedLevel:     level,
		AssistantGuidance: MemoryOperationAssistantGuidance{
			ReplyNaturally: true,
			Say:            "好的，先不删除。",
			DoNot:          []string{"不要复述候选内容", "不要展示内部 ID"},
		},
	}
}

func manualForgetConfirmationQuestion(candidates []MemoryOperationSafeCandidate) string {
	if len(candidates) == 0 {
		return "我找到可能相关的记忆。处理前请先确认要继续吗？"
	}
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.DisplayID) != "" {
			ids = append(ids, strings.TrimSpace(candidate.DisplayID))
		}
	}
	if len(candidates) == 1 {
		return "我找到 1 条可能相关的记忆。处理前请确认：要处理这条吗？"
	}
	if len(ids) == 0 {
		return fmt.Sprintf("我找到 %d 条可能相关的记忆。处理前请确认：要处理全部，还是只处理其中几条？", len(candidates))
	}
	return fmt.Sprintf("我找到 %d 条可能相关的记忆（%s）。处理前请确认：要处理全部，还是只处理其中哪些编号？", len(candidates), strings.Join(ids, "、"))
}

func dedupeManualForgetCandidates(candidates []ForgetCandidate) []ForgetCandidate {
	seen := map[string]struct{}{}
	var out []ForgetCandidate
	for _, candidate := range candidates {
		key := candidate.TargetType + "\x1f" + candidate.TargetID
		if candidate.TargetType == "" || candidate.TargetID == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Score > out[j].Score
	})
	for i := range out {
		out[i].DisplayID = displayIDForIndex(i)
	}
	return out
}

func filterRecentPromptRefsForManualForget(req PlanManualForgetRequest) []RecentPromptMemoryRef {
	target := strings.TrimSpace(req.Directive.TargetDescription)
	if target == "" {
		target = safeDirectiveTargetDescription(req.UserText)
	}
	if strings.TrimSpace(target) == "" {
		return req.RecentPromptRefs
	}
	var matched []RecentPromptMemoryRef
	for _, ref := range req.RecentPromptRefs {
		if manualForgetRecentPromptTargetScore(target, ref.Summary) >= 3 {
			matched = append(matched, ref)
		}
	}
	if len(matched) == 0 {
		return req.RecentPromptRefs
	}
	return matched
}

func manualForgetRecentPromptTargetScore(target string, summary string) int {
	targetText := compactManualForgetMatchText(target)
	summaryText := compactManualForgetMatchText(summary)
	if targetText == "" || summaryText == "" {
		return 0
	}
	score := 0
	if strings.Contains(summaryText, targetText) {
		score += 4
	}
	for _, phrase := range []string{"不喜欢", "喜欢", "讨厌", "害怕", "住在", "来自", "参与", "压力", "偏好"} {
		if strings.Contains(targetText, phrase) && strings.Contains(summaryText, phrase) {
			score += 2
		}
	}
	for _, term := range manualForgetCJKBigrams(targetText) {
		if strings.Contains(summaryText, term) {
			score++
		}
	}
	for _, term := range manualForgetASCIITerms(target) {
		if strings.Contains(summaryText, strings.ToLower(term)) {
			score++
		}
	}
	return score
}

func compactManualForgetMatchText(value string) string {
	replacer := strings.NewReplacer(
		"以后", "",
		"不要再提", "",
		"别再提", "",
		"别提", "",
		"关于", "",
		"用户", "",
		"我", "",
		"的", "",
		"了", "",
		"。", "",
		"，", "",
		",", "",
		".", "",
		"！", "",
		"!", "",
		"？", "",
		"?",
		"",
		" ", "",
		"\t", "",
		"\r", "",
		"\n", "",
	)
	return strings.ToLower(strings.TrimSpace(replacer.Replace(value)))
}

func manualForgetCJKBigrams(value string) []string {
	var runes []rune
	for _, r := range value {
		if r >= 0x4e00 && r <= 0x9fff {
			runes = append(runes, r)
		}
	}
	if len(runes) < 2 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		term := string(runes[i : i+2])
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

func manualForgetASCIITerms(value string) []string {
	fields := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-'
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func capManualForgetCandidates(candidates []ForgetCandidate, max int) []ForgetCandidate {
	if max <= 0 || len(candidates) <= max {
		return candidates
	}
	return append([]ForgetCandidate(nil), candidates[:max]...)
}

func selectManualForgetCandidates(candidates []ForgetCandidate, ids []string) []ForgetCandidate {
	if len(ids) == 0 {
		return append([]ForgetCandidate(nil), candidates...)
	}
	selected := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			selected[id] = struct{}{}
		}
	}
	var out []ForgetCandidate
	for _, candidate := range candidates {
		if _, ok := selected[candidate.TargetID]; ok {
			out = append(out, candidate)
		}
	}
	return out
}

func addManualForgetCounts(counts map[string]int, candidate ForgetCandidate, result ForgetResult) {
	switch candidate.TargetType {
	case ForgetNodeFact:
		counts["facts"]++
	case ForgetNodeEpisode:
		counts["episodes"]++
	}
	counts["search_documents_deleted"] += int(result.SearchDocumentsDeleted)
	counts["fts_rows_deleted"] += int(result.FTSRowsDeleted)
	counts["links_scrubbed"] += int(result.LinksScrubbed)
	if result.MirrorDeletesEnqueued > 0 {
		counts["mirror_deletes_enqueued"] += int(result.MirrorDeletesEnqueued)
	}
}

func selectDisplayIDs(reply string, candidates []ForgetCandidate) []string {
	upper := strings.ToUpper(reply)
	fields := strings.FieldsFunc(upper, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})
	var selected []string
	for _, candidate := range candidates {
		displayID := strings.ToUpper(strings.TrimSpace(candidate.DisplayID))
		if displayID == "" {
			continue
		}
		for _, field := range fields {
			if field == displayID {
				selected = append(selected, candidate.TargetID)
				break
			}
		}
	}
	return selected
}

func targetIDsForDisplayIDs(displayIDs []string, candidates []ForgetCandidate) []string {
	wanted := map[string]struct{}{}
	for _, id := range displayIDs {
		id = strings.ToUpper(strings.TrimSpace(id))
		if id != "" {
			wanted[id] = struct{}{}
		}
	}
	var selected []string
	for _, candidate := range candidates {
		displayID := strings.ToUpper(strings.TrimSpace(candidate.DisplayID))
		if _, ok := wanted[displayID]; ok {
			selected = append(selected, candidate.TargetID)
		}
	}
	return selected
}

func candidateTargetIDs(candidates []ForgetCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.TargetID)
	}
	return out
}

func (s *service) newMemoryOperationLLM() (ExtractionLLM, ExtractionProviderOptions, bool) {
	cfg := normalizeExtractionOptions(s.extraction)
	if strings.TrimSpace(cfg.Provider.Kind) == "" || cfg.Provider.Kind == ExtractionProviderDisabled {
		return nil, ExtractionProviderOptions{}, false
	}
	llm, err := newExtractionLLM(cfg.Provider)
	if err != nil {
		return nil, ExtractionProviderOptions{}, false
	}
	return llm, cfg.Provider, true
}

func memoryOperationLLMSystemPrompt() string {
	return "You are MemoryCore's internal memory operation classifier. Return one strict JSON object only. Never write user-facing prose."
}

func memoryOperationDirectiveDeveloperPrompt() string {
	return `Classify whether the user is requesting a memory operation. Output fields: intent (none|pin|forget|correction|unclear), confidence, forget_level_hint (soft_forget|hard_forget|source_redact|purge), target_description, target_node_type_hint (fact|episode), requires_llm_confirm, reason_codes. Do not execute deletion. Prefer safe confirmation for broad or destructive requests.`
}

func memoryOperationConfirmationDeveloperPrompt() string {
	return `Classify the user's reply to a pending manual forget confirmation. Output fields: decision (confirm|deny|select|modify|unclear), confidence, selected_display_ids, modified_target, followup_hint, reason_codes. Use selected_display_ids only for display IDs shown in safe_candidates. For broad purge, only confirm when the reply is clear.`
}

func validManualForgetIntent(intent string) bool {
	switch strings.TrimSpace(intent) {
	case ManualForgetIntentNone, ManualForgetIntentPin, ManualForgetIntentForget, ManualForgetIntentCorrection, ManualForgetIntentUnclear:
		return true
	default:
		return false
	}
}

func normalizeManualForgetLevel(level string) string {
	normalized := strings.TrimSpace(strings.ToLower(level))
	switch normalized {
	case ForgetLevelSoft, ForgetLevelHard, ForgetLevelSourceRedact, ForgetLevelPurge:
		return normalized
	case "soft", "avoid_mention", "do_not_mention":
		return ForgetLevelSoft
	case "hard", "delete", "remove", "delete_memory":
		return ForgetLevelHard
	case "source", "redact", "source_redaction", "delete_source":
		return ForgetLevelSourceRedact
	case "complete", "full", "all", "delete_all", "full_delete", "complete_delete":
		return ForgetLevelPurge
	default:
		return ""
	}
}

func validForgetConfirmationDecision(decision string) bool {
	switch strings.TrimSpace(decision) {
	case ForgetConfirmationConfirm, ForgetConfirmationDeny, ForgetConfirmationSelect, ForgetConfirmationModify, ForgetConfirmationUnclear:
		return true
	default:
		return false
	}
}

func safeDirectiveTargetDescription(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if match := broadForgetTopicPattern.FindStringSubmatch(text); len(match) == 2 {
		return trimManualForgetTarget(match[1])
	}
	if match := relatedForgetTopicPattern.FindStringSubmatch(text); len(match) == 2 {
		return trimManualForgetTarget(match[1])
	}
	for _, prefix := range []string{"彻底删除", "删除", "忘记", "忘掉", "别再提", "不要再提", "别记", "不要记"} {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	return trimManualForgetTarget(text)
}

func trimManualForgetTarget(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "关于")
	for _, suffix := range []string{"的所有记忆", "所有记忆", "相关记忆", "这类内容", "这个话题", "这件事"} {
		value = strings.TrimSuffix(strings.TrimSpace(value), suffix)
	}
	return strings.Trim(strings.TrimSpace(value), " \t\r\n。！？!?.,，；;：:\"'“”‘’")
}

func safeManualForgetSummary(nodeType string) string {
	switch nodeType {
	case ForgetNodeEpisode:
		return "一段稍早的对话原文"
	default:
		return "一条相关长期事实记忆"
	}
}

func manualForgetNodeTypeLabel(nodeType string) string {
	switch nodeType {
	case ForgetNodeEpisode:
		return "对话原文"
	default:
		return "事实记忆"
	}
}

func manualForgetEffectLabel(nodeType string) string {
	switch nodeType {
	case ForgetNodeEpisode:
		return "处理后不会再作为记忆来源使用"
	default:
		return "处理后不会再用于回复"
	}
}

func safePlanSummary(level string, count int) string {
	if count <= 0 {
		return "没有找到明确对应的已保存记忆。"
	}
	switch level {
	case ForgetLevelSourceRedact:
		return fmt.Sprintf("找到 %d 段可处理的对话原文。", count)
	default:
		return fmt.Sprintf("找到 %d 条可能相关的记忆。", count)
	}
}

func safeExecutionSummary(counts map[string]int) string {
	total := counts["facts"] + counts["episodes"]
	if total <= 0 {
		return "已处理相关记忆。"
	}
	return fmt.Sprintf("已处理 %d 条相关记忆。", total)
}

func suggestedExecutionReply(level string) string {
	switch level {
	case ForgetLevelSoft:
		return "好的，我会避开这个话题，不再主动提起。"
	case ForgetLevelSourceRedact:
		return "已处理，这段原文不会再作为记忆来源使用。"
	case ForgetLevelPurge:
		return "已删除相关记忆，之后不会再使用。"
	default:
		return "已处理，我不会再使用这条记忆。"
	}
}

func manualForgetRiskLevel(level string, count int) string {
	if level == ForgetLevelPurge || count > 1 {
		return "high"
	}
	if level == ForgetLevelSourceRedact {
		return "medium"
	}
	return "low"
}

func displayIDForIndex(idx int) string {
	if idx >= 0 && idx < 26 {
		return string(rune('A' + idx))
	}
	return fmt.Sprintf("%d", idx+1)
}

func manualForgetContainsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func maxFloat(a float64, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func cloneStringPtr(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	out := strings.TrimSpace(*value)
	return &out
}

func stringPtrFromNull(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

func stringValueFromNull(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func manualForgetBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableManualForgetString(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return strings.TrimSpace(*value)
}

func nullableManualForgetValue(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func manualForgetNow(s *service, value time.Time) time.Time {
	if !value.IsZero() {
		return value.UTC()
	}
	return s.now().UTC()
}

func formatManualForgetTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseManualForgetTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed
	}
	parsed, _ = time.Parse("2006-01-02 15:04:05", value)
	return parsed
}
