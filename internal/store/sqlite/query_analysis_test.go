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
