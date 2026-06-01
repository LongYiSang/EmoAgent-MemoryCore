package sqlite

import "github.com/longyisang/emoagent-memorycore/internal/core"

func buildMemoryPipelineTrace(query QueryAnalysis, fusedAnchors []FusedAnchor, preRerank []scoredFact, rerankResults []RerankResultItem, rerankDiagnostics *RerankDiagnostics, selected []scoredFact, selectionScores map[string]float64) *MemoryPipelineTrace {
	contentByFactID := map[string]string{}
	for _, candidate := range preRerank {
		contentByFactID[candidate.Fact.ID] = candidate.Fact.ContentSummary
	}
	for _, candidate := range selected {
		contentByFactID[candidate.Fact.ID] = candidate.Fact.ContentSummary
	}

	return &MemoryPipelineTrace{
		QueryAnalysis: MemoryPipelineQueryAnalysis{
			Normalized: query.Normalized,
			Scores: MemoryPipelineQueryScores{
				RuleFit:                     query.Scores.RuleFit,
				AnchorReadiness:             query.Scores.AnchorReadiness,
				SemanticNeed:                query.Scores.SemanticNeed,
				ExpectedRetrievalConfidence: query.Scores.ExpectedRetrievalConfidence,
			},
		},
		Stages: MemoryPipelineStages{
			AnchorRecall:          anchorRecallTraceItems(fusedAnchors, contentByFactID),
			RRFFusion:             rrfFusionTraceItems(fusedAnchors, contentByFactID),
			SQLiteAuthorityFilter: sqliteAuthorityFilterTraceItems(preRerank),
			SafeRerank:            safeRerankTraceItems(rerankResults, rerankDiagnostics, contentByFactID),
			FinalSelectionMMR:     finalSelectionMMRTraceItems(selected, selectionScores),
		},
	}
}

func anchorRecallTraceItems(fusedAnchors []FusedAnchor, contentByFactID map[string]string) []MemoryPipelineTraceItem {
	items := make([]MemoryPipelineTraceItem, 0, len(fusedAnchors))
	for _, anchor := range fusedAnchors {
		items = append(items, MemoryPipelineTraceItem{
			ContentSummary: traceContentSummary(anchor.NodeType, anchor.NodeID, contentByFactID),
			Score:          strongestAnchorRecallScore(anchor.SourceBreakdown),
		})
	}
	return items
}

func strongestAnchorRecallScore(breakdown []AnchorSourceBreakdown) float64 {
	best := 0.0
	for _, source := range breakdown {
		score := source.RawScore
		if score <= 0 {
			score = source.RRFContribution
		}
		if score > best {
			best = score
		}
	}
	return best
}

func rrfFusionTraceItems(fusedAnchors []FusedAnchor, contentByFactID map[string]string) []MemoryPipelineTraceItem {
	items := make([]MemoryPipelineTraceItem, 0, len(fusedAnchors))
	for _, anchor := range fusedAnchors {
		items = append(items, MemoryPipelineTraceItem{
			ContentSummary: traceContentSummary(anchor.NodeType, anchor.NodeID, contentByFactID),
			Score:          anchor.FusedAnchorScore,
		})
	}
	return items
}

func sqliteAuthorityFilterTraceItems(scored []scoredFact) []MemoryPipelineTraceItem {
	items := make([]MemoryPipelineTraceItem, 0, len(scored))
	for _, candidate := range scored {
		items = append(items, MemoryPipelineTraceItem{
			ContentSummary: candidate.Fact.ContentSummary,
			Score:          candidate.Score,
		})
	}
	return items
}

func safeRerankTraceItems(rerankResults []RerankResultItem, diagnostics *RerankDiagnostics, contentByFactID map[string]string) []MemoryPipelineTraceItem {
	if diagnostics == nil || diagnostics.Status != "used" || diagnostics.Degraded || len(rerankResults) == 0 {
		return nil
	}
	items := make([]MemoryPipelineTraceItem, 0, len(rerankResults))
	for _, item := range rerankResults {
		items = append(items, MemoryPipelineTraceItem{
			ContentSummary: traceContentSummary(core.NodeType(item.NodeType), item.NodeID, contentByFactID),
			Score:          item.RerankScore,
		})
	}
	return items
}

func finalSelectionMMRTraceItems(selected []scoredFact, selectionScores map[string]float64) []MemoryPipelineTraceItem {
	items := make([]MemoryPipelineTraceItem, 0, len(selected))
	for _, candidate := range selected {
		score := candidate.Score
		if selectionScore, ok := selectionScores[candidate.Fact.ID]; ok {
			score = selectionScore
		}
		items = append(items, MemoryPipelineTraceItem{
			ContentSummary: candidate.Fact.ContentSummary,
			Score:          score,
		})
	}
	return items
}

func traceContentSummary(nodeType core.NodeType, nodeID string, contentByFactID map[string]string) string {
	if nodeType != core.NodeTypeFact {
		return ""
	}
	return contentByFactID[nodeID]
}
