package memorycore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync"
	"time"
	"unicode"

	internalmirror "github.com/longyisang/emoagent-memorycore/internal/mirror"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const (
	defaultQueryAnalysisTimeout                 = 1500 * time.Millisecond
	defaultQueryAnalysisMinConfidenceToOverride = 0.72
	defaultQueryAnalysisMinEntityConfidence     = 0.70
	defaultQueryAnalysisMaxQueryRewrites        = 5
	defaultQueryAnalysisMaxSemanticAnchors      = 8
	defaultQueryAnalysisSemanticEnergyCap       = 5.0
	defaultQueryAnalysisGeneratedDenseWeightSum = 3.0
	defaultQueryAnalysisMinRuleFit              = 0.66
	defaultQueryAnalysisMinAnchorReadiness      = 0.45
	defaultQueryAnalysisSemanticNeed            = 0.58
	defaultQueryAnalysisMinComplexity           = 0.50
	defaultQueryAnalysisFullComplexity          = 0.72
	defaultQueryAnalysisDecomposeComplexity     = 0.80
	defaultQueryAnalysisMinFieldConfidence      = 0.70
	defaultQueryAnalysisOverrideMargin          = 0.08
	defaultQueryAnalysisHighSafetyRisk          = 0.80
)

const rewriteDropReasonLanguageMismatch = "rewrite_language_mismatch"

type QueryAnalyzer interface {
	AnalyzeQuery(ctx context.Context, req QueryAnalysisRequest) (memsqlite.QueryAnalysis, error)
}

type RuleQueryAnalyzer interface {
	AnalyzeRuleQuery(ctx context.Context, req QueryAnalysisRequest) (memsqlite.QueryAnalysis, error)
}

type SemanticQueryAnalyzer interface {
	AnalyzeSemanticQuery(ctx context.Context, req SemanticQueryAnalysisRequest) (*SemanticQueryAnalysisResult, error)
}

type QueryAnalysisCache struct {
	mu      sync.Mutex
	entries map[string]queryAnalysisCacheEntry
}

type queryAnalysisCacheEntry struct {
	result *SemanticQueryAnalysisResult
	err    error
}

func NewQueryAnalysisCache() *QueryAnalysisCache {
	return &QueryAnalysisCache{entries: map[string]queryAnalysisCacheEntry{}}
}

func (c *QueryAnalysisCache) Analyze(ctx context.Context, req SemanticQueryAnalysisRequest, analyzer SemanticQueryAnalyzer) (*SemanticQueryAnalysisResult, error) {
	if c == nil || analyzer == nil {
		if analyzer == nil {
			return nil, fmt.Errorf("semantic query analyzer is required")
		}
		return analyzer.AnalyzeSemanticQuery(ctx, req)
	}
	key := semanticQueryAnalysisCacheKey(req)
	if key == "" {
		return analyzer.AnalyzeSemanticQuery(ctx, req)
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]queryAnalysisCacheEntry{}
	}
	if entry, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return cloneSemanticQueryAnalysisResult(entry.result), entry.err
	}
	c.mu.Unlock()

	result, err := analyzer.AnalyzeSemanticQuery(ctx, req)
	if !shouldCacheSemanticQueryAnalysis(err) {
		return result, err
	}
	c.mu.Lock()
	if _, ok := c.entries[key]; !ok {
		c.entries[key] = queryAnalysisCacheEntry{result: cloneSemanticQueryAnalysisResult(result), err: err}
	}
	c.mu.Unlock()
	return result, err
}

func shouldCacheSemanticQueryAnalysis(err error) bool {
	return err == nil
}

func semanticQueryAnalysisCacheKey(req SemanticQueryAnalysisRequest) string {
	req.RequestID = ""
	data, err := json.Marshal(req)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneSemanticQueryAnalysisResult(value *SemanticQueryAnalysisResult) *SemanticQueryAnalysisResult {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		copy := *value
		return &copy
	}
	var out SemanticQueryAnalysisResult
	if err := json.Unmarshal(data, &out); err != nil {
		copy := *value
		return &copy
	}
	return &out
}

type QueryAnalysisRequest struct {
	RequestID string
	PersonaID string
	SessionID *string
	MessageID *string
	QueryText string
	Now       time.Time
	Timezone  string
	Policy    RetrievalPolicy
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

type SemanticQueryAnalysisRequest struct {
	RequestID                  string
	PersonaID                  string
	SessionID                  *string
	MessageID                  *string
	QueryText                  string
	SemanticMode               string
	Now                        time.Time
	Timezone                   string
	RuleAnalysis               memsqlite.QueryAnalysis
	VisibleEntityHints         []VisibleEntityHint
	AllowedEnums               QueryAnalysisAllowedEnums
	RetrievalPolicy            RetrievalPolicy
	DeadlineMS                 int
	ProviderTimeoutMS          int
	IncludeRationaleSummary    bool
	MaxGeneratedDenseWeightSum float64
}

type SemanticQueryAnalysisResult struct {
	Status         string
	Degraded       bool
	FallbackReason string
	Provider       string
	Model          string
	PromptVersion  string
	LatencyMs      int64
	Analysis       SemanticQueryAnalysis
}

type SemanticQueryAnalysis struct {
	TimeMode          string
	SemanticMode      string
	Signals           []string
	MemoryDomain      string
	MemoryAbility     string
	EvidenceNeed      string
	Confidence        float64
	FieldConfidence   QueryAnalysisConfidence
	FieldProposals    map[string]SemanticFieldProposal
	Scores            QueryAnalysisScores
	Probes            QueryAnchorProbe
	Decision          QueryAnalysisDecision
	Evidence          []QueryAnalysisEvidence
	Alternatives      []QueryAnalysisAlternative
	EntityMentions    []SemanticQueryEntityMention
	QueryRewrites     []QueryRewrite
	SemanticAnchors   []SemanticAnchor
	Subqueries        []string
	SafetyNotes       []string
	ContextBlockHints []string
	PolicyHints       QueryPolicyHints
}

type SemanticQueryEntityMention struct {
	EntityID      string
	CanonicalName string
	Alias         string
	MatchText     string
	MatchKind     string
	Confidence    float64
}

type queryAnalysisPipeline struct {
	rule     RuleQueryAnalyzer
	semantic SemanticQueryAnalyzer
	options  QueryAnalysisOptions
	now      func() time.Time
}

type storeRuleQueryAnalyzer struct {
	repo *memsqlite.RetrievalRepository
}

func (a storeRuleQueryAnalyzer) AnalyzeRuleQuery(ctx context.Context, req QueryAnalysisRequest) (memsqlite.QueryAnalysis, error) {
	if a.repo == nil {
		return memsqlite.QueryAnalysis{}, fmt.Errorf("query analysis rule repository is required")
	}
	return a.repo.AnalyzeQuery(ctx, req.PersonaID, req.QueryText, retrievalPolicyToStore(req.Policy))
}

func newQueryAnalysisPipeline(rule RuleQueryAnalyzer, semantic SemanticQueryAnalyzer, options QueryAnalysisOptions) queryAnalysisPipeline {
	return queryAnalysisPipeline{
		rule:     rule,
		semantic: semantic,
		options:  normalizeQueryAnalysisOptions(options),
		now:      time.Now,
	}
}

func (p queryAnalysisPipeline) AnalyzeQuery(ctx context.Context, req QueryAnalysisRequest) (memsqlite.QueryAnalysis, error) {
	rule, err := p.AnalyzeRuleQuery(ctx, req)
	if err != nil {
		return memsqlite.QueryAnalysis{}, err
	}
	return p.AnalyzeSemanticForRule(ctx, req, rule), nil
}

func (p queryAnalysisPipeline) AnalyzeRuleQuery(ctx context.Context, req QueryAnalysisRequest) (memsqlite.QueryAnalysis, error) {
	if p.rule == nil {
		return memsqlite.QueryAnalysis{}, fmt.Errorf("query analysis rule analyzer is required")
	}
	rule, err := p.rule.AnalyzeRuleQuery(ctx, req)
	if err != nil {
		return memsqlite.QueryAnalysis{}, err
	}
	rule.Source = memsqlite.QueryAnalysisSourceRuleOnly
	return rule, nil
}

func (p queryAnalysisPipeline) AnalyzeSemanticForRule(ctx context.Context, req QueryAnalysisRequest, rule memsqlite.QueryAnalysis) memsqlite.QueryAnalysis {
	options := normalizeQueryAnalysisOptions(p.options)
	legacyUseSemantic := p.shouldUseLegacySemantic(rule)
	adaptiveDecision := memsqlite.QueryAnalysisDecision{}
	if isAdaptiveQueryAnalysisMode(options.Mode) {
		adaptiveDecision = DecideSemanticRoute(rule, options)
	}
	useSemantic := legacyUseSemantic
	semanticMode := ""
	switch options.Mode {
	case QueryAnalysisModeAdaptive, QueryAnalysisModeAdaptiveSafe, QueryAnalysisModeAdaptiveFull:
		useSemantic = adaptiveDecision.UseSemantic
		semanticMode = adaptiveDecision.SemanticMode
	case QueryAnalysisModeShadowAdaptive:
		useSemantic = legacyUseSemantic
	}
	rule = annotateSemanticDecisions(rule, legacyUseSemantic, adaptiveDecision, options.MinConfidenceToOverride)
	if !useSemantic {
		return rule
	}
	if p.semantic == nil {
		return semanticRuleFallback(rule, "semantic_analyzer_missing", SemanticQueryAnalysisResult{})
	}
	semanticReq := p.semanticRequestForRule(req, rule, semanticMode)
	stageCtx := ctx
	cancel := func() {}
	if p.options.Timeout > 0 {
		stageCtx, cancel = context.WithTimeout(ctx, p.options.Timeout)
	}
	defer cancel()
	started := p.now
	if started == nil {
		started = time.Now
	}
	begin := started()
	semantic, err := p.analyzeSemantic(stageCtx, semanticReq)
	latencyMs := time.Since(begin).Milliseconds()
	if err != nil || semantic == nil {
		semanticResult := SemanticQueryAnalysisResult{LatencyMs: latencyMs}
		if semantic != nil {
			semanticResult = *semantic
			if semanticResult.LatencyMs <= 0 {
				semanticResult.LatencyMs = latencyMs
			}
		}
		return semanticRuleFallback(rule, semanticFallbackReasonFromError(ctx, err), semanticResult)
	}
	semantic.LatencyMs = latencyMs
	if !isValidSemanticQueryAnalysisStatus(semantic.Status) {
		return semanticRuleFallback(rule, "semantic_protocol_error", *semantic)
	}
	if semantic.Status != "ok" {
		return semanticRuleFallback(rule, sanitizeSemanticFallbackReason(semantic.FallbackReason, "semantic_sidecar_error"), *semantic)
	}
	if semantic.Degraded {
		return semanticRuleFallback(rule, sanitizeSemanticFallbackReason(semantic.FallbackReason, "semantic_unavailable"), *semantic)
	}
	return mergeSemanticQueryAnalysis(rule, *semantic, options, semanticReq.VisibleEntityHints)
}

func (p queryAnalysisPipeline) semanticRequestForRule(req QueryAnalysisRequest, rule memsqlite.QueryAnalysis, semanticMode string) SemanticQueryAnalysisRequest {
	providerBudget := queryAnalysisProviderBudget(p.options)
	return SemanticQueryAnalysisRequest{
		RequestID:                  req.RequestID,
		PersonaID:                  req.PersonaID,
		SessionID:                  req.SessionID,
		MessageID:                  req.MessageID,
		QueryText:                  req.QueryText,
		SemanticMode:               semanticMode,
		Now:                        req.Now,
		Timezone:                   req.Timezone,
		RuleAnalysis:               cloneStoreQueryAnalysis(rule),
		VisibleEntityHints:         visibleEntityHintsFromRule(rule),
		AllowedEnums:               defaultQueryAnalysisAllowedEnums(),
		RetrievalPolicy:            req.Policy,
		DeadlineMS:                 durationMillis(providerBudget),
		ProviderTimeoutMS:          durationMillis(providerBudget),
		IncludeRationaleSummary:    p.options.IncludeRationaleSummary,
		MaxGeneratedDenseWeightSum: p.options.MaxGeneratedDenseWeightSum,
	}
}

func queryAnalysisProviderBudget(options QueryAnalysisOptions) time.Duration {
	options = normalizeQueryAnalysisOptions(options)
	if options.SoftJoinTimeout > 0 {
		return options.SoftJoinTimeout
	}
	return options.Timeout
}

func (p queryAnalysisPipeline) analyzeSemantic(ctx context.Context, req SemanticQueryAnalysisRequest) (*SemanticQueryAnalysisResult, error) {
	if p.options.Cache == nil {
		return p.semantic.AnalyzeSemanticQuery(ctx, req)
	}
	return p.options.Cache.Analyze(ctx, req, p.semantic)
}

func (p queryAnalysisPipeline) shouldUseLegacySemantic(rule memsqlite.QueryAnalysis) bool {
	options := normalizeQueryAnalysisOptions(p.options)
	if options.Provider != QueryAnalysisProviderSidecar {
		return false
	}
	switch options.Mode {
	case QueryAnalysisModeSemanticAlways:
		return true
	case QueryAnalysisModeSemanticRewriteOnly:
		return true
	case QueryAnalysisModeSemanticOnLowConfidence:
		confidence := rule.Confidence
		if rule.Diagnostics != nil && rule.Diagnostics.RuleConfidenceReason != "" {
			confidence = rule.Diagnostics.RuleConfidenceLegacy
		}
		return confidence < options.MinConfidenceToOverride
	default:
		return false
	}
}

func (p queryAnalysisPipeline) shouldUseSemantic(rule memsqlite.QueryAnalysis) bool {
	options := normalizeQueryAnalysisOptions(p.options)
	if options.Provider != QueryAnalysisProviderSidecar {
		return false
	}
	switch options.Mode {
	case QueryAnalysisModeAdaptive, QueryAnalysisModeAdaptiveSafe, QueryAnalysisModeAdaptiveFull:
		return DecideSemanticRoute(rule, options).UseSemantic
	default:
		return p.shouldUseLegacySemantic(rule)
	}
}

func (p queryAnalysisPipeline) annotateRetrievalRuleDecision(rule memsqlite.QueryAnalysis) memsqlite.QueryAnalysis {
	options := normalizeQueryAnalysisOptions(p.options)
	legacyDecision := p.shouldUseLegacySemantic(rule)
	var adaptiveDecision memsqlite.QueryAnalysisDecision
	if isAdaptiveQueryAnalysisMode(options.Mode) {
		adaptiveDecision = DecideSemanticRoute(rule, options)
	}
	if !legacyDecision && isZeroStoreQueryAnalysisDecision(adaptiveDecision) {
		return rule
	}
	return annotateSemanticDecisions(rule, legacyDecision, adaptiveDecision, options.MinConfidenceToOverride)
}

func isAdaptiveQueryAnalysisMode(mode QueryAnalysisMode) bool {
	switch mode {
	case QueryAnalysisModeShadowAdaptive, QueryAnalysisModeAdaptive, QueryAnalysisModeAdaptiveSafe, QueryAnalysisModeAdaptiveFull:
		return true
	default:
		return false
	}
}

func DecideSemanticRoute(rule memsqlite.QueryAnalysis, options QueryAnalysisOptions) memsqlite.QueryAnalysisDecision {
	options = normalizeQueryAnalysisOptions(options)
	decision := memsqlite.QueryAnalysisDecision{
		SemanticMode:     "none",
		RetrievalMode:    "rule",
		ReasonCodes:      []string{"rule_and_anchor_sufficient"},
		ThresholdVersion: "adaptive_semantic_router_v1",
		ScorerVersion:    "query_analysis_scorer_v1",
	}
	if strings.TrimSpace(rule.Raw) == "" {
		decision.ReasonCodes = []string{"empty_query"}
		return decision
	}
	if hasStoreQuerySignal(rule.Signals, memsqlite.QuerySignalForgetDelete) || rule.MemoryAbility == memsqlite.MemoryAbilityBoundary {
		decision.SemanticMode = "target_resolver"
		decision.RetrievalMode = "target_resolver"
		decision.ReasonCodes = []string{"forget_or_delete_intent"}
		return decision
	}
	scores := rule.Scores
	if hasStoreQuerySignal(rule.Signals, memsqlite.QuerySignalSensitivity) ||
		(validUnitScore(scores.SafetyRisk) && scores.SafetyRisk >= options.HighSafetyRiskThreshold) {
		decision.ReasonCodes = []string{"safety_policy_first"}
		return decision
	}
	if validUnitScore(scores.SemanticNeed) && scores.SemanticNeed >= options.SemanticNeedThreshold {
		return semanticRouteDecisionByComplexity(rule, decision, scores, options, "semantic_need_high")
	}
	if scores.RuleFit < options.MinRuleFit {
		return semanticRouteDecisionByComplexity(rule, decision, scores, options, "rule_fit_low")
	}
	if scores.AnchorReadiness < options.MinAnchorReadiness && scores.Complexity >= options.MinComplexityForSemantic {
		return semanticRouteDecisionByComplexity(rule, decision, scores, options, "anchor_readiness_low")
	}
	return decision
}

func semanticRouteDecisionByComplexity(rule memsqlite.QueryAnalysis, decision memsqlite.QueryAnalysisDecision, scores memsqlite.QueryAnalysisScores, options QueryAnalysisOptions, reason string) memsqlite.QueryAnalysisDecision {
	decision.UseSemantic = true
	decision.RetrievalMode = "semantic"
	decision.ReasonCodes = adaptiveSemanticReasonCodes(rule, scores, options, reason)
	if rule.MemoryAbility == memsqlite.MemoryAbilityCausalExplain && scores.AnchorReadiness <= options.MinAnchorReadiness {
		decision.SemanticMode = "semantic_decompose"
		decision.RetrievalMode = "graph_contextual"
		return decision
	}
	if scores.Complexity >= options.DecomposeSemanticComplexity {
		decision.SemanticMode = "semantic_decompose"
		return decision
	}
	if options.Mode == QueryAnalysisModeAdaptiveFull && scores.Complexity >= options.FullSemanticComplexity {
		decision.SemanticMode = "semantic_full"
		return decision
	}
	decision.SemanticMode = "semantic_light"
	return decision
}

func adaptiveSemanticReasonCodes(rule memsqlite.QueryAnalysis, scores memsqlite.QueryAnalysisScores, options QueryAnalysisOptions, reason string) []string {
	reasons := make([]string, 0, 4)
	if rule.MemoryAbility == memsqlite.MemoryAbilityCausalExplain {
		reasons = append(reasons, "causal_intent")
	}
	if scores.AnchorReadiness <= options.MinAnchorReadiness {
		reasons = append(reasons, "weak_anchor")
	}
	if reason != "" {
		reasons = append(reasons, reason)
	}
	out := reasons[:0]
	seen := make(map[string]struct{}, len(reasons))
	for _, value := range reasons {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func validUnitScore(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func semanticRuleFallback(rule memsqlite.QueryAnalysis, fallbackReason string, semantic SemanticQueryAnalysisResult) memsqlite.QueryAnalysis {
	out := cloneStoreQueryAnalysis(rule)
	out.Source = memsqlite.QueryAnalysisSourceSemanticFallback
	out.Diagnostics = &memsqlite.QueryAnalysisDiagnostics{
		SemanticStatus:    "failed",
		SemanticProvider:  semantic.Provider,
		SemanticModel:     semantic.Model,
		PromptVersion:     semantic.PromptVersion,
		SemanticLatencyMs: semantic.LatencyMs,
		FallbackReason:    sanitizeSemanticFallbackReason(fallbackReason, "semantic_sidecar_error"),
		SemanticAnalysis:  semanticAnalysisDiagnosticsFromSemantic(semantic.Analysis),
	}
	copyLegacyQueryAnalysisDiagnostics(out.Diagnostics, rule.Diagnostics)
	return out
}

func mergeSemanticQueryAnalysis(rule memsqlite.QueryAnalysis, semantic SemanticQueryAnalysisResult, options QueryAnalysisOptions, hints []VisibleEntityHint) memsqlite.QueryAnalysis {
	options = normalizeQueryAnalysisOptions(options)
	out := cloneStoreQueryAnalysis(rule)
	analysis := semantic.Analysis
	controlLocked := options.Mode == QueryAnalysisModeSemanticRewriteOnly
	var fieldMergeDecisions []memsqlite.FieldMergeDecision
	if !controlLocked {
		fieldMergeDecisions = mergeSemanticControlFields(&out, rule, analysis, options)
		if validUnitConfidence(analysis.Confidence) && analysis.Confidence >= options.MinConfidenceToOverride {
			out.Confidence = analysis.Confidence
			out.FieldConfidence = queryAnalysisConfidenceToStore(analysis.FieldConfidence)
		}
		out.Signals = mergeQuerySignals(rule.Signals, analysis.Signals)
		applyUnsupportedPremiseClamp(&out, rule, acceptedSemanticPremiseOverride(fieldMergeDecisions))
		applyHistoricalTransitionClamp(&out, rule)
		out.EntityMentions = mergeSemanticEntityMentions(rule.EntityMentions, analysis.EntityMentions, hints, options.MinEntitySemanticConfidence)
	}
	budget := generatedWeightBudget(options)
	var rewriteDiagnostics rewriteSanitizationDiagnostics
	var anchorDiagnostics semanticAnchorSanitizationDiagnostics
	if !options.DisableGeneratedDense {
		out.QueryRewrites, rewriteDiagnostics = sanitizedQueryRewrites(out.Raw, analysis.QueryRewrites, options.MaxQueryRewrites, &budget)
		out.SemanticAnchors, anchorDiagnostics = sanitizedSemanticAnchors(analysis.SemanticAnchors, hints, options, &budget)
	}
	out.ContextBlockHints = primaryContextBlockHint(out)
	if !controlLocked {
		out.PolicyHints = queryPolicyHintsToStore(analysis.PolicyHints)
	}
	mergeSemanticQueryAnalysisPhase1DTO(&out, analysis)
	out.Source = memsqlite.QueryAnalysisSourceMerged
	out.Diagnostics = &memsqlite.QueryAnalysisDiagnostics{
		SemanticStatus:               defaultString(semantic.Status, "ok"),
		SemanticProvider:             semantic.Provider,
		SemanticModel:                semantic.Model,
		PromptVersion:                semantic.PromptVersion,
		SemanticLatencyMs:            semantic.LatencyMs,
		FallbackReason:               semantic.FallbackReason,
		RewriteCount:                 len(out.QueryRewrites),
		SemanticAnchorCount:          len(out.SemanticAnchors),
		DroppedRewriteCount:          rewriteDiagnostics.DroppedCount,
		DroppedRewriteReasons:        rewriteDiagnostics.DroppedReasons,
		DroppedSemanticAnchorCount:   anchorDiagnostics.DroppedCount,
		DroppedSemanticAnchorReasons: anchorDiagnostics.DroppedReasons,
		EnglishRewriteCount:          rewriteDiagnostics.EnglishCount,
		SemanticDriftCount:           semanticControlDriftCount(rule, out),
		FieldMergeDecisions:          fieldMergeDecisions,
		SemanticAnalysis:             semanticAnalysisDiagnosticsFromSemantic(analysis),
	}
	copyLegacyQueryAnalysisDiagnostics(out.Diagnostics, rule.Diagnostics)
	return out
}

func mergeSemanticControlFields(out *memsqlite.QueryAnalysis, rule memsqlite.QueryAnalysis, analysis SemanticQueryAnalysis, options QueryAnalysisOptions) []memsqlite.FieldMergeDecision {
	if out == nil {
		return nil
	}
	proposals := semanticFieldProposals(analysis)
	if len(proposals) == 0 {
		return nil
	}
	fields := []string{"time_mode", "memory_ability", "memory_domain", "evidence_need"}
	decisions := make([]memsqlite.FieldMergeDecision, 0, len(fields))
	for _, field := range fields {
		proposal, ok := proposals[field]
		if !ok {
			continue
		}
		decision := mergeSemanticFieldDecision(field, rule, proposal, options)
		decisions = append(decisions, decision)
		if !decision.UseSemantic {
			continue
		}
		switch field {
		case "time_mode":
			out.TimeMode = memsqlite.QueryTimeMode(decision.SemanticValue)
		case "memory_ability":
			out.MemoryAbility = memsqlite.MemoryAbility(decision.SemanticValue)
		case "memory_domain":
			out.MemoryDomain = memsqlite.MemoryDomain(decision.SemanticValue)
		case "evidence_need":
			out.EvidenceNeed = memsqlite.EvidenceNeed(decision.SemanticValue)
		}
	}
	return decisions
}

func mergeSemanticFieldDecision(field string, rule memsqlite.QueryAnalysis, proposal SemanticFieldProposal, options QueryAnalysisOptions) memsqlite.FieldMergeDecision {
	ruleValue := ruleFieldValue(rule, field)
	semanticValue := strings.TrimSpace(proposal.Value)
	ruleConfidence := ruleConfidenceForField(rule, field)
	semanticConfidence := proposal.Confidence
	decision := memsqlite.FieldMergeDecision{
		Field:              field,
		RuleValue:          ruleValue,
		SemanticValue:      semanticValue,
		RuleConfidence:     ruleConfidence,
		SemanticConfidence: semanticConfidence,
		Evidence:           append([]string(nil), proposal.Evidence...),
	}
	switch {
	case !isValidSemanticFieldValue(field, semanticValue):
		decision.Reason = "semantic_invalid"
	case len(proposal.Evidence) == 0:
		decision.Reason = "semantic_missing_evidence"
	case semanticFieldPolicyClamped(field, semanticValue, rule):
		decision.Reason = "policy_clamp"
	case !validUnitConfidence(semanticConfidence) || semanticConfidence < options.MinSemanticFieldConfidence:
		decision.Reason = "semantic_low_confidence"
	case semanticConfidence-ruleConfidence < options.MinOverrideMargin:
		decision.Reason = "insufficient_margin"
	default:
		decision.Reason = "semantic_higher_confidence"
		decision.UseSemantic = true
	}
	return decision
}

func semanticFieldProposals(analysis SemanticQueryAnalysis) map[string]SemanticFieldProposal {
	if len(analysis.FieldProposals) == 0 {
		return nil
	}
	out := make(map[string]SemanticFieldProposal, len(analysis.FieldProposals))
	for key, value := range analysis.FieldProposals {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value.Value = strings.TrimSpace(value.Value)
		value.Evidence = append([]string(nil), value.Evidence...)
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ruleFieldValue(rule memsqlite.QueryAnalysis, field string) string {
	switch field {
	case "time_mode":
		return string(rule.TimeMode)
	case "memory_ability":
		return string(rule.MemoryAbility)
	case "memory_domain":
		return string(rule.MemoryDomain)
	case "evidence_need":
		return string(rule.EvidenceNeed)
	default:
		return ""
	}
}

func ruleConfidenceForField(rule memsqlite.QueryAnalysis, field string) float64 {
	fieldConfidence := 0.0
	switch field {
	case "time_mode":
		fieldConfidence = rule.FieldConfidence.TimeMode
	case "memory_ability":
		fieldConfidence = rule.FieldConfidence.MemoryAbility
	case "memory_domain":
		fieldConfidence = rule.FieldConfidence.MemoryDomain
	case "evidence_need":
		fieldConfidence = rule.FieldConfidence.EvidenceNeed
	}
	if validUnitConfidence(fieldConfidence) && validUnitConfidence(rule.Confidence) {
		return math.Min(fieldConfidence, rule.Confidence)
	}
	if validUnitConfidence(fieldConfidence) {
		return fieldConfidence
	}
	if validUnitConfidence(rule.Confidence) {
		return rule.Confidence
	}
	if rule.Diagnostics != nil && validUnitConfidence(rule.Diagnostics.RuleConfidenceLegacy) {
		return rule.Diagnostics.RuleConfidenceLegacy
	}
	return 0
}

func isValidSemanticFieldValue(field string, value string) bool {
	switch field {
	case "time_mode":
		return isValidQueryTimeMode(value)
	case "memory_ability":
		return isValidMemoryAbility(value)
	case "memory_domain":
		return isValidMemoryDomain(value)
	case "evidence_need":
		return isValidEvidenceNeed(value)
	default:
		return false
	}
}

func semanticFieldPolicyClamped(field string, value string, rule memsqlite.QueryAnalysis) bool {
	if field == "memory_ability" && hasStoreQuerySignal(rule.Signals, memsqlite.QuerySignalForgetDelete) && value != string(rule.MemoryAbility) {
		return true
	}
	return false
}

func annotateSemanticDecisions(rule memsqlite.QueryAnalysis, legacyDecision bool, adaptiveDecision memsqlite.QueryAnalysisDecision, minConfidence float64) memsqlite.QueryAnalysis {
	out := cloneStoreQueryAnalysis(rule)
	if out.Diagnostics == nil {
		out.Diagnostics = &memsqlite.QueryAnalysisDiagnostics{}
	}
	out.Diagnostics.SemanticDecisionLegacy = legacyDecision
	out.Diagnostics.MinConfidenceToOverride = minConfidence
	if !isZeroStoreQueryAnalysisDecision(adaptiveDecision) {
		out.Diagnostics.AdaptiveDecision = cloneStoreQueryAnalysisDecision(adaptiveDecision)
	}
	return out
}

func copyLegacyQueryAnalysisDiagnostics(dst *memsqlite.QueryAnalysisDiagnostics, src *memsqlite.QueryAnalysisDiagnostics) {
	if dst == nil || src == nil {
		return
	}
	dst.ScorerVersion = src.ScorerVersion
	dst.RuleConfidenceLegacy = src.RuleConfidenceLegacy
	dst.RuleConfidenceReason = src.RuleConfidenceReason
	dst.SemanticDecisionLegacy = src.SemanticDecisionLegacy
	dst.MinConfidenceToOverride = src.MinConfidenceToOverride
	dst.Signals = append([]string(nil), src.Signals...)
	dst.EntityMentionCount = src.EntityMentionCount
	dst.Scores = src.Scores
	dst.FieldConfidence = src.FieldConfidence
	dst.RuleDecision = cloneStoreQueryAnalysisDecision(src.RuleDecision)
	dst.AdaptiveDecision = cloneStoreQueryAnalysisDecision(src.AdaptiveDecision)
	dst.RuleEvidence = append([]memsqlite.QueryAnalysisEvidence(nil), src.RuleEvidence...)
	dst.RuleAlternatives = cloneStoreQueryAnalysisAlternatives(src.RuleAlternatives)
}

func semanticAnalysisDiagnosticsFromSemantic(value SemanticQueryAnalysis) *memsqlite.SemanticQueryAnalysisDiagnostics {
	if isEmptySemanticQueryAnalysis(value) {
		return nil
	}
	out := &memsqlite.SemanticQueryAnalysisDiagnostics{
		TimeMode:          strings.TrimSpace(value.TimeMode),
		SemanticMode:      strings.TrimSpace(value.SemanticMode),
		Signals:           append([]string(nil), value.Signals...),
		MemoryDomain:      strings.TrimSpace(value.MemoryDomain),
		MemoryAbility:     strings.TrimSpace(value.MemoryAbility),
		EvidenceNeed:      strings.TrimSpace(value.EvidenceNeed),
		Confidence:        value.Confidence,
		FieldConfidence:   queryAnalysisConfidenceToStore(value.FieldConfidence),
		FieldProposals:    semanticFieldProposalsToStore(semanticFieldProposals(value)),
		Scores:            queryAnalysisScoresToStore(value.Scores),
		Probes:            queryAnchorProbeToStore(value.Probes),
		Decision:          queryAnalysisDecisionToStore(value.Decision),
		Evidence:          queryAnalysisEvidenceToStore(value.Evidence),
		Alternatives:      queryAnalysisAlternativesToStore(value.Alternatives),
		QueryRewrites:     queryRewritesToStoreDiagnostics(value.QueryRewrites),
		SemanticAnchors:   semanticAnchorsToStoreDiagnostics(value.SemanticAnchors),
		Subqueries:        append([]string(nil), value.Subqueries...),
		SafetyNotes:       append([]string(nil), value.SafetyNotes...),
		ContextBlockHints: append([]string(nil), value.ContextBlockHints...),
		PolicyHints:       queryPolicyHintsToStore(value.PolicyHints),
	}
	for _, mention := range value.EntityMentions {
		out.EntityMentions = append(out.EntityMentions, memsqlite.SemanticQueryEntityMentionDiagnostics{
			EntityID:      mention.EntityID,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
			MatchKind:     mention.MatchKind,
			Confidence:    mention.Confidence,
		})
	}
	return out
}

func isEmptySemanticQueryAnalysis(value SemanticQueryAnalysis) bool {
	if strings.TrimSpace(value.TimeMode) != "" ||
		strings.TrimSpace(value.SemanticMode) != "" ||
		strings.TrimSpace(value.MemoryDomain) != "" ||
		strings.TrimSpace(value.MemoryAbility) != "" ||
		strings.TrimSpace(value.EvidenceNeed) != "" ||
		value.Confidence != 0 ||
		!isZeroQueryAnalysisConfidence(value.FieldConfidence) ||
		len(value.FieldProposals) > 0 ||
		!isZeroQueryAnalysisScores(value.Scores) ||
		!isZeroQueryAnchorProbe(value.Probes) ||
		!isZeroQueryAnalysisDecision(value.Decision) ||
		len(value.Signals) > 0 ||
		len(value.Evidence) > 0 ||
		len(value.Alternatives) > 0 ||
		len(value.EntityMentions) > 0 ||
		len(value.QueryRewrites) > 0 ||
		len(value.SemanticAnchors) > 0 ||
		len(value.Subqueries) > 0 ||
		len(value.SafetyNotes) > 0 ||
		len(value.ContextBlockHints) > 0 ||
		!isZeroQueryPolicyHints(value.PolicyHints) {
		return false
	}
	return true
}

func isZeroQueryAnalysisConfidence(value QueryAnalysisConfidence) bool {
	return value.Overall == 0 &&
		value.TimeMode == 0 &&
		value.MemoryAbility == 0 &&
		value.MemoryDomain == 0 &&
		value.EvidenceNeed == 0 &&
		value.EntityResolution == 0
}

func mergeSemanticQueryAnalysisPhase1DTO(out *memsqlite.QueryAnalysis, analysis SemanticQueryAnalysis) {
	if !isZeroQueryAnalysisScores(analysis.Scores) {
		out.Scores = queryAnalysisScoresToStore(analysis.Scores)
	}
	if !isZeroQueryAnchorProbe(analysis.Probes) {
		out.Probes = queryAnchorProbeToStore(analysis.Probes)
	}
	if !isZeroQueryAnalysisDecision(analysis.Decision) {
		out.Decision = queryAnalysisDecisionToStore(analysis.Decision)
	}
	if len(analysis.Evidence) > 0 {
		out.Evidence = queryAnalysisEvidenceToStore(analysis.Evidence)
	}
	if len(analysis.Alternatives) > 0 {
		out.Alternatives = queryAnalysisAlternativesToStore(analysis.Alternatives)
	}
}

func isZeroQueryAnalysisScores(value QueryAnalysisScores) bool {
	return value.RuleFit == 0 &&
		value.AnchorReadiness == 0 &&
		value.ExpectedRetrievalConfidence == 0 &&
		value.SemanticNeed == 0 &&
		value.Complexity == 0 &&
		value.Ambiguity == 0 &&
		value.Specificity == 0 &&
		value.SafetyRisk == 0 &&
		value.IntentEvidence == 0 &&
		value.TimeEvidence == 0 &&
		value.DomainEvidence == 0 &&
		value.EvidenceNeedEvidence == 0 &&
		value.EntityResolution == 0 &&
		value.FieldConsistency == 0 &&
		value.DefaultFallbackPenalty == 0 &&
		value.MultiIntentConflictPenalty == 0 &&
		value.SensitivityPenalty == 0
}

func isZeroQueryAnchorProbe(value QueryAnchorProbe) bool {
	return value.EntityExactConf == 0 &&
		value.EntityAmbiguity == 0 &&
		value.SparseProbeConf == 0 &&
		value.PredicateProbeConf == 0 &&
		value.RecentProbeConf == 0 &&
		value.PinnedCoreProbeConf == 0 &&
		value.NarrativeProbeConf == 0 &&
		value.FallbackSearchHitCount == 0 &&
		value.Top1Score == 0 &&
		value.Top2Score == 0 &&
		value.Top1Margin == 0 &&
		len(value.Breakdown) == 0
}

func isZeroQueryAnalysisDecision(value QueryAnalysisDecision) bool {
	return !value.UseSemantic &&
		strings.TrimSpace(value.SemanticMode) == "" &&
		strings.TrimSpace(value.RetrievalMode) == "" &&
		len(value.ReasonCodes) == 0 &&
		strings.TrimSpace(value.ThresholdVersion) == "" &&
		strings.TrimSpace(value.ScorerVersion) == ""
}

func isZeroQueryPolicyHints(value QueryPolicyHints) bool {
	return !value.PreferEvidencedByLinks &&
		!value.PreferSupersedesLinks &&
		!value.PreferCausalLinks &&
		!value.PreferCounterexamples &&
		!value.PreferNarratives &&
		value.MaxHopsHint == 0
}

func queryRewritesToStoreDiagnostics(values []QueryRewrite) []memsqlite.QueryRewrite {
	if len(values) == 0 {
		return nil
	}
	out := make([]memsqlite.QueryRewrite, 0, len(values))
	for _, value := range values {
		out = append(out, memsqlite.QueryRewrite{
			Text:    value.Text,
			Purpose: value.Purpose,
			Weight:  value.Weight,
		})
	}
	return out
}

func semanticFieldProposalsToStore(values map[string]SemanticFieldProposal) map[string]memsqlite.SemanticFieldProposal {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]memsqlite.SemanticFieldProposal, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = memsqlite.SemanticFieldProposal{
			Value:      strings.TrimSpace(value.Value),
			Confidence: value.Confidence,
			Evidence:   append([]string(nil), value.Evidence...),
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func semanticAnchorsToStoreDiagnostics(values []SemanticAnchor) []memsqlite.SemanticAnchor {
	if len(values) == 0 {
		return nil
	}
	out := make([]memsqlite.SemanticAnchor, 0, len(values))
	for _, value := range values {
		out = append(out, memsqlite.SemanticAnchor{
			Text:       value.Text,
			AnchorType: value.AnchorType,
			EntityID:   value.EntityID,
			Weight:     value.Weight,
			Confidence: value.Confidence,
		})
	}
	return out
}

func semanticControlDriftCount(rule memsqlite.QueryAnalysis, merged memsqlite.QueryAnalysis) int {
	count := 0
	if rule.TimeMode != merged.TimeMode {
		count++
	}
	if rule.MemoryDomain != merged.MemoryDomain {
		count++
	}
	if rule.MemoryAbility != merged.MemoryAbility {
		count++
	}
	if rule.EvidenceNeed != merged.EvidenceNeed {
		count++
	}
	return count
}

func normalizeQueryAnalysisOptions(options QueryAnalysisOptions) QueryAnalysisOptions {
	if options.Provider == "" {
		options.Provider = QueryAnalysisProviderNone
	}
	if options.Provider == "none" {
		options.Provider = QueryAnalysisProviderNone
	}
	if options.Mode == "" {
		options.Mode = QueryAnalysisModeRuleOnlyExplicit
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultQueryAnalysisTimeout
	}
	if options.SoftJoinTimeout > 0 && options.SoftJoinTimeout > options.Timeout {
		options.SoftJoinTimeout = options.Timeout
	}
	if options.MinConfidenceToOverride <= 0 || options.MinConfidenceToOverride > 1 {
		options.MinConfidenceToOverride = defaultQueryAnalysisMinConfidenceToOverride
	}
	if options.MinEntitySemanticConfidence <= 0 || options.MinEntitySemanticConfidence > 1 {
		options.MinEntitySemanticConfidence = defaultQueryAnalysisMinEntityConfidence
	}
	if options.MinRuleFit <= 0 || options.MinRuleFit > 1 {
		options.MinRuleFit = defaultQueryAnalysisMinRuleFit
	}
	if options.MinAnchorReadiness <= 0 || options.MinAnchorReadiness > 1 {
		options.MinAnchorReadiness = defaultQueryAnalysisMinAnchorReadiness
	}
	if options.SemanticNeedThreshold <= 0 || options.SemanticNeedThreshold > 1 {
		options.SemanticNeedThreshold = defaultQueryAnalysisSemanticNeed
	}
	if options.MinComplexityForSemantic <= 0 || options.MinComplexityForSemantic > 1 {
		options.MinComplexityForSemantic = defaultQueryAnalysisMinComplexity
	}
	if options.FullSemanticComplexity <= 0 || options.FullSemanticComplexity > 1 {
		options.FullSemanticComplexity = defaultQueryAnalysisFullComplexity
	}
	if options.DecomposeSemanticComplexity <= 0 || options.DecomposeSemanticComplexity > 1 {
		options.DecomposeSemanticComplexity = defaultQueryAnalysisDecomposeComplexity
	}
	if options.MinSemanticFieldConfidence <= 0 || options.MinSemanticFieldConfidence > 1 {
		options.MinSemanticFieldConfidence = defaultQueryAnalysisMinFieldConfidence
	}
	if options.MinOverrideMargin <= 0 || options.MinOverrideMargin > 1 {
		options.MinOverrideMargin = defaultQueryAnalysisOverrideMargin
	}
	if options.HighSafetyRiskThreshold <= 0 || options.HighSafetyRiskThreshold > 1 {
		options.HighSafetyRiskThreshold = defaultQueryAnalysisHighSafetyRisk
	}
	if options.MaxQueryRewrites <= 0 {
		options.MaxQueryRewrites = defaultQueryAnalysisMaxQueryRewrites
	}
	if options.MaxSemanticAnchors <= 0 {
		options.MaxSemanticAnchors = defaultQueryAnalysisMaxSemanticAnchors
	}
	if options.SemanticTotalEnergyCap <= 0 {
		options.SemanticTotalEnergyCap = defaultQueryAnalysisSemanticEnergyCap
	}
	if options.MaxGeneratedDenseWeightSum <= 0 {
		options.MaxGeneratedDenseWeightSum = defaultQueryAnalysisGeneratedDenseWeightSum
	}
	return options
}

func isValidSemanticQueryAnalysisStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "ok", "degraded", "error":
		return true
	default:
		return false
	}
}

func semanticFallbackReasonFromError(parent context.Context, err error) string {
	if parent != nil && parent.Err() != nil {
		return "go_context_timeout"
	}
	if err == nil {
		return "semantic_protocol_error"
	}
	if errors.Is(err, context.Canceled) {
		return "go_context_timeout"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "semantic_timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "semantic_timeout"
	}
	if reason := internalmirror.QueryAnalysisErrorReason(err); reason != "" {
		return sanitizeSemanticFallbackReason(reason, "semantic_sidecar_error")
	}
	return "semantic_sidecar_error"
}

func sanitizeSemanticFallbackReason(reason string, fallback string) string {
	switch strings.TrimSpace(reason) {
	case "semantic_sidecar_error", "semantic_timeout", "semantic_protocol_error", "semantic_invalid_response", "semantic_unavailable",
		"semantic_soft_timeout", "go_context_timeout", "semantic_budget_exhausted", "provider_budget_exhausted",
		"sidecar_provider_timeout", "provider_none", "missing_api_key", "invalid_json", "invalid_response", "validation_failed", "provider_error", "provider_timeout":
		return strings.TrimSpace(reason)
	default:
		if strings.TrimSpace(fallback) != "" {
			return fallback
		}
		return "semantic_sidecar_error"
	}
}

func newSemanticQueryAnalyzerFromOptions(options QueryAnalysisOptions) SemanticQueryAnalyzer {
	options = normalizeQueryAnalysisOptions(options)
	if options.Provider != QueryAnalysisProviderSidecar || strings.TrimSpace(options.SidecarURL) == "" {
		return nil
	}
	return sidecarSemanticQueryAnalyzer{
		client: internalmirror.NewSidecarClient(internalmirror.SidecarClientOptions{
			BaseURL: options.SidecarURL,
			Timeout: options.Timeout,
		}),
	}
}

type sidecarSemanticQueryAnalyzer struct {
	client *internalmirror.SidecarClient
}

func (a sidecarSemanticQueryAnalyzer) AnalyzeSemanticQuery(ctx context.Context, req SemanticQueryAnalysisRequest) (*SemanticQueryAnalysisResult, error) {
	if a.client == nil {
		return nil, fmt.Errorf("sidecar query analysis client is required")
	}
	result, err := a.client.QueryAnalysis(ctx, internalmirror.QueryAnalysisRequest{
		RequestID:          req.RequestID,
		PersonaID:          req.PersonaID,
		SessionID:          req.SessionID,
		MessageID:          req.MessageID,
		QueryText:          req.QueryText,
		SemanticMode:       req.SemanticMode,
		Now:                req.Now,
		Timezone:           req.Timezone,
		RuleAnalysis:       queryAnalysisToMirror(req.RuleAnalysis),
		VisibleEntityHints: visibleEntityHintsToMirror(req.VisibleEntityHints),
		AllowedEnums:       allowedEnumsToMirror(req.AllowedEnums),
		RetrievalPolicy: internalmirror.RetrievalPolicy{
			SensitivityPermission: req.RetrievalPolicy.SensitivityPermission,
			AllowHistorical:       req.RetrievalPolicy.AllowHistorical,
			AllowDeepArchive:      req.RetrievalPolicy.AllowDeepArchive,
			FinalMemoryCount:      req.RetrievalPolicy.FinalMemoryCount,
			ContextBudgetTokens:   req.RetrievalPolicy.ContextBudgetTokens,
			UseFTS:                req.RetrievalPolicy.UseFTS,
			UseMirror:             req.RetrievalPolicy.UseMirror,
		},
		DeadlineMS:        req.DeadlineMS,
		ProviderTimeoutMS: req.ProviderTimeoutMS,
		Debug:             internalmirror.QueryAnalysisDebug{IncludeRationaleSummary: req.IncludeRationaleSummary},
	})
	if err != nil {
		return nil, err
	}
	return semanticQueryAnalysisResultFromMirror(result), nil
}

func visibleEntityHintsFromRule(rule memsqlite.QueryAnalysis) []VisibleEntityHint {
	hints := make([]VisibleEntityHint, 0, len(rule.EntityMentions))
	seen := map[string]struct{}{}
	for _, mention := range rule.EntityMentions {
		id := strings.TrimSpace(mention.EntityID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		hints = append(hints, VisibleEntityHint{
			EntityID:      id,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
		})
	}
	return hints
}

func defaultQueryAnalysisAllowedEnums() QueryAnalysisAllowedEnums {
	return QueryAnalysisAllowedEnums{
		TimeModes: []string{string(memsqlite.QueryTimeModeCurrent), string(memsqlite.QueryTimeModeHistorical), string(memsqlite.QueryTimeModeBitemporalCheck)},
		Signals: []string{
			string(memsqlite.QuerySignalCausal), string(memsqlite.QuerySignalHistorical), string(memsqlite.QuerySignalProvenance),
			string(memsqlite.QuerySignalSensitivity), string(memsqlite.QuerySignalDebug), string(memsqlite.QuerySignalPremiseCheck),
			string(memsqlite.QuerySignalRelationshipArc), string(memsqlite.QuerySignalForgetDelete),
			string(memsqlite.QuerySignalPastEventDirectFact), string(memsqlite.QuerySignalStateTransition), string(memsqlite.QuerySignalProvenanceSource),
			string(memsqlite.QuerySignalCausalChain), string(memsqlite.QuerySignalPremiseCounterexample), string(memsqlite.QuerySignalEventBundle),
			string(memsqlite.QuerySignalReflectionSummary), string(memsqlite.QuerySignalExactFact),
		},
		MemoryDomains: []string{string(memsqlite.MemoryDomainRelationship), string(memsqlite.MemoryDomainUserProfile), string(memsqlite.MemoryDomainWorkExperience), string(memsqlite.MemoryDomainEnvironmentExperience)},
		MemoryAbilities: []string{
			string(memsqlite.MemoryAbilityDirectFact), string(memsqlite.MemoryAbilityCausalExplain), string(memsqlite.MemoryAbilityHistorical),
			string(memsqlite.MemoryAbilityProvenance), string(memsqlite.MemoryAbilityBoundary), string(memsqlite.MemoryAbilitySupportive),
			string(memsqlite.MemoryAbilityPlanning), string(memsqlite.MemoryAbilityStaticState), string(memsqlite.MemoryAbilityDynamicState),
			string(memsqlite.MemoryAbilityWorkflow), string(memsqlite.MemoryAbilityGotcha), string(memsqlite.MemoryAbilityPremiseCheck),
			string(memsqlite.MemoryAbilityRelationshipArc),
		},
		EvidenceNeeds: []string{
			string(memsqlite.EvidenceNeedExactObservation), string(memsqlite.EvidenceNeedStateTransition), string(memsqlite.EvidenceNeedProcedureNote),
			string(memsqlite.EvidenceNeedGotchaNote), string(memsqlite.EvidenceNeedPremiseCounterexample), string(memsqlite.EvidenceNeedProvenanceSource),
			string(memsqlite.EvidenceNeedRelationshipTimeline),
		},
		EntityMentionKinds: []string{string(memsqlite.QueryEntityMentionKindCanonical), string(memsqlite.QueryEntityMentionKindAlias)},
		ContextBlockHints: []string{
			memsqlite.MemoryBlockTypeFacts, memsqlite.MemoryBlockTypeRelevantCausalMemory, memsqlite.MemoryBlockTypeHistoricalTransitionMemory,
			memsqlite.MemoryBlockTypeProvenanceMemory, memsqlite.MemoryBlockTypePremiseCheckMemory, memsqlite.MemoryBlockTypeRelationshipArcMemory,
			memsqlite.MemoryBlockTypeSupportiveMemory, memsqlite.MemoryBlockTypeExperienceContext,
		},
	}
}

func mergeQuerySignals(rule []memsqlite.QuerySignal, semantic []string) []memsqlite.QuerySignal {
	seen := map[memsqlite.QuerySignal]struct{}{}
	var out []memsqlite.QuerySignal
	add := func(signal memsqlite.QuerySignal) {
		if !isValidQuerySignal(string(signal)) {
			return
		}
		if _, ok := seen[signal]; ok {
			return
		}
		seen[signal] = struct{}{}
		out = append(out, signal)
	}
	for _, signal := range rule {
		add(signal)
	}
	for _, signal := range semantic {
		add(memsqlite.QuerySignal(strings.TrimSpace(signal)))
	}
	return out
}

func mergeSemanticEntityMentions(rule []memsqlite.QueryEntityMention, semantic []SemanticQueryEntityMention, hints []VisibleEntityHint, minConfidence float64) []memsqlite.QueryEntityMention {
	visible := visibleEntitySet(hints)
	out := append([]memsqlite.QueryEntityMention(nil), rule...)
	seen := map[string]struct{}{}
	for _, mention := range out {
		if mention.EntityID != "" {
			seen[mention.EntityID] = struct{}{}
		}
	}
	for _, mention := range semantic {
		id := strings.TrimSpace(mention.EntityID)
		if id == "" || mention.Confidence < minConfidence {
			continue
		}
		if _, ok := visible[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		kind := memsqlite.QueryEntityMentionKindCanonical
		if isValidEntityMentionKind(mention.MatchKind) {
			kind = memsqlite.QueryEntityMentionKind(mention.MatchKind)
		}
		out = append(out, memsqlite.QueryEntityMention{
			EntityID:      id,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
			MatchKind:     kind,
		})
		seen[id] = struct{}{}
	}
	return out
}

type rewriteSanitizationDiagnostics struct {
	DroppedCount   int
	DroppedReasons []string
	EnglishCount   int
}

type semanticAnchorSanitizationDiagnostics struct {
	DroppedCount   int
	DroppedReasons []string
}

func sanitizedQueryRewrites(rawQuery string, values []QueryRewrite, limit int, budget *float64) ([]memsqlite.QueryRewrite, rewriteSanitizationDiagnostics) {
	var diagnostics rewriteSanitizationDiagnostics
	if limit <= 0 || len(values) == 0 {
		return nil, diagnostics
	}
	out := make([]memsqlite.QueryRewrite, 0, minInt(len(values), limit))
	rawCJKHeavy := isCJKHeavy(rawQuery)
	for _, value := range values {
		if len(out) >= limit {
			break
		}
		text := strings.TrimSpace(value.Text)
		if text == "" {
			continue
		}
		textCJKHeavy := isCJKHeavy(text)
		if !textCJKHeavy && hasASCIILetter(text) {
			diagnostics.EnglishCount++
		}
		if rawCJKHeavy && !textCJKHeavy && runeLen(text) > 12 {
			diagnostics.DroppedCount++
			diagnostics.DroppedReasons = append(diagnostics.DroppedReasons, rewriteDropReasonLanguageMismatch)
			continue
		}
		weight, ok := consumeGeneratedWeight(clampFloat(value.Weight, 0.1, 0.9), 0.1, budget)
		if !ok {
			break
		}
		out = append(out, memsqlite.QueryRewrite{
			Text:    text,
			Purpose: strings.TrimSpace(value.Purpose),
			Weight:  weight,
		})
	}
	return out, diagnostics
}

func applyHistoricalTransitionClamp(out *memsqlite.QueryAnalysis, rule memsqlite.QueryAnalysis) {
	if out == nil {
		return
	}
	if !hasHistoricalTransitionIntent(out.Raw) && !hasStoreQuerySignal(rule.Signals, memsqlite.QuerySignalHistorical) {
		return
	}
	if out.EvidenceNeed != memsqlite.EvidenceNeedStateTransition && rule.EvidenceNeed != memsqlite.EvidenceNeedStateTransition {
		return
	}
	if out.TimeMode == memsqlite.QueryTimeModeCurrent {
		out.TimeMode = memsqlite.QueryTimeModeHistorical
	}
	switch out.MemoryAbility {
	case memsqlite.MemoryAbilityDirectFact, memsqlite.MemoryAbilityDynamicState:
		if hasStoreQuerySignal(out.Signals, memsqlite.QuerySignalRelationshipArc) {
			out.MemoryAbility = memsqlite.MemoryAbilityRelationshipArc
		} else {
			out.MemoryAbility = memsqlite.MemoryAbilityHistorical
		}
	}
}

func applyUnsupportedPremiseClamp(out *memsqlite.QueryAnalysis, rule memsqlite.QueryAnalysis, allowSemanticPremise bool) {
	if out == nil || ruleSupportsPremiseCheck(rule) || allowSemanticPremise {
		return
	}
	if out.TimeMode == memsqlite.QueryTimeModeBitemporalCheck {
		out.TimeMode = rule.TimeMode
	}
	if out.MemoryAbility == memsqlite.MemoryAbilityPremiseCheck {
		out.MemoryAbility = rule.MemoryAbility
	}
	if out.EvidenceNeed == memsqlite.EvidenceNeedPremiseCounterexample {
		out.EvidenceNeed = rule.EvidenceNeed
	}
	out.Signals = withoutPremiseSignals(out.Signals)
}

func acceptedSemanticPremiseOverride(decisions []memsqlite.FieldMergeDecision) bool {
	for _, decision := range decisions {
		if !decision.UseSemantic {
			continue
		}
		switch decision.Field {
		case "time_mode":
			if decision.SemanticValue == string(memsqlite.QueryTimeModeBitemporalCheck) {
				return true
			}
		case "memory_ability":
			if decision.SemanticValue == string(memsqlite.MemoryAbilityPremiseCheck) {
				return true
			}
		case "evidence_need":
			if decision.SemanticValue == string(memsqlite.EvidenceNeedPremiseCounterexample) {
				return true
			}
		}
	}
	return false
}

func ruleSupportsPremiseCheck(rule memsqlite.QueryAnalysis) bool {
	return rule.TimeMode == memsqlite.QueryTimeModeBitemporalCheck ||
		rule.MemoryAbility == memsqlite.MemoryAbilityPremiseCheck ||
		rule.EvidenceNeed == memsqlite.EvidenceNeedPremiseCounterexample ||
		hasStoreQuerySignal(rule.Signals, memsqlite.QuerySignalPremiseCheck) ||
		hasStoreQuerySignal(rule.Signals, memsqlite.QuerySignalPremiseCounterexample)
}

func withoutPremiseSignals(signals []memsqlite.QuerySignal) []memsqlite.QuerySignal {
	if len(signals) == 0 {
		return nil
	}
	out := make([]memsqlite.QuerySignal, 0, len(signals))
	for _, signal := range signals {
		switch signal {
		case memsqlite.QuerySignalPremiseCheck, memsqlite.QuerySignalPremiseCounterexample:
			continue
		default:
			out = append(out, signal)
		}
	}
	return out
}

func hasHistoricalTransitionIntent(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if containsAny(normalized, "一开始", "后来", "以前", "曾经", "从前", "发生变化", "变成") {
		return true
	}
	if containsAny(normalized, "变", "变化") && (strings.Contains(normalized, "从") || strings.Contains(normalized, "到")) {
		return true
	}
	return false
}

func isCJKHeavy(value string) bool {
	var cjk int
	var letters int
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			cjk++
			continue
		}
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			letters++
		}
	}
	return cjk > 0 && cjk >= maxInt(1, letters/3)
}

func runeLen(value string) int {
	return len([]rune(value))
}

func hasASCIILetter(value string) bool {
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			return true
		}
	}
	return false
}

func sanitizedSemanticAnchors(values []SemanticAnchor, hints []VisibleEntityHint, options QueryAnalysisOptions, budget *float64) ([]memsqlite.SemanticAnchor, semanticAnchorSanitizationDiagnostics) {
	diagnostics := semanticAnchorSanitizationDiagnostics{}
	if options.MaxSemanticAnchors <= 0 || len(values) == 0 {
		return nil, diagnostics
	}
	visible := visibleEntitySet(hints)
	out := make([]memsqlite.SemanticAnchor, 0, minInt(len(values), options.MaxSemanticAnchors))
	for _, value := range values {
		if len(out) >= options.MaxSemanticAnchors {
			break
		}
		text := strings.TrimSpace(value.Text)
		if text == "" {
			continue
		}
		if isGenericSemanticAnchor(text) {
			diagnostics.DroppedCount++
			diagnostics.DroppedReasons = append(diagnostics.DroppedReasons, "generic_semantic_anchor")
			continue
		}
		entityID := strings.TrimSpace(value.EntityID)
		if entityID != "" {
			if _, ok := visible[entityID]; !ok {
				continue
			}
			if value.Confidence < options.MinEntitySemanticConfidence {
				continue
			}
		}
		weight := value.Weight
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight <= 0 {
			continue
		}
		if weight > 0.65 {
			weight = 0.65
		}
		var ok bool
		weight, ok = consumeGeneratedWeight(weight, 0, budget)
		if !ok {
			break
		}
		out = append(out, memsqlite.SemanticAnchor{
			Text:       text,
			AnchorType: strings.TrimSpace(value.AnchorType),
			EntityID:   entityID,
			Weight:     weight,
			Confidence: clampFloat(value.Confidence, 0, 1),
		})
	}
	return out, diagnostics
}

func isGenericSemanticAnchor(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "工作", "情绪", "最近", "事情", "记忆", "memory", "recent":
		return true
	default:
		return false
	}
}

func generatedWeightBudget(options QueryAnalysisOptions) float64 {
	options = normalizeQueryAnalysisOptions(options)
	rawRemaining := options.SemanticTotalEnergyCap - 1.0
	if rawRemaining < 0 {
		rawRemaining = 0
	}
	if options.MaxGeneratedDenseWeightSum < rawRemaining {
		return normalizedGeneratedWeight(options.MaxGeneratedDenseWeightSum)
	}
	return normalizedGeneratedWeight(rawRemaining)
}

func consumeGeneratedWeight(weight float64, minWeight float64, budget *float64) (float64, bool) {
	if budget == nil {
		return weight, true
	}
	remaining := normalizedGeneratedWeight(*budget)
	if remaining <= 0 {
		return 0, false
	}
	weight = normalizedGeneratedWeight(weight)
	if weight > remaining {
		weight = remaining
	}
	if minWeight > 0 && weight+1e-9 < minWeight {
		return 0, false
	}
	if minWeight > 0 && weight < minWeight {
		weight = minWeight
	}
	*budget = normalizedGeneratedWeight(remaining - weight)
	return weight, true
}

func normalizedGeneratedWeight(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0
	}
	return math.Round(value*1_000_000) / 1_000_000
}

func sanitizedContextBlockHints(values []string) []string {
	allowed := map[string]struct{}{}
	for _, value := range defaultQueryAnalysisAllowedEnums().ContextBlockHints {
		allowed[value] = struct{}{}
	}
	var out []string
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, ok := allowed[value]; !ok {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func primaryContextBlockHint(query memsqlite.QueryAnalysis) []string {
	switch {
	case query.EvidenceNeed == memsqlite.EvidenceNeedProvenanceSource ||
		hasStoreQuerySignal(query.Signals, memsqlite.QuerySignalProvenanceSource) ||
		hasStoreQuerySignal(query.Signals, memsqlite.QuerySignalProvenance):
		return []string{MemoryBlockTypeProvenanceMemory}
	case hasStoreQuerySignal(query.Signals, memsqlite.QuerySignalCausal) ||
		hasStoreQuerySignal(query.Signals, memsqlite.QuerySignalCausalChain):
		return []string{MemoryBlockTypeRelevantCausalMemory}
	case query.EvidenceNeed == memsqlite.EvidenceNeedPremiseCounterexample ||
		hasStoreQuerySignal(query.Signals, memsqlite.QuerySignalPremiseCounterexample):
		return []string{MemoryBlockTypePremiseCheckMemory}
	case query.EvidenceNeed == memsqlite.EvidenceNeedRelationshipTimeline ||
		hasStoreQuerySignal(query.Signals, memsqlite.QuerySignalRelationshipArc):
		return []string{MemoryBlockTypeRelationshipArcMemory}
	case query.EvidenceNeed == memsqlite.EvidenceNeedStateTransition ||
		hasStoreQuerySignal(query.Signals, memsqlite.QuerySignalStateTransition):
		return []string{MemoryBlockTypeHistoricalTransitionMemory}
	case query.MemoryAbility == memsqlite.MemoryAbilitySupportive:
		return []string{MemoryBlockTypeSupportiveMemory}
	default:
		return nil
	}
}

func visibleEntitySet(hints []VisibleEntityHint) map[string]struct{} {
	visible := map[string]struct{}{}
	for _, hint := range hints {
		id := strings.TrimSpace(hint.EntityID)
		if id != "" {
			visible[id] = struct{}{}
		}
	}
	return visible
}

func confidenceForField(field float64, overall float64, semantic float64) float64 {
	for _, value := range []float64{field, overall, semantic} {
		if validUnitConfidence(value) {
			return value
		}
	}
	return 0
}

func validUnitConfidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0 && value <= 1
}

func isValidQueryTimeMode(value string) bool {
	switch memsqlite.QueryTimeMode(strings.TrimSpace(value)) {
	case memsqlite.QueryTimeModeCurrent, memsqlite.QueryTimeModeHistorical, memsqlite.QueryTimeModeBitemporalCheck:
		return true
	default:
		return false
	}
}

func isValidQuerySignal(value string) bool {
	switch memsqlite.QuerySignal(strings.TrimSpace(value)) {
	case memsqlite.QuerySignalCausal, memsqlite.QuerySignalHistorical, memsqlite.QuerySignalProvenance, memsqlite.QuerySignalSensitivity,
		memsqlite.QuerySignalDebug, memsqlite.QuerySignalPremiseCheck, memsqlite.QuerySignalRelationshipArc, memsqlite.QuerySignalForgetDelete:
		return true
	case memsqlite.QuerySignalPastEventDirectFact, memsqlite.QuerySignalStateTransition, memsqlite.QuerySignalProvenanceSource,
		memsqlite.QuerySignalCausalChain, memsqlite.QuerySignalPremiseCounterexample, memsqlite.QuerySignalEventBundle,
		memsqlite.QuerySignalReflectionSummary, memsqlite.QuerySignalExactFact:
		return true
	default:
		return false
	}
}

func isValidMemoryDomain(value string) bool {
	switch memsqlite.MemoryDomain(strings.TrimSpace(value)) {
	case memsqlite.MemoryDomainRelationship, memsqlite.MemoryDomainUserProfile, memsqlite.MemoryDomainWorkExperience, memsqlite.MemoryDomainEnvironmentExperience:
		return true
	default:
		return false
	}
}

func isValidMemoryAbility(value string) bool {
	switch memsqlite.MemoryAbility(strings.TrimSpace(value)) {
	case memsqlite.MemoryAbilityDirectFact, memsqlite.MemoryAbilityCausalExplain, memsqlite.MemoryAbilityHistorical, memsqlite.MemoryAbilityProvenance,
		memsqlite.MemoryAbilityBoundary, memsqlite.MemoryAbilitySupportive, memsqlite.MemoryAbilityPlanning, memsqlite.MemoryAbilityStaticState,
		memsqlite.MemoryAbilityDynamicState, memsqlite.MemoryAbilityWorkflow, memsqlite.MemoryAbilityGotcha, memsqlite.MemoryAbilityPremiseCheck,
		memsqlite.MemoryAbilityRelationshipArc:
		return true
	default:
		return false
	}
}

func isValidEvidenceNeed(value string) bool {
	switch memsqlite.EvidenceNeed(strings.TrimSpace(value)) {
	case memsqlite.EvidenceNeedExactObservation, memsqlite.EvidenceNeedStateTransition, memsqlite.EvidenceNeedProcedureNote, memsqlite.EvidenceNeedGotchaNote,
		memsqlite.EvidenceNeedPremiseCounterexample, memsqlite.EvidenceNeedProvenanceSource, memsqlite.EvidenceNeedRelationshipTimeline:
		return true
	default:
		return false
	}
}

func isValidEntityMentionKind(value string) bool {
	switch memsqlite.QueryEntityMentionKind(strings.TrimSpace(value)) {
	case memsqlite.QueryEntityMentionKindCanonical, memsqlite.QueryEntityMentionKindAlias:
		return true
	default:
		return false
	}
}

func hasStoreQuerySignal(signals []memsqlite.QuerySignal, want memsqlite.QuerySignal) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}

func queryAnalysisConfidenceToStore(value QueryAnalysisConfidence) memsqlite.QueryAnalysisConfidence {
	return memsqlite.QueryAnalysisConfidence{
		Overall:          clampFloat(value.Overall, 0, 1),
		TimeMode:         clampFloat(value.TimeMode, 0, 1),
		MemoryAbility:    clampFloat(value.MemoryAbility, 0, 1),
		MemoryDomain:     clampFloat(value.MemoryDomain, 0, 1),
		EvidenceNeed:     clampFloat(value.EvidenceNeed, 0, 1),
		EntityResolution: clampFloat(value.EntityResolution, 0, 1),
	}
}

func queryAnalysisScoresToStore(value QueryAnalysisScores) memsqlite.QueryAnalysisScores {
	return memsqlite.QueryAnalysisScores{
		RuleFit:                     value.RuleFit,
		AnchorReadiness:             value.AnchorReadiness,
		ExpectedRetrievalConfidence: value.ExpectedRetrievalConfidence,
		SemanticNeed:                value.SemanticNeed,
		Complexity:                  value.Complexity,
		Ambiguity:                   value.Ambiguity,
		Specificity:                 value.Specificity,
		SafetyRisk:                  value.SafetyRisk,
		IntentEvidence:              value.IntentEvidence,
		TimeEvidence:                value.TimeEvidence,
		DomainEvidence:              value.DomainEvidence,
		EvidenceNeedEvidence:        value.EvidenceNeedEvidence,
		EntityResolution:            value.EntityResolution,
		FieldConsistency:            value.FieldConsistency,
		DefaultFallbackPenalty:      value.DefaultFallbackPenalty,
		MultiIntentConflictPenalty:  value.MultiIntentConflictPenalty,
		SensitivityPenalty:          value.SensitivityPenalty,
	}
}

func queryAnchorProbeToStore(value QueryAnchorProbe) memsqlite.QueryAnchorProbe {
	return memsqlite.QueryAnchorProbe{
		EntityExactConf:        value.EntityExactConf,
		EntityAmbiguity:        value.EntityAmbiguity,
		SparseProbeConf:        value.SparseProbeConf,
		PredicateProbeConf:     value.PredicateProbeConf,
		RecentProbeConf:        value.RecentProbeConf,
		PinnedCoreProbeConf:    value.PinnedCoreProbeConf,
		NarrativeProbeConf:     value.NarrativeProbeConf,
		FallbackSearchHitCount: value.FallbackSearchHitCount,
		Top1Score:              value.Top1Score,
		Top2Score:              value.Top2Score,
		Top1Margin:             value.Top1Margin,
		Breakdown:              queryAnchorProbeBreakdownToStore(value.Breakdown),
	}
}

func queryAnchorProbeBreakdownToStore(values []QueryAnchorProbeBreakdown) []memsqlite.QueryAnchorProbeBreakdown {
	if len(values) == 0 {
		return nil
	}
	out := make([]memsqlite.QueryAnchorProbeBreakdown, 0, len(values))
	for _, value := range values {
		out = append(out, memsqlite.QueryAnchorProbeBreakdown{
			Source:      value.Source,
			Confidence:  value.Confidence,
			HitCount:    value.HitCount,
			TopScore:    value.TopScore,
			SecondScore: value.SecondScore,
			Reason:      value.Reason,
			Error:       value.Error,
		})
	}
	return out
}

func queryAnalysisDecisionToStore(value QueryAnalysisDecision) memsqlite.QueryAnalysisDecision {
	return memsqlite.QueryAnalysisDecision{
		UseSemantic:      value.UseSemantic,
		SemanticMode:     value.SemanticMode,
		RetrievalMode:    value.RetrievalMode,
		ReasonCodes:      append([]string(nil), value.ReasonCodes...),
		ThresholdVersion: value.ThresholdVersion,
		ScorerVersion:    value.ScorerVersion,
	}
}

func queryAnalysisEvidenceToStore(values []QueryAnalysisEvidence) []memsqlite.QueryAnalysisEvidence {
	if len(values) == 0 {
		return nil
	}
	out := make([]memsqlite.QueryAnalysisEvidence, 0, len(values))
	for _, value := range values {
		out = append(out, memsqlite.QueryAnalysisEvidence{
			Field:     value.Field,
			Signal:    value.Signal,
			MatchText: value.MatchText,
			SpanStart: value.SpanStart,
			SpanEnd:   value.SpanEnd,
			Weight:    value.Weight,
			Detector:  value.Detector,
		})
	}
	return out
}

func queryAnalysisAlternativesToStore(values []QueryAnalysisAlternative) []memsqlite.QueryAnalysisAlternative {
	if len(values) == 0 {
		return nil
	}
	out := make([]memsqlite.QueryAnalysisAlternative, 0, len(values))
	for _, value := range values {
		out = append(out, memsqlite.QueryAnalysisAlternative{
			Field:       value.Field,
			Value:       value.Value,
			Confidence:  value.Confidence,
			ReasonCodes: append([]string(nil), value.ReasonCodes...),
			Detector:    value.Detector,
		})
	}
	return out
}

func queryPolicyHintsToStore(value QueryPolicyHints) memsqlite.QueryPolicyHints {
	return memsqlite.QueryPolicyHints{
		PreferEvidencedByLinks: value.PreferEvidencedByLinks,
		PreferSupersedesLinks:  value.PreferSupersedesLinks,
		PreferCausalLinks:      value.PreferCausalLinks,
		PreferCounterexamples:  value.PreferCounterexamples,
		PreferNarratives:       value.PreferNarratives,
		MaxHopsHint:            value.MaxHopsHint,
	}
}

func queryAnalysisToMirror(value memsqlite.QueryAnalysis) internalmirror.QueryAnalysis {
	out := internalmirror.QueryAnalysis{
		Raw:               value.Raw,
		Normalized:        value.Normalized,
		Terms:             append([]string(nil), value.Terms...),
		TimeMode:          string(value.TimeMode),
		MemoryDomain:      string(value.MemoryDomain),
		MemoryAbility:     string(value.MemoryAbility),
		EvidenceNeed:      string(value.EvidenceNeed),
		Source:            string(value.Source),
		Confidence:        value.Confidence,
		FieldConfidence:   queryAnalysisConfidenceToMirror(value.FieldConfidence),
		Scores:            queryAnalysisScoresToMirror(value.Scores),
		Probes:            queryAnchorProbeToMirror(value.Probes),
		Decision:          queryAnalysisDecisionToMirror(value.Decision),
		Evidence:          queryAnalysisEvidenceToMirror(value.Evidence),
		Alternatives:      queryAnalysisAlternativesToMirror(value.Alternatives),
		ContextBlockHints: append([]string(nil), value.ContextBlockHints...),
		PolicyHints:       queryPolicyHintsToMirror(value.PolicyHints),
	}
	for _, signal := range value.Signals {
		out.Signals = append(out.Signals, string(signal))
	}
	for _, mention := range value.EntityMentions {
		out.EntityMentions = append(out.EntityMentions, internalmirror.QueryEntityMention{
			EntityID:      mention.EntityID,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
			MatchKind:     string(mention.MatchKind),
		})
	}
	for _, rewrite := range value.QueryRewrites {
		out.QueryRewrites = append(out.QueryRewrites, internalmirror.QueryRewrite{
			Text:    rewrite.Text,
			Purpose: rewrite.Purpose,
			Weight:  rewrite.Weight,
		})
	}
	for _, anchor := range value.SemanticAnchors {
		out.SemanticAnchors = append(out.SemanticAnchors, internalmirror.SemanticAnchor{
			Text:       anchor.Text,
			AnchorType: anchor.AnchorType,
			EntityID:   anchor.EntityID,
			Weight:     anchor.Weight,
			Confidence: anchor.Confidence,
		})
	}
	return out
}

func semanticQueryAnalysisResultFromMirror(value internalmirror.QueryAnalysisResult) *SemanticQueryAnalysisResult {
	return &SemanticQueryAnalysisResult{
		Status:         value.Status,
		Degraded:       value.Degraded,
		FallbackReason: value.FallbackReason,
		Provider:       value.Provider,
		Model:          value.Model,
		PromptVersion:  value.PromptVersion,
		Analysis:       semanticQueryAnalysisFromMirror(value.Analysis),
	}
}

func semanticQueryAnalysisFromMirror(value internalmirror.QueryAnalysis) SemanticQueryAnalysis {
	out := SemanticQueryAnalysis{
		TimeMode:          value.TimeMode,
		SemanticMode:      value.SemanticMode,
		Signals:           append([]string(nil), value.Signals...),
		MemoryDomain:      value.MemoryDomain,
		MemoryAbility:     value.MemoryAbility,
		EvidenceNeed:      value.EvidenceNeed,
		Confidence:        value.Confidence,
		FieldConfidence:   queryAnalysisConfidenceFromMirror(value.FieldConfidence),
		FieldProposals:    semanticFieldProposalsFromMirror(value.FieldProposals),
		Scores:            queryAnalysisScoresFromMirror(value.Scores),
		Probes:            queryAnchorProbeFromMirror(value.Probes),
		Decision:          queryAnalysisDecisionFromMirror(value.Decision),
		Evidence:          queryAnalysisEvidenceFromMirror(value.Evidence),
		Alternatives:      queryAnalysisAlternativesFromMirror(value.Alternatives),
		Subqueries:        append([]string(nil), value.Subqueries...),
		SafetyNotes:       append([]string(nil), value.SafetyNotes...),
		ContextBlockHints: append([]string(nil), value.ContextBlockHints...),
		PolicyHints:       queryPolicyHintsFromMirror(value.PolicyHints),
	}
	for _, mention := range value.EntityMentions {
		out.EntityMentions = append(out.EntityMentions, SemanticQueryEntityMention{
			EntityID:      mention.EntityID,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
			MatchKind:     mention.MatchKind,
			Confidence:    mention.Confidence,
		})
	}
	for _, rewrite := range value.QueryRewrites {
		out.QueryRewrites = append(out.QueryRewrites, QueryRewrite{
			Text:    rewrite.Text,
			Purpose: rewrite.Purpose,
			Weight:  rewrite.Weight,
		})
	}
	for _, anchor := range value.SemanticAnchors {
		out.SemanticAnchors = append(out.SemanticAnchors, SemanticAnchor{
			Text:       anchor.Text,
			AnchorType: anchor.AnchorType,
			EntityID:   anchor.EntityID,
			Weight:     anchor.Weight,
			Confidence: anchor.Confidence,
		})
	}
	return out
}

func visibleEntityHintsToMirror(values []VisibleEntityHint) []internalmirror.VisibleEntityHint {
	out := make([]internalmirror.VisibleEntityHint, 0, len(values))
	for _, value := range values {
		out = append(out, internalmirror.VisibleEntityHint{
			EntityID:      value.EntityID,
			CanonicalName: value.CanonicalName,
			Alias:         value.Alias,
			MatchText:     value.MatchText,
		})
	}
	return out
}

func allowedEnumsToMirror(value QueryAnalysisAllowedEnums) internalmirror.QueryAnalysisAllowedEnums {
	return internalmirror.QueryAnalysisAllowedEnums{
		TimeModes:          append([]string(nil), value.TimeModes...),
		Signals:            append([]string(nil), value.Signals...),
		MemoryDomains:      append([]string(nil), value.MemoryDomains...),
		MemoryAbilities:    append([]string(nil), value.MemoryAbilities...),
		EvidenceNeeds:      append([]string(nil), value.EvidenceNeeds...),
		EntityMentionKinds: append([]string(nil), value.EntityMentionKinds...),
		ContextBlockHints:  append([]string(nil), value.ContextBlockHints...),
	}
}

func queryAnalysisConfidenceToMirror(value memsqlite.QueryAnalysisConfidence) internalmirror.QueryAnalysisConfidence {
	return internalmirror.QueryAnalysisConfidence{
		Overall:          value.Overall,
		TimeMode:         value.TimeMode,
		MemoryAbility:    value.MemoryAbility,
		MemoryDomain:     value.MemoryDomain,
		EvidenceNeed:     value.EvidenceNeed,
		EntityResolution: value.EntityResolution,
	}
}

func queryAnalysisScoresToMirror(value memsqlite.QueryAnalysisScores) internalmirror.QueryAnalysisScores {
	return internalmirror.QueryAnalysisScores{
		RuleFit:                     value.RuleFit,
		AnchorReadiness:             value.AnchorReadiness,
		ExpectedRetrievalConfidence: value.ExpectedRetrievalConfidence,
		SemanticNeed:                value.SemanticNeed,
		Complexity:                  value.Complexity,
		Ambiguity:                   value.Ambiguity,
		Specificity:                 value.Specificity,
		SafetyRisk:                  value.SafetyRisk,
		IntentEvidence:              value.IntentEvidence,
		TimeEvidence:                value.TimeEvidence,
		DomainEvidence:              value.DomainEvidence,
		EvidenceNeedEvidence:        value.EvidenceNeedEvidence,
		EntityResolution:            value.EntityResolution,
		FieldConsistency:            value.FieldConsistency,
		DefaultFallbackPenalty:      value.DefaultFallbackPenalty,
		MultiIntentConflictPenalty:  value.MultiIntentConflictPenalty,
		SensitivityPenalty:          value.SensitivityPenalty,
	}
}

func queryAnchorProbeToMirror(value memsqlite.QueryAnchorProbe) internalmirror.QueryAnchorProbe {
	return internalmirror.QueryAnchorProbe{
		EntityExactConf:        value.EntityExactConf,
		EntityAmbiguity:        value.EntityAmbiguity,
		SparseProbeConf:        value.SparseProbeConf,
		PredicateProbeConf:     value.PredicateProbeConf,
		RecentProbeConf:        value.RecentProbeConf,
		PinnedCoreProbeConf:    value.PinnedCoreProbeConf,
		NarrativeProbeConf:     value.NarrativeProbeConf,
		FallbackSearchHitCount: value.FallbackSearchHitCount,
		Top1Score:              value.Top1Score,
		Top2Score:              value.Top2Score,
		Top1Margin:             value.Top1Margin,
		Breakdown:              queryAnchorProbeBreakdownToMirror(value.Breakdown),
	}
}

func queryAnchorProbeBreakdownToMirror(values []memsqlite.QueryAnchorProbeBreakdown) []internalmirror.QueryAnchorProbeBreakdown {
	if len(values) == 0 {
		return nil
	}
	out := make([]internalmirror.QueryAnchorProbeBreakdown, 0, len(values))
	for _, value := range values {
		out = append(out, internalmirror.QueryAnchorProbeBreakdown{
			Source:      value.Source,
			Confidence:  value.Confidence,
			HitCount:    value.HitCount,
			TopScore:    value.TopScore,
			SecondScore: value.SecondScore,
			Reason:      value.Reason,
			Error:       value.Error,
		})
	}
	return out
}

func queryAnalysisDecisionToMirror(value memsqlite.QueryAnalysisDecision) internalmirror.QueryAnalysisDecision {
	return internalmirror.QueryAnalysisDecision{
		UseSemantic:      value.UseSemantic,
		SemanticMode:     value.SemanticMode,
		RetrievalMode:    value.RetrievalMode,
		ReasonCodes:      append([]string(nil), value.ReasonCodes...),
		ThresholdVersion: value.ThresholdVersion,
		ScorerVersion:    value.ScorerVersion,
	}
}

func queryAnalysisEvidenceToMirror(values []memsqlite.QueryAnalysisEvidence) []internalmirror.QueryAnalysisEvidence {
	if len(values) == 0 {
		return nil
	}
	out := make([]internalmirror.QueryAnalysisEvidence, 0, len(values))
	for _, value := range values {
		out = append(out, internalmirror.QueryAnalysisEvidence{
			Field:     value.Field,
			Signal:    value.Signal,
			MatchText: value.MatchText,
			SpanStart: value.SpanStart,
			SpanEnd:   value.SpanEnd,
			Weight:    value.Weight,
			Detector:  value.Detector,
		})
	}
	return out
}

func queryAnalysisAlternativesToMirror(values []memsqlite.QueryAnalysisAlternative) []internalmirror.QueryAnalysisAlternative {
	if len(values) == 0 {
		return nil
	}
	out := make([]internalmirror.QueryAnalysisAlternative, 0, len(values))
	for _, value := range values {
		out = append(out, internalmirror.QueryAnalysisAlternative{
			Field:       value.Field,
			Value:       value.Value,
			Confidence:  value.Confidence,
			ReasonCodes: append([]string(nil), value.ReasonCodes...),
			Detector:    value.Detector,
		})
	}
	return out
}

func queryAnalysisConfidenceFromMirror(value internalmirror.QueryAnalysisConfidence) QueryAnalysisConfidence {
	return QueryAnalysisConfidence{
		Overall:          value.Overall,
		TimeMode:         value.TimeMode,
		MemoryAbility:    value.MemoryAbility,
		MemoryDomain:     value.MemoryDomain,
		EvidenceNeed:     value.EvidenceNeed,
		EntityResolution: value.EntityResolution,
	}
}

func semanticFieldProposalsFromMirror(values map[string]internalmirror.SemanticFieldProposal) map[string]SemanticFieldProposal {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]SemanticFieldProposal, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = SemanticFieldProposal{
			Value:      strings.TrimSpace(value.Value),
			Confidence: value.Confidence,
			Evidence:   append([]string(nil), value.Evidence...),
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func queryAnalysisScoresFromMirror(value internalmirror.QueryAnalysisScores) QueryAnalysisScores {
	return QueryAnalysisScores{
		RuleFit:                     value.RuleFit,
		AnchorReadiness:             value.AnchorReadiness,
		ExpectedRetrievalConfidence: value.ExpectedRetrievalConfidence,
		SemanticNeed:                value.SemanticNeed,
		Complexity:                  value.Complexity,
		Ambiguity:                   value.Ambiguity,
		Specificity:                 value.Specificity,
		SafetyRisk:                  value.SafetyRisk,
		IntentEvidence:              value.IntentEvidence,
		TimeEvidence:                value.TimeEvidence,
		DomainEvidence:              value.DomainEvidence,
		EvidenceNeedEvidence:        value.EvidenceNeedEvidence,
		EntityResolution:            value.EntityResolution,
		FieldConsistency:            value.FieldConsistency,
		DefaultFallbackPenalty:      value.DefaultFallbackPenalty,
		MultiIntentConflictPenalty:  value.MultiIntentConflictPenalty,
		SensitivityPenalty:          value.SensitivityPenalty,
	}
}

func queryAnchorProbeFromMirror(value internalmirror.QueryAnchorProbe) QueryAnchorProbe {
	return QueryAnchorProbe{
		EntityExactConf:        value.EntityExactConf,
		EntityAmbiguity:        value.EntityAmbiguity,
		SparseProbeConf:        value.SparseProbeConf,
		PredicateProbeConf:     value.PredicateProbeConf,
		RecentProbeConf:        value.RecentProbeConf,
		PinnedCoreProbeConf:    value.PinnedCoreProbeConf,
		NarrativeProbeConf:     value.NarrativeProbeConf,
		FallbackSearchHitCount: value.FallbackSearchHitCount,
		Top1Score:              value.Top1Score,
		Top2Score:              value.Top2Score,
		Top1Margin:             value.Top1Margin,
		Breakdown:              queryAnchorProbeBreakdownFromMirror(value.Breakdown),
	}
}

func queryAnchorProbeBreakdownFromMirror(values []internalmirror.QueryAnchorProbeBreakdown) []QueryAnchorProbeBreakdown {
	if len(values) == 0 {
		return nil
	}
	out := make([]QueryAnchorProbeBreakdown, 0, len(values))
	for _, value := range values {
		out = append(out, QueryAnchorProbeBreakdown{
			Source:      value.Source,
			Confidence:  value.Confidence,
			HitCount:    value.HitCount,
			TopScore:    value.TopScore,
			SecondScore: value.SecondScore,
			Reason:      value.Reason,
			Error:       value.Error,
		})
	}
	return out
}

func queryAnalysisDecisionFromMirror(value internalmirror.QueryAnalysisDecision) QueryAnalysisDecision {
	return QueryAnalysisDecision{
		UseSemantic:      value.UseSemantic,
		SemanticMode:     value.SemanticMode,
		RetrievalMode:    value.RetrievalMode,
		ReasonCodes:      append([]string(nil), value.ReasonCodes...),
		ThresholdVersion: value.ThresholdVersion,
		ScorerVersion:    value.ScorerVersion,
	}
}

func queryAnalysisEvidenceFromMirror(values []internalmirror.QueryAnalysisEvidence) []QueryAnalysisEvidence {
	if len(values) == 0 {
		return nil
	}
	out := make([]QueryAnalysisEvidence, 0, len(values))
	for _, value := range values {
		out = append(out, QueryAnalysisEvidence{
			Field:     value.Field,
			Signal:    value.Signal,
			MatchText: value.MatchText,
			SpanStart: value.SpanStart,
			SpanEnd:   value.SpanEnd,
			Weight:    value.Weight,
			Detector:  value.Detector,
		})
	}
	return out
}

func queryAnalysisAlternativesFromMirror(values []internalmirror.QueryAnalysisAlternative) []QueryAnalysisAlternative {
	if len(values) == 0 {
		return nil
	}
	out := make([]QueryAnalysisAlternative, 0, len(values))
	for _, value := range values {
		out = append(out, QueryAnalysisAlternative{
			Field:       value.Field,
			Value:       value.Value,
			Confidence:  value.Confidence,
			ReasonCodes: append([]string(nil), value.ReasonCodes...),
			Detector:    value.Detector,
		})
	}
	return out
}

func queryPolicyHintsToMirror(value memsqlite.QueryPolicyHints) internalmirror.QueryPolicyHints {
	return internalmirror.QueryPolicyHints{
		PreferEvidencedByLinks: value.PreferEvidencedByLinks,
		PreferSupersedesLinks:  value.PreferSupersedesLinks,
		PreferCausalLinks:      value.PreferCausalLinks,
		PreferCounterexamples:  value.PreferCounterexamples,
		PreferNarratives:       value.PreferNarratives,
		MaxHopsHint:            value.MaxHopsHint,
	}
}

func queryPolicyHintsFromMirror(value internalmirror.QueryPolicyHints) QueryPolicyHints {
	return QueryPolicyHints{
		PreferEvidencedByLinks: value.PreferEvidencedByLinks,
		PreferSupersedesLinks:  value.PreferSupersedesLinks,
		PreferCausalLinks:      value.PreferCausalLinks,
		PreferCounterexamples:  value.PreferCounterexamples,
		PreferNarratives:       value.PreferNarratives,
		MaxHopsHint:            value.MaxHopsHint,
	}
}

func cloneStoreQueryAnalysis(value memsqlite.QueryAnalysis) memsqlite.QueryAnalysis {
	out := value
	out.Terms = append([]string(nil), value.Terms...)
	out.EntityMentions = append([]memsqlite.QueryEntityMention(nil), value.EntityMentions...)
	out.Signals = append([]memsqlite.QuerySignal(nil), value.Signals...)
	out.Decision = cloneStoreQueryAnalysisDecision(value.Decision)
	out.Evidence = append([]memsqlite.QueryAnalysisEvidence(nil), value.Evidence...)
	out.Alternatives = cloneStoreQueryAnalysisAlternatives(value.Alternatives)
	out.QueryRewrites = append([]memsqlite.QueryRewrite(nil), value.QueryRewrites...)
	out.SemanticAnchors = append([]memsqlite.SemanticAnchor(nil), value.SemanticAnchors...)
	out.ContextBlockHints = append([]string(nil), value.ContextBlockHints...)
	if value.Diagnostics != nil {
		diagnostics := *value.Diagnostics
		diagnostics.Signals = append([]string(nil), value.Diagnostics.Signals...)
		diagnostics.RuleDecision = cloneStoreQueryAnalysisDecision(value.Diagnostics.RuleDecision)
		diagnostics.AdaptiveDecision = cloneStoreQueryAnalysisDecision(value.Diagnostics.AdaptiveDecision)
		diagnostics.RuleEvidence = append([]memsqlite.QueryAnalysisEvidence(nil), value.Diagnostics.RuleEvidence...)
		diagnostics.RuleAlternatives = cloneStoreQueryAnalysisAlternatives(value.Diagnostics.RuleAlternatives)
		diagnostics.DroppedRewriteReasons = append([]string(nil), value.Diagnostics.DroppedRewriteReasons...)
		diagnostics.DroppedSemanticAnchorReasons = append([]string(nil), value.Diagnostics.DroppedSemanticAnchorReasons...)
		diagnostics.FieldMergeDecisions = cloneStoreFieldMergeDecisions(value.Diagnostics.FieldMergeDecisions)
		diagnostics.SemanticAnalysis = cloneStoreSemanticQueryAnalysisDiagnostics(value.Diagnostics.SemanticAnalysis)
		out.Diagnostics = &diagnostics
	}
	return out
}

func cloneStoreSemanticQueryAnalysisDiagnostics(value *memsqlite.SemanticQueryAnalysisDiagnostics) *memsqlite.SemanticQueryAnalysisDiagnostics {
	if value == nil {
		return nil
	}
	out := *value
	out.Signals = append([]string(nil), value.Signals...)
	out.Decision = cloneStoreQueryAnalysisDecision(value.Decision)
	out.FieldProposals = cloneStoreSemanticFieldProposals(value.FieldProposals)
	out.Evidence = append([]memsqlite.QueryAnalysisEvidence(nil), value.Evidence...)
	out.Alternatives = cloneStoreQueryAnalysisAlternatives(value.Alternatives)
	out.EntityMentions = append([]memsqlite.SemanticQueryEntityMentionDiagnostics(nil), value.EntityMentions...)
	out.QueryRewrites = append([]memsqlite.QueryRewrite(nil), value.QueryRewrites...)
	out.SemanticAnchors = append([]memsqlite.SemanticAnchor(nil), value.SemanticAnchors...)
	out.Subqueries = append([]string(nil), value.Subqueries...)
	out.SafetyNotes = append([]string(nil), value.SafetyNotes...)
	out.ContextBlockHints = append([]string(nil), value.ContextBlockHints...)
	return &out
}

func cloneStoreSemanticFieldProposals(values map[string]memsqlite.SemanticFieldProposal) map[string]memsqlite.SemanticFieldProposal {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]memsqlite.SemanticFieldProposal, len(values))
	for key, value := range values {
		value.Evidence = append([]string(nil), value.Evidence...)
		out[key] = value
	}
	return out
}

func cloneStoreFieldMergeDecisions(values []memsqlite.FieldMergeDecision) []memsqlite.FieldMergeDecision {
	if len(values) == 0 {
		return nil
	}
	out := make([]memsqlite.FieldMergeDecision, 0, len(values))
	for _, value := range values {
		value.Evidence = append([]string(nil), value.Evidence...)
		out = append(out, value)
	}
	return out
}

func cloneStoreQueryAnalysisDecision(value memsqlite.QueryAnalysisDecision) memsqlite.QueryAnalysisDecision {
	out := value
	out.ReasonCodes = append([]string(nil), value.ReasonCodes...)
	return out
}

func isZeroStoreQueryAnalysisDecision(value memsqlite.QueryAnalysisDecision) bool {
	return !value.UseSemantic &&
		strings.TrimSpace(value.SemanticMode) == "" &&
		strings.TrimSpace(value.RetrievalMode) == "" &&
		len(value.ReasonCodes) == 0 &&
		strings.TrimSpace(value.ThresholdVersion) == "" &&
		strings.TrimSpace(value.ScorerVersion) == ""
}

func cloneStoreQueryAnalysisAlternatives(values []memsqlite.QueryAnalysisAlternative) []memsqlite.QueryAnalysisAlternative {
	if len(values) == 0 {
		return nil
	}
	out := make([]memsqlite.QueryAnalysisAlternative, 0, len(values))
	for _, value := range values {
		item := value
		item.ReasonCodes = append([]string(nil), value.ReasonCodes...)
		out = append(out, item)
	}
	return out
}

func retrievalPolicyToStore(policy RetrievalPolicy) memsqlite.RetrievalPolicy {
	return memsqlite.RetrievalPolicy{
		SensitivityPermission: policy.SensitivityPermission,
		AllowHistorical:       policy.AllowHistorical,
		AllowDeepArchive:      policy.AllowDeepArchive,
		FinalMemoryCount:      policy.FinalMemoryCount,
		ContextBudgetTokens:   policy.ContextBudgetTokens,
		UseFTS:                policy.UseFTS,
		UseMirror:             policy.UseMirror,
	}
}

func clampFloat(value float64, min float64, max float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func durationMillis(value time.Duration) int {
	if value <= 0 {
		return 0
	}
	ms := value / time.Millisecond
	if ms <= 0 {
		return 1
	}
	const maxInt32 = int64(1<<31 - 1)
	if ms > time.Duration(maxInt32) {
		return int(maxInt32)
	}
	return int(ms)
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
