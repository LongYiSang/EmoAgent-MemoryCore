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
			ScorerVersion:           "rule_confidence_legacy.v0",
			RuleConfidenceLegacy:    0.42,
			RuleConfidenceReason:    "exact_fact_only",
			SemanticDecisionLegacy:  true,
			MinConfidenceToOverride: 0.72,
			Signals:                 []string{string(memsqlite.QuerySignalExactFact)},
			EntityMentionCount:      1,
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
	if analysis.Diagnostics.ScorerVersion != "rule_confidence_legacy.v0" ||
		analysis.Diagnostics.RuleConfidenceLegacy != 0.42 ||
		analysis.Diagnostics.RuleConfidenceReason != "exact_fact_only" ||
		!analysis.Diagnostics.SemanticDecisionLegacy ||
		analysis.Diagnostics.MinConfidenceToOverride != 0.72 ||
		analysis.Diagnostics.EntityMentionCount != 1 {
		t.Fatalf("legacy diagnostics = %#v, want mapped legacy fields", analysis.Diagnostics)
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
			SemanticAnalysis: &memsqlite.SemanticQueryAnalysisDiagnostics{
				Scores: memsqlite.QueryAnalysisScores{SemanticNeed: 0.77},
				Probes: memsqlite.QueryAnchorProbe{SparseProbeConf: 0.52},
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
		analysis.Decision.ReasonCodes[1] != "weak_anchor" ||
		analysis.Evidence[0].MatchText != "为什么" ||
		analysis.Alternatives[0].ReasonCodes[0] != "historical_phrase" ||
		analysis.Diagnostics.SemanticAnalysis.Decision.ReasonCodes[0] != "semantic_need_high" ||
		analysis.Diagnostics.SemanticAnalysis.Alternatives[0].ReasonCodes[0] != "fallback" {
		t.Fatalf("phase 1 fields = %#v", analysis)
	}

	storeAnalysis.Decision.ReasonCodes[0] = "mutated"
	storeAnalysis.Evidence[0].MatchText = "mutated"
	storeAnalysis.Alternatives[0].ReasonCodes[0] = "mutated"
	storeAnalysis.Diagnostics.SemanticAnalysis.Decision.ReasonCodes[0] = "mutated"
	storeAnalysis.Diagnostics.SemanticAnalysis.Evidence[0].Field = "mutated"
	storeAnalysis.Diagnostics.SemanticAnalysis.Alternatives[0].ReasonCodes[0] = "mutated"

	if analysis.Decision.ReasonCodes[0] != "causal_intent" ||
		analysis.Evidence[0].MatchText != "为什么" ||
		analysis.Alternatives[0].ReasonCodes[0] != "historical_phrase" ||
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
