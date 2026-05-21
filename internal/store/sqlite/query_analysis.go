package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"unicode"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type QueryTimeMode string
type QuerySignal string
type MemoryDomain string
type MemoryAbility string
type EvidenceNeed string
type QueryEntityMentionKind string
type QueryAnalysisSource string

const (
	QueryTimeModeCurrent         QueryTimeMode = "current"
	QueryTimeModeHistorical      QueryTimeMode = "historical"
	QueryTimeModeBitemporalCheck QueryTimeMode = "bitemporal_check"

	QuerySignalCausal                QuerySignal = "causal"
	QuerySignalHistorical            QuerySignal = "historical"
	QuerySignalProvenance            QuerySignal = "provenance"
	QuerySignalSensitivity           QuerySignal = "sensitivity"
	QuerySignalDebug                 QuerySignal = "debug"
	QuerySignalPremiseCheck          QuerySignal = "premise_check"
	QuerySignalRelationshipArc       QuerySignal = "relationship_arc"
	QuerySignalForgetDelete          QuerySignal = "forget_delete"
	QuerySignalPastEventDirectFact   QuerySignal = "past_event_direct_fact"
	QuerySignalStateTransition       QuerySignal = "state_transition"
	QuerySignalProvenanceSource      QuerySignal = "provenance_source"
	QuerySignalCausalChain           QuerySignal = "causal_chain"
	QuerySignalPremiseCounterexample QuerySignal = "premise_counterexample"
	QuerySignalEventBundle           QuerySignal = "event_bundle"
	QuerySignalReflectionSummary     QuerySignal = "reflection_summary"
	QuerySignalExactFact             QuerySignal = "exact_fact"

	MemoryDomainRelationship          MemoryDomain = "relationship_memory"
	MemoryDomainUserProfile           MemoryDomain = "user_profile_memory"
	MemoryDomainWorkExperience        MemoryDomain = "work_experience_memory"
	MemoryDomainEnvironmentExperience MemoryDomain = "environment_experience_memory"

	MemoryAbilityDirectFact      MemoryAbility = "direct_fact"
	MemoryAbilityCausalExplain   MemoryAbility = "causal_explain"
	MemoryAbilityHistorical      MemoryAbility = "historical"
	MemoryAbilityProvenance      MemoryAbility = "provenance"
	MemoryAbilityBoundary        MemoryAbility = "boundary"
	MemoryAbilitySupportive      MemoryAbility = "supportive"
	MemoryAbilityPlanning        MemoryAbility = "planning"
	MemoryAbilityStaticState     MemoryAbility = "static_state"
	MemoryAbilityDynamicState    MemoryAbility = "dynamic_state"
	MemoryAbilityWorkflow        MemoryAbility = "workflow"
	MemoryAbilityGotcha          MemoryAbility = "gotcha"
	MemoryAbilityPremiseCheck    MemoryAbility = "premise_check"
	MemoryAbilityRelationshipArc MemoryAbility = "relationship_arc"

	EvidenceNeedExactObservation      EvidenceNeed = "exact_observation"
	EvidenceNeedStateTransition       EvidenceNeed = "state_transition"
	EvidenceNeedProcedureNote         EvidenceNeed = "procedure_note"
	EvidenceNeedGotchaNote            EvidenceNeed = "gotcha_note"
	EvidenceNeedPremiseCounterexample EvidenceNeed = "premise_counterexample"
	EvidenceNeedProvenanceSource      EvidenceNeed = "provenance_source"
	EvidenceNeedRelationshipTimeline  EvidenceNeed = "relationship_timeline"

	QueryEntityMentionKindCanonical QueryEntityMentionKind = "canonical_name"
	QueryEntityMentionKindAlias     QueryEntityMentionKind = "entity_alias"

	QueryAnalysisSourceRuleOnly         QueryAnalysisSource = "rule_only"
	QueryAnalysisSourceSemantic         QueryAnalysisSource = "semantic"
	QueryAnalysisSourceMerged           QueryAnalysisSource = "merged"
	QueryAnalysisSourceSemanticFallback QueryAnalysisSource = "semantic_failed_rule_fallback"
)

const ruleFeatureScorerVersion = "query_analysis_rule_feature_scorer.v1"

type QueryAnalysis struct {
	Raw               string
	Normalized        string
	Terms             []string
	EntityMentions    []QueryEntityMention
	TimeMode          QueryTimeMode
	Signals           []QuerySignal
	MemoryDomain      MemoryDomain
	MemoryAbility     MemoryAbility
	EvidenceNeed      EvidenceNeed
	Source            QueryAnalysisSource
	Confidence        float64
	FieldConfidence   QueryAnalysisConfidence
	Scores            QueryAnalysisScores
	Probes            QueryAnchorProbe
	Decision          QueryAnalysisDecision
	Evidence          []QueryAnalysisEvidence
	Alternatives      []QueryAnalysisAlternative
	QueryRewrites     []QueryRewrite
	SemanticAnchors   []SemanticAnchor
	ContextBlockHints []string
	PolicyHints       QueryPolicyHints
	Diagnostics       *QueryAnalysisDiagnostics
}

type QueryEntityMention struct {
	EntityID      string
	CanonicalName string
	Alias         string
	MatchText     string
	MatchKind     QueryEntityMentionKind
}

type QueryRewrite struct {
	Text    string
	Purpose string
	Weight  float64
}

type SemanticAnchor struct {
	Text       string
	AnchorType string
	EntityID   string
	Weight     float64
	Confidence float64
}

type QueryAnalysisConfidence struct {
	Overall          float64
	TimeMode         float64
	MemoryAbility    float64
	MemoryDomain     float64
	EvidenceNeed     float64
	EntityResolution float64
}

type QueryAnalysisScores struct {
	RuleFit                     float64
	AnchorReadiness             float64
	ExpectedRetrievalConfidence float64
	SemanticNeed                float64
	Complexity                  float64
	Ambiguity                   float64
	Specificity                 float64
	SafetyRisk                  float64
	IntentEvidence              float64
	TimeEvidence                float64
	DomainEvidence              float64
	EvidenceNeedEvidence        float64
	EntityResolution            float64
	FieldConsistency            float64
	DefaultFallbackPenalty      float64
	MultiIntentConflictPenalty  float64
	SensitivityPenalty          float64
}

type QueryAnchorProbe struct {
	EntityExactConf        float64
	EntityAmbiguity        float64
	SparseProbeConf        float64
	PredicateProbeConf     float64
	RecentProbeConf        float64
	PinnedCoreProbeConf    float64
	NarrativeProbeConf     float64
	FallbackSearchHitCount int
	Top1Score              float64
	Top2Score              float64
	Top1Margin             float64
	Breakdown              []QueryAnchorProbeBreakdown
}

type QueryAnchorProbeBreakdown struct {
	Source      string
	Status      string
	Confidence  float64
	HitCount    int
	TopScore    float64
	SecondScore float64
	Reason      string
	Error       string
}

type QueryAnalysisDecision struct {
	UseSemantic      bool
	SemanticMode     string
	RetrievalMode    string
	ReasonCodes      []string
	ThresholdVersion string
	ScorerVersion    string
}

type QueryAnalysisEvidence struct {
	Field     string
	Signal    string
	MatchText string
	SpanStart int
	SpanEnd   int
	Weight    float64
	Detector  string
}

type QueryAnalysisAlternative struct {
	Field       string
	Value       string
	Confidence  float64
	ReasonCodes []string
	Detector    string
}

type QueryPolicyHints struct {
	PreferEvidencedByLinks bool
	PreferSupersedesLinks  bool
	PreferCausalLinks      bool
	PreferCounterexamples  bool
	PreferNarratives       bool
	MaxHopsHint            int
}

type QueryAnalysisDiagnostics struct {
	ScorerVersion                string
	RuleConfidenceLegacy         float64
	RuleConfidenceReason         string
	SemanticDecisionLegacy       bool
	MinConfidenceToOverride      float64
	Signals                      []string
	EntityMentionCount           int
	Scores                       QueryAnalysisScores
	FieldConfidence              QueryAnalysisConfidence
	RuleDecision                 QueryAnalysisDecision
	AdaptiveDecision             QueryAnalysisDecision
	RuleEvidence                 []QueryAnalysisEvidence
	RuleAlternatives             []QueryAnalysisAlternative
	SemanticStatus               string
	SemanticProvider             string
	SemanticModel                string
	PromptVersion                string
	SemanticLatencyMs            int64
	FallbackReason               string
	RewriteCount                 int
	SemanticAnchorCount          int
	DroppedRewriteCount          int
	DroppedRewriteReasons        []string
	DroppedSemanticAnchorCount   int
	DroppedSemanticAnchorReasons []string
	EnglishRewriteCount          int
	SemanticDriftCount           int
	FieldMergeDecisions          []FieldMergeDecision
	SemanticAnalysis             *SemanticQueryAnalysisDiagnostics
}

type SemanticQueryAnalysisDiagnostics struct {
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
	EntityMentions    []SemanticQueryEntityMentionDiagnostics
	QueryRewrites     []QueryRewrite
	SemanticAnchors   []SemanticAnchor
	Subqueries        []string
	SafetyNotes       []string
	ContextBlockHints []string
	PolicyHints       QueryPolicyHints
}

type SemanticFieldProposal struct {
	Value      string
	Confidence float64
	Evidence   []string
}

type FieldMergeDecision struct {
	Field              string
	RuleValue          string
	SemanticValue      string
	RuleConfidence     float64
	SemanticConfidence float64
	Reason             string
	Evidence           []string
	UseSemantic        bool
}

type SemanticQueryEntityMentionDiagnostics struct {
	EntityID      string
	CanonicalName string
	Alias         string
	MatchText     string
	MatchKind     string
	Confidence    float64
}

func (r *RetrievalRepository) AnalyzeQuery(ctx context.Context, personaID string, query string, policy RetrievalPolicy) (QueryAnalysis, error) {
	return r.analyzeQuery(ctx, personaID, query, normalizeRetrievalPolicy(policy))
}

func (r *RetrievalRepository) analyzeQuery(ctx context.Context, personaID string, query string, policy RetrievalPolicy) (QueryAnalysis, error) {
	raw := strings.TrimSpace(query)
	normalized := strings.ToLower(raw)
	analysis := QueryAnalysis{
		Raw:           raw,
		Normalized:    normalized,
		Terms:         strings.Fields(normalized),
		TimeMode:      queryTimeMode(normalized),
		MemoryDomain:  queryMemoryDomain(normalized),
		MemoryAbility: queryMemoryAbility(normalized),
		EvidenceNeed:  queryEvidenceNeed(normalized),
		Source:        QueryAnalysisSourceRuleOnly,
	}
	analysis.Signals = querySignals(normalized, analysis.TimeMode)
	mentions, err := r.matchEntityMentions(ctx, personaID, normalized, policy)
	if err != nil {
		return QueryAnalysis{}, err
	}
	analysis.EntityMentions = mentions
	legacy := ruleConfidenceLegacy(normalized, analysis)
	analysis.Evidence = ruleQueryAnalysisEvidence(normalized, analysis)
	analysis.Probes = r.probeQueryAnchors(ctx, personaID, normalized, analysis, policy)
	analysis.Scores = ComputeRuleFit(normalized, analysis, analysis.Evidence)
	analysis.Confidence = analysis.Scores.ExpectedRetrievalConfidence
	analysis.FieldConfidence = ComputeFieldConfidence(analysis, analysis.Scores)
	analysis.Alternatives = ruleQueryAnalysisAlternatives(normalized, analysis)
	analysis.Decision = ruleQueryAnalysisDecision(analysis, analysis.Scores)
	analysis.Diagnostics = ruleQueryAnalysisDiagnostics(normalized, analysis, legacy, analysis.Scores, analysis.FieldConfidence)
	return analysis, nil
}

func cloneQueryAnalysis(value QueryAnalysis) QueryAnalysis {
	out := value
	out.Terms = append([]string(nil), value.Terms...)
	out.EntityMentions = append([]QueryEntityMention(nil), value.EntityMentions...)
	out.Signals = append([]QuerySignal(nil), value.Signals...)
	out.Probes = cloneQueryAnchorProbe(value.Probes)
	out.Decision = cloneQueryAnalysisDecision(value.Decision)
	out.Evidence = cloneQueryAnalysisEvidence(value.Evidence)
	out.Alternatives = cloneQueryAnalysisAlternatives(value.Alternatives)
	out.QueryRewrites = append([]QueryRewrite(nil), value.QueryRewrites...)
	out.SemanticAnchors = append([]SemanticAnchor(nil), value.SemanticAnchors...)
	out.ContextBlockHints = append([]string(nil), value.ContextBlockHints...)
	if value.Diagnostics != nil {
		diagnostics := *value.Diagnostics
		diagnostics.Signals = append([]string(nil), value.Diagnostics.Signals...)
		diagnostics.RuleDecision = cloneQueryAnalysisDecision(value.Diagnostics.RuleDecision)
		diagnostics.AdaptiveDecision = cloneQueryAnalysisDecision(value.Diagnostics.AdaptiveDecision)
		diagnostics.RuleEvidence = cloneQueryAnalysisEvidence(value.Diagnostics.RuleEvidence)
		diagnostics.RuleAlternatives = cloneQueryAnalysisAlternatives(value.Diagnostics.RuleAlternatives)
		diagnostics.DroppedRewriteReasons = append([]string(nil), value.Diagnostics.DroppedRewriteReasons...)
		diagnostics.DroppedSemanticAnchorReasons = append([]string(nil), value.Diagnostics.DroppedSemanticAnchorReasons...)
		diagnostics.FieldMergeDecisions = cloneFieldMergeDecisions(value.Diagnostics.FieldMergeDecisions)
		diagnostics.SemanticAnalysis = cloneSemanticQueryAnalysisDiagnostics(value.Diagnostics.SemanticAnalysis)
		out.Diagnostics = &diagnostics
	}
	return out
}

func cloneSemanticQueryAnalysisDiagnostics(value *SemanticQueryAnalysisDiagnostics) *SemanticQueryAnalysisDiagnostics {
	if value == nil {
		return nil
	}
	out := *value
	out.Signals = append([]string(nil), value.Signals...)
	out.Probes = cloneQueryAnchorProbe(value.Probes)
	out.FieldProposals = cloneSemanticFieldProposals(value.FieldProposals)
	out.Decision = cloneQueryAnalysisDecision(value.Decision)
	out.Evidence = cloneQueryAnalysisEvidence(value.Evidence)
	out.Alternatives = cloneQueryAnalysisAlternatives(value.Alternatives)
	out.EntityMentions = append([]SemanticQueryEntityMentionDiagnostics(nil), value.EntityMentions...)
	out.QueryRewrites = append([]QueryRewrite(nil), value.QueryRewrites...)
	out.SemanticAnchors = append([]SemanticAnchor(nil), value.SemanticAnchors...)
	out.Subqueries = append([]string(nil), value.Subqueries...)
	out.SafetyNotes = append([]string(nil), value.SafetyNotes...)
	out.ContextBlockHints = append([]string(nil), value.ContextBlockHints...)
	return &out
}

func cloneQueryAnchorProbe(value QueryAnchorProbe) QueryAnchorProbe {
	out := value
	out.Breakdown = append([]QueryAnchorProbeBreakdown(nil), value.Breakdown...)
	return out
}

func cloneQueryAnalysisDecision(value QueryAnalysisDecision) QueryAnalysisDecision {
	out := value
	out.ReasonCodes = append([]string(nil), value.ReasonCodes...)
	return out
}

func cloneQueryAnalysisEvidence(values []QueryAnalysisEvidence) []QueryAnalysisEvidence {
	return append([]QueryAnalysisEvidence(nil), values...)
}

func cloneQueryAnalysisAlternatives(values []QueryAnalysisAlternative) []QueryAnalysisAlternative {
	out := make([]QueryAnalysisAlternative, 0, len(values))
	for _, value := range values {
		item := value
		item.ReasonCodes = append([]string(nil), value.ReasonCodes...)
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneSemanticFieldProposals(values map[string]SemanticFieldProposal) map[string]SemanticFieldProposal {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]SemanticFieldProposal, len(values))
	for key, value := range values {
		value.Evidence = append([]string(nil), value.Evidence...)
		out[key] = value
	}
	return out
}

func cloneFieldMergeDecisions(values []FieldMergeDecision) []FieldMergeDecision {
	if len(values) == 0 {
		return nil
	}
	out := make([]FieldMergeDecision, 0, len(values))
	for _, value := range values {
		value.Evidence = append([]string(nil), value.Evidence...)
		out = append(out, value)
	}
	return out
}

func queryTimeMode(normalized string) QueryTimeMode {
	if hasStateTransitionIntent(normalized) {
		return QueryTimeModeHistorical
	}
	if hasPastEventDirectFactIntent(normalized) {
		return QueryTimeModeHistorical
	}
	if containsAny(normalized, "以前", "过去", "上次", "历史", "之前", "曾经", "从前", "prior", "previous", "last time", "historical", "history", "before") {
		return QueryTimeModeHistorical
	}
	if hasUniversalPremiseIntent(normalized) {
		return QueryTimeModeBitemporalCheck
	}
	return QueryTimeModeCurrent
}

func querySignals(normalized string, timeMode QueryTimeMode) []QuerySignal {
	var signals []QuerySignal
	premiseCheckIntent := hasPremiseCheckIntent(normalized)
	stateTransitionIntent := hasStateTransitionIntent(normalized)
	if hasPastEventDirectFactIntent(normalized) && !stateTransitionIntent && !premiseCheckIntent {
		signals = append(signals, QuerySignalPastEventDirectFact)
		if hasEventBundleSlotLanguage(normalized) {
			signals = append(signals, QuerySignalEventBundle)
		}
	}
	if stateTransitionIntent {
		signals = append(signals, QuerySignalStateTransition)
	}
	if hasProvenanceSourceIntent(normalized) {
		signals = append(signals, QuerySignalProvenanceSource)
	}
	if hasReflectionSummaryIntent(normalized) {
		signals = append(signals, QuerySignalReflectionSummary)
	}
	if premiseCheckIntent {
		signals = append(signals, QuerySignalPremiseCounterexample)
	}
	if hasCausalExplainIntent(normalized) {
		signals = append(signals, QuerySignalCausal)
		if stateTransitionIntent {
			signals = append(signals, QuerySignalCausalChain)
		}
	}
	if timeMode == QueryTimeModeHistorical {
		signals = append(signals, QuerySignalHistorical)
	}
	if hasProvenanceSourceIntent(normalized) {
		signals = append(signals, QuerySignalProvenance)
	}
	if premiseCheckIntent {
		signals = append(signals, QuerySignalPremiseCheck)
	}
	if hasRelationshipArcIntent(normalized) {
		signals = append(signals, QuerySignalRelationshipArc)
	}
	if containsAny(normalized, "隐私", "敏感", "不要提", "别提", "不要再提", "忘掉", "边界", "boundary", "private", "sensitive") {
		signals = append(signals, QuerySignalSensitivity)
	}
	if containsAny(normalized, "忘掉", "删除", "删掉", "清除", "不要再提", "forget", "delete", "remove") {
		signals = append(signals, QuerySignalForgetDelete)
	}
	if containsAny(normalized, "debug", "调试", "diagnostic", "diagnostics", "诊断") {
		signals = append(signals, QuerySignalDebug)
	}
	if len(signals) == 0 && strings.TrimSpace(normalized) != "" {
		signals = append(signals, QuerySignalExactFact)
	}
	return signals
}

func hasHistoricalTransitionIntent(normalized string) bool {
	return hasStateTransitionIntent(normalized)
}

func hasPastEventDirectFactIntent(normalized string) bool {
	return containsAny(normalized, "上次", "那天", "五一", "有一次", "周末", "最近一次", "前几天")
}

func hasStateTransitionIntent(normalized string) bool {
	if strings.Contains(normalized, "不再") && strings.Contains(normalized, "开始") {
		return true
	}
	if strings.Contains(normalized, "从") && (strings.Contains(normalized, "变成") || strings.Contains(normalized, "变为") || strings.Contains(normalized, "变得")) {
		return true
	}
	return hasOldStateMarker(normalized) && hasNewStateMarker(normalized)
}

func hasOldStateMarker(normalized string) bool {
	return containsAny(normalized, "一开始", "之前", "以前", "曾经", "从前", "原来", "过去", "从来不", "以前从来", "起初")
}

func hasNewStateMarker(normalized string) bool {
	return containsAny(normalized, "后来", "最近", "现在", "已经", "开始", "不再", "效果怎么样", "发生变化") ||
		containsAny(normalized, "变成", "变为", "变得", "和好", "和解", "翻篇")
}

func hasProvenanceSourceIntent(normalized string) bool {
	return containsAny(
		normalized,
		"什么时候告诉我的",
		"哪次告诉我的",
		"什么时候说过",
		"哪次说过",
		"从哪里知道",
		"哪里知道的",
		"来源",
		"证据",
		"谁告诉我的",
		"什么时候说的",
		"最早什么时候",
		"source",
		"evidence",
		"provenance",
	)
}

func hasReflectionSummaryIntent(normalized string) bool {
	if containsAny(normalized, "反思", "成长", "最近进步") {
		return true
	}
	return containsAny(normalized, "这两个月", "最近") && containsAny(normalized, "变化", "进步")
}

func hasUniversalPremiseIntent(normalized string) bool {
	return containsAny(
		normalized,
		"是不是完全",
		"是否完全",
		"完全没有",
		"没有任何",
		"从来没",
		"从来没有",
		"从来不",
		"一直都",
		"一直没有",
		"一直没",
		"一直不",
		"是否一直",
		"是不是一直",
		"所有",
		"每个",
		"任何",
		"什么都",
		"必须",
		"从头到尾",
		"总是",
		"每次",
		"永远",
		"always",
		"never",
		"every time",
	)
}

func hasPremiseCheckIntent(normalized string) bool {
	return hasUniversalPremiseIntent(normalized) ||
		hasConditionalPremiseCheckIntent(normalized) ||
		hasPremiseChallengeEvidenceIntent(normalized)
}

func hasConditionalPremiseCheckIntent(normalized string) bool {
	if !containsAny(normalized, "如果", "假如", "一旦", "if ") {
		return false
	}
	return containsAny(
		normalized,
		"是否",
		"是不是",
		"会不会",
		"能不能",
		"可不可以",
		"还能",
		"还会不会",
		"can ",
		"will ",
	)
}

func hasPremiseChallengeEvidenceIntent(normalized string) bool {
	return containsAny(
		normalized,
		"有没有反例",
		"有反例",
		"反例",
		"有没有例外",
		"有例外",
		"例外",
		"推翻",
		"打脸",
		"还成立吗",
		"仍然成立吗",
		"counterexample",
		"disprove",
		"exception",
	)
}

func hasEventBundleSlotLanguage(normalized string) bool {
	slotCategories := 0
	if containsAny(normalized, "跟谁", "和谁", "谁") {
		slotCategories++
	}
	if containsAny(normalized, "多久", "多长时间", "排了多久") {
		slotCategories++
	}
	if containsAny(normalized, "哪里", "哪儿") {
		slotCategories++
	}
	if containsAny(normalized, "什么时候") {
		slotCategories++
	}
	return slotCategories >= 2
}

func hasCausalExplainIntent(normalized string) bool {
	return containsAny(normalized, "为什么", "原因", "导致", "怎么会", "为何", "why", "cause", "caused", "because") &&
		!hasDirectEventReasonSlotIntent(normalized)
}

func hasDirectEventReasonSlotIntent(normalized string) bool {
	if !containsAny(normalized, "因为什么", "什么原因", "什么事情", "什么事", "什么由头", "什么名义", "what occasion", "what reason") {
		return false
	}
	return containsAny(normalized,
		"庆祝", "祝贺", "纪念", "请客", "请大家", "聚餐", "活动", "仪式", "安排",
		"celebrate", "celebration", "occasion", "treat",
	)
}

func queryMemoryDomain(normalized string) MemoryDomain {
	if containsAny(normalized, "环境", "路径", "依赖", "python", "uv", "windows", "powershell", "权限", "toolchain", "runtime", "缓存", "cache") {
		return MemoryDomainEnvironmentExperience
	}
	if containsAny(normalized, "部署", "上线", "ci", "测试", "命令", "repo", "仓库", "构建", "编译", "工作流", "workflow", "任务", "pr", "commit", "branch") {
		return MemoryDomainWorkExperience
	}
	if containsAny(normalized, "我是谁", "身份", "名字", "昵称", "偏好", "喜欢", "讨厌", "住在", "profile", "preference", "identity") {
		return MemoryDomainUserProfile
	}
	return MemoryDomainRelationship
}

func queryMemoryAbility(normalized string) MemoryAbility {
	switch {
	case hasProvenanceSourceIntent(normalized):
		return MemoryAbilityProvenance
	case hasRelationshipArcIntent(normalized):
		return MemoryAbilityRelationshipArc
	case hasStateTransitionIntent(normalized):
		return MemoryAbilityHistorical
	case hasPremiseCheckIntent(normalized):
		return MemoryAbilityPremiseCheck
	case hasPastEventDirectFactIntent(normalized):
		return MemoryAbilityDirectFact
	case hasCausalExplainIntent(normalized):
		return MemoryAbilityCausalExplain
	case containsAny(normalized, "忘掉", "删除", "删掉", "清除", "不要提", "别提", "不要再提", "边界", "不要提醒", "forget", "delete", "remove", "boundary"):
		return MemoryAbilityBoundary
	case containsAny(normalized, "支持", "安慰", "鼓励", "陪伴", "support", "supportive"):
		return MemoryAbilitySupportive
	case hasReflectionSummaryIntent(normalized):
		return MemoryAbilityHistorical
	case containsAny(normalized, "坑", "踩坑", "失败", "报错", "错误", "故障", "gotcha", "pitfall", "failed", "failure", "error"):
		return MemoryAbilityGotcha
	case containsAny(normalized, "流程", "步骤", "怎么做", "操作步骤", "workflow", "procedure"):
		return MemoryAbilityWorkflow
	case hasDynamicStateIntent(normalized):
		return MemoryAbilityDynamicState
	case hasStaticStateIntent(normalized):
		return MemoryAbilityStaticState
	case containsAny(normalized, "计划", "规划", "planning"):
		return MemoryAbilityPlanning
	default:
		return MemoryAbilityDirectFact
	}
}

func queryEvidenceNeed(normalized string) EvidenceNeed {
	switch {
	case hasProvenanceSourceIntent(normalized):
		return EvidenceNeedProvenanceSource
	case hasRelationshipArcIntent(normalized):
		return EvidenceNeedRelationshipTimeline
	case hasStateTransitionIntent(normalized):
		return EvidenceNeedStateTransition
	case hasPremiseCheckIntent(normalized):
		return EvidenceNeedPremiseCounterexample
	case hasPastEventDirectFactIntent(normalized) && !hasStateTransitionIntent(normalized):
		return EvidenceNeedExactObservation
	case containsAny(normalized, "坑", "踩坑", "失败", "报错", "错误", "故障", "gotcha", "pitfall", "failed", "failure", "error"):
		return EvidenceNeedGotchaNote
	case containsAny(normalized, "流程", "步骤", "怎么做", "操作步骤", "workflow", "procedure"):
		return EvidenceNeedProcedureNote
	case hasDynamicStateIntent(normalized):
		return EvidenceNeedStateTransition
	default:
		return EvidenceNeedExactObservation
	}
}

func hasDynamicStateIntent(normalized string) bool {
	return containsAny(
		normalized,
		"最近状态",
		"当前状态",
		"最新状态",
		"进度",
		"进展",
		"变化",
		"有没有变",
		"latest status",
		"current status",
		"progress",
		"update",
		"changed",
	)
}

func hasRelationshipArcIntent(normalized string) bool {
	return containsAny(
		normalized,
		"关系变化",
		"关系轨迹",
		"关系时间线",
		"关系发展",
		"relationship arc",
		"relationship timeline",
	)
}

func ruleConfidence(normalized string, analysis QueryAnalysis) float64 {
	return ComputeRuleFit(normalized, analysis, analysis.Evidence).ExpectedRetrievalConfidence
}

type ruleConfidenceLegacyResult struct {
	Score  float64
	Reason string
}

func ruleConfidenceLegacy(normalized string, analysis QueryAnalysis) ruleConfidenceLegacyResult {
	switch {
	case normalized == "":
		return ruleConfidenceLegacyResult{Score: 0, Reason: "empty_query"}
	case analysis.MemoryAbility != MemoryAbilityDirectFact:
		return ruleConfidenceLegacyResult{Score: 0.78, Reason: "non_direct_memory_ability"}
	case len(analysis.EntityMentions) > 0:
		return ruleConfidenceLegacyResult{Score: 0.74, Reason: "entity_mention"}
	case onlyExactFactSignal(analysis.Signals):
		return ruleConfidenceLegacyResult{Score: 0.42, Reason: "exact_fact_only"}
	case len(analysis.Signals) > 0:
		return ruleConfidenceLegacyResult{Score: 0.68, Reason: "query_signal"}
	default:
		return ruleConfidenceLegacyResult{Score: 0.42, Reason: "default_direct_fact"}
	}
}

func ruleQueryAnalysisDiagnostics(normalized string, analysis QueryAnalysis, legacy ruleConfidenceLegacyResult, scores QueryAnalysisScores, fieldConfidence QueryAnalysisConfidence) *QueryAnalysisDiagnostics {
	return &QueryAnalysisDiagnostics{
		ScorerVersion:        ruleFeatureScorerVersion,
		RuleConfidenceLegacy: legacy.Score,
		RuleConfidenceReason: legacy.Reason,
		Signals:              querySignalsToStrings(analysis.Signals),
		EntityMentionCount:   len(analysis.EntityMentions),
		Scores:               scores,
		FieldConfidence:      fieldConfidence,
		RuleDecision:         cloneQueryAnalysisDecision(analysis.Decision),
		RuleEvidence:         cloneQueryAnalysisEvidence(analysis.Evidence),
		RuleAlternatives:     cloneQueryAnalysisAlternatives(analysis.Alternatives),
	}
}

func ruleQueryAnalysisEvidence(normalized string, analysis QueryAnalysis) []QueryAnalysisEvidence {
	if strings.TrimSpace(normalized) == "" {
		return nil
	}
	var evidence []QueryAnalysisEvidence
	evidence = appendRuleTimeEvidence(evidence, normalized, analysis)
	evidence = appendRuleAbilityEvidence(evidence, normalized, analysis)
	evidence = appendRuleDomainEvidence(evidence, normalized, analysis)
	evidence = appendRuleEvidenceNeedEvidence(evidence, normalized, analysis)
	evidence = appendRuleEntityEvidence(evidence, analysis)
	return evidence
}

func appendRuleTimeEvidence(out []QueryAnalysisEvidence, normalized string, analysis QueryAnalysis) []QueryAnalysisEvidence {
	switch analysis.TimeMode {
	case QueryTimeModeHistorical:
		if match := firstContained(normalized, "以前", "过去", "上次", "历史", "之前", "曾经", "从前", "later", "previous", "before"); match != "" {
			return appendRuleEvidence(out, "time_mode", "historical_time_marker", match, 0.86)
		}
		if hasStateTransitionIntent(normalized) {
			return appendRuleEvidence(out, "time_mode", "state_transition_time", firstContained(normalized, "一开始", "之前", "后来", "最近", "现在", "开始", "不再", "变成", "变为", "变得"), 0.90)
		}
	case QueryTimeModeBitemporalCheck:
		return appendRuleEvidence(out, "time_mode", "bitemporal_quantifier", firstContained(normalized, "一直", "从来", "所有", "每个", "永远", "always", "never"), 0.84)
	case QueryTimeModeCurrent:
		if match := firstContained(normalized, "最近状态", "当前状态", "最新状态", "最近", "现在", "当前", "latest", "current"); match != "" {
			return appendRuleEvidence(out, "time_mode", "current_or_recent_marker", match, 0.72)
		}
	}
	return appendRuleEvidence(out, "time_mode", "default_current_time", "", 0.46)
}

func appendRuleAbilityEvidence(out []QueryAnalysisEvidence, normalized string, analysis QueryAnalysis) []QueryAnalysisEvidence {
	switch analysis.MemoryAbility {
	case MemoryAbilityCausalExplain:
		return appendRuleEvidence(out, "memory_ability", "causal_intent", firstContained(normalized, "为什么", "原因", "导致", "怎么会", "为何", "why", "cause"), 0.90)
	case MemoryAbilityHistorical:
		return appendRuleEvidence(out, "memory_ability", "historical_or_transition_intent", firstContained(normalized, "以前", "之前", "后来", "过去", "曾经", "变化", "变成", "变为"), 0.88)
	case MemoryAbilityProvenance:
		return appendRuleEvidence(out, "memory_ability", "provenance_source_intent", firstContained(normalized, "什么时候说过", "从哪里知道", "哪里知道的", "来源", "证据", "source", "evidence"), 0.90)
	case MemoryAbilityPremiseCheck:
		return appendRuleEvidence(out, "memory_ability", "premise_check_intent", firstContained(normalized, "是不是", "是否", "有没有反例", "反例", "例外", "always", "never"), 0.90)
	case MemoryAbilityRelationshipArc:
		return appendRuleEvidence(out, "memory_ability", "relationship_arc_intent", firstContained(normalized, "关系变化", "关系轨迹", "关系时间线", "relationship arc", "relationship timeline"), 0.90)
	case MemoryAbilityBoundary:
		return appendRuleEvidence(out, "memory_ability", "boundary_or_forget_intent", firstContained(normalized, "忘掉", "删除", "删掉", "清除", "不要再提", "边界", "forget", "delete", "remove"), 0.86)
	case MemoryAbilityWorkflow:
		return appendRuleEvidence(out, "memory_ability", "workflow_intent", firstContained(normalized, "流程", "步骤", "怎么做", "操作步骤", "workflow", "procedure"), 0.82)
	case MemoryAbilityGotcha:
		return appendRuleEvidence(out, "memory_ability", "gotcha_intent", firstContained(normalized, "坑", "踩坑", "失败", "报错", "错误", "gotcha", "pitfall", "error"), 0.82)
	case MemoryAbilityDynamicState:
		return appendRuleEvidence(out, "memory_ability", "dynamic_state_intent", firstContained(normalized, "最近状态", "当前状态", "最新状态", "进度", "进展", "变化", "changed"), 0.82)
	case MemoryAbilityStaticState:
		return appendRuleEvidence(out, "memory_ability", "static_state_intent", firstContained(normalized, "身份", "偏好", "默认配置", "住址", "profile", "preference", "address"), 0.82)
	case MemoryAbilityDirectFact:
		if len(analysis.Signals) == 1 && analysis.Signals[0] == QuerySignalExactFact {
			return appendRuleEvidence(out, "memory_ability", "direct_fact_fallback", "", 0.42)
		}
		return appendRuleEvidence(out, "memory_ability", "direct_fact_signal", firstContained(normalized, "上次", "那天", "最近一次", "喜欢", "住在"), 0.62)
	default:
		return appendRuleEvidence(out, "memory_ability", "unknown_ability_fallback", "", 0.35)
	}
}

func appendRuleDomainEvidence(out []QueryAnalysisEvidence, normalized string, analysis QueryAnalysis) []QueryAnalysisEvidence {
	switch analysis.MemoryDomain {
	case MemoryDomainUserProfile:
		return appendRuleEvidence(out, "memory_domain", "profile_domain_keyword", firstContained(normalized, "我是谁", "身份", "名字", "昵称", "偏好", "喜欢", "讨厌", "住在", "profile", "preference", "identity"), 0.84)
	case MemoryDomainWorkExperience:
		return appendRuleEvidence(out, "memory_domain", "work_domain_keyword", firstContained(normalized, "部署", "上线", "ci", "测试", "命令", "repo", "仓库", "构建", "编译", "工作流", "workflow", "任务", "pr", "commit", "branch"), 0.82)
	case MemoryDomainEnvironmentExperience:
		return appendRuleEvidence(out, "memory_domain", "environment_domain_keyword", firstContained(normalized, "环境", "路径", "依赖", "python", "uv", "windows", "powershell", "权限", "toolchain", "runtime", "缓存", "cache"), 0.84)
	case MemoryDomainRelationship:
		if match := firstContained(normalized, "关系", "朋友", "同事", "家人", "小李", "relationship", "friend"); match != "" {
			return appendRuleEvidence(out, "memory_domain", "relationship_domain_keyword", match, 0.78)
		}
		return appendRuleEvidence(out, "memory_domain", "default_relationship_domain", "", 0.44)
	default:
		return appendRuleEvidence(out, "memory_domain", "domain_fallback", "", 0.35)
	}
}

func appendRuleEvidenceNeedEvidence(out []QueryAnalysisEvidence, normalized string, analysis QueryAnalysis) []QueryAnalysisEvidence {
	switch analysis.EvidenceNeed {
	case EvidenceNeedStateTransition:
		return appendRuleEvidence(out, "evidence_need", "state_transition_evidence_need", firstContained(normalized, "最近状态", "当前状态", "后来", "变化", "变成", "变为", "changed"), 0.86)
	case EvidenceNeedProcedureNote:
		return appendRuleEvidence(out, "evidence_need", "procedure_evidence_need", firstContained(normalized, "流程", "步骤", "怎么做", "workflow", "procedure"), 0.82)
	case EvidenceNeedGotchaNote:
		return appendRuleEvidence(out, "evidence_need", "gotcha_evidence_need", firstContained(normalized, "坑", "踩坑", "失败", "报错", "错误", "gotcha", "error"), 0.82)
	case EvidenceNeedPremiseCounterexample:
		return appendRuleEvidence(out, "evidence_need", "counterexample_evidence_need", firstContained(normalized, "反例", "例外", "是不是", "是否", "always", "never"), 0.86)
	case EvidenceNeedProvenanceSource:
		return appendRuleEvidence(out, "evidence_need", "provenance_evidence_need", firstContained(normalized, "什么时候说过", "从哪里知道", "来源", "证据", "source", "evidence"), 0.86)
	case EvidenceNeedRelationshipTimeline:
		return appendRuleEvidence(out, "evidence_need", "relationship_timeline_evidence_need", firstContained(normalized, "关系变化", "关系轨迹", "关系时间线", "relationship timeline"), 0.86)
	case EvidenceNeedExactObservation:
		return appendRuleEvidence(out, "evidence_need", "exact_observation_default", firstContained(normalized, "上次", "那天", "喜欢", "住在哪里", "哪个城市"), 0.48)
	default:
		return appendRuleEvidence(out, "evidence_need", "evidence_need_fallback", "", 0.35)
	}
}

func appendRuleEntityEvidence(out []QueryAnalysisEvidence, analysis QueryAnalysis) []QueryAnalysisEvidence {
	if len(analysis.EntityMentions) == 0 {
		return appendRuleEvidence(out, "entity_resolution", "no_entity_mention", "", 0.20)
	}
	for _, mention := range analysis.EntityMentions {
		weight := 0.68
		if mention.MatchKind == QueryEntityMentionKindCanonical {
			weight = 0.88
		} else if mention.MatchKind == QueryEntityMentionKindAlias {
			weight = 0.78
		}
		out = appendRuleEvidence(out, "entity_resolution", string(mention.MatchKind), mention.MatchText, weight)
	}
	return out
}

func appendRuleEvidence(out []QueryAnalysisEvidence, field string, signal string, matchText string, weight float64) []QueryAnalysisEvidence {
	return append(out, QueryAnalysisEvidence{
		Field:     field,
		Signal:    signal,
		MatchText: matchText,
		SpanStart: -1,
		SpanEnd:   -1,
		Weight:    clamp01(weight),
		Detector:  ruleFeatureScorerVersion,
	})
}

func ruleQueryAnalysisDecision(analysis QueryAnalysis, scores QueryAnalysisScores) QueryAnalysisDecision {
	reasonCodes := ruleDecisionReasonCodes(analysis, scores)
	return QueryAnalysisDecision{
		UseSemantic:      false,
		SemanticMode:     "none",
		RetrievalMode:    ruleRetrievalMode(analysis),
		ReasonCodes:      reasonCodes,
		ThresholdVersion: "rule_path_explanation.v1",
		ScorerVersion:    ruleFeatureScorerVersion,
	}
}

func ruleRetrievalMode(analysis QueryAnalysis) string {
	switch analysis.MemoryAbility {
	case MemoryAbilityBoundary:
		return "target_resolver"
	case MemoryAbilityCausalExplain, MemoryAbilityRelationshipArc:
		return "graph_contextual"
	case MemoryAbilityHistorical, MemoryAbilityDynamicState:
		return "historical"
	case MemoryAbilityProvenance:
		return "provenance"
	case MemoryAbilityPremiseCheck:
		return "premise_check"
	case MemoryAbilityDirectFact:
		if len(analysis.EntityMentions) > 0 || analysis.Probes.PredicateProbeConf >= 0.60 {
			return "exact"
		}
		return "hybrid"
	default:
		return "hybrid"
	}
}

func ruleDecisionReasonCodes(analysis QueryAnalysis, scores QueryAnalysisScores) []string {
	reasons := make([]string, 0, 6)
	switch analysis.MemoryAbility {
	case MemoryAbilityCausalExplain:
		reasons = append(reasons, "causal_intent")
	case MemoryAbilityHistorical:
		reasons = append(reasons, "historical_intent")
	case MemoryAbilityProvenance:
		reasons = append(reasons, "provenance_intent")
	case MemoryAbilityPremiseCheck:
		reasons = append(reasons, "premise_check_intent")
	case MemoryAbilityRelationshipArc:
		reasons = append(reasons, "relationship_arc_intent")
	case MemoryAbilityBoundary:
		reasons = append(reasons, "boundary_or_forget_intent")
	case MemoryAbilityDirectFact:
		reasons = append(reasons, "direct_fact_intent")
	}
	if len(analysis.EntityMentions) > 0 {
		reasons = append(reasons, "entity_resolved")
	}
	if scores.AnchorReadiness < 0.45 {
		reasons = append(reasons, "weak_anchor")
	} else {
		reasons = append(reasons, "anchor_ready")
	}
	if scores.DefaultFallbackPenalty >= 0.60 {
		reasons = append(reasons, "default_fallback")
	}
	if scores.Ambiguity >= 0.50 {
		reasons = append(reasons, "ambiguous_reference")
	}
	if scores.SafetyRisk >= 0.50 {
		reasons = append(reasons, "safety_risk")
	}
	return uniqueOrderedStrings(reasons)
}

func ruleQueryAnalysisAlternatives(normalized string, analysis QueryAnalysis) []QueryAnalysisAlternative {
	var out []QueryAnalysisAlternative
	if analysis.MemoryAbility == MemoryAbilityDirectFact && containsAny(normalized, "这件事", "这个", "那个", "这次", "那次", "后来", "之前") {
		out = append(out, QueryAnalysisAlternative{
			Field:       "memory_ability",
			Value:       string(MemoryAbilityHistorical),
			Confidence:  0.55,
			ReasonCodes: []string{"ambiguous_reference", "temporal_marker"},
			Detector:    ruleFeatureScorerVersion,
		})
		out = append(out, QueryAnalysisAlternative{
			Field:       "time_mode",
			Value:       string(QueryTimeModeHistorical),
			Confidence:  0.50,
			ReasonCodes: []string{"ambiguous_reference", "temporal_marker"},
			Detector:    ruleFeatureScorerVersion,
		})
	}
	if analysis.MemoryDomain == MemoryDomainRelationship && containsAny(normalized, "上班", "工作") && !containsAny(normalized, "关系", "朋友", "家人") {
		out = append(out, QueryAnalysisAlternative{
			Field:       "memory_domain",
			Value:       string(MemoryDomainWorkExperience),
			Confidence:  0.50,
			ReasonCodes: []string{"weak_work_domain_signal"},
			Detector:    ruleFeatureScorerVersion,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstContained(value string, needles ...string) string {
	for _, needle := range needles {
		if strings.TrimSpace(needle) == "" {
			continue
		}
		lower := strings.ToLower(needle)
		if strings.Contains(value, lower) {
			return lower
		}
	}
	return ""
}

func querySignalsToStrings(values []QuerySignal) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

func (r *RetrievalRepository) probeQueryAnchors(ctx context.Context, personaID string, normalized string, analysis QueryAnalysis, policy RetrievalPolicy) QueryAnchorProbe {
	var probe QueryAnchorProbe
	probeEntityAnchors(&probe, analysis.EntityMentions)

	if err := r.probeSparseAnchors(ctx, personaID, analysis, policy, &probe); err != nil {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("sparse_probe", 0, 0, 0, 0, "", err))
	}
	if err := r.probePredicateAnchors(ctx, personaID, normalized, analysis, policy, &probe); err != nil {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("predicate_probe", 0, 0, 0, 0, "", err))
	}
	if err := r.probeRecentAnchors(ctx, personaID, analysis, policy, &probe); err != nil {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("recent_probe", 0, 0, 0, 0, "", err))
	}
	if err := r.probePinnedCoreAnchors(ctx, personaID, analysis, policy, &probe); err != nil {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("pinned_core_probe", 0, 0, 0, 0, "", err))
	}
	if err := r.probeNarrativeAnchors(ctx, personaID, analysis, policy, &probe); err != nil {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("narrative_probe", 0, 0, 0, 0, "", err))
	}
	return probe
}

func probeEntityAnchors(probe *QueryAnchorProbe, mentions []QueryEntityMention) {
	if len(mentions) == 0 {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("entity_exact", 0, 0, 0, 0, "no entity mention", nil))
		return
	}
	best := 0.0
	seen := map[string]struct{}{}
	hasAlias := false
	for _, mention := range mentions {
		if mention.EntityID != "" {
			seen[mention.EntityID] = struct{}{}
		}
		switch mention.MatchKind {
		case QueryEntityMentionKindCanonical:
			best = maxFloat(best, 0.95)
		case QueryEntityMentionKindAlias:
			best = maxFloat(best, 0.85)
			hasAlias = true
		default:
			best = maxFloat(best, 0.65)
		}
	}
	reason := "canonical entity match"
	if hasAlias {
		reason = "entity alias match"
	}
	if len(seen) > 1 {
		probe.EntityAmbiguity = 0.75
		best = minScore(best, 0.65)
		reason = "ambiguous entity match"
	}
	probe.EntityExactConf = clamp01(best)
	probe.Breakdown = append(probe.Breakdown, probeBreakdown("entity_exact", probe.EntityExactConf, len(mentions), probe.EntityExactConf, 0, reason, nil))
}

func (r *RetrievalRepository) probeSparseAnchors(ctx context.Context, personaID string, analysis QueryAnalysis, policy RetrievalPolicy, probe *QueryAnchorProbe) error {
	docs, err := r.search.SearchDocumentsForAnalyzedRetrieval(ctx, personaID, analysis, policy.UseFTS, 32, policy)
	if err != nil {
		return err
	}
	scores := make([]float64, 0, len(docs))
	for _, doc := range docs {
		allowed, err := r.searchDocumentProbeAuthorityAllows(ctx, personaID, doc, policy)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}
		score := textMatchScore(analysis, doc.SearchText)
		if score <= 0 {
			score = 0.20
		}
		scores = append(scores, clamp01(score))
		if len(scores) >= 8 {
			break
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(scores)))
	top1, top2 := topProbeScores(scores)
	probe.FallbackSearchHitCount = len(scores)
	probe.Top1Score = top1
	probe.Top2Score = top2
	probe.Top1Margin = clamp01(top1 - top2)
	switch {
	case len(scores) >= 3 && top1 >= 0.60:
		probe.SparseProbeConf = 0.75
	case len(scores) > 0:
		probe.SparseProbeConf = 0.40
	default:
		probe.SparseProbeConf = 0
	}
	reason := "no sparse search hit"
	if probe.SparseProbeConf >= 0.75 {
		reason = "strong sqlite search document match"
	} else if probe.SparseProbeConf > 0 {
		reason = "weak sqlite search document match"
	}
	probe.Breakdown = append(probe.Breakdown, probeBreakdown("sparse_probe", probe.SparseProbeConf, len(scores), top1, top2, reason, nil))
	return nil
}

func (r *RetrievalRepository) searchDocumentProbeAuthorityAllows(ctx context.Context, personaID string, doc core.SearchDocument, policy RetrievalPolicy) (bool, error) {
	if doc.NodeType != core.NodeTypeFact {
		return searchDocumentAuthorityAllows(doc, policy), nil
	}
	fact, err := r.getFact(ctx, personaID, doc.NodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return r.authorityAllows(ctx, fact, policy)
}

func (r *RetrievalRepository) probePredicateAnchors(ctx context.Context, personaID string, normalized string, analysis QueryAnalysis, policy RetrievalPolicy, probe *QueryAnchorProbe) error {
	predicates := predicateProbePredicates(normalized)
	if len(predicates) > 0 {
		count, top, err := r.countFactsForPredicates(ctx, personaID, predicates, policy)
		if err != nil {
			return err
		}
		if count > 0 {
			probe.PredicateProbeConf = clamp01(0.70 + 0.20*top)
			probe.Breakdown = append(probe.Breakdown, probeBreakdown("predicate_probe", probe.PredicateProbeConf, count, top, 0, "explicit predicate match", nil))
			return nil
		}
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("predicate_probe", 0, 0, 0, 0, "explicit predicate not found", nil))
		return nil
	}
	count, top, err := r.countFactsForGenericTerms(ctx, personaID, analysis, policy)
	if err != nil {
		return err
	}
	if count > 0 {
		probe.PredicateProbeConf = clamp01(0.20 + minScore(0.15, 0.05*float64(count)))
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("predicate_probe", probe.PredicateProbeConf, count, top, 0, "generic predicate/object weak match", nil))
		return nil
	}
	probe.Breakdown = append(probe.Breakdown, probeBreakdown("predicate_probe", 0, 0, 0, 0, "no predicate/object match", nil))
	return nil
}

func (r *RetrievalRepository) probeRecentAnchors(ctx context.Context, personaID string, analysis QueryAnalysis, policy RetrievalPolicy, probe *QueryAnchorProbe) error {
	if !queryAllowsRecentImportantAnchors(analysis) {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("recent_probe", 0, 0, 0, 0, "query mode does not use recent anchors", nil))
		return nil
	}
	count, top, err := r.countFactsByQuery(ctx, personaID, policy, `
SELECT id, importance
FROM facts
WHERE persona_id = ?
  AND importance >= 0.7
  AND visibility_status = 'visible'
  AND searchable = 1
  AND lifecycle_status IN ('active', 'dormant', 'consolidated')
  AND (validity_status != 'invalidated' OR ? = 1)
  AND (lifecycle_status != 'archived' OR ? = 1)
  AND (lifecycle_status != 'deep_archived' OR ? = 1)
ORDER BY importance DESC, updated_at DESC, id ASC
LIMIT 32`)
	if err != nil {
		return err
	}
	if count > 0 {
		probe.RecentProbeConf = clamp01(0.25 + 0.25*top)
	}
	probe.Breakdown = append(probe.Breakdown, probeBreakdown("recent_probe", probe.RecentProbeConf, count, top, 0, "recent important fact probe", nil))
	return nil
}

func (r *RetrievalRepository) probePinnedCoreAnchors(ctx context.Context, personaID string, analysis QueryAnalysis, policy RetrievalPolicy, probe *QueryAnchorProbe) error {
	if !queryAllowsPinnedCoreAnchors(analysis) {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("pinned_core_probe", 0, 0, 0, 0, "query mode does not use pinned/core anchors", nil))
		return nil
	}
	count, top, err := r.countFactsByQuery(ctx, personaID, policy, `
SELECT id, importance
FROM facts
WHERE persona_id = ?
  AND pinned = 1
  AND fact_type IN ('core_identity', 'commitment', 'stable_preference', 'relational_state')
  AND visibility_status = 'visible'
  AND searchable = 1
  AND (validity_status != 'invalidated' OR ? = 1)
  AND (lifecycle_status != 'archived' OR ? = 1)
  AND (lifecycle_status != 'deep_archived' OR ? = 1)
ORDER BY importance DESC, updated_at DESC, id ASC
LIMIT 32`)
	if err != nil {
		return err
	}
	if count > 0 {
		probe.PinnedCoreProbeConf = clamp01(0.35 + 0.25*top)
	}
	probe.Breakdown = append(probe.Breakdown, probeBreakdown("pinned_core_probe", probe.PinnedCoreProbeConf, count, top, 0, "pinned/core fact probe", nil))
	return nil
}

func (r *RetrievalRepository) probeNarrativeAnchors(ctx context.Context, personaID string, analysis QueryAnalysis, policy RetrievalPolicy, probe *QueryAnchorProbe) error {
	if !queryAllowsNarrativeInsightAnchors(analysis) {
		probe.Breakdown = append(probe.Breakdown, probeBreakdown("narrative_probe", 0, 0, 0, 0, "query mode does not use narrative anchors", nil))
		return nil
	}
	docs, err := r.listNarrativeInsightSearchDocuments(ctx, personaID, 8)
	if err != nil {
		return err
	}
	var scores []float64
	for _, doc := range docs {
		if !searchDocumentAuthorityAllows(doc, policy) {
			continue
		}
		score := textMatchScore(analysis, doc.SearchText)
		if score <= 0 {
			continue
		}
		scores = append(scores, score)
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(scores)))
	top1, top2 := topProbeScores(scores)
	if len(scores) > 0 {
		probe.NarrativeProbeConf = clamp01(0.20 + 0.25*top1)
	}
	probe.Breakdown = append(probe.Breakdown, probeBreakdown("narrative_probe", probe.NarrativeProbeConf, len(scores), top1, top2, "narrative/insight weak hit probe", nil))
	return nil
}

func (r *RetrievalRepository) countFactsForPredicates(ctx context.Context, personaID string, predicates []string, policy RetrievalPolicy) (int, float64, error) {
	predicates = uniqueOrderedStrings(predicates)
	if len(predicates) == 0 {
		return 0, 0, nil
	}
	placeholders := make([]string, len(predicates))
	args := make([]any, 0, 1+len(predicates)+3+1)
	args = append(args, personaID)
	for i, predicate := range predicates {
		placeholders[i] = "?"
		args = append(args, predicate)
	}
	args = append(args,
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowDeepArchive),
	)
	args = append(args, 32)
	query := `
SELECT id, importance
FROM facts
WHERE persona_id = ?
  AND predicate IN (` + strings.Join(placeholders, ", ") + `)
  AND visibility_status = 'visible'
  AND searchable = 1
  AND (validity_status != 'invalidated' OR ? = 1)
  AND (lifecycle_status != 'archived' OR ? = 1)
  AND (lifecycle_status != 'deep_archived' OR ? = 1)
ORDER BY importance DESC, updated_at DESC, id ASC
LIMIT ?`
	return r.countEligibleFactsByQuery(ctx, personaID, policy, query, args...)
}

func (r *RetrievalRepository) countFactsForGenericTerms(ctx context.Context, personaID string, analysis QueryAnalysis, policy RetrievalPolicy) (int, float64, error) {
	terms := discriminatingSlotTerms(analysis)
	if len(terms) == 0 {
		return 0, 0, nil
	}
	if len(terms) > 4 {
		terms = terms[:4]
	}
	clauses := make([]string, 0, len(terms))
	args := []any{personaID}
	for _, term := range terms {
		like := "%" + term + "%"
		clauses = append(clauses, "(predicate LIKE ? OR COALESCE(object_literal, '') LIKE ? OR content_summary LIKE ?)")
		args = append(args, like, like, like)
	}
	args = append(args,
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowDeepArchive),
	)
	args = append(args, 32)
	query := `
SELECT id, importance
FROM facts
WHERE persona_id = ?
  AND (` + strings.Join(clauses, " OR ") + `)
  AND visibility_status = 'visible'
  AND searchable = 1
  AND (validity_status != 'invalidated' OR ? = 1)
  AND (lifecycle_status != 'archived' OR ? = 1)
  AND (lifecycle_status != 'deep_archived' OR ? = 1)
ORDER BY importance DESC, updated_at DESC, id ASC
LIMIT ?`
	return r.countEligibleFactsByQuery(ctx, personaID, policy, query, args...)
}

func (r *RetrievalRepository) countFactsByQuery(ctx context.Context, personaID string, policy RetrievalPolicy, query string) (int, float64, error) {
	return r.countEligibleFactsByQuery(ctx, personaID, policy, query,
		personaID,
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowDeepArchive),
	)
}

func (r *RetrievalRepository) countEligibleFactsByQuery(ctx context.Context, personaID string, policy RetrievalPolicy, query string, args ...any) (int, float64, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, 0, err
	}
	var candidates []factProbeCandidate
	for rows.Next() {
		var candidate factProbeCandidate
		if err := rows.Scan(&candidate.ID, &candidate.Importance); err != nil {
			_ = rows.Close()
			return 0, 0, err
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}

	var count int
	var top float64
	for _, candidate := range candidates {
		fact, err := r.getFact(ctx, personaID, candidate.ID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, 0, err
		}
		allowed, err := r.authorityAllows(ctx, fact, policy)
		if err != nil {
			return 0, 0, err
		}
		if !allowed {
			continue
		}
		count++
		top = maxFloat(top, candidate.Importance)
	}
	return count, clamp01(top), nil
}

type factProbeCandidate struct {
	ID         string
	Importance float64
}

func predicateProbePredicates(normalized string) []string {
	switch {
	case containsAny(normalized, "不喜欢", "讨厌", "dislike", "dislikes", "hate"):
		return []string{"dislikes"}
	case containsAny(normalized, "喜欢", "偏好", "爱好", "like", "likes", "preference", "prefer"):
		return []string{"likes", "prefers"}
	case containsAny(normalized, "住在哪里", "住在", "哪个城市", "城市", "live", "lives", "city"):
		return []string{"lives_in"}
	case containsAny(normalized, "名字", "昵称", "叫我", "称呼", "name"):
		return []string{"prefers_name"}
	default:
		return nil
	}
}

func topProbeScores(scores []float64) (float64, float64) {
	if len(scores) == 0 {
		return 0, 0
	}
	top1 := clamp01(scores[0])
	if len(scores) == 1 {
		return top1, 0
	}
	return top1, clamp01(scores[1])
}

func probeBreakdown(source string, confidence float64, hitCount int, topScore float64, secondScore float64, reason string, err error) QueryAnchorProbeBreakdown {
	item := QueryAnchorProbeBreakdown{
		Source:      source,
		Status:      "ok",
		Confidence:  clamp01(confidence),
		HitCount:    hitCount,
		TopScore:    clamp01(topScore),
		SecondScore: clamp01(secondScore),
		Reason:      strings.TrimSpace(reason),
	}
	if err != nil {
		item.Status = "unknown"
		item.Error = sanitizeProbeError(err)
	}
	return item
}

func sanitizeProbeError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 160 {
		msg = msg[:160]
	}
	return msg
}

func onlyExactFactSignal(signals []QuerySignal) bool {
	return len(signals) == 1 && signals[0] == QuerySignalExactFact
}

func ComputeRuleFit(normalized string, analysis QueryAnalysis, ev []QueryAnalysisEvidence) QueryAnalysisScores {
	normalized = strings.TrimSpace(normalized)
	if len(ev) == 0 {
		ev = analysis.Evidence
	}
	if normalized == "" {
		return QueryAnalysisScores{}
	}

	var s QueryAnalysisScores
	s.IntentEvidence = scoreIntentEvidence(normalized, analysis, ev)
	s.FieldConsistency = scoreFieldConsistency(analysis)
	s.EntityResolution = scoreEntityResolution(analysis.EntityMentions)
	s.TimeEvidence = scoreTimeEvidence(normalized, analysis.TimeMode, ev)
	s.DomainEvidence = scoreDomainEvidence(normalized, analysis.MemoryDomain, ev)
	s.EvidenceNeedEvidence = scoreEvidenceNeed(normalized, analysis.EvidenceNeed, ev)
	s.Specificity = scoreLexicalSpecificity(normalized)
	s.Ambiguity = scoreAmbiguity(normalized, analysis, ev)
	s.Complexity = scoreComplexity(normalized, analysis)
	s.SafetyRisk = scoreSafetyRisk(normalized, analysis)
	s.DefaultFallbackPenalty = scoreDefaultFallbackPenalty(normalized, analysis)
	s.MultiIntentConflictPenalty = scoreMultiIntentConflictPenalty(analysis)
	s.SensitivityPenalty = s.SafetyRisk

	s.RuleFit = clamp01(
		0.20 +
			0.18*s.IntentEvidence +
			0.14*s.FieldConsistency +
			0.13*s.EntityResolution +
			0.10*s.TimeEvidence +
			0.09*s.DomainEvidence +
			0.08*s.EvidenceNeedEvidence +
			0.08*s.Specificity -
			0.15*s.DefaultFallbackPenalty -
			0.12*s.Ambiguity -
			0.08*s.MultiIntentConflictPenalty -
			0.06*s.SensitivityPenalty,
	)
	s.AnchorReadiness = ComputeAnchorReadiness(analysis.Probes)
	s.SemanticNeed = clamp01(
		0.35*(1-s.RuleFit) +
			0.25*(1-s.AnchorReadiness) +
			0.25*s.Complexity +
			0.15*s.Ambiguity,
	)
	if analysis.MemoryAbility == MemoryAbilityCausalExplain && s.AnchorReadiness < 0.50 {
		s.SemanticNeed = maxFloat(s.SemanticNeed, 0.60)
	}
	s.ExpectedRetrievalConfidence = clamp01(0.60*s.RuleFit + 0.40*s.AnchorReadiness - 0.10*s.SafetyRisk)
	return s
}

func ComputeAnchorReadiness(probe QueryAnchorProbe) float64 {
	readiness := noisyOr(
		probe.EntityExactConf,
		probe.SparseProbeConf,
		probe.PredicateProbeConf,
		probe.RecentProbeConf,
		probe.PinnedCoreProbeConf,
		probe.NarrativeProbeConf,
	)
	if probe.EntityAmbiguity > 0.6 {
		readiness *= 0.75
	}
	if probe.Top1Margin < 0.08 && probe.FallbackSearchHitCount > 5 {
		readiness *= 0.85
	}
	return clamp01(readiness)
}

func noisyOr(values ...float64) float64 {
	miss := 1.0
	for _, value := range values {
		miss *= 1.0 - clamp01(value)
	}
	return clamp01(1.0 - miss)
}

func ComputeFieldConfidence(_ QueryAnalysis, s QueryAnalysisScores) QueryAnalysisConfidence {
	return QueryAnalysisConfidence{
		Overall:          clamp01(s.ExpectedRetrievalConfidence),
		TimeMode:         clamp01(s.TimeEvidence),
		MemoryAbility:    clamp01(s.IntentEvidence),
		MemoryDomain:     clamp01(s.DomainEvidence),
		EvidenceNeed:     clamp01(s.EvidenceNeedEvidence),
		EntityResolution: clamp01(s.EntityResolution),
	}
}

func scoreIntentEvidence(normalized string, analysis QueryAnalysis, ev []QueryAnalysisEvidence) float64 {
	if strings.TrimSpace(normalized) == "" {
		return 0
	}
	score := evidenceScore(ev, "memory_ability")
	switch analysis.MemoryAbility {
	case MemoryAbilityDirectFact:
		if onlyExactFactSignal(analysis.Signals) {
			score = maxFloat(score, 0.42)
		} else if len(analysis.Signals) > 0 {
			score = maxFloat(score, 0.62)
		} else {
			score = maxFloat(score, 0.45)
		}
	case MemoryAbilityCausalExplain:
		score = maxFloat(score, 0.78)
		if hasQuerySignal(analysis, QuerySignalCausal) {
			score += 0.12
		}
		if hasQuerySignal(analysis, QuerySignalCausalChain) {
			score += 0.04
		}
	case MemoryAbilityHistorical:
		score = maxFloat(score, 0.76)
		if hasQuerySignal(analysis, QuerySignalHistorical) || hasQuerySignal(analysis, QuerySignalStateTransition) {
			score += 0.12
		}
	case MemoryAbilityProvenance:
		score = maxFloat(score, 0.78)
		if hasQuerySignal(analysis, QuerySignalProvenance) || hasQuerySignal(analysis, QuerySignalProvenanceSource) {
			score += 0.12
		}
	case MemoryAbilityPremiseCheck:
		score = maxFloat(score, 0.78)
		if hasQuerySignal(analysis, QuerySignalPremiseCheck) || hasQuerySignal(analysis, QuerySignalPremiseCounterexample) {
			score += 0.12
		}
	case MemoryAbilityRelationshipArc:
		score = maxFloat(score, 0.78)
		if hasQuerySignal(analysis, QuerySignalRelationshipArc) {
			score += 0.12
		}
	case MemoryAbilityBoundary:
		score = maxFloat(score, 0.76)
		if hasQuerySignal(analysis, QuerySignalForgetDelete) {
			score += 0.10
		}
	case MemoryAbilityWorkflow, MemoryAbilityGotcha, MemoryAbilityDynamicState, MemoryAbilityStaticState, MemoryAbilityPlanning, MemoryAbilitySupportive:
		score = maxFloat(score, 0.70)
	default:
		score = maxFloat(score, 0.35)
	}
	return clamp01(score)
}

func scoreFieldConsistency(analysis QueryAnalysis) float64 {
	if analysis.TimeMode == "" && analysis.MemoryAbility == "" && analysis.EvidenceNeed == "" {
		return 0
	}
	score := 0.62
	switch analysis.MemoryAbility {
	case MemoryAbilityDirectFact:
		if analysis.EvidenceNeed == EvidenceNeedExactObservation {
			score += 0.08
		}
		if analysis.EvidenceNeed != "" && analysis.EvidenceNeed != EvidenceNeedExactObservation {
			score -= 0.25
		}
		if hasQuerySignal(analysis, QuerySignalStateTransition) || hasQuerySignal(analysis, QuerySignalCausal) ||
			hasQuerySignal(analysis, QuerySignalPremiseCounterexample) || hasQuerySignal(analysis, QuerySignalProvenanceSource) {
			score -= 0.15
		}
	case MemoryAbilityHistorical:
		if analysis.TimeMode == QueryTimeModeHistorical {
			score += 0.08
		}
		if analysis.EvidenceNeed == EvidenceNeedStateTransition || hasQuerySignal(analysis, QuerySignalStateTransition) {
			score += 0.16
		}
	case MemoryAbilityCausalExplain:
		if hasQuerySignal(analysis, QuerySignalCausal) {
			score += 0.16
		}
		if analysis.EvidenceNeed == EvidenceNeedStateTransition || hasQuerySignal(analysis, QuerySignalCausalChain) {
			score += 0.06
		}
	case MemoryAbilityProvenance:
		if analysis.EvidenceNeed == EvidenceNeedProvenanceSource || hasQuerySignal(analysis, QuerySignalProvenanceSource) {
			score += 0.18
		}
	case MemoryAbilityPremiseCheck:
		if analysis.EvidenceNeed == EvidenceNeedPremiseCounterexample || hasQuerySignal(analysis, QuerySignalPremiseCounterexample) {
			score += 0.18
		}
	case MemoryAbilityRelationshipArc:
		if analysis.EvidenceNeed == EvidenceNeedRelationshipTimeline || hasQuerySignal(analysis, QuerySignalRelationshipArc) {
			score += 0.18
		}
	case MemoryAbilityWorkflow:
		if analysis.EvidenceNeed == EvidenceNeedProcedureNote {
			score += 0.14
		}
	case MemoryAbilityGotcha:
		if analysis.EvidenceNeed == EvidenceNeedGotchaNote {
			score += 0.14
		}
	}
	if analysis.TimeMode == QueryTimeModeCurrent && hasQuerySignal(analysis, QuerySignalHistorical) {
		score -= 0.14
	}
	if analysis.MemoryAbility != MemoryAbilityDirectFact && onlyExactFactSignal(analysis.Signals) {
		score -= 0.20
	}
	return clamp01(score)
}

func scoreEntityResolution(mentions []QueryEntityMention) float64 {
	if len(mentions) == 0 {
		return 0.20
	}
	best := 0.45
	seen := make(map[string]struct{}, len(mentions))
	for _, mention := range mentions {
		switch mention.MatchKind {
		case QueryEntityMentionKindCanonical:
			best = maxFloat(best, 0.88)
		case QueryEntityMentionKindAlias:
			best = maxFloat(best, 0.78)
		default:
			best = maxFloat(best, 0.68)
		}
		if mention.EntityID != "" {
			seen[mention.EntityID] = struct{}{}
		}
	}
	if len(seen) > 1 {
		best -= 0.12
	}
	return clamp01(best)
}

func scoreTimeEvidence(normalized string, mode QueryTimeMode, ev []QueryAnalysisEvidence) float64 {
	if strings.TrimSpace(normalized) == "" {
		return 0
	}
	score := evidenceScore(ev, "time_mode")
	switch mode {
	case QueryTimeModeHistorical:
		score = maxFloat(score, 0.66)
		if hasOldStateMarker(normalized) || hasPastEventDirectFactIntent(normalized) {
			score = maxFloat(score, 0.86)
		}
		if hasStateTransitionIntent(normalized) {
			score = maxFloat(score, 0.90)
		}
	case QueryTimeModeBitemporalCheck:
		score = maxFloat(score, 0.84)
	case QueryTimeModeCurrent:
		score = maxFloat(score, 0.46)
		if containsAny(normalized, "最近", "现在", "当前", "最新", "current", "latest") {
			score = maxFloat(score, 0.72)
		}
	default:
		score = maxFloat(score, 0.35)
	}
	return clamp01(score)
}

func scoreDomainEvidence(normalized string, domain MemoryDomain, ev []QueryAnalysisEvidence) float64 {
	if strings.TrimSpace(normalized) == "" {
		return 0
	}
	score := evidenceScore(ev, "memory_domain")
	switch domain {
	case MemoryDomainUserProfile:
		score = maxFloat(score, 0.58)
		if containsAny(normalized, "我是谁", "身份", "名字", "昵称", "偏好", "喜欢", "讨厌", "住在", "profile", "preference", "identity") {
			score = maxFloat(score, 0.84)
		}
	case MemoryDomainWorkExperience:
		score = maxFloat(score, 0.58)
		if containsAny(normalized, "部署", "上线", "ci", "测试", "命令", "repo", "仓库", "构建", "编译", "工作流", "workflow", "任务", "pr", "commit", "branch", "上班", "工作") {
			score = maxFloat(score, 0.82)
		}
	case MemoryDomainEnvironmentExperience:
		score = maxFloat(score, 0.58)
		if containsAny(normalized, "环境", "路径", "依赖", "python", "uv", "windows", "powershell", "权限", "toolchain", "runtime", "缓存", "cache") {
			score = maxFloat(score, 0.84)
		}
	case MemoryDomainRelationship:
		score = maxFloat(score, 0.44)
		if containsAny(normalized, "关系", "朋友", "同事", "家人", "小李", "relationship", "friend") {
			score = maxFloat(score, 0.78)
		}
	default:
		score = maxFloat(score, 0.35)
	}
	return clamp01(score)
}

func scoreEvidenceNeed(normalized string, need EvidenceNeed, ev []QueryAnalysisEvidence) float64 {
	if strings.TrimSpace(normalized) == "" {
		return 0
	}
	score := evidenceScore(ev, "evidence_need")
	switch need {
	case EvidenceNeedStateTransition:
		score = maxFloat(score, 0.68)
		if hasStateTransitionIntent(normalized) || containsAny(normalized, "最近", "后来", "变化", "变成", "changed") {
			score = maxFloat(score, 0.86)
		}
	case EvidenceNeedProcedureNote:
		score = maxFloat(score, 0.82)
	case EvidenceNeedGotchaNote:
		score = maxFloat(score, 0.82)
	case EvidenceNeedPremiseCounterexample:
		score = maxFloat(score, 0.86)
	case EvidenceNeedProvenanceSource:
		score = maxFloat(score, 0.86)
	case EvidenceNeedRelationshipTimeline:
		score = maxFloat(score, 0.86)
	case EvidenceNeedExactObservation:
		score = maxFloat(score, 0.48)
		if hasPastEventDirectFactIntent(normalized) || containsAny(normalized, "住在哪里", "喜欢什么", "哪个城市") {
			score = maxFloat(score, 0.62)
		}
	default:
		score = maxFloat(score, 0.35)
	}
	return clamp01(score)
}

func scoreLexicalSpecificity(normalized string) float64 {
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return 0
	}
	runes := nonSpaceRuneCount(normalized)
	score := 0.25
	if runes >= 4 {
		score += 0.12
	}
	if runes >= 8 {
		score += 0.13
	}
	if runes >= 16 {
		score += 0.15
	}
	if len(strings.Fields(normalized)) >= 2 {
		score += 0.08
	}
	if containsAny(normalized, "什么时候", "哪里", "哪次", "谁", "哪个城市", "住在哪里", "source", "evidence") {
		score += 0.10
	}
	if containsLatinOrDigit(normalized) {
		score += 0.08
	}
	if containsAny(normalized, "什么都", "任何", "所有", "每个") {
		score -= 0.08
	}
	if runes <= 2 {
		score = minScore(score, 0.35)
	}
	return clamp01(score)
}

func scoreAmbiguity(normalized string, analysis QueryAnalysis, ev []QueryAnalysisEvidence) float64 {
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return 1
	}
	score := 0.08
	if containsAny(normalized, "这件事", "这个", "那个", "这次", "那次", "它", "this thing", "that thing") {
		score += 0.28
	}
	if containsAny(normalized, "什么", "哪些", "怎么样", "怎么回事", "anything", "whatever") {
		score += 0.14
	}
	if len(analysis.EntityMentions) == 0 &&
		(analysis.MemoryAbility != MemoryAbilityDirectFact || containsAny(normalized, "这件事", "这个", "那个")) {
		score += 0.18
	}
	if len(analysis.EntityMentions) > 1 {
		score += 0.12
	}
	if scoreMultiIntentConflictPenalty(analysis) > 0 {
		score += 0.14
	}
	if len(ev) > 1 {
		score += 0.05
	}
	if len(analysis.EntityMentions) == 1 {
		score -= 0.12
	}
	return clamp01(score)
}

func scoreComplexity(normalized string, analysis QueryAnalysis) float64 {
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return 0
	}
	score := 0.15
	switch analysis.MemoryAbility {
	case MemoryAbilityCausalExplain, MemoryAbilityHistorical, MemoryAbilityProvenance, MemoryAbilityPremiseCheck, MemoryAbilityRelationshipArc:
		score += 0.30
	case MemoryAbilityWorkflow, MemoryAbilityGotcha, MemoryAbilityDynamicState:
		score += 0.18
	case MemoryAbilityBoundary:
		score += 0.12
	}
	semanticSignals := 0
	for _, signal := range analysis.Signals {
		switch signal {
		case QuerySignalCausal, QuerySignalHistorical, QuerySignalStateTransition, QuerySignalProvenanceSource, QuerySignalPremiseCounterexample, QuerySignalRelationshipArc, QuerySignalCausalChain:
			semanticSignals++
		}
	}
	score += minScore(float64(semanticSignals)*0.04, 0.18)
	if nonSpaceRuneCount(normalized) >= 14 {
		score += 0.08
	}
	if containsAny(normalized, "为什么", "后来", "之前", "从什么时候", "是不是", "有没有反例", "causal", "why") {
		score += 0.08
	}
	return clamp01(score)
}

func scoreSafetyRisk(normalized string, analysis QueryAnalysis) float64 {
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return 0
	}
	score := 0.0
	if hasQuerySignal(analysis, QuerySignalForgetDelete) {
		score += 0.70
	}
	if hasQuerySignal(analysis, QuerySignalSensitivity) {
		score += 0.15
	}
	if containsAny(normalized, "忘掉", "删除", "删掉", "清除", "不要再提", "forget", "delete", "remove") {
		score += 0.10
	}
	if containsAny(normalized, "隐私", "敏感", "private", "sensitive") {
		score += 0.15
	}
	if hasQuerySignal(analysis, QuerySignalDebug) {
		score += 0.10
	}
	return clamp01(score)
}

func scoreDefaultFallbackPenalty(normalized string, analysis QueryAnalysis) float64 {
	if strings.TrimSpace(normalized) == "" {
		return 1
	}
	if analysis.MemoryAbility != MemoryAbilityDirectFact || len(analysis.EntityMentions) > 0 {
		return 0
	}
	switch {
	case onlyExactFactSignal(analysis.Signals):
		return 0.65
	case len(analysis.Signals) == 0:
		return 0.75
	case len(analysis.Signals) == 1 && analysis.Signals[0] == QuerySignalPastEventDirectFact:
		return 0.20
	default:
		return 0.30
	}
}

func scoreMultiIntentConflictPenalty(analysis QueryAnalysis) float64 {
	intents := 0
	for _, signal := range analysis.Signals {
		switch signal {
		case QuerySignalCausal, QuerySignalProvenanceSource, QuerySignalPremiseCounterexample, QuerySignalRelationshipArc, QuerySignalForgetDelete, QuerySignalReflectionSummary, QuerySignalEventBundle:
			intents++
		}
	}
	if intents <= 1 {
		return 0
	}
	penalty := minScore(0.10*float64(intents-1), 0.35)
	if hasQuerySignal(analysis, QuerySignalForgetDelete) {
		penalty += 0.15
	}
	return clamp01(penalty)
}

func evidenceScore(ev []QueryAnalysisEvidence, field string) float64 {
	best := 0.0
	for _, item := range ev {
		if item.Field == field {
			best = maxFloat(best, item.Weight)
		}
	}
	return clamp01(best)
}

func nonSpaceRuneCount(value string) int {
	count := 0
	for _, r := range value {
		if !unicode.IsSpace(r) {
			count++
		}
	}
	return count
}

func containsLatinOrDigit(value string) bool {
	for _, r := range value {
		if ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') || ('0' <= r && r <= '9') {
			return true
		}
	}
	return false
}

func clamp01(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

func maxFloat(a float64, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func minScore(a float64, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func hasStaticStateIntent(normalized string) bool {
	return containsAny(
		normalized,
		"身份",
		"偏好",
		"默认配置",
		"住址",
		"常驻状态",
		"profile",
		"preference",
		"default",
		"address",
		"stable setting",
	)
}

func (r *RetrievalRepository) matchEntityMentions(ctx context.Context, personaID string, normalizedQuery string, policy RetrievalPolicy) ([]QueryEntityMention, error) {
	if normalizedQuery == "" {
		return nil, nil
	}
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT e.id, e.canonical_name, COALESCE(a.alias, '')
FROM entities e
LEFT JOIN entity_aliases a
  ON a.persona_id = e.persona_id
 AND a.entity_id = e.id
WHERE e.persona_id = ?
  AND e.visibility_status = 'visible'
  AND e.searchable = 1
  AND CASE e.sensitivity_level
      WHEN 'normal' THEN 0
      WHEN 'sensitive' THEN 1
      WHEN 'highly_sensitive' THEN 2
      ELSE 3
  END <= ?
ORDER BY e.id, a.alias`, personaID, allowedSensitivityRank)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mentionsByEntity := map[string]QueryEntityMention{}
	for rows.Next() {
		var id, canonicalName, alias string
		if err := rows.Scan(&id, &canonicalName, &alias); err != nil {
			return nil, err
		}
		canonicalMatch := matchedText(normalizedQuery, canonicalName)
		aliasMatch := matchedText(normalizedQuery, alias)
		if canonicalMatch == "" && aliasMatch == "" {
			continue
		}
		mention := QueryEntityMention{
			EntityID:      id,
			CanonicalName: canonicalName,
		}
		if aliasMatch != "" && len([]rune(alias)) >= len([]rune(canonicalName)) {
			mention.Alias = alias
			mention.MatchText = alias
			mention.MatchKind = QueryEntityMentionKindAlias
		} else if canonicalMatch != "" {
			mention.MatchText = canonicalName
			mention.MatchKind = QueryEntityMentionKindCanonical
		} else {
			mention.Alias = alias
			mention.MatchText = alias
			mention.MatchKind = QueryEntityMentionKindAlias
		}
		if existing, ok := mentionsByEntity[id]; ok && existing.MatchKind == QueryEntityMentionKindCanonical {
			continue
		}
		mentionsByEntity[id] = mention
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	mentions := make([]QueryEntityMention, 0, len(mentionsByEntity))
	for _, mention := range mentionsByEntity {
		mentions = append(mentions, mention)
	}
	sort.Slice(mentions, func(i, j int) bool {
		return mentions[i].EntityID < mentions[j].EntityID
	})
	return mentions, nil
}

func matchedText(normalizedQuery string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(normalizedQuery, strings.ToLower(value)) {
		return value
	}
	return ""
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
