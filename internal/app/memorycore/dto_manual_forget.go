package memorycore

import "time"

const (
	ManualForgetIntentNone       = "none"
	ManualForgetIntentPin        = "pin"
	ManualForgetIntentForget     = "forget"
	ManualForgetIntentCorrection = "correction"
	ManualForgetIntentUnclear    = "unclear"

	ManualRuleHintForget = "forget"
	ManualRuleHintPin    = "pin"

	ManualForgetStatusIgnored             = "ignored"
	ManualForgetStatusExecutable          = "executable"
	ManualForgetStatusNeedsConfirmation   = "needs_confirmation"
	ManualForgetStatusPendingConfirmation = "pending_confirmation"
	ManualForgetStatusNoMatch             = "no_match"
	ManualForgetStatusExecuted            = "executed"
	ManualForgetStatusCancelled           = "cancelled"
	ManualForgetStatusFailed              = "failed"

	ManualForgetActionAutoExecute        = "auto_execute"
	ManualForgetActionAskLLMConfirmation = "ask_llm_confirmation"
	ManualForgetActionNoMatchReply       = "no_match_reply"

	ForgetConfirmationConfirm = "confirm"
	ForgetConfirmationDeny    = "deny"
	ForgetConfirmationSelect  = "select"
	ForgetConfirmationModify  = "modify"
	ForgetConfirmationUnclear = "unclear"

	MemoryOperationContextSchemaVersion = "memory_operation_context.v0.1"
	MemoryOperationLLMPurposeDirective  = "memory_operation_directive"
	MemoryOperationLLMPurposeConfirm    = "memory_operation_confirmation"
)

type ManualRuleHint struct {
	Kind   string `json:"kind,omitempty"`
	Prefix string `json:"prefix,omitempty"`
}

type RecentPromptMemoryRef struct {
	NodeType string  `json:"node_type"`
	NodeID   string  `json:"node_id"`
	Summary  string  `json:"summary,omitempty"`
	Score    float64 `json:"score,omitempty"`
}

type ManualForgetDirectiveRequest struct {
	PersonaID        string                  `json:"persona_id,omitempty"`
	SessionID        *string                 `json:"session_id,omitempty"`
	RequestEpisodeID string                  `json:"request_episode_id,omitempty"`
	UserText         string                  `json:"user_text"`
	RuleHint         *ManualRuleHint         `json:"rule_hint,omitempty"`
	RecentPromptRefs []RecentPromptMemoryRef `json:"recent_prompt_refs,omitempty"`
	Now              time.Time               `json:"now,omitempty"`
}

type ManualForgetDirectiveResult struct {
	Intent             string   `json:"intent"`
	Confidence         float64  `json:"confidence"`
	ForgetLevelHint    string   `json:"forget_level_hint,omitempty"`
	TargetDescription  string   `json:"target_description,omitempty"`
	TargetNodeTypeHint string   `json:"target_node_type_hint,omitempty"`
	RequiresLLMConfirm bool     `json:"requires_llm_confirm,omitempty"`
	ReasonCodes        []string `json:"reason_codes,omitempty"`
}

type PlanManualForgetRequest struct {
	PersonaID        string                      `json:"persona_id,omitempty"`
	SessionID        *string                     `json:"session_id,omitempty"`
	ChatSessionID    string                      `json:"chat_session_id,omitempty"`
	RequestEpisodeID string                      `json:"request_episode_id"`
	SourceEpisodeID  string                      `json:"source_episode_id,omitempty"`
	UserText         string                      `json:"user_text"`
	Directive        ManualForgetDirectiveResult `json:"directive"`
	RecentPromptRefs []RecentPromptMemoryRef     `json:"recent_prompt_refs,omitempty"`
	DryRun           bool                        `json:"dry_run,omitempty"`
	Now              time.Time                   `json:"now,omitempty"`
}

type PlanManualForgetResult struct {
	Status                 string                     `json:"status"`
	OperationID            string                     `json:"operation_id,omitempty"`
	RecommendedAction      string                     `json:"recommended_action,omitempty"`
	RequestedLevel         string                     `json:"requested_level,omitempty"`
	RequiresConfirmation   bool                       `json:"requires_confirmation,omitempty"`
	SuppressOrdinaryMemory bool                       `json:"suppress_ordinary_memory,omitempty"`
	Candidates             []ForgetCandidate          `json:"candidates,omitempty"`
	SafeSummary            string                     `json:"safe_summary,omitempty"`
	ConfirmationContext    *MemoryOperationLLMContext `json:"confirmation_context,omitempty"`
	ResultContext          *MemoryOperationLLMContext `json:"result_context,omitempty"`
}

type ForgetCandidate struct {
	TargetType      string  `json:"target_type"`
	TargetID        string  `json:"target_id"`
	Score           float64 `json:"score"`
	Source          string  `json:"source,omitempty"`
	DisplayID       string  `json:"display_id,omitempty"`
	SafeSummary     string  `json:"safe_summary"`
	NodeTypeLabel   string  `json:"node_type_label,omitempty"`
	EffectLabel     string  `json:"effect_label,omitempty"`
	RequiresConfirm bool    `json:"requires_confirmation,omitempty"`
}

type PendingManualForgetOperation struct {
	ID                   string            `json:"id"`
	PersonaID            string            `json:"persona_id"`
	SessionID            *string           `json:"session_id,omitempty"`
	ChatSessionID        string            `json:"chat_session_id,omitempty"`
	RequestEpisodeID     string            `json:"request_episode_id,omitempty"`
	Status               string            `json:"status"`
	RequestedLevel       string            `json:"requested_level"`
	ScopeMode            string            `json:"scope_mode,omitempty"`
	RequiresConfirmation bool              `json:"requires_confirmation"`
	Candidates           []ForgetCandidate `json:"candidates,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
	ExpiresAt            time.Time         `json:"expires_at"`
}

type GetPendingManualForgetOperationRequest struct {
	PersonaID     string    `json:"persona_id,omitempty"`
	SessionID     *string   `json:"session_id,omitempty"`
	ChatSessionID string    `json:"chat_session_id,omitempty"`
	Now           time.Time `json:"now,omitempty"`
}

type ClassifyForgetConfirmationRequest struct {
	PersonaID      string                     `json:"persona_id,omitempty"`
	SessionID      *string                    `json:"session_id,omitempty"`
	OperationID    string                     `json:"operation_id,omitempty"`
	UserReply      string                     `json:"user_reply"`
	PendingContext *MemoryOperationLLMContext `json:"pending_context,omitempty"`
}

type ClassifyForgetConfirmationResult struct {
	Decision          string   `json:"decision"`
	Confidence        float64  `json:"confidence"`
	SelectedTargetIDs []string `json:"selected_target_ids,omitempty"`
	ModifiedTarget    string   `json:"modified_target,omitempty"`
	FollowupHint      string   `json:"followup_hint,omitempty"`
	ReasonCodes       []string `json:"reason_codes,omitempty"`
}

type ExecuteManualForgetOperationRequest struct {
	PersonaID          string   `json:"persona_id,omitempty"`
	OperationID        string   `json:"operation_id"`
	ConfirmedTargetIDs []string `json:"confirmed_target_ids,omitempty"`
	Confirmed          bool     `json:"confirmed"`
	Actor              string   `json:"actor,omitempty"`
	ReasonCode         string   `json:"reason_code,omitempty"`
}

type ExecuteManualForgetOperationResult struct {
	Status               string                     `json:"status"`
	OperationID          string                     `json:"operation_id"`
	DeletionEventIDs     []string                   `json:"deletion_event_ids,omitempty"`
	DeletedCounts        map[string]int             `json:"deleted_counts,omitempty"`
	Level                string                     `json:"level,omitempty"`
	SafeSummaries        []string                   `json:"safe_summaries,omitempty"`
	VerifyPassed         bool                       `json:"verify_passed"`
	VerifyResult         *ForgetVerifyResult        `json:"verify_result,omitempty"`
	UserFacingLLMContext *MemoryOperationLLMContext `json:"user_facing_llm_context,omitempty"`
}

type MemoryOperationLLMContext struct {
	SchemaVersion      string                           `json:"schema_version"`
	OperationType      string                           `json:"operation_type"`
	Status             string                           `json:"status"`
	PendingOperationID string                           `json:"pending_operation_id,omitempty"`
	RequestedLevel     string                           `json:"requested_level,omitempty"`
	RiskLevel          string                           `json:"risk_level,omitempty"`
	SafeCandidateCount int                              `json:"safe_candidate_count,omitempty"`
	SafeCandidates     []MemoryOperationSafeCandidate   `json:"safe_candidates,omitempty"`
	DeletedCounts      map[string]int                   `json:"deleted_counts,omitempty"`
	Verify             *MemoryOperationVerifyContext    `json:"verify,omitempty"`
	SafeResultSummary  string                           `json:"safe_result_summary,omitempty"`
	AssistantGuidance  MemoryOperationAssistantGuidance `json:"assistant_guidance"`
}

type MemoryOperationSafeCandidate struct {
	DisplayID     string `json:"display_id"`
	SafeSummary   string `json:"safe_summary"`
	NodeTypeLabel string `json:"node_type_label,omitempty"`
	EffectLabel   string `json:"effect_label,omitempty"`
}

type MemoryOperationVerifyContext struct {
	Passed          bool `json:"passed"`
	SearchableAfter bool `json:"searchable_after"`
}

type MemoryOperationAssistantGuidance struct {
	AskConfirmation              bool     `json:"ask_confirmation,omitempty"`
	ReplyNaturally               bool     `json:"reply_naturally,omitempty"`
	Tone                         string   `json:"tone,omitempty"`
	Say                          string   `json:"say,omitempty"`
	SuggestedUserVisibleQuestion string   `json:"suggested_user_visible_question,omitempty"`
	DoNot                        []string `json:"do_not,omitempty"`
}
