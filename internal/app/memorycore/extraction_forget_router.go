package memorycore

import (
	"context"
	"errors"
	"strings"
	"time"
)

type extractionDeletionIntentPreviewer interface {
	PreviewExtractionDeletionIntents(ctx context.Context, req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult) []RoutedForgetPreview
}

func previewExtractionDeletionIntents(ctx context.Context, svc Service, req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult) []RoutedForgetPreview {
	previewer, ok := svc.(extractionDeletionIntentPreviewer)
	if !ok || previewer == nil {
		return nil
	}
	return previewer.PreviewExtractionDeletionIntents(ctx, req, resp, gate)
}

func (s *service) PreviewExtractionDeletionIntents(ctx context.Context, req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult) []RoutedForgetPreview {
	decisionByCandidate := map[string]CandidateGateDecision{}
	for _, decision := range gate.DeletionIntentDecisions {
		decisionByCandidate[decision.CandidateID] = decision
	}
	routes := make([]RoutedForgetPreview, 0, len(resp.DeletionIntents))
	for _, intent := range resp.DeletionIntents {
		gateDecision, ok := decisionByCandidate[intent.CandidateID]
		if !ok || gateDecision.Decision != decisionRouteOnly {
			continue
		}
		route := RoutedForgetPreview{
			IntentCandidateID: intent.CandidateID,
			ForgetLevel:       intent.ForgetLevel,
			PreviewOnly:       true,
		}
		previewReq, ok, code := forgetPreviewRequestFromDeletionIntent(req, intent)
		if !ok {
			route.ErrorCode = code
			routes = append(routes, route)
			continue
		}
		applySafePreviewRequest(&route, previewReq)
		preview, err := s.PreviewForget(ctx, previewReq)
		if err != nil {
			route.ErrorCode = forgetPreviewErrorCode(err)
			route.ErrorMessage = "forget preview could not be resolved"
			routes = append(routes, route)
			continue
		}
		route.Preview = preview
		if preview.Status == "no_match" {
			route.ErrorCode = "forget_preview_no_match"
		}
		enforceExtractionForgetConfirmation(intent, preview)
		routes = append(routes, route)
	}
	return routes
}

func forgetPreviewRequestFromDeletionIntent(req ExtractionRequest, intent ExtractedDeletionIntent) (ForgetPreviewRequest, bool, string) {
	previewReq := ForgetPreviewRequest{
		PersonaID: req.PersonaID,
		Limit:     20,
	}
	if nodeType, nodeID, ok := exactForgetNodeFromIntent(intent); ok {
		previewReq.ScopeMode = ForgetScopeExactNode
		previewReq.NodeType = nodeType
		previewReq.NodeID = nodeID
		return previewReq, true, ""
	}
	if entityID, ok := exactEntityFromIntent(intent); ok {
		previewReq.ScopeMode = ForgetScopeEntity
		previewReq.EntityID = entityID
		previewReq.Limit = 50
		return previewReq, true, ""
	}
	target := strings.TrimSpace(intent.TargetDescription)
	if target == "" || currentWindowOnlyForgetIntent(target) {
		return ForgetPreviewRequest{}, false, "current_window_only"
	}
	if intent.ForgetLevel == ForgetLevelSourceRedact {
		previewReq.ScopeMode = ForgetScopeExactNode
		previewReq.NodeType = ForgetNodeEpisode
		previewReq.NodeID = intent.SourceEpisodeID
		return previewReq, true, ""
	}
	if recentEpisodeWindowIntent(target) {
		previewReq.ScopeMode = ForgetScopeRecentEpisodeWindow
		previewReq.SessionID = deref(req.SessionID)
		setRecentEpisodeWindowBounds(&previewReq, req)
		return previewReq, true, ""
	}
	previewReq.ScopeMode = ForgetScopeBroadTopic
	previewReq.Topic = target
	return previewReq, true, ""
}

func applySafePreviewRequest(route *RoutedForgetPreview, req ForgetPreviewRequest) {
	route.ScopeMode = req.ScopeMode
	switch req.ScopeMode {
	case ForgetScopeExactNode, ForgetScopeRecentPromptItem:
		route.NodeType = req.NodeType
		route.NodeID = req.NodeID
	case ForgetScopeEntity:
		route.EntityID = req.EntityID
	}
}

func enforceExtractionForgetConfirmation(intent ExtractedDeletionIntent, preview *ForgetPreviewResult) {
	if preview == nil {
		return
	}
	requires := intent.RequiresConfirmation ||
		intent.ForgetLevel == ForgetLevelHard ||
		intent.ForgetLevel == ForgetLevelSourceRedact ||
		intent.ForgetLevel == ForgetLevelPurge ||
		preview.ScopeMode == ForgetScopeEntity ||
		preview.ScopeMode == ForgetScopeBroadTopic
	if !requires {
		return
	}
	preview.RequiresConfirmation = true
	if preview.Reason == "" {
		preview.Reason = "extraction_deletion_intent_requires_confirmation"
	}
}

func exactForgetNodeFromIntent(intent ExtractedDeletionIntent) (string, string, bool) {
	target := strings.TrimSpace(intent.TargetDescription)
	hint := normalizeForgetNodeType(deref(intent.TargetNodeTypeHint))
	for _, prefix := range []struct {
		token    string
		nodeType string
	}{
		{"fact:", ForgetNodeFact},
		{"fact/", ForgetNodeFact},
		{"episode:", ForgetNodeEpisode},
		{"episode/", ForgetNodeEpisode},
	} {
		if strings.HasPrefix(target, prefix.token) {
			nodeID := strings.TrimSpace(strings.TrimPrefix(target, prefix.token))
			if nodeID != "" {
				return prefix.nodeType, nodeID, true
			}
		}
	}
	if hint == ForgetNodeFact && looksExactFactID(target) {
		return hint, target, true
	}
	if hint == ForgetNodeEpisode && looksExactEpisodeID(target) {
		return hint, target, true
	}
	return "", "", false
}

func exactEntityFromIntent(intent ExtractedDeletionIntent) (string, bool) {
	target := strings.TrimSpace(intent.TargetDescription)
	if strings.HasPrefix(target, "entity:") {
		entityID := strings.TrimSpace(strings.TrimPrefix(target, "entity:"))
		return entityID, entityID != ""
	}
	if strings.HasPrefix(target, "entity/") {
		entityID := strings.TrimSpace(strings.TrimPrefix(target, "entity/"))
		return entityID, entityID != ""
	}
	if normalizeForgetNodeType(deref(intent.TargetNodeTypeHint)) == "entity" && looksExactEntityID(target) {
		return target, true
	}
	return "", false
}

func normalizeForgetNodeType(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case ForgetNodeFact, "memory_fact":
		return ForgetNodeFact
	case ForgetNodeEpisode, "source_episode":
		return ForgetNodeEpisode
	case "entity", ForgetScopeEntity:
		return "entity"
	default:
		return ""
	}
}

func looksExactNodeID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, " \t\r\n，。:：") {
		return false
	}
	return looksExactFactID(value) || looksExactEpisodeID(value) || looksExactEntityID(value)
}

func looksExactFactID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, " \t\r\n，。:：") && strings.HasPrefix(value, "fact_")
}

func looksExactEpisodeID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, " \t\r\n，。:：") && (strings.HasPrefix(value, "episode_") || strings.HasPrefix(value, "ep_"))
}

func looksExactEntityID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, " \t\r\n，。:：") && strings.HasPrefix(value, "ent_")
}

func currentWindowOnlyForgetIntent(target string) bool {
	text := normalizeAdmissionText(target)
	if !containsDoNotRemember(text) {
		return false
	}
	return !containsAnyAdmissionText(text, []string{"之前", "以前", "已经", "记得的", "旧记忆", "不要再提", "别再提", "删除", "忘掉", "previous", "already remembered"})
}

func recentEpisodeWindowIntent(target string) bool {
	return containsAnyAdmissionText(target, []string{"刚才", "这段", "原文", "source_redact", "source redact", "recent episode", "episode window"})
}

func setRecentEpisodeWindowBounds(previewReq *ForgetPreviewRequest, req ExtractionRequest) {
	if req.SessionID != nil {
		previewReq.SessionID = *req.SessionID
	}
	for _, episode := range req.Episodes {
		if episode.OccurredAt.IsZero() {
			continue
		}
		occurred := episode.OccurredAt
		if previewReq.Since == nil || occurred.Before(*previewReq.Since) {
			since := occurred.Add(-time.Second)
			previewReq.Since = &since
		}
		if previewReq.Until == nil || occurred.After(*previewReq.Until) {
			until := occurred.Add(time.Second)
			previewReq.Until = &until
		}
	}
	if len(req.Episodes) > 0 {
		previewReq.Limit = len(req.Episodes)
	}
}

func forgetPreviewErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_forget_preview_request"
	case errors.Is(err, ErrNotFound):
		return "forget_preview_not_found"
	default:
		return "forget_preview_failed"
	}
}
