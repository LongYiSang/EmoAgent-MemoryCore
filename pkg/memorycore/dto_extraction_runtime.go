package memorycore

import (
	"context"
	"time"
)

const (
	ExtractionPreFilterSchemaVersion = "memory_extraction_prefilter.v0.1"

	ExtractionLLMPurposePreFilter  = "prefilter"
	ExtractionLLMPurposeExtraction = "extraction"
	ExtractionLLMPurposeRepair     = "repair"

	ExtractionAuditOn  = "on"
	ExtractionAuditOff = "off"
)

type ExtractionLLM interface {
	CompleteJSON(ctx context.Context, req ExtractionLLMRequest) (ExtractionLLMResponse, error)
}

type ExtractionLLMRequest struct {
	Purpose         string            `json:"purpose"`
	ProviderID      string            `json:"provider_id,omitempty"`
	ProviderKind    string            `json:"provider_kind,omitempty"`
	Model           string            `json:"model,omitempty"`
	SystemPrompt    string            `json:"system_prompt"`
	DeveloperPrompt string            `json:"developer_prompt,omitempty"`
	UserPrompt      string            `json:"user_prompt"`
	Temperature     float64           `json:"temperature"`
	MaxTokens       int               `json:"max_tokens"`
	Timeout         time.Duration     `json:"timeout"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type ExtractionLLMResponse struct {
	Text            string   `json:"text"`
	Model           string   `json:"model,omitempty"`
	Usage           LLMUsage `json:"usage,omitempty"`
	RawFinishReason string   `json:"raw_finish_reason,omitempty"`
}

type LLMUsage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
}

type ExtractionRunMode string

const (
	ExtractionRunModeValidate ExtractionRunMode = "validate"
	ExtractionRunModeDryRun   ExtractionRunMode = "dry-run"
	ExtractionRunModeApply    ExtractionRunMode = "apply"
)

type ExtractionRunStatus string

const (
	ExtractionRunStatusSkipped         ExtractionRunStatus = "skipped"
	ExtractionRunStatusValidated       ExtractionRunStatus = "validated"
	ExtractionRunStatusDryRun          ExtractionRunStatus = "dry_run"
	ExtractionRunStatusApplied         ExtractionRunStatus = "applied"
	ExtractionRunStatusNothingApplied  ExtractionRunStatus = "nothing_applied"
	ExtractionRunStatusBlocked         ExtractionRunStatus = "blocked"
	ExtractionRunStatusFailed          ExtractionRunStatus = "failed"
	ExtractionRunStatusPartiallyFailed ExtractionRunStatus = "partially_failed"
)

type ExtractionRunRequest struct {
	Request          ExtractionRequest   `json:"request"`
	Mode             ExtractionRunMode   `json:"mode"`
	ProviderID       string              `json:"provider_id,omitempty"`
	ProviderKind     string              `json:"provider_kind,omitempty"`
	Model            string              `json:"model,omitempty"`
	Temperature      float64             `json:"temperature,omitempty"`
	MaxTokens        int                 `json:"max_tokens,omitempty"`
	Timeout          time.Duration       `json:"timeout,omitempty"`
	UsePreFilter     bool                `json:"use_prefilter,omitempty"`
	RepairEnabled    bool                `json:"repair_enabled,omitempty"`
	RequireCleanGate bool                `json:"require_clean_gate,omitempty"`
	Audit            string              `json:"audit,omitempty"`
	Force            bool                `json:"force,omitempty"`
	Window           ExtractionRunWindow `json:"window,omitempty"`
}

type ExtractionRunWindow struct {
	EpisodeIDs []string   `json:"episode_ids,omitempty"`
	Since      *time.Time `json:"since,omitempty"`
	Until      *time.Time `json:"until,omitempty"`
	Limit      int        `json:"limit,omitempty"`
}

type ExtractionRunResult struct {
	RequestID             string                  `json:"request_id"`
	PersonaID             string                  `json:"persona_id"`
	SessionID             *string                 `json:"session_id,omitempty"`
	Trigger               string                  `json:"trigger"`
	Mode                  ExtractionRunMode       `json:"mode"`
	Status                ExtractionRunStatus     `json:"status"`
	Fingerprint           string                  `json:"fingerprint,omitempty"`
	SkippedByFingerprint  bool                    `json:"skipped_by_fingerprint,omitempty"`
	OriginalEpisodeCount  int                     `json:"original_episode_count"`
	KeptEpisodeCount      int                     `json:"kept_episode_count"`
	SkippedEpisodeCount   int                     `json:"skipped_episode_count"`
	PreFilterReviewCount  int                     `json:"prefilter_review_count,omitempty"`
	Repaired              bool                    `json:"repaired,omitempty"`
	GateResult            *ExtractionGateResult   `json:"gate_result,omitempty"`
	DryRunResult          *ExtractionDryRunResult `json:"dry_run_result,omitempty"`
	ApplyResult           *ExtractionApplyResult  `json:"apply_result,omitempty"`
	AcceptedCount         int                     `json:"accepted_count"`
	ReviewCount           int                     `json:"review_count"`
	RejectedCount         int                     `json:"rejected_count"`
	RoutedCount           int                     `json:"routed_count"`
	NotAppliedCount       int                     `json:"not_applied_count"`
	AppliedCount          int                     `json:"applied_count"`
	FailureCount          int                     `json:"failure_count"`
	Usage                 LLMUsage                `json:"usage,omitempty"`
	SanitizedErrorCode    string                  `json:"sanitized_error_code,omitempty"`
	SanitizedErrorMessage string                  `json:"sanitized_error_message,omitempty"`
	DurationMS            int64                   `json:"duration_ms,omitempty"`
}

type ExtractionPreFilterResponse struct {
	SchemaVersion string                       `json:"schema_version"`
	RequestID     string                       `json:"request_id"`
	PersonaID     string                       `json:"persona_id"`
	SessionID     *string                      `json:"session_id"`
	Trigger       string                       `json:"trigger"`
	Episodes      []ExtractionPreFilterEpisode `json:"episodes"`
	QualityFlags  []string                     `json:"quality_flags"`
}

type ExtractionPreFilterEpisode struct {
	EpisodeID   string   `json:"episode_id"`
	Keep        bool     `json:"keep"`
	RoutingHint string   `json:"routing_hint"`
	ReasonCodes []string `json:"reason_codes"`
}

type ExtractionBatchRequest struct {
	PersonaID        string            `json:"persona_id,omitempty"`
	SessionIDs       []string          `json:"session_ids,omitempty"`
	Trigger          string            `json:"trigger,omitempty"`
	Mode             ExtractionRunMode `json:"mode"`
	ProviderID       string            `json:"provider_id,omitempty"`
	ProviderKind     string            `json:"provider_kind,omitempty"`
	Model            string            `json:"model,omitempty"`
	Temperature      float64           `json:"temperature,omitempty"`
	MaxTokens        int               `json:"max_tokens,omitempty"`
	Timeout          time.Duration     `json:"timeout,omitempty"`
	Limit            int               `json:"limit,omitempty"`
	Since            *time.Time        `json:"since,omitempty"`
	Until            *time.Time        `json:"until,omitempty"`
	UsePreFilter     bool              `json:"use_prefilter,omitempty"`
	RepairEnabled    bool              `json:"repair_enabled,omitempty"`
	RequireCleanGate bool              `json:"require_clean_gate,omitempty"`
	Audit            string            `json:"audit,omitempty"`
	Force            bool              `json:"force,omitempty"`
	StopOnError      bool              `json:"stop_on_error,omitempty"`
}

type ExtractionBatchResult struct {
	Mode           ExtractionRunMode     `json:"mode"`
	Status         string                `json:"status"`
	ProcessedCount int                   `json:"processed_count"`
	SkippedCount   int                   `json:"skipped_count"`
	FailedCount    int                   `json:"failed_count"`
	Results        []ExtractionRunResult `json:"results"`
}

type ExtractionRunAuditRecord struct {
	ID                     string
	RequestID              string
	PersonaID              string
	SessionID              *string
	Trigger                string
	Mode                   ExtractionRunMode
	Status                 ExtractionRunStatus
	Fingerprint            string
	ProviderID             string
	ProviderKind           string
	Model                  string
	PromptVersion          string
	PreFilterPromptVersion string
	RepairPromptVersion    string
	OriginalEpisodeCount   int
	KeptEpisodeCount       int
	SkippedEpisodeCount    int
	AcceptedCount          int
	ReviewCount            int
	RejectedCount          int
	RoutedCount            int
	NotAppliedCount        int
	AppliedCount           int
	FailureCount           int
	PromptHash             string
	ResponseHash           string
	RepairedResponseHash   string
	PreFilterHash          string
	Usage                  LLMUsage
	LatencyMS              int64
	DurationMS             int64
	SanitizedErrorCode     string
	SanitizedErrorMessage  string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}
