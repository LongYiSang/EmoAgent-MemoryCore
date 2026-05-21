package memorycore_test

import (
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestQueryAnalysisPhase1PublicAliasesCompile(t *testing.T) {
	analysis := memorycore.QueryAnalysis{
		Scores: memorycore.QueryAnalysisScores{
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
		Probes: memorycore.QueryAnchorProbe{
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
		Decision: memorycore.QueryAnalysisDecision{
			UseSemantic:      true,
			SemanticMode:     "decompose",
			RetrievalMode:    "graph_contextual",
			ReasonCodes:      []string{"causal_intent", "weak_anchor"},
			ThresholdVersion: "semantic_router_v1",
			ScorerVersion:    "query_analysis_scorer_v1",
		},
		Evidence: []memorycore.QueryAnalysisEvidence{{
			Field:     "memory_ability",
			Signal:    "causal_word",
			MatchText: "为什么",
			SpanStart: 0,
			SpanEnd:   6,
			Weight:    0.38,
			Detector:  "rule_regex_v1",
		}},
		Alternatives: []memorycore.QueryAnalysisAlternative{{
			Field:       "time_mode",
			Value:       string(memorycore.QueryTimeModeHistorical),
			Confidence:  0.41,
			ReasonCodes: []string{"historical_phrase"},
			Detector:    "rule_regex_v1",
		}},
		Diagnostics: &memorycore.QueryAnalysisDiagnostics{
			SemanticAnalysis: &memorycore.SemanticQueryAnalysisDiagnostics{
				Scores:       memorycore.QueryAnalysisScores{SemanticNeed: 0.77},
				Probes:       memorycore.QueryAnchorProbe{SparseProbeConf: 0.52},
				Decision:     memorycore.QueryAnalysisDecision{UseSemantic: true, SemanticMode: "light", ReasonCodes: []string{"semantic_need_high"}},
				Evidence:     []memorycore.QueryAnalysisEvidence{{Field: "evidence_need", Signal: "why"}},
				Alternatives: []memorycore.QueryAnalysisAlternative{{Field: "memory_ability", Value: string(memorycore.MemoryAbilityDirectFact), Confidence: 0.22}},
			},
		},
	}

	if analysis.Scores.RuleFit != 0.61 ||
		analysis.Probes.Top1Margin != 0.14 ||
		analysis.Decision.ReasonCodes[1] != "weak_anchor" ||
		analysis.Evidence[0].Detector != "rule_regex_v1" ||
		analysis.Alternatives[0].Value != string(memorycore.QueryTimeModeHistorical) ||
		analysis.Diagnostics.SemanticAnalysis.Decision.SemanticMode != "light" {
		t.Fatalf("phase 1 public aliases not retained: %#v", analysis)
	}
}
