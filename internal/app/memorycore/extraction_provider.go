package memorycore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type MockLLM struct {
	FixedResponse     string
	RepairResponse    string
	RepairFailure     bool
	PrefilterResponse string
	ExtractCalls      int
	RepairCalls       int
	PreFilterCalls    int
}

func NewDeterministicMockLLM() *MockLLM {
	return &MockLLM{}
}

func (m *MockLLM) CompleteJSON(ctx context.Context, req ExtractionLLMRequest) (ExtractionLLMResponse, error) {
	switch req.Purpose {
	case ExtractionLLMPurposePreFilter:
		m.PreFilterCalls++
		if m.PrefilterResponse != "" {
			return ExtractionLLMResponse{Text: m.PrefilterResponse, Model: "mock"}, nil
		}
		return ExtractionLLMResponse{Text: deterministicPreFilterResponse(req), Model: "mock"}, nil
	case ExtractionLLMPurposeRepair:
		m.RepairCalls++
		if m.RepairFailure {
			return ExtractionLLMResponse{Text: "{", Model: "mock"}, nil
		}
		if m.RepairResponse != "" {
			return ExtractionLLMResponse{Text: m.RepairResponse, Model: "mock"}, nil
		}
		return ExtractionLLMResponse{Text: deterministicExtractionResponse(req), Model: "mock"}, nil
	case ExtractionLLMPurposeCuration:
		m.ExtractCalls++
		if m.FixedResponse != "" {
			return ExtractionLLMResponse{Text: m.FixedResponse, Model: "mock"}, nil
		}
		return ExtractionLLMResponse{Text: deterministicCurationResponse(req), Model: "mock"}, nil
	default:
		m.ExtractCalls++
		if m.FixedResponse != "" {
			return ExtractionLLMResponse{Text: m.FixedResponse, Model: "mock"}, nil
		}
		return ExtractionLLMResponse{Text: deterministicExtractionResponse(req), Model: "mock"}, nil
	}
}

func deterministicPreFilterResponse(req ExtractionLLMRequest) string {
	extractReq := requestFromUserPrompt(req)
	items := make([]ExtractionPreFilterEpisode, 0, len(extractReq.Episodes))
	for _, episode := range extractReq.Episodes {
		items = append(items, ExtractionPreFilterEpisode{
			EpisodeID:   episode.EpisodeID,
			Keep:        true,
			RoutingHint: "extract",
			ReasonCodes: []string{"mock_keep"},
		})
	}
	body := ExtractionPreFilterResponse{
		SchemaVersion: ExtractionPreFilterSchemaVersion,
		RequestID:     extractReq.RequestID,
		PersonaID:     extractReq.PersonaID,
		SessionID:     extractReq.SessionID,
		Trigger:       extractReq.Trigger,
		Episodes:      items,
		QualityFlags:  []string{},
	}
	data, _ := json.Marshal(body)
	return string(data)
}

func deterministicExtractionResponse(req ExtractionLLMRequest) string {
	extractReq := requestFromUserPrompt(req)
	episodeIDs := make([]string, 0, len(extractReq.Episodes))
	for _, episode := range extractReq.Episodes {
		episodeIDs = append(episodeIDs, episode.EpisodeID)
	}
	if len(episodeIDs) == 0 {
		episodeIDs = []string{"unknown"}
	}
	if target, ok := deterministicDeletionTarget(firstEpisodeContent(extractReq)); ok {
		body := map[string]any{
			"schema_version": ExtractionResponseSchemaVersion,
			"request_id":     extractReq.RequestID,
			"persona_id":     extractReq.PersonaID,
			"session_id":     extractReq.SessionID,
			"trigger":        extractReq.Trigger,
			"source_window":  map[string]any{"episode_ids": episodeIDs, "started_at": nil, "ended_at": nil},
			"entities":       []any{},
			"facts":          []any{},
			"links":          []any{},
			"affect_events":  []any{},
			"deletion_intents": []any{map[string]any{
				"candidate_id":          "mock_delete_1",
				"forget_level":          ForgetLevelSoft,
				"target_description":    target,
				"target_node_type_hint": ForgetNodeFact,
				"source_episode_id":     episodeIDs[0],
				"confidence":            0.9,
				"reasoning":             nil,
				"requires_confirmation": false,
			}},
			"pin_intents":         []any{},
			"correction_hints":    []any{},
			"rejected_candidates": []any{},
			"quality_flags":       []any{},
			"gate_summary":        map[string]any{"accepted_fact_count": 0, "needs_review_count": 0, "rejected_count": 0, "has_deletion_intent": true, "has_pin_intent": false, "requires_human_review": false, "notes": "mock deletion route"},
		}
		data, _ := json.Marshal(body)
		return string(data)
	}
	if extractReq.Trigger == ExtractionTriggerManualForget {
		body := map[string]any{
			"schema_version": ExtractionResponseSchemaVersion,
			"request_id":     extractReq.RequestID,
			"persona_id":     extractReq.PersonaID,
			"session_id":     extractReq.SessionID,
			"trigger":        extractReq.Trigger,
			"source_window":  map[string]any{"episode_ids": episodeIDs, "started_at": nil, "ended_at": nil},
			"entities":       []any{},
			"facts":          []any{mockOrdinaryFact(episodeIDs[0], "用户要求忘记一项内容。", "不要再提的内容")},
			"links":          []any{},
			"affect_events":  []any{},
			"deletion_intents": []any{map[string]any{
				"candidate_id":          "mock_delete_1",
				"forget_level":          "soft_forget",
				"target_description":    "用户要求不要再提的内容",
				"target_node_type_hint": "fact",
				"source_episode_id":     episodeIDs[0],
				"confidence":            0.9,
				"reasoning":             nil,
				"requires_confirmation": true,
			}},
			"pin_intents":         []any{},
			"correction_hints":    []any{},
			"rejected_candidates": []any{},
			"quality_flags":       []any{},
			"gate_summary":        map[string]any{"accepted_fact_count": 0, "needs_review_count": 0, "rejected_count": 1, "has_deletion_intent": true, "has_pin_intent": false, "requires_human_review": false, "notes": "mock route"},
		}
		data, _ := json.Marshal(body)
		return string(data)
	}
	object := "用户提到的重要偏好"
	summary := "用户提到了一项重要偏好。"
	if strings.Contains(firstEpisodeContent(extractReq), "早上八点") {
		object = "早上八点开会"
		summary = "用户不喜欢早上八点开会。"
	} else if strings.Contains(firstEpisodeContent(extractReq), "咖啡") {
		object = "手冲咖啡"
		summary = "用户喜欢手冲咖啡。"
	}
	body := map[string]any{
		"schema_version":      ExtractionResponseSchemaVersion,
		"request_id":          extractReq.RequestID,
		"persona_id":          extractReq.PersonaID,
		"session_id":          extractReq.SessionID,
		"trigger":             extractReq.Trigger,
		"source_window":       map[string]any{"episode_ids": episodeIDs, "started_at": nil, "ended_at": nil},
		"entities":            []any{},
		"facts":               []any{mockOrdinaryFact(episodeIDs[0], summary, object)},
		"links":               []any{},
		"affect_events":       []any{},
		"deletion_intents":    []any{},
		"pin_intents":         []any{},
		"correction_hints":    []any{},
		"rejected_candidates": []any{},
		"quality_flags":       []any{},
		"gate_summary":        map[string]any{"accepted_fact_count": 1, "needs_review_count": 0, "rejected_count": 0, "has_deletion_intent": false, "has_pin_intent": false, "requires_human_review": false, "notes": "mock"},
	}
	data, _ := json.Marshal(body)
	return string(data)
}

func mockOrdinaryFact(episodeID string, summary string, object string) map[string]any {
	return map[string]any{
		"candidate_id":                "mock_fact_1",
		"subject_entity_candidate_id": "user",
		"predicate":                   predicateForSummary(summary),
		"object_entity_candidate_id":  nil,
		"object_literal":              object,
		"content_summary":             summary,
		"fact_type":                   FactTypeStablePreference,
		"valid_from":                  nil,
		"valid_to":                    nil,
		"temporal_precision":          "unknown",
		"extraction_confidence":       ConfidenceExplicit,
		"extraction_confidence_score": 0.9,
		"importance":                  0.7,
		"valence":                     -0.2,
		"arousal":                     0.3,
		"sensitivity_level":           SensitivityNormal,
		"source_episode_ids":          []string{episodeID},
		"evidence_notes":              nil,
		"reasoning":                   nil,
		"operation_hint":              "insert_candidate",
		"pinned":                      false,
		"user_requested":              false,
		"searchable_hint":             true,
		"quality_decision":            "accept_for_consolidation",
		"quality_reasons":             []string{"mock"},
	}
}

func requestFromUserPrompt(req ExtractionLLMRequest) ExtractionRequest {
	var extractReq ExtractionRequest
	_ = json.Unmarshal([]byte(req.UserPrompt), &extractReq)
	return extractReq
}

func firstEpisodeContent(req ExtractionRequest) string {
	if len(req.Episodes) == 0 {
		return ""
	}
	return req.Episodes[0].Content
}

func deterministicDeletionTarget(content string) (string, bool) {
	lower := strings.ToLower(content)
	if !(strings.Contains(lower, "do not mention") || strings.Contains(lower, "don't mention") || strings.Contains(lower, "forget") || strings.Contains(lower, "delete")) {
		return "", false
	}
	switch {
	case strings.Contains(lower, "green tea"):
		return "green tea", true
	case strings.Contains(lower, "coffee"):
		return "coffee", true
	default:
		return strings.TrimSpace(content), true
	}
}

func predicateForSummary(summary string) string {
	if strings.Contains(summary, "不喜欢") {
		return "dislikes"
	}
	return "likes"
}

type OpenAICompatibleOptions struct {
	BaseURL        string
	APIKeyEnv      string
	Model          string
	Timeout        time.Duration
	Temperature    float64
	MaxTokens      int
	ResponseFormat ExtractionResponseFormat
	Thinking       *OpenAICompatibleThinkingOptions
	HTTPClient     *http.Client
}

type OpenAICompatibleThinkingOptions struct {
	Type string
}

type OpenAICompatibleLLM struct {
	opts OpenAICompatibleOptions
}

func NewOpenAICompatibleLLM(opts OpenAICompatibleOptions) *OpenAICompatibleLLM {
	if opts.APIKeyEnv == "" {
		opts.APIKeyEnv = "MEMORYCORE_LLM_API_KEY"
	}
	if opts.Timeout == 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: opts.Timeout}
	}
	return &OpenAICompatibleLLM{opts: opts}
}

func (l *OpenAICompatibleLLM) CompleteJSON(ctx context.Context, req ExtractionLLMRequest) (ExtractionLLMResponse, error) {
	apiKey := os.Getenv(l.opts.APIKeyEnv)
	if strings.TrimSpace(apiKey) == "" {
		return ExtractionLLMResponse{}, fmt.Errorf("api key env %s is not set", l.opts.APIKeyEnv)
	}
	baseURL := strings.TrimRight(l.opts.BaseURL, "/")
	if baseURL == "" {
		return ExtractionLLMResponse{}, fmt.Errorf("base url is required")
	}
	model := firstNonEmpty(req.Model, l.opts.Model)
	if model == "" {
		return ExtractionLLMResponse{}, fmt.Errorf("model is required")
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": req.SystemPrompt},
			{"role": "user", "content": strings.TrimSpace(req.DeveloperPrompt + "\n\n" + req.UserPrompt)},
		},
		"temperature":     firstFloat(req.Temperature, l.opts.Temperature),
		"max_tokens":      firstInt(req.MaxTokens, l.opts.MaxTokens),
		"response_format": buildOpenAIResponseFormat(firstResponseFormat(req.ResponseFormat, l.opts.ResponseFormat)),
	}
	if thinking, err := thinkingPayload(l.opts.Thinking); err != nil {
		return ExtractionLLMResponse{}, err
	} else if thinking != nil {
		payload["thinking"] = thinking
	}
	providerResp := ExtractionLLMResponse{ProviderRequestBody: payload}
	data, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return ExtractionLLMResponse{}, fmt.Errorf("create request failed")
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := l.opts.HTTPClient.Do(httpReq)
	if err != nil {
		return ExtractionLLMResponse{}, fmt.Errorf("provider request failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	providerResp.ProviderRawResponse = string(body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return providerResp, fmt.Errorf("provider returned status %d", resp.StatusCode)
	}
	var decoded struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return providerResp, fmt.Errorf("provider response decode failed")
	}
	if len(decoded.Choices) == 0 {
		return providerResp, fmt.Errorf("provider response had no choices")
	}
	content := decoded.Choices[0].Message.Content
	providerResp.Text = content
	providerResp.Model = firstNonEmpty(decoded.Model, model)
	providerResp.RawFinishReason = decoded.Choices[0].FinishReason
	providerResp.Usage = LLMUsage{
		PromptTokens:     decoded.Usage.PromptTokens,
		CompletionTokens: decoded.Usage.CompletionTokens,
		TotalTokens:      decoded.Usage.TotalTokens,
	}
	if strings.TrimSpace(content) == "" {
		return providerResp, fmt.Errorf("provider response content was empty")
	}
	return providerResp, nil
}

func thinkingPayload(opts *OpenAICompatibleThinkingOptions) (map[string]string, error) {
	if opts == nil {
		return nil, nil
	}
	switch strings.TrimSpace(strings.ToLower(opts.Type)) {
	case "enabled":
		return map[string]string{"type": "enabled"}, nil
	case "disabled":
		return map[string]string{"type": "disabled"}, nil
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("thinking.type must be enabled or disabled")
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstFloat(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func firstResponseFormat(values ...ExtractionResponseFormat) ExtractionResponseFormat {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ExtractionResponseFormatJSONObject
}

func buildOpenAIResponseFormat(format ExtractionResponseFormat) any {
	switch format {
	case ExtractionResponseFormatJSONSchema:
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "memory_extraction_response",
				"strict": false,
				"schema": map[string]any{"type": "object"},
			},
		}
	case ExtractionResponseFormatJSONObject, ExtractionResponseFormatDefault:
		return map[string]any{"type": "json_object"}
	default:
		return map[string]any{"type": "json_object"}
	}
}
