package sqlite

import (
	"context"
	"sort"
	"strings"

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
	completionSourceNarrative        = "narrative_insight"
	completionSourcePremiseCheck     = "premise_counterexample"
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
		if queryAllowsCausalCompletion(query) {
			if err := r.addLinkedFactCompletions(ctx, req.PersonaID, sourceFactIDs, []string{"CAUSED_BY", "EXPLAINS", "TRIGGERED_BY"}, false, completionSourceCausal, causalChainCompletionBonus, acc); err != nil {
				return nil, err
			}
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

func queryAllowsHistoricalTransitionCompletion(query QueryAnalysis, policy RetrievalPolicy) bool {
	if !policy.AllowHistorical {
		return false
	}
	if queryWantsPremiseCounterexample(query) {
		return false
	}
	return query.MemoryAbility == MemoryAbilityHistorical ||
		query.EvidenceNeed == EvidenceNeedStateTransition ||
		query.TimeMode == QueryTimeModeHistorical
}

func queryAllowsCausalCompletion(query QueryAnalysis) bool {
	return query.MemoryAbility == MemoryAbilityCausalExplain ||
		hasQuerySignal(query, QuerySignalCausal)
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
	for _, term := range []string{"不能吃辣", "不太能吃辣", "辛辣耐受度低", "不是所有", "反例", "例外"} {
		if strings.Contains(text, term) {
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
