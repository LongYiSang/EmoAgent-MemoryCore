package memorycore

import "time"

type RunCurationRequest struct {
	PersonaID      string     `json:"persona_id,omitempty"`
	Mode           string     `json:"mode,omitempty"`
	Trigger        string     `json:"trigger,omitempty"`
	SinceCreatedAt *time.Time `json:"since_created_at,omitempty"`
	SinceFactID    string     `json:"since_fact_id,omitempty"`
	UntilCreatedAt *time.Time `json:"until_created_at,omitempty"`
	UntilFactID    string     `json:"until_fact_id,omitempty"`

	CandidateLimitPerFact  int     `json:"candidate_limit_per_fact,omitempty"`
	MaxNewFacts            int     `json:"max_new_facts,omitempty"`
	MaxFactsPerGroup       int     `json:"max_facts_per_group,omitempty"`
	MinAutoApplyConfidence float64 `json:"min_auto_apply_confidence,omitempty"`

	ProviderID   string        `json:"provider_id,omitempty"`
	ProviderKind string        `json:"provider_kind,omitempty"`
	Model        string        `json:"model,omitempty"`
	Temperature  float64       `json:"temperature,omitempty"`
	MaxTokens    int           `json:"max_tokens,omitempty"`
	Timeout      time.Duration `json:"timeout,omitempty"`

	Force            bool                   `json:"force,omitempty"`
	UpdateCheckpoint bool                   `json:"update_checkpoint,omitempty"`
	RawLog           *CurationRawLogOptions `json:"raw_log,omitempty"`
}

type CurationRawLogOptions struct {
	Enabled   bool   `json:"enabled,omitempty"`
	Directory string `json:"directory,omitempty"`
}

type RunCurationResult struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
	Mode   string `json:"mode"`

	NewFactCount      int `json:"new_fact_count"`
	GroupCount        int `json:"group_count"`
	LLMGroupCount     int `json:"llm_group_count"`
	AppliedGroupCount int `json:"applied_group_count"`
	ReviewGroupCount  int `json:"review_group_count"`
	NoopGroupCount    int `json:"noop_group_count"`
	ErrorCount        int `json:"error_count"`

	CursorFromCreatedAt *time.Time `json:"cursor_from_created_at,omitempty"`
	CursorFromFactID    string     `json:"cursor_from_fact_id,omitempty"`
	CursorToCreatedAt   *time.Time `json:"cursor_to_created_at,omitempty"`
	CursorToFactID      string     `json:"cursor_to_fact_id,omitempty"`

	Groups []CurationGroupResult `json:"groups,omitempty"`
}

type CurationGroupResult struct {
	GroupID          string   `json:"group_id"`
	GroupStatus      string   `json:"group_status"`
	Decision         string   `json:"decision"`
	SemanticRelation string   `json:"semantic_relation"`
	AnswerGain       string   `json:"answer_gain"`
	Confidence       float64  `json:"confidence"`
	CanonicalFactID  string   `json:"canonical_fact_id,omitempty"`
	SourceFactIDs    []string `json:"source_fact_ids,omitempty"`
	ReasonCodes      []string `json:"reason_codes,omitempty"`
}
