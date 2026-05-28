package memorycore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

var errPreFilterEnvelope = errors.New("prefilter envelope mismatch")

func (r *Runner) runPreFilter(ctx context.Context, req ExtractionRequest, runReq ExtractionRunRequest, trace *rawLogTrace) (ExtractionRequest, string, LLMUsage, int, error) {
	llmReq := r.buildPreFilterLLMRequest(req, runReq)
	trace.recordPreFilterRequest(llmReq)
	raw, err := r.llm.CompleteJSON(ctx, llmReq)
	trace.recordPreFilterResponse(raw)
	if err != nil {
		return req, "", raw.Usage, 0, err
	}
	prefilterHash := hashText(raw.Text)
	resp, err := ParsePreFilterResponse(strings.NewReader(raw.Text))
	if err != nil && runReq.RepairEnabled {
		trace.recordPreFilterParseError(err)
		repairReq := r.buildPreFilterRepairLLMRequest(raw.Text, runReq)
		trace.recordPreFilterRepairRequest(repairReq)
		repaired, repairErr := r.llm.CompleteJSON(ctx, repairReq)
		trace.recordPreFilterRepairResponse(repaired)
		raw.Usage = addUsage(raw.Usage, repaired.Usage)
		if repairErr != nil {
			return req, prefilterHash, raw.Usage, 0, repairErr
		}
		prefilterHash = hashText(repaired.Text)
		resp, err = ParsePreFilterResponse(strings.NewReader(repaired.Text))
		trace.recordPreFilterRepairParseError(err)
	}
	if err != nil {
		trace.recordPreFilterParseError(err)
		return req, prefilterHash, raw.Usage, 0, err
	}
	if err := validatePreFilterEnvelope(req, resp); err != nil {
		return req, prefilterHash, raw.Usage, 0, err
	}
	filtered, reviews := applyPreFilter(req, resp)
	return filtered, prefilterHash, raw.Usage, reviews, nil
}

func validatePreFilterEnvelope(req ExtractionRequest, resp ExtractionPreFilterResponse) error {
	if resp.RequestID != req.RequestID {
		return errPreFilterEnvelope
	}
	if resp.PersonaID != req.PersonaID {
		return errPreFilterEnvelope
	}
	if (resp.SessionID == nil) != (req.SessionID == nil) {
		return errPreFilterEnvelope
	}
	if resp.SessionID != nil && req.SessionID != nil && *resp.SessionID != *req.SessionID {
		return errPreFilterEnvelope
	}
	if resp.Trigger != req.Trigger {
		return errPreFilterEnvelope
	}
	return nil
}

func (r *Runner) buildPreFilterLLMRequest(req ExtractionRequest, runReq ExtractionRunRequest) ExtractionLLMRequest {
	requestJSON, _ := json.Marshal(req)
	return ExtractionLLMRequest{
		Purpose:         ExtractionLLMPurposePreFilter,
		ProviderID:      runReq.ProviderID,
		ProviderKind:    runReq.ProviderKind,
		Model:           runReq.Model,
		SystemPrompt:    "MemoryCore prefilter " + r.promptVersions.PreFilter + ". Decide whether each episode should be kept for ",
		DeveloperPrompt: "Return strict JSON for schema " + ExtractionPreFilterSchemaVersion + " with keep boolean and routing_hint extract|forget_manager|pin_manager|skip|review. Review and manager routes mean keep.",
		UserPrompt:      string(requestJSON),
		Temperature:     runReq.Temperature,
		MaxTokens:       runReq.MaxTokens,
		Timeout:         runReq.Timeout,
		ResponseFormat:  ExtractionResponseFormatJSONObject,
		Metadata:        requestMetadata(ExtractionLLMPurposePreFilter, req.RequestID, r.promptVersions.PreFilter, ExtractionPreFilterSchemaVersion),
	}
}

func (r *Runner) buildPreFilterRepairLLMRequest(raw string, runReq ExtractionRunRequest) ExtractionLLMRequest {
	return ExtractionLLMRequest{
		Purpose:         ExtractionLLMPurposeRepair,
		ProviderID:      runReq.ProviderID,
		ProviderKind:    runReq.ProviderKind,
		Model:           runReq.Model,
		SystemPrompt:    "MemoryCore prefilter JSON repair " + r.promptVersions.Repair + ". Repair JSON only.",
		DeveloperPrompt: "Return strict JSON for schema " + ExtractionPreFilterSchemaVersion + ". Do not include episode text.",
		UserPrompt:      raw,
		Temperature:     runReq.Temperature,
		MaxTokens:       runReq.MaxTokens,
		Timeout:         runReq.Timeout,
		ResponseFormat:  ExtractionResponseFormatJSONObject,
		Metadata:        requestMetadata(ExtractionLLMPurposeRepair, "", r.promptVersions.Repair, ExtractionPreFilterSchemaVersion),
	}
}

func applyPreFilter(req ExtractionRequest, resp ExtractionPreFilterResponse) (ExtractionRequest, int) {
	decisions := map[string]ExtractionPreFilterEpisode{}
	for _, decision := range resp.Episodes {
		decisions[decision.EpisodeID] = decision
	}
	filtered := cloneRequest(req)
	filtered.Episodes = filtered.Episodes[:0]
	reviewCount := 0
	for _, episode := range req.Episodes {
		decision, ok := decisions[episode.EpisodeID]
		keep := true
		if ok {
			keep = decision.Keep
			if preFilterHintForcesKeep(decision.RoutingHint) {
				keep = true
				reviewCount++
			}
		} else {
			reviewCount++
		}
		if mustKeepEpisode(req, episode) {
			keep = true
		}
		if keep {
			filtered.Episodes = append(filtered.Episodes, episode)
		}
	}
	return filtered, reviewCount
}

func preFilterHintForcesKeep(hint string) bool {
	switch hint {
	case "forget_manager", "pin_manager", "review", "route":
		return true
	default:
		return false
	}
}

func mustKeepEpisode(req ExtractionRequest, episode ExtractionEpisode) bool {
	if req.Trigger == ExtractionTriggerManualPin || req.Trigger == ExtractionTriggerManualForget || req.Policy.ManualPin || req.Policy.ManualForget {
		return true
	}
	text := strings.ToLower(episode.Content)
	needles := []string{
		"不要再提", "别再提", "不要提", "忘记", "删除", "删掉", "source_redact", "do-not-mention", "do not mention", "forget",
		"纠正", "更正", "修正", "不是", "记住", "请记住", "remember",
		"我是", "我叫", "我的名字", "核心", "身份",
		"喜欢", "不喜欢", "讨厌", "偏好", "边界", "不要", "承诺", "答应", "计划", "长期", "重要",
		"痛苦", "开心", "难过", "信任", "关系", "害怕", "焦虑",
	}
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
