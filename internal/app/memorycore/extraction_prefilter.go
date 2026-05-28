package memorycore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		repairReq := r.buildPreFilterRepairLLMRequest(req, raw.Text, err, runReq)
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
		SystemPrompt:    prefilterSystemPrompt(r.promptVersions.PreFilter),
		DeveloperPrompt: prefilterDeveloperPrompt(),
		UserPrompt:      string(requestJSON),
		Temperature:     runReq.Temperature,
		MaxTokens:       runReq.MaxTokens,
		Timeout:         runReq.Timeout,
		ResponseFormat:  ExtractionResponseFormatJSONObject,
		Metadata:        requestMetadata(ExtractionLLMPurposePreFilter, req.RequestID, r.promptVersions.PreFilter, ExtractionPreFilterSchemaVersion),
	}
}

func (r *Runner) buildPreFilterRepairLLMRequest(req ExtractionRequest, raw string, parseErr error, runReq ExtractionRunRequest) ExtractionLLMRequest {
	repairJSON, _ := json.Marshal(preFilterRepairPayload(req, raw))
	return ExtractionLLMRequest{
		Purpose:         ExtractionLLMPurposeRepair,
		ProviderID:      runReq.ProviderID,
		ProviderKind:    runReq.ProviderKind,
		Model:           runReq.Model,
		SystemPrompt:    "MemoryCore prefilter JSON repair " + r.promptVersions.Repair + ". Repair JSON only.",
		DeveloperPrompt: prefilterRepairDeveloperPrompt(parseErr),
		UserPrompt:      string(repairJSON),
		Temperature:     runReq.Temperature,
		MaxTokens:       runReq.MaxTokens,
		Timeout:         runReq.Timeout,
		ResponseFormat:  ExtractionResponseFormatJSONObject,
		Metadata:        requestMetadata(ExtractionLLMPurposeRepair, "", r.promptVersions.Repair, ExtractionPreFilterSchemaVersion),
	}
}

func prefilterSystemPrompt(version string) string {
	return fmt.Sprintf(`MemoryCore prefilter %s. Decide whether each input episode should be kept for the extraction runtime.
Return exactly one JSON object and no prose, markdown, code fences, or wrapper text.
FORMAT ONLY JSON EXAMPLE:
{"schema_version":"%s","request_id":"req_example","persona_id":"default","session_id":null,"trigger":"session_end","episodes":[{"episode_id":"ep_1","keep":true,"routing_hint":"extract","reason_codes":["memory_candidate"]}],"quality_flags":[]}`, version, ExtractionPreFilterSchemaVersion)
}

func prefilterDeveloperPrompt() string {
	return "Return strict JSON for schema " + ExtractionPreFilterSchemaVersion + ". Top-level fields must include schema_version, request_id, persona_id, session_id, trigger, episodes, and quality_flags. Each episodes item must include episode_id, keep, routing_hint, and reason_codes. Each input episode_id must appear exactly once in episodes. Allowed routing_hint values: extract, forget_manager, pin_manager, skip, review. Do not put keep or routing_hint at the top level. Do not copy episode content into the response. When unsure, use keep=true and routing_hint=\"review\". forget_manager, pin_manager, and review mean keep the episode."
}

func prefilterRepairDeveloperPrompt(parseErr error) string {
	message := ""
	if parseErr != nil {
		message = " Parser error to fix: " + parseErr.Error()
	}
	return "Return only one strict JSON object for schema " + ExtractionPreFilterSchemaVersion + ". Do not include markdown fences. Do not include episode text. Repair to the complete prefilter envelope with top-level schema_version, request_id, persona_id, session_id, trigger, episodes, and quality_flags. If the original response lacks per-episode decisions, output every episode_id with keep=true and routing_hint=\"review\"." + message + "\n" + prefilterDeveloperPrompt()
}

func preFilterRepairPayload(req ExtractionRequest, raw string) map[string]any {
	episodeIDs := make([]string, 0, len(req.Episodes))
	for _, episode := range req.Episodes {
		episodeIDs = append(episodeIDs, episode.EpisodeID)
	}
	return map[string]any{
		"original_response": raw,
		"request_context": map[string]any{
			"request_id":  req.RequestID,
			"persona_id":  req.PersonaID,
			"session_id":  req.SessionID,
			"trigger":     req.Trigger,
			"episode_ids": episodeIDs,
		},
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
