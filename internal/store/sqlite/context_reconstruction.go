package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	relatedFactDirectionOutbound = "outbound"
	relatedFactDirectionInbound  = "inbound"

	maxRelatedFactsPerItem = 2
	maxSourceRefsPerItem   = 2
)

func (r *RetrievalRepository) reconstructMemoryBlocks(ctx context.Context, req RetrievalRequest, query QueryAnalysis, policy RetrievalPolicy, selected []scoredFact) ([]MemoryBlock, map[string]string, int, error) {
	if len(selected) == 0 {
		return nil, nil, 0, nil
	}
	blocksByType := map[string]*MemoryBlock{}
	var blockOrder []string
	blockTypeByFactID := map[string]string{}
	tokenEstimate := 0

	personaID := req.PersonaID
	if personaID == "" {
		personaID = selected[0].Fact.PersonaID
	}
	pf, err := r.buildReconstructionPrefetch(ctx, personaID, selected, blockTypeByFactID, policy)
	if err != nil {
		return nil, nil, 0, err
	}

	for _, candidate := range selected {
		blockType := primaryContextBlockForSelectedFact(query, candidate.Fact, policy, pf)
		if _, ok := blocksByType[blockType]; !ok {
			blocksByType[blockType] = &MemoryBlock{BlockType: blockType}
			blockOrder = append(blockOrder, blockType)
		}
		blockTypeByFactID[candidate.Fact.ID] = blockType
	}

	for _, candidate := range selected {
		blockType := blockTypeByFactID[candidate.Fact.ID]
		item := reconstructMemoryContextItemFromPrefetch(candidate.Fact, blockType, policy, pf)
		blocksByType[blockType].Items = append(blocksByType[blockType].Items, item)
		tokenEstimate += estimateMemoryContextItemTokens(item)
	}

	blocks := make([]MemoryBlock, 0, len(blockOrder))
	for _, blockType := range blockOrder {
		blocks = append(blocks, *blocksByType[blockType])
	}
	trimMemoryBlocksToBudget(blocks, policy.ContextBudgetTokens, &tokenEstimate)
	return blocks, blockTypeByFactID, tokenEstimate, nil
}

func contextBlockType(query QueryAnalysis, fact core.Fact) string {
	return primaryContextBlockForSelectedFact(query, fact, RetrievalPolicy{}, reconstructionPrefetch{})
}

func primaryContextBlockForSelectedFact(query QueryAnalysis, fact core.Fact, policy RetrievalPolicy, pf reconstructionPrefetch) string {
	switch {
	case queryWantsSupportiveMemory(query):
		return MemoryBlockTypeSupportiveMemory
	case queryWantsExplicitProvenanceMemory(query):
		return MemoryBlockTypeProvenanceMemory
	case queryWantsHistoricalTransitionMemory(query, fact, policy, pf):
		return MemoryBlockTypeHistoricalTransitionMemory
	case queryWantsRelevantCausalMemory(query):
		return MemoryBlockTypeRelevantCausalMemory
	case queryWantsPremiseCheckMemory(query):
		return MemoryBlockTypePremiseCheckMemory
	case queryWantsRelationshipArcMemory(query, fact, pf):
		return MemoryBlockTypeRelationshipArcMemory
	case queryUsesExperienceContext(query, fact):
		return MemoryBlockTypeExperienceContext
	case queryHasSourceEvidencePath(query, fact, pf):
		return MemoryBlockTypeProvenanceMemory
	case !queryUsesDirectFactBlock(query):
		if blockType := secondaryContextBlockHint(query, fact, policy, pf); blockType != "" {
			return blockType
		}
	default:
		return MemoryBlockTypeFacts
	}
	return MemoryBlockTypeFacts
}

func queryWantsExplicitProvenanceMemory(query QueryAnalysis) bool {
	return query.EvidenceNeed == EvidenceNeedProvenanceSource ||
		hasQuerySignal(query, QuerySignalProvenanceSource) ||
		hasQuerySignal(query, QuerySignalProvenance)
}

func queryHasSourceEvidencePath(query QueryAnalysis, fact core.Fact, pf reconstructionPrefetch) bool {
	return !queryUsesDirectFactBlock(query) && len(pf.sourceRefsByFact[fact.ID]) > 0
}

func queryWantsHistoricalTransitionMemory(query QueryAnalysis, fact core.Fact, policy RetrievalPolicy, pf reconstructionPrefetch) bool {
	if hasAuthorizedSupersedesEvidence(fact, policy, pf) {
		return true
	}
	if queryUsesDirectFactBlock(query) {
		return false
	}
	if query.EvidenceNeed != EvidenceNeedStateTransition && !hasQuerySignal(query, QuerySignalStateTransition) {
		return false
	}
	return true
}

func queryWantsRelevantCausalMemory(query QueryAnalysis) bool {
	hasCausalSignal := hasQuerySignal(query, QuerySignalCausalChain) || hasQuerySignal(query, QuerySignalCausal)
	return hasCausalSignal
}

func queryWantsPremiseCheckMemory(query QueryAnalysis) bool {
	return query.EvidenceNeed == EvidenceNeedPremiseCounterexample ||
		hasQuerySignal(query, QuerySignalPremiseCounterexample)
}

func queryWantsRelationshipArcMemory(query QueryAnalysis, fact core.Fact, pf reconstructionPrefetch) bool {
	if query.EvidenceNeed == EvidenceNeedRelationshipTimeline || hasQuerySignal(query, QuerySignalRelationshipArc) {
		return true
	}
	return pf.completionSourceByFactID[fact.ID] == completionSourceNarrative
}

func queryWantsSupportiveMemory(query QueryAnalysis) bool {
	return query.MemoryAbility == MemoryAbilitySupportive ||
		query.MemoryAbility == MemoryAbilityBoundary ||
		hasQuerySignal(query, QuerySignalForgetDelete)
}

func queryUsesDirectFactBlock(query QueryAnalysis) bool {
	return query.MemoryAbility == MemoryAbilityDirectFact ||
		query.EvidenceNeed == EvidenceNeedExactObservation ||
		hasQuerySignal(query, QuerySignalExactFact) ||
		hasQuerySignal(query, QuerySignalPastEventDirectFact)
}

func secondaryContextBlockHint(query QueryAnalysis, fact core.Fact, policy RetrievalPolicy, pf reconstructionPrefetch) string {
	for _, hint := range query.ContextBlockHints {
		switch strings.TrimSpace(hint) {
		case MemoryBlockTypeProvenanceMemory:
			if queryWantsExplicitProvenanceMemory(query) || queryHasSourceEvidencePath(query, fact, pf) {
				return MemoryBlockTypeProvenanceMemory
			}
		case MemoryBlockTypeHistoricalTransitionMemory:
			if queryWantsHistoricalTransitionMemory(query, fact, policy, pf) {
				return MemoryBlockTypeHistoricalTransitionMemory
			}
		case MemoryBlockTypeRelevantCausalMemory:
			if queryWantsRelevantCausalMemory(query) {
				return MemoryBlockTypeRelevantCausalMemory
			}
		case MemoryBlockTypePremiseCheckMemory:
			if queryWantsPremiseCheckMemory(query) {
				return MemoryBlockTypePremiseCheckMemory
			}
		case MemoryBlockTypeRelationshipArcMemory:
			if queryWantsRelationshipArcMemory(query, fact, pf) {
				return MemoryBlockTypeRelationshipArcMemory
			}
		case MemoryBlockTypeSupportiveMemory:
			if queryWantsSupportiveMemory(query) {
				return MemoryBlockTypeSupportiveMemory
			}
		case MemoryBlockTypeExperienceContext:
			if queryUsesExperienceContext(query, fact) {
				return MemoryBlockTypeExperienceContext
			}
		case MemoryBlockTypeFacts:
			return MemoryBlockTypeFacts
		}
		break
	}
	return ""
}

func hasAuthorizedSupersedesEvidence(fact core.Fact, policy RetrievalPolicy, pf reconstructionPrefetch) bool {
	return historicalStatusFromPrefetch(fact, policy, pf) == MemoryHistoricalStatusSuperseded
}

func queryUsesExperienceContext(query QueryAnalysis, fact core.Fact) bool {
	if query.MemoryDomain != MemoryDomainWorkExperience && query.MemoryDomain != MemoryDomainEnvironmentExperience {
		return false
	}
	switch query.MemoryAbility {
	case MemoryAbilityWorkflow, MemoryAbilityGotcha, MemoryAbilityPremiseCheck:
	default:
		return false
	}
	return fact.FactType == core.FactTypeTaskRelevantContext
}

func (r *RetrievalRepository) reconstructMemoryContextItem(ctx context.Context, fact core.Fact, blockType string, policy RetrievalPolicy) (MemoryContextItem, error) {
	historicalStatus, err := r.historicalStatus(ctx, fact, policy)
	if err != nil {
		return MemoryContextItem{}, err
	}
	sourceRefs, err := r.safeSourceRefs(ctx, fact, policy, maxSourceRefsPerItem)
	if err != nil {
		return MemoryContextItem{}, err
	}
	relatedFacts, err := r.relatedFactRefs(ctx, fact, blockType, policy, maxRelatedFactsPerItem)
	if err != nil {
		return MemoryContextItem{}, err
	}
	return MemoryContextItem{
		NodeType:         string(core.NodeTypeFact),
		NodeID:           fact.ID,
		Summary:          fact.ContentSummary,
		Confidence:       fact.ExtractionConfidenceScore,
		UsageGuidance:    usageGuidance(fact),
		HistoricalStatus: historicalStatus,
		ValidFrom:        cloneTimePtrForContext(fact.ValidFrom),
		ValidTo:          cloneTimePtrForContext(fact.ValidTo),
		SourceRefs:       sourceRefs,
		RelatedFacts:     relatedFacts,
		DoNotOverstate:   blockType != MemoryBlockTypeFacts,
	}, nil
}

func (r *RetrievalRepository) historicalStatus(ctx context.Context, fact core.Fact, policy RetrievalPolicy) (string, error) {
	if superseded, err := r.factHasIncomingSupersedes(ctx, fact, policy); err != nil {
		return "", err
	} else if superseded {
		return MemoryHistoricalStatusSuperseded, nil
	}
	if factLikelyHistorical(fact) {
		return MemoryHistoricalStatusHistorical, nil
	}
	return MemoryHistoricalStatusCurrent, nil
}

func factLikelyHistorical(fact core.Fact) bool {
	return fact.ValidityStatus == core.ValidityInvalidated ||
		fact.LifecycleStatus == core.LifecycleArchived ||
		fact.LifecycleStatus == core.LifecycleDeepArchived ||
		fact.ValidTo != nil
}

func (r *RetrievalRepository) factHasIncomingSupersedes(ctx context.Context, fact core.Fact, policy RetrievalPolicy) (bool, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT from_node_id
FROM memory_links
WHERE persona_id = ?
  AND link_type = 'SUPERSEDES'
  AND from_node_type = 'fact'
  AND to_node_type = 'fact'
  AND to_node_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY ABS(weight) DESC, created_at ASC, id ASC`, fact.PersonaID, fact.ID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var supersedingFactIDs []string
	for rows.Next() {
		var factID string
		if err := rows.Scan(&factID); err != nil {
			return false, err
		}
		supersedingFactIDs = append(supersedingFactIDs, factID)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}

	for _, factID := range supersedingFactIDs {
		supersedingFact, err := r.getFact(ctx, fact.PersonaID, factID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return false, err
		}
		if ok, err := r.authorityAllows(ctx, supersedingFact, policy); err != nil {
			return false, err
		} else if ok {
			return true, nil
		}
	}
	return false, nil
}

func (r *RetrievalRepository) safeSourceRefs(ctx context.Context, fact core.Fact, policy RetrievalPolicy, limit int) ([]MemorySourceRef, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT e.id, e.session_id, COALESCE(s.title, ''), e.occurred_at, e.visibility_status
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
LEFT JOIN sessions s
  ON s.persona_id = e.persona_id
 AND s.id = e.session_id
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.from_node_id = ?
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
ORDER BY e.occurred_at ASC, e.id ASC`, fact.PersonaID, fact.ID, sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []MemorySourceRef
	for rows.Next() {
		var ref MemorySourceRef
		var occurredAt string
		if err := rows.Scan(&ref.EpisodeID, &ref.SessionID, &ref.SessionTitle, &occurredAt, &ref.SourceStatus); err != nil {
			return nil, err
		}
		ref.OccurredAt = parseTime(occurredAt)
		ref.QuoteAllowed = false
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	evidenceCount := len(refs)
	if evidenceCount > limit {
		evidenceCount = len(refs)
		refs = refs[:limit]
	}
	for i := range refs {
		refs[i].EvidenceCount = evidenceCount
	}
	return refs, nil
}

func (r *RetrievalRepository) relatedFactRefs(ctx context.Context, fact core.Fact, blockType string, policy RetrievalPolicy, limit int) ([]MemoryRelatedFactRef, error) {
	allowed := relatedLinkTypesForBlock(blockType)
	if len(allowed) == 0 || limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT link_type, from_node_id, to_node_id
FROM memory_links
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND (
    (from_node_type = 'fact' AND from_node_id = ? AND to_node_type = 'fact')
    OR
    (to_node_type = 'fact' AND to_node_id = ? AND from_node_type = 'fact')
  )
ORDER BY ABS(weight) DESC, created_at ASC, id ASC`, fact.PersonaID, fact.ID, fact.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type relatedLinkRow struct {
		linkType   string
		fromFactID string
		toFactID   string
	}
	var linkRows []relatedLinkRow
	for rows.Next() {
		var row relatedLinkRow
		if err := rows.Scan(&row.linkType, &row.fromFactID, &row.toFactID); err != nil {
			return nil, err
		}
		linkRows = append(linkRows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var refs []MemoryRelatedFactRef
	for _, row := range linkRows {
		if !allowed[row.linkType] {
			continue
		}
		relatedFactID := row.toFactID
		direction := relatedFactDirectionOutbound
		if row.toFactID == fact.ID {
			relatedFactID = row.fromFactID
			direction = relatedFactDirectionInbound
		}
		if relatedFactID == "" || relatedFactID == fact.ID {
			continue
		}
		relatedFact, err := r.getFact(ctx, fact.PersonaID, relatedFactID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if ok, err := r.authorityAllows(ctx, relatedFact, policy); err != nil {
			return nil, err
		} else if !ok {
			continue
		}
		historicalStatus, err := r.historicalStatus(ctx, relatedFact, policy)
		if err != nil {
			return nil, err
		}
		refs = append(refs, MemoryRelatedFactRef{
			NodeType:         string(core.NodeTypeFact),
			NodeID:           relatedFact.ID,
			Summary:          relatedFact.ContentSummary,
			LinkType:         row.linkType,
			Direction:        direction,
			HistoricalStatus: historicalStatus,
		})
		if len(refs) >= limit {
			break
		}
	}
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Direction == refs[j].Direction {
			return refs[i].NodeID < refs[j].NodeID
		}
		return refs[i].Direction > refs[j].Direction
	})
	return refs, nil
}

func relatedLinkTypesForBlock(blockType string) map[string]bool {
	switch blockType {
	case MemoryBlockTypeRelevantCausalMemory:
		return map[string]bool{
			"CAUSED_BY":      true,
			"CONTRIBUTED_TO": true,
			"EXPLAINS":       true,
			"SUPPORTS":       true,
			"TRIGGERED_BY":   true,
		}
	case MemoryBlockTypeHistoricalTransitionMemory:
		return map[string]bool{
			"SUPERSEDES": true,
		}
	default:
		return nil
	}
}

func trimMemoryBlocksToBudget(blocks []MemoryBlock, budget int, tokenEstimate *int) {
	if budget <= 0 || tokenEstimate == nil || *tokenEstimate <= budget {
		return
	}
	for blockIndex := len(blocks) - 1; blockIndex >= 0 && *tokenEstimate > budget; blockIndex-- {
		for itemIndex := len(blocks[blockIndex].Items) - 1; itemIndex >= 0 && *tokenEstimate > budget; itemIndex-- {
			item := &blocks[blockIndex].Items[itemIndex]
			for len(item.RelatedFacts) > 0 && *tokenEstimate > budget {
				last := item.RelatedFacts[len(item.RelatedFacts)-1]
				*tokenEstimate -= estimateTokens(last.Summary)
				item.RelatedFacts = item.RelatedFacts[:len(item.RelatedFacts)-1]
			}
			for len(item.SourceRefs) > 0 && *tokenEstimate > budget {
				*tokenEstimate -= 6
				item.SourceRefs = item.SourceRefs[:len(item.SourceRefs)-1]
			}
		}
	}
}

func estimateMemoryContextItemTokens(item MemoryContextItem) int {
	total := estimateTokens(item.Summary)
	for _, related := range item.RelatedFacts {
		total += estimateTokens(related.Summary)
	}
	total += len(item.SourceRefs) * 6
	return total
}

func cloneTimePtrForContext(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func hasQuerySignal(query QueryAnalysis, signal QuerySignal) bool {
	for _, item := range query.Signals {
		if item == signal {
			return true
		}
	}
	return false
}
