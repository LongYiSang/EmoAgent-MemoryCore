package sqlite

import (
	"context"
	"database/sql"
	"math"
	"sort"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const sqliteInChunkSize = 500

type scoringPrefetch struct {
	facts         map[string]core.Fact
	entityAllowed map[string]bool
	provenance    map[string]provenanceSnapshot
	fatigue       map[string]int
}

type provenanceSnapshot struct {
	hasEvidenceLink   bool
	visibleEpisodeIDs []string
	evidenceStrength  float64
	provenanceAllowed bool
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func chunkedIDs(ids []string, size int) [][]string {
	if size <= 0 {
		size = sqliteInChunkSize
	}
	if len(ids) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(ids)+size-1)/size)
	for start := 0; start < len(ids); start += size {
		end := start + size
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[start:end])
	}
	return chunks
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	values := make([]string, count)
	for i := range values {
		values[i] = "?"
	}
	return strings.Join(values, ",")
}

func (r *RetrievalRepository) buildScoringPrefetch(ctx context.Context, req RetrievalRequest, policy RetrievalPolicy, candidates map[string]retrievalCandidate) (scoringPrefetch, error) {
	factIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		factIDs = append(factIDs, candidate.FactID)
	}
	return r.buildScoringPrefetchForFactIDs(ctx, req.PersonaID, req.SessionID, factIDs, policy)
}

func (r *RetrievalRepository) buildScoringPrefetchForFactIDs(ctx context.Context, personaID string, sessionID *string, factIDs []string, policy RetrievalPolicy) (scoringPrefetch, error) {
	facts, err := r.loadFactsByID(ctx, personaID, factIDs)
	if err != nil {
		return scoringPrefetch{}, err
	}
	entityIDs := make([]string, 0, len(facts)*2)
	for _, fact := range facts {
		entityIDs = append(entityIDs, linkedEntityIDs(fact)...)
	}
	entityAllowed, err := r.loadEntityEligibility(ctx, personaID, entityIDs, policy)
	if err != nil {
		return scoringPrefetch{}, err
	}
	provenance, err := r.loadEvidenceAndProvenanceByFactID(ctx, personaID, facts)
	if err != nil {
		return scoringPrefetch{}, err
	}
	fatigue, err := r.loadFatigueCounts(ctx, sessionID, factIDs)
	if err != nil {
		return scoringPrefetch{}, err
	}
	return scoringPrefetch{
		facts:         facts,
		entityAllowed: entityAllowed,
		provenance:    provenance,
		fatigue:       fatigue,
	}, nil
}

func (r *RetrievalRepository) loadFactsByID(ctx context.Context, personaID string, factIDs []string) (map[string]core.Fact, error) {
	ids := uniqueSortedStrings(factIDs)
	facts := make(map[string]core.Fact, len(ids))
	for _, chunk := range chunkedIDs(ids, sqliteInChunkSize) {
		args := stringArgs(personaID, chunk)
		rows, err := r.db.QueryContext(ctx, `
SELECT id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
       content_summary, fact_type, valid_from, valid_to,
       extraction_confidence, extraction_confidence_score, extraction_reasoning,
       importance, valence, arousal, sensitivity_level,
       validity_status, visibility_status, lifecycle_status,
       pinned, pin_reason, pin_actor, reinforcement_count, searchable, created_at, updated_at
FROM facts
WHERE persona_id = ?
  AND id IN (`+placeholders(len(chunk))+`)
ORDER BY id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			fact, err := scanFact(rows)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			facts[fact.ID] = fact
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return facts, nil
}

func (r *RetrievalRepository) loadEntityEligibility(ctx context.Context, personaID string, entityIDs []string, policy RetrievalPolicy) (map[string]bool, error) {
	ids := uniqueSortedStrings(entityIDs)
	allowed := make(map[string]bool, len(ids))
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	for _, chunk := range chunkedIDs(ids, sqliteInChunkSize) {
		args := stringArgs(personaID, chunk)
		rows, err := r.db.QueryContext(ctx, `
SELECT id, visibility_status, sensitivity_level, searchable
FROM entities
WHERE persona_id = ?
  AND id IN (`+placeholders(len(chunk))+`)
ORDER BY id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var visibilityStatus string
			var sensitivityLevel string
			var searchable int
			if err := rows.Scan(&id, &visibilityStatus, &sensitivityLevel, &searchable); err != nil {
				_ = rows.Close()
				return nil, err
			}
			allowed[id] = visibilityStatus == string(core.VisibilityVisible) &&
				searchable == 1 &&
				sensitivityRank(core.SensitivityLevel(sensitivityLevel)) <= allowedSensitivityRank
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return allowed, nil
}

func (r *RetrievalRepository) loadEvidenceAndProvenanceByFactID(ctx context.Context, personaID string, facts map[string]core.Fact) (map[string]provenanceSnapshot, error) {
	ids := factIDsFromMap(facts)
	provenance := make(map[string]provenanceSnapshot, len(ids))
	for _, id := range ids {
		provenance[id] = provenanceSnapshot{}
	}
	for _, chunk := range chunkedIDs(ids, sqliteInChunkSize) {
		args := stringArgs(personaID, chunk)
		rows, err := r.db.QueryContext(ctx, `
SELECT l.from_node_id AS fact_id, e.id AS episode_id, e.visibility_status, e.searchable, e.occurred_at
FROM memory_links l
LEFT JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.from_node_id IN (`+placeholders(len(chunk))+`)
  AND l.link_type = 'EVIDENCED_BY'
  AND l.to_node_type = 'episode'
ORDER BY l.from_node_id ASC, e.occurred_at ASC, e.id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var factID string
			var episodeID sql.NullString
			var visibilityStatus sql.NullString
			var searchable sql.NullInt64
			var occurredAt sql.NullString
			if err := rows.Scan(&factID, &episodeID, &visibilityStatus, &searchable, &occurredAt); err != nil {
				_ = rows.Close()
				return nil, err
			}
			snapshot := provenance[factID]
			snapshot.hasEvidenceLink = true
			if episodeID.Valid &&
				visibilityStatus.Valid &&
				visibilityStatus.String == string(core.VisibilityVisible) &&
				searchable.Valid &&
				searchable.Int64 == 1 {
				snapshot.visibleEpisodeIDs = append(snapshot.visibleEpisodeIDs, episodeID.String)
			}
			provenance[factID] = snapshot
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	for factID, snapshot := range provenance {
		fact := facts[factID]
		snapshot.provenanceAllowed = len(snapshot.visibleEpisodeIDs) > 0 || (!snapshot.hasEvidenceLink && fact.Pinned)
		snapshot.evidenceStrength = computeEvidenceStrength(fact, snapshot.visibleEpisodeIDs)
		provenance[factID] = snapshot
	}
	return provenance, nil
}

func (r *RetrievalRepository) loadFatigueCounts(ctx context.Context, sessionID *string, factIDs []string) (map[string]int, error) {
	counts := map[string]int{}
	if sessionID == nil || strings.TrimSpace(*sessionID) == "" {
		return counts, nil
	}
	ids := uniqueSortedStrings(factIDs)
	for _, chunk := range chunkedIDs(ids, sqliteInChunkSize) {
		args := stringArgs(*sessionID, chunk)
		rows, err := r.db.QueryContext(ctx, `
SELECT node_id, COUNT(*)
FROM memory_access_events
WHERE session_id = ?
  AND node_type = 'fact'
  AND access_type = 'retrieved'
  AND node_id IN (`+placeholders(len(chunk))+`)
GROUP BY node_id
ORDER BY node_id ASC`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var factID string
			var count int
			if err := rows.Scan(&factID, &count); err != nil {
				_ = rows.Close()
				return nil, err
			}
			counts[factID] = count
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return counts, nil
}

func authorityAllowsFromPrefetch(fact core.Fact, policy RetrievalPolicy, pf scoringPrefetch) bool {
	if fact.VisibilityStatus != core.VisibilityVisible || !fact.Searchable {
		return false
	}
	if fact.ValidityStatus == core.ValidityInvalidated && !policy.AllowHistorical {
		return false
	}
	switch fact.LifecycleStatus {
	case core.LifecycleArchived:
		if !policy.AllowHistorical {
			return false
		}
	case core.LifecycleDeepArchived:
		if !policy.AllowDeepArchive {
			return false
		}
	}
	if sensitivityRank(fact.SensitivityLevel) > sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission)) {
		return false
	}
	for _, entityID := range linkedEntityIDs(fact) {
		if !pf.entityAllowed[entityID] {
			return false
		}
	}
	snapshot, ok := pf.provenance[fact.ID]
	if !ok {
		return fact.Pinned
	}
	return snapshot.provenanceAllowed
}

func evidenceStrengthFromPrefetch(fact core.Fact, pf scoringPrefetch) (float64, []string) {
	snapshot, ok := pf.provenance[fact.ID]
	if !ok {
		return computeEvidenceStrength(fact, nil), nil
	}
	return snapshot.evidenceStrength, append([]string(nil), snapshot.visibleEpisodeIDs...)
}

func computeEvidenceStrength(fact core.Fact, sourceEpisodeIDs []string) float64 {
	confidence := fact.ExtractionConfidenceScore
	if confidence <= 0 {
		confidence = 0.5
	}
	evidenceCountScore := 0.0
	if len(sourceEpisodeIDs) > 0 {
		evidenceCountScore = math.Min(1, math.Log(1+float64(len(sourceEpisodeIDs)))/math.Log(4))
	}
	sourceQuality := 0.0
	if len(sourceEpisodeIDs) > 0 {
		sourceQuality = 1
	} else if fact.Pinned {
		sourceQuality = 0.5
	}
	reinforcement := 0.0
	if fact.ReinforcementCount > 0 {
		reinforcement = math.Min(1, math.Log(1+float64(fact.ReinforcementCount))/math.Log(4))
	}
	return 0.45*confidence + 0.25*evidenceCountScore + 0.20*sourceQuality + 0.10*reinforcement
}

func factIDsFromMap(facts map[string]core.Fact) []string {
	ids := make([]string, 0, len(facts))
	for id := range facts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func stringArgs(first string, rest []string) []any {
	args := make([]any, 0, 1+len(rest))
	args = append(args, first)
	for _, value := range rest {
		args = append(args, value)
	}
	return args
}
