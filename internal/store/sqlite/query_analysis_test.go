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
			if got := querySignals(query, timeMode); !equalQuerySignals(got, []QuerySignal{QuerySignalProvenance}) {
				t.Fatalf("querySignals(%q) = %#v, want provenance", query, got)
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
		{name: "gotcha beats workflow", query: "报错的操作步骤和坑", want: MemoryAbilityGotcha},
		{name: "workflow", query: "操作步骤是什么", want: MemoryAbilityWorkflow},
		{name: "historical", query: "上次部署结果", want: MemoryAbilityHistorical},
		{name: "planning", query: "后续计划", want: MemoryAbilityPlanning},
		{name: "dynamic state", query: "这个项目最近进展怎么样", want: MemoryAbilityDynamicState},
		{name: "static preference with current wording", query: "我现在的偏好是什么", want: MemoryAbilityStaticState},
		{name: "static default config", query: "我的默认配置是什么", want: MemoryAbilityStaticState},
		{name: "historical beats static", query: "我以前住在哪里", want: MemoryAbilityHistorical},
		{name: "causal beats dynamic", query: "为什么我的状态变了", want: MemoryAbilityCausalExplain},
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
		{name: "gotcha note", query: "这次失败的坑", want: EvidenceNeedGotchaNote},
		{name: "procedure note", query: "部署流程步骤", want: EvidenceNeedProcedureNote},
		{name: "historical state transition", query: "以前住在哪里", want: EvidenceNeedStateTransition},
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
