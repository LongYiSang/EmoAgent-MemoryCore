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

const (
	QueryTimeModeCurrent         QueryTimeMode = "current"
	QueryTimeModeHistorical      QueryTimeMode = "historical"
	QueryTimeModeBitemporalCheck QueryTimeMode = "bitemporal_check"

	QuerySignalCausal      QuerySignal = "causal"
	QuerySignalHistorical  QuerySignal = "historical"
	QuerySignalProvenance  QuerySignal = "provenance"
	QuerySignalSensitivity QuerySignal = "sensitivity"
	QuerySignalDebug       QuerySignal = "debug"

	MemoryDomainRelationship          MemoryDomain = "relationship_memory"
	MemoryDomainUserProfile           MemoryDomain = "user_profile_memory"
	MemoryDomainWorkExperience        MemoryDomain = "work_experience_memory"
	MemoryDomainEnvironmentExperience MemoryDomain = "environment_experience_memory"

	MemoryAbilityDirectFact    MemoryAbility = "direct_fact"
	MemoryAbilityCausalExplain MemoryAbility = "causal_explain"
	MemoryAbilityHistorical    MemoryAbility = "historical"
	MemoryAbilityProvenance    MemoryAbility = "provenance"
	MemoryAbilityBoundary      MemoryAbility = "boundary"
	MemoryAbilitySupportive    MemoryAbility = "supportive"
	MemoryAbilityPlanning      MemoryAbility = "planning"
	MemoryAbilityStaticState   MemoryAbility = "static_state"
	MemoryAbilityDynamicState  MemoryAbility = "dynamic_state"
	MemoryAbilityWorkflow      MemoryAbility = "workflow"
	MemoryAbilityGotcha        MemoryAbility = "gotcha"
	MemoryAbilityPremiseCheck  MemoryAbility = "premise_check"

	EvidenceNeedExactObservation      EvidenceNeed = "exact_observation"
	EvidenceNeedStateTransition       EvidenceNeed = "state_transition"
	EvidenceNeedProcedureNote         EvidenceNeed = "procedure_note"
	EvidenceNeedGotchaNote            EvidenceNeed = "gotcha_note"
	EvidenceNeedPremiseCounterexample EvidenceNeed = "premise_counterexample"
	EvidenceNeedProvenanceSource      EvidenceNeed = "provenance_source"

	QueryEntityMentionKindCanonical QueryEntityMentionKind = "canonical_name"
	QueryEntityMentionKindAlias     QueryEntityMentionKind = "entity_alias"
)

type QueryAnalysis struct {
	Raw            string
	Normalized     string
	Terms          []string
	EntityMentions []QueryEntityMention
	TimeMode       QueryTimeMode
	Signals        []QuerySignal
	MemoryDomain   MemoryDomain
	MemoryAbility  MemoryAbility
	EvidenceNeed   EvidenceNeed
}

type QueryEntityMention struct {
	EntityID      string
	CanonicalName string
	Alias         string
	MatchText     string
	MatchKind     QueryEntityMentionKind
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
	}
	analysis.Signals = querySignals(normalized, analysis.TimeMode)
	mentions, err := r.matchEntityMentions(ctx, personaID, normalized, policy)
	if err != nil {
		return QueryAnalysis{}, err
	}
	analysis.EntityMentions = mentions
	return analysis, nil
}

func queryTimeMode(normalized string) QueryTimeMode {
	if containsAny(normalized, "以前", "过去", "上次", "历史", "之前", "曾经", "从前", "prior", "previous", "last time", "historical", "history", "before") {
		return QueryTimeModeHistorical
	}
	if containsAny(normalized, "一直", "是否一直", "是不是一直", "always") {
		return QueryTimeModeBitemporalCheck
	}
	return QueryTimeModeCurrent
}

func querySignals(normalized string, timeMode QueryTimeMode) []QuerySignal {
	var signals []QuerySignal
	if containsAny(normalized, "为什么", "原因", "导致", "怎么会", "为何", "why", "cause", "caused", "because") {
		signals = append(signals, QuerySignalCausal)
	}
	if timeMode == QueryTimeModeHistorical {
		signals = append(signals, QuerySignalHistorical)
	}
	if containsAny(normalized, "证据", "来源", "根据", "我什么时候说过", "哪次说过", "什么时候说过", "source", "evidence", "provenance") {
		signals = append(signals, QuerySignalProvenance)
	}
	if containsAny(normalized, "隐私", "敏感", "不要提", "别提", "不要再提", "忘掉", "边界", "boundary", "private", "sensitive") {
		signals = append(signals, QuerySignalSensitivity)
	}
	if containsAny(normalized, "debug", "调试", "diagnostic", "diagnostics", "诊断") {
		signals = append(signals, QuerySignalDebug)
	}
	return signals
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
	case containsAny(normalized, "证据", "来源", "根据", "我什么时候说过", "哪次说过", "什么时候说过", "source", "evidence", "provenance"):
		return MemoryAbilityProvenance
	case containsAny(normalized, "为什么", "原因", "导致", "怎么会", "为何", "why", "cause", "caused", "because"):
		return MemoryAbilityCausalExplain
	case containsAny(normalized, "不要提", "别提", "不要再提", "边界", "不要提醒", "boundary"):
		return MemoryAbilityBoundary
	case containsAny(normalized, "支持", "安慰", "鼓励", "陪伴", "support", "supportive"):
		return MemoryAbilitySupportive
	case containsAny(normalized, "是不是", "是否", "真的", "一直", "always"):
		return MemoryAbilityPremiseCheck
	case containsAny(normalized, "坑", "踩坑", "失败", "报错", "错误", "故障", "gotcha", "pitfall", "failed", "failure", "error"):
		return MemoryAbilityGotcha
	case containsAny(normalized, "流程", "步骤", "怎么做", "操作步骤", "workflow", "procedure"):
		return MemoryAbilityWorkflow
	case queryTimeMode(normalized) == QueryTimeModeHistorical:
		return MemoryAbilityHistorical
	case containsAny(normalized, "计划", "规划", "planning"):
		return MemoryAbilityPlanning
	default:
		return MemoryAbilityDirectFact
	}
}

func queryEvidenceNeed(normalized string) EvidenceNeed {
	switch {
	case containsAny(normalized, "证据", "来源", "根据", "我什么时候说过", "哪次说过", "什么时候说过", "source", "evidence", "provenance"):
		return EvidenceNeedProvenanceSource
	case containsAny(normalized, "是不是", "是否", "真的", "一直", "always"):
		return EvidenceNeedPremiseCounterexample
	case containsAny(normalized, "坑", "踩坑", "失败", "报错", "错误", "故障", "gotcha", "pitfall", "failed", "failure", "error"):
		return EvidenceNeedGotchaNote
	case containsAny(normalized, "流程", "步骤", "怎么做", "操作步骤", "workflow", "procedure"):
		return EvidenceNeedProcedureNote
	case queryTimeMode(normalized) == QueryTimeModeHistorical:
		return EvidenceNeedStateTransition
	default:
		return EvidenceNeedExactObservation
	}
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
