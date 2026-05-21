package sqlite

import (
	"math"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func TestEvaluateRetrievalConfidenceBreakdownMetrics(t *testing.T) {
	finalCandidates := PreparedFinalCandidates{
		Request: RetrievalRequest{
			MirrorDiagnostics: &MirrorDiagnostics{
				Candidates: []MirrorCandidateDiagnostic{
					{SQLiteFactID: "hidden", DropReason: "dropped_by_authority_filter"},
				},
			},
		},
		Query: QueryAnalysis{
			Raw:           "咖啡",
			Normalized:    "咖啡",
			MemoryAbility: MemoryAbilityDirectFact,
			EvidenceNeed:  EvidenceNeedExactObservation,
			Scores: QueryAnalysisScores{
				RuleFit:                     0.72,
				AnchorReadiness:             0.65,
				SemanticNeed:                0.21,
				ExpectedRetrievalConfidence: 0.69,
			},
			Decision: QueryAnalysisDecision{SemanticMode: "none"},
		},
		Policy: RetrievalPolicy{FinalMemoryCount: 4},
	}
	scored := []scoredFact{
		confidenceFact("a", 0.90, []AnchorSourceBreakdown{{Source: AnchorSourceSQLiteSparse, Rank: 1, RawScore: 0.80}}, retrievalScoreBreakdown{AnchorEnergy: 0.80, LexicalCoverage: 0.40, FinalScore: 0.90}),
		confidenceFact("b", 0.75, []AnchorSourceBreakdown{{Source: AnchorSourceTriviumDense, Rank: 2, RawScore: 0.70}}, retrievalScoreBreakdown{GraphEnergy: 0.60, FinalScore: 0.75}),
		confidenceFact("c", 0.50, []AnchorSourceBreakdown{{Source: AnchorSourceRecentImportant, Rank: 3, RawScore: 0.50}}, retrievalScoreBreakdown{CompletionSource: completionSourceCausal, CompletionBonus: 0.36, FinalScore: 0.50}),
	}
	suppressions := []MemorySuppression{{NodeType: string(core.NodeTypeFact), NodeID: "dup", Reason: MemorySuppressionReasonMMRDuplicate}}

	got := evaluateRetrievalConfidence(finalCandidates, scored, scored, nil, suppressions)

	requireFloatNear(t, got.AuthorityPassRatio, 0.75)
	requireFloatNear(t, got.AnchorCoverage, 1.0)
	requireFloatNear(t, got.TopRankMargin, (0.90-0.75)/0.90)
	requireFloatNear(t, got.SourceDiversity, 1.0)
	requireFloatNear(t, got.MMRDiversity, 0.75)
	requireFloatNear(t, got.CandidateRecallProxy, 1.0)
	if got.CorrectiveAction != "" {
		t.Fatalf("corrective action = %q, want none", got.CorrectiveAction)
	}
	if got.Overall <= 0 {
		t.Fatalf("overall = %f, want positive observed confidence", got.Overall)
	}
}

func TestEvaluateRetrievalConfidenceTemporalInconsistencyHardFailure(t *testing.T) {
	finalCandidates := PreparedFinalCandidates{
		Query: QueryAnalysis{
			Raw:      "我现在住在哪里",
			TimeMode: QueryTimeModeCurrent,
		},
		Policy: RetrievalPolicy{FinalMemoryCount: 2, AllowHistorical: true},
	}
	selected := []scoredFact{
		confidenceFact("old", 0.90, nil, retrievalScoreBreakdown{AnchorEnergy: 0.8, FinalScore: 0.90}),
	}
	selected[0].Fact.ValidityStatus = core.ValidityInvalidated
	selected[0].Fact.LifecycleStatus = core.LifecycleArchived

	got := evaluateRetrievalConfidence(finalCandidates, selected, selected, nil, nil)

	requireFloatNear(t, got.TemporalConsistency, 0)
	if got.CorrectiveAction != RetrievalCorrectiveActionSuppressMemoryInjection {
		t.Fatalf("corrective action = %q, want suppress", got.CorrectiveAction)
	}
	if got.HardFailureReason != RetrievalHardFailureTemporalInconsistency {
		t.Fatalf("hard failure reason = %q, want temporal inconsistency", got.HardFailureReason)
	}
}

func TestEvaluateRetrievalConfidenceForbiddenSelectedHardFailure(t *testing.T) {
	finalCandidates := PreparedFinalCandidates{
		Query:  QueryAnalysis{Raw: "隐藏事实", TimeMode: QueryTimeModeCurrent},
		Policy: RetrievalPolicy{FinalMemoryCount: 2},
	}
	selected := []scoredFact{
		confidenceFact("hidden", 0.90, nil, retrievalScoreBreakdown{AnchorEnergy: 0.8, FinalScore: 0.90}),
	}
	selected[0].Fact.VisibilityStatus = core.VisibilityHidden

	got := evaluateRetrievalConfidence(finalCandidates, selected, selected, nil, nil)

	requireFloatNear(t, got.SensitivitySafety, 0)
	if got.CorrectiveAction != RetrievalCorrectiveActionSuppressMemoryInjection {
		t.Fatalf("corrective action = %q, want suppress", got.CorrectiveAction)
	}
	if got.HardFailureReason != RetrievalHardFailureForbiddenCandidate {
		t.Fatalf("hard failure reason = %q, want forbidden candidate", got.HardFailureReason)
	}
}

func TestEvaluateRetrievalConfidenceLowAnchorCoverageRequestsSemanticLight(t *testing.T) {
	finalCandidates := PreparedFinalCandidates{
		Query: QueryAnalysis{
			Raw:           "我为什么最近抗拒早会",
			TimeMode:      QueryTimeModeCurrent,
			MemoryAbility: MemoryAbilityCausalExplain,
			EvidenceNeed:  EvidenceNeedExactObservation,
		},
		Policy: RetrievalPolicy{FinalMemoryCount: 4},
	}
	selected := []scoredFact{
		confidenceFact("weak", 0.30, nil, retrievalScoreBreakdown{FinalScore: 0.30}),
	}

	got := evaluateRetrievalConfidence(finalCandidates, selected, selected, nil, nil)

	requireFloatNear(t, got.AnchorCoverage, 0)
	if got.CorrectiveAction != RetrievalCorrectiveActionSemanticLight {
		t.Fatalf("corrective action = %q, want semantic_light", got.CorrectiveAction)
	}
}

func confidenceFact(id string, score float64, sources []AnchorSourceBreakdown, breakdown retrievalScoreBreakdown) scoredFact {
	breakdown.FinalScore = score
	return scoredFact{
		Fact: core.Fact{
			ID:                        id,
			PersonaID:                 "default",
			ContentSummary:            id,
			ExtractionConfidenceScore: 0.8,
			Importance:                0.7,
			SensitivityLevel:          core.SensitivityNormal,
			ValidityStatus:            core.ValidityValid,
			VisibilityStatus:          core.VisibilityVisible,
			LifecycleStatus:           core.LifecycleActive,
			Searchable:                true,
		},
		Score:           score,
		Breakdown:       breakdown,
		SourceBreakdown: sources,
	}
}

func requireFloatNear(t *testing.T, got float64, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("got %f, want %f", got, want)
	}
}
