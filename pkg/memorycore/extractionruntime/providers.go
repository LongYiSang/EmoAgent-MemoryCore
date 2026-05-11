package extractionruntime

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

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
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

func (m *MockLLM) CompleteJSON(ctx context.Context, req memorycore.ExtractionLLMRequest) (memorycore.ExtractionLLMResponse, error) {
	switch req.Purpose {
	case memorycore.ExtractionLLMPurposePreFilter:
		m.PreFilterCalls++
		if m.PrefilterResponse != "" {
			return memorycore.ExtractionLLMResponse{Text: m.PrefilterResponse, Model: "mock"}, nil
		}
		return memorycore.ExtractionLLMResponse{Text: deterministicPreFilterResponse(req), Model: "mock"}, nil
	case memorycore.ExtractionLLMPurposeRepair:
		m.RepairCalls++
		if m.RepairFailure {
			return memorycore.ExtractionLLMResponse{Text: "{", Model: "mock"}, nil
		}
		if m.RepairResponse != "" {
			return memorycore.ExtractionLLMResponse{Text: m.RepairResponse, Model: "mock"}, nil
		}
		return memorycore.ExtractionLLMResponse{Text: deterministicExtractionResponse(req), Model: "mock"}, nil
	default:
		m.ExtractCalls++
		if m.FixedResponse != "" {
			return memorycore.ExtractionLLMResponse{Text: m.FixedResponse, Model: "mock"}, nil
		}
		return memorycore.ExtractionLLMResponse{Text: deterministicExtractionResponse(req), Model: "mock"}, nil
	}
}

func deterministicPreFilterResponse(req memorycore.ExtractionLLMRequest) string {
	var extractReq memorycore.ExtractionRequest
	_ = json.Unmarshal([]byte(req.Metadata["request_json"]), &extractReq)
	items := make([]memorycore.ExtractionPreFilterEpisode, 0, len(extractReq.Episodes))
	for _, episode := range extractReq.Episodes {
		items = append(items, memorycore.ExtractionPreFilterEpisode{
			EpisodeID:   episode.EpisodeID,
			Keep:        true,
			RoutingHint: "extract",
			ReasonCodes: []string{"mock_keep"},
		})
	}
	body := memorycore.ExtractionPreFilterResponse{
		SchemaVersion: memorycore.ExtractionPreFilterSchemaVersion,
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

func deterministicExtractionResponse(req memorycore.ExtractionLLMRequest) string {
	var extractReq memorycore.ExtractionRequest
	_ = json.Unmarshal([]byte(req.Metadata["request_json"]), &extractReq)
	episodeIDs := make([]string, 0, len(extractReq.Episodes))
	for _, episode := range extractReq.Episodes {
		episodeIDs = append(episodeIDs, episode.EpisodeID)
	}
	if len(episodeIDs) == 0 {
		episodeIDs = []string{"unknown"}
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
		"schema_version": memorycore.ExtractionResponseSchemaVersion,
		"request_id":     extractReq.RequestID,
		"persona_id":     extractReq.PersonaID,
		"session_id":     extractReq.SessionID,
		"trigger":        extractReq.Trigger,
		"source_window":  map[string]any{"episode_ids": episodeIDs, "started_at": nil, "ended_at": nil},
		"entities":       []any{},
		"facts": []any{map[string]any{
			"candidate_id":                "mock_fact_1",
			"subject_entity_candidate_id": "user",
			"predicate":                   predicateForSummary(summary),
			"object_entity_candidate_id":  nil,
			"object_literal":              object,
			"content_summary":             summary,
			"fact_type":                   memorycore.FactTypeStablePreference,
			"valid_from":                  nil,
			"valid_to":                    nil,
			"temporal_precision":          "unknown",
			"extraction_confidence":       memorycore.ConfidenceExplicit,
			"extraction_confidence_score": 0.9,
			"importance":                  0.7,
			"valence":                     -0.2,
			"arousal":                     0.3,
			"sensitivity_level":           memorycore.SensitivityNormal,
			"source_episode_ids":          []string{episodeIDs[0]},
			"evidence_notes":              nil,
			"reasoning":                   nil,
			"operation_hint":              "insert_candidate",
			"pinned":                      false,
			"user_requested":              false,
			"searchable_hint":             true,
			"quality_decision":            "accept_for_consolidation",
			"quality_reasons":             []string{"mock"},
		}},
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

func firstEpisodeContent(req memorycore.ExtractionRequest) string {
	if len(req.Episodes) == 0 {
		return ""
	}
	return req.Episodes[0].Content
}

func predicateForSummary(summary string) string {
	if strings.Contains(summary, "不喜欢") {
		return "dislikes"
	}
	return "likes"
}

type OpenAICompatibleOptions struct {
	BaseURL     string
	APIKeyEnv   string
	Model       string
	Timeout     time.Duration
	Temperature float64
	MaxTokens   int
	HTTPClient  *http.Client
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

func (l *OpenAICompatibleLLM) CompleteJSON(ctx context.Context, req memorycore.ExtractionLLMRequest) (memorycore.ExtractionLLMResponse, error) {
	apiKey := os.Getenv(l.opts.APIKeyEnv)
	if strings.TrimSpace(apiKey) == "" {
		return memorycore.ExtractionLLMResponse{}, fmt.Errorf("api key env %s is not set", l.opts.APIKeyEnv)
	}
	baseURL := strings.TrimRight(l.opts.BaseURL, "/")
	if baseURL == "" {
		return memorycore.ExtractionLLMResponse{}, fmt.Errorf("base url is required")
	}
	model := firstNonEmpty(req.Model, l.opts.Model)
	if model == "" {
		return memorycore.ExtractionLLMResponse{}, fmt.Errorf("model is required")
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": req.SystemPrompt},
			{"role": "user", "content": strings.TrimSpace(req.DeveloperPrompt + "\n\n" + req.UserPrompt)},
		},
		"temperature": firstFloat(req.Temperature, l.opts.Temperature),
		"max_tokens":  firstInt(req.MaxTokens, l.opts.MaxTokens),
		"response_format": map[string]string{
			"type": "json_object",
		},
	}
	data, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return memorycore.ExtractionLLMResponse{}, fmt.Errorf("create request failed")
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := l.opts.HTTPClient.Do(httpReq)
	if err != nil {
		return memorycore.ExtractionLLMResponse{}, fmt.Errorf("provider request failed")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return memorycore.ExtractionLLMResponse{}, fmt.Errorf("provider returned status %d", resp.StatusCode)
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
		return memorycore.ExtractionLLMResponse{}, fmt.Errorf("provider response decode failed")
	}
	if len(decoded.Choices) == 0 {
		return memorycore.ExtractionLLMResponse{}, fmt.Errorf("provider response had no choices")
	}
	return memorycore.ExtractionLLMResponse{
		Text:            decoded.Choices[0].Message.Content,
		Model:           firstNonEmpty(decoded.Model, model),
		RawFinishReason: decoded.Choices[0].FinishReason,
		Usage: memorycore.LLMUsage{
			PromptTokens:     decoded.Usage.PromptTokens,
			CompletionTokens: decoded.Usage.CompletionTokens,
			TotalTokens:      decoded.Usage.TotalTokens,
		},
	}, nil
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
