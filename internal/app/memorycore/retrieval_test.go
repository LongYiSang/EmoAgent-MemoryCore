package memorycore

import (
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestShouldSkipSelectiveRerankAllowsLargeDirectRawMargin(t *testing.T) {
	query := memsqlite.QueryAnalysis{
		Raw:           "which studio did I register for and which days do I attend",
		Normalized:    "which studio did i register for and which days do i attend",
		Terms:         []string{"studio", "register", "days", "attend"},
		MemoryAbility: memsqlite.MemoryAbilityDirectFact,
		EvidenceNeed:  memsqlite.EvidenceNeedExactObservation,
		Confidence:    0.60,
	}
	candidates := []memsqlite.RerankCandidate{
		{
			NodeID:       "fact_direct_match",
			NodeType:     "fact",
			CurrentScore: 0.92,
			SourceScores: map[string]float64{
				"raw_dense":        0.99,
				"lexical_coverage": 0.25,
			},
		},
		{
			NodeID:       "fact_neighbor_1",
			NodeType:     "fact",
			CurrentScore: 0.51,
			SourceScores: map[string]float64{"raw_dense": 0.52},
		},
		{
			NodeID:       "fact_neighbor_2",
			NodeType:     "fact",
			CurrentScore: 0.48,
			SourceScores: map[string]float64{"raw_dense": 0.50},
		},
		{
			NodeID:       "fact_neighbor_3",
			NodeType:     "fact",
			CurrentScore: 0.45,
			SourceScores: map[string]float64{"raw_dense": 0.49},
		},
		{
			NodeID:       "fact_neighbor_4",
			NodeType:     "fact",
			CurrentScore: 0.43,
			SourceScores: map[string]float64{"raw_dense": 0.48},
		},
	}

	if !shouldSkipSelectiveRerank(query, candidates, nil) {
		t.Fatalf("shouldSkipSelectiveRerank = false, want skip for direct raw candidate with clear margin")
	}
}

func TestShouldUseCorrectedRetrievalAllowsSemanticReplacement(t *testing.T) {
	original := &MemoryContext{
		Blocks: []MemoryBlock{{
			Items: []MemoryContextItem{{NodeID: "weak_fact"}},
		}},
		RetrievalConfidence: &RetrievalConfidence{
			Overall:          0.30,
			CorrectiveAction: memsqlite.RetrievalCorrectiveActionSemanticLight,
		},
	}
	corrected := &MemoryContext{
		Blocks: []MemoryBlock{{
			Items: []MemoryContextItem{{NodeID: "better_fact"}},
		}},
		RetrievalConfidence: &RetrievalConfidence{Overall: 0.72},
	}

	if !shouldUseCorrectedRetrieval(original, corrected) {
		t.Fatalf("shouldUseCorrectedRetrieval = false, want semantic_light correction to replace weak non-empty result")
	}
}

func TestShouldUseCorrectedRetrievalRejectsSemanticReplacementWithWorseOverallAndTinyDimensionGain(t *testing.T) {
	original := &MemoryContext{
		Blocks: []MemoryBlock{{
			Items: []MemoryContextItem{{NodeID: "strong_fact"}},
		}},
		RetrievalConfidence: &RetrievalConfidence{
			Overall:          0.80,
			SourceDiversity:  0.30,
			CorrectiveAction: memsqlite.RetrievalCorrectiveActionSemanticLight,
		},
	}
	corrected := &MemoryContext{
		Blocks: []MemoryBlock{{
			Items: []MemoryContextItem{{NodeID: "different_fact"}},
		}},
		RetrievalConfidence: &RetrievalConfidence{
			Overall:         0.60,
			SourceDiversity: 0.31,
		},
	}

	if shouldUseCorrectedRetrieval(original, corrected) {
		t.Fatalf("shouldUseCorrectedRetrieval = true, want reject when semantic_light changes nodes with worse overall and only tiny dimension gain")
	}
}
