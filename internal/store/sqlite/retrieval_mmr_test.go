package sqlite

import (
	"math"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func TestMMRFactSimilarity(t *testing.T) {
	base := testScoredFact("fact-a", "用户讨厌早会，因为早会让他焦虑。", "ent-user", "dislikes", "ep-1", core.ValidityValid, core.LifecycleActive, 1.0)

	t.Run("exact duplicate is duplicate strength", func(t *testing.T) {
		duplicate := testScoredFact("fact-a-copy", "用户讨厌早会，因为早会让他焦虑。", "ent-user", "dislikes", "ep-1", core.ValidityValid, core.LifecycleActive, 0.9)
		got := factSimilarity(duplicate, base)
		if got < 0.88 || !nearFloat(got, 1.0) {
			t.Fatalf("factSimilarity duplicate = %.12f, want 1.0 and >= 0.88", got)
		}
	})

	t.Run("same entity different summary stays below duplicate strength", func(t *testing.T) {
		distinct := testScoredFact("fact-distinct", "用户喜欢咖啡，咖啡能帮助他恢复精力。", "ent-user", "dislikes", "ep-1", core.ValidityValid, core.LifecycleActive, 0.8)
		if got := factSimilarity(distinct, base); got > 0.80 {
			t.Fatalf("factSimilarity same entity different summary = %.12f, want <= 0.80", got)
		}
	})

	t.Run("same summary with different validity is capped", func(t *testing.T) {
		invalidated := testScoredFact("fact-invalidated", "用户讨厌早会，因为早会让他焦虑。", "ent-user", "dislikes", "ep-1", core.ValidityInvalidated, core.LifecycleActive, 0.8)
		if got := factSimilarity(invalidated, base); got > 0.80 || !nearFloat(got, 0.80) {
			t.Fatalf("factSimilarity different validity = %.12f, want 0.80 cap", got)
		}
	})

	t.Run("same summary with different lifecycle is capped", func(t *testing.T) {
		archived := testScoredFact("fact-archived", "用户讨厌早会，因为早会让他焦虑。", "ent-user", "dislikes", "ep-1", core.ValidityValid, core.LifecycleArchived, 0.8)
		if got := factSimilarity(archived, base); got > 0.80 || !nearFloat(got, 0.80) {
			t.Fatalf("factSimilarity different lifecycle = %.12f, want 0.80 cap", got)
		}
	})
}

func TestMMRBestCandidateIndexPrefersDiversityOverHighScoreDuplicate(t *testing.T) {
	selected := []scoredFact{
		testScoredFact("fact-a", "用户讨厌早会，因为早会让他焦虑。", "ent-user", "dislikes", "ep-1", core.ValidityValid, core.LifecycleActive, 1.0),
	}
	candidates := []scoredFact{
		testScoredFact("fact-a-copy", "用户讨厌早会，因为早会让他焦虑。", "ent-user", "dislikes", "ep-1", core.ValidityValid, core.LifecycleActive, 1.0),
		testScoredFact("fact-b", "旅行计划需要提前订票。", "ent-trip", "plans", "ep-2", core.ValidityValid, core.LifecycleActive, 0.75),
	}

	if got := bestMMRCandidateIndex(candidates, selected); got != 1 {
		t.Fatalf("bestMMRCandidateIndex = %d, want diverse lower-score candidate at index 1", got)
	}
}

func TestMMRBestCandidateIndexTieBreaksByScoreThenFactID(t *testing.T) {
	candidates := []scoredFact{
		testScoredFact("fact-b", "用户喜欢咖啡。", "ent-user", "likes", "ep-1", core.ValidityValid, core.LifecycleActive, 0.8),
		testScoredFact("fact-a", "用户喜欢茶。", "ent-user", "likes", "ep-1", core.ValidityValid, core.LifecycleActive, 0.8),
	}

	if got := bestMMRCandidateIndex(candidates, nil); got != 1 {
		t.Fatalf("bestMMRCandidateIndex tie = %d, want lexicographically smaller fact id at index 1", got)
	}
}

func TestAppendSuppressionDeduplicatesByNodeTypeNodeIDAndReason(t *testing.T) {
	suppressions := []MemorySuppression{
		{NodeType: string(core.NodeTypeFact), NodeID: "fact_same", Reason: MemorySuppressionReasonFatigue},
	}
	suppressions = appendSuppression(suppressions, MemorySuppression{NodeType: string(core.NodeTypeFact), NodeID: "fact_same", Reason: MemorySuppressionReasonFatigue})
	suppressions = appendSuppression(suppressions, MemorySuppression{NodeType: string(core.NodeTypeFact), NodeID: "fact_same", Reason: MemorySuppressionReasonContextBudget})

	if len(suppressions) != 2 {
		t.Fatalf("suppressions = %#v, want duplicate reason removed but distinct reason preserved", suppressions)
	}
	if suppressions[0].Reason != MemorySuppressionReasonFatigue || suppressions[1].Reason != MemorySuppressionReasonContextBudget {
		t.Fatalf("suppression reasons = %#v, want fatigue then context_budget", suppressions)
	}
}

func testScoredFact(id string, summary string, entityID string, predicate string, sourceEpisodeID string, validity core.ValidityStatus, lifecycle core.LifecycleStatus, score float64) scoredFact {
	entity := entityID
	return scoredFact{
		Fact: core.Fact{
			ID:               id,
			PersonaID:        "default",
			SubjectEntityID:  &entity,
			Predicate:        predicate,
			ContentSummary:   summary,
			ValidityStatus:   validity,
			LifecycleStatus:  lifecycle,
			VisibilityStatus: core.VisibilityVisible,
			Searchable:       true,
		},
		Score:            score,
		SourceEpisodeIDs: []string{sourceEpisodeID},
	}
}

func nearFloat(got float64, want float64) bool {
	return math.Abs(got-want) <= 1e-12
}
