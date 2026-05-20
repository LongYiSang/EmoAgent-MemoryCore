package sqlite

import "testing"

func TestQueryAnalysisTimeModeCurrentRules(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  QueryTimeMode
	}{
		{name: "historical beats bitemporal", query: "以前是否一直喜欢咖啡", want: QueryTimeModeHistorical},
		{name: "bitemporal check", query: "是否一直讨厌早会", want: QueryTimeModeBitemporalCheck},
		{name: "current default", query: "喜欢咖啡吗", want: QueryTimeModeCurrent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryTimeMode(tt.query); got != tt.want {
				t.Fatalf("queryTimeMode(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestQueryAnalysisSignalsAccumulateCurrentRules(t *testing.T) {
	query := "debug 上次为什么不要提 我什么时候说过"
	got := querySignals(query, queryTimeMode(query))
	want := []QuerySignal{
		QuerySignalPastEventDirectFact,
		QuerySignalProvenanceSource,
		QuerySignalCausal,
		QuerySignalHistorical,
		QuerySignalProvenance,
		QuerySignalSensitivity,
		QuerySignalDebug,
	}
	if !equalQuerySignals(got, want) {
		t.Fatalf("querySignals(%q) = %#v, want %#v", query, got, want)
	}
}

func TestQueryAnalysisSupportsSemanticFields(t *testing.T) {
	analysis := QueryAnalysis{
		Source:     QueryAnalysisSourceMerged,
		Confidence: 0.81,
		FieldConfidence: QueryAnalysisConfidence{
			Overall:          0.81,
			TimeMode:         0.75,
			MemoryAbility:    0.82,
			MemoryDomain:     0.8,
			EvidenceNeed:     0.83,
			EntityResolution: 0.78,
		},
		QueryRewrites: []QueryRewrite{{
			Text:    "用户喜欢 Laufey 的来源",
			Purpose: "provenance_dense",
			Weight:  0.8,
		}},
		SemanticAnchors: []SemanticAnchor{{
			Text:       "Laufey",
			AnchorType: "entity_semantic",
			EntityID:   "ent_laufey",
			Weight:     0.65,
			Confidence: 0.78,
		}},
		ContextBlockHints: []string{MemoryBlockTypeProvenanceMemory},
		PolicyHints: QueryPolicyHints{
			PreferEvidencedByLinks: true,
			MaxHopsHint:            2,
		},
		Diagnostics: &QueryAnalysisDiagnostics{
			SemanticStatus:      "ok",
			SemanticProvider:    "sidecar",
			SemanticModel:       "configured-model",
			PromptVersion:       "semantic_query_analyzer.v0.1",
			SemanticLatencyMs:   17,
			RewriteCount:        1,
			SemanticAnchorCount: 1,
		},
	}
	if analysis.Source != QueryAnalysisSourceMerged ||
		analysis.QueryRewrites[0].Purpose != "provenance_dense" ||
		analysis.SemanticAnchors[0].AnchorType != "entity_semantic" ||
		analysis.ContextBlockHints[0] != MemoryBlockTypeProvenanceMemory ||
		!analysis.PolicyHints.PreferEvidencedByLinks ||
		analysis.Diagnostics.RewriteCount != 1 {
		t.Fatalf("semantic fields not retained: %#v", analysis)
	}
}

func TestQueryAnalysisRelationshipAndForgetRules(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		wantDomain   MemoryDomain
		wantAbility  MemoryAbility
		wantEvidence EvidenceNeed
		wantSignal   QuerySignal
	}{
		{
			name:         "relationship arc",
			query:        "我和 May 的关系变化轨迹是什么",
			wantDomain:   MemoryDomainRelationship,
			wantAbility:  MemoryAbilityRelationshipArc,
			wantEvidence: EvidenceNeedRelationshipTimeline,
			wantSignal:   QuerySignalRelationshipArc,
		},
		{
			name:         "forget delete",
			query:        "忘掉团子这条记忆",
			wantDomain:   MemoryDomainRelationship,
			wantAbility:  MemoryAbilityBoundary,
			wantEvidence: EvidenceNeedExactObservation,
			wantSignal:   QuerySignalForgetDelete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryMemoryDomain(tt.query); got != tt.wantDomain {
				t.Fatalf("queryMemoryDomain(%q) = %q, want %q", tt.query, got, tt.wantDomain)
			}
			if got := queryMemoryAbility(tt.query); got != tt.wantAbility {
				t.Fatalf("queryMemoryAbility(%q) = %q, want %q", tt.query, got, tt.wantAbility)
			}
			if got := queryEvidenceNeed(tt.query); got != tt.wantEvidence {
				t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", tt.query, got, tt.wantEvidence)
			}
			if !hasQuerySignal(QueryAnalysis{Signals: querySignals(tt.query, queryTimeMode(tt.query))}, tt.wantSignal) {
				t.Fatalf("querySignals(%q) missing %q", tt.query, tt.wantSignal)
			}
		})
	}
}

func TestQueryAnalysisProvenanceQuestionVariantsCurrentRules(t *testing.T) {
	tests := []string{
		"你是从哪里知道我喜欢Laufey的",
		"这件事你哪里知道的",
		"我喜欢Laufey是什么时候说的",
		"最早什么时候提过Laufey",
	}

	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			timeMode := queryTimeMode(query)
			got := querySignals(query, timeMode)
			if !hasQuerySignal(QueryAnalysis{Signals: got}, QuerySignalProvenance) ||
				!hasQuerySignal(QueryAnalysis{Signals: got}, QuerySignalProvenanceSource) {
				t.Fatalf("querySignals(%q) = %#v, want provenance and provenance_source", query, got)
			}
			if got := queryMemoryAbility(query); got != MemoryAbilityProvenance {
				t.Fatalf("queryMemoryAbility(%q) = %q, want %q", query, got, MemoryAbilityProvenance)
			}
			if got := queryEvidenceNeed(query); got != EvidenceNeedProvenanceSource {
				t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", query, got, EvidenceNeedProvenanceSource)
			}
		})
	}
}

func TestQueryAnalysisMemoryDomainPriorityCurrentRules(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  MemoryDomain
	}{
		{name: "environment beats work and profile", query: "python repo 偏好 缓存", want: MemoryDomainEnvironmentExperience},
		{name: "work beats profile", query: "repo workflow 喜欢", want: MemoryDomainWorkExperience},
		{name: "user profile", query: "我喜欢咖啡", want: MemoryDomainUserProfile},
		{name: "relationship default", query: "Long 和 May 最近聊了什么", want: MemoryDomainRelationship},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryMemoryDomain(tt.query); got != tt.want {
				t.Fatalf("queryMemoryDomain(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestQueryAnalysisMemoryAbilityPriorityCurrentRules(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  MemoryAbility
	}{
		{name: "provenance beats causal", query: "为什么失败的证据", want: MemoryAbilityProvenance},
		{name: "causal beats boundary", query: "为什么不要提早会", want: MemoryAbilityCausalExplain},
		{name: "boundary", query: "不要提早会", want: MemoryAbilityBoundary},
		{name: "supportive", query: "支持鼓励一下", want: MemoryAbilitySupportive},
		{name: "premise beats gotcha", query: "是不是一直报错", want: MemoryAbilityPremiseCheck},
		{name: "premise beats past event direct fact", query: "小李上次跟我吵架之后是不是老样子，完全没有任何改变？", want: MemoryAbilityPremiseCheck},
		{name: "gotcha beats workflow", query: "报错的操作步骤和坑", want: MemoryAbilityGotcha},
		{name: "workflow", query: "操作步骤是什么", want: MemoryAbilityWorkflow},
		{name: "past event direct fact", query: "上次部署结果", want: MemoryAbilityDirectFact},
		{name: "planning", query: "后续计划", want: MemoryAbilityPlanning},
		{name: "dynamic state", query: "这个项目最近进展怎么样", want: MemoryAbilityDynamicState},
		{name: "static preference with current wording", query: "我现在的偏好是什么", want: MemoryAbilityStaticState},
		{name: "static default config", query: "我的默认配置是什么", want: MemoryAbilityStaticState},
		{name: "bare historical direct fact", query: "我以前住在哪里", want: MemoryAbilityDirectFact},
		{name: "causal beats dynamic", query: "为什么我的状态变了", want: MemoryAbilityCausalExplain},
		{name: "direct celebration occasion is an event slot", query: "同事最近请大家喝了什么，是因为什么事情庆祝？", want: MemoryAbilityDirectFact},
		{name: "premise beats static", query: "我是不是一直不喜欢早会", want: MemoryAbilityPremiseCheck},
		{name: "direct fact", query: "咖啡", want: MemoryAbilityDirectFact},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryMemoryAbility(tt.query); got != tt.want {
				t.Fatalf("queryMemoryAbility(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestQueryAnalysisEvidenceNeedCurrentRules(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  EvidenceNeed
	}{
		{name: "provenance source", query: "这条记忆的来源", want: EvidenceNeedProvenanceSource},
		{name: "premise counterexample", query: "是不是一直讨厌上班", want: EvidenceNeedPremiseCounterexample},
		{name: "premise counterexample beats past event direct fact", query: "小李上次跟我吵架之后是不是老样子，完全没有任何改变？", want: EvidenceNeedPremiseCounterexample},
		{name: "gotcha note", query: "这次失败的坑", want: EvidenceNeedGotchaNote},
		{name: "procedure note", query: "部署流程步骤", want: EvidenceNeedProcedureNote},
		{name: "bare historical direct lookup", query: "以前住在哪里", want: EvidenceNeedExactObservation},
		{name: "dynamic state transition", query: "这个项目最近进展怎么样", want: EvidenceNeedStateTransition},
		{name: "static exact observation", query: "我的默认配置是什么", want: EvidenceNeedExactObservation},
		{name: "exact observation default", query: "喜欢咖啡", want: EvidenceNeedExactObservation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := queryEvidenceNeed(tt.query); got != tt.want {
				t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

func TestQueryAnalysisOrdinaryBooleanAndBareAlwaysStayDirectFact(t *testing.T) {
	tests := []string{
		"我是不是喜欢咖啡？",
		"我是否喜欢咖啡？",
		"我真的喜欢咖啡吗？",
		"我一直喜欢的饮料是什么？",
	}

	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			if got := queryTimeMode(query); got == QueryTimeModeBitemporalCheck {
				t.Fatalf("queryTimeMode(%q) = %q, want direct fact routing", query, got)
			}
			if got := queryMemoryAbility(query); got != MemoryAbilityDirectFact {
				t.Fatalf("queryMemoryAbility(%q) = %q, want %q", query, got, MemoryAbilityDirectFact)
			}
			if got := queryEvidenceNeed(query); got != EvidenceNeedExactObservation {
				t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", query, got, EvidenceNeedExactObservation)
			}
			signals := querySignals(query, queryTimeMode(query))
			for _, reject := range []QuerySignal{QuerySignalPremiseCheck, QuerySignalPremiseCounterexample} {
				if hasQuerySignal(QueryAnalysis{Signals: signals}, reject) {
					t.Fatalf("querySignals(%q) = %#v, should not include %q", query, signals, reject)
				}
			}
		})
	}
}

func TestQueryAnalysisStrongPremiseMarkersStillRouteToCounterexample(t *testing.T) {
	tests := []string{
		"我是不是一直都不喜欢早会？",
		"我从来没有自己下过厨房吗？",
		"我总是跟每个朋友都闹矛盾吗？",
	}

	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			if got := queryTimeMode(query); got != QueryTimeModeBitemporalCheck {
				t.Fatalf("queryTimeMode(%q) = %q, want %q", query, got, QueryTimeModeBitemporalCheck)
			}
			if got := queryMemoryAbility(query); got != MemoryAbilityPremiseCheck {
				t.Fatalf("queryMemoryAbility(%q) = %q, want %q", query, got, MemoryAbilityPremiseCheck)
			}
			if got := queryEvidenceNeed(query); got != EvidenceNeedPremiseCounterexample {
				t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", query, got, EvidenceNeedPremiseCounterexample)
			}
			signals := querySignals(query, queryTimeMode(query))
			for _, want := range []QuerySignal{QuerySignalPremiseCheck, QuerySignalPremiseCounterexample} {
				if !hasQuerySignal(QueryAnalysis{Signals: signals}, want) {
					t.Fatalf("querySignals(%q) = %#v, missing %q", query, signals, want)
				}
			}
		})
	}
}

func TestQueryAnalysisConditionalBooleanRiskRoutesToCounterexample(t *testing.T) {
	query := "如果 episode 被 redacted，是否还能暴露原文内容"

	if got := queryMemoryAbility(query); got != MemoryAbilityPremiseCheck {
		t.Fatalf("queryMemoryAbility(%q) = %q, want %q", query, got, MemoryAbilityPremiseCheck)
	}
	if got := queryEvidenceNeed(query); got != EvidenceNeedPremiseCounterexample {
		t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", query, got, EvidenceNeedPremiseCounterexample)
	}
	signals := querySignals(query, queryTimeMode(query))
	for _, want := range []QuerySignal{QuerySignalPremiseCheck, QuerySignalPremiseCounterexample} {
		if !hasQuerySignal(QueryAnalysis{Signals: signals}, want) {
			t.Fatalf("querySignals(%q) = %#v, missing %q", query, signals, want)
		}
	}
}

func TestQueryAnalysisStateTransitionCurrentRules(t *testing.T) {
	query := "我一开始把AI助手当成什么？后来这种看法发生了什么变化？"

	if got := queryTimeMode(query); got != QueryTimeModeHistorical {
		t.Fatalf("queryTimeMode(%q) = %q, want %q", query, got, QueryTimeModeHistorical)
	}
	if got := queryMemoryAbility(query); got != MemoryAbilityHistorical {
		t.Fatalf("queryMemoryAbility(%q) = %q, want %q", query, got, MemoryAbilityHistorical)
	}
	if got := queryEvidenceNeed(query); got != EvidenceNeedStateTransition {
		t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", query, got, EvidenceNeedStateTransition)
	}
	if !hasQuerySignal(QueryAnalysis{Signals: querySignals(query, queryTimeMode(query))}, QuerySignalHistorical) {
		t.Fatalf("querySignals(%q) missing historical", query)
	}
	if !hasQuerySignal(QueryAnalysis{Signals: querySignals(query, queryTimeMode(query))}, QuerySignalStateTransition) {
		t.Fatalf("querySignals(%q) missing state_transition", query)
	}
}

func TestQueryAnalysisBareHistoricalLookupIsNotStateTransition(t *testing.T) {
	query := "以前住在哪里"

	if got := queryTimeMode(query); got != QueryTimeModeHistorical {
		t.Fatalf("queryTimeMode(%q) = %q, want %q", query, got, QueryTimeModeHistorical)
	}
	if got := queryMemoryAbility(query); got != MemoryAbilityDirectFact {
		t.Fatalf("queryMemoryAbility(%q) = %q, want %q", query, got, MemoryAbilityDirectFact)
	}
	if got := queryEvidenceNeed(query); got != EvidenceNeedExactObservation {
		t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", query, got, EvidenceNeedExactObservation)
	}
	signals := querySignals(query, queryTimeMode(query))
	if hasQuerySignal(QueryAnalysis{Signals: signals}, QuerySignalStateTransition) {
		t.Fatalf("querySignals(%q) = %#v, should not include %q", query, signals, QuerySignalStateTransition)
	}
	if !hasQuerySignal(QueryAnalysis{Signals: signals}, QuerySignalHistorical) {
		t.Fatalf("querySignals(%q) = %#v, missing %q", query, signals, QuerySignalHistorical)
	}
}

func TestQueryAnalysisStateTransitionDoesNotTreatBareFromToAsHistorical(t *testing.T) {
	for _, query := range []string{"从北京到上海怎么走", "从 repo 到 docs 的路径"} {
		t.Run(query, func(t *testing.T) {
			if got := queryTimeMode(query); got != QueryTimeModeCurrent {
				t.Fatalf("queryTimeMode(%q) = %q, want %q", query, got, QueryTimeModeCurrent)
			}
			if got := queryEvidenceNeed(query); got != EvidenceNeedExactObservation {
				t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", query, got, EvidenceNeedExactObservation)
			}
		})
	}
}

func TestQueryAnalysisPastEventDirectFactMarkersAreHistorical(t *testing.T) {
	for _, query := range []string{
		"那天我跟谁去的？",
		"五一我跟谁去的？",
		"有一次我跟谁去的？",
		"周末我跟谁去的？",
		"最近一次我跟谁去的？",
		"前几天我跟谁去的？",
	} {
		t.Run(query, func(t *testing.T) {
			if got := queryTimeMode(query); got != QueryTimeModeHistorical {
				t.Fatalf("queryTimeMode(%q) = %q, want %q", query, got, QueryTimeModeHistorical)
			}
			signals := querySignals(query, queryTimeMode(query))
			if !hasQuerySignal(QueryAnalysis{Signals: signals}, QuerySignalPastEventDirectFact) {
				t.Fatalf("querySignals(%q) = %#v, missing %q", query, signals, QuerySignalPastEventDirectFact)
			}
		})
	}
}

func TestQueryAnalysisPastEventBundleRequiresIndependentSlots(t *testing.T) {
	oneSlot := "那天我跟谁去的？"
	oneSlotSignals := querySignals(oneSlot, queryTimeMode(oneSlot))
	if !hasQuerySignal(QueryAnalysis{Signals: oneSlotSignals}, QuerySignalPastEventDirectFact) {
		t.Fatalf("querySignals(%q) = %#v, missing %q", oneSlot, oneSlotSignals, QuerySignalPastEventDirectFact)
	}
	if hasQuerySignal(QueryAnalysis{Signals: oneSlotSignals}, QuerySignalEventBundle) {
		t.Fatalf("querySignals(%q) = %#v, should not include %q for one slot", oneSlot, oneSlotSignals, QuerySignalEventBundle)
	}

	bundled := "上次去蜀九香火锅，我跟谁去的，排了多久的队才吃上？"
	bundledSignals := querySignals(bundled, queryTimeMode(bundled))
	if !hasQuerySignal(QueryAnalysis{Signals: bundledSignals}, QuerySignalEventBundle) {
		t.Fatalf("querySignals(%q) = %#v, missing %q", bundled, bundledSignals, QuerySignalEventBundle)
	}
}

func TestQueryAnalysisStateTransitionRequiresOldAndNewContrast(t *testing.T) {
	for _, query := range []string{
		"后来我们聊了什么？",
		"我变成会员了吗？",
		"这个东西发生变化了吗？",
	} {
		t.Run(query, func(t *testing.T) {
			if hasStateTransitionIntent(query) {
				t.Fatalf("hasStateTransitionIntent(%q) = true, want false without old/new contrast", query)
			}
			signals := querySignals(query, queryTimeMode(query))
			if hasQuerySignal(QueryAnalysis{Signals: signals}, QuerySignalStateTransition) {
				t.Fatalf("querySignals(%q) = %#v, should not include %q", query, signals, QuerySignalStateTransition)
			}
		})
	}
}

func TestQueryAnalysisSocialRelationshipTransitionMarkers(t *testing.T) {
	for _, query := range []string{
		"我跟小李之前闹了什么矛盾，后来是怎么和好的？",
		"我跟小李以前闹矛盾，现在已经和解了吗？",
		"之前跟朋友闹矛盾，后来是不是翻篇了？",
	} {
		t.Run(query, func(t *testing.T) {
			if got := queryTimeMode(query); got != QueryTimeModeHistorical {
				t.Fatalf("queryTimeMode(%q) = %q, want %q", query, got, QueryTimeModeHistorical)
			}
			if got := queryMemoryAbility(query); got != MemoryAbilityHistorical {
				t.Fatalf("queryMemoryAbility(%q) = %q, want %q", query, got, MemoryAbilityHistorical)
			}
			if got := queryEvidenceNeed(query); got != EvidenceNeedStateTransition {
				t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", query, got, EvidenceNeedStateTransition)
			}
			signals := querySignals(query, queryTimeMode(query))
			if !hasQuerySignal(QueryAnalysis{Signals: signals}, QuerySignalStateTransition) {
				t.Fatalf("querySignals(%q) = %#v, missing %q", query, signals, QuerySignalStateTransition)
			}
			if !hasQuerySignal(QueryAnalysis{Signals: signals}, QuerySignalHistorical) {
				t.Fatalf("querySignals(%q) = %#v, missing %q", query, signals, QuerySignalHistorical)
			}
		})
	}
}

func TestQueryAnalysisExactFactSignalDoesNotRaisePlainDirectFactConfidence(t *testing.T) {
	analysis := QueryAnalysis{
		Normalized:    "咖啡",
		TimeMode:      queryTimeMode("咖啡"),
		Signals:       querySignals("咖啡", queryTimeMode("咖啡")),
		MemoryAbility: queryMemoryAbility("咖啡"),
		EvidenceNeed:  queryEvidenceNeed("咖啡"),
	}

	if analysis.MemoryAbility != MemoryAbilityDirectFact {
		t.Fatalf("memory_ability = %q, want %q", analysis.MemoryAbility, MemoryAbilityDirectFact)
	}
	if analysis.EvidenceNeed != EvidenceNeedExactObservation {
		t.Fatalf("evidence_need = %q, want %q", analysis.EvidenceNeed, EvidenceNeedExactObservation)
	}
	if !hasQuerySignal(analysis, QuerySignalExactFact) {
		t.Fatalf("signals = %#v, missing %q", analysis.Signals, QuerySignalExactFact)
	}
	if got := ruleConfidence("咖啡", analysis); got != 0.42 {
		t.Fatalf("ruleConfidence for exact_fact-only plain query = %v, want 0.42", got)
	}
}

func TestQueryAnalysisW004SoftRoutingClasses(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		wantTimeMode  QueryTimeMode
		wantAbility   MemoryAbility
		wantEvidence  EvidenceNeed
		wantSignals   []QuerySignal
		rejectSignals []QuerySignal
	}{
		{
			name:         "social conflict reconciliation state transition",
			query:        "我跟小李之前闹了什么矛盾，后来是怎么和好的？",
			wantTimeMode: QueryTimeModeHistorical,
			wantAbility:  MemoryAbilityHistorical,
			wantEvidence: EvidenceNeedStateTransition,
			wantSignals: []QuerySignal{
				QuerySignalStateTransition,
				QuerySignalHistorical,
			},
		},
		{
			name:         "social all-friends premise counterexample",
			query:        "我的人际关系是不是很糟糕，跟身边每个朋友都闹过矛盾？",
			wantTimeMode: QueryTimeModeBitemporalCheck,
			wantAbility:  MemoryAbilityPremiseCheck,
			wantEvidence: EvidenceNeedPremiseCounterexample,
			wantSignals: []QuerySignal{
				QuerySignalPremiseCounterexample,
				QuerySignalPremiseCheck,
			},
		},
		{
			name:         "social negative premise beats last-time direct fact",
			query:        "小李上次跟我吵架之后是不是老样子，完全没有任何改变？",
			wantTimeMode: QueryTimeModeHistorical,
			wantAbility:  MemoryAbilityPremiseCheck,
			wantEvidence: EvidenceNeedPremiseCounterexample,
			wantSignals: []QuerySignal{
				QuerySignalPremiseCounterexample,
				QuerySignalPremiseCheck,
			},
			rejectSignals: []QuerySignal{QuerySignalPastEventDirectFact},
		},
		{
			name:         "past event direct fact with event bundle",
			query:        "上次去蜀九香火锅，我跟谁去的，排了多久的队才吃上？",
			wantTimeMode: QueryTimeModeHistorical,
			wantAbility:  MemoryAbilityDirectFact,
			wantEvidence: EvidenceNeedExactObservation,
			wantSignals: []QuerySignal{
				QuerySignalPastEventDirectFact,
				QuerySignalEventBundle,
			},
			rejectSignals: []QuerySignal{QuerySignalStateTransition},
		},
		{
			name:         "state transition keeps causal as secondary signal",
			query:        "我以前从来不运动，最近为什么开始健身了，效果怎么样？",
			wantTimeMode: QueryTimeModeHistorical,
			wantAbility:  MemoryAbilityHistorical,
			wantEvidence: EvidenceNeedStateTransition,
			wantSignals: []QuerySignal{
				QuerySignalStateTransition,
				QuerySignalCausalChain,
			},
		},
		{
			name:         "provenance source question",
			query:        "小陈建议我睡前听白噪音这件事，是什么时候告诉我的？",
			wantTimeMode: QueryTimeModeCurrent,
			wantAbility:  MemoryAbilityProvenance,
			wantEvidence: EvidenceNeedProvenanceSource,
			wantSignals:  []QuerySignal{QuerySignalProvenanceSource},
		},
		{
			name:         "universal premise counterexample",
			query:        "我是不是完全不会做饭，从来没自己下过厨房？",
			wantTimeMode: QueryTimeModeBitemporalCheck,
			wantAbility:  MemoryAbilityPremiseCheck,
			wantEvidence: EvidenceNeedPremiseCounterexample,
			wantSignals:  []QuerySignal{QuerySignalPremiseCounterexample},
		},
		{
			name:         "reflection summary",
			query:        "这两个月我变化最大或者进步最大的是什么？",
			wantTimeMode: QueryTimeModeCurrent,
			wantAbility:  MemoryAbilityHistorical,
			wantEvidence: EvidenceNeedStateTransition,
			wantSignals:  []QuerySignal{QuerySignalReflectionSummary},
		},
		{
			name:         "direct celebration occasion does not become causal",
			query:        "同事最近请大家喝了什么，是因为什么事情庆祝？",
			wantTimeMode: QueryTimeModeCurrent,
			wantAbility:  MemoryAbilityDirectFact,
			wantEvidence: EvidenceNeedExactObservation,
			rejectSignals: []QuerySignal{
				QuerySignalCausal,
				QuerySignalCausalChain,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			timeMode := queryTimeMode(tt.query)
			if timeMode != tt.wantTimeMode {
				t.Fatalf("queryTimeMode(%q) = %q, want %q", tt.query, timeMode, tt.wantTimeMode)
			}
			if got := queryMemoryAbility(tt.query); got != tt.wantAbility {
				t.Fatalf("queryMemoryAbility(%q) = %q, want %q", tt.query, got, tt.wantAbility)
			}
			if got := queryEvidenceNeed(tt.query); got != tt.wantEvidence {
				t.Fatalf("queryEvidenceNeed(%q) = %q, want %q", tt.query, got, tt.wantEvidence)
			}
			signals := querySignals(tt.query, timeMode)
			for _, want := range tt.wantSignals {
				if !hasQuerySignal(QueryAnalysis{Signals: signals}, want) {
					t.Fatalf("querySignals(%q) = %#v, missing %q", tt.query, signals, want)
				}
			}
			for _, reject := range tt.rejectSignals {
				if hasQuerySignal(QueryAnalysis{Signals: signals}, reject) {
					t.Fatalf("querySignals(%q) = %#v, should not include %q", tt.query, signals, reject)
				}
			}
		})
	}
}

func equalQuerySignals(a []QuerySignal, b []QuerySignal) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
