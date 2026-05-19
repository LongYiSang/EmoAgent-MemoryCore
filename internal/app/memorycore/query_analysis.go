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
	c.mu.Lock()
	c.entries[key] = queryAnalysisCacheEntry{result: cloneSemanticQueryAnalysisResult(result), err: err}
	c.mu.Unlock()
	return result, err
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
	Now                        time.Time
	Timezone                   string
	RuleAnalysis               memsqlite.QueryAnalysis
	VisibleEntityHints         []VisibleEntityHint
	AllowedEnums               QueryAnalysisAllowedEnums
	RetrievalPolicy            RetrievalPolicy
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
	Signals           []string
	MemoryDomain      string
	MemoryAbility     string
	EvidenceNeed      string
	Confidence        float64
	FieldConfidence   QueryAnalysisConfidence
	EntityMentions    []SemanticQueryEntityMention
	QueryRewrites     []QueryRewrite
	SemanticAnchors   []SemanticAnchor
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

func newQueryAnalysisPipeline(rule RuleQueryAnalyzer, semantic SemanticQueryAnalyzer, options QueryAnalysisOptions) QueryAnalyzer {
	return queryAnalysisPipeline{
		rule:     rule,
		semantic: semantic,
		options:  normalizeQueryAnalysisOptions(options),
		now:      time.Now,
	}
}

func (p queryAnalysisPipeline) AnalyzeQuery(ctx context.Context, req QueryAnalysisRequest) (memsqlite.QueryAnalysis, error) {
	if p.rule == nil {
		return memsqlite.QueryAnalysis{}, fmt.Errorf("query analysis rule analyzer is required")
	}
	rule, err := p.rule.AnalyzeRuleQuery(ctx, req)
	if err != nil {
		return memsqlite.QueryAnalysis{}, err
	}
	rule.Source = memsqlite.QueryAnalysisSourceRuleOnly
	if !p.shouldUseSemantic(rule) {
		return rule, nil
	}
	if p.semantic == nil {
		return semanticRuleFallback(rule, "semantic_analyzer_missing", SemanticQueryAnalysisResult{}), nil
	}
	semanticReq := SemanticQueryAnalysisRequest{
		RequestID:                  req.RequestID,
		PersonaID:                  req.PersonaID,
		SessionID:                  req.SessionID,
		MessageID:                  req.MessageID,
		QueryText:                  req.QueryText,
		Now:                        req.Now,
		Timezone:                   req.Timezone,
		RuleAnalysis:               cloneStoreQueryAnalysis(rule),
		VisibleEntityHints:         visibleEntityHintsFromRule(rule),
		AllowedEnums:               defaultQueryAnalysisAllowedEnums(),
		RetrievalPolicy:            req.Policy,
		IncludeRationaleSummary:    p.options.IncludeRationaleSummary,
		MaxGeneratedDenseWeightSum: p.options.MaxGeneratedDenseWeightSum,
	}
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
		return semanticRuleFallback(rule, semanticFallbackReasonFromError(err), SemanticQueryAnalysisResult{LatencyMs: latencyMs}), nil
	}
	semantic.LatencyMs = latencyMs
	if !isValidSemanticQueryAnalysisStatus(semantic.Status) {
		return semanticRuleFallback(rule, "semantic_protocol_error", *semantic), nil
	}
	if semantic.Status != "ok" {
		return semanticRuleFallback(rule, sanitizeSemanticFallbackReason(semantic.FallbackReason, "semantic_sidecar_error"), *semantic), nil
	}
	if semantic.Degraded {
		return semanticRuleFallback(rule, sanitizeSemanticFallbackReason(semantic.FallbackReason, "semantic_unavailable"), *semantic), nil
	}
	return mergeSemanticQueryAnalysis(rule, *semantic, p.options, semanticReq.VisibleEntityHints), nil
}

func (p queryAnalysisPipeline) analyzeSemantic(ctx context.Context, req SemanticQueryAnalysisRequest) (*SemanticQueryAnalysisResult, error) {
	if p.options.Cache == nil {
		return p.semantic.AnalyzeSemanticQuery(ctx, req)
	}
	return p.options.Cache.Analyze(ctx, req, p.semantic)
}

func (p queryAnalysisPipeline) shouldUseSemantic(rule memsqlite.QueryAnalysis) bool {
	options := normalizeQueryAnalysisOptions(p.options)
	if options.Provider != QueryAnalysisProviderSidecar {
		return false
	}
	switch options.Mode {
	case QueryAnalysisModeSemanticAlways:
		return true
	case QueryAnalysisModeSemanticOnLowConfidence:
		return rule.Confidence < options.MinConfidenceToOverride
	default:
		return false
	}
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
	}
	return out
}

func mergeSemanticQueryAnalysis(rule memsqlite.QueryAnalysis, semantic SemanticQueryAnalysisResult, options QueryAnalysisOptions, hints []VisibleEntityHint) memsqlite.QueryAnalysis {
	options = normalizeQueryAnalysisOptions(options)
	out := cloneStoreQueryAnalysis(rule)
	analysis := semantic.Analysis
	if isValidQueryTimeMode(analysis.TimeMode) && confidenceForField(analysis.FieldConfidence.TimeMode, analysis.FieldConfidence.Overall, analysis.Confidence) >= options.MinConfidenceToOverride {
		out.TimeMode = memsqlite.QueryTimeMode(analysis.TimeMode)
	}
	if isValidMemoryDomain(analysis.MemoryDomain) && confidenceForField(analysis.FieldConfidence.MemoryDomain, analysis.FieldConfidence.Overall, analysis.Confidence) >= options.MinConfidenceToOverride {
		out.MemoryDomain = memsqlite.MemoryDomain(analysis.MemoryDomain)
	}
	if isValidMemoryAbility(analysis.MemoryAbility) && confidenceForField(analysis.FieldConfidence.MemoryAbility, analysis.FieldConfidence.Overall, analysis.Confidence) >= options.MinConfidenceToOverride {
		out.MemoryAbility = memsqlite.MemoryAbility(analysis.MemoryAbility)
	}
	if hasStoreQuerySignal(rule.Signals, memsqlite.QuerySignalForgetDelete) {
		out.MemoryAbility = rule.MemoryAbility
	}
	if isValidEvidenceNeed(analysis.EvidenceNeed) && confidenceForField(analysis.FieldConfidence.EvidenceNeed, analysis.FieldConfidence.Overall, analysis.Confidence) >= options.MinConfidenceToOverride {
		out.EvidenceNeed = memsqlite.EvidenceNeed(analysis.EvidenceNeed)
	}
	if validUnitConfidence(analysis.Confidence) && analysis.Confidence >= options.MinConfidenceToOverride {
		out.Confidence = analysis.Confidence
		out.FieldConfidence = queryAnalysisConfidenceToStore(analysis.FieldConfidence)
	}
	out.Signals = mergeQuerySignals(rule.Signals, analysis.Signals)
	applyHistoricalTransitionClamp(&out, rule)
	out.EntityMentions = mergeSemanticEntityMentions(rule.EntityMentions, analysis.EntityMentions, hints, options.MinEntitySemanticConfidence)
	budget := generatedWeightBudget(options)
	var rewriteDiagnostics rewriteSanitizationDiagnostics
	out.QueryRewrites, rewriteDiagnostics = sanitizedQueryRewrites(out.Raw, analysis.QueryRewrites, options.MaxQueryRewrites, &budget)
	out.SemanticAnchors = sanitizedSemanticAnchors(analysis.SemanticAnchors, hints, options, &budget)
	out.ContextBlockHints = sanitizedContextBlockHints(analysis.ContextBlockHints)
	out.PolicyHints = queryPolicyHintsToStore(analysis.PolicyHints)
	out.Source = memsqlite.QueryAnalysisSourceMerged
	out.Diagnostics = &memsqlite.QueryAnalysisDiagnostics{
		SemanticStatus:        defaultString(semantic.Status, "ok"),
		SemanticProvider:      semantic.Provider,
		SemanticModel:         semantic.Model,
		PromptVersion:         semantic.PromptVersion,
		SemanticLatencyMs:     semantic.LatencyMs,
		FallbackReason:        semantic.FallbackReason,
		RewriteCount:          len(out.QueryRewrites),
		SemanticAnchorCount:   len(out.SemanticAnchors),
		DroppedRewriteCount:   rewriteDiagnostics.DroppedCount,
		DroppedRewriteReasons: rewriteDiagnostics.DroppedReasons,
		EnglishRewriteCount:   rewriteDiagnostics.EnglishCount,
	}
	return out
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
	if options.MinConfidenceToOverride <= 0 || options.MinConfidenceToOverride > 1 {
		options.MinConfidenceToOverride = defaultQueryAnalysisMinConfidenceToOverride
	}
	if options.MinEntitySemanticConfidence <= 0 || options.MinEntitySemanticConfidence > 1 {
		options.MinEntitySemanticConfidence = defaultQueryAnalysisMinEntityConfidence
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

func semanticFallbackReasonFromError(err error) string {
	if err == nil {
		return "semantic_protocol_error"
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
		"provider_none", "missing_api_key", "invalid_json", "invalid_response", "validation_failed", "provider_error", "provider_timeout":
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
		Debug: internalmirror.QueryAnalysisDebug{IncludeRationaleSummary: req.IncludeRationaleSummary},
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

func sanitizedSemanticAnchors(values []SemanticAnchor, hints []VisibleEntityHint, options QueryAnalysisOptions, budget *float64) []memsqlite.SemanticAnchor {
	if options.MaxSemanticAnchors <= 0 || len(values) == 0 {
		return nil
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
	return out
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
		Signals:           append([]string(nil), value.Signals...),
		MemoryDomain:      value.MemoryDomain,
		MemoryAbility:     value.MemoryAbility,
		EvidenceNeed:      value.EvidenceNeed,
		Confidence:        value.Confidence,
		FieldConfidence:   queryAnalysisConfidenceFromMirror(value.FieldConfidence),
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
	out.QueryRewrites = append([]memsqlite.QueryRewrite(nil), value.QueryRewrites...)
	out.SemanticAnchors = append([]memsqlite.SemanticAnchor(nil), value.SemanticAnchors...)
	out.ContextBlockHints = append([]string(nil), value.ContextBlockHints...)
	if value.Diagnostics != nil {
		diagnostics := *value.Diagnostics
		diagnostics.DroppedRewriteReasons = append([]string(nil), value.Diagnostics.DroppedRewriteReasons...)
		out.Diagnostics = &diagnostics
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

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
