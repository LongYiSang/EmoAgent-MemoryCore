package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func (r *RetrievalRepository) collectFusedAnchors(ctx context.Context, personaID string, query QueryAnalysis, policy RetrievalPolicy, mirrorCandidates []RetrievalMirrorCandidate, mirrorDiagnostics *MirrorDiagnostics) ([]FusedAnchor, error) {
	hits, err := r.collectAnchorHits(ctx, personaID, query, policy, mirrorCandidates, mirrorDiagnostics)
	if err != nil {
		return nil, err
	}
	return FuseAnchors(hits, DefaultAnchorFusionConfig()), nil
}

func (r *RetrievalRepository) collectAnchorHits(ctx context.Context, personaID string, query QueryAnalysis, policy RetrievalPolicy, mirrorCandidates []RetrievalMirrorCandidate, mirrorDiagnostics *MirrorDiagnostics) ([]AnchorHit, error) {
	var hits []AnchorHit
	if querySuppressesOperationTargetContext(query) {
		return hits, nil
	}
	if err := r.addMirrorAnchorHits(ctx, personaID, policy, mirrorCandidates, mirrorDiagnostics, &hits); err != nil {
		return nil, err
	}
	if err := r.addSearchAnchorHits(ctx, personaID, query, policy, &hits); err != nil {
		return nil, err
	}
	if err := r.addEntityAnchorHits(ctx, personaID, query, policy, &hits); err != nil {
		return nil, err
	}
	if err := r.addPinnedCoreAnchorHits(ctx, personaID, query, policy, &hits); err != nil {
		return nil, err
	}
	if err := r.addRecentImportantAnchorHits(ctx, personaID, query, policy, &hits); err != nil {
		return nil, err
	}
	if err := r.addNarrativeInsightAnchorHits(ctx, personaID, query, policy, &hits); err != nil {
		return nil, err
	}
	return hits, nil
}

func (r *RetrievalRepository) addMirrorAnchorHits(ctx context.Context, personaID string, policy RetrievalPolicy, mirrorCandidates []RetrievalMirrorCandidate, mirrorDiagnostics *MirrorDiagnostics, hits *[]AnchorHit) error {
	for idx, mirror := range mirrorCandidates {
		if mirror.FactID == "" {
			continue
		}
		rank := mirror.Rank
		if rank <= 0 {
			rank = idx + 1
		}
		fact, err := r.getFact(ctx, personaID, mirror.FactID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		ok, err := r.authorityAllows(ctx, fact, policy)
		if err != nil {
			return err
		}
		if !ok {
			markMirrorCandidateAuthorityDrop(mirrorDiagnostics, mirror)
			continue
		}
		for _, hit := range mirrorAnchorHits(fact.ID, mirror, rank) {
			*hits = append(*hits, hit)
		}
	}
	return nil
}

func mirrorAnchorHits(factID string, mirror RetrievalMirrorCandidate, fallbackRank int) []AnchorHit {
	if len(mirror.SourceBreakdown) == 0 {
		source := strings.TrimSpace(mirror.Source)
		if source == "" {
			source = AnchorSourceTriviumDense
		}
		return []AnchorHit{{
			NodeID:      factID,
			NodeType:    core.NodeTypeFact,
			Source:      source,
			Rank:        fallbackRank,
			RawScore:    mirror.Score,
			DebugReason: fmt.Sprintf("mirror candidate rank=%d", fallbackRank),
		}}
	}
	hits := make([]AnchorHit, 0, len(mirror.SourceBreakdown))
	for _, item := range mirror.SourceBreakdown {
		source := strings.TrimSpace(item.Source)
		if source == "" {
			continue
		}
		rank := item.Rank
		if rank <= 0 {
			rank = fallbackRank
		}
		score := item.Score
		if score <= 0 {
			score = mirror.Score
		}
		hits = append(hits, AnchorHit{
			NodeID:      factID,
			NodeType:    core.NodeTypeFact,
			Source:      source,
			Rank:        rank,
			RawScore:    score,
			DebugReason: fmt.Sprintf("mirror %s rank=%d", source, rank),
		})
	}
	if len(hits) == 0 {
		return mirrorAnchorHits(factID, RetrievalMirrorCandidate{
			Score:  mirror.Score,
			Source: mirror.Source,
		}, fallbackRank)
	}
	return hits
}

func (r *RetrievalRepository) addSearchAnchorHits(ctx context.Context, personaID string, query QueryAnalysis, policy RetrievalPolicy, hits *[]AnchorHit) error {
	if querySuppressesOperationTargetContext(query) {
		return nil
	}
	docs, err := r.search.SearchDocumentsForAnalyzedRetrieval(ctx, personaID, query, policy.UseFTS, policy.FinalMemoryCount*4, policy)
	if err != nil {
		return err
	}
	source := AnchorSourceSQLiteSparse
	if policy.UseFTS {
		source = AnchorSourceSQLiteFTS
	}
	rank := 0
	for _, doc := range docs {
		if doc.NodeType != core.NodeTypeFact {
			continue
		}
		fact, err := r.getFact(ctx, personaID, doc.NodeID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		ok, err := r.authorityAllows(ctx, fact, policy)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		rank++
		*hits = append(*hits, AnchorHit{
			NodeID:      doc.NodeID,
			NodeType:    core.NodeTypeFact,
			Source:      source,
			Rank:        rank,
			RawScore:    textMatchScore(query, doc.SearchText),
			DebugReason: "sqlite search document match",
		})
	}
	return nil
}

func querySuppressesOperationTargetContext(query QueryAnalysis) bool {
	if !hasQuerySignal(query, QuerySignalForgetDelete) {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(query.Raw))
	if text == "" {
		text = strings.ToLower(strings.TrimSpace(query.Normalized))
	}
	if containsAny(text, "忘掉", "删除", "删掉", "不要再提", "forget", "delete", "remove") {
		return true
	}
	if strings.Contains(text, "清除") && !containsAny(text, "已清除", "被清除") {
		return true
	}
	return false
}

func (r *RetrievalRepository) addEntityAnchorHits(ctx context.Context, personaID string, query QueryAnalysis, policy RetrievalPolicy, hits *[]AnchorHit) error {
	rankBySource := map[string]int{}
	for _, mention := range query.EntityMentions {
		source := AnchorSourceEntityExact
		if mention.MatchKind == QueryEntityMentionKindAlias {
			source = AnchorSourceAliasMatch
		}
		factIDs, err := r.factIDsForEntity(ctx, personaID, mention.EntityID, policy)
		if err != nil {
			return err
		}
		for _, factID := range factIDs {
			fact, err := r.getFact(ctx, personaID, factID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			ok, err := r.authorityAllows(ctx, fact, policy)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			rankBySource[source]++
			*hits = append(*hits, AnchorHit{
				NodeID:      factID,
				NodeType:    core.NodeTypeFact,
				Source:      source,
				Rank:        rankBySource[source],
				RawScore:    1,
				DebugReason: "matched safe entity mention",
			})
		}
	}
	return nil
}

func (r *RetrievalRepository) addPinnedCoreAnchorHits(ctx context.Context, personaID string, query QueryAnalysis, policy RetrievalPolicy, hits *[]AnchorHit) error {
	if !queryAllowsPinnedCoreAnchors(query) {
		return nil
	}
	return r.addFactIDsFromQuery(ctx, personaID, policy, `
SELECT id
FROM facts
WHERE persona_id = ?
  AND pinned = 1
  AND fact_type IN ('core_identity', 'commitment', 'stable_preference', 'relational_state')
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY importance DESC, updated_at DESC, id ASC
LIMIT ?`, AnchorSourcePinnedCore, "pinned/core fact", hits)
}

func (r *RetrievalRepository) addRecentImportantAnchorHits(ctx context.Context, personaID string, query QueryAnalysis, policy RetrievalPolicy, hits *[]AnchorHit) error {
	if !queryAllowsRecentImportantAnchors(query) {
		return nil
	}
	return r.addFactIDsFromQuery(ctx, personaID, policy, `
SELECT id
FROM facts
WHERE persona_id = ?
  AND importance >= 0.7
  AND visibility_status = 'visible'
  AND searchable = 1
  AND lifecycle_status IN ('active', 'dormant', 'consolidated')
ORDER BY updated_at DESC, importance DESC, id ASC
LIMIT ?`, AnchorSourceRecentImportant, "recent important fact", hits)
}

func (r *RetrievalRepository) addFactIDsFromQuery(ctx context.Context, personaID string, policy RetrievalPolicy, query string, source string, debugReason string, hits *[]AnchorHit) error {
	limit := 30
	rows, err := r.db.QueryContext(ctx, query, personaID, limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	var factIDs []string
	for rows.Next() {
		var factID string
		if err := rows.Scan(&factID); err != nil {
			return err
		}
		factIDs = append(factIDs, factID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	rank := 0
	for _, factID := range factIDs {
		fact, err := r.getFact(ctx, personaID, factID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		ok, err := r.authorityAllows(ctx, fact, policy)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		rank++
		*hits = append(*hits, AnchorHit{
			NodeID:      factID,
			NodeType:    core.NodeTypeFact,
			Source:      source,
			Rank:        rank,
			RawScore:    fact.Importance,
			DebugReason: debugReason,
		})
	}
	return nil
}

func (r *RetrievalRepository) addNarrativeInsightAnchorHits(ctx context.Context, personaID string, query QueryAnalysis, policy RetrievalPolicy, hits *[]AnchorHit) error {
	if !queryAllowsNarrativeInsightAnchors(query) {
		return nil
	}
	docs, err := r.listNarrativeInsightSearchDocuments(ctx, personaID, 30)
	if err != nil {
		return err
	}
	rank := 0
	for _, doc := range docs {
		if doc.NodeType != core.NodeTypeNarrative && doc.NodeType != core.NodeTypeInsight {
			continue
		}
		if !searchDocumentAuthorityAllows(doc, policy) {
			continue
		}
		matchScore := textMatchScore(query, doc.SearchText)
		if matchScore <= 0 {
			continue
		}
		rank++
		*hits = append(*hits, AnchorHit{
			NodeID:      doc.NodeID,
			NodeType:    doc.NodeType,
			Source:      AnchorSourceNarrativeInsight,
			Rank:        rank,
			RawScore:    matchScore,
			DebugReason: string(doc.NodeType) + " search document match",
		})
	}
	return nil
}

func (r *RetrievalRepository) listNarrativeInsightSearchDocuments(ctx context.Context, personaID string, limit int) ([]core.SearchDocument, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, persona_id, node_type, node_id, search_text, search_tier,
       visibility_status, sensitivity_level, lifecycle_status, searchable
FROM memory_search_documents
WHERE persona_id = ?
  AND node_type IN ('narrative', 'insight')
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY updated_at DESC, node_type ASC, node_id ASC
LIMIT ?`, personaID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchDocuments(rows)
}

func factCandidatesFromAnchors(anchors []FusedAnchor) map[string]retrievalCandidate {
	candidates := make(map[string]retrievalCandidate)
	for _, anchor := range anchors {
		if anchor.NodeType != core.NodeTypeFact {
			continue
		}
		candidates[anchor.NodeID] = retrievalCandidate{
			FactID:           anchor.NodeID,
			FusedAnchorScore: anchor.FusedAnchorScore,
			AnchorEnergy:     anchor.SeedEnergy,
			SourceBreakdown:  cloneAnchorSourceBreakdown(anchor.SourceBreakdown),
		}
	}
	return candidates
}

func mergeActivationCandidates(candidates map[string]retrievalCandidate, graphCandidates []RetrievalActivationCandidate) {
	for _, graph := range graphCandidates {
		if graph.FactID == "" || graph.Score <= 0 {
			continue
		}
		candidate := candidates[graph.FactID]
		candidate.FactID = graph.FactID
		if graph.Score > candidate.GraphEnergy {
			candidate.GraphEnergy = graph.Score
		}
		candidate.SourceBreakdown = append(candidate.SourceBreakdown, AnchorSourceBreakdown{
			Source:          strings.TrimSpace(graph.Source),
			Rank:            graph.Rank,
			RawScore:        graph.Score,
			Weight:          1,
			RRFContribution: graph.Score,
			DebugReason:     "graph activation candidate",
		})
		candidates[graph.FactID] = candidate
	}
}

func cloneAnchorSourceBreakdown(values []AnchorSourceBreakdown) []AnchorSourceBreakdown {
	if len(values) == 0 {
		return nil
	}
	return append([]AnchorSourceBreakdown(nil), values...)
}

func cloneGraphActivationPaths(paths []GraphActivationPath) []GraphActivationPath {
	result := make([]GraphActivationPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, GraphActivationPath{
			TriviumNodeIDs: append([]int64(nil), path.TriviumNodeIDs...),
			LinkTypes:      append([]string(nil), path.LinkTypes...),
		})
	}
	return result
}

func queryAllowsPinnedCoreAnchors(query QueryAnalysis) bool {
	if querySuppressesOperationTargetContext(query) {
		return false
	}
	return query.MemoryAbility == MemoryAbilityBoundary ||
		query.MemoryAbility == MemoryAbilitySupportive ||
		query.MemoryAbility == MemoryAbilityPremiseCheck
}

func queryAllowsRecentImportantAnchors(query QueryAnalysis) bool {
	if querySuppressesOperationTargetContext(query) {
		return false
	}
	return query.MemoryAbility == MemoryAbilityCausalExplain ||
		query.MemoryAbility == MemoryAbilityHistorical ||
		query.MemoryAbility == MemoryAbilityProvenance ||
		query.MemoryAbility == MemoryAbilityPlanning ||
		query.MemoryAbility == MemoryAbilityPremiseCheck ||
		query.MemoryAbility == MemoryAbilityGotcha ||
		query.MemoryAbility == MemoryAbilityWorkflow
}

func queryAllowsNarrativeInsightAnchors(query QueryAnalysis) bool {
	if querySuppressesOperationTargetContext(query) {
		return false
	}
	if (query.MemoryAbility == MemoryAbilityRelationshipArc || query.MemoryAbility == MemoryAbilityDynamicState) &&
		(query.EvidenceNeed == EvidenceNeedStateTransition || query.EvidenceNeed == EvidenceNeedRelationshipTimeline) {
		return true
	}
	switch query.MemoryAbility {
	case MemoryAbilityCausalExplain,
		MemoryAbilitySupportive,
		MemoryAbilityProvenance,
		MemoryAbilityPlanning,
		MemoryAbilityPremiseCheck:
		return true
	default:
		return false
	}
}

func searchDocumentAuthorityAllows(doc core.SearchDocument, policy RetrievalPolicy) bool {
	if doc.VisibilityStatus != core.VisibilityVisible || !doc.Searchable {
		return false
	}
	if sensitivityRank(doc.SensitivityLevel) > sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission)) {
		return false
	}
	switch doc.LifecycleStatus {
	case core.LifecycleArchived:
		return policy.AllowHistorical
	case core.LifecycleDeepArchived:
		return policy.AllowDeepArchive
	default:
		return true
	}
}

func markMirrorCandidateAuthorityDrop(diagnostics *MirrorDiagnostics, mirror RetrievalMirrorCandidate) {
	if diagnostics == nil {
		return
	}
	for idx := range diagnostics.Candidates {
		item := &diagnostics.Candidates[idx]
		if item.TriviumNodeID == mirror.TriviumNodeID && item.SQLiteFactID == mirror.FactID && item.DropReason == "" {
			item.DropReason = "dropped_by_authority_filter"
			diagnostics.DroppedCandidateCount++
			return
		}
	}
	diagnostics.Candidates = append(diagnostics.Candidates, MirrorCandidateDiagnostic{
		TriviumNodeID:  mirror.TriviumNodeID,
		SQLiteFactID:   mirror.FactID,
		Score:          mirror.Score,
		Source:         mirror.Source,
		PrimaryPurpose: mirror.PrimaryPurpose,
		Rank:           mirror.Rank,
		HitCount:       mirror.HitCount,
		DropReason:     "dropped_by_authority_filter",
	})
	diagnostics.DroppedCandidateCount++
}
