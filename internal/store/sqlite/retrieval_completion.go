package sqlite

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	maxCompletionCandidates          = 12
	maxCompletionDiscoveryCandidates = maxCompletionCandidates * 2
	chainCompletionBonus             = 0.18
	causalChainCompletionBonus       = 0.36
	outcomeCompletionBonus           = 0.22
	premiseCounterexampleBonus       = 0.20
	positiveWorkCounterexampleBonus  = 0.25
	completionSourceHistorical       = "historical_transition"
	completionSourceCausal           = "causal_chain"
	completionSourceEventBundle      = "event_bundle"
	completionSourceNarrative        = "narrative_insight"
	completionSourcePremiseCheck     = "premise_counterexample"
	completionSourceProvenance       = "provenance_source"
)

type retrievalCompletionCandidate struct {
	factID   string
	source   string
	linkType string
	bonus    float64
	priority float64
}

func (r *RetrievalRepository) completeLinkedCandidates(ctx context.Context, req RetrievalRequest, query QueryAnalysis, policy RetrievalPolicy, scored []scoredFact) (map[string]retrievalCandidate, error) {
	acc := map[string]retrievalCompletionCandidate{}
	sourceFactIDs := selectableScoredFactIDs(scored)
	if len(sourceFactIDs) > 0 {
		if queryAllowsHistoricalTransitionCompletion(query, policy) {
			if err := r.addLinkedFactCompletions(ctx, req.PersonaID, sourceFactIDs, []string{"SUPERSEDES"}, true, completionSourceHistorical, chainCompletionBonus, acc); err != nil {
				return nil, err
			}
		}
		if queryAllowsEventBundleCompletion(query) {
			if err := r.addEventBundleCompletions(ctx, req.PersonaID, sourceFactIDs, policy, completionSourceEventBundle, chainCompletionBonus, acc); err != nil {
				return nil, err
			}
		}
		if queryAllowsCausalCompletion(query) {
			if err := r.addLinkedFactCompletions(ctx, req.PersonaID, sourceFactIDs, []string{"CAUSED_BY", "EXPLAINS", "TRIGGERED_BY"}, false, completionSourceCausal, causalChainCompletionBonus, acc); err != nil {
				return nil, err
			}
		}
		if queryAllowsProvenanceCompletion(query) {
			if err := r.addProvenanceSourceCompletions(ctx, req.PersonaID, provenanceTargetScoredFactIDs(query, scored), policy, completionSourceProvenance, chainCompletionBonus, acc); err != nil {
				return nil, err
			}
			pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
		}
	}
	if queryAllowsNarrativeInsightAnchors(query) {
		if err := r.addNarrativeInsightCompletions(ctx, req.PersonaID, query, policy, acc); err != nil {
			return nil, err
		}
	}
	return r.authorizedCompletionCandidates(ctx, req, policy, acc)
}

func selectableScoredFactIDs(scored []scoredFact) []string {
	ids := make([]string, 0, len(scored))
	for _, candidate := range scored {
		if candidate.Suppressed || candidate.Fact.ID == "" {
			continue
		}
		ids = append(ids, candidate.Fact.ID)
	}
	return uniqueSortedStrings(ids)
}

func provenanceTargetScoredFactIDs(query QueryAnalysis, scored []scoredFact) []string {
	ids := make([]string, 0, len(scored))
	for _, candidate := range scored {
		if candidate.Suppressed || candidate.Fact.ID == "" {
			continue
		}
		factText := strings.Join(nonEmptyStrings(
			candidate.Fact.ContentSummary,
			string(candidate.Fact.Predicate),
			stringFromPtr(candidate.Fact.ObjectLiteral),
		), " ")
		if candidate.Breakdown.LexicalCoverage <= 0 && textMatchScore(query, factText) <= 0 {
			continue
		}
		ids = append(ids, candidate.Fact.ID)
	}
	return uniqueSortedStrings(ids)
}

func queryAllowsHistoricalTransitionCompletion(query QueryAnalysis, policy RetrievalPolicy) bool {
	if !policy.AllowHistorical {
		return false
	}
	if queryWantsPremiseCounterexample(query) {
		return false
	}
	return query.EvidenceNeed == EvidenceNeedStateTransition ||
		hasQuerySignal(query, QuerySignalStateTransition)
}

func (r *RetrievalRepository) ensureSelectedHistoricalSupersedesCompletions(ctx context.Context, req RetrievalRequest, query QueryAnalysis, policy RetrievalPolicy, now time.Time, selected []scoredFact, scored []scoredFact) ([]scoredFact, error) {
	if !queryAllowsHistoricalTransitionCompletion(query, policy) || len(selected) == 0 {
		return selected, nil
	}
	personaID := req.PersonaID
	if personaID == "" {
		personaID = selected[0].Fact.PersonaID
	}
	var sourceFactIDs []string
	for _, candidate := range selected {
		if historicalSupersedesSourceEligible(candidate) {
			sourceFactIDs = append(sourceFactIDs, candidate.Fact.ID)
		}
	}
	sourceFactIDs = uniqueSortedStrings(sourceFactIDs)
	if len(sourceFactIDs) == 0 {
		return selected, nil
	}
	targetsBySource, err := r.outboundSupersedesTargetsBySource(ctx, personaID, sourceFactIDs)
	if err != nil {
		return nil, err
	}
	if len(targetsBySource) == 0 {
		return selected, nil
	}
	scoredByFact := scoredFactByID(scored)
	selectedByFact := selectedFactIDSet(selected)
	missingCandidates := map[string]retrievalCandidate{}
	for _, sourceFactID := range sourceFactIDs {
		for _, targetFactID := range targetsBySource[sourceFactID] {
			if _, ok := selectedByFact[targetFactID]; ok {
				continue
			}
			if _, ok := scoredByFact[targetFactID]; ok {
				continue
			}
			missingCandidates[targetFactID] = retrievalCandidate{
				FactID:             targetFactID,
				CompletionSource:   completionSourceHistorical,
				CompletionLinkType: "SUPERSEDES",
				CompletionBonus:    chainCompletionBonus,
			}
		}
	}
	if len(missingCandidates) > 0 {
		extraScored, _, err := r.scoreCandidates(ctx, req, query, policy, now, missingCandidates)
		if err != nil {
			return nil, err
		}
		for _, candidate := range extraScored {
			if candidate.Suppressed || candidate.Fact.ID == "" {
				continue
			}
			scoredByFact[candidate.Fact.ID] = candidate
		}
	}
	for _, sourceFactID := range sourceFactIDs {
		for _, targetFactID := range targetsBySource[sourceFactID] {
			if _, ok := selectedByFact[targetFactID]; ok {
				continue
			}
			candidate, ok := scoredByFact[targetFactID]
			if !ok || candidate.Suppressed {
				continue
			}
			next, inserted := insertSelectedHistoricalCompletion(selected, candidate, sourceFactID, policy.FinalMemoryCount)
			if !inserted {
				continue
			}
			selected = next
			selectedByFact = selectedFactIDSet(selected)
		}
	}
	return selected, nil
}

func historicalSupersedesSourceEligible(candidate scoredFact) bool {
	if candidate.Fact.ID == "" {
		return false
	}
	if candidate.Breakdown.LexicalCoverage > 0 || candidate.Breakdown.SlotCoverage > 0 {
		return true
	}
	if candidate.Breakdown.CompletionSource == completionSourceHistorical {
		return true
	}
	for _, source := range candidate.SourceBreakdown {
		switch source.Source {
		case AnchorSourceRecentImportant, AnchorSourcePinnedCore, AnchorSourceNarrativeInsight:
			continue
		default:
			return true
		}
	}
	return false
}

func (r *RetrievalRepository) outboundSupersedesTargetsBySource(ctx context.Context, personaID string, sourceFactIDs []string) (map[string][]string, error) {
	result := map[string][]string{}
	for _, chunk := range chunkedIDs(sourceFactIDs, sqliteInChunkSize) {
		args := stringArgs(personaID, chunk)
		rows, err := r.db.QueryContext(ctx, `
SELECT from_node_id, to_node_id
FROM memory_links
WHERE persona_id = ?
  AND link_type = 'SUPERSEDES'
  AND visibility_status = 'visible'
  AND searchable = 1
  AND from_node_type = 'fact'
  AND to_node_type = 'fact'
  AND from_node_id IN (`+placeholders(len(chunk))+`)
ORDER BY from_node_id ASC, ABS(weight) DESC, created_at ASC, id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sourceFactID, targetFactID string
			if err := rows.Scan(&sourceFactID, &targetFactID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			result[sourceFactID] = append(result[sourceFactID], targetFactID)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func scoredFactByID(scored []scoredFact) map[string]scoredFact {
	result := make(map[string]scoredFact, len(scored))
	for _, candidate := range scored {
		if candidate.Fact.ID == "" {
			continue
		}
		result[candidate.Fact.ID] = candidate
	}
	return result
}

func selectedFactIDSet(selected []scoredFact) map[string]struct{} {
	result := make(map[string]struct{}, len(selected))
	for _, candidate := range selected {
		if candidate.Fact.ID == "" {
			continue
		}
		result[candidate.Fact.ID] = struct{}{}
	}
	return result
}

func insertSelectedHistoricalCompletion(selected []scoredFact, candidate scoredFact, sourceFactID string, limit int) ([]scoredFact, bool) {
	if candidate.Fact.ID == "" {
		return selected, false
	}
	if limit <= 0 {
		limit = len(selected) + 1
	}
	if len(selected) >= limit {
		evictIndex := historicalCompletionEvictionIndex(selected, sourceFactID)
		if evictIndex < 0 {
			return selected, false
		}
		selected = removeScoredFactAt(selected, evictIndex)
	}
	insertIndex := len(selected)
	for index, item := range selected {
		if item.Fact.ID == sourceFactID {
			insertIndex = index + 1
			break
		}
	}
	selected = append(selected, scoredFact{})
	copy(selected[insertIndex+1:], selected[insertIndex:])
	selected[insertIndex] = candidate
	return selected, true
}

func historicalCompletionEvictionIndex(selected []scoredFact, sourceFactID string) int {
	for index := len(selected) - 1; index >= 0; index-- {
		factID := selected[index].Fact.ID
		if factID == "" || factID == sourceFactID {
			continue
		}
		if selected[index].Breakdown.CompletionSource == completionSourceHistorical {
			continue
		}
		return index
	}
	return -1
}

func queryAllowsEventBundleCompletion(query QueryAnalysis) bool {
	return hasQuerySignal(query, QuerySignalPastEventDirectFact) ||
		hasQuerySignal(query, QuerySignalEventBundle)
}

func queryAllowsCausalCompletion(query QueryAnalysis) bool {
	return query.MemoryAbility == MemoryAbilityCausalExplain ||
		hasQuerySignal(query, QuerySignalCausal) ||
		hasQuerySignal(query, QuerySignalCausalChain)
}

func queryAllowsProvenanceCompletion(query QueryAnalysis) bool {
	return query.EvidenceNeed == EvidenceNeedProvenanceSource ||
		hasQuerySignal(query, QuerySignalProvenanceSource) ||
		hasQuerySignal(query, QuerySignalProvenance)
}

func (r *RetrievalRepository) addLinkedFactCompletions(ctx context.Context, personaID string, sourceFactIDs []string, linkTypes []string, outboundOnly bool, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	for _, chunk := range chunkedIDs(sourceFactIDs, sqliteInChunkSize) {
		if err := r.addOutboundFactLinkCompletions(ctx, personaID, chunk, linkTypes, source, bonus, acc); err != nil {
			return err
		}
		if outboundOnly {
			continue
		}
		if err := r.addInboundFactLinkCompletions(ctx, personaID, chunk, linkTypes, source, bonus, acc); err != nil {
			return err
		}
	}
	return nil
}

func (r *RetrievalRepository) addOutboundFactLinkCompletions(ctx context.Context, personaID string, sourceFactIDs []string, linkTypes []string, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	args := completionQueryArgs(personaID, linkTypes, sourceFactIDs)
	rows, err := r.db.QueryContext(ctx, `
SELECT link_type, from_node_id, to_node_id
FROM memory_links
WHERE persona_id = ?
  AND link_type IN (`+placeholders(len(linkTypes))+`)
  AND visibility_status = 'visible'
  AND searchable = 1
  AND from_node_type = 'fact'
  AND to_node_type = 'fact'
  AND from_node_id IN (`+placeholders(len(sourceFactIDs))+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var linkType, fromFactID, toFactID string
		if err := rows.Scan(&linkType, &fromFactID, &toFactID); err != nil {
			return err
		}
		addRetrievalCompletion(acc, toFactID, source, linkType, bonus, 0)
	}
	return rows.Err()
}

func (r *RetrievalRepository) addInboundFactLinkCompletions(ctx context.Context, personaID string, sourceFactIDs []string, linkTypes []string, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	args := completionQueryArgs(personaID, linkTypes, sourceFactIDs)
	rows, err := r.db.QueryContext(ctx, `
SELECT link_type, from_node_id, to_node_id
FROM memory_links
WHERE persona_id = ?
  AND link_type IN (`+placeholders(len(linkTypes))+`)
  AND visibility_status = 'visible'
  AND searchable = 1
  AND from_node_type = 'fact'
  AND to_node_type = 'fact'
  AND to_node_id IN (`+placeholders(len(sourceFactIDs))+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var linkType, fromFactID, toFactID string
		if err := rows.Scan(&linkType, &fromFactID, &toFactID); err != nil {
			return err
		}
		addRetrievalCompletion(acc, fromFactID, source, linkType, bonus, 0)
	}
	return rows.Err()
}

func completionQueryArgs(personaID string, linkTypes []string, factIDs []string) []any {
	args := make([]any, 0, 1+len(linkTypes)+len(factIDs))
	args = append(args, personaID)
	for _, linkType := range linkTypes {
		args = append(args, linkType)
	}
	for _, factID := range factIDs {
		args = append(args, factID)
	}
	return args
}

func completionDiscoveryRemaining(acc map[string]retrievalCompletionCandidate) int {
	remaining := maxCompletionDiscoveryCandidates - len(acc)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (r *RetrievalRepository) addBoundedLinkedFactCompletions(ctx context.Context, personaID string, sourceFactIDs []string, linkTypes []string, outboundOnly bool, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	for _, chunk := range chunkedIDs(sourceFactIDs, sqliteInChunkSize) {
		if err := r.addBoundedOutboundFactLinkCompletions(ctx, personaID, chunk, linkTypes, source, bonus, acc); err != nil {
			return err
		}
		pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
		if outboundOnly || completionDiscoveryRemaining(acc) <= 0 {
			continue
		}
		if err := r.addBoundedInboundFactLinkCompletions(ctx, personaID, chunk, linkTypes, source, bonus, acc); err != nil {
			return err
		}
		pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
		if completionDiscoveryRemaining(acc) <= 0 {
			return nil
		}
	}
	return nil
}

func (r *RetrievalRepository) addBoundedOutboundFactLinkCompletions(ctx context.Context, personaID string, sourceFactIDs []string, linkTypes []string, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	limit := completionDiscoveryRemaining(acc)
	if limit <= 0 {
		return nil
	}
	args := completionQueryArgs(personaID, linkTypes, sourceFactIDs)
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, `
SELECT link_type, to_node_id
FROM memory_links
WHERE persona_id = ?
  AND link_type IN (`+placeholders(len(linkTypes))+`)
  AND visibility_status = 'visible'
  AND searchable = 1
  AND from_node_type = 'fact'
  AND to_node_type = 'fact'
  AND from_node_id IN (`+placeholders(len(sourceFactIDs))+`)
ORDER BY link_type ASC, to_node_id ASC
LIMIT ?`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var linkType, toFactID string
		if err := rows.Scan(&linkType, &toFactID); err != nil {
			return err
		}
		addRetrievalCompletion(acc, toFactID, source, linkType, bonus, 0)
		if len(acc) >= maxCompletionDiscoveryCandidates {
			break
		}
	}
	return rows.Err()
}

func (r *RetrievalRepository) addBoundedInboundFactLinkCompletions(ctx context.Context, personaID string, sourceFactIDs []string, linkTypes []string, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	limit := completionDiscoveryRemaining(acc)
	if limit <= 0 {
		return nil
	}
	args := completionQueryArgs(personaID, linkTypes, sourceFactIDs)
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, `
SELECT link_type, from_node_id
FROM memory_links
WHERE persona_id = ?
  AND link_type IN (`+placeholders(len(linkTypes))+`)
  AND visibility_status = 'visible'
  AND searchable = 1
  AND from_node_type = 'fact'
  AND to_node_type = 'fact'
  AND to_node_id IN (`+placeholders(len(sourceFactIDs))+`)
ORDER BY link_type ASC, from_node_id ASC
LIMIT ?`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var linkType, fromFactID string
		if err := rows.Scan(&linkType, &fromFactID); err != nil {
			return err
		}
		addRetrievalCompletion(acc, fromFactID, source, linkType, bonus, 0)
		if len(acc) >= maxCompletionDiscoveryCandidates {
			break
		}
	}
	return rows.Err()
}

func (r *RetrievalRepository) addProvenanceSourceCompletions(ctx context.Context, personaID string, sourceFactIDs []string, policy RetrievalPolicy, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	for _, chunk := range chunkedIDs(sourceFactIDs, sqliteInChunkSize) {
		limit := completionDiscoveryRemaining(acc)
		if limit <= 0 {
			return nil
		}
		args := stringArgs(personaID, chunk)
		args = append(args, allowedSensitivityRank, limit)
		rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT l.from_node_id
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.from_node_id IN (`+placeholders(len(chunk))+`)
  AND l.link_type = 'EVIDENCED_BY'
  AND l.to_node_type = 'episode'
  AND l.visibility_status = 'visible'
  AND l.searchable = 1
  AND e.visibility_status = 'visible'
  AND e.searchable = 1
  AND CASE e.sensitivity_level
      WHEN 'normal' THEN 0
      WHEN 'sensitive' THEN 1
      WHEN 'highly_sensitive' THEN 2
      ELSE 3
  END <= ?
ORDER BY l.from_node_id ASC
LIMIT ?`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var factID string
			if err := rows.Scan(&factID); err != nil {
				_ = rows.Close()
				return err
			}
			addRetrievalCompletion(acc, factID, source, "EVIDENCED_BY", bonus, 0)
			if len(acc) >= maxCompletionDiscoveryCandidates {
				break
			}
		}
		pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (r *RetrievalRepository) addEventBundleCompletions(ctx context.Context, personaID string, sourceFactIDs []string, policy RetrievalPolicy, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	if err := r.addSharedEpisodeFactCompletions(ctx, personaID, sourceFactIDs, policy, source, bonus, acc); err != nil {
		return err
	}
	pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
	if err := r.addSameSessionWindowFactCompletions(ctx, personaID, sourceFactIDs, policy, source, bonus, acc); err != nil {
		return err
	}
	pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
	if err := r.addBoundedLinkedFactCompletions(ctx, personaID, sourceFactIDs, []string{"ABOUT_ENTITY", "CO_OCCURS_WITH"}, false, source, bonus, acc); err != nil {
		return err
	}
	pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
	if err := r.addSharedEventEntityLinkCompletions(ctx, personaID, sourceFactIDs, policy, source, bonus, acc); err != nil {
		return err
	}
	pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
	return nil
}

func (r *RetrievalRepository) addSharedEpisodeFactCompletions(ctx context.Context, personaID string, sourceFactIDs []string, policy RetrievalPolicy, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	for _, chunk := range chunkedIDs(sourceFactIDs, sqliteInChunkSize) {
		limit := completionDiscoveryRemaining(acc)
		if limit <= 0 {
			return nil
		}
		args := stringArgs(personaID, chunk)
		args = append(args, allowedSensitivityRank, limit)
		rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT other.from_node_id
FROM memory_links selected
JOIN episodes source_episode
  ON source_episode.persona_id = selected.persona_id
 AND source_episode.id = selected.to_node_id
JOIN memory_links other
  ON other.persona_id = selected.persona_id
 AND other.link_type = 'EVIDENCED_BY'
 AND other.to_node_type = 'episode'
 AND other.to_node_id = selected.to_node_id
WHERE selected.persona_id = ?
  AND selected.from_node_type = 'fact'
  AND selected.from_node_id IN (`+placeholders(len(chunk))+`)
  AND selected.link_type = 'EVIDENCED_BY'
  AND selected.to_node_type = 'episode'
  AND selected.visibility_status = 'visible'
  AND selected.searchable = 1
  AND source_episode.visibility_status = 'visible'
  AND source_episode.searchable = 1
  AND CASE source_episode.sensitivity_level
      WHEN 'normal' THEN 0
      WHEN 'sensitive' THEN 1
      WHEN 'highly_sensitive' THEN 2
      ELSE 3
  END <= ?
  AND other.from_node_type = 'fact'
  AND other.from_node_id != selected.from_node_id
  AND other.visibility_status = 'visible'
  AND other.searchable = 1
ORDER BY other.from_node_id ASC
LIMIT ?`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var factID string
			if err := rows.Scan(&factID); err != nil {
				_ = rows.Close()
				return err
			}
			addRetrievalCompletion(acc, factID, source, "EVIDENCED_BY", bonus, 0)
			if len(acc) >= maxCompletionDiscoveryCandidates {
				break
			}
		}
		pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (r *RetrievalRepository) addSameSessionWindowFactCompletions(ctx context.Context, personaID string, sourceFactIDs []string, policy RetrievalPolicy, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	for _, chunk := range chunkedIDs(sourceFactIDs, sqliteInChunkSize) {
		limit := completionDiscoveryRemaining(acc)
		if limit <= 0 {
			return nil
		}
		args := stringArgs(personaID, chunk)
		args = append(args, allowedSensitivityRank, allowedSensitivityRank, limit)
		rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT other_link.from_node_id
FROM memory_links selected_link
JOIN episodes selected_episode
  ON selected_episode.persona_id = selected_link.persona_id
 AND selected_episode.id = selected_link.to_node_id
JOIN episodes other_episode
  ON other_episode.persona_id = selected_episode.persona_id
 AND other_episode.session_id = selected_episode.session_id
 AND ABS(strftime('%s', other_episode.occurred_at) - strftime('%s', selected_episode.occurred_at)) <= 7200
JOIN memory_links other_link
  ON other_link.persona_id = other_episode.persona_id
 AND other_link.link_type = 'EVIDENCED_BY'
 AND other_link.to_node_type = 'episode'
 AND other_link.to_node_id = other_episode.id
WHERE selected_link.persona_id = ?
  AND selected_link.from_node_type = 'fact'
  AND selected_link.from_node_id IN (`+placeholders(len(chunk))+`)
  AND selected_link.link_type = 'EVIDENCED_BY'
  AND selected_link.to_node_type = 'episode'
  AND selected_link.visibility_status = 'visible'
  AND selected_link.searchable = 1
  AND selected_episode.visibility_status = 'visible'
  AND selected_episode.searchable = 1
  AND CASE selected_episode.sensitivity_level
      WHEN 'normal' THEN 0
      WHEN 'sensitive' THEN 1
      WHEN 'highly_sensitive' THEN 2
      ELSE 3
  END <= ?
  AND other_episode.visibility_status = 'visible'
  AND other_episode.searchable = 1
  AND CASE other_episode.sensitivity_level
      WHEN 'normal' THEN 0
      WHEN 'sensitive' THEN 1
      WHEN 'highly_sensitive' THEN 2
      ELSE 3
  END <= ?
  AND other_link.from_node_type = 'fact'
  AND other_link.from_node_id != selected_link.from_node_id
  AND other_link.visibility_status = 'visible'
  AND other_link.searchable = 1
ORDER BY other_link.from_node_id ASC
LIMIT ?`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var factID string
			if err := rows.Scan(&factID); err != nil {
				_ = rows.Close()
				return err
			}
			addRetrievalCompletion(acc, factID, source, "session_window", bonus, 0)
			if len(acc) >= maxCompletionDiscoveryCandidates {
				break
			}
		}
		pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (r *RetrievalRepository) addSharedEventEntityLinkCompletions(ctx context.Context, personaID string, sourceFactIDs []string, policy RetrievalPolicy, source string, bonus float64, acc map[string]retrievalCompletionCandidate) error {
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	for _, chunk := range chunkedIDs(sourceFactIDs, sqliteInChunkSize) {
		limit := completionDiscoveryRemaining(acc)
		if limit <= 0 {
			return nil
		}
		args := make([]any, 0, 12+len(chunk)*2)
		args = append(args, personaID, "ABOUT_ENTITY", "CO_OCCURS_WITH")
		for _, factID := range chunk {
			args = append(args, factID)
		}
		for _, factID := range chunk {
			args = append(args, factID)
		}
		args = append(args, personaID, personaID, allowedSensitivityRank, allowedSensitivityRank)
		args = append(args, personaID, personaID, personaID, allowedSensitivityRank, allowedSensitivityRank)
		args = append(args, limit)
		rows, err := r.db.QueryContext(ctx, `
WITH selected_links AS (
  SELECT link_type,
         CASE WHEN from_node_type = 'fact' THEN to_node_type ELSE from_node_type END AS anchor_node_type,
         CASE WHEN from_node_type = 'fact' THEN to_node_id ELSE from_node_id END AS anchor_node_id,
         CASE WHEN from_node_type = 'fact' THEN from_node_id ELSE to_node_id END AS fact_id
  FROM memory_links
  WHERE persona_id = ?
    AND link_type IN (`+placeholders(2)+`)
    AND visibility_status = 'visible'
    AND searchable = 1
    AND (
      (from_node_type = 'fact' AND from_node_id IN (`+placeholders(len(chunk))+`))
      OR
      (to_node_type = 'fact' AND to_node_id IN (`+placeholders(len(chunk))+`))
    )
),
authorized_selected_links AS (
  SELECT selected.*
  FROM selected_links selected
  LEFT JOIN entities selected_entity
    ON selected_entity.persona_id = ?
   AND selected.anchor_node_type = 'entity'
   AND selected_entity.id = selected.anchor_node_id
  LEFT JOIN episodes selected_episode
    ON selected_episode.persona_id = ?
   AND selected.anchor_node_type = 'episode'
   AND selected_episode.id = selected.anchor_node_id
  WHERE (
    selected.anchor_node_type = 'entity'
    AND selected_entity.visibility_status = 'visible'
    AND selected_entity.searchable = 1
    AND CASE selected_entity.sensitivity_level
        WHEN 'normal' THEN 0
        WHEN 'sensitive' THEN 1
        WHEN 'highly_sensitive' THEN 2
        ELSE 3
    END <= ?
  ) OR (
    selected.anchor_node_type = 'episode'
    AND selected_episode.visibility_status = 'visible'
    AND selected_episode.searchable = 1
    AND CASE selected_episode.sensitivity_level
        WHEN 'normal' THEN 0
        WHEN 'sensitive' THEN 1
        WHEN 'highly_sensitive' THEN 2
        ELSE 3
    END <= ?
  )
),
other_links AS (
  SELECT DISTINCT other.link_type,
         selected.anchor_node_type,
         selected.anchor_node_id,
         CASE WHEN other.from_node_type = 'fact' THEN other.from_node_id ELSE other.to_node_id END AS fact_id
  FROM authorized_selected_links selected
  JOIN memory_links other
    ON other.persona_id = ?
   AND other.link_type = selected.link_type
   AND other.visibility_status = 'visible'
   AND other.searchable = 1
   AND (
     (other.from_node_type = 'fact'
      AND other.to_node_type = selected.anchor_node_type
      AND other.to_node_id = selected.anchor_node_id)
     OR
     (other.to_node_type = 'fact'
      AND other.from_node_type = selected.anchor_node_type
      AND other.from_node_id = selected.anchor_node_id)
   )
  WHERE (CASE WHEN other.from_node_type = 'fact' THEN other.from_node_id ELSE other.to_node_id END) != selected.fact_id
),
authorized_other_links AS (
  SELECT other_links.*
  FROM other_links
  LEFT JOIN entities other_entity
    ON other_entity.persona_id = ?
   AND other_links.anchor_node_type = 'entity'
   AND other_entity.id = other_links.anchor_node_id
  LEFT JOIN episodes other_episode
    ON other_episode.persona_id = ?
   AND other_links.anchor_node_type = 'episode'
   AND other_episode.id = other_links.anchor_node_id
  WHERE (
    other_links.anchor_node_type = 'entity'
    AND other_entity.visibility_status = 'visible'
    AND other_entity.searchable = 1
    AND CASE other_entity.sensitivity_level
        WHEN 'normal' THEN 0
        WHEN 'sensitive' THEN 1
        WHEN 'highly_sensitive' THEN 2
        ELSE 3
    END <= ?
  ) OR (
    other_links.anchor_node_type = 'episode'
    AND other_episode.visibility_status = 'visible'
    AND other_episode.searchable = 1
    AND CASE other_episode.sensitivity_level
        WHEN 'normal' THEN 0
        WHEN 'sensitive' THEN 1
        WHEN 'highly_sensitive' THEN 2
        ELSE 3
    END <= ?
  )
)
SELECT DISTINCT other.link_type, other.fact_id
FROM authorized_selected_links selected
JOIN authorized_other_links other
  ON other.link_type = selected.link_type
 AND other.anchor_node_type = selected.anchor_node_type
 AND other.anchor_node_id = selected.anchor_node_id
 AND other.fact_id != selected.fact_id
ORDER BY other.link_type ASC, other.fact_id ASC
LIMIT ?`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var linkType string
			var factID string
			if err := rows.Scan(&linkType, &factID); err != nil {
				_ = rows.Close()
				return err
			}
			addRetrievalCompletion(acc, factID, source, linkType, bonus, 0)
			if len(acc) >= maxCompletionDiscoveryCandidates {
				break
			}
		}
		pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (r *RetrievalRepository) addNarrativeInsightCompletions(ctx context.Context, personaID string, query QueryAnalysis, policy RetrievalPolicy, acc map[string]retrievalCompletionCandidate) error {
	docs, err := r.listNarrativeInsightSearchDocuments(ctx, personaID, 30)
	if err != nil {
		return err
	}
	type anchor struct {
		nodeType core.NodeType
		nodeID   string
		score    float64
	}
	var anchors []anchor
	for _, doc := range docs {
		if !searchDocumentAuthorityAllows(doc, policy) {
			continue
		}
		score := textMatchScore(query, doc.SearchText)
		if score <= 0 {
			continue
		}
		anchors = append(anchors, anchor{nodeType: doc.NodeType, nodeID: doc.NodeID, score: score})
	}
	sort.SliceStable(anchors, func(i, j int) bool {
		if anchors[i].score == anchors[j].score {
			if anchors[i].nodeType == anchors[j].nodeType {
				return anchors[i].nodeID < anchors[j].nodeID
			}
			return anchors[i].nodeType < anchors[j].nodeType
		}
		return anchors[i].score > anchors[j].score
	})
	for _, anchor := range anchors {
		if len(acc) >= maxCompletionDiscoveryCandidates {
			break
		}
		remaining := maxCompletionDiscoveryCandidates - len(acc)
		if remaining < maxCompletionCandidates {
			remaining = maxCompletionCandidates
		}
		if err := r.addNarrativeInsightFactLinks(ctx, personaID, anchor.nodeType, anchor.nodeID, anchor.score, remaining, acc); err != nil {
			return err
		}
		pruneRetrievalCompletions(acc, maxCompletionDiscoveryCandidates)
	}
	return nil
}

func (r *RetrievalRepository) addNarrativeInsightFactLinks(ctx context.Context, personaID string, nodeType core.NodeType, nodeID string, anchorScore float64, limit int, acc map[string]retrievalCompletionCandidate) error {
	if limit <= 0 {
		return nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT link_type, from_node_type, from_node_id, to_node_type, to_node_id, weight
FROM memory_links
WHERE persona_id = ?
  AND link_type IN ('DERIVED_FROM', 'SUPPORTS', 'ABOUT_ENTITY')
  AND visibility_status = 'visible'
  AND searchable = 1
  AND (
    (from_node_type = ? AND from_node_id = ? AND to_node_type = 'fact')
    OR
    (to_node_type = ? AND to_node_id = ? AND from_node_type = 'fact')
  )
ORDER BY ABS(weight) DESC, created_at ASC, id ASC
LIMIT ?`, personaID, string(nodeType), nodeID, string(nodeType), nodeID, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var linkType, fromNodeType, fromNodeID, toNodeType, toNodeID string
		var weight float64
		if err := rows.Scan(&linkType, &fromNodeType, &fromNodeID, &toNodeType, &toNodeID, &weight); err != nil {
			return err
		}
		if factID, ok := narrativeInsightCompletionFactID(linkType, nodeType, nodeID, fromNodeType, fromNodeID, toNodeType, toNodeID); ok {
			if weight < 0 {
				weight = -weight
			}
			addRetrievalCompletion(acc, factID, completionSourceNarrative, linkType, chainCompletionBonus, anchorScore*weight)
		}
	}
	return rows.Err()
}

func narrativeInsightCompletionFactID(linkType string, anchorNodeType core.NodeType, anchorNodeID string, fromNodeType string, fromNodeID string, toNodeType string, toNodeID string) (string, bool) {
	if fromNodeType == string(anchorNodeType) &&
		fromNodeID == anchorNodeID &&
		toNodeType == string(core.NodeTypeFact) &&
		narrativeInsightOutboundLinkCompletesFact(linkType) {
		return toNodeID, true
	}
	if toNodeType == string(anchorNodeType) &&
		toNodeID == anchorNodeID &&
		fromNodeType == string(core.NodeTypeFact) &&
		narrativeInsightInboundLinkCompletesFact(linkType) {
		return fromNodeID, true
	}
	return "", false
}

func narrativeInsightOutboundLinkCompletesFact(linkType string) bool {
	switch linkType {
	case "DERIVED_FROM", "SUPPORTS", "ABOUT_ENTITY":
		return true
	default:
		return false
	}
}

func narrativeInsightInboundLinkCompletesFact(linkType string) bool {
	switch linkType {
	case "SUPPORTS", "ABOUT_ENTITY":
		return true
	default:
		return false
	}
}

func (r *RetrievalRepository) authorizedCompletionCandidates(ctx context.Context, req RetrievalRequest, policy RetrievalPolicy, acc map[string]retrievalCompletionCandidate) (map[string]retrievalCandidate, error) {
	ordered := make([]retrievalCompletionCandidate, 0, len(acc))
	for _, candidate := range acc {
		if candidate.factID == "" || candidate.bonus <= 0 {
			continue
		}
		ordered = append(ordered, candidate)
	}
	sortRetrievalCompletions(ordered)
	factIDs := make([]string, 0, len(ordered))
	for _, candidate := range ordered {
		factIDs = append(factIDs, candidate.factID)
	}
	pf, err := r.buildScoringPrefetchForFactIDs(ctx, req.PersonaID, req.SessionID, factIDs, policy)
	if err != nil {
		return nil, err
	}
	result := map[string]retrievalCandidate{}
	for _, candidate := range ordered {
		if len(result) >= maxCompletionCandidates {
			break
		}
		fact, ok := pf.facts[candidate.factID]
		if !ok || !authorityAllowsFromPrefetch(fact, policy, pf) {
			continue
		}
		result[candidate.factID] = retrievalCandidate{
			FactID:             candidate.factID,
			CompletionSource:   candidate.source,
			CompletionLinkType: candidate.linkType,
			CompletionBonus:    candidate.bonus,
		}
	}
	return result, nil
}

func pruneRetrievalCompletions(acc map[string]retrievalCompletionCandidate, limit int) {
	if limit <= 0 || len(acc) <= limit {
		return
	}
	ordered := make([]retrievalCompletionCandidate, 0, len(acc))
	for _, candidate := range acc {
		ordered = append(ordered, candidate)
	}
	sortRetrievalCompletions(ordered)
	for _, candidate := range ordered[limit:] {
		delete(acc, candidate.factID)
	}
}

func sortRetrievalCompletions(candidates []retrievalCompletionCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].bonus == candidates[j].bonus {
			if candidates[i].priority == candidates[j].priority {
				if candidates[i].source == candidates[j].source {
					if candidates[i].linkType == candidates[j].linkType {
						return candidates[i].factID < candidates[j].factID
					}
					return candidates[i].linkType < candidates[j].linkType
				}
				return candidates[i].source < candidates[j].source
			}
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].bonus > candidates[j].bonus
	})
}

func addRetrievalCompletion(acc map[string]retrievalCompletionCandidate, factID string, source string, linkType string, bonus float64, priority float64) {
	factID = strings.TrimSpace(factID)
	if factID == "" || bonus <= 0 {
		return
	}
	existing := acc[factID]
	if existing.factID == "" || bonus > existing.bonus || (bonus == existing.bonus && priority > existing.priority) {
		acc[factID] = retrievalCompletionCandidate{
			factID:   factID,
			source:   source,
			linkType: linkType,
			bonus:    bonus,
			priority: priority,
		}
	}
}

func mergeRetrievalCompletionCandidates(candidates map[string]retrievalCandidate, completionCandidates map[string]retrievalCandidate) {
	for factID, completion := range completionCandidates {
		if factID == "" {
			continue
		}
		candidate := candidates[factID]
		candidate.FactID = factID
		if completion.CompletionBonus > candidate.CompletionBonus {
			candidate.CompletionBonus = completion.CompletionBonus
			candidate.CompletionSource = completion.CompletionSource
			candidate.CompletionLinkType = completion.CompletionLinkType
		}
		candidates[factID] = candidate
	}
}

func retrievalCandidateCompletionBonus(query QueryAnalysis, fact core.Fact, factSearchText string, candidate retrievalCandidate, mirror RetrievalMirrorCandidate) (float64, string, string) {
	bonus := candidate.CompletionBonus
	source := candidate.CompletionSource
	linkType := candidate.CompletionLinkType
	if queryWantsPremiseCounterexample(query) && factLooksLikePremiseCounterexample(query, fact, factSearchText, mirror) {
		bonus += premiseCounterexampleBonus
		if queryLooksForPositiveWorkCounterexample(query) && factLooksLikePositiveWorkCounterexample(fact, factSearchText) {
			bonus += positiveWorkCounterexampleBonus
		}
		if source == "" {
			source = completionSourcePremiseCheck
		}
	}
	if queryWantsRelationshipOutcome(query) && factLooksLikeRelationshipOutcome(fact) {
		bonus += outcomeCompletionBonus
		if source == "" {
			source = "relationship_outcome"
		}
	}
	return bonus, source, linkType
}

func queryWantsPremiseCounterexample(query QueryAnalysis) bool {
	return query.MemoryAbility == MemoryAbilityPremiseCheck &&
		query.EvidenceNeed == EvidenceNeedPremiseCounterexample
}

func factLooksLikePremiseCounterexample(query QueryAnalysis, fact core.Fact, factSearchText string, mirror RetrievalMirrorCandidate) bool {
	if strings.TrimSpace(mirror.PrimaryPurpose) == "premise_counterexample_dense" {
		return true
	}
	text := strings.ToLower(strings.Join(nonEmptyStrings(
		string(fact.Predicate),
		fact.ContentSummary,
		stringFromPtr(fact.ObjectLiteral),
		factSearchText,
	), " "))
	for _, term := range []string{"不能吃辣", "不太能吃辣", "辛辣耐受度低", "不能暴露", "不能把", "禁止", "不要", "不得", "不允许", "不是所有", "反例", "例外"} {
		if strings.Contains(text, term) {
			return true
		}
	}
	if premiseRestatementPenalty(query, fact, factSearchText) > 0 {
		return false
	}
	for _, term := range deterministicPremiseCounterexampleExpansions(strings.Join(nonEmptyStrings(query.Raw, query.Normalized), " ")) {
		if isAmbiguousCounterexampleExpansion(term) {
			continue
		}
		if strings.Contains(text, strings.ToLower(term)) {
			return true
		}
	}
	if queryLooksForPositiveWorkCounterexample(query) {
		if factLooksLikePositiveWorkCounterexample(fact, factSearchText) {
			return true
		}
	}
	return false
}

func isAmbiguousCounterexampleExpansion(term string) bool {
	term = strings.TrimSpace(term)
	for _, marker := range premiseCounterexamplePositiveMarkers {
		if term == marker {
			return true
		}
	}
	switch term {
	case "下厨":
		return true
	default:
		return false
	}
}

func factLooksLikePositiveWorkCounterexample(fact core.Fact, factSearchText string) bool {
	text := strings.ToLower(strings.Join(nonEmptyStrings(
		string(fact.Predicate),
		fact.ContentSummary,
		stringFromPtr(fact.ObjectLiteral),
		factSearchText,
	), " "))
	for _, term := range []string{"side project", "开始做", "充实", "积极", "正面", "进展", "推进"} {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func queryLooksForPositiveWorkCounterexample(query QueryAnalysis) bool {
	text := strings.ToLower(strings.Join(nonEmptyStrings(query.Raw, query.Normalized), " "))
	if !strings.Contains(text, "工作") {
		return false
	}
	hasNegativePremise := strings.Contains(text, "焦虑") ||
		strings.Contains(text, "负面") ||
		strings.Contains(text, "消极")
	hasPositiveContrast := strings.Contains(text, "积极") ||
		strings.Contains(text, "正面") ||
		strings.Contains(text, "变化")
	return hasNegativePremise && hasPositiveContrast
}

func (r *RetrievalRepository) loadFactSearchTextByFactID(ctx context.Context, personaID string, factIDs []string, policy RetrievalPolicy) (map[string]string, error) {
	ids := uniqueSortedStrings(factIDs)
	result := make(map[string]string, len(ids))
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	for _, chunk := range chunkedIDs(ids, sqliteInChunkSize) {
		args := make([]any, 0, 2+len(chunk))
		args = append(args, personaID)
		for _, id := range chunk {
			args = append(args, id)
		}
		args = append(args, allowedSensitivityRank)
		rows, err := r.db.QueryContext(ctx, `
SELECT node_id, search_text
FROM memory_search_documents
WHERE persona_id = ?
  AND node_type = 'fact'
  AND node_id IN (`+placeholders(len(chunk))+`)
  AND visibility_status = 'visible'
  AND searchable = 1
  AND CASE sensitivity_level
      WHEN 'normal' THEN 0
      WHEN 'sensitive' THEN 1
      WHEN 'highly_sensitive' THEN 2
      ELSE 3
  END <= ?
ORDER BY node_id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var factID string
			var searchText string
			if err := rows.Scan(&factID, &searchText); err != nil {
				_ = rows.Close()
				return nil, err
			}
			result[factID] = searchText
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func queryWantsRelationshipOutcome(query QueryAnalysis) bool {
	switch query.MemoryAbility {
	case MemoryAbilityRelationshipArc, MemoryAbilityDynamicState:
	default:
		return false
	}
	return query.EvidenceNeed == EvidenceNeedStateTransition ||
		query.EvidenceNeed == EvidenceNeedRelationshipTimeline
}

func factLooksLikeRelationshipOutcome(fact core.Fact) bool {
	if fact.FactType == core.FactTypeRelationalState {
		return true
	}
	text := strings.ToLower(strings.Join(nonEmptyStrings(
		string(fact.Predicate),
		string(fact.FactType),
		fact.ContentSummary,
		stringFromPtr(fact.ObjectLiteral),
	), " "))
	for _, term := range []string{"feels_with_agent", "relational_state", "companionship", "less lonely", "不孤独", "没那么孤独", "陪伴"} {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func stringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
