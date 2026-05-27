package memorycore

import "time"

const (
	ForgetScopeExactNode           = "exact_node"
	ForgetScopeRecentPromptItem    = "recent_prompt_item"
	ForgetScopeRecentEpisodeWindow = "recent_episode_window"
	ForgetScopeEntity              = "entity_scope"
	ForgetScopeBroadTopic          = "broad_topic"

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
	PersonaID         string
	ScopeMode         string
	NodeType          string
	NodeID            string
	EntityID          string
	Topic             string
	SessionID         string
	Since             *time.Time
	Until             *time.Time
	Limit             int
	RecentPromptItems []ForgetPromptItem
}

type ForgetPromptItem struct {
	NodeType string
	NodeID   string
	Summary  string
}

type ForgetPreviewResult struct {
	PersonaID            string
	ScopeMode            string
	Status               string
	RequiresConfirmation bool
	Reason               string
	Targets              []ForgetResolvedTarget
}

type ForgetResolvedTarget struct {
	NodeType    string
	NodeID      string
	Summary     string
	SafeSummary string
}

type ForgetExecuteRequest struct {
	PersonaID      string
	Actor          string
	ReasonCode     string
	Level          string
	PreviewRequest ForgetPreviewRequest
	Preview        ForgetPreviewResult
	Confirmed      bool
}

type ForgetExecuteResult struct {
	PersonaID string
	Executed  int
	Results   []ForgetResult
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
