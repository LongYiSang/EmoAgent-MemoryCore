package eval

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

func (s *runState) assert(ctx context.Context, assertion Assertion) error {
	switch assertion.Type {
	case "consolidation_result":
		return s.assertConsolidationResult(assertion)
	case "memory_contains":
		return s.assertMemoryContains(assertion, true)
	case "memory_not_contains":
		return s.assertMemoryContains(assertion, false)
	case "query_analysis":
		return s.assertQueryAnalysis(assertion)
	case "anchor_fusion":
		return s.assertAnchorFusion(assertion)
	case "selected_recall_at_k":
		return s.assertSelectedRecallAtK(assertion)
	case "context_precision_at_k":
		return s.assertContextPrecisionAtK(assertion)
	case "forbidden_recall_zero":
		return s.assertForbiddenRecallZero(assertion)
	case "block_contains":
		return s.assertBlockContains(assertion, true)
	case "block_not_contains":
		return s.assertBlockContains(assertion, false)
	case "selected_chain_correct":
		return s.assertSelectedChainCorrect(assertion)
	case "suppression_event":
		return s.assertSuppressionEvent(ctx, assertion)
	case "mirror_candidate":
		return s.assertMirrorCandidate(assertion)
	case "graph_activation_candidate":
		return s.assertGraphActivationCandidate(assertion)
	case "rerank_status":
		return s.assertRerankStatus(assertion)
	case "rerank_input":
		return s.assertRerankInput(assertion)
	case "unsupported_premise_not_asserted":
		return s.assertUnsupportedPremiseNotAsserted(assertion)
	case "ablation_improves":
		return s.assertAblationImproves(assertion)
	case "fact_count":
		return s.assertFactCount(ctx, assertion)
	case "fact_column":
		return s.assertFactColumn(ctx, assertion)
	case "link_exists":
		return s.assertLinkExists(ctx, assertion)
	case "narrative_exists":
		return s.assertNarrativeExists(ctx, assertion)
	case "insight_exists":
		return s.assertInsightExists(ctx, assertion)
	case "derived_link_count":
		return s.assertDerivedLinkCount(ctx, assertion)
	case "search_absent":
		return s.assertSearchAbsent(ctx, assertion)
	case "deletion_event_safe":
		return s.assertDeletionEventSafe(ctx, assertion)
	case "episode_tombstone_exists":
		return s.assertEpisodeTombstoneExists(ctx, assertion)
	case "mirror_index_status":
		return s.assertMirrorIndexStatus(ctx, assertion)
	case "queue_count":
		return s.assertQueueCount(ctx, assertion)
	case "queue_status":
		return s.assertQueueStatus(ctx, assertion)
	default:
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "known assertion type", Actual: assertion.Type}
	}
}

func (s *runState) assertConsolidationResult(assertion Assertion) error {
	result, ok := s.steps[assertion.Step]
	if !ok || result.Consolidation == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "consolidation step " + assertion.Step, Actual: "missing"}
	}
	if assertion.Action != "" && result.Consolidation.Action != assertion.Action {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "action=" + assertion.Action, Actual: "action=" + result.Consolidation.Action}
	}
	if assertion.Status != "" && result.Consolidation.Status != assertion.Status {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "status=" + assertion.Status, Actual: "status=" + result.Consolidation.Status}
	}
	return nil
}

func (s *runState) assertMemoryContains(assertion Assertion, wantPresent bool) error {
	nodeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	result, ok := s.steps[assertion.Step]
	if !ok || result.Retrieval == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "retrieve step " + assertion.Step, Actual: "missing"}
	}
	for _, block := range result.Retrieval.Blocks {
		for _, item := range block.Items {
			if item.NodeID != nodeID {
				continue
			}
			if !wantPresent {
				return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "node " + nodeID + " absent", Actual: "present"}
			}
			if assertion.Summary != "" && item.Summary != assertion.Summary {
				return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "summary=" + assertion.Summary, Actual: "summary=" + item.Summary}
			}
			if assertion.UsageGuidanceContains != "" && !strings.Contains(item.UsageGuidance, assertion.UsageGuidanceContains) {
				return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "usage_guidance contains " + assertion.UsageGuidanceContains, Actual: item.UsageGuidance}
			}
			return nil
		}
	}
	if wantPresent {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "node " + nodeID + " present", Actual: memoryItemsDebug(result.Retrieval)}
	}
	return nil
}

func (s *runState) assertQueryAnalysis(assertion Assertion) error {
	result, ok := s.steps[assertion.Step]
	if !ok || result.Retrieval == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "retrieve step " + assertion.Step, Actual: "missing"}
	}
	analysis := result.Retrieval.QueryAnalysis
	if analysis == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "query analysis present", Actual: "nil"}
	}
	if assertion.TimeMode != "" && string(analysis.TimeMode) != assertion.TimeMode {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "time_mode=" + assertion.TimeMode, Actual: "time_mode=" + string(analysis.TimeMode)}
	}
	if assertion.MemoryDomain != "" && string(analysis.MemoryDomain) != assertion.MemoryDomain {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "memory_domain=" + assertion.MemoryDomain, Actual: "memory_domain=" + string(analysis.MemoryDomain)}
	}
	if assertion.MemoryAbility != "" && string(analysis.MemoryAbility) != assertion.MemoryAbility {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "memory_ability=" + assertion.MemoryAbility, Actual: "memory_ability=" + string(analysis.MemoryAbility)}
	}
	if assertion.EvidenceNeed != "" && string(analysis.EvidenceNeed) != assertion.EvidenceNeed {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "evidence_need=" + assertion.EvidenceNeed, Actual: "evidence_need=" + string(analysis.EvidenceNeed)}
	}
	if assertion.Source != "" && string(analysis.Source) != assertion.Source {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "source=" + assertion.Source, Actual: "source=" + string(analysis.Source)}
	}
	if assertion.Status != "" {
		actual := ""
		if analysis.Diagnostics != nil {
			actual = analysis.Diagnostics.SemanticStatus
		}
		if actual != assertion.Status {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "semantic_status=" + assertion.Status, Actual: "semantic_status=" + actual}
		}
	}
	if assertion.FallbackReason != "" {
		actual := ""
		if analysis.Diagnostics != nil {
			actual = analysis.Diagnostics.FallbackReason
		}
		if actual != assertion.FallbackReason {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "fallback_reason=" + assertion.FallbackReason, Actual: "fallback_reason=" + actual}
		}
	}
	if len(assertion.Signals) > 0 {
		actual := make([]string, 0, len(analysis.Signals))
		for _, signal := range analysis.Signals {
			actual = append(actual, string(signal))
		}
		if !sameStringSet(actual, assertion.Signals) {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "signals=" + strings.Join(assertion.Signals, ","), Actual: "signals=" + strings.Join(actual, ",")}
		}
	}
	if len(assertion.EntityMentions) > 0 {
		actual := make([]string, 0, len(analysis.EntityMentions))
		for _, mention := range analysis.EntityMentions {
			actual = append(actual, mention.EntityID)
		}
		if !sameStringSet(actual, assertion.EntityMentions) {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "entity_mentions=" + strings.Join(assertion.EntityMentions, ","), Actual: "entity_mentions=" + strings.Join(actual, ",")}
		}
	}
	if len(assertion.QueryRewrites) > 0 {
		actual := make([]string, 0, len(analysis.QueryRewrites))
		for _, rewrite := range analysis.QueryRewrites {
			actual = append(actual, rewrite.Text)
		}
		if !sameStringSet(actual, assertion.QueryRewrites) {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "query_rewrites=" + strings.Join(assertion.QueryRewrites, ","), Actual: "query_rewrites=" + strings.Join(actual, ",")}
		}
	}
	if len(assertion.ContextBlockHints) > 0 {
		if !sameStringSet(analysis.ContextBlockHints, assertion.ContextBlockHints) {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "context_block_hints=" + strings.Join(assertion.ContextBlockHints, ","), Actual: "context_block_hints=" + strings.Join(analysis.ContextBlockHints, ",")}
		}
	}
	if assertion.DroppedRewriteCount > 0 {
		actual := 0
		if analysis.Diagnostics != nil {
			actual = analysis.Diagnostics.DroppedRewriteCount
		}
		if actual != assertion.DroppedRewriteCount {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("dropped_rewrite_count=%d", assertion.DroppedRewriteCount), Actual: fmt.Sprintf("dropped_rewrite_count=%d", actual)}
		}
	}
	if len(assertion.DroppedRewriteReasons) > 0 {
		var actual []string
		if analysis.Diagnostics != nil {
			actual = analysis.Diagnostics.DroppedRewriteReasons
		}
		if !sameStringSet(actual, assertion.DroppedRewriteReasons) {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "dropped_rewrite_reasons=" + strings.Join(assertion.DroppedRewriteReasons, ","), Actual: "dropped_rewrite_reasons=" + strings.Join(actual, ",")}
		}
	}
	if assertion.EnglishRewriteCount > 0 {
		actual := 0
		if analysis.Diagnostics != nil {
			actual = analysis.Diagnostics.EnglishRewriteCount
		}
		if actual != assertion.EnglishRewriteCount {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("english_rewrite_count=%d", assertion.EnglishRewriteCount), Actual: fmt.Sprintf("english_rewrite_count=%d", actual)}
		}
	}
	return nil
}

func (s *runState) assertAnchorFusion(assertion Assertion) error {
	result, ok := s.steps[assertion.Step]
	if !ok || result.Retrieval == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "retrieve step " + assertion.Step, Actual: "missing"}
	}
	diagnostics := result.Retrieval.AnchorFusion
	if diagnostics == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "anchor fusion present", Actual: "nil"}
	}
	nodeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	nodeType := defaultString(assertion.NodeType, "fact")
	for _, seed := range diagnostics.Seeds {
		if seed.NodeType != nodeType || seed.NodeID != nodeID {
			continue
		}
		if seed.FusedAnchorScore <= 0 || seed.SeedEnergy <= 0 {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "positive fused score and seed energy", Actual: fmt.Sprintf("score=%f energy=%f", seed.FusedAnchorScore, seed.SeedEnergy)}
		}
		if assertion.Source == "" {
			return nil
		}
		for _, breakdown := range seed.SourceBreakdown {
			if breakdown.Source != assertion.Source {
				continue
			}
			if assertion.Rank > 0 && breakdown.Rank != assertion.Rank {
				return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("source=%s rank=%d", assertion.Source, assertion.Rank), Actual: fmt.Sprintf("rank=%d", breakdown.Rank)}
			}
			return nil
		}
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "source=" + assertion.Source, Actual: anchorSourcesDebug(seed.SourceBreakdown)}
	}
	return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: nodeType + " " + nodeID + " anchor seed", Actual: anchorSeedsDebug(diagnostics)}
}

func (s *runState) assertSelectedRecallAtK(assertion Assertion) error {
	selected, err := s.selectedNodeIDs(assertion)
	if err != nil {
		return err
	}
	relevant, err := s.resolveStrings(assertion.RelevantNodeIDs)
	if err != nil {
		return err
	}
	recall := recallAtK(selected, relevant, assertion.At)
	minimum := assertion.Min
	if recall < minimum {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("recall@%d >= %.3f", normalizedAt(assertion.At, len(selected)), minimum), Actual: fmt.Sprintf("recall=%.3f selected=%s relevant=%s", recall, strings.Join(selected, ","), strings.Join(relevant, ","))}
	}
	return nil
}

func (s *runState) assertContextPrecisionAtK(assertion Assertion) error {
	selected, err := s.selectedNodeIDs(assertion)
	if err != nil {
		return err
	}
	relevant, err := s.resolveStrings(assertion.RelevantNodeIDs)
	if err != nil {
		return err
	}
	precision := precisionAtK(selected, relevant, assertion.At)
	minimum := assertion.Min
	if precision < minimum {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("precision@%d >= %.3f", normalizedAt(assertion.At, len(selected)), minimum), Actual: fmt.Sprintf("precision=%.3f selected=%s relevant=%s", precision, strings.Join(selected, ","), strings.Join(relevant, ","))}
	}
	return nil
}

func (s *runState) assertForbiddenRecallZero(assertion Assertion) error {
	result, err := s.retrievalResult(assertion)
	if err != nil {
		return err
	}
	forbidden, err := s.resolveStrings(assertion.ForbiddenNodeIDs)
	if err != nil {
		return err
	}
	forbiddenSet := stringSet(forbidden)
	var present []string
	for _, block := range result.Blocks {
		for _, item := range block.Items {
			if _, ok := forbiddenSet[item.NodeID]; ok {
				present = append(present, item.NodeID)
			}
			for _, related := range item.RelatedFacts {
				if _, ok := forbiddenSet[related.NodeID]; ok {
					present = append(present, related.NodeID)
				}
			}
			for _, source := range item.SourceRefs {
				if _, ok := forbiddenSet[source.EpisodeID]; ok {
					present = append(present, "source_ref:"+source.EpisodeID)
				}
			}
		}
	}
	if len(present) > 0 {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "forbidden nodes and source refs absent from selected context", Actual: strings.Join(present, ",")}
	}
	return nil
}

func (s *runState) assertBlockContains(assertion Assertion, wantPresent bool) error {
	result, err := s.retrievalResult(assertion)
	if err != nil {
		return err
	}
	nodeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	for _, block := range result.Blocks {
		if assertion.BlockType != "" && !blockTypeMatches(block.BlockType, assertion.BlockType) {
			continue
		}
		for _, item := range block.Items {
			if item.NodeID != nodeID {
				continue
			}
			if !wantPresent {
				return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "node " + nodeID + " absent from block " + assertion.BlockType, Actual: "present"}
			}
			if assertion.Summary != "" && item.Summary != assertion.Summary {
				return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "summary=" + assertion.Summary, Actual: "summary=" + item.Summary}
			}
			return nil
		}
	}
	if wantPresent {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "node " + nodeID + " present in block " + assertion.BlockType, Actual: memoryItemsDebug(result)}
	}
	return nil
}

func (s *runState) assertSelectedChainCorrect(assertion Assertion) error {
	result, err := s.retrievalResult(assertion)
	if err != nil {
		return err
	}
	nodeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	item, ok := findMemoryItem(result, assertion.BlockType, nodeID)
	if !ok {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "selected node " + nodeID + " in block " + assertion.BlockType, Actual: memoryItemsDebug(result)}
	}
	if assertion.HistoricalStatus != "" && item.HistoricalStatus != assertion.HistoricalStatus {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "historical_status=" + assertion.HistoricalStatus, Actual: "historical_status=" + item.HistoricalStatus}
	}
	if assertion.SourceRefCount > 0 && len(item.SourceRefs) != assertion.SourceRefCount {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("source_ref_count=%d", assertion.SourceRefCount), Actual: fmt.Sprintf("source_ref_count=%d", len(item.SourceRefs))}
	}
	relatedIDs, err := s.resolveStrings(assertion.NodeIDs)
	if err != nil {
		return err
	}
	for _, relatedID := range relatedIDs {
		if !hasRelatedFact(item, relatedID, assertion.LinkType, assertion.Direction, assertion.RelatedHistoricalStatus) {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: relatedExpectation(relatedID, assertion.LinkType, assertion.Direction, assertion.RelatedHistoricalStatus), Actual: relatedFactsDebug(item.RelatedFacts)}
		}
	}
	return nil
}

func (s *runState) assertSuppressionEvent(ctx context.Context, assertion Assertion) error {
	nodeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT COALESCE(context_block_type, ''), COALESCE(score_breakdown_json, '')
FROM memory_access_events
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?
  AND access_type = 'suppressed'`, s.persona, defaultString(assertion.NodeType, "fact"), nodeID)
	if err != nil {
		return fmt.Errorf("case %s assertion %s access events: %w", s.caseID, assertion.Type, err)
	}
	defer rows.Close()
	wantReason := assertion.SuppressionReason
	for rows.Next() {
		var blockType string
		var breakdown string
		if err := rows.Scan(&blockType, &breakdown); err != nil {
			return fmt.Errorf("case %s assertion %s access event scan: %w", s.caseID, assertion.Type, err)
		}
		if assertion.BlockType != "" && !blockTypeMatches(blockType, assertion.BlockType) {
			continue
		}
		if wantReason == "" || strings.Contains(breakdown, `"suppression_reason":"`+wantReason+`"`) {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("case %s assertion %s access events: %w", s.caseID, assertion.Type, err)
	}
	return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "suppressed access event reason=" + wantReason, Actual: "missing"}
}

func (s *runState) assertMirrorCandidate(assertion Assertion) error {
	result, err := s.retrievalResult(assertion)
	if err != nil {
		return err
	}
	diagnostics := result.Mirror
	if diagnostics == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "mirror diagnostics present", Actual: "nil"}
	}
	if assertion.Status != "" && diagnostics.Status != assertion.Status {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "status=" + assertion.Status, Actual: "status=" + diagnostics.Status}
	}
	if assertion.FallbackReason != "" && diagnostics.FallbackReason != assertion.FallbackReason {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "fallback_reason=" + assertion.FallbackReason, Actual: "fallback_reason=" + diagnostics.FallbackReason}
	}
	if assertion.QueryCount > 0 && diagnostics.QueryCount != assertion.QueryCount {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("query_count=%d", assertion.QueryCount), Actual: fmt.Sprintf("query_count=%d", diagnostics.QueryCount)}
	}
	if assertion.RawQueryCount > 0 && diagnostics.RawQueryCount != assertion.RawQueryCount {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("raw_query_count=%d", assertion.RawQueryCount), Actual: fmt.Sprintf("raw_query_count=%d", diagnostics.RawQueryCount)}
	}
	if assertion.RewriteQueryCount > 0 && diagnostics.RewriteQueryCount != assertion.RewriteQueryCount {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("rewrite_query_count=%d", assertion.RewriteQueryCount), Actual: fmt.Sprintf("rewrite_query_count=%d", diagnostics.RewriteQueryCount)}
	}
	if assertion.AnchorQueryCount > 0 && diagnostics.AnchorQueryCount != assertion.AnchorQueryCount {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("anchor_query_count=%d", assertion.AnchorQueryCount), Actual: fmt.Sprintf("anchor_query_count=%d", diagnostics.AnchorQueryCount)}
	}
	statusOnly := assertion.NodeID == "" &&
		assertion.Source == "" &&
		assertion.Rank == 0 &&
		assertion.SuppressionReason == ""
	if statusOnly {
		return nil
	}
	nodeID := assertion.NodeID
	if nodeID != "" {
		resolved, err := s.resolveString(nodeID)
		if err != nil {
			return err
		}
		nodeID = resolved
	}
	for _, candidate := range diagnostics.Candidates {
		if nodeID != "" && candidate.SQLiteFactID != nodeID {
			continue
		}
		if assertion.Source != "" && candidate.Source != assertion.Source {
			continue
		}
		if assertion.Rank > 0 && candidate.Rank != assertion.Rank {
			continue
		}
		if assertion.SuppressionReason != "" && candidate.DropReason != assertion.SuppressionReason {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "drop_reason=" + assertion.SuppressionReason, Actual: "drop_reason=" + candidate.DropReason}
		}
		return nil
	}
	return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "mirror candidate " + nodeID, Actual: mirrorCandidatesDebug(diagnostics)}
}

func (s *runState) assertGraphActivationCandidate(assertion Assertion) error {
	result, err := s.retrievalResult(assertion)
	if err != nil {
		return err
	}
	diagnostics := result.GraphActivation
	if diagnostics == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "graph activation diagnostics present", Actual: "nil"}
	}
	if assertion.Status != "" && diagnostics.Status != assertion.Status {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "status=" + assertion.Status, Actual: "status=" + diagnostics.Status}
	}
	statusOnly := assertion.NodeID == "" &&
		assertion.NodeType == "" &&
		assertion.Source == "" &&
		assertion.Rank == 0 &&
		assertion.SuppressionReason == ""
	if statusOnly {
		return nil
	}
	nodeID := assertion.NodeID
	if nodeID != "" {
		resolved, err := s.resolveString(nodeID)
		if err != nil {
			return err
		}
		nodeID = resolved
	}
	for _, candidate := range diagnostics.Candidates {
		if nodeID != "" && candidate.SQLiteNodeID != nodeID {
			continue
		}
		if assertion.NodeType != "" && candidate.NodeType != assertion.NodeType {
			continue
		}
		if assertion.Source != "" && candidate.Source != assertion.Source {
			continue
		}
		if assertion.Rank > 0 && candidate.Rank != assertion.Rank {
			continue
		}
		if assertion.SuppressionReason != "" && candidate.DropReason != assertion.SuppressionReason {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "drop_reason=" + assertion.SuppressionReason, Actual: "drop_reason=" + candidate.DropReason}
		}
		return nil
	}
	return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "graph activation candidate " + nodeID, Actual: graphCandidatesDebug(diagnostics)}
}

func (s *runState) assertRerankStatus(assertion Assertion) error {
	result, err := s.retrievalResult(assertion)
	if err != nil {
		return err
	}
	if result.Rerank == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "rerank diagnostics present", Actual: "nil"}
	}
	if assertion.Status != "" && result.Rerank.Status != assertion.Status {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "status=" + assertion.Status, Actual: "status=" + result.Rerank.Status}
	}
	return nil
}

func (s *runState) assertRerankInput(assertion Assertion) error {
	result, ok := s.steps[assertion.Step]
	if !ok || result.RerankRequest == nil {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "rerank request for step " + assertion.Step, Actual: "missing"}
	}
	actual := map[string]string{}
	for _, candidate := range result.RerankRequest.Candidates {
		actual[candidate.NodeID] = candidate.SafeSummary
	}
	expected, err := s.resolveStrings(assertion.NodeIDs)
	if err != nil {
		return err
	}
	for _, nodeID := range expected {
		if _, ok := actual[nodeID]; !ok {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "rerank input contains " + nodeID, Actual: strings.Join(mapKeys(actual), ",")}
		}
	}
	forbidden, err := s.resolveStrings(assertion.ForbiddenNodeIDs)
	if err != nil {
		return err
	}
	for _, nodeID := range forbidden {
		if _, ok := actual[nodeID]; ok {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "rerank input excludes " + nodeID, Actual: strings.Join(mapKeys(actual), ",")}
		}
	}
	for nodeID, summary := range actual {
		for _, forbiddenText := range assertion.ForbiddenContains {
			if strings.Contains(summary, forbiddenText) {
				return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "rerank summary excludes " + forbiddenText, Actual: nodeID + ":" + summary}
			}
		}
	}
	return nil
}

func (s *runState) assertUnsupportedPremiseNotAsserted(assertion Assertion) error {
	if err := s.assertForbiddenRecallZero(assertion); err != nil {
		return err
	}
	result, err := s.retrievalResult(assertion)
	if err != nil {
		return err
	}
	for _, block := range result.Blocks {
		for _, item := range block.Items {
			for _, forbidden := range assertion.ForbiddenContains {
				if strings.Contains(item.Summary, forbidden) || strings.Contains(item.UsageGuidance, forbidden) {
					return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "context does not assert unsupported premise " + forbidden, Actual: item.NodeID + ":" + item.Summary}
				}
			}
		}
	}
	expected, err := s.resolveStrings(assertion.NodeIDs)
	if err != nil {
		return err
	}
	selected := stringSet(flattenSelectedNodeIDs(result))
	for _, nodeID := range expected {
		if _, ok := selected[nodeID]; !ok {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "counterexample node " + nodeID + " selected", Actual: memoryItemsDebug(result)}
		}
	}
	return nil
}

func (s *runState) assertAblationImproves(assertion Assertion) error {
	selected, err := s.selectedNodeIDs(assertion)
	if err != nil {
		return err
	}
	baselineAssertion := assertion
	baselineAssertion.Step = assertion.CompareStep
	baseline, err := s.selectedNodeIDs(baselineAssertion)
	if err != nil {
		return err
	}
	relevant, err := s.resolveStrings(assertion.RelevantNodeIDs)
	if err != nil {
		return err
	}
	actual := recallAtK(selected, relevant, assertion.At)
	previous := recallAtK(baseline, relevant, assertion.At)
	improvement := actual - previous
	if assertion.Min == 0 {
		if improvement <= 0 {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("recall improvement > 0 over %s", assertion.CompareStep), Actual: fmt.Sprintf("improvement=%.3f current=%.3f baseline=%.3f", improvement, actual, previous)}
		}
		return nil
	}
	if improvement < assertion.Min {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: fmt.Sprintf("recall improvement >= %.3f over %s", assertion.Min, assertion.CompareStep), Actual: fmt.Sprintf("improvement=%.3f current=%.3f baseline=%.3f", improvement, actual, previous)}
	}
	return nil
}

func (s *runState) assertFactCount(ctx context.Context, assertion Assertion) error {
	query := `SELECT COUNT(*) FROM facts WHERE persona_id = ?`
	args := []any{s.persona}
	if assertion.Predicate != "" {
		query += ` AND predicate = ?`
		args = append(args, assertion.Predicate)
	}
	var got int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil {
		return fmt.Errorf("case %s assertion %s count facts: %w", s.caseID, assertion.Type, err)
	}
	actual := fmt.Sprintf("%d", got)
	if actual != assertion.Equals {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "fact_count=" + assertion.Equals, Actual: "fact_count=" + actual}
	}
	return nil
}

func (s *runState) retrievalResult(assertion Assertion) (*memorycore.MemoryContext, error) {
	result, ok := s.steps[assertion.Step]
	if !ok || result.Retrieval == nil {
		return nil, AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "retrieve step " + assertion.Step, Actual: "missing"}
	}
	return result.Retrieval, nil
}

func (s *runState) selectedNodeIDs(assertion Assertion) ([]string, error) {
	result, err := s.retrievalResult(assertion)
	if err != nil {
		return nil, err
	}
	return flattenSelectedNodeIDs(result), nil
}

func (s *runState) resolveStrings(values []string) ([]string, error) {
	resolved := make([]string, 0, len(values))
	for _, value := range values {
		item, err := s.resolveString(value)
		if err != nil {
			return nil, err
		}
		if item != "" {
			resolved = append(resolved, item)
		}
	}
	return resolved, nil
}

func flattenSelectedNodeIDs(context *memorycore.MemoryContext) []string {
	if context == nil {
		return nil
	}
	var ids []string
	for _, block := range context.Blocks {
		for _, item := range block.Items {
			ids = append(ids, item.NodeID)
		}
	}
	return ids
}

func recallAtK(selected []string, relevant []string, at int) float64 {
	if len(relevant) == 0 {
		return 1
	}
	selected = limitStrings(selected, at)
	selectedSet := stringSet(selected)
	hits := 0
	for _, nodeID := range relevant {
		if _, ok := selectedSet[nodeID]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(relevant))
}

func precisionAtK(selected []string, relevant []string, at int) float64 {
	selected = limitStrings(selected, at)
	if len(selected) == 0 {
		if len(relevant) == 0 {
			return 1
		}
		return 0
	}
	relevantSet := stringSet(relevant)
	hits := 0
	for _, nodeID := range selected {
		if _, ok := relevantSet[nodeID]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(selected))
}

func limitStrings(values []string, at int) []string {
	if at <= 0 || at > len(values) {
		return values
	}
	return values[:at]
}

func normalizedAt(at int, length int) int {
	if at <= 0 || at > length {
		return length
	}
	return at
}

func stringSet(values []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	return set
}

func findMemoryItem(context *memorycore.MemoryContext, blockType string, nodeID string) (memorycore.MemoryContextItem, bool) {
	if context == nil {
		return memorycore.MemoryContextItem{}, false
	}
	for _, block := range context.Blocks {
		if blockType != "" && !blockTypeMatches(block.BlockType, blockType) {
			continue
		}
		for _, item := range block.Items {
			if item.NodeID == nodeID {
				return item, true
			}
		}
	}
	return memorycore.MemoryContextItem{}, false
}

func blockTypeMatches(actual string, expected string) bool {
	return canonicalEvalBlockType(actual) == canonicalEvalBlockType(expected)
}

func canonicalEvalBlockType(value string) string {
	switch strings.TrimSpace(value) {
	case "causal_context":
		return memorycore.MemoryBlockTypeRelevantCausalMemory
	case "historical_context":
		return memorycore.MemoryBlockTypeHistoricalTransitionMemory
	case "provenance_context":
		return memorycore.MemoryBlockTypeProvenanceMemory
	case "supportive_context":
		return memorycore.MemoryBlockTypeSupportiveMemory
	default:
		return strings.TrimSpace(value)
	}
}

func hasRelatedFact(item memorycore.MemoryContextItem, nodeID string, linkType string, direction string, historicalStatus string) bool {
	for _, related := range item.RelatedFacts {
		if related.NodeID != nodeID {
			continue
		}
		if linkType != "" && related.LinkType != linkType {
			continue
		}
		if direction != "" && related.Direction != direction {
			continue
		}
		if historicalStatus != "" && related.HistoricalStatus != historicalStatus {
			continue
		}
		return true
	}
	return false
}

func relatedExpectation(nodeID string, linkType string, direction string, historicalStatus string) string {
	parts := []string{"related_node=" + nodeID}
	if linkType != "" {
		parts = append(parts, "link_type="+linkType)
	}
	if direction != "" {
		parts = append(parts, "direction="+direction)
	}
	if historicalStatus != "" {
		parts = append(parts, "historical_status="+historicalStatus)
	}
	return strings.Join(parts, " ")
}

func relatedFactsDebug(items []memorycore.MemoryRelatedFactRef) string {
	if len(items) == 0 {
		return "no related facts"
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, fmt.Sprintf("%s/%s/%s/%s", item.NodeID, item.LinkType, item.Direction, item.HistoricalStatus))
	}
	return strings.Join(parts, ", ")
}

func mirrorCandidatesDebug(diagnostics *memorycore.MirrorRetrievalDiagnostics) string {
	if diagnostics == nil || len(diagnostics.Candidates) == 0 {
		return "no mirror candidates"
	}
	parts := make([]string, 0, len(diagnostics.Candidates))
	for _, candidate := range diagnostics.Candidates {
		parts = append(parts, fmt.Sprintf("%s#%d source=%s drop=%s", candidate.SQLiteFactID, candidate.Rank, candidate.Source, candidate.DropReason))
	}
	return strings.Join(parts, ", ")
}

func graphCandidatesDebug(diagnostics *memorycore.GraphActivationDiagnostics) string {
	if diagnostics == nil || len(diagnostics.Candidates) == 0 {
		return "no graph activation candidates"
	}
	parts := make([]string, 0, len(diagnostics.Candidates))
	for _, candidate := range diagnostics.Candidates {
		parts = append(parts, fmt.Sprintf("%s/%s#%d drop=%s", candidate.NodeType, candidate.SQLiteNodeID, candidate.Rank, candidate.DropReason))
	}
	return strings.Join(parts, ", ")
}

func sameStringSet(actual []string, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	counts := map[string]int{}
	for _, item := range actual {
		counts[item]++
	}
	for _, item := range expected {
		counts[item]--
		if counts[item] < 0 {
			return false
		}
	}
	return true
}

func (s *runState) assertFactColumn(ctx context.Context, assertion Assertion) error {
	if !allowedFactAssertionColumn(assertion.Column) {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "supported fact column", Actual: assertion.Column}
	}
	factID, err := s.resolveString(assertion.FactID)
	if err != nil {
		return err
	}
	actual, err := queryScalarString(ctx, s.db, "SELECT "+assertion.Column+" FROM facts WHERE id = ?", factID)
	if err != nil {
		return fmt.Errorf("case %s assertion %s query fact %s.%s: %w", s.caseID, assertion.Type, factID, assertion.Column, err)
	}
	if actual != assertion.Equals {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: assertion.Column + "=" + assertion.Equals, Actual: assertion.Column + "=" + actual}
	}
	return nil
}

func (s *runState) assertLinkExists(ctx context.Context, assertion Assertion) error {
	fromID, err := s.resolveString(assertion.FromNodeID)
	if err != nil {
		return err
	}
	toID, err := s.resolveString(assertion.ToNodeID)
	if err != nil {
		return err
	}
	fromType := defaultString(assertion.FromNodeType, "fact")
	toType := defaultString(assertion.ToNodeType, "fact")
	var count int
	err = s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_links
WHERE from_node_type = ? AND from_node_id = ?
  AND link_type = ?
  AND to_node_type = ? AND to_node_id = ?`,
		fromType, fromID, assertion.LinkType, toType, toID).Scan(&count)
	if err != nil {
		return fmt.Errorf("case %s assertion %s link query: %w", s.caseID, assertion.Type, err)
	}
	if count != 1 {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "one link", Actual: fmt.Sprintf("count=%d", count)}
	}
	return nil
}

func (s *runState) assertNarrativeExists(ctx context.Context, assertion Assertion) error {
	narrativeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	var summary string
	err = s.db.QueryRowContext(ctx, `SELECT summary FROM narratives WHERE id = ?`, narrativeID).Scan(&summary)
	if err != nil {
		return fmt.Errorf("case %s assertion %s narrative %s: %w", s.caseID, assertion.Type, narrativeID, err)
	}
	if assertion.Summary != "" && summary != assertion.Summary {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "summary=" + assertion.Summary, Actual: "summary=" + summary}
	}
	return nil
}

func (s *runState) assertInsightExists(ctx context.Context, assertion Assertion) error {
	insightID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	var content string
	err = s.db.QueryRowContext(ctx, `SELECT content FROM insights WHERE id = ?`, insightID).Scan(&content)
	if err != nil {
		return fmt.Errorf("case %s assertion %s insight %s: %w", s.caseID, assertion.Type, insightID, err)
	}
	if assertion.Content != "" && content != assertion.Content {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "content=" + assertion.Content, Actual: "content=" + content}
	}
	return nil
}

func (s *runState) assertDerivedLinkCount(ctx context.Context, assertion Assertion) error {
	query := `SELECT COUNT(*) FROM memory_links WHERE link_type = 'DERIVED_FROM'`
	args := []any{}
	if assertion.FromNodeID != "" {
		fromID, err := s.resolveString(assertion.FromNodeID)
		if err != nil {
			return err
		}
		query += ` AND from_node_id = ?`
		args = append(args, fromID)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return fmt.Errorf("case %s assertion %s derived links: %w", s.caseID, assertion.Type, err)
	}
	actual := fmt.Sprintf("%d", count)
	if actual != assertion.Equals {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "derived_link_count=" + assertion.Equals, Actual: "derived_link_count=" + actual}
	}
	return nil
}

func (s *runState) assertSearchAbsent(ctx context.Context, assertion Assertion) error {
	nodeID := assertion.NodeID
	if nodeID != "" {
		resolved, err := s.resolveString(nodeID)
		if err != nil {
			return err
		}
		nodeID = resolved
	}
	nodeType := defaultString(assertion.NodeType, "fact")
	docCount, err := countSearchDocuments(ctx, s.db, s.persona, nodeType, nodeID, assertion.SearchText)
	if err != nil {
		return fmt.Errorf("case %s assertion %s search docs: %w", s.caseID, assertion.Type, err)
	}
	ftsCount, err := countSearchFTS(ctx, s.db, s.persona, nodeType, nodeID, assertion.SearchText)
	if err != nil {
		return fmt.Errorf("case %s assertion %s search fts: %w", s.caseID, assertion.Type, err)
	}
	if docCount != 0 || ftsCount != 0 {
		return AssertionFailure{
			CaseID:    s.caseID,
			Assertion: assertion.Type,
			Expected:  "search rows absent",
			Actual:    fmt.Sprintf("documents=%d fts=%d", docCount, ftsCount),
		}
	}
	return nil
}

func (s *runState) assertDeletionEventSafe(ctx context.Context, assertion Assertion) error {
	eventID := assertion.DeletionEventID
	if eventID == "" && assertion.Step != "" {
		eventID = "$" + assertion.Step + ".deletion_event_id"
	}
	resolved, err := s.resolveString(eventID)
	if err != nil {
		return err
	}
	var payload string
	err = s.db.QueryRowContext(ctx, `
SELECT id || ' ' || persona_id || ' ' || deletion_level || ' ' ||
       target_node_type || ' ' || target_node_id || ' ' ||
       actor || ' ' || reason_code || ' ' ||
       COALESCE(scope_json, '') || ' ' || COALESCE(cascade_summary_json, '')
FROM deletion_events
WHERE id = ?`, resolved).Scan(&payload)
	if err != nil {
		return fmt.Errorf("case %s assertion %s deletion event %s: %w", s.caseID, assertion.Type, resolved, err)
	}
	for _, forbidden := range assertion.ForbiddenContains {
		if strings.Contains(payload, forbidden) {
			return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "deletion_event excludes " + forbidden, Actual: payload}
		}
	}
	return nil
}

func (s *runState) assertEpisodeTombstoneExists(ctx context.Context, assertion Assertion) error {
	episodeID, err := s.resolveString(assertion.EpisodeID)
	if err != nil {
		return err
	}
	var count int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_tombstones WHERE episode_id = ?`, episodeID).Scan(&count)
	if err != nil {
		return fmt.Errorf("case %s assertion %s tombstone query: %w", s.caseID, assertion.Type, err)
	}
	if count != 1 {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "one tombstone for " + episodeID, Actual: fmt.Sprintf("count=%d", count)}
	}
	return nil
}

func (s *runState) assertMirrorIndexStatus(ctx context.Context, assertion Assertion) error {
	nodeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	nodeType := defaultString(assertion.NodeType, "fact")
	actual, err := queryScalarString(ctx, s.db, `
SELECT index_status
FROM memory_index_map
WHERE persona_id = ?
  AND node_type = ?
	AND node_id = ?`, s.persona, nodeType, nodeID)
	if err != nil {
		return fmt.Errorf("case %s assertion %s query mirror index %s/%s: %w", s.caseID, assertion.Type, nodeType, nodeID, err)
	}
	if actual != assertion.Equals {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "mirror_index_status=" + assertion.Equals, Actual: "mirror_index_status=" + actual}
	}
	return nil
}

func (s *runState) assertQueueCount(ctx context.Context, assertion Assertion) error {
	nodeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	query := `
SELECT COUNT(*)
FROM index_sync_queue
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?`
	args := []any{s.persona, defaultString(assertion.NodeType, "fact"), nodeID}
	if assertion.Action != "" {
		query += ` AND operation = ?`
		args = append(args, assertion.Action)
	}
	if assertion.Status != "" {
		query += ` AND status = ?`
		args = append(args, assertion.Status)
	}
	var got int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil {
		return fmt.Errorf("case %s assertion %s queue count: %w", s.caseID, assertion.Type, err)
	}
	actual := fmt.Sprintf("%d", got)
	if actual != assertion.Equals {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "queue_count=" + assertion.Equals, Actual: "queue_count=" + actual}
	}
	return nil
}

func (s *runState) assertQueueStatus(ctx context.Context, assertion Assertion) error {
	nodeID, err := s.resolveString(assertion.NodeID)
	if err != nil {
		return err
	}
	query := `
SELECT status
FROM index_sync_queue
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?`
	args := []any{s.persona, defaultString(assertion.NodeType, "fact"), nodeID}
	if assertion.Action != "" {
		query += ` AND operation = ?`
		args = append(args, assertion.Action)
	}
	query += `
ORDER BY updated_at DESC, created_at DESC
LIMIT 1`
	actual, err := queryScalarString(ctx, s.db, query, args...)
	if err != nil {
		return fmt.Errorf("case %s assertion %s queue status: %w", s.caseID, assertion.Type, err)
	}
	if actual != assertion.Status {
		return AssertionFailure{CaseID: s.caseID, Assertion: assertion.Type, Expected: "queue_status=" + assertion.Status, Actual: "queue_status=" + actual}
	}
	return nil
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func allowedFactAssertionColumn(column string) bool {
	switch column {
	case "validity_status", "visibility_status", "lifecycle_status", "sensitivity_level", "searchable", "pinned", "predicate", "content_summary", "object_literal", "valid_to":
		return true
	default:
		return false
	}
}

func queryScalarString(ctx context.Context, db *sql.DB, query string, args ...any) (string, error) {
	var value any
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return "", err
	}
	switch typed := value.(type) {
	case nil:
		return "", nil
	case []byte:
		return string(typed), nil
	case string:
		return typed, nil
	case int64:
		return fmt.Sprintf("%d", typed), nil
	case float64:
		return fmt.Sprintf("%g", typed), nil
	case bool:
		if typed {
			return "1", nil
		}
		return "0", nil
	default:
		return fmt.Sprintf("%v", typed), nil
	}
}

func countSearchDocuments(ctx context.Context, db *sql.DB, personaID string, nodeType string, nodeID string, searchText string) (int, error) {
	query := `
SELECT COUNT(*)
FROM memory_search_documents
WHERE persona_id = ? AND node_type = ?`
	args := []any{personaID, nodeType}
	if nodeID != "" {
		query += " AND node_id = ?"
		args = append(args, nodeID)
	}
	if searchText != "" {
		query += " AND search_text LIKE ?"
		args = append(args, "%"+searchText+"%")
	}
	var count int
	err := db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func countSearchFTS(ctx context.Context, db *sql.DB, personaID string, nodeType string, nodeID string, searchText string) (int, error) {
	if exists, err := tableExists(ctx, db, "memory_search_fts"); err != nil || !exists {
		return 0, err
	}
	query := `
SELECT COUNT(*)
FROM memory_search_fts
WHERE persona_id = ? AND node_type = ?`
	args := []any{personaID, nodeType}
	if nodeID != "" {
		query += " AND node_id = ?"
		args = append(args, nodeID)
	}
	if searchText != "" {
		query += " AND search_text LIKE ?"
		args = append(args, "%"+searchText+"%")
	}
	var count int
	err := db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_master
WHERE name = ?`, name).Scan(&count)
	return count > 0, err
}

func memoryItemsDebug(context *memorycore.MemoryContext) string {
	if context == nil {
		return "<nil>"
	}
	var parts []string
	for _, block := range context.Blocks {
		for _, item := range block.Items {
			parts = append(parts, item.NodeID+":"+item.Summary)
		}
	}
	if len(parts) == 0 {
		return "no memory items"
	}
	return strings.Join(parts, ", ")
}

func anchorSeedsDebug(diagnostics *memorycore.AnchorFusionDiagnostics) string {
	if diagnostics == nil || len(diagnostics.Seeds) == 0 {
		return "no anchor seeds"
	}
	parts := make([]string, 0, len(diagnostics.Seeds))
	for _, seed := range diagnostics.Seeds {
		parts = append(parts, seed.NodeType+":"+seed.NodeID)
	}
	return strings.Join(parts, ", ")
}

func anchorSourcesDebug(breakdown []memorycore.AnchorSourceBreakdown) string {
	if len(breakdown) == 0 {
		return "no sources"
	}
	parts := make([]string, 0, len(breakdown))
	for _, source := range breakdown {
		parts = append(parts, fmt.Sprintf("%s#%d", source.Source, source.Rank))
	}
	return strings.Join(parts, ", ")
}
