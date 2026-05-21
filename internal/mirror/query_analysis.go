package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	sidecarQueryAnalysisRequestSchemaVersion  = "memory_query_analysis_request.v0.1"
	sidecarQueryAnalysisResponseSchemaVersion = "memory_query_analysis_result.v0.1"
)

type QueryAnalysisRequest struct {
	RequestID          string                    `json:"request_id,omitempty"`
	PersonaID          string                    `json:"persona_id"`
	SessionID          *string                   `json:"session_id,omitempty"`
	MessageID          *string                   `json:"message_id,omitempty"`
	QueryText          string                    `json:"query_text"`
	SemanticMode       string                    `json:"semantic_mode,omitempty"`
	Now                time.Time                 `json:"now"`
	Timezone           string                    `json:"timezone,omitempty"`
	RuleAnalysis       QueryAnalysis             `json:"rule_analysis"`
	VisibleEntityHints []VisibleEntityHint       `json:"visible_entity_hints"`
	AllowedEnums       QueryAnalysisAllowedEnums `json:"allowed_enums"`
	RetrievalPolicy    RetrievalPolicy           `json:"retrieval_policy"`
	DeadlineMS         int                       `json:"deadline_ms,omitempty"`
	ProviderTimeoutMS  int                       `json:"provider_timeout_ms,omitempty"`
	Debug              QueryAnalysisDebug        `json:"debug"`
}

type QueryAnalysis struct {
	Raw               string                           `json:"raw,omitempty"`
	Normalized        string                           `json:"normalized,omitempty"`
	Terms             []string                         `json:"terms,omitempty"`
	EntityMentions    []QueryEntityMention             `json:"entity_mentions,omitempty"`
	TimeMode          string                           `json:"time_mode,omitempty"`
	SemanticMode      string                           `json:"semantic_mode,omitempty"`
	Signals           []string                         `json:"signals,omitempty"`
	MemoryDomain      string                           `json:"memory_domain,omitempty"`
	MemoryAbility     string                           `json:"memory_ability,omitempty"`
	EvidenceNeed      string                           `json:"evidence_need,omitempty"`
	Source            string                           `json:"source,omitempty"`
	Confidence        float64                          `json:"confidence,omitempty"`
	FieldConfidence   QueryAnalysisConfidence          `json:"field_confidence,omitempty"`
	FieldProposals    map[string]SemanticFieldProposal `json:"field_proposals,omitempty"`
	Scores            QueryAnalysisScores              `json:"scores,omitempty"`
	Probes            QueryAnchorProbe                 `json:"probes,omitempty"`
	Decision          QueryAnalysisDecision            `json:"decision,omitempty"`
	Evidence          []QueryAnalysisEvidence          `json:"evidence,omitempty"`
	Alternatives      []QueryAnalysisAlternative       `json:"alternatives,omitempty"`
	QueryRewrites     []QueryRewrite                   `json:"query_rewrites,omitempty"`
	SemanticAnchors   []SemanticAnchor                 `json:"semantic_anchors,omitempty"`
	Subqueries        []string                         `json:"subqueries,omitempty"`
	SafetyNotes       []string                         `json:"safety_notes,omitempty"`
	ContextBlockHints []string                         `json:"context_block_hints,omitempty"`
	PolicyHints       QueryPolicyHints                 `json:"policy_hints,omitempty"`
}

type QueryEntityMention struct {
	EntityID      string  `json:"entity_id"`
	CanonicalName string  `json:"canonical_name,omitempty"`
	Alias         string  `json:"alias,omitempty"`
	MatchText     string  `json:"match_text,omitempty"`
	MatchKind     string  `json:"match_kind,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

type QueryRewrite struct {
	Text    string  `json:"text"`
	Purpose string  `json:"purpose,omitempty"`
	Weight  float64 `json:"weight,omitempty"`
}

type SemanticAnchor struct {
	Text       string  `json:"text"`
	AnchorType string  `json:"anchor_type,omitempty"`
	EntityID   string  `json:"entity_id,omitempty"`
	Weight     float64 `json:"weight,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type QueryAnalysisConfidence struct {
	Overall          float64 `json:"overall,omitempty"`
	TimeMode         float64 `json:"time_mode,omitempty"`
	MemoryAbility    float64 `json:"memory_ability,omitempty"`
	MemoryDomain     float64 `json:"memory_domain,omitempty"`
	EvidenceNeed     float64 `json:"evidence_need,omitempty"`
	EntityResolution float64 `json:"entity_resolution,omitempty"`
}

type SemanticFieldProposal struct {
	Value      string   `json:"value,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
}

type QueryAnalysisScores struct {
	RuleFit                     float64 `json:"rule_fit,omitempty"`
	AnchorReadiness             float64 `json:"anchor_readiness,omitempty"`
	ExpectedRetrievalConfidence float64 `json:"expected_retrieval_confidence,omitempty"`
	SemanticNeed                float64 `json:"semantic_need,omitempty"`
	Complexity                  float64 `json:"complexity,omitempty"`
	Ambiguity                   float64 `json:"ambiguity,omitempty"`
	Specificity                 float64 `json:"specificity,omitempty"`
	SafetyRisk                  float64 `json:"safety_risk,omitempty"`
	IntentEvidence              float64 `json:"intent_evidence,omitempty"`
	TimeEvidence                float64 `json:"time_evidence,omitempty"`
	DomainEvidence              float64 `json:"domain_evidence,omitempty"`
	EvidenceNeedEvidence        float64 `json:"evidence_need_evidence,omitempty"`
	EntityResolution            float64 `json:"entity_resolution,omitempty"`
	FieldConsistency            float64 `json:"field_consistency,omitempty"`
	DefaultFallbackPenalty      float64 `json:"default_fallback_penalty,omitempty"`
	MultiIntentConflictPenalty  float64 `json:"multi_intent_conflict_penalty,omitempty"`
	SensitivityPenalty          float64 `json:"sensitivity_penalty,omitempty"`
}

type QueryAnchorProbe struct {
	EntityExactConf        float64                     `json:"entity_exact_conf,omitempty"`
	EntityAmbiguity        float64                     `json:"entity_ambiguity,omitempty"`
	SparseProbeConf        float64                     `json:"sparse_probe_conf,omitempty"`
	PredicateProbeConf     float64                     `json:"predicate_probe_conf,omitempty"`
	RecentProbeConf        float64                     `json:"recent_probe_conf,omitempty"`
	PinnedCoreProbeConf    float64                     `json:"pinned_core_probe_conf,omitempty"`
	NarrativeProbeConf     float64                     `json:"narrative_probe_conf,omitempty"`
	FallbackSearchHitCount int                         `json:"fallback_search_hit_count,omitempty"`
	Top1Score              float64                     `json:"top1_score,omitempty"`
	Top2Score              float64                     `json:"top2_score,omitempty"`
	Top1Margin             float64                     `json:"top1_margin,omitempty"`
	Breakdown              []QueryAnchorProbeBreakdown `json:"breakdown,omitempty"`
}

type QueryAnchorProbeBreakdown struct {
	Source      string  `json:"source,omitempty"`
	Status      string  `json:"status,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	HitCount    int     `json:"hit_count,omitempty"`
	TopScore    float64 `json:"top_score,omitempty"`
	SecondScore float64 `json:"second_score,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	Error       string  `json:"error,omitempty"`
}

type QueryAnalysisDecision struct {
	UseSemantic      bool     `json:"use_semantic,omitempty"`
	SemanticMode     string   `json:"semantic_mode,omitempty"`
	RetrievalMode    string   `json:"retrieval_mode,omitempty"`
	ReasonCodes      []string `json:"reason_codes,omitempty"`
	ThresholdVersion string   `json:"threshold_version,omitempty"`
	ScorerVersion    string   `json:"scorer_version,omitempty"`
}

type QueryAnalysisEvidence struct {
	Field     string  `json:"field,omitempty"`
	Signal    string  `json:"signal,omitempty"`
	MatchText string  `json:"match_text,omitempty"`
	SpanStart int     `json:"span_start,omitempty"`
	SpanEnd   int     `json:"span_end,omitempty"`
	Weight    float64 `json:"weight,omitempty"`
	Detector  string  `json:"detector,omitempty"`
}

type QueryAnalysisAlternative struct {
	Field       string   `json:"field,omitempty"`
	Value       string   `json:"value,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
	ReasonCodes []string `json:"reason_codes,omitempty"`
	Detector    string   `json:"detector,omitempty"`
}

type QueryPolicyHints struct {
	PreferEvidencedByLinks bool `json:"prefer_evidenced_by_links,omitempty"`
	PreferSupersedesLinks  bool `json:"prefer_supersedes_links,omitempty"`
	PreferCausalLinks      bool `json:"prefer_causal_links,omitempty"`
	PreferCounterexamples  bool `json:"prefer_counterexamples,omitempty"`
	PreferNarratives       bool `json:"prefer_narratives,omitempty"`
	MaxHopsHint            int  `json:"max_hops_hint,omitempty"`
}

type VisibleEntityHint struct {
	EntityID      string `json:"entity_id"`
	CanonicalName string `json:"canonical_name,omitempty"`
	Alias         string `json:"alias,omitempty"`
	MatchText     string `json:"match_text,omitempty"`
}

type QueryAnalysisAllowedEnums struct {
	TimeModes          []string `json:"time_modes,omitempty"`
	Signals            []string `json:"signals,omitempty"`
	MemoryDomains      []string `json:"memory_domains,omitempty"`
	MemoryAbilities    []string `json:"memory_abilities,omitempty"`
	EvidenceNeeds      []string `json:"evidence_needs,omitempty"`
	EntityMentionKinds []string `json:"entity_mention_kinds,omitempty"`
	ContextBlockHints  []string `json:"context_block_hints,omitempty"`
}

type RetrievalPolicy struct {
	SensitivityPermission string `json:"sensitivity_permission,omitempty"`
	AllowHistorical       bool   `json:"allow_historical,omitempty"`
	AllowDeepArchive      bool   `json:"allow_deep_archive,omitempty"`
	FinalMemoryCount      int    `json:"final_memory_count,omitempty"`
	ContextBudgetTokens   int    `json:"context_budget_tokens,omitempty"`
	UseFTS                bool   `json:"use_fts,omitempty"`
	UseMirror             bool   `json:"use_mirror,omitempty"`
}

type QueryAnalysisDebug struct {
	IncludeRationaleSummary bool `json:"include_rationale_summary"`
}

type QueryAnalysisResult struct {
	Status         string
	Degraded       bool
	FallbackReason string
	Provider       string
	Model          string
	PromptVersion  string
	Analysis       QueryAnalysis
}

type QueryAnalysisError struct {
	Reason string
	Detail string
	Err    error
}

func (e QueryAnalysisError) Error() string {
	if strings.TrimSpace(e.Detail) != "" {
		return e.Reason + ": " + e.Detail
	}
	if e.Err != nil {
		return e.Reason + ": " + e.Err.Error()
	}
	return e.Reason
}

func (e QueryAnalysisError) Unwrap() error {
	return e.Err
}

type sidecarQueryAnalysisRequest struct {
	SchemaVersion string `json:"schema_version"`
	QueryAnalysisRequest
}

type sidecarQueryAnalysisResponse struct {
	SchemaVersion  string         `json:"schema_version"`
	RequestID      string         `json:"request_id,omitempty"`
	Status         string         `json:"status"`
	Degraded       bool           `json:"degraded"`
	FallbackReason string         `json:"fallback_reason,omitempty"`
	Provider       string         `json:"provider,omitempty"`
	Model          string         `json:"model,omitempty"`
	PromptVersion  string         `json:"prompt_version,omitempty"`
	Analysis       *QueryAnalysis `json:"analysis,omitempty"`
}

func (c *SidecarClient) QueryAnalysis(ctx context.Context, request QueryAnalysisRequest) (QueryAnalysisResult, error) {
	endpoint, err := c.endpoint("/retrieval/query-analysis")
	if err != nil {
		return QueryAnalysisResult{}, err
	}
	requestID := queryAnalysisRequestID(request)
	request.RequestID = requestID
	body, err := json.Marshal(sidecarQueryAnalysisRequest{
		SchemaVersion:        sidecarQueryAnalysisRequestSchemaVersion,
		QueryAnalysisRequest: request,
	})
	if err != nil {
		return QueryAnalysisResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return QueryAnalysisResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return QueryAnalysisResult{}, fmt.Errorf("sidecar query analysis request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		reason := "semantic_sidecar_error"
		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusGatewayTimeout {
			reason = "semantic_unavailable"
		}
		return QueryAnalysisResult{}, QueryAnalysisError{Reason: reason, Detail: fmt.Sprintf("status %d", resp.StatusCode)}
	}
	var response sidecarQueryAnalysisResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return QueryAnalysisResult{}, QueryAnalysisError{Reason: "semantic_invalid_response", Detail: "decode", Err: err}
	}
	if response.SchemaVersion != sidecarQueryAnalysisResponseSchemaVersion {
		return QueryAnalysisResult{}, QueryAnalysisError{Reason: "semantic_protocol_error", Detail: "schema mismatch"}
	}
	if response.RequestID != requestID {
		return QueryAnalysisResult{}, QueryAnalysisError{Reason: "semantic_protocol_error", Detail: "request_id mismatch"}
	}
	if !isValidQueryAnalysisResponseStatus(response.Status) {
		return QueryAnalysisResult{}, QueryAnalysisError{Reason: "semantic_protocol_error", Detail: "status missing or unknown"}
	}
	if response.Status != "error" && response.Analysis == nil {
		return QueryAnalysisResult{}, QueryAnalysisError{Reason: "semantic_protocol_error", Detail: "analysis missing"}
	}
	analysis := QueryAnalysis{}
	if response.Analysis != nil {
		analysis = *response.Analysis
	}
	if response.Status != "error" && !hasRequiredQueryAnalysisFields(analysis) {
		return QueryAnalysisResult{}, QueryAnalysisError{Reason: "semantic_protocol_error", Detail: "analysis missing required fields"}
	}
	return QueryAnalysisResult{
		Status:         response.Status,
		Degraded:       response.Degraded,
		FallbackReason: response.FallbackReason,
		Provider:       response.Provider,
		Model:          response.Model,
		PromptVersion:  response.PromptVersion,
		Analysis:       analysis,
	}, nil
}

func hasRequiredQueryAnalysisFields(analysis QueryAnalysis) bool {
	return strings.TrimSpace(analysis.TimeMode) != "" &&
		strings.TrimSpace(analysis.MemoryDomain) != "" &&
		strings.TrimSpace(analysis.MemoryAbility) != "" &&
		strings.TrimSpace(analysis.EvidenceNeed) != ""
}

func isValidQueryAnalysisResponseStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "ok", "degraded", "error":
		return true
	default:
		return false
	}
}

func QueryAnalysisErrorReason(err error) string {
	var qaErr QueryAnalysisError
	if errors.As(err, &qaErr) && strings.TrimSpace(qaErr.Reason) != "" {
		return qaErr.Reason
	}
	return ""
}

func queryAnalysisRequestID(request QueryAnalysisRequest) string {
	if requestID := strings.TrimSpace(request.RequestID); requestID != "" {
		return requestID
	}
	parts := []string{
		"query_analysis",
		strings.TrimSpace(request.PersonaID),
		strings.TrimSpace(request.QueryText),
	}
	if request.SessionID != nil {
		parts = append(parts, strings.TrimSpace(*request.SessionID))
	}
	if request.MessageID != nil {
		parts = append(parts, strings.TrimSpace(*request.MessageID))
	}
	return strings.Join(parts, ":")
}
