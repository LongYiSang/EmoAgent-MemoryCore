package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	MemoryBlockTypeFacts = "facts"

	MemorySuppressionReasonFatigue = "fatigue"
)

type RetrievalRepository struct {
	db     *sql.DB
	search *SearchRepository
	newID  func() string
	now    func() time.Time
}

type RetrievalRequest struct {
	PersonaID string
	SessionID *string
	QueryText string
	Now       time.Time
	Policy    RetrievalPolicy
	Context   RetrievalAffectContext
}

type RetrievalPolicy struct {
	SensitivityPermission string
	AllowHistorical       bool
	AllowDeepArchive      bool
	FinalMemoryCount      int
	ContextBudgetTokens   int
	UseFTS                bool
}

type RetrievalAffectContext struct {
	UserMoodLabel         string
	RelationshipMoodLabel string
}

type MemoryContext struct {
	Blocks        []MemoryBlock
	DoNotMention  []MemorySuppression
	TokenEstimate int
}

type MemoryBlock struct {
	BlockType string
	Items     []MemoryContextItem
}

type MemoryContextItem struct {
	NodeType      string
	NodeID        string
	Summary       string
	Confidence    float64
	UsageGuidance string
}

type MemorySuppression struct {
	NodeType string
	NodeID   string
	Reason   string
}

type retrievalCandidate struct {
	FactID      string
	TextMatch   float64
	EntityMatch float64
}

type scoredFact struct {
	Fact        core.Fact
	Score       float64
	TokenCost   int
	Suppressed  bool
	Suppression string
}

func NewRetrievalRepository(db *sql.DB, newID func() string, now func() time.Time) *RetrievalRepository {
	if newID == nil {
		counter := 0
		newID = func() string {
			counter++
			return "retrieval_event_" + formatInt(counter)
		}
	}
	if now == nil {
		now = time.Now
	}
	return &RetrievalRepository{
		db:     db,
		search: NewSearchRepository(db),
		newID:  newID,
		now:    now,
	}
}

func (r *RetrievalRepository) Retrieve(ctx context.Context, req RetrievalRequest) (MemoryContext, error) {
	if strings.TrimSpace(req.PersonaID) == "" {
		return MemoryContext{}, errors.New("persona_id is required")
	}
	policy := normalizeRetrievalPolicy(req.Policy)
	now := req.Now
	if now.IsZero() {
		now = r.now()
	}
	query := analyzeQuery(req.QueryText)

	candidates, err := r.collectCandidates(ctx, req.PersonaID, query, policy)
	if err != nil {
		return MemoryContext{}, err
	}
	scored, suppressions, err := r.scoreCandidates(ctx, req, query, policy, now, candidates)
	if err != nil {
		return MemoryContext{}, err
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Fact.ID < scored[j].Fact.ID
		}
		return scored[i].Score > scored[j].Score
	})

	contextResult := MemoryContext{
		DoNotMention: suppressions,
	}
	block := MemoryBlock{BlockType: MemoryBlockTypeFacts}
	for _, candidate := range scored {
		if candidate.Suppressed {
			if err := r.logAccessEvent(ctx, req, candidate.Fact, "suppressed", candidate.Score, nil); err != nil {
				return MemoryContext{}, err
			}
			continue
		}
		if len(block.Items) >= policy.FinalMemoryCount {
			break
		}
		if contextResult.TokenEstimate+candidate.TokenCost > policy.ContextBudgetTokens {
			break
		}
		rank := len(block.Items)
		item := MemoryContextItem{
			NodeType:      string(core.NodeTypeFact),
			NodeID:        candidate.Fact.ID,
			Summary:       candidate.Fact.ContentSummary,
			Confidence:    candidate.Fact.ExtractionConfidenceScore,
			UsageGuidance: usageGuidance(candidate.Fact),
		}
		block.Items = append(block.Items, item)
		contextResult.TokenEstimate += candidate.TokenCost
		if err := r.logAccessEvent(ctx, req, candidate.Fact, "retrieved", candidate.Score, &rank); err != nil {
			return MemoryContext{}, err
		}
	}
	if len(block.Items) > 0 {
		contextResult.Blocks = append(contextResult.Blocks, block)
	}
	return contextResult, nil
}

func (r *RetrievalRepository) collectCandidates(ctx context.Context, personaID string, query queryAnalysis, policy RetrievalPolicy) (map[string]retrievalCandidate, error) {
	candidates := make(map[string]retrievalCandidate)
	docs, err := r.search.SearchDocuments(ctx, personaID, query.Raw, policy.UseFTS, policy.FinalMemoryCount*4)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if doc.NodeType != core.NodeTypeFact {
			continue
		}
		candidate := candidates[doc.NodeID]
		candidate.FactID = doc.NodeID
		candidate.TextMatch = math.Max(candidate.TextMatch, textMatchScore(query, doc.SearchText))
		candidates[doc.NodeID] = candidate
	}

	entityIDs, err := r.matchEntities(ctx, personaID, query.Normalized)
	if err != nil {
		return nil, err
	}
	for _, entityID := range entityIDs {
		factIDs, err := r.factIDsForEntity(ctx, personaID, entityID)
		if err != nil {
			return nil, err
		}
		for _, factID := range factIDs {
			candidate := candidates[factID]
			candidate.FactID = factID
			candidate.EntityMatch = 1
			candidates[factID] = candidate
		}
	}
	return candidates, nil
}

func (r *RetrievalRepository) scoreCandidates(ctx context.Context, req RetrievalRequest, query queryAnalysis, policy RetrievalPolicy, now time.Time, candidates map[string]retrievalCandidate) ([]scoredFact, []MemorySuppression, error) {
	scored := make([]scoredFact, 0, len(candidates))
	var suppressions []MemorySuppression
	for _, candidate := range candidates {
		fact, err := r.getFact(ctx, req.PersonaID, candidate.FactID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if ok, err := r.authorityAllows(ctx, fact, policy); err != nil {
			return nil, nil, err
		} else if !ok {
			continue
		}
		fatigue, err := r.fatigueCount(ctx, req.SessionID, fact.ID)
		if err != nil {
			return nil, nil, err
		}
		score := 0.35*candidate.TextMatch +
			0.20*candidate.EntityMatch +
			0.20*fact.Importance +
			0.10*recencyScore(fact, now) +
			0.10*factTypePrior(fact.FactType) +
			0.05*pinnedScore(fact) -
			fatiguePenalty(fatigue) -
			sensitivityPenalty(fact.SensitivityLevel)
		item := scoredFact{
			Fact:      fact,
			Score:     score,
			TokenCost: estimateTokens(fact.ContentSummary),
		}
		if fatigue > 0 {
			item.Suppressed = true
			item.Suppression = MemorySuppressionReasonFatigue
			suppressions = append(suppressions, MemorySuppression{
				NodeType: string(core.NodeTypeFact),
				NodeID:   fact.ID,
				Reason:   MemorySuppressionReasonFatigue,
			})
		}
		if len(query.Terms) == 0 && candidate.EntityMatch == 0 && candidate.TextMatch == 0 {
			continue
		}
		scored = append(scored, item)
	}
	return scored, suppressions, nil
}

func (r *RetrievalRepository) getFact(ctx context.Context, personaID string, factID string) (core.Fact, error) {
	row := r.db.QueryRowContext(ctx, `
SELECT id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
       content_summary, fact_type, valid_from, valid_to,
       extraction_confidence, extraction_confidence_score, extraction_reasoning,
       importance, valence, arousal, sensitivity_level,
       validity_status, visibility_status, lifecycle_status,
       pinned, pin_reason, pin_actor, reinforcement_count, searchable, created_at, updated_at
FROM facts
WHERE persona_id = ? AND id = ?`, personaID, factID)
	return scanFact(row)
}

func (r *RetrievalRepository) authorityAllows(ctx context.Context, fact core.Fact, policy RetrievalPolicy) (bool, error) {
	if fact.VisibilityStatus != core.VisibilityVisible || !fact.Searchable {
		return false, nil
	}
	if fact.ValidityStatus == core.ValidityInvalidated && !policy.AllowHistorical {
		return false, nil
	}
	switch fact.LifecycleStatus {
	case core.LifecycleArchived:
		if !policy.AllowHistorical {
			return false, nil
		}
	case core.LifecycleDeepArchived:
		if !policy.AllowDeepArchive {
			return false, nil
		}
	}
	if sensitivityRank(fact.SensitivityLevel) > sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission)) {
		return false, nil
	}
	return r.provenanceAllows(ctx, fact)
}

func (r *RetrievalRepository) provenanceAllows(ctx context.Context, fact core.Fact) (bool, error) {
	var evidenceCount int
	var visibleEvidenceCount int
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*),
       COALESCE(SUM(CASE WHEN e.visibility_status = 'visible' AND e.searchable = 1 THEN 1 ELSE 0 END), 0)
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.from_node_id = ?
  AND l.link_type = 'EVIDENCED_BY'
  AND l.to_node_type = 'episode'`, fact.PersonaID, fact.ID).Scan(&evidenceCount, &visibleEvidenceCount)
	if err != nil {
		return false, err
	}
	if evidenceCount == 0 {
		return fact.Pinned, nil
	}
	return visibleEvidenceCount > 0, nil
}

func (r *RetrievalRepository) matchEntities(ctx context.Context, personaID string, normalizedQuery string) ([]string, error) {
	if normalizedQuery == "" {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT DISTINCT e.id, e.canonical_name, COALESCE(a.alias, '')
FROM entities e
LEFT JOIN entity_aliases a
  ON a.persona_id = e.persona_id
 AND a.entity_id = e.id
WHERE e.persona_id = ?
  AND e.visibility_status = 'visible'
  AND e.searchable = 1`, personaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	var ids []string
	for rows.Next() {
		var id, canonicalName, alias string
		if err := rows.Scan(&id, &canonicalName, &alias); err != nil {
			return nil, err
		}
		if !containsNormalized(normalizedQuery, canonicalName) && !containsNormalized(normalizedQuery, alias) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *RetrievalRepository) factIDsForEntity(ctx context.Context, personaID string, entityID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id
FROM facts
WHERE persona_id = ?
  AND (subject_entity_id = ? OR object_entity_id = ?)`, personaID, entityID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *RetrievalRepository) fatigueCount(ctx context.Context, sessionID *string, factID string) (int, error) {
	if sessionID == nil || *sessionID == "" {
		return 0, nil
	}
	var count int
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_access_events
WHERE session_id = ?
  AND node_type = 'fact'
  AND node_id = ?
  AND access_type = 'retrieved'`, *sessionID, factID).Scan(&count)
	return count, err
}

func (r *RetrievalRepository) logAccessEvent(ctx context.Context, req RetrievalRequest, fact core.Fact, accessType string, score float64, rank *int) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO memory_access_events (
    id, persona_id, session_id, node_type, node_id, access_type,
    retrieval_score, rank_position, context_block_type,
    user_mood_label, relationship_affect_label
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.newID(),
		req.PersonaID,
		nullableString(req.SessionID),
		string(core.NodeTypeFact),
		fact.ID,
		accessType,
		score,
		nullableInt(rank),
		MemoryBlockTypeFacts,
		nullableNonEmptyString(req.Context.UserMoodLabel),
		nullableNonEmptyString(req.Context.RelationshipMoodLabel),
	)
	return err
}

type queryAnalysis struct {
	Raw        string
	Normalized string
	Terms      []string
}

func analyzeQuery(query string) queryAnalysis {
	raw := strings.TrimSpace(query)
	normalized := strings.ToLower(raw)
	return queryAnalysis{
		Raw:        raw,
		Normalized: normalized,
		Terms:      strings.Fields(normalized),
	}
}

func normalizeRetrievalPolicy(policy RetrievalPolicy) RetrievalPolicy {
	if policy.SensitivityPermission == "" {
		policy.SensitivityPermission = string(core.SensitivityNormal)
	}
	if policy.FinalMemoryCount <= 0 {
		policy.FinalMemoryCount = 8
	}
	if policy.ContextBudgetTokens <= 0 {
		policy.ContextBudgetTokens = 1200
	}
	if isZeroRetrievalPolicy(policy) {
		policy.UseFTS = true
	}
	return policy
}

func isZeroRetrievalPolicy(policy RetrievalPolicy) bool {
	return policy.SensitivityPermission == string(core.SensitivityNormal) &&
		!policy.AllowHistorical &&
		!policy.AllowDeepArchive &&
		policy.FinalMemoryCount == 8 &&
		policy.ContextBudgetTokens == 1200 &&
		!policy.UseFTS
}

func textMatchScore(query queryAnalysis, searchText string) float64 {
	if query.Raw == "" {
		return 0
	}
	normalizedText := strings.ToLower(searchText)
	if strings.Contains(normalizedText, query.Normalized) {
		return 1
	}
	if len(query.Terms) == 0 {
		return 0
	}
	matches := 0
	for _, term := range query.Terms {
		if strings.Contains(normalizedText, term) {
			matches++
		}
	}
	return float64(matches) / float64(len(query.Terms))
}

func recencyScore(fact core.Fact, now time.Time) float64 {
	if fact.CreatedAt.IsZero() {
		return 0.5
	}
	age := now.Sub(fact.CreatedAt)
	if age <= 0 {
		return 1
	}
	days := age.Hours() / 24
	return 1 / (1 + days/30)
}

func factTypePrior(factType core.FactType) float64 {
	switch factType {
	case core.FactTypeCoreIdentity:
		return 1
	case core.FactTypeStablePreference, core.FactTypeRelationalState:
		return 0.8
	case core.FactTypeCommitment:
		return 0.7
	default:
		return 0.5
	}
}

func pinnedScore(fact core.Fact) float64 {
	if fact.Pinned {
		return 1
	}
	return 0
}

func fatiguePenalty(count int) float64 {
	if count <= 0 {
		return 0
	}
	return 0.6
}

func sensitivityPenalty(level core.SensitivityLevel) float64 {
	switch level {
	case core.SensitivityHighlySensitive:
		return 0.1
	case core.SensitivitySensitive:
		return 0.05
	default:
		return 0
	}
}

func usageGuidance(fact core.Fact) string {
	if fact.ValidityStatus == core.ValidityInvalidated {
		return "historical; do not treat as current fact"
	}
	return ""
}

func sensitivityRank(level core.SensitivityLevel) int {
	switch level {
	case core.SensitivityHighlySensitive:
		return 2
	case core.SensitivitySensitive:
		return 1
	default:
		return 0
	}
}

func estimateTokens(summary string) int {
	runes := len([]rune(summary))
	if runes == 0 {
		return 1
	}
	return runes/2 + 8
}

func nullableInt(value *int) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*value), Valid: true}
}

func nullableNonEmptyString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func containsNormalized(query string, value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value != "" && strings.Contains(query, value)
}

func formatInt(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
