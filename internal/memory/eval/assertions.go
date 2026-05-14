package eval

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
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
