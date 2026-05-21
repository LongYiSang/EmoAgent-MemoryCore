package memorycore

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	internalmirror "github.com/longyisang/emoagent-memorycore/internal/mirror"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestQueryAnalysisRuleOnlyDefaultDoesNotCallSemantic(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		Terms:         []string{"咖啡"},
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
	}
	pipeline := newQueryAnalysisPipeline(staticRuleQueryAnalyzer{analysis: rule}, panicSemanticQueryAnalyzer{}, QueryAnalysisOptions{})

	got, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Now:       fixedQueryAnalysisNow(),
	})
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if got.Source != memsqlite.QueryAnalysisSourceRuleOnly {
		t.Fatalf("source = %q, want rule_only", got.Source)
	}
	if got.MemoryAbility != memsqlite.MemoryAbilityDirectFact || got.Confidence != 0.42 {
		t.Fatalf("analysis = %#v, want unchanged rule analysis", got)
	}
}

func TestQueryAnalysisSemanticOnLowConfidenceLegacyDiagnostics(t *testing.T) {
	semantic := &SemanticQueryAnalysisResult{
		Status: "ok",
		Analysis: SemanticQueryAnalysis{
			Confidence: 0.95,
			FieldConfidence: QueryAnalysisConfidence{
				Overall: 0.95,
			},
		},
	}

	tests := []struct {
		name         string
		rule         memsqlite.QueryAnalysis
		wantSemantic bool
	}{
		{
			name: "low confidence triggers semantic",
			rule: memsqlite.QueryAnalysis{
				Raw:           "咖啡",
				Normalized:    "咖啡",
				Terms:         []string{"咖啡"},
				TimeMode:      memsqlite.QueryTimeModeCurrent,
				MemoryDomain:  memsqlite.MemoryDomainUserProfile,
				MemoryAbility: memsqlite.MemoryAbilityDirectFact,
				EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
				Source:        memsqlite.QueryAnalysisSourceRuleOnly,
				Confidence:    0.42,
				Signals:       []memsqlite.QuerySignal{memsqlite.QuerySignalExactFact},
				Diagnostics: &memsqlite.QueryAnalysisDiagnostics{
					RuleConfidenceLegacy: 0.42,
					RuleConfidenceReason: "exact_fact_only",
					Signals:              []string{string(memsqlite.QuerySignalExactFact)},
					EntityMentionCount:   0,
				},
			},
			wantSemantic: true,
		},
		{
			name: "high confidence skips semantic",
			rule: memsqlite.QueryAnalysis{
				Raw:           "LongYi 喜欢咖啡吗",
				Normalized:    "longyi 喜欢咖啡吗",
				Terms:         []string{"longyi", "喜欢咖啡吗"},
				TimeMode:      memsqlite.QueryTimeModeCurrent,
				MemoryDomain:  memsqlite.MemoryDomainUserProfile,
				MemoryAbility: memsqlite.MemoryAbilityDirectFact,
				EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
				Source:        memsqlite.QueryAnalysisSourceRuleOnly,
				Confidence:    0.74,
				EntityMentions: []memsqlite.QueryEntityMention{{
					EntityID:  "ent_user",
					MatchText: "LongYi",
				}},
				Diagnostics: &memsqlite.QueryAnalysisDiagnostics{
					RuleConfidenceLegacy: 0.74,
					RuleConfidenceReason: "entity_mention",
					EntityMentionCount:   1,
				},
			},
			wantSemantic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pipeline := newQueryAnalysisPipeline(
				staticRuleQueryAnalyzer{analysis: tt.rule},
				staticSemanticQueryAnalyzer{result: semantic},
				QueryAnalysisOptions{Provider: QueryAnalysisProviderSidecar, Mode: QueryAnalysisModeSemanticOnLowConfidence, MinConfidenceToOverride: 0.72},
			)

			got, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{
				PersonaID: "default",
				QueryText: tt.rule.Raw,
				Now:       fixedQueryAnalysisNow(),
			})
			if err != nil {
				t.Fatalf("analyze query: %v", err)
			}
			if got.Diagnostics == nil {
				t.Fatal("diagnostics is nil")
			}
			if got.Diagnostics.SemanticDecisionLegacy != tt.wantSemantic {
				t.Fatalf("semantic decision = %v, want %v", got.Diagnostics.SemanticDecisionLegacy, tt.wantSemantic)
			}
			if got.Diagnostics.MinConfidenceToOverride != 0.72 {
				t.Fatalf("min confidence = %v, want 0.72", got.Diagnostics.MinConfidenceToOverride)
			}
			if got.Diagnostics.RuleConfidenceReason == "" {
				t.Fatalf("legacy reason is empty: %#v", got.Diagnostics)
			}
		})
	}
}

func TestQueryAnalysisRuleOnlyKeepsLegacyDiagnosticsVisible(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		Terms:         []string{"咖啡"},
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
		Diagnostics: &memsqlite.QueryAnalysisDiagnostics{
			RuleConfidenceLegacy: 0.42,
			RuleConfidenceReason: "default_direct_fact",
			EntityMentionCount:   0,
		},
	}
	pipeline := newQueryAnalysisPipeline(staticRuleQueryAnalyzer{analysis: rule}, panicSemanticQueryAnalyzer{}, QueryAnalysisOptions{})

	got, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Now:       fixedQueryAnalysisNow(),
	})
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if got.Source != memsqlite.QueryAnalysisSourceRuleOnly {
		t.Fatalf("source = %q, want rule_only", got.Source)
	}
	if got.Diagnostics == nil || got.Diagnostics.RuleConfidenceReason != "default_direct_fact" {
		t.Fatalf("diagnostics = %#v, want visible legacy reason", got.Diagnostics)
	}
	if got.Diagnostics.SemanticDecisionLegacy {
		t.Fatalf("semantic decision = true, want false in rule-only mode")
	}
}

func TestQueryAnalysisSemanticMergeClampsUntrustedFields(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "Long 证据",
		Normalized:    "long 证据",
		Terms:         []string{"long", "证据"},
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.4,
		EntityMentions: []memsqlite.QueryEntityMention{{
			EntityID:      "ent_visible",
			CanonicalName: "Long",
			MatchText:     "Long",
			MatchKind:     memsqlite.QueryEntityMentionKindCanonical,
		}},
	}
	semantic := SemanticQueryAnalysisResult{
		Status:        "ok",
		Provider:      "sidecar",
		Model:         "semantic-model",
		PromptVersion: "semantic_query_analyzer.v0.1",
		Analysis: SemanticQueryAnalysis{
			TimeMode:      string(memsqlite.QueryTimeModeHistorical),
			MemoryDomain:  string(memsqlite.MemoryDomainRelationship),
			MemoryAbility: string(memsqlite.MemoryAbilityProvenance),
			EvidenceNeed:  string(memsqlite.EvidenceNeedProvenanceSource),
			Signals: []string{
				string(memsqlite.QuerySignalProvenance),
				string(memsqlite.QuerySignalPastEventDirectFact),
				string(memsqlite.QuerySignalEventBundle),
				"made_up_signal",
			},
			Confidence: 0.9,
			FieldConfidence: QueryAnalysisConfidence{
				Overall:       0.9,
				TimeMode:      0.9,
				MemoryAbility: 0.9,
				MemoryDomain:  0.9,
				EvidenceNeed:  0.9,
			},
			EntityMentions: []SemanticQueryEntityMention{
				{EntityID: "ent_visible", CanonicalName: "Long", MatchText: "Long", MatchKind: string(memsqlite.QueryEntityMentionKindCanonical), Confidence: 0.8},
				{EntityID: "ent_hidden", CanonicalName: "Hidden", MatchText: "Hidden", MatchKind: string(memsqlite.QueryEntityMentionKindCanonical), Confidence: 0.99},
				{EntityID: "ent_visible", CanonicalName: "Low", MatchText: "Low", MatchKind: string(memsqlite.QueryEntityMentionKindAlias), Confidence: 0.2},
			},
			QueryRewrites: []QueryRewrite{
				{Text: "rewrite low", Purpose: "dense", Weight: -2},
				{Text: "rewrite high", Purpose: "dense", Weight: 5},
				{Text: "rewrite over limit", Purpose: "dense", Weight: 0.5},
			},
			SemanticAnchors: []SemanticAnchor{
				{Text: "Long", AnchorType: "entity_semantic", EntityID: "ent_visible", Weight: 0.99, Confidence: 0.8},
				{Text: "Hidden", AnchorType: "entity_semantic", EntityID: "ent_hidden", Weight: 0.5, Confidence: 0.9},
			},
			ContextBlockHints: []string{MemoryBlockTypeProvenanceMemory, "not_a_block"},
			PolicyHints: QueryPolicyHints{
				PreferEvidencedByLinks: true,
				MaxHopsHint:            3,
			},
		},
	}

	got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{
		MinConfidenceToOverride:     0.72,
		MinEntitySemanticConfidence: 0.70,
		MaxQueryRewrites:            2,
		MaxSemanticAnchors:          1,
	}, visibleEntityHintsFromRule(rule))

	if got.Source != memsqlite.QueryAnalysisSourceMerged {
		t.Fatalf("source = %q, want merged", got.Source)
	}
	if got.TimeMode != memsqlite.QueryTimeModeHistorical || got.MemoryAbility != memsqlite.MemoryAbilityProvenance || got.EvidenceNeed != memsqlite.EvidenceNeedProvenanceSource {
		t.Fatalf("semantic enum fields not merged: %#v", got)
	}
	if len(got.QueryRewrites) != 2 || got.QueryRewrites[0].Weight != 0.1 || got.QueryRewrites[1].Weight != 0.9 {
		t.Fatalf("query rewrites = %#v, want capped and clamped", got.QueryRewrites)
	}
	if len(got.SemanticAnchors) != 1 || got.SemanticAnchors[0].EntityID != "ent_visible" || got.SemanticAnchors[0].Weight != 0.65 {
		t.Fatalf("semantic anchors = %#v, want visible entity and capped weight", got.SemanticAnchors)
	}
	if len(got.EntityMentions) != 1 || got.EntityMentions[0].EntityID != "ent_visible" {
		t.Fatalf("entity mentions = %#v, want only visible confident entity", got.EntityMentions)
	}
	if !hasStoreSignal(got.Signals, memsqlite.QuerySignalProvenance) ||
		!hasStoreSignal(got.Signals, memsqlite.QuerySignalPastEventDirectFact) ||
		!hasStoreSignal(got.Signals, memsqlite.QuerySignalEventBundle) ||
		hasStoreSignal(got.Signals, memsqlite.QuerySignal("made_up_signal")) {
		t.Fatalf("signals = %#v, want valid semantic signals only", got.Signals)
	}
	if got.Diagnostics == nil || got.Diagnostics.SemanticStatus != "ok" || got.Diagnostics.RewriteCount != 2 || got.Diagnostics.SemanticAnchorCount != 1 {
		t.Fatalf("diagnostics = %#v, want semantic merge diagnostics", got.Diagnostics)
	}
	if got.Diagnostics.SemanticAnalysis == nil {
		t.Fatalf("semantic analysis diagnostics = nil, want LLM returned struct snapshot")
	}
	if got.Diagnostics.SemanticAnalysis.MemoryAbility != string(memsqlite.MemoryAbilityProvenance) ||
		len(got.Diagnostics.SemanticAnalysis.QueryRewrites) != 3 ||
		len(got.Diagnostics.SemanticAnalysis.EntityMentions) != 3 ||
		len(got.Diagnostics.SemanticAnalysis.SemanticAnchors) != 2 {
		t.Fatalf("semantic analysis diagnostics = %#v, want unclamped provider struct snapshot", got.Diagnostics.SemanticAnalysis)
	}
}

func TestQueryAnalysisSemanticFallbackKeepsReturnedStructSnapshot(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
	}
	got := semanticRuleFallback(rule, "provider_error", SemanticQueryAnalysisResult{
		Status:        "error",
		Provider:      "deepseek",
		Model:         "deepseek-v4-flash",
		PromptVersion: "semantic_query_analyzer.v0.1",
		LatencyMs:     8123,
		Analysis: SemanticQueryAnalysis{
			MemoryAbility: string(memsqlite.MemoryAbilityDirectFact),
			QueryRewrites: []QueryRewrite{{Text: "咖啡偏好", Purpose: "semantic_recall", Weight: 0.5}},
		},
	})

	if got.Source != memsqlite.QueryAnalysisSourceSemanticFallback || got.Diagnostics == nil {
		t.Fatalf("fallback analysis = %#v, want semantic fallback diagnostics", got)
	}
	if got.Diagnostics.FallbackReason != "provider_error" || got.Diagnostics.SemanticProvider != "deepseek" || got.Diagnostics.SemanticLatencyMs != 8123 {
		t.Fatalf("diagnostics = %#v, want provider error metadata retained", got.Diagnostics)
	}
	if got.Diagnostics.SemanticAnalysis == nil ||
		got.Diagnostics.SemanticAnalysis.MemoryAbility != string(memsqlite.MemoryAbilityDirectFact) ||
		len(got.Diagnostics.SemanticAnalysis.QueryRewrites) != 1 {
		t.Fatalf("semantic analysis diagnostics = %#v, want returned struct retained on error", got.Diagnostics.SemanticAnalysis)
	}
}

func TestQueryAnalysisSemanticRewriteOnlyKeepsRuleControlFields(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		Terms:         []string{"咖啡"},
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.6,
	}
	semantic := SemanticQueryAnalysisResult{
		Status: "ok",
		Analysis: SemanticQueryAnalysis{
			TimeMode:      string(memsqlite.QueryTimeModeHistorical),
			MemoryDomain:  string(memsqlite.MemoryDomainRelationship),
			MemoryAbility: string(memsqlite.MemoryAbilityCausalExplain),
			EvidenceNeed:  string(memsqlite.EvidenceNeedStateTransition),
			Confidence:    0.99,
			FieldConfidence: QueryAnalysisConfidence{
				Overall:       0.99,
				TimeMode:      0.99,
				MemoryAbility: 0.99,
				MemoryDomain:  0.99,
				EvidenceNeed:  0.99,
			},
			QueryRewrites: []QueryRewrite{{Text: "用户关于咖啡的偏好", Purpose: "semantic_recall", Weight: 0.5}},
		},
	}

	got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{
		Mode:                    QueryAnalysisModeSemanticRewriteOnly,
		MinConfidenceToOverride: 0.72,
		MaxQueryRewrites:        2,
	}, nil)

	if got.TimeMode != rule.TimeMode || got.MemoryAbility != rule.MemoryAbility || got.EvidenceNeed != rule.EvidenceNeed || got.MemoryDomain != rule.MemoryDomain {
		t.Fatalf("control fields changed in rewrite-only mode: %#v", got)
	}
	if len(got.QueryRewrites) != 1 {
		t.Fatalf("query rewrites = %#v, want semantic dense hints kept", got.QueryRewrites)
	}
	if len(got.ContextBlockHints) != 0 {
		t.Fatalf("context block hints = %#v, want rule-derived none for direct fact", got.ContextBlockHints)
	}
}

func TestQueryAnalysisSemanticMergeDoesNotForcePremiseForDirectFactRule(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "我是不是喜欢咖啡？",
		Normalized:    "我是不是喜欢咖啡？",
		Terms:         []string{"我是不是喜欢咖啡？"},
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Signals:       []memsqlite.QuerySignal{memsqlite.QuerySignalExactFact},
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
	}
	semantic := SemanticQueryAnalysisResult{
		Status: "ok",
		Analysis: SemanticQueryAnalysis{
			TimeMode:      string(memsqlite.QueryTimeModeBitemporalCheck),
			MemoryDomain:  string(memsqlite.MemoryDomainUserProfile),
			MemoryAbility: string(memsqlite.MemoryAbilityPremiseCheck),
			EvidenceNeed:  string(memsqlite.EvidenceNeedPremiseCounterexample),
			Signals: []string{
				string(memsqlite.QuerySignalPremiseCheck),
				string(memsqlite.QuerySignalPremiseCounterexample),
			},
			Confidence: 0.99,
			FieldConfidence: QueryAnalysisConfidence{
				Overall:       0.99,
				TimeMode:      0.99,
				MemoryAbility: 0.99,
				MemoryDomain:  0.99,
				EvidenceNeed:  0.99,
			},
		},
	}

	got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{MinConfidenceToOverride: 0.72}, nil)

	if got.TimeMode != memsqlite.QueryTimeModeCurrent {
		t.Fatalf("time_mode = %q, want current direct fact routing", got.TimeMode)
	}
	if got.MemoryAbility != memsqlite.MemoryAbilityDirectFact {
		t.Fatalf("memory_ability = %q, want direct_fact", got.MemoryAbility)
	}
	if got.EvidenceNeed != memsqlite.EvidenceNeedExactObservation {
		t.Fatalf("evidence_need = %q, want exact_observation", got.EvidenceNeed)
	}
	for _, reject := range []memsqlite.QuerySignal{memsqlite.QuerySignalPremiseCheck, memsqlite.QuerySignalPremiseCounterexample} {
		if hasStoreSignal(got.Signals, reject) {
			t.Fatalf("signals = %#v, should not include semantic-only %q", got.Signals, reject)
		}
	}
	if len(got.ContextBlockHints) != 0 {
		t.Fatalf("context block hints = %#v, want no premise_check_memory for direct fact", got.ContextBlockHints)
	}
}

func TestQueryAnalysisSemanticMergeAllowsPremiseWhenRuleSupportsPremise(t *testing.T) {
	tests := []string{
		"我的人际关系是不是很糟糕，跟身边每个朋友都闹过矛盾？",
		"小李上次跟我吵架之后是不是老样子，完全没有任何改变？",
		"如果 episode 被 redacted，是否还能暴露原文内容",
	}

	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			rule := memsqlite.QueryAnalysis{
				Raw:           query,
				Normalized:    strings.ToLower(query),
				TimeMode:      memsqlite.QueryTimeModeBitemporalCheck,
				MemoryDomain:  memsqlite.MemoryDomainRelationship,
				MemoryAbility: memsqlite.MemoryAbilityPremiseCheck,
				EvidenceNeed:  memsqlite.EvidenceNeedPremiseCounterexample,
				Signals: []memsqlite.QuerySignal{
					memsqlite.QuerySignalPremiseCounterexample,
					memsqlite.QuerySignalPremiseCheck,
				},
				Source:     memsqlite.QueryAnalysisSourceRuleOnly,
				Confidence: 0.78,
			}
			semantic := SemanticQueryAnalysisResult{
				Status: "ok",
				Analysis: SemanticQueryAnalysis{
					TimeMode:      string(memsqlite.QueryTimeModeBitemporalCheck),
					MemoryDomain:  string(memsqlite.MemoryDomainRelationship),
					MemoryAbility: string(memsqlite.MemoryAbilityPremiseCheck),
					EvidenceNeed:  string(memsqlite.EvidenceNeedPremiseCounterexample),
					Signals: []string{
						string(memsqlite.QuerySignalPremiseCounterexample),
						string(memsqlite.QuerySignalPremiseCheck),
					},
					Confidence: 0.95,
					FieldConfidence: QueryAnalysisConfidence{
						Overall:       0.95,
						TimeMode:      0.95,
						MemoryAbility: 0.95,
						MemoryDomain:  0.95,
						EvidenceNeed:  0.95,
					},
				},
			}

			got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{MinConfidenceToOverride: 0.72}, nil)

			if got.MemoryAbility != memsqlite.MemoryAbilityPremiseCheck {
				t.Fatalf("memory_ability = %q, want premise_check", got.MemoryAbility)
			}
			if got.EvidenceNeed != memsqlite.EvidenceNeedPremiseCounterexample {
				t.Fatalf("evidence_need = %q, want premise_counterexample", got.EvidenceNeed)
			}
			for _, want := range []memsqlite.QuerySignal{memsqlite.QuerySignalPremiseCheck, memsqlite.QuerySignalPremiseCounterexample} {
				if !hasStoreSignal(got.Signals, want) {
					t.Fatalf("signals = %#v, missing %q", got.Signals, want)
				}
			}
			if len(got.ContextBlockHints) != 1 || got.ContextBlockHints[0] != MemoryBlockTypePremiseCheckMemory {
				t.Fatalf("context block hints = %#v, want premise_check_memory", got.ContextBlockHints)
			}
		})
	}
}

func TestPrimaryContextBlockHintRequiresPreciseSignalsForSoftGatedBlocks(t *testing.T) {
	tests := []struct {
		name  string
		query memsqlite.QueryAnalysis
		want  []string
	}{
		{
			name:  "causal ability alone",
			query: memsqlite.QueryAnalysis{MemoryAbility: memsqlite.MemoryAbilityCausalExplain},
		},
		{
			name:  "provenance ability alone",
			query: memsqlite.QueryAnalysis{MemoryAbility: memsqlite.MemoryAbilityProvenance},
		},
		{
			name:  "historical ability alone",
			query: memsqlite.QueryAnalysis{MemoryAbility: memsqlite.MemoryAbilityHistorical},
		},
		{
			name:  "historical time alone",
			query: memsqlite.QueryAnalysis{TimeMode: memsqlite.QueryTimeModeHistorical},
		},
		{
			name:  "causal signal",
			query: memsqlite.QueryAnalysis{Signals: []memsqlite.QuerySignal{memsqlite.QuerySignalCausal}},
			want:  []string{MemoryBlockTypeRelevantCausalMemory},
		},
		{
			name:  "provenance source",
			query: memsqlite.QueryAnalysis{EvidenceNeed: memsqlite.EvidenceNeedProvenanceSource},
			want:  []string{MemoryBlockTypeProvenanceMemory},
		},
		{
			name:  "state transition",
			query: memsqlite.QueryAnalysis{Signals: []memsqlite.QuerySignal{memsqlite.QuerySignalStateTransition}},
			want:  []string{MemoryBlockTypeHistoricalTransitionMemory},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := primaryContextBlockHint(tt.query)
			if len(got) != len(tt.want) {
				t.Fatalf("primaryContextBlockHint() = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("primaryContextBlockHint() = %#v, want %#v", got, tt.want)
				}
			}
		})
	}
}

func TestQueryAnalysisSemanticRequestCarriesBudgetFields(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Confidence:    0.1,
	}
	semantic := &capturingSemanticQueryAnalyzer{
		result: &SemanticQueryAnalysisResult{
			Status: "ok",
			Analysis: SemanticQueryAnalysis{
				QueryRewrites: []QueryRewrite{{Text: "咖啡偏好", Weight: 0.5}},
			},
		},
	}
	pipeline := newQueryAnalysisPipeline(staticRuleQueryAnalyzer{analysis: rule}, semantic, QueryAnalysisOptions{
		Provider: QueryAnalysisProviderSidecar,
		Mode:     QueryAnalysisModeSemanticAlways,
		Timeout:  1200 * time.Millisecond,
	})
	if _, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{PersonaID: "default", QueryText: "咖啡", Now: fixedQueryAnalysisNow()}); err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if semantic.request.DeadlineMS != 1200 || semantic.request.ProviderTimeoutMS != 1200 {
		t.Fatalf("budget fields = deadline:%d provider:%d, want 1200/1200", semantic.request.DeadlineMS, semantic.request.ProviderTimeoutMS)
	}
}

func TestQueryAnalysisSemanticRequestCapsBudgetToSoftJoinTimeout(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Confidence:    0.1,
	}
	semantic := &capturingSemanticQueryAnalyzer{
		result: &SemanticQueryAnalysisResult{
			Status: "ok",
			Analysis: SemanticQueryAnalysis{
				QueryRewrites: []QueryRewrite{{Text: "咖啡偏好", Weight: 0.5}},
			},
		},
	}
	pipeline := newQueryAnalysisPipeline(staticRuleQueryAnalyzer{analysis: rule}, semantic, QueryAnalysisOptions{
		Provider:        QueryAnalysisProviderSidecar,
		Mode:            QueryAnalysisModeSemanticAlways,
		Timeout:         5 * time.Second,
		SoftJoinTimeout: 800 * time.Millisecond,
	})
	if _, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{PersonaID: "default", QueryText: "咖啡", Now: fixedQueryAnalysisNow()}); err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if semantic.request.DeadlineMS != 800 || semantic.request.ProviderTimeoutMS != 800 {
		t.Fatalf("budget fields = deadline:%d provider:%d, want 800/800", semantic.request.DeadlineMS, semantic.request.ProviderTimeoutMS)
	}
}

func TestQueryAnalysisInvalidSemanticDoesNotDowngradeRuleForgetDelete(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "忘掉这条记忆",
		Normalized:    "忘掉这条记忆",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainRelationship,
		MemoryAbility: memsqlite.MemoryAbilityBoundary,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Signals:       []memsqlite.QuerySignal{memsqlite.QuerySignalForgetDelete},
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.78,
	}
	semantic := SemanticQueryAnalysisResult{
		Status: "ok",
		Analysis: SemanticQueryAnalysis{
			TimeMode:      "future",
			MemoryDomain:  "unsafe_domain",
			MemoryAbility: string(memsqlite.MemoryAbilityDirectFact),
			EvidenceNeed:  string(memsqlite.EvidenceNeedExactObservation),
			Signals:       []string{},
			Confidence:    0.95,
			FieldConfidence: QueryAnalysisConfidence{
				Overall:       0.95,
				TimeMode:      0.95,
				MemoryAbility: 0.95,
				MemoryDomain:  0.95,
				EvidenceNeed:  0.95,
			},
		},
	}

	got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{MinConfidenceToOverride: 0.72}, nil)
	if got.TimeMode != rule.TimeMode || got.MemoryDomain != rule.MemoryDomain {
		t.Fatalf("invalid enum fields changed rule analysis: %#v", got)
	}
	if got.MemoryAbility != memsqlite.MemoryAbilityBoundary {
		t.Fatalf("memory ability = %q, semantic must not downgrade forget_delete boundary", got.MemoryAbility)
	}
	if !hasStoreSignal(got.Signals, memsqlite.QuerySignalForgetDelete) {
		t.Fatalf("signals = %#v, want forget_delete preserved", got.Signals)
	}
}

func TestQueryAnalysisForgetDeleteProtectsBoundaryFromAnySemanticAbilityDowngrade(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "忘掉这条记忆",
		Normalized:    "忘掉这条记忆",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainRelationship,
		MemoryAbility: memsqlite.MemoryAbilityBoundary,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Signals:       []memsqlite.QuerySignal{memsqlite.QuerySignalForgetDelete},
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.78,
	}
	for _, ability := range []memsqlite.MemoryAbility{
		memsqlite.MemoryAbilitySupportive,
		memsqlite.MemoryAbilityPlanning,
	} {
		t.Run(string(ability), func(t *testing.T) {
			semantic := SemanticQueryAnalysisResult{
				Status: "ok",
				Analysis: SemanticQueryAnalysis{
					MemoryAbility: string(ability),
					Signals:       []string{},
					Confidence:    0.95,
					FieldConfidence: QueryAnalysisConfidence{
						Overall:       0.95,
						MemoryAbility: 0.95,
					},
				},
			}

			got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{MinConfidenceToOverride: 0.72}, nil)
			if got.MemoryAbility != memsqlite.MemoryAbilityBoundary {
				t.Fatalf("memory ability = %q, semantic must not downgrade forget_delete boundary to %q", got.MemoryAbility, ability)
			}
			if !hasStoreSignal(got.Signals, memsqlite.QuerySignalForgetDelete) {
				t.Fatalf("signals = %#v, want forget_delete preserved", got.Signals)
			}
		})
	}
}

func TestQueryAnalysisSemanticFailureFallsBackToRule(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
	}
	pipeline := newQueryAnalysisPipeline(
		staticRuleQueryAnalyzer{analysis: rule},
		errorSemanticQueryAnalyzer{err: errors.New("sidecar down")},
		QueryAnalysisOptions{Provider: QueryAnalysisProviderSidecar, Mode: QueryAnalysisModeSemanticAlways},
	)

	got, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Now:       fixedQueryAnalysisNow(),
	})
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if got.Source != memsqlite.QueryAnalysisSourceSemanticFallback {
		t.Fatalf("source = %q, want semantic_failed_rule_fallback", got.Source)
	}
	if got.Diagnostics == nil || got.Diagnostics.SemanticStatus != "failed" || got.Diagnostics.FallbackReason == "" {
		t.Fatalf("diagnostics = %#v, want failed semantic diagnostics", got.Diagnostics)
	}
}

func TestQueryAnalysisSemanticFailurePreservesStateTransitionRuleFallback(t *testing.T) {
	raw := "我一开始把AI助手当成什么？后来这种看法发生了什么变化？"
	rule := memsqlite.QueryAnalysis{
		Raw:           raw,
		Normalized:    "我一开始把ai助手当成什么？后来这种看法发生了什么变化？",
		TimeMode:      memsqlite.QueryTimeModeHistorical,
		MemoryDomain:  memsqlite.MemoryDomainRelationship,
		MemoryAbility: memsqlite.MemoryAbilityHistorical,
		EvidenceNeed:  memsqlite.EvidenceNeedStateTransition,
		Signals:       []memsqlite.QuerySignal{memsqlite.QuerySignalHistorical},
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.78,
	}
	pipeline := newQueryAnalysisPipeline(
		staticRuleQueryAnalyzer{analysis: rule},
		errorSemanticQueryAnalyzer{err: errors.New("sidecar down")},
		QueryAnalysisOptions{Provider: QueryAnalysisProviderSidecar, Mode: QueryAnalysisModeSemanticAlways},
	)

	got, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{
		PersonaID: "default",
		QueryText: raw,
		Now:       fixedQueryAnalysisNow(),
	})
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if got.Source != memsqlite.QueryAnalysisSourceSemanticFallback {
		t.Fatalf("source = %q, want semantic_failed_rule_fallback", got.Source)
	}
	if got.TimeMode != memsqlite.QueryTimeModeHistorical {
		t.Fatalf("time_mode = %q, want historical", got.TimeMode)
	}
	if got.EvidenceNeed != memsqlite.EvidenceNeedStateTransition {
		t.Fatalf("evidence_need = %q, want state_transition", got.EvidenceNeed)
	}
	if got.MemoryAbility != memsqlite.MemoryAbilityHistorical && got.MemoryAbility != memsqlite.MemoryAbilityRelationshipArc {
		t.Fatalf("memory_ability = %q, want historical or relationship_arc", got.MemoryAbility)
	}
	if got.Diagnostics == nil || got.Diagnostics.SemanticStatus != "failed" || got.Diagnostics.FallbackReason == "" {
		t.Fatalf("diagnostics = %#v, want failed semantic diagnostics with fallback reason", got.Diagnostics)
	}
}

func TestQueryAnalysisParentCancellationUsesGoContextTimeoutReason(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
	}
	pipeline := newQueryAnalysisPipeline(
		staticRuleQueryAnalyzer{analysis: rule},
		errorSemanticQueryAnalyzer{err: context.Canceled},
		QueryAnalysisOptions{Provider: QueryAnalysisProviderSidecar, Mode: QueryAnalysisModeSemanticAlways},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := pipeline.AnalyzeQuery(ctx, QueryAnalysisRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Now:       fixedQueryAnalysisNow(),
	})
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if got.Diagnostics == nil || got.Diagnostics.FallbackReason != "go_context_timeout" {
		t.Fatalf("diagnostics = %#v, want go_context_timeout", got.Diagnostics)
	}
}

func TestQueryAnalysisSidecarFailureFallbackReasonDoesNotLeakBodyText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "SECRET user private note: do not leak", http.StatusInternalServerError)
	}))
	defer server.Close()

	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
	}
	pipeline := newQueryAnalysisPipeline(
		staticRuleQueryAnalyzer{analysis: rule},
		sidecarSemanticQueryAnalyzer{client: internalmirror.NewSidecarClient(internalmirror.SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})},
		QueryAnalysisOptions{Provider: QueryAnalysisProviderSidecar, Mode: QueryAnalysisModeSemanticAlways},
	)

	got, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Now:       fixedQueryAnalysisNow(),
	})
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if got.Diagnostics == nil {
		t.Fatal("diagnostics is nil")
	}
	if got.Diagnostics.FallbackReason != "semantic_sidecar_error" {
		t.Fatalf("fallback reason = %q, want bounded semantic_sidecar_error", got.Diagnostics.FallbackReason)
	}
	if strings.Contains(got.Diagnostics.FallbackReason, "SECRET") || strings.Contains(got.Diagnostics.FallbackReason, "private note") {
		t.Fatalf("fallback reason leaked sidecar body: %q", got.Diagnostics.FallbackReason)
	}
}

func TestQueryAnalysisSemanticFallbackPreservesSafeSidecarReason(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
	}
	for _, reason := range []string{"invalid_json", "validation_failed", "provider_error", "provider_timeout", "provider_budget_exhausted", "sidecar_provider_timeout"} {
		t.Run(reason, func(t *testing.T) {
			pipeline := newQueryAnalysisPipeline(
				staticRuleQueryAnalyzer{analysis: rule},
				staticSemanticQueryAnalyzer{result: &SemanticQueryAnalysisResult{
					Status:         "degraded",
					Degraded:       true,
					FallbackReason: reason,
					Provider:       "openai-compatible",
					Model:          "test-model",
				}},
				QueryAnalysisOptions{Provider: QueryAnalysisProviderSidecar, Mode: QueryAnalysisModeSemanticAlways},
			)

			got, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{
				PersonaID: "default",
				QueryText: "咖啡",
				Now:       fixedQueryAnalysisNow(),
			})
			if err != nil {
				t.Fatalf("analyze query: %v", err)
			}
			if got.Source != memsqlite.QueryAnalysisSourceSemanticFallback || got.Diagnostics == nil {
				t.Fatalf("analysis = %#v, want semantic fallback diagnostics", got)
			}
			if got.Diagnostics.FallbackReason != reason {
				t.Fatalf("fallback reason = %q, want %q", got.Diagnostics.FallbackReason, reason)
			}
		})
	}
}

func TestQueryAnalysisGeneratedWeightsRespectCumulativeCaps(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
	}
	semantic := SemanticQueryAnalysisResult{
		Status: "ok",
		Analysis: SemanticQueryAnalysis{
			QueryRewrites: []QueryRewrite{
				{Text: "rewrite one", Purpose: "dense", Weight: 0.9},
				{Text: "rewrite two", Purpose: "dense", Weight: 0.9},
			},
			SemanticAnchors: []SemanticAnchor{
				{Text: "anchor one", AnchorType: "semantic", Weight: 0.65, Confidence: 0.9},
				{Text: "anchor two", AnchorType: "semantic", Weight: 0.65, Confidence: 0.9},
			},
		},
	}

	got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{
		MaxQueryRewrites:           5,
		MaxSemanticAnchors:         5,
		MaxGeneratedDenseWeightSum: 1.0,
		SemanticTotalEnergyCap:     2.0,
	}, nil)

	var total float64
	for _, rewrite := range got.QueryRewrites {
		total += rewrite.Weight
	}
	for _, anchor := range got.SemanticAnchors {
		total += anchor.Weight
	}
	if total > 1.0 {
		t.Fatalf("generated weight total = %v, want <= 1.0; rewrites=%#v anchors=%#v", total, got.QueryRewrites, got.SemanticAnchors)
	}
	if len(got.QueryRewrites) != 2 || got.QueryRewrites[0].Weight != 0.9 || got.QueryRewrites[1].Weight != 0.1 {
		t.Fatalf("query rewrites = %#v, want first full and second trimmed to remaining cap", got.QueryRewrites)
	}
	if len(got.SemanticAnchors) != 0 {
		t.Fatalf("semantic anchors = %#v, want trimmed after rewrite cap exhausted", got.SemanticAnchors)
	}
}

func TestQueryAnalysisDropsLongEnglishRewriteForChineseQuery(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "我喜欢Laufey这件事是从哪里知道的？",
		Normalized:    "我喜欢laufey这件事是从哪里知道的？",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityProvenance,
		EvidenceNeed:  memsqlite.EvidenceNeedProvenanceSource,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.78,
	}
	semantic := SemanticQueryAnalysisResult{
		Status: "ok",
		Analysis: SemanticQueryAnalysis{
			QueryRewrites: []QueryRewrite{
				{Text: "when did the user say they like Laufey", Purpose: "semantic_recall", Weight: 0.7},
				{Text: "用户喜欢Laufey的来源", Purpose: "semantic_recall", Weight: 0.6},
				{Text: "Laufey", Purpose: "entity_anchor", Weight: 0.5},
			},
		},
	}

	got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{
		MaxQueryRewrites:           5,
		MaxGeneratedDenseWeightSum: 1.5,
		SemanticTotalEnergyCap:     3,
	}, nil)

	if len(got.QueryRewrites) != 2 {
		t.Fatalf("query rewrites = %#v, want long English rewrite dropped and two rewrites retained", got.QueryRewrites)
	}
	if got.QueryRewrites[0].Text != "用户喜欢Laufey的来源" || got.QueryRewrites[1].Text != "Laufey" {
		t.Fatalf("query rewrites = %#v, want Chinese rewrite and short proper noun retained", got.QueryRewrites)
	}
	if got.Diagnostics == nil {
		t.Fatal("diagnostics = nil")
	}
	if got.Diagnostics.DroppedRewriteCount != 1 {
		t.Fatalf("dropped rewrite count = %d, want 1", got.Diagnostics.DroppedRewriteCount)
	}
	if got.Diagnostics.EnglishRewriteCount != 2 {
		t.Fatalf("English rewrite count = %d, want 2", got.Diagnostics.EnglishRewriteCount)
	}
	if len(got.Diagnostics.DroppedRewriteReasons) != 1 || got.Diagnostics.DroppedRewriteReasons[0] != "rewrite_language_mismatch" {
		t.Fatalf("dropped rewrite reasons = %#v, want rewrite_language_mismatch", got.Diagnostics.DroppedRewriteReasons)
	}
}

func TestQueryAnalysisClampsHistoricalStateTransitionAfterSemanticMerge(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "我一开始把AI助手当成什么？后来这种看法发生了什么变化？",
		Normalized:    "我一开始把ai助手当成什么？后来这种看法发生了什么变化？",
		TimeMode:      memsqlite.QueryTimeModeHistorical,
		MemoryDomain:  memsqlite.MemoryDomainRelationship,
		MemoryAbility: memsqlite.MemoryAbilityHistorical,
		EvidenceNeed:  memsqlite.EvidenceNeedStateTransition,
		Signals:       []memsqlite.QuerySignal{memsqlite.QuerySignalHistorical},
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.78,
	}
	semantic := SemanticQueryAnalysisResult{
		Status: "ok",
		Analysis: SemanticQueryAnalysis{
			TimeMode:      string(memsqlite.QueryTimeModeCurrent),
			MemoryAbility: string(memsqlite.MemoryAbilityDynamicState),
			EvidenceNeed:  string(memsqlite.EvidenceNeedStateTransition),
			Confidence:    0.95,
			FieldConfidence: QueryAnalysisConfidence{
				Overall:       0.95,
				TimeMode:      0.95,
				MemoryAbility: 0.95,
				EvidenceNeed:  0.95,
			},
		},
	}

	got := mergeSemanticQueryAnalysis(rule, semantic, QueryAnalysisOptions{MinConfidenceToOverride: 0.72}, nil)

	if got.TimeMode != memsqlite.QueryTimeModeHistorical {
		t.Fatalf("time_mode = %q, want historical", got.TimeMode)
	}
	if got.EvidenceNeed != memsqlite.EvidenceNeedStateTransition {
		t.Fatalf("evidence_need = %q, want state_transition", got.EvidenceNeed)
	}
	if got.MemoryAbility != memsqlite.MemoryAbilityHistorical && got.MemoryAbility != memsqlite.MemoryAbilityRelationshipArc {
		t.Fatalf("memory_ability = %q, want historical or relationship_arc", got.MemoryAbility)
	}
}

func TestQueryAnalysisPipelineTreatsMissingStatusAsFallback(t *testing.T) {
	rule := memsqlite.QueryAnalysis{
		Raw:           "咖啡",
		Normalized:    "咖啡",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
		Confidence:    0.42,
	}
	pipeline := newQueryAnalysisPipeline(
		staticRuleQueryAnalyzer{analysis: rule},
		staticSemanticQueryAnalyzer{result: &SemanticQueryAnalysisResult{}},
		QueryAnalysisOptions{Provider: QueryAnalysisProviderSidecar, Mode: QueryAnalysisModeSemanticAlways},
	)

	got, err := pipeline.AnalyzeQuery(context.Background(), QueryAnalysisRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Now:       fixedQueryAnalysisNow(),
	})
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if got.Source != memsqlite.QueryAnalysisSourceSemanticFallback || got.Diagnostics == nil || got.Diagnostics.FallbackReason != "semantic_protocol_error" {
		t.Fatalf("analysis = %#v, want degraded rule fallback for missing semantic status", got)
	}
}

type staticRuleQueryAnalyzer struct {
	analysis memsqlite.QueryAnalysis
}

func (a staticRuleQueryAnalyzer) AnalyzeRuleQuery(context.Context, QueryAnalysisRequest) (memsqlite.QueryAnalysis, error) {
	return a.analysis, nil
}

type panicSemanticQueryAnalyzer struct{}

func (panicSemanticQueryAnalyzer) AnalyzeSemanticQuery(context.Context, SemanticQueryAnalysisRequest) (*SemanticQueryAnalysisResult, error) {
	panic("semantic analyzer should not be called")
}

type errorSemanticQueryAnalyzer struct {
	err error
}

func (a errorSemanticQueryAnalyzer) AnalyzeSemanticQuery(context.Context, SemanticQueryAnalysisRequest) (*SemanticQueryAnalysisResult, error) {
	return nil, a.err
}

type staticSemanticQueryAnalyzer struct {
	result *SemanticQueryAnalysisResult
}

func (a staticSemanticQueryAnalyzer) AnalyzeSemanticQuery(context.Context, SemanticQueryAnalysisRequest) (*SemanticQueryAnalysisResult, error) {
	if a.result == nil {
		return nil, nil
	}
	cloned := *a.result
	raw, _ := json.Marshal(a.result.Analysis)
	_ = json.Unmarshal(raw, &cloned.Analysis)
	return &cloned, nil
}

type capturingSemanticQueryAnalyzer struct {
	request SemanticQueryAnalysisRequest
	result  *SemanticQueryAnalysisResult
}

func (a *capturingSemanticQueryAnalyzer) AnalyzeSemanticQuery(_ context.Context, req SemanticQueryAnalysisRequest) (*SemanticQueryAnalysisResult, error) {
	a.request = req
	if a.result == nil {
		return nil, nil
	}
	cloned := *a.result
	raw, _ := json.Marshal(a.result.Analysis)
	_ = json.Unmarshal(raw, &cloned.Analysis)
	return &cloned, nil
}

func fixedQueryAnalysisNow() time.Time {
	return time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC)
}

func hasStoreSignal(signals []memsqlite.QuerySignal, want memsqlite.QuerySignal) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}
