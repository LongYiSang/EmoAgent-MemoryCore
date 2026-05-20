package sqlite

import (
	"context"
	"sort"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type reconstructionPrefetch struct {
	facts                    map[string]core.Fact
	authority                scoringPrefetch
	sourceRefsByFact         map[string][]MemorySourceRef
	relatedLinksByFact       map[string][]relatedLinkSnapshot
	supersedesByFact         map[string][]string
	completionSourceByFactID map[string]string
}

type relatedLinkSnapshot struct {
	linkType   string
	fromFactID string
	toFactID   string
}

func (r *RetrievalRepository) buildReconstructionPrefetch(ctx context.Context, personaID string, selected []scoredFact, blockTypeByFactID map[string]string, policy RetrievalPolicy) (reconstructionPrefetch, error) {
	selectedFactIDs := make([]string, 0, len(selected))
	for _, candidate := range selected {
		selectedFactIDs = append(selectedFactIDs, candidate.Fact.ID)
	}
	selectedFactIDs = uniqueSortedStrings(selectedFactIDs)

	sourceRefsByFact, err := r.loadSourceRefsByFactID(ctx, personaID, selectedFactIDs, policy)
	if err != nil {
		return reconstructionPrefetch{}, err
	}
	relatedLinksByFact, err := r.loadRelatedLinksByFactID(ctx, personaID, selectedFactIDs)
	if err != nil {
		return reconstructionPrefetch{}, err
	}
	relatedFactIDs := relatedFactIDsFromLinkSnapshots(selectedFactIDs, relatedLinksByFact)

	selectedSupersedesByFact, err := r.loadIncomingSupersedesByFactID(ctx, personaID, selectedFactIDs)
	if err != nil {
		return reconstructionPrefetch{}, err
	}
	relatedSupersedesByFact, err := r.loadIncomingSupersedesByFactID(ctx, personaID, relatedFactIDs)
	if err != nil {
		return reconstructionPrefetch{}, err
	}
	supersedesByFact := mergeSupersedesMaps(selectedSupersedesByFact, relatedSupersedesByFact)

	authorityFactIDs := combineStringIDs(
		selectedFactIDs,
		relatedFactIDs,
		flattenStringMapValues(selectedSupersedesByFact),
		flattenStringMapValues(relatedSupersedesByFact),
	)
	authority, err := r.buildScoringPrefetchForFactIDs(ctx, personaID, nil, authorityFactIDs, policy)
	if err != nil {
		return reconstructionPrefetch{}, err
	}
	return reconstructionPrefetch{
		facts:                    authority.facts,
		authority:                authority,
		sourceRefsByFact:         sourceRefsByFact,
		relatedLinksByFact:       relatedLinksByFact,
		supersedesByFact:         supersedesByFact,
		completionSourceByFactID: completionSourceByFactID(selected),
	}, nil
}

func (r *RetrievalRepository) loadSourceRefsByFactID(ctx context.Context, personaID string, factIDs []string, policy RetrievalPolicy) (map[string][]MemorySourceRef, error) {
	ids := uniqueSortedStrings(factIDs)
	grouped := make(map[string][]MemorySourceRef, len(ids))
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	for _, chunk := range chunkedIDs(ids, sqliteInChunkSize) {
		args := stringArgs(personaID, chunk)
		args = append(args, allowedSensitivityRank)
		rows, err := r.db.QueryContext(ctx, `
SELECT l.from_node_id AS fact_id,
       e.id,
       e.session_id,
       COALESCE(s.title, ''),
       e.occurred_at,
       e.visibility_status
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
LEFT JOIN sessions s
  ON s.persona_id = e.persona_id
 AND s.id = e.session_id
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
ORDER BY l.from_node_id ASC, e.occurred_at ASC, e.id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var factID string
			var ref MemorySourceRef
			var occurredAt string
			if err := rows.Scan(&factID, &ref.EpisodeID, &ref.SessionID, &ref.SessionTitle, &occurredAt, &ref.SourceStatus); err != nil {
				_ = rows.Close()
				return nil, err
			}
			ref.OccurredAt = parseTime(occurredAt)
			ref.QuoteAllowed = false
			grouped[factID] = append(grouped[factID], ref)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	result := make(map[string][]MemorySourceRef, len(grouped))
	for factID, refs := range grouped {
		evidenceCount := len(refs)
		if len(refs) > maxSourceRefsPerItem {
			refs = refs[:maxSourceRefsPerItem]
		}
		capped := append([]MemorySourceRef(nil), refs...)
		for i := range capped {
			capped[i].EvidenceCount = evidenceCount
			capped[i].QuoteAllowed = false
		}
		result[factID] = capped
	}
	return result, nil
}

func (r *RetrievalRepository) loadRelatedLinksByFactID(ctx context.Context, personaID string, factIDs []string) (map[string][]relatedLinkSnapshot, error) {
	ids := uniqueSortedStrings(factIDs)
	linksByFact := make(map[string][]relatedLinkSnapshot, len(ids))
	for _, chunk := range chunkedIDs(ids, sqliteInChunkSize) {
		args := stringArgs(personaID, chunk)
		args = append(args, stringArgs(personaID, chunk)...)
		rows, err := r.db.QueryContext(ctx, `
SELECT owner_fact_id, link_type, from_node_id, to_node_id
FROM (
  SELECT from_node_id AS owner_fact_id, link_type, from_node_id, to_node_id, ABS(weight) AS abs_weight, created_at, id
  FROM memory_links
  WHERE persona_id = ?
    AND from_node_type = 'fact'
    AND from_node_id IN (`+placeholders(len(chunk))+`)
    AND to_node_type = 'fact'
    AND visibility_status = 'visible'
    AND searchable = 1
  UNION ALL
  SELECT to_node_id AS owner_fact_id, link_type, from_node_id, to_node_id, ABS(weight) AS abs_weight, created_at, id
  FROM memory_links
  WHERE persona_id = ?
    AND to_node_type = 'fact'
    AND to_node_id IN (`+placeholders(len(chunk))+`)
    AND from_node_type = 'fact'
    AND visibility_status = 'visible'
    AND searchable = 1
)
ORDER BY owner_fact_id ASC, abs_weight DESC, created_at ASC, id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var ownerFactID string
			var link relatedLinkSnapshot
			if err := rows.Scan(&ownerFactID, &link.linkType, &link.fromFactID, &link.toFactID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			linksByFact[ownerFactID] = append(linksByFact[ownerFactID], link)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return linksByFact, nil
}

func (r *RetrievalRepository) loadIncomingSupersedesByFactID(ctx context.Context, personaID string, factIDs []string) (map[string][]string, error) {
	ids := uniqueSortedStrings(factIDs)
	supersedesByFact := make(map[string][]string, len(ids))
	for _, chunk := range chunkedIDs(ids, sqliteInChunkSize) {
		args := stringArgs(personaID, chunk)
		rows, err := r.db.QueryContext(ctx, `
SELECT to_node_id AS owner_fact_id, from_node_id AS superseding_fact_id
FROM memory_links
WHERE persona_id = ?
  AND link_type = 'SUPERSEDES'
  AND from_node_type = 'fact'
  AND to_node_type = 'fact'
  AND to_node_id IN (`+placeholders(len(chunk))+`)
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY to_node_id ASC, ABS(weight) DESC, created_at ASC, id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var ownerFactID string
			var supersedingFactID string
			if err := rows.Scan(&ownerFactID, &supersedingFactID); err != nil {
				_ = rows.Close()
				return nil, err
			}
			supersedesByFact[ownerFactID] = append(supersedesByFact[ownerFactID], supersedingFactID)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return supersedesByFact, nil
}

func reconstructMemoryContextItemFromPrefetch(fact core.Fact, blockType string, policy RetrievalPolicy, pf reconstructionPrefetch) MemoryContextItem {
	return MemoryContextItem{
		NodeType:         string(core.NodeTypeFact),
		NodeID:           fact.ID,
		Summary:          fact.ContentSummary,
		Confidence:       fact.ExtractionConfidenceScore,
		UsageGuidance:    usageGuidance(fact),
		HistoricalStatus: historicalStatusFromPrefetch(fact, policy, pf),
		ValidFrom:        cloneTimePtrForContext(fact.ValidFrom),
		ValidTo:          cloneTimePtrForContext(fact.ValidTo),
		SourceRefs:       append([]MemorySourceRef(nil), pf.sourceRefsByFact[fact.ID]...),
		RelatedFacts:     relatedFactRefsFromPrefetch(fact, blockType, policy, maxRelatedFactsPerItem, pf),
		DoNotOverstate:   blockType != MemoryBlockTypeFacts,
	}
}

func historicalStatusFromPrefetch(fact core.Fact, policy RetrievalPolicy, pf reconstructionPrefetch) string {
	for _, supersedingFactID := range pf.supersedesByFact[fact.ID] {
		supersedingFact, ok := pf.facts[supersedingFactID]
		if !ok {
			continue
		}
		if authorityAllowsFromPrefetch(supersedingFact, policy, pf.authority) {
			return MemoryHistoricalStatusSuperseded
		}
	}
	if factLikelyHistorical(fact) {
		return MemoryHistoricalStatusHistorical
	}
	return MemoryHistoricalStatusCurrent
}

func relatedFactRefsFromPrefetch(fact core.Fact, blockType string, policy RetrievalPolicy, limit int, pf reconstructionPrefetch) []MemoryRelatedFactRef {
	allowed := relatedLinkTypesForBlock(blockType)
	if len(allowed) == 0 || limit <= 0 {
		return nil
	}
	var refs []MemoryRelatedFactRef
	for _, link := range pf.relatedLinksByFact[fact.ID] {
		if !allowed[link.linkType] {
			continue
		}
		relatedFactID := link.toFactID
		direction := relatedFactDirectionOutbound
		if link.toFactID == fact.ID {
			relatedFactID = link.fromFactID
			direction = relatedFactDirectionInbound
		}
		if relatedFactID == "" || relatedFactID == fact.ID {
			continue
		}
		relatedFact, ok := pf.facts[relatedFactID]
		if !ok {
			continue
		}
		if !authorityAllowsFromPrefetch(relatedFact, policy, pf.authority) {
			continue
		}
		refs = append(refs, MemoryRelatedFactRef{
			NodeType:         string(core.NodeTypeFact),
			NodeID:           relatedFact.ID,
			Summary:          relatedFact.ContentSummary,
			LinkType:         link.linkType,
			Direction:        direction,
			HistoricalStatus: historicalStatusFromPrefetch(relatedFact, policy, pf),
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
	return refs
}

func relatedFactIDsFromLinkSnapshots(ownerFactIDs []string, linksByFact map[string][]relatedLinkSnapshot) []string {
	owners := uniqueSortedStrings(ownerFactIDs)
	var ids []string
	for _, ownerFactID := range owners {
		for _, link := range linksByFact[ownerFactID] {
			relatedFactID := link.toFactID
			if link.toFactID == ownerFactID {
				relatedFactID = link.fromFactID
			}
			if relatedFactID == "" || relatedFactID == ownerFactID {
				continue
			}
			ids = append(ids, relatedFactID)
		}
	}
	return uniqueSortedStrings(ids)
}

func mergeSupersedesMaps(maps ...map[string][]string) map[string][]string {
	result := map[string][]string{}
	for _, items := range maps {
		keys := make([]string, 0, len(items))
		for key := range items {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			result[key] = append(result[key], items[key]...)
		}
	}
	return result
}

func flattenStringMapValues(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result []string
	for _, key := range keys {
		result = append(result, values[key]...)
	}
	return result
}

func combineStringIDs(groups ...[]string) []string {
	var values []string
	for _, group := range groups {
		values = append(values, group...)
	}
	return uniqueSortedStrings(values)
}

func completionSourceByFactID(selected []scoredFact) map[string]string {
	result := make(map[string]string, len(selected))
	for _, candidate := range selected {
		if candidate.Breakdown.CompletionSource == "" {
			continue
		}
		result[candidate.Fact.ID] = candidate.Breakdown.CompletionSource
	}
	return result
}
