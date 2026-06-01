package sqlite

import (
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func TestFinalSelectionMMRTraceUsesSelectionScoreWhenAvailable(t *testing.T) {
	selected := []scoredFact{{
		Fact:  core.Fact{ID: "fact-a", ContentSummary: "用户喜欢咖啡。"},
		Score: 0.91,
	}}

	items := finalSelectionMMRTraceItems(selected, map[string]float64{"fact-a": 0.42})

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Score != 0.42 {
		t.Fatalf("score = %v, want MMR selection score", items[0].Score)
	}
}
