package sqlite

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

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

func TestQueryAnalysisSupportsPhase1DiagnosticsDTO(t *testing.T) {
	analysis := QueryAnalysis{
		Confidence: 0.66,
		Scores: QueryAnalysisScores{
			RuleFit:                     0.61,
			AnchorReadiness:             0.42,
			ExpectedRetrievalConfidence: 0.55,
			SemanticNeed:                0.73,
			Complexity:                  0.64,
			Ambiguity:                   0.31,
			Specificity:                 0.82,
			SafetyRisk:                  0.12,
			IntentEvidence:              0.79,
			TimeEvidence:                0.58,
			DomainEvidence:              0.67,
			EvidenceNeedEvidence:        0.69,
			EntityResolution:            0.44,
			FieldConsistency:            0.71,
			DefaultFallbackPenalty:      0.13,
			MultiIntentConflictPenalty:  0.08,
			SensitivityPenalty:          0.03,
		},
		Probes: QueryAnchorProbe{
			EntityExactConf:        0.85,
			EntityAmbiguity:        0.20,
			SparseProbeConf:        0.62,
			PredicateProbeConf:     0.58,
			RecentProbeConf:        0.34,
			PinnedCoreProbeConf:    0.27,
			NarrativeProbeConf:     0.39,
			FallbackSearchHitCount: 4,
			Top1Score:              0.91,
			Top2Score:              0.77,
			Top1Margin:             0.14,
			Breakdown: []QueryAnchorProbeBreakdown{{
				Source:      "sparse_probe",
				Confidence:  0.62,
				HitCount:    4,
				TopScore:    0.91,
				SecondScore: 0.77,
				Reason:      "sqlite search document match",
			}},
		},
		Decision: QueryAnalysisDecision{
			UseSemantic:      true,
			SemanticMode:     "decompose",
			RetrievalMode:    "graph_contextual",
			ReasonCodes:      []string{"causal_intent", "weak_anchor"},
			ThresholdVersion: "semantic_router_v1",
			ScorerVersion:    "query_analysis_scorer_v1",
		},
		Evidence: []QueryAnalysisEvidence{{
			Field:     "memory_ability",
			Signal:    "causal_word",
			MatchText: "为什么",
			SpanStart: 0,
			SpanEnd:   6,
			Weight:    0.38,
			Detector:  "rule_regex_v1",
		}},
		Alternatives: []QueryAnalysisAlternative{{
			Field:       "time_mode",
			Value:       string(QueryTimeModeHistorical),
			Confidence:  0.41,
			ReasonCodes: []string{"historical_phrase"},
			Detector:    "rule_regex_v1",
		}},
		Diagnostics: &QueryAnalysisDiagnostics{
			SemanticAnalysis: &SemanticQueryAnalysisDiagnostics{
				Scores: QueryAnalysisScores{
					SemanticNeed: 0.77,
				},
				Probes: QueryAnchorProbe{
					SparseProbeConf: 0.52,
					Breakdown: []QueryAnchorProbeBreakdown{{
						Source:     "sparse_probe",
						Confidence: 0.52,
						HitCount:   2,
						Reason:     "semantic returned probe snapshot",
					}},
				},
				Decision: QueryAnalysisDecision{
					UseSemantic:  true,
					SemanticMode: "light",
					ReasonCodes:  []string{"semantic_need_high"},
				},
				Evidence: []QueryAnalysisEvidence{{Field: "evidence_need", Signal: "why"}},
				Alternatives: []QueryAnalysisAlternative{{
					Field:       "memory_ability",
					Value:       string(MemoryAbilityDirectFact),
					Confidence:  0.22,
					ReasonCodes: []string{"fallback"},
				}},
			},
		},
	}

	if analysis.Scores.RuleFit != 0.61 ||
		analysis.Probes.Top1Margin != 0.14 ||
		analysis.Probes.Breakdown[0].Source != "sparse_probe" ||
		!analysis.Decision.UseSemantic ||
		analysis.Decision.ReasonCodes[1] != "weak_anchor" ||
		analysis.Evidence[0].Detector != "rule_regex_v1" ||
		analysis.Alternatives[0].ReasonCodes[0] != "historical_phrase" ||
		analysis.Diagnostics.SemanticAnalysis.Decision.SemanticMode != "light" ||
		analysis.Diagnostics.SemanticAnalysis.Probes.Breakdown[0].Source != "sparse_probe" ||
		analysis.Diagnostics.SemanticAnalysis.Alternatives[0].Value != string(MemoryAbilityDirectFact) {
		t.Fatalf("phase 1 DTO fields not retained: %#v", analysis)
	}
}

func TestAnalyzeQueryPopulatesEntityAnchorProbeExactAliasAndAmbiguous(t *testing.T) {
	ctx := context.Background()
	db := openQueryProbeDB(t, ctx, true)
	defer db.Close()
	repo := NewRetrievalRepository(db.SQLDB(), nil, nil)
	policy := RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal), UseFTS: true}

	insertProbeEntity(t, ctx, db.SQLDB(), "ent_user", "Long")
	insertProbeEntity(t, ctx, db.SQLDB(), "ent_lilei", "李雷")
	insertProbeEntity(t, ctx, db.SQLDB(), "ent_other_lilei", "李雷2")
	insertProbeAlias(t, ctx, db.SQLDB(), "alias_lilei_unique", "ent_lilei", "小雷")
	insertProbeAlias(t, ctx, db.SQLDB(), "alias_lilei", "ent_lilei", "小李")
	insertProbeAlias(t, ctx, db.SQLDB(), "alias_other_lilei", "ent_other_lilei", "小李")

	t.Run("canonical exact", func(t *testing.T) {
		got, err := repo.AnalyzeQuery(ctx, "default", "Long 喜欢咖啡吗", policy)
		if err != nil {
			t.Fatalf("analyze query: %v", err)
		}
		if got.Probes.EntityExactConf != 0.95 {
			t.Fatalf("entity exact confidence = %v, want 0.95", got.Probes.EntityExactConf)
		}
		requireProbeBreakdown(t, got.Probes, "entity_exact", 0.95, "")
	})

	t.Run("alias", func(t *testing.T) {
		got, err := repo.AnalyzeQuery(ctx, "default", "小雷最近怎么样", policy)
		if err != nil {
			t.Fatalf("analyze query: %v", err)
		}
		if got.Probes.EntityExactConf != 0.85 {
			t.Fatalf("alias entity confidence = %v, want 0.85", got.Probes.EntityExactConf)
		}
		if got.Probes.EntityAmbiguity != 0 {
			t.Fatalf("entity ambiguity = %v, want 0", got.Probes.EntityAmbiguity)
		}
		requireProbeBreakdown(t, got.Probes, "entity_exact", 0.85, "")
	})

	t.Run("alias ambiguous", func(t *testing.T) {
		got, err := repo.AnalyzeQuery(ctx, "default", "小李最近怎么样", policy)
		if err != nil {
			t.Fatalf("analyze query: %v", err)
		}
		if got.Probes.EntityExactConf != 0.65 {
			t.Fatalf("ambiguous entity confidence = %v, want 0.65", got.Probes.EntityExactConf)
		}
		if got.Probes.EntityAmbiguity <= 0.6 {
			t.Fatalf("entity ambiguity = %v, want > 0.6", got.Probes.EntityAmbiguity)
		}
		requireProbeBreakdown(t, got.Probes, "entity_exact", 0.65, "")
	})
}

func TestAnalyzeQueryPopulatesRuleExplanationArtifacts(t *testing.T) {
	ctx := context.Background()
	db := openQueryProbeDB(t, ctx, true)
	defer db.Close()
	repo := NewRetrievalRepository(db.SQLDB(), nil, nil)
	policy := RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal), UseFTS: true}

	got, err := repo.AnalyzeQuery(ctx, "default", "为什么最近状态抗拒上班", policy)
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if got.Decision.RetrievalMode != "graph_contextual" {
		t.Fatalf("retrieval mode = %q, want graph_contextual", got.Decision.RetrievalMode)
	}
	if got.Decision.UseSemantic {
		t.Fatalf("rule decision use semantic = true, want false until adaptive routing phase")
	}
	if !containsString(got.Decision.ReasonCodes, "causal_intent") ||
		!containsString(got.Decision.ReasonCodes, "weak_anchor") {
		t.Fatalf("decision reason codes = %#v, want causal_intent and weak_anchor", got.Decision.ReasonCodes)
	}
	if !hasEvidenceForField(got.Evidence, "memory_ability") ||
		!hasEvidenceForField(got.Evidence, "time_mode") ||
		!hasEvidenceForField(got.Evidence, "memory_domain") ||
		!hasEvidenceForField(got.Evidence, "evidence_need") {
		t.Fatalf("rule evidence = %#v, want field evidence for rule scores", got.Evidence)
	}
	if got.Diagnostics == nil {
		t.Fatal("diagnostics = nil")
	}
	if got.Diagnostics.RuleDecision.RetrievalMode != got.Decision.RetrievalMode ||
		len(got.Diagnostics.RuleEvidence) != len(got.Evidence) {
		t.Fatalf("diagnostics = %#v, want rule decision/evidence snapshot", got.Diagnostics)
	}
}

func TestAnalyzeQueryPopulatesRuleAlternativesForAmbiguousDirectFact(t *testing.T) {
	ctx := context.Background()
	db := openQueryProbeDB(t, ctx, true)
	defer db.Close()
	repo := NewRetrievalRepository(db.SQLDB(), nil, nil)
	policy := RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal), UseFTS: true}

	got, err := repo.AnalyzeQuery(ctx, "default", "这件事后来怎么样了", policy)
	if err != nil {
		t.Fatalf("analyze query: %v", err)
	}
	if len(got.Alternatives) == 0 {
		t.Fatalf("alternatives = nil, want ambiguity alternatives")
	}
	if !hasAlternative(got.Alternatives, "memory_ability", string(MemoryAbilityHistorical)) {
		t.Fatalf("alternatives = %#v, want historical memory ability alternative", got.Alternatives)
	}
	if got.Diagnostics == nil || !hasAlternative(got.Diagnostics.RuleAlternatives, "memory_ability", string(MemoryAbilityHistorical)) {
		t.Fatalf("diagnostics alternatives = %#v, want rule alternatives snapshot", got.Diagnostics)
	}
}

func TestAnalyzeQueryPopulatesSparsePredicateAndSupportProbeBreakdown(t *testing.T) {
	ctx := context.Background()
	db := openQueryProbeDB(t, ctx, true)
	defer db.Close()
	repo := NewRetrievalRepository(db.SQLDB(), nil, nil)
	policy := RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal), UseFTS: true}

	insertProbeEntity(t, ctx, db.SQLDB(), "ent_user", "Long")
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:         "fact_coffee_strong_1",
		predicate:  "likes",
		object:     "coffee",
		summary:    "user likes coffee morning ritual",
		factType:   core.FactTypeStablePreference,
		importance: 0.80,
	})
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:         "fact_coffee_strong_2",
		predicate:  "likes",
		object:     "coffee beans",
		summary:    "coffee morning preference is stable",
		factType:   core.FactTypeStablePreference,
		importance: 0.75,
	})
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:         "fact_coffee_strong_3",
		predicate:  "drinks",
		object:     "coffee",
		summary:    "coffee morning drink appears in notes",
		factType:   core.FactTypeStablePreference,
		importance: 0.70,
	})
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:         "fact_pinned_core",
		predicate:  "prefers_name",
		object:     "Long",
		summary:    "user prefers the name Long",
		factType:   core.FactTypeCoreIdentity,
		importance: 0.95,
		pinned:     true,
	})
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:         "fact_recent",
		predicate:  "felt",
		object:     "work resistance",
		summary:    "recent work resistance felt heavy",
		factType:   core.FactTypeTransientContext,
		importance: 0.90,
	})
	insertProbeNarrativeDocument(t, ctx, db.SQLDB(), "narrative_work", "recent work resistance pattern")
	rebuildProbeSearch(t, ctx, db.SQLDB())

	strong, err := repo.AnalyzeQuery(ctx, "default", "coffee morning", policy)
	if err != nil {
		t.Fatalf("analyze strong sparse query: %v", err)
	}
	if strong.Probes.SparseProbeConf != 0.75 {
		t.Fatalf("strong sparse confidence = %v, want 0.75", strong.Probes.SparseProbeConf)
	}
	if strong.Probes.FallbackSearchHitCount < 3 || strong.Probes.Top1Score < strong.Probes.Top2Score || strong.Probes.Top1Margin < 0 {
		t.Fatalf("strong sparse probe = %#v, want stable hit count and top scores", strong.Probes)
	}
	requireProbeBreakdown(t, strong.Probes, "sparse_probe", 0.75, "")

	weak, err := repo.AnalyzeQuery(ctx, "default", "beans", policy)
	if err != nil {
		t.Fatalf("analyze weak sparse query: %v", err)
	}
	if weak.Probes.SparseProbeConf != 0.40 {
		t.Fatalf("weak sparse confidence = %v, want 0.40", weak.Probes.SparseProbeConf)
	}

	predicate, err := repo.AnalyzeQuery(ctx, "default", "我喜欢什么", policy)
	if err != nil {
		t.Fatalf("analyze predicate query: %v", err)
	}
	if predicate.Probes.PredicateProbeConf < 0.70 {
		t.Fatalf("predicate confidence = %v, want >= 0.70", predicate.Probes.PredicateProbeConf)
	}
	requireProbeBreakdown(t, predicate.Probes, "predicate_probe", predicate.Probes.PredicateProbeConf, "")

	support, err := repo.AnalyzeQuery(ctx, "default", "为什么最近 work resistance", policy)
	if err != nil {
		t.Fatalf("analyze support query: %v", err)
	}
	if support.Probes.RecentProbeConf == 0 || support.Probes.NarrativeProbeConf == 0 {
		t.Fatalf("support probes = %#v, want recent and narrative confidence", support.Probes)
	}
	requireProbeBreakdown(t, support.Probes, "recent_probe", support.Probes.RecentProbeConf, "")
	requireProbeBreakdown(t, support.Probes, "narrative_probe", support.Probes.NarrativeProbeConf, "")

	pinned, err := repo.AnalyzeQuery(ctx, "default", "我的边界 Long", policy)
	if err != nil {
		t.Fatalf("analyze pinned query: %v", err)
	}
	if pinned.Probes.PinnedCoreProbeConf == 0 {
		t.Fatalf("pinned core confidence = %v, want non-zero", pinned.Probes.PinnedCoreProbeConf)
	}
	requireProbeBreakdown(t, pinned.Probes, "pinned_core_probe", pinned.Probes.PinnedCoreProbeConf, "")
}

func TestAnalyzeQueryAnchorProbesRespectAuthorityPolicy(t *testing.T) {
	ctx := context.Background()
	db := openQueryProbeDB(t, ctx, true)
	defer db.Close()
	repo := NewRetrievalRepository(db.SQLDB(), nil, nil)
	normalPolicy := RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal), UseFTS: true}
	sensitivePolicy := RetrievalPolicy{SensitivityPermission: string(core.SensitivitySensitive), UseFTS: true}

	insertProbeEntity(t, ctx, db.SQLDB(), "ent_user", "Long")
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:          "fact_sensitive_coffee_1",
		predicate:   "likes",
		object:      "coffee",
		summary:     "user likes coffee morning ritual",
		factType:    core.FactTypeStablePreference,
		importance:  0.90,
		sensitivity: core.SensitivitySensitive,
	})
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:          "fact_sensitive_coffee_2",
		predicate:   "likes",
		object:      "coffee beans",
		summary:     "coffee morning preference is stable",
		factType:    core.FactTypeStablePreference,
		importance:  0.85,
		sensitivity: core.SensitivitySensitive,
	})
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:          "fact_sensitive_coffee_3",
		predicate:   "drinks",
		object:      "coffee",
		summary:     "coffee morning drink appears in notes",
		factType:    core.FactTypeStablePreference,
		importance:  0.80,
		sensitivity: core.SensitivitySensitive,
	})
	insertProbeFact(t, ctx, db.SQLDB(), probeFact{
		id:          "fact_sensitive_pinned_core",
		predicate:   "prefers_name",
		object:      "Long",
		summary:     "sensitive pinned core identity note",
		factType:    core.FactTypeCoreIdentity,
		importance:  0.95,
		pinned:      true,
		sensitivity: core.SensitivitySensitive,
	})
	rebuildProbeSearch(t, ctx, db.SQLDB())

	normalSparse, err := repo.AnalyzeQuery(ctx, "default", "coffee morning", normalPolicy)
	if err != nil {
		t.Fatalf("analyze normal sparse query: %v", err)
	}
	if normalSparse.Probes.SparseProbeConf != 0 || normalSparse.Probes.PredicateProbeConf != 0 || normalSparse.Scores.AnchorReadiness != 0 {
		t.Fatalf("normal sparse probes = %#v scores=%#v, want no readiness from sensitive facts", normalSparse.Probes, normalSparse.Scores)
	}

	normalPredicate, err := repo.AnalyzeQuery(ctx, "default", "我喜欢什么", normalPolicy)
	if err != nil {
		t.Fatalf("analyze normal predicate query: %v", err)
	}
	if normalPredicate.Probes.PredicateProbeConf != 0 {
		t.Fatalf("normal predicate confidence = %v, want 0 from sensitive facts", normalPredicate.Probes.PredicateProbeConf)
	}

	normalRecent, err := repo.AnalyzeQuery(ctx, "default", "为什么 coffee morning", normalPolicy)
	if err != nil {
		t.Fatalf("analyze normal recent query: %v", err)
	}
	if normalRecent.Probes.SparseProbeConf != 0 || normalRecent.Probes.RecentProbeConf != 0 {
		t.Fatalf("normal recent probes = %#v, want no sparse/recent readiness from sensitive facts", normalRecent.Probes)
	}

	normalPinned, err := repo.AnalyzeQuery(ctx, "default", "我的边界是什么", normalPolicy)
	if err != nil {
		t.Fatalf("analyze normal pinned query: %v", err)
	}
	if normalPinned.Probes.PinnedCoreProbeConf != 0 {
		t.Fatalf("normal pinned confidence = %v, want 0 from sensitive facts", normalPinned.Probes.PinnedCoreProbeConf)
	}

	sensitiveSparse, err := repo.AnalyzeQuery(ctx, "default", "coffee morning", sensitivePolicy)
	if err != nil {
		t.Fatalf("analyze sensitive sparse query: %v", err)
	}
	if sensitiveSparse.Probes.SparseProbeConf != 0.75 {
		t.Fatalf("sensitive sparse confidence = %v, want 0.75", sensitiveSparse.Probes.SparseProbeConf)
	}

	sensitivePredicate, err := repo.AnalyzeQuery(ctx, "default", "我喜欢什么", sensitivePolicy)
	if err != nil {
		t.Fatalf("analyze sensitive predicate query: %v", err)
	}
	if sensitivePredicate.Probes.PredicateProbeConf < 0.70 {
		t.Fatalf("sensitive predicate confidence = %v, want >= 0.70", sensitivePredicate.Probes.PredicateProbeConf)
	}

	sensitiveRecent, err := repo.AnalyzeQuery(ctx, "default", "为什么 coffee morning", sensitivePolicy)
	if err != nil {
		t.Fatalf("analyze sensitive recent query: %v", err)
	}
	if sensitiveRecent.Probes.RecentProbeConf == 0 {
		t.Fatalf("sensitive recent confidence = %v, want non-zero", sensitiveRecent.Probes.RecentProbeConf)
	}

	sensitivePinned, err := repo.AnalyzeQuery(ctx, "default", "我的边界是什么", sensitivePolicy)
	if err != nil {
		t.Fatalf("analyze sensitive pinned query: %v", err)
	}
	if sensitivePinned.Probes.PinnedCoreProbeConf == 0 {
		t.Fatalf("sensitive pinned confidence = %v, want non-zero", sensitivePinned.Probes.PinnedCoreProbeConf)
	}
}

func TestComputeAnchorReadinessNoisyOrAndPenalties(t *testing.T) {
	base := QueryAnchorProbe{
		EntityExactConf:     0.50,
		SparseProbeConf:     0.40,
		PredicateProbeConf:  0.30,
		RecentProbeConf:     0.20,
		PinnedCoreProbeConf: 0.10,
		NarrativeProbeConf:  0.05,
	}
	want := 1 - (1-0.50)*(1-0.40)*(1-0.30)*(1-0.20)*(1-0.10)*(1-0.05)
	if got := ComputeAnchorReadiness(base); math.Abs(got-want) > 1e-9 {
		t.Fatalf("anchor readiness = %v, want noisy-or %v", got, want)
	}

	ambiguous := base
	ambiguous.EntityAmbiguity = 0.75
	if got := ComputeAnchorReadiness(ambiguous); math.Abs(got-(want*0.75)) > 1e-9 {
		t.Fatalf("ambiguous anchor readiness = %v, want %v", got, want*0.75)
	}

	lowMargin := base
	lowMargin.FallbackSearchHitCount = 6
	lowMargin.Top1Score = 0.60
	lowMargin.Top2Score = 0.56
	lowMargin.Top1Margin = 0.04
	if got := ComputeAnchorReadiness(lowMargin); math.Abs(got-(want*0.85)) > 1e-9 {
		t.Fatalf("low-margin anchor readiness = %v, want %v", got, want*0.85)
	}
}

func TestAnalyzeQueryProbeErrorFallsBackToRuleAnalysis(t *testing.T) {
	ctx := context.Background()
	db := openQueryProbeDB(t, ctx, true)
	defer db.Close()
	repo := NewRetrievalRepository(db.SQLDB(), nil, nil)
	if _, err := db.SQLDB().ExecContext(ctx, `DROP TABLE memory_search_documents`); err != nil {
		t.Fatalf("drop search documents: %v", err)
	}

	got, err := repo.AnalyzeQuery(ctx, "default", "咖啡", RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal), UseFTS: true})
	if err != nil {
		t.Fatalf("analyze query after probe error: %v", err)
	}
	if got.Raw != "咖啡" || got.MemoryAbility != MemoryAbilityDirectFact || got.Diagnostics == nil {
		t.Fatalf("fallback analysis = %#v, want rule analysis with diagnostics", got)
	}
	if len(got.Probes.Breakdown) == 0 {
		t.Fatalf("probe breakdown = %#v, want error entry", got.Probes.Breakdown)
	}
	hasError := false
	for _, item := range got.Probes.Breakdown {
		if item.Error != "" {
			if item.Status != "unknown" {
				t.Fatalf("probe breakdown status = %q, want unknown for error item %#v", item.Status, item)
			}
			hasError = true
			break
		}
	}
	if !hasError {
		t.Fatalf("probe breakdown = %#v, want sanitized error", got.Probes.Breakdown)
	}
	if got.Scores.AnchorReadiness != 0 {
		t.Fatalf("anchor readiness = %v, want 0 on probe failure", got.Scores.AnchorReadiness)
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
	if got := ruleConfidenceLegacy("咖啡", analysis).Score; got != 0.42 {
		t.Fatalf("legacy rule confidence for exact_fact-only plain query = %v, want 0.42", got)
	}
}

func TestRuleConfidenceLegacySnapshots(t *testing.T) {
	tests := []struct {
		name       string
		normalized string
		analysis   QueryAnalysis
		wantScore  float64
		wantReason string
	}{
		{
			name:       "empty query",
			normalized: "",
			analysis:   QueryAnalysis{},
			wantScore:  0,
			wantReason: "empty_query",
		},
		{
			name:       "non direct ability",
			normalized: "为什么我后来不喝咖啡了",
			analysis: QueryAnalysis{
				MemoryAbility: MemoryAbilityCausalExplain,
			},
			wantScore:  0.78,
			wantReason: "non_direct_memory_ability",
		},
		{
			name:       "entity mention",
			normalized: "longyi 喜欢咖啡吗",
			analysis: QueryAnalysis{
				MemoryAbility:  MemoryAbilityDirectFact,
				EntityMentions: []QueryEntityMention{{EntityID: "ent_user", MatchText: "longyi"}},
			},
			wantScore:  0.74,
			wantReason: "entity_mention",
		},
		{
			name:       "exact fact only",
			normalized: "咖啡",
			analysis: QueryAnalysis{
				MemoryAbility: MemoryAbilityDirectFact,
				Signals:       []QuerySignal{QuerySignalExactFact},
			},
			wantScore:  0.42,
			wantReason: "exact_fact_only",
		},
		{
			name:       "signal only",
			normalized: "什么时候说过咖啡",
			analysis: QueryAnalysis{
				MemoryAbility: MemoryAbilityDirectFact,
				Signals:       []QuerySignal{QuerySignalProvenanceSource},
			},
			wantScore:  0.68,
			wantReason: "query_signal",
		},
		{
			name:       "default direct fact",
			normalized: "咖啡",
			analysis: QueryAnalysis{
				MemoryAbility: MemoryAbilityDirectFact,
			},
			wantScore:  0.42,
			wantReason: "default_direct_fact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ruleConfidenceLegacy(tt.normalized, tt.analysis)
			if got.Score != tt.wantScore {
				t.Fatalf("score = %v, want %v", got.Score, tt.wantScore)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestScoreIntentEvidence(t *testing.T) {
	causal := QueryAnalysis{
		MemoryAbility: MemoryAbilityCausalExplain,
		Signals:       []QuerySignal{QuerySignalCausal},
	}
	direct := QueryAnalysis{
		MemoryAbility: MemoryAbilityDirectFact,
		Signals:       []QuerySignal{QuerySignalExactFact},
	}

	if got := scoreIntentEvidence("为什么最近抗拒上班", causal, nil); got < 0.85 {
		t.Fatalf("causal intent evidence = %v, want >= 0.85", got)
	}
	if got := scoreIntentEvidence("咖啡", direct, nil); got > 0.50 {
		t.Fatalf("plain direct intent evidence = %v, want <= 0.50", got)
	}
}

func TestScoreFieldConsistency(t *testing.T) {
	consistent := QueryAnalysis{
		TimeMode:      QueryTimeModeHistorical,
		MemoryAbility: MemoryAbilityHistorical,
		EvidenceNeed:  EvidenceNeedStateTransition,
		Signals:       []QuerySignal{QuerySignalHistorical, QuerySignalStateTransition},
	}
	conflicting := QueryAnalysis{
		TimeMode:      QueryTimeModeCurrent,
		MemoryAbility: MemoryAbilityDirectFact,
		EvidenceNeed:  EvidenceNeedStateTransition,
		Signals:       []QuerySignal{QuerySignalStateTransition},
	}

	if got := scoreFieldConsistency(consistent); got < 0.85 {
		t.Fatalf("consistent field score = %v, want >= 0.85", got)
	}
	if got := scoreFieldConsistency(conflicting); got > 0.60 {
		t.Fatalf("conflicting field score = %v, want <= 0.60", got)
	}
}

func TestScoreLexicalSpecificity(t *testing.T) {
	specific := scoreLexicalSpecificity("我什么时候告诉过你我喜欢laufey这件事")
	generic := scoreLexicalSpecificity("咖啡")
	if specific <= generic {
		t.Fatalf("specificity specific=%v generic=%v, want specific > generic", specific, generic)
	}
	if generic > 0.45 {
		t.Fatalf("generic specificity = %v, want <= 0.45", generic)
	}
}

func TestScoreAmbiguity(t *testing.T) {
	ambiguous := QueryAnalysis{MemoryAbility: MemoryAbilityHistorical}
	anchored := QueryAnalysis{
		MemoryAbility:  MemoryAbilityProvenance,
		EntityMentions: []QueryEntityMention{{EntityID: "ent_laufey", MatchText: "laufey"}},
	}

	if got := scoreAmbiguity("这件事后来怎么样了", ambiguous, nil); got < 0.50 {
		t.Fatalf("ambiguous query score = %v, want >= 0.50", got)
	}
	if got := scoreAmbiguity("我喜欢laufey这件事是从哪里知道的", anchored, nil); got > 0.45 {
		t.Fatalf("anchored query ambiguity = %v, want <= 0.45", got)
	}
}

func TestScoreComplexity(t *testing.T) {
	complex := QueryAnalysis{
		MemoryAbility: MemoryAbilityCausalExplain,
		Signals:       []QuerySignal{QuerySignalCausal, QuerySignalHistorical, QuerySignalStateTransition},
	}
	simple := QueryAnalysis{
		MemoryAbility: MemoryAbilityDirectFact,
		Signals:       []QuerySignal{QuerySignalExactFact},
	}

	if got := scoreComplexity("为什么我后来不喝咖啡了", complex); got < 0.60 {
		t.Fatalf("complex query score = %v, want >= 0.60", got)
	}
	if got := scoreComplexity("咖啡", simple); got > 0.35 {
		t.Fatalf("simple query complexity = %v, want <= 0.35", got)
	}
}

func TestScoreSafetyRisk(t *testing.T) {
	forget := QueryAnalysis{Signals: []QuerySignal{QuerySignalForgetDelete, QuerySignalSensitivity}}
	normal := QueryAnalysis{Signals: []QuerySignal{QuerySignalExactFact}}

	if got := scoreSafetyRisk("忘掉我不喜欢香菜这件事", forget); got < 0.80 {
		t.Fatalf("forget safety risk = %v, want >= 0.80", got)
	}
	if got := scoreSafetyRisk("我喜欢咖啡吗", normal); got != 0 {
		t.Fatalf("normal safety risk = %v, want 0", got)
	}
}

func TestComputeFieldConfidenceUsesScoreDimensions(t *testing.T) {
	scores := QueryAnalysisScores{
		ExpectedRetrievalConfidence: 0.61,
		TimeEvidence:                0.86,
		IntentEvidence:              0.82,
		DomainEvidence:              0.44,
		EvidenceNeedEvidence:        0.76,
		EntityResolution:            0.20,
	}

	got := ComputeFieldConfidence(QueryAnalysis{}, scores)
	if got.Overall != 0.61 ||
		got.TimeMode != 0.86 ||
		got.MemoryAbility != 0.82 ||
		got.MemoryDomain != 0.44 ||
		got.EvidenceNeed != 0.76 ||
		got.EntityResolution != 0.20 {
		t.Fatalf("field confidence = %#v, want score-backed fields", got)
	}
	if got.TimeMode == got.Overall || got.MemoryDomain == got.Overall {
		t.Fatalf("field confidence copied overall: %#v", got)
	}
}

func TestComputeRuleFitPopulatesFeatureScoresAndClamps(t *testing.T) {
	analysis := QueryAnalysis{
		TimeMode:      QueryTimeModeHistorical,
		MemoryDomain:  MemoryDomainWorkExperience,
		MemoryAbility: MemoryAbilityCausalExplain,
		EvidenceNeed:  EvidenceNeedStateTransition,
		Signals:       []QuerySignal{QuerySignalCausal, QuerySignalHistorical, QuerySignalStateTransition},
		Probes: QueryAnchorProbe{
			SparseProbeConf: 0.75,
		},
	}

	got := ComputeRuleFit("为什么我后来不喝咖啡了", analysis, nil)
	if got.RuleFit <= 0 || got.RuleFit > 1 {
		t.Fatalf("rule fit = %v, want clamped unit score", got.RuleFit)
	}
	wantExpected := clamp01(0.60*got.RuleFit + 0.40*got.AnchorReadiness - 0.10*got.SafetyRisk)
	if got.AnchorReadiness != 0.75 || got.ExpectedRetrievalConfidence != wantExpected {
		t.Fatalf("scores = %#v, want anchor readiness 0.75 and expected retrieval confidence %v", got, wantExpected)
	}
	wantSemanticNeed := clamp01(
		0.35*(1-got.RuleFit) +
			0.25*(1-got.AnchorReadiness) +
			0.25*got.Complexity +
			0.15*got.Ambiguity,
	)
	if got.SemanticNeed != wantSemanticNeed {
		t.Fatalf("semantic need = %v, want %v from feature formula; scores=%#v", got.SemanticNeed, wantSemanticNeed, got)
	}
	if got.IntentEvidence == 0 ||
		got.FieldConsistency == 0 ||
		got.TimeEvidence == 0 ||
		got.DomainEvidence == 0 ||
		got.EvidenceNeedEvidence == 0 ||
		got.Specificity == 0 ||
		got.Complexity == 0 {
		t.Fatalf("scores missing feature dimensions: %#v", got)
	}
}

func TestComputeRuleFitRaisesSemanticNeedForCausalWeakAnchor(t *testing.T) {
	analysis := QueryAnalysis{
		TimeMode:      QueryTimeModeCurrent,
		MemoryDomain:  MemoryDomainRelationship,
		MemoryAbility: MemoryAbilityCausalExplain,
		EvidenceNeed:  EvidenceNeedExactObservation,
		Signals:       []QuerySignal{QuerySignalCausal},
		Probes: QueryAnchorProbe{
			RecentProbeConf: 0.42,
		},
	}

	got := ComputeRuleFit("我为什么最近这么抗拒上班？", analysis, nil)

	if got.RuleFit < 0.60 {
		t.Fatalf("rule fit = %v, want >= 0.60; scores=%#v", got.RuleFit, got)
	}
	if got.AnchorReadiness > 0.45 {
		t.Fatalf("anchor readiness = %v, want weak anchor <= 0.45; scores=%#v", got.AnchorReadiness, got)
	}
	if got.SemanticNeed < 0.58 {
		t.Fatalf("semantic need = %v, want >= 0.58 for causal weak anchor; scores=%#v", got.SemanticNeed, got)
	}
}

func TestRuleConfidenceUsesFeatureScorer(t *testing.T) {
	analysis := QueryAnalysis{
		MemoryAbility: MemoryAbilityCausalExplain,
		Signals:       []QuerySignal{QuerySignalCausal},
	}
	legacy := ruleConfidenceLegacy("为什么最近抗拒上班", analysis).Score
	active := ruleConfidence("为什么最近抗拒上班", analysis)
	if active == legacy {
		t.Fatalf("active rule confidence = legacy score %v, want feature scorer value", legacy)
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

type probeFact struct {
	id          string
	predicate   string
	object      string
	summary     string
	factType    core.FactType
	importance  float64
	pinned      bool
	sensitivity core.SensitivityLevel
}

func openQueryProbeDB(t *testing.T, ctx context.Context, enableFTS bool) *DB {
	t.Helper()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.MigrateWithOptions(ctx, MigrateOptions{EnableFTS: enableFTS}); err != nil {
		_ = db.Close()
		t.Fatalf("migrate db: %v", err)
	}
	if err := NewStore(db.SQLDB()).EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		_ = db.Close()
		t.Fatalf("ensure persona: %v", err)
	}
	if err := NewStore(db.SQLDB()).EnsureSession(ctx, core.Session{ID: "session_probe", PersonaID: "default", Channel: core.ChannelAPI}); err != nil {
		_ = db.Close()
		t.Fatalf("ensure session: %v", err)
	}
	return db
}

func insertProbeEntity(t *testing.T, ctx context.Context, db *sql.DB, entityID string, canonical string) {
	t.Helper()
	if err := NewEntityRepository(db).Upsert(ctx, core.Entity{
		ID:            entityID,
		PersonaID:     "default",
		CanonicalName: canonical,
		EntityType:    core.EntityTypePerson,
	}); err != nil {
		t.Fatalf("insert entity %s: %v", entityID, err)
	}
}

func insertProbeAlias(t *testing.T, ctx context.Context, db *sql.DB, aliasID string, entityID string, alias string) {
	t.Helper()
	if err := NewEntityRepository(db).AddAlias(ctx, core.EntityAlias{
		ID:        aliasID,
		PersonaID: "default",
		EntityID:  entityID,
		Alias:     alias,
		AliasType: core.AliasTypeSurface,
	}); err != nil {
		t.Fatalf("insert alias %s: %v", aliasID, err)
	}
}

func insertProbeFact(t *testing.T, ctx context.Context, db *sql.DB, value probeFact) {
	t.Helper()
	if value.factType == "" {
		value.factType = core.FactTypeStablePreference
	}
	object := value.object
	if err := NewFactRepository(db).Insert(ctx, core.Fact{
		ID:                   value.id,
		PersonaID:            "default",
		SubjectEntityID:      probeStringPtr("ent_user"),
		Predicate:            value.predicate,
		ObjectLiteral:        &object,
		ContentSummary:       value.summary,
		FactType:             value.factType,
		ExtractionConfidence: core.ExtractionConfidenceExplicit,
		Importance:           value.importance,
		SensitivityLevel:     value.sensitivity,
		Pinned:               value.pinned,
	}); err != nil {
		t.Fatalf("insert fact %s: %v", value.id, err)
	}
	insertProbeEvidence(t, ctx, db, value.id, value.sensitivity)
}

func insertProbeEvidence(t *testing.T, ctx context.Context, db *sql.DB, factID string, sensitivity core.SensitivityLevel) {
	t.Helper()
	episodeID := "episode_" + factID
	if err := NewEpisodeRepository(db).Append(ctx, core.Episode{
		ID:               episodeID,
		PersonaID:        "default",
		SessionID:        "session_probe",
		Role:             core.RoleUser,
		Content:          "source for " + factID,
		OccurredAt:       time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
		SensitivityLevel: sensitivity,
	}); err != nil {
		t.Fatalf("insert episode for fact %s: %v", factID, err)
	}
	if err := NewLinkRepository(db).Insert(ctx, core.MemoryLink{
		ID:           "link_" + factID,
		PersonaID:    "default",
		FromNodeType: core.NodeTypeFact,
		FromNodeID:   factID,
		LinkType:     core.LinkTypeEvidencedBy,
		ToNodeType:   core.NodeTypeEpisode,
		ToNodeID:     episodeID,
	}); err != nil {
		t.Fatalf("insert evidence link for fact %s: %v", factID, err)
	}
}

func insertProbeNarrativeDocument(t *testing.T, ctx context.Context, db *sql.DB, nodeID string, text string) {
	t.Helper()
	if err := NewSearchRepository(db).UpsertDocument(ctx, core.SearchDocument{
		ID:               "search_" + nodeID,
		PersonaID:        "default",
		NodeType:         core.NodeTypeNarrative,
		NodeID:           nodeID,
		SearchText:       text,
		SearchTier:       core.SearchTierHot,
		VisibilityStatus: core.VisibilityVisible,
		SensitivityLevel: core.SensitivityNormal,
		LifecycleStatus:  core.LifecycleActive,
		Searchable:       true,
	}); err != nil {
		t.Fatalf("insert narrative search document %s: %v", nodeID, err)
	}
}

func rebuildProbeSearch(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	if _, err := NewSearchRepository(db).RebuildSearchDocuments(ctx, "default"); err != nil {
		t.Fatalf("rebuild search documents: %v", err)
	}
}

func requireProbeBreakdown(t *testing.T, probe QueryAnchorProbe, source string, confidence float64, wantError string) {
	t.Helper()
	for _, item := range probe.Breakdown {
		if item.Source != source {
			continue
		}
		if item.Confidence != confidence {
			t.Fatalf("probe breakdown %s confidence = %v, want %v in %#v", source, item.Confidence, confidence, probe.Breakdown)
		}
		if item.Error != wantError {
			t.Fatalf("probe breakdown %s error = %q, want %q in %#v", source, item.Error, wantError, probe.Breakdown)
		}
		return
	}
	t.Fatalf("probe breakdown = %#v, missing source %s", probe.Breakdown, source)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasEvidenceForField(values []QueryAnalysisEvidence, field string) bool {
	for _, value := range values {
		if value.Field == field {
			return true
		}
	}
	return false
}

func hasAlternative(values []QueryAnalysisAlternative, field string, value string) bool {
	for _, item := range values {
		if item.Field == field && item.Value == value {
			return true
		}
	}
	return false
}

func probeStringPtr(value string) *string {
	return &value
}
