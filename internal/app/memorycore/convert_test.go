package memorycore

import (
	"encoding/json"
	"strings"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestQueryAnalysisFromStoreOmitsEmptySemanticFields(t *testing.T) {
	analysis := queryAnalysisFromStore(&memsqlite.QueryAnalysis{
		Raw:           "secret query",
		Normalized:    "secret query",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainRelationship,
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Source:        memsqlite.QueryAnalysisSourceRuleOnly,
	})
	if analysis == nil {
		t.Fatal("analysis = nil")
	}
	if analysis.QueryRewrites != nil || analysis.SemanticAnchors != nil || analysis.ContextBlockHints != nil || analysis.Diagnostics != nil {
		t.Fatalf("semantic fields = rewrites:%#v anchors:%#v hints:%#v diagnostics:%#v, want nil", analysis.QueryRewrites, analysis.SemanticAnchors, analysis.ContextBlockHints, analysis.Diagnostics)
	}

	raw, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal query analysis: %v", err)
	}
	jsonText := string(raw)
	for _, field := range []string{"QueryRewrites", "SemanticAnchors", "ContextBlockHints", "Diagnostics"} {
		if strings.Contains(jsonText, field) {
			t.Fatalf("json = %s, want %s omitted", jsonText, field)
		}
	}
	if strings.Contains(jsonText, "semantic-only-sensitive-text") {
		t.Fatalf("json = %s, unexpected manufactured semantic text", jsonText)
	}
}

func TestQueryAnalysisRewriteDropDiagnosticsMapToPublicDTO(t *testing.T) {
	analysis := queryAnalysisFromStore(&memsqlite.QueryAnalysis{
		Raw:           "我喜欢Laufey这件事是从哪里知道的？",
		Normalized:    "我喜欢laufey这件事是从哪里知道的？",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainUserProfile,
		MemoryAbility: memsqlite.MemoryAbilityProvenance,
		EvidenceNeed:  memsqlite.EvidenceNeedProvenanceSource,
		Source:        memsqlite.QueryAnalysisSourceMerged,
		QueryRewrites: []memsqlite.QueryRewrite{{
			Text:    "用户喜欢Laufey的来源",
			Purpose: "semantic_recall",
			Weight:  0.7,
		}},
		Diagnostics: &memsqlite.QueryAnalysisDiagnostics{
			SemanticStatus:          "ok",
			RewriteCount:            1,
			DroppedRewriteCount:     1,
			DroppedRewriteReasons:   []string{"rewrite_language_mismatch"},
			EnglishRewriteCount:     2,
			ScorerVersion:           "query_analysis_rule_feature_scorer.v1",
			RuleConfidenceLegacy:    0.42,
			RuleConfidenceReason:    "exact_fact_only",
			SemanticDecisionLegacy:  true,
			MinConfidenceToOverride: 0.72,
			Signals:                 []string{string(memsqlite.QuerySignalExactFact)},
			EntityMentionCount:      1,
			Scores: memsqlite.QueryAnalysisScores{
				RuleFit:                     0.61,
				ExpectedRetrievalConfidence: 0.61,
				IntentEvidence:              0.42,
			},
			FieldConfidence: memsqlite.QueryAnalysisConfidence{
				Overall:       0.61,
				MemoryAbility: 0.42,
			},
			RuleDecision: memsqlite.QueryAnalysisDecision{
				RetrievalMode: "provenance",
				ReasonCodes:   []string{"provenance_intent"},
				ScorerVersion: "query_analysis_rule_feature_scorer.v1",
			},
			RuleEvidence: []memsqlite.QueryAnalysisEvidence{{
				Field:     "memory_ability",
				Signal:    "provenance_source",
				MatchText: "从哪里知道",
				Weight:    0.9,
				Detector:  "rule_feature_scorer.v1",
			}},
			RuleAlternatives: []memsqlite.QueryAnalysisAlternative{{
				Field:       "time_mode",
				Value:       string(memsqlite.QueryTimeModeHistorical),
				Confidence:  0.41,
				ReasonCodes: []string{"ambiguous_reference"},
				Detector:    "rule_feature_scorer.v1",
			}},
		},
	})
	if analysis == nil || analysis.Diagnostics == nil {
		t.Fatalf("analysis diagnostics = %#v, want populated diagnostics", analysis)
	}
	if analysis.Diagnostics.DroppedRewriteCount != 1 {
		t.Fatalf("dropped rewrite count = %d, want 1", analysis.Diagnostics.DroppedRewriteCount)
	}
	if analysis.Diagnostics.EnglishRewriteCount != 2 {
		t.Fatalf("English rewrite count = %d, want 2", analysis.Diagnostics.EnglishRewriteCount)
	}
	if len(analysis.Diagnostics.DroppedRewriteReasons) != 1 || analysis.Diagnostics.DroppedRewriteReasons[0] != "rewrite_language_mismatch" {
		t.Fatalf("dropped rewrite reasons = %#v, want rewrite_language_mismatch", analysis.Diagnostics.DroppedRewriteReasons)
	}
	if analysis.Diagnostics.ScorerVersion != "query_analysis_rule_feature_scorer.v1" ||
		analysis.Diagnostics.RuleConfidenceLegacy != 0.42 ||
		analysis.Diagnostics.RuleConfidenceReason != "exact_fact_only" ||
		!analysis.Diagnostics.SemanticDecisionLegacy ||
		analysis.Diagnostics.MinConfidenceToOverride != 0.72 ||
		analysis.Diagnostics.EntityMentionCount != 1 {
		t.Fatalf("legacy diagnostics = %#v, want mapped legacy fields", analysis.Diagnostics)
	}
	if analysis.Diagnostics.Scores.RuleFit != 0.61 ||
		analysis.Diagnostics.Scores.ExpectedRetrievalConfidence != 0.61 ||
		analysis.Diagnostics.Scores.IntentEvidence != 0.42 ||
		analysis.Diagnostics.FieldConfidence.Overall != 0.61 ||
		analysis.Diagnostics.FieldConfidence.MemoryAbility != 0.42 {
		t.Fatalf("diagnostic scores = %#v field confidence = %#v, want mapped phase 2 scores", analysis.Diagnostics.Scores, analysis.Diagnostics.FieldConfidence)
	}
	if analysis.Diagnostics.RuleDecision.RetrievalMode != "provenance" ||
		analysis.Diagnostics.RuleDecision.ReasonCodes[0] != "provenance_intent" ||
		analysis.Diagnostics.RuleEvidence[0].MatchText != "从哪里知道" ||
		analysis.Diagnostics.RuleAlternatives[0].ReasonCodes[0] != "ambiguous_reference" {
		t.Fatalf("rule diagnostics = decision:%#v evidence:%#v alternatives:%#v, want mapped rule explanation", analysis.Diagnostics.RuleDecision, analysis.Diagnostics.RuleEvidence, analysis.Diagnostics.RuleAlternatives)
	}
	if len(analysis.Diagnostics.Signals) != 1 || analysis.Diagnostics.Signals[0] != string(memsqlite.QuerySignalExactFact) {
		t.Fatalf("diagnostic signals = %#v, want exact_fact", analysis.Diagnostics.Signals)
	}

	raw, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal query analysis: %v", err)
	}
	jsonText := string(raw)
	if strings.Contains(jsonText, "when did the user say they like Laufey") {
		t.Fatalf("json leaked dropped rewrite text: %s", jsonText)
	}
	if !strings.Contains(jsonText, "rewrite_language_mismatch") {
		t.Fatalf("json = %s, want public drop reason", jsonText)
	}
}

func TestQueryAnalysisPhase1DiagnosticsMapToPublicDTOWithDeepCopies(t *testing.T) {
	storeAnalysis := &memsqlite.QueryAnalysis{
		Raw:           "为什么最近抗拒上班",
		Normalized:    "为什么最近抗拒上班",
		TimeMode:      memsqlite.QueryTimeModeCurrent,
		MemoryDomain:  memsqlite.MemoryDomainWorkExperience,
		MemoryAbility: memsqlite.MemoryAbilityCausalExplain,
		EvidenceNeed:  memsqlite.EvidenceNeedStateTransition,
		Source:        memsqlite.QueryAnalysisSourceMerged,
		Confidence:    0.66,
		Scores: memsqlite.QueryAnalysisScores{
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
		Probes: memsqlite.QueryAnchorProbe{
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
			Breakdown: []memsqlite.QueryAnchorProbeBreakdown{{
				Source:      "sparse_probe",
				Confidence:  0.62,
				HitCount:    4,
				TopScore:    0.91,
				SecondScore: 0.77,
				Reason:      "sqlite search document match",
			}},
		},
		Decision: memsqlite.QueryAnalysisDecision{
			UseSemantic:      true,
			SemanticMode:     "decompose",
			RetrievalMode:    "graph_contextual",
			ReasonCodes:      []string{"causal_intent", "weak_anchor"},
			ThresholdVersion: "semantic_router_v1",
			ScorerVersion:    "query_analysis_scorer_v1",
		},
		Evidence: []memsqlite.QueryAnalysisEvidence{{
			Field:     "memory_ability",
			Signal:    "causal_word",
			MatchText: "为什么",
			SpanStart: 0,
			SpanEnd:   6,
			Weight:    0.38,
			Detector:  "rule_regex_v1",
		}},
		Alternatives: []memsqlite.QueryAnalysisAlternative{{
			Field:       "time_mode",
			Value:       string(memsqlite.QueryTimeModeHistorical),
			Confidence:  0.41,
			ReasonCodes: []string{"historical_phrase"},
			Detector:    "rule_regex_v1",
		}},
		Diagnostics: &memsqlite.QueryAnalysisDiagnostics{
			AdaptiveDecision: memsqlite.QueryAnalysisDecision{
				UseSemantic:   true,
				SemanticMode:  "semantic_light",
				RetrievalMode: "semantic",
				ReasonCodes:   []string{"rule_fit_low"},
			},
			SemanticAnalysis: &memsqlite.SemanticQueryAnalysisDiagnostics{
				Scores: memsqlite.QueryAnalysisScores{SemanticNeed: 0.77},
				Probes: memsqlite.QueryAnchorProbe{
					SparseProbeConf: 0.52,
					Breakdown: []memsqlite.QueryAnchorProbeBreakdown{{
						Source:     "sparse_probe",
						Confidence: 0.52,
						HitCount:   2,
						Reason:     "semantic returned probe snapshot",
					}},
				},
				Decision: memsqlite.QueryAnalysisDecision{
					UseSemantic:  true,
					SemanticMode: "light",
					ReasonCodes:  []string{"semantic_need_high"},
				},
				Evidence: []memsqlite.QueryAnalysisEvidence{{Field: "evidence_need", Signal: "why"}},
				Alternatives: []memsqlite.QueryAnalysisAlternative{{
					Field:       "memory_ability",
					Value:       string(memsqlite.MemoryAbilityDirectFact),
					Confidence:  0.22,
					ReasonCodes: []string{"fallback"},
				}},
			},
		},
	}

	analysis := queryAnalysisFromStore(storeAnalysis)
	if analysis == nil {
		t.Fatal("analysis = nil")
	}
	if analysis.Scores.RuleFit != 0.61 ||
		analysis.Probes.Top1Margin != 0.14 ||
		analysis.Probes.Breakdown[0].Source != "sparse_probe" ||
		analysis.Decision.ReasonCodes[1] != "weak_anchor" ||
		analysis.Diagnostics.AdaptiveDecision.ReasonCodes[0] != "rule_fit_low" ||
		analysis.Evidence[0].MatchText != "为什么" ||
		analysis.Alternatives[0].ReasonCodes[0] != "historical_phrase" ||
		analysis.Diagnostics.SemanticAnalysis.Decision.ReasonCodes[0] != "semantic_need_high" ||
		analysis.Diagnostics.SemanticAnalysis.Probes.Breakdown[0].Source != "sparse_probe" ||
		analysis.Diagnostics.SemanticAnalysis.Alternatives[0].ReasonCodes[0] != "fallback" {
		t.Fatalf("phase 1 fields = %#v", analysis)
	}

	storeAnalysis.Probes.Breakdown[0].Source = "mutated"
	storeAnalysis.Decision.ReasonCodes[0] = "mutated"
	storeAnalysis.Diagnostics.AdaptiveDecision.ReasonCodes[0] = "mutated"
	storeAnalysis.Evidence[0].MatchText = "mutated"
	storeAnalysis.Alternatives[0].ReasonCodes[0] = "mutated"
	storeAnalysis.Diagnostics.SemanticAnalysis.Probes.Breakdown[0].Source = "mutated"
	storeAnalysis.Diagnostics.SemanticAnalysis.Decision.ReasonCodes[0] = "mutated"
	storeAnalysis.Diagnostics.SemanticAnalysis.Evidence[0].Field = "mutated"
	storeAnalysis.Diagnostics.SemanticAnalysis.Alternatives[0].ReasonCodes[0] = "mutated"

	if analysis.Probes.Breakdown[0].Source != "sparse_probe" ||
		analysis.Decision.ReasonCodes[0] != "causal_intent" ||
		analysis.Diagnostics.AdaptiveDecision.ReasonCodes[0] != "rule_fit_low" ||
		analysis.Evidence[0].MatchText != "为什么" ||
		analysis.Alternatives[0].ReasonCodes[0] != "historical_phrase" ||
		analysis.Diagnostics.SemanticAnalysis.Probes.Breakdown[0].Source != "sparse_probe" ||
		analysis.Diagnostics.SemanticAnalysis.Decision.ReasonCodes[0] != "semantic_need_high" ||
		analysis.Diagnostics.SemanticAnalysis.Evidence[0].Field != "evidence_need" ||
		analysis.Diagnostics.SemanticAnalysis.Alternatives[0].ReasonCodes[0] != "fallback" {
		t.Fatalf("phase 1 conversion was not deep-copied: %#v", analysis)
	}

	raw, err := json.Marshal(analysis)
	if err != nil {
		t.Fatalf("marshal query analysis: %v", err)
	}
	jsonText := string(raw)
	for _, want := range []string{"Scores", "Probes", "Decision", "Evidence", "Alternatives", "semantic_analysis"} {
		if !strings.Contains(jsonText, want) {
			t.Fatalf("json = %s, want %s", jsonText, want)
		}
	}
}
