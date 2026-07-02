package memorycore

import "time"

type ExtractionResponseFormat string

const (
	ExtractionResponseFormatDefault    ExtractionResponseFormat = ""
	ExtractionResponseFormatJSONObject ExtractionResponseFormat = "json_object"
	ExtractionResponseFormatJSONSchema ExtractionResponseFormat = "json_schema"
)

type RunExtractionRequest struct {
	RequestID string  `json:"request_id,omitempty"`
	PersonaID string  `json:"persona_id,omitempty"`
	SessionID *string `json:"session_id,omitempty"`
	Trigger   string  `json:"trigger,omitempty"`
	Timezone  string  `json:"timezone,omitempty"`

	Request *ExtractionRequest       `json:"request,omitempty"`
	Build   *ExtractionBuildSelector `json:"build,omitempty"`

	Mode     ExtractionRunMode          `json:"mode,omitempty"`
	Policy   ExtractionPolicyOverride   `json:"policy,omitempty"`
	Runtime  ExtractionRuntimeOverride  `json:"runtime,omitempty"`
	Provider ExtractionProviderOverride `json:"provider,omitempty"`

	SemanticDedup SemanticDedupOptions     `json:"semantic_dedup,omitempty"`
	Force         bool                     `json:"force,omitempty"`
	RawLog        *ExtractionRawLogOptions `json:"raw_log,omitempty"`
}

type ExtractionBuildSelector struct {
	EpisodeIDs []string   `json:"episode_ids,omitempty"`
	SessionID  *string    `json:"session_id,omitempty"`
	Since      *time.Time `json:"since,omitempty"`
	Until      *time.Time `json:"until,omitempty"`
	Limit      int        `json:"limit,omitempty"`
}

type ExtractionPolicyOverride struct {
	AllowSensitiveExtraction *bool    `json:"allow_sensitive_extraction,omitempty"`
	AllowInference           *bool    `json:"allow_inference,omitempty"`
	ManualPin                *bool    `json:"manual_pin,omitempty"`
	ManualForget             *bool    `json:"manual_forget,omitempty"`
	MaxFacts                 *int     `json:"max_facts,omitempty"`
	MaxLinks                 *int     `json:"max_links,omitempty"`
	DisallowedPredicates     []string `json:"disallowed_predicates,omitempty"`
	RequireCleanGate         *bool    `json:"require_clean_gate,omitempty"`
	ApplyAcceptedFacts       *bool    `json:"apply_accepted_facts,omitempty"`
	ExecuteDeletionIntents   *bool    `json:"execute_deletion_intents,omitempty"`
}

type ExtractionRuntimeOverride struct {
	UsePreFilter  *bool   `json:"use_prefilter,omitempty"`
	RepairEnabled *bool   `json:"repair_enabled,omitempty"`
	Audit         *string `json:"audit,omitempty"`
}

type ExtractionProviderOverride struct {
	Kind           string                           `json:"kind,omitempty"`
	ID             string                           `json:"id,omitempty"`
	BaseURL        string                           `json:"base_url,omitempty"`
	APIKeyEnv      string                           `json:"api_key_env,omitempty"`
	Model          string                           `json:"model,omitempty"`
	Temperature    *float64                         `json:"temperature,omitempty"`
	MaxTokens      *int                             `json:"max_tokens,omitempty"`
	Timeout        time.Duration                    `json:"timeout,omitempty"`
	ResponseFormat ExtractionResponseFormat         `json:"response_format,omitempty"`
	Thinking       *OpenAICompatibleThinkingOptions `json:"thinking,omitempty"`
}
