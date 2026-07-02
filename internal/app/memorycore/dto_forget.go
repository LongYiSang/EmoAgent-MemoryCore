package memorycore

import "time"

const (
	ForgetScopeExactNode           = "exact_node"
	ForgetScopeRecentPromptItem    = "recent_prompt_item"
	ForgetScopeRecentEpisodeWindow = "recent_episode_window"
	ForgetScopeEntity              = "entity_scope"
	ForgetScopeBroadTopic          = "broad_topic"
	ForgetScopeSemanticQuery       = "semantic_query"
	ForgetScopePredicate           = "predicate"

	ForgetNodeFact    = "fact"
	ForgetNodeEpisode = "episode"

	ForgetActorUser   = "user"
	ForgetActorSystem = "system"
	ForgetActorAdmin  = "admin"

	ForgetReasonUserRequested   = "user_requested"
	ForgetReasonRetentionPolicy = "retention_policy"
	ForgetReasonSafety          = "safety"
	ForgetReasonAdminPolicy     = "admin_policy"

	ForgetLevelSoft         = "soft_forget"
	ForgetLevelHard         = "hard_forget"
	ForgetLevelSourceRedact = "source_redact"
	ForgetLevelPurge        = "purge"

	ForgottenPlaceholder = "[forgotten]"
	RedactedPlaceholder  = "[redacted]"
)

type ForgetRequest struct {
	PersonaID  string
	Actor      string
	ReasonCode string
	Level      string
	Target     ForgetTarget
}

type ForgetTarget struct {
	ScopeMode string
	NodeType  string
	NodeID    string
}

type ForgetResult struct {
	DeletionEventID        string
	TargetNodeType         string
	TargetNodeID           string
	SearchDocumentsDeleted int64
	FTSRowsDeleted         int64
	MirrorDeletesEnqueued  int64
	LinksScrubbed          int64
}

type ForgetPreviewRequest struct {
	RequestID           string
	PersonaID           string
	Actor               string
	RequestedLevel      string
	ScopeMode           string
	NodeType            string
	NodeID              string
	EntityID            string
	Predicate           string
	Topic               string
	SessionID           string
	ChatSessionID       string
	RequestEpisodeID    string
	Since               *time.Time
	Until               *time.Time
	Limit               int
	SemanticQuery       *string
	RequireConfirmation bool
	RecentPromptItems   []ForgetPromptItem
}

type ForgetPromptItem struct {
	NodeType string
	NodeID   string
	Summary  string
}

type ForgetPreviewResult struct {
	PersonaID            string
	RequestID            string
	OperationID          string
	PreviewHash          string
	RequestedLevel       string
	ScopeMode            string
	Status               string
	RequiresConfirmation bool
	Reason               string
	RiskFlags            []string
	SidecarStatus        string
	Targets              []ForgetResolvedTarget
}

type ForgetResolvedTarget struct {
	NodeType      string
	NodeID        string
	Summary       string
	SafeSummary   string
	ObjectLiteral string
}

type ForgetExecuteRequest struct {
	PersonaID        string
	OperationID      string
	Actor            string
	ReasonCode       string
	Level            string
	PreviewRequest   ForgetPreviewRequest
	Preview          ForgetPreviewResult
	PreviewHash      string
	ConfirmedTargets []ExactNodeRef
	Confirmed        bool
}

type ForgetExecuteResult struct {
	PersonaID   string
	OperationID string
	Executed    int
	PreviewHash string
	Results     []ForgetResult
}

type ExactNodeRef struct {
	NodeType string
	NodeID   string
}

const (
	ManualForgetOperationStatusPendingConfirmation = "pending_confirmation"
	ManualForgetOperationStatusExecuted            = "executed"
	ManualForgetOperationStatusCancelled           = "cancelled"
	ManualForgetOperationStatusFailed              = "failed"
	ManualForgetOperationStatusExpired             = "expired"
)

type PendingManualForgetOperation struct {
	OperationID          string
	PersonaID            string
	SessionID            string
	ChatSessionID        string
	RequestEpisodeID     string
	Status               string
	RequestedLevel       string
	ScopeMode            string
	PreviewHash          string
	RequiresConfirmation bool
	Targets              []ForgetResolvedTarget
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ExpiresAt            time.Time
}

type GetPendingManualForgetOperationRequest struct {
	PersonaID     string
	ChatSessionID string
}

type CancelPendingManualForgetOperationRequest struct {
	PersonaID     string
	OperationID   string
	ChatSessionID string
}

type ForgetVerifyRequest struct {
	PersonaID string
	Targets   []ForgetResolvedTarget
}

type ForgetVerifyResult struct {
	PersonaID string
	Passed    bool
	Targets   []ForgetVerifyTargetResult
}

type ForgetVerifyTargetResult struct {
	NodeType             string
	NodeID               string
	Passed               bool
	VisibilityStatus     string
	Searchable           bool
	SearchDocumentsFound int
	FTSRowsFound         int
}
