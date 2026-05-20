package sqlite

import (
	"context"
	"sort"
	"strings"

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

type QueryPolicyHints struct {
	PreferEvidencedByLinks bool
	PreferSupersedesLinks  bool
	PreferCausalLinks      bool
	PreferCounterexamples  bool
	PreferNarratives       bool
	MaxHopsHint            int
}

type QueryAnalysisDiagnostics struct {
	SemanticStatus        string
	SemanticProvider      string
	SemanticModel         string
	PromptVersion         string
	SemanticLatencyMs     int64
	FallbackReason        string
	RewriteCount          int
	SemanticAnchorCount   int
	DroppedRewriteCount   int
	DroppedRewriteReasons []string
	EnglishRewriteCount   int
	SemanticDriftCount    int
	SemanticAnalysis      *SemanticQueryAnalysisDiagnostics
}

type SemanticQueryAnalysisDiagnostics struct {
	TimeMode          string
	Signals           []string
	MemoryDomain      string
	MemoryAbility     string
	EvidenceNeed      string
	Confidence        float64
	FieldConfidence   QueryAnalysisConfidence
	EntityMentions    []SemanticQueryEntityMentionDiagnostics
	QueryRewrites     []QueryRewrite
	SemanticAnchors   []SemanticAnchor
	ContextBlockHints []string
	PolicyHints       QueryPolicyHints
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
	analysis.Confidence = ruleConfidence(normalized, analysis)
	analysis.FieldConfidence = ruleFieldConfidence(normalized, analysis)
	return analysis, nil
}

func cloneQueryAnalysis(value QueryAnalysis) QueryAnalysis {
	out := value
	out.Terms = append([]string(nil), value.Terms...)
	out.EntityMentions = append([]QueryEntityMention(nil), value.EntityMentions...)
	out.Signals = append([]QuerySignal(nil), value.Signals...)
	out.QueryRewrites = append([]QueryRewrite(nil), value.QueryRewrites...)
	out.SemanticAnchors = append([]SemanticAnchor(nil), value.SemanticAnchors...)
	out.ContextBlockHints = append([]string(nil), value.ContextBlockHints...)
	if value.Diagnostics != nil {
		diagnostics := *value.Diagnostics
		diagnostics.DroppedRewriteReasons = append([]string(nil), value.Diagnostics.DroppedRewriteReasons...)
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
	out.EntityMentions = append([]SemanticQueryEntityMentionDiagnostics(nil), value.EntityMentions...)
	out.QueryRewrites = append([]QueryRewrite(nil), value.QueryRewrites...)
	out.SemanticAnchors = append([]SemanticAnchor(nil), value.SemanticAnchors...)
	out.ContextBlockHints = append([]string(nil), value.ContextBlockHints...)
	return &out
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
	switch {
	case normalized == "":
		return 0
	case analysis.MemoryAbility != MemoryAbilityDirectFact:
		return 0.78
	case len(analysis.EntityMentions) > 0:
		return 0.74
	case onlyExactFactSignal(analysis.Signals):
		return 0.42
	case len(analysis.Signals) > 0:
		return 0.68
	default:
		return 0.42
	}
}

func onlyExactFactSignal(signals []QuerySignal) bool {
	return len(signals) == 1 && signals[0] == QuerySignalExactFact
}

func ruleFieldConfidence(normalized string, analysis QueryAnalysis) QueryAnalysisConfidence {
	confidence := ruleConfidence(normalized, analysis)
	entityResolution := 0.0
	if len(analysis.EntityMentions) > 0 {
		entityResolution = 0.74
	}
	return QueryAnalysisConfidence{
		Overall:          confidence,
		TimeMode:         confidence,
		MemoryAbility:    confidence,
		MemoryDomain:     confidence,
		EvidenceNeed:     confidence,
		EntityResolution: entityResolution,
	}
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
