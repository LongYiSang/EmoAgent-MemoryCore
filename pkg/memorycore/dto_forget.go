package memorycore

const (
	ForgetScopeExactNode = "exact_node"

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
