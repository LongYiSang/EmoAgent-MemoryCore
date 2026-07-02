package memorycore

import "time"

const (
	ExtractionPreFilterSchemaVersion = "memory_extraction_protocol.v0.1.prefilter"

	ExtractionAuditOn  = "on"
	ExtractionAuditOff = "off"
)

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

type ExtractionRawLogOptions struct {
	Enabled   bool   `json:"enabled,omitempty"`
	Directory string `json:"directory,omitempty"`
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
	QualityFlags          []string                `json:"quality_flags,omitempty"`
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
	ForgetExecutedCount   int                     `json:"forget_executed_count"`
	ForgetFailureCount    int                     `json:"forget_failure_count"`
	Usage                 LLMUsage                `json:"usage,omitempty"`
	SanitizedErrorCode    string                  `json:"sanitized_error_code,omitempty"`
	SanitizedErrorMessage string                  `json:"sanitized_error_message,omitempty"`
	RoutedDeletionIntents []DeletionIntentRoute   `json:"routed_deletion_intents,omitempty"`
	RoutedForgetPreviews  []RoutedForgetPreview   `json:"routed_forget_previews,omitempty"`
	RoutedPinIntents      []PinIntentRoute        `json:"routed_pin_intents,omitempty"`
	DedupDiagnostics      *DedupDiagnostics       `json:"dedup_diagnostics,omitempty"`
	DurationMS            int64                   `json:"duration_ms,omitempty"`
}

type DedupDiagnostics struct {
	Ran            bool            `json:"ran"`
	Shadow         bool            `json:"shadow"`
	Degraded       bool            `json:"degraded,omitempty"`
	SidecarStatus  string          `json:"sidecar_status,omitempty"`
	FallbackReason string          `json:"fallback_reason,omitempty"`
	CandidateCount int             `json:"candidate_count"`
	Decisions      []DedupDecision `json:"decisions,omitempty"`
}

type DedupDecision struct {
	CandidateID string  `json:"candidate_id,omitempty"`
	NodeType    string  `json:"node_type,omitempty"`
	NodeID      string  `json:"node_id,omitempty"`
	Similarity  float64 `json:"similarity,omitempty"`
	Action      string  `json:"action,omitempty"`
	Reason      string  `json:"reason,omitempty"`
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
	PersonaID                string                  `json:"persona_id,omitempty"`
	SessionIDs               []string                `json:"session_ids,omitempty"`
	Trigger                  string                  `json:"trigger,omitempty"`
	Mode                     ExtractionRunMode       `json:"mode"`
	ProviderID               string                  `json:"provider_id,omitempty"`
	ProviderKind             string                  `json:"provider_kind,omitempty"`
	Model                    string                  `json:"model,omitempty"`
	Temperature              float64                 `json:"temperature,omitempty"`
	MaxTokens                int                     `json:"max_tokens,omitempty"`
	Timeout                  time.Duration           `json:"timeout,omitempty"`
	Limit                    int                     `json:"limit,omitempty"`
	EpisodeLimit             int                     `json:"episode_limit,omitempty"`
	Timezone                 string                  `json:"timezone,omitempty"`
	AllowSensitiveExtraction bool                    `json:"allow_sensitive_extraction,omitempty"`
	AllowInference           bool                    `json:"allow_inference,omitempty"`
	ManualPin                bool                    `json:"manual_pin,omitempty"`
	ManualForget             bool                    `json:"manual_forget,omitempty"`
	MaxFacts                 int                     `json:"max_facts,omitempty"`
	MaxLinks                 int                     `json:"max_links,omitempty"`
	DisallowedPredicates     []string                `json:"disallowed_predicates,omitempty"`
	Since                    *time.Time              `json:"since,omitempty"`
	Until                    *time.Time              `json:"until,omitempty"`
	UsePreFilter             bool                    `json:"use_prefilter,omitempty"`
	RepairEnabled            bool                    `json:"repair_enabled,omitempty"`
	RequireCleanGate         bool                    `json:"require_clean_gate,omitempty"`
	Audit                    string                  `json:"audit,omitempty"`
	Force                    bool                    `json:"force,omitempty"`
	StopOnError              bool                    `json:"stop_on_error,omitempty"`
	AllowPartialFailure      bool                    `json:"allow_partial_failure,omitempty"`
	RawLog                   ExtractionRawLogOptions `json:"raw_log,omitempty"`
}

type ExtractionBatchResult struct {
	Mode           ExtractionRunMode     `json:"mode"`
	Status         string                `json:"status"`
	ProcessedCount int                   `json:"processed_count"`
	SkippedCount   int                   `json:"skipped_count"`
	FailedCount    int                   `json:"failed_count"`
	Results        []ExtractionRunResult `json:"results"`
}
