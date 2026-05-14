package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	MemoryBlockTypeFacts             = "facts"
	MemoryBlockTypeCausalContext     = "causal_context"
	MemoryBlockTypeHistoricalContext = "historical_context"
	MemoryBlockTypeProvenanceContext = "provenance_context"
	MemoryBlockTypeSupportiveContext = "supportive_context"
	MemoryBlockTypeExperienceContext = "experience_context"

	MemoryHistoricalStatusCurrent    = "current"
	MemoryHistoricalStatusHistorical = "historical"
	MemoryHistoricalStatusSuperseded = "superseded"

	MemorySuppressionReasonFatigue = "fatigue"

	memorySuppressionReasonMMRDuplicate  = "mmr_duplicate"
	memorySuppressionReasonContextBudget = "context_budget"

	defaultMMRLambda          = 0.72
	defaultDuplicateThreshold = 0.88
	defaultMinFinalScore      = 0.20
	rerankBoostWeight         = 0.08
	defaultRerankTopN         = 30
	maxRerankSafeSummaryRunes = 512
)

type RetrievalRepository struct {
	db     *sql.DB
	search *SearchRepository
	newID  func() string
	now    func() time.Time
}

type RetrievalRequest struct {
	PersonaID                  string
	SessionID                  *string
	QueryText                  string
	Now                        time.Time
	Policy                     RetrievalPolicy
	Context                    RetrievalAffectContext
	Mirror                     []RetrievalMirrorCandidate
	MirrorDiagnostics          *MirrorDiagnostics
	GraphActivation            []RetrievalActivationCandidate
	GraphActivationDiagnostics *GraphActivationDiagnostics
}

type RetrievalPolicy struct {
	SensitivityPermission string
	AllowHistorical       bool
	AllowDeepArchive      bool
	FinalMemoryCount      int
	ContextBudgetTokens   int
	UseFTS                bool
	UseMirror             bool
}

type RetrievalAffectContext struct {
	UserMoodLabel         string
	RelationshipMoodLabel string
}

type MemoryContext struct {
	Blocks          []MemoryBlock
	DoNotMention    []MemorySuppression
	TokenEstimate   int
	Mirror          *MirrorDiagnostics
	GraphActivation *GraphActivationDiagnostics
	Rerank          *RerankDiagnostics
	QueryAnalysis   *QueryAnalysis
	AnchorFusion    *AnchorFusionDiagnostics
}

type MirrorDiagnostics struct {
	Status                string
	SidecarCandidateCount int
	MappedCandidateCount  int
	DroppedCandidateCount int
	Candidates            []MirrorCandidateDiagnostic
}

type GraphActivationDiagnostics struct {
	Status                string
	SidecarCandidateCount int
	MappedCandidateCount  int
	DroppedCandidateCount int
	Candidates            []GraphActivationCandidateDiagnostic
}

type GraphActivationCandidateDiagnostic struct {
	TriviumNodeID int64
	SQLiteNodeID  string
	NodeType      string
	Score         float64
	Source        string
	Rank          int
	DropReason    string
	Paths         []GraphActivationPath
}

type GraphActivationPath struct {
	TriviumNodeIDs []int64
	LinkTypes      []string
}

type RerankDiagnostics struct {
	Status             string
	SafeCandidateCount int
	ResultCount        int
	Degraded           bool
	FallbackReason     string
}

type RerankCandidate struct {
	NodeID       string
	NodeType     string
	SafeSummary  string
	CurrentScore float64
	AnchorEnergy float64
	GraphEnergy  float64
}

type RerankResultItem struct {
	NodeID      string
	NodeType    string
	RerankScore float64
	DebugReason string
}

type MemoryBlock struct {
	BlockType string
	Items     []MemoryContextItem
}

type MemoryContextItem struct {
	NodeType         string
	NodeID           string
	Summary          string
	Confidence       float64
	UsageGuidance    string
	HistoricalStatus string
	ValidFrom        *time.Time
	ValidTo          *time.Time
	SourceRefs       []MemorySourceRef
	RelatedFacts     []MemoryRelatedFactRef
	DoNotOverstate   bool
}

type MemorySourceRef struct {
	EpisodeID     string
	SessionID     string
	SessionTitle  string
	OccurredAt    time.Time
	SourceStatus  string
	EvidenceCount int
	QuoteAllowed  bool
}

type MemoryRelatedFactRef struct {
	NodeType         string
	NodeID           string
	Summary          string
	LinkType         string
	Direction        string
	HistoricalStatus string
}

type MemorySuppression struct {
	NodeType string
	NodeID   string
	Reason   string
}

type retrievalCandidate struct {
	FactID           string
	FusedAnchorScore float64
	AnchorEnergy     float64
	GraphEnergy      float64
}

type RetrievalActivationCandidate struct {
	FactID        string
	TriviumNodeID int64
	Score         float64
	Source        string
	Rank          int
	Paths         []GraphActivationPath
}

type PreparedRetrieval struct {
	Request      RetrievalRequest
	Query        QueryAnalysis
	Policy       RetrievalPolicy
	Now          time.Time
	FusedAnchors []FusedAnchor
}

type PreparedFinalCandidates struct {
	Request      RetrievalRequest
	Query        QueryAnalysis
	Policy       RetrievalPolicy
	Now          time.Time
	FusedAnchors []FusedAnchor
	Scored       []scoredFact
	Suppressions []MemorySuppression
}

type scoredFact struct {
	Fact             core.Fact
	Score            float64
	TokenCost        int
	Suppressed       bool
	Suppression      string
	Breakdown        retrievalScoreBreakdown
	SourceEpisodeIDs []string
}

type retrievalScoreBreakdown struct {
	AnchorEnergy        float64 `json:"anchor_energy"`
	GraphEnergy         float64 `json:"graph_energy"`
	Importance          float64 `json:"importance"`
	Recency             float64 `json:"recency"`
	FactTypePrior       float64 `json:"fact_type_prior"`
	Pinned              float64 `json:"pinned"`
	EvidenceStrength    float64 `json:"evidence_strength"`
	LifecycleMultiplier float64 `json:"lifecycle_multiplier"`
	FatiguePenalty      float64 `json:"fatigue_penalty"`
	SensitivityPenalty  float64 `json:"sensitivity_penalty"`
	RerankScore         float64 `json:"rerank_score"`
	RerankBoost         float64 `json:"rerank_boost"`
	RerankStatus        string  `json:"rerank_status,omitempty"`
	RerankDebugReason   string  `json:"rerank_debug_reason,omitempty"`
	FinalScore          float64 `json:"final_score"`
	SuppressionReason   string  `json:"suppression_reason,omitempty"`
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
	prepared, err := r.Prepare(ctx, req)
	if err != nil {
		return MemoryContext{}, err
	}
	return r.Complete(ctx, prepared, req.GraphActivation, req.GraphActivationDiagnostics)
}

func (r *RetrievalRepository) Prepare(ctx context.Context, req RetrievalRequest) (PreparedRetrieval, error) {
	if strings.TrimSpace(req.PersonaID) == "" {
		return PreparedRetrieval{}, errors.New("persona_id is required")
	}
	basePolicy := normalizeRetrievalPolicy(req.Policy)
	now := req.Now
	if now.IsZero() {
		now = r.now()
	}
	query, err := r.analyzeQuery(ctx, req.PersonaID, req.QueryText, basePolicy)
	if err != nil {
		return PreparedRetrieval{}, err
	}
	policy := effectiveRetrievalPolicy(basePolicy, query)

	fusedAnchors, err := r.collectFusedAnchors(ctx, req.PersonaID, query, policy, req.Mirror, req.MirrorDiagnostics)
	if err != nil {
		return PreparedRetrieval{}, err
	}
	req.Policy = basePolicy
	req.Now = now
	return PreparedRetrieval{
		Request:      req,
		Query:        query,
		Policy:       policy,
		Now:          now,
		FusedAnchors: fusedAnchors,
	}, nil
}

func (r *RetrievalRepository) Complete(ctx context.Context, prepared PreparedRetrieval, graphCandidates []RetrievalActivationCandidate, graphDiagnostics *GraphActivationDiagnostics) (MemoryContext, error) {
	finalCandidates, _, err := r.BuildRerankCandidates(ctx, prepared, graphCandidates, graphDiagnostics)
	if err != nil {
		return MemoryContext{}, err
	}
	return r.CompleteFinal(ctx, finalCandidates, nil, nil)
}

func (r *RetrievalRepository) BuildRerankCandidates(ctx context.Context, prepared PreparedRetrieval, graphCandidates []RetrievalActivationCandidate, graphDiagnostics *GraphActivationDiagnostics) (PreparedFinalCandidates, []RerankCandidate, error) {
	req := prepared.Request
	req.GraphActivation = graphCandidates
	req.GraphActivationDiagnostics = graphDiagnostics
	query := prepared.Query
	policy := prepared.Policy
	now := prepared.Now
	fusedAnchors := prepared.FusedAnchors
	candidates := factCandidatesFromAnchors(fusedAnchors)
	mergeActivationCandidates(candidates, graphCandidates)
	scored, suppressions, err := r.scoreCandidates(ctx, req, query, policy, now, candidates)
	if err != nil {
		return PreparedFinalCandidates{}, nil, err
	}
	finalCandidates := PreparedFinalCandidates{
		Request:      req,
		Query:        query,
		Policy:       policy,
		Now:          now,
		FusedAnchors: fusedAnchors,
		Scored:       scored,
		Suppressions: suppressions,
	}
	return finalCandidates, safeRerankCandidates(scored), nil
}

func (r *RetrievalRepository) CompleteFinal(ctx context.Context, finalCandidates PreparedFinalCandidates, rerankResults []RerankResultItem, rerankDiagnostics *RerankDiagnostics) (MemoryContext, error) {
	req := finalCandidates.Request
	query := finalCandidates.Query
	policy := finalCandidates.Policy
	fusedAnchors := finalCandidates.FusedAnchors
	scored := append([]scoredFact(nil), finalCandidates.Scored...)
	suppressions := append([]MemorySuppression(nil), finalCandidates.Suppressions...)
	applyRerankResults(scored, rerankResults, rerankDiagnostics)

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return scored[i].Fact.ID < scored[j].Fact.ID
		}
		return scored[i].Score > scored[j].Score
	})

	contextResult := MemoryContext{
		DoNotMention:    suppressions,
		Mirror:          req.MirrorDiagnostics,
		GraphActivation: req.GraphActivationDiagnostics,
		Rerank:          rerankDiagnostics,
		QueryAnalysis:   &query,
		AnchorFusion:    &AnchorFusionDiagnostics{Seeds: fusedAnchors},
	}
	selectable := make([]scoredFact, 0, len(scored))
	for _, candidate := range scored {
		if candidate.Suppressed {
			candidate.Breakdown.SuppressionReason = candidate.Suppression
			if err := r.logAccessEvent(ctx, req, candidate.Fact, "suppressed", candidate.Score, nil, MemoryBlockTypeFacts, candidate.Breakdown); err != nil {
				return MemoryContext{}, err
			}
			continue
		}
		if candidate.Score < defaultMinFinalScore {
			continue
		}
		selectable = append(selectable, candidate)
	}
	remaining := append([]scoredFact(nil), selectable...)
	var selected []scoredFact
	for len(selected) < policy.FinalMemoryCount && len(remaining) > 0 {
		bestIndex := bestMMRCandidateIndex(remaining, selected)
		if bestIndex < 0 {
			break
		}
		candidate := remaining[bestIndex]
		remaining = removeScoredFactAt(remaining, bestIndex)

		if maxFactSimilarity(candidate, selected) > defaultDuplicateThreshold {
			candidate.Breakdown.SuppressionReason = memorySuppressionReasonMMRDuplicate
			if err := r.logAccessEvent(ctx, req, candidate.Fact, "suppressed", candidate.Score, nil, MemoryBlockTypeFacts, candidate.Breakdown); err != nil {
				return MemoryContext{}, err
			}
			continue
		}
		if contextResult.TokenEstimate+candidate.TokenCost > policy.ContextBudgetTokens {
			candidate.Breakdown.SuppressionReason = memorySuppressionReasonContextBudget
			if err := r.logAccessEvent(ctx, req, candidate.Fact, "suppressed", candidate.Score, nil, MemoryBlockTypeFacts, candidate.Breakdown); err != nil {
				return MemoryContext{}, err
			}
			continue
		}
		selected = append(selected, candidate)
		contextResult.TokenEstimate += candidate.TokenCost
	}
	for _, candidate := range remaining {
		if maxFactSimilarity(candidate, selected) <= defaultDuplicateThreshold {
			continue
		}
		candidate.Breakdown.SuppressionReason = memorySuppressionReasonMMRDuplicate
		if err := r.logAccessEvent(ctx, req, candidate.Fact, "suppressed", candidate.Score, nil, MemoryBlockTypeFacts, candidate.Breakdown); err != nil {
			return MemoryContext{}, err
		}
	}
	blocks, blockTypeByFactID, tokenEstimate, err := r.reconstructMemoryBlocks(ctx, req, query, policy, selected)
	if err != nil {
		return MemoryContext{}, err
	}
	contextResult.Blocks = blocks
	if tokenEstimate > 0 {
		contextResult.TokenEstimate = tokenEstimate
	}
	for rank, candidate := range selected {
		rank := rank
		blockType := blockTypeByFactID[candidate.Fact.ID]
		if blockType == "" {
			blockType = MemoryBlockTypeFacts
		}
		if err := r.logAccessEvent(ctx, req, candidate.Fact, "retrieved", candidate.Score, &rank, blockType, candidate.Breakdown); err != nil {
			return MemoryContext{}, err
		}
	}
	return contextResult, nil
}

func (r *RetrievalRepository) scoreCandidates(ctx context.Context, req RetrievalRequest, query QueryAnalysis, policy RetrievalPolicy, now time.Time, candidates map[string]retrievalCandidate) ([]scoredFact, []MemorySuppression, error) {
	scored := make([]scoredFact, 0, len(candidates))
	var suppressions []MemorySuppression
	mirrorByFact := map[string]RetrievalMirrorCandidate{}
	for _, mirror := range req.Mirror {
		if mirror.FactID == "" {
			continue
		}
		mirrorByFact[mirror.FactID] = mirror
	}
	graphByFact := map[string]RetrievalActivationCandidate{}
	for _, activation := range req.GraphActivation {
		if activation.FactID == "" {
			continue
		}
		graphByFact[activation.FactID] = activation
	}
	mirrorCandidateIndexByFact := map[string]int{}
	if req.MirrorDiagnostics != nil {
		for idx, item := range req.MirrorDiagnostics.Candidates {
			if item.SQLiteFactID != "" {
				mirrorCandidateIndexByFact[item.SQLiteFactID] = idx
			}
		}
	}
	graphCandidateIndexByFact := map[string]int{}
	if req.GraphActivationDiagnostics != nil {
		for idx, item := range req.GraphActivationDiagnostics.Candidates {
			if item.SQLiteNodeID != "" && item.NodeType == string(core.NodeTypeFact) {
				graphCandidateIndexByFact[item.SQLiteNodeID] = idx
			}
		}
	}
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
			if req.MirrorDiagnostics != nil {
				if idx, ok := mirrorCandidateIndexByFact[fact.ID]; ok {
					if req.MirrorDiagnostics.Candidates[idx].DropReason == "" {
						req.MirrorDiagnostics.Candidates[idx].DropReason = "dropped_by_authority_filter"
						req.MirrorDiagnostics.DroppedCandidateCount++
					}
				} else if mirror, ok := mirrorByFact[fact.ID]; ok {
					req.MirrorDiagnostics.Candidates = append(req.MirrorDiagnostics.Candidates, MirrorCandidateDiagnostic{
						TriviumNodeID: mirror.TriviumNodeID,
						SQLiteFactID:  fact.ID,
						Score:         mirror.Score,
						Source:        mirror.Source,
						Rank:          mirror.Rank,
						DropReason:    "dropped_by_authority_filter",
					})
					req.MirrorDiagnostics.DroppedCandidateCount++
				}
			}
			if req.GraphActivationDiagnostics != nil {
				if idx, ok := graphCandidateIndexByFact[fact.ID]; ok {
					if req.GraphActivationDiagnostics.Candidates[idx].DropReason == "" {
						req.GraphActivationDiagnostics.Candidates[idx].DropReason = "dropped_by_authority_filter"
						req.GraphActivationDiagnostics.DroppedCandidateCount++
					}
				} else if activation, ok := graphByFact[fact.ID]; ok {
					req.GraphActivationDiagnostics.Candidates = append(req.GraphActivationDiagnostics.Candidates, GraphActivationCandidateDiagnostic{
						TriviumNodeID: activation.TriviumNodeID,
						SQLiteNodeID:  fact.ID,
						NodeType:      string(core.NodeTypeFact),
						Score:         activation.Score,
						Source:        activation.Source,
						Rank:          activation.Rank,
						Paths:         cloneGraphActivationPaths(activation.Paths),
						DropReason:    "dropped_by_authority_filter",
					})
					req.GraphActivationDiagnostics.DroppedCandidateCount++
				}
			}
			continue
		}
		fatigue, err := r.fatigueCount(ctx, req.SessionID, fact.ID)
		if err != nil {
			return nil, nil, err
		}
		evidenceStrength, sourceEpisodeIDs, err := r.evidenceStrength(ctx, fact)
		if err != nil {
			return nil, nil, err
		}
		recency := recencyScore(fact, now)
		typePrior := factTypePrior(fact.FactType)
		pinned := pinnedScore(fact)
		fatiguePenalty := fatiguePenalty(fatigue)
		sensitivityPenalty := sensitivityPenalty(fact.SensitivityLevel)
		lifecycleMultiplier := lifecycleScoreMultiplier(fact.LifecycleStatus)
		baseScore := 0.55*candidate.AnchorEnergy +
			0.25*candidate.GraphEnergy +
			0.20*fact.Importance +
			0.10*recency +
			0.10*typePrior +
			0.10*evidenceStrength +
			0.05*pinned -
			fatiguePenalty -
			sensitivityPenalty
		score := baseScore * lifecycleMultiplier
		breakdown := retrievalScoreBreakdown{
			AnchorEnergy:        candidate.AnchorEnergy,
			GraphEnergy:         candidate.GraphEnergy,
			Importance:          fact.Importance,
			Recency:             recency,
			FactTypePrior:       typePrior,
			Pinned:              pinned,
			EvidenceStrength:    evidenceStrength,
			LifecycleMultiplier: lifecycleMultiplier,
			FatiguePenalty:      fatiguePenalty,
			SensitivityPenalty:  sensitivityPenalty,
			RerankStatus:        "not_requested",
			FinalScore:          score,
		}
		item := scoredFact{
			Fact:             fact,
			Score:            score,
			TokenCost:        estimateTokens(fact.ContentSummary),
			Breakdown:        breakdown,
			SourceEpisodeIDs: sourceEpisodeIDs,
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
		if len(query.Terms) == 0 && candidate.AnchorEnergy == 0 && candidate.GraphEnergy == 0 && candidate.FusedAnchorScore == 0 {
			continue
		}
		scored = append(scored, item)
	}
	return scored, suppressions, nil
}

func safeRerankCandidates(scored []scoredFact) []RerankCandidate {
	safe := make([]scoredFact, 0, len(scored))
	for _, candidate := range scored {
		if candidate.Suppressed {
			continue
		}
		safe = append(safe, candidate)
	}
	sort.Slice(safe, func(i, j int) bool {
		if safe[i].Score == safe[j].Score {
			return safe[i].Fact.ID < safe[j].Fact.ID
		}
		return safe[i].Score > safe[j].Score
	})
	if len(safe) > defaultRerankTopN {
		safe = safe[:defaultRerankTopN]
	}

	result := make([]RerankCandidate, 0, len(safe))
	for _, candidate := range safe {
		result = append(result, RerankCandidate{
			NodeID:       candidate.Fact.ID,
			NodeType:     string(core.NodeTypeFact),
			SafeSummary:  capRunes(candidate.Fact.ContentSummary, maxRerankSafeSummaryRunes),
			CurrentScore: candidate.Score,
			AnchorEnergy: candidate.Breakdown.AnchorEnergy,
			GraphEnergy:  candidate.Breakdown.GraphEnergy,
		})
	}
	return result
}

func applyRerankResults(scored []scoredFact, results []RerankResultItem, diagnostics *RerankDiagnostics) {
	if diagnostics == nil || strings.TrimSpace(diagnostics.Status) == "" {
		return
	}
	status := strings.TrimSpace(diagnostics.Status)
	for index := range scored {
		scored[index].Breakdown.RerankStatus = status
	}
	if status != "used" {
		return
	}
	byFact := map[string]RerankResultItem{}
	for _, item := range results {
		if item.NodeID == "" {
			continue
		}
		if item.NodeType != "" && item.NodeType != string(core.NodeTypeFact) {
			continue
		}
		byFact[item.NodeID] = item
	}
	for index := range scored {
		item, ok := byFact[scored[index].Fact.ID]
		if !ok {
			continue
		}
		score := clampUnitScore(item.RerankScore)
		boost := rerankBoostWeight * score
		scored[index].Score += boost * scored[index].Breakdown.LifecycleMultiplier
		scored[index].Breakdown.RerankScore = score
		scored[index].Breakdown.RerankBoost = boost
		scored[index].Breakdown.RerankDebugReason = capRunes(strings.TrimSpace(item.DebugReason), 160)
		scored[index].Breakdown.FinalScore = scored[index].Score
	}
}

func clampUnitScore(score float64) float64 {
	if math.IsNaN(score) || math.IsInf(score, 0) || score <= 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func capRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
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
	if ok, err := r.linkedEntitiesAllow(ctx, fact, policy); err != nil {
		return false, err
	} else if !ok {
		return false, nil
	}
	return r.provenanceAllows(ctx, fact)
}

func (r *RetrievalRepository) linkedEntitiesAllow(ctx context.Context, fact core.Fact, policy RetrievalPolicy) (bool, error) {
	entityIDs := linkedEntityIDs(fact)
	if len(entityIDs) == 0 {
		return true, nil
	}
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	for _, entityID := range entityIDs {
		var visibilityStatus string
		var sensitivityLevel string
		var searchable int
		err := r.db.QueryRowContext(ctx, `
SELECT visibility_status, sensitivity_level, searchable
FROM entities
WHERE persona_id = ? AND id = ?`, fact.PersonaID, entityID).Scan(&visibilityStatus, &sensitivityLevel, &searchable)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if visibilityStatus != string(core.VisibilityVisible) || searchable != 1 {
			return false, nil
		}
		if sensitivityRank(core.SensitivityLevel(sensitivityLevel)) > allowedSensitivityRank {
			return false, nil
		}
	}
	return true, nil
}

func linkedEntityIDs(fact core.Fact) []string {
	seen := map[string]struct{}{}
	var ids []string
	for _, id := range []*string{fact.SubjectEntityID, fact.ObjectEntityID} {
		if id == nil || *id == "" {
			continue
		}
		if _, ok := seen[*id]; ok {
			continue
		}
		seen[*id] = struct{}{}
		ids = append(ids, *id)
	}
	return ids
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

func (r *RetrievalRepository) evidenceStrength(ctx context.Context, fact core.Fact) (float64, []string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT e.id
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.from_node_id = ?
  AND l.link_type = 'EVIDENCED_BY'
  AND l.to_node_type = 'episode'
  AND e.visibility_status = 'visible'
  AND e.searchable = 1
ORDER BY e.occurred_at ASC, e.id ASC`, fact.PersonaID, fact.ID)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	var sourceEpisodeIDs []string
	for rows.Next() {
		var episodeID string
		if err := rows.Scan(&episodeID); err != nil {
			return 0, nil, err
		}
		sourceEpisodeIDs = append(sourceEpisodeIDs, episodeID)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
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
	strength := 0.45*confidence + 0.25*evidenceCountScore + 0.20*sourceQuality + 0.10*reinforcement
	return strength, sourceEpisodeIDs, nil
}

func (r *RetrievalRepository) factIDsForEntity(ctx context.Context, personaID string, entityID string, policy RetrievalPolicy) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id
FROM facts
WHERE persona_id = ?
  AND (subject_entity_id = ? OR object_entity_id = ?)
  AND visibility_status = 'visible'
  AND searchable = 1
  AND (validity_status != 'invalidated' OR ? = 1)
  AND (lifecycle_status != 'archived' OR ? = 1)
  AND (lifecycle_status != 'deep_archived' OR ? = 1)
ORDER BY importance DESC, updated_at DESC, id ASC`,
		personaID,
		entityID,
		entityID,
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowDeepArchive))
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

func (r *RetrievalRepository) logAccessEvent(ctx context.Context, req RetrievalRequest, fact core.Fact, accessType string, score float64, rank *int, contextBlockType string, breakdown retrievalScoreBreakdown) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO memory_access_events (
    id, persona_id, session_id, node_type, node_id, access_type,
    retrieval_score, rank_position, context_block_type,
    score_breakdown_json,
    user_mood_label, relationship_affect_label
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.newID(),
		req.PersonaID,
		nullableString(req.SessionID),
		string(core.NodeTypeFact),
		fact.ID,
		accessType,
		score,
		nullableInt(rank),
		contextBlockType,
		scoreBreakdownJSON(breakdown),
		nullableNonEmptyString(req.Context.UserMoodLabel),
		nullableNonEmptyString(req.Context.RelationshipMoodLabel),
	)
	return err
}

func scoreBreakdownJSON(breakdown retrievalScoreBreakdown) sql.NullString {
	data, err := json.Marshal(breakdown)
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(data), Valid: true}
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

func effectiveRetrievalPolicy(policy RetrievalPolicy, analysis QueryAnalysis) RetrievalPolicy {
	if analysis.TimeMode == QueryTimeModeHistorical {
		policy.AllowHistorical = true
	}
	return policy
}

func isZeroRetrievalPolicy(policy RetrievalPolicy) bool {
	return policy.SensitivityPermission == string(core.SensitivityNormal) &&
		!policy.AllowHistorical &&
		!policy.AllowDeepArchive &&
		policy.FinalMemoryCount == 8 &&
		policy.ContextBudgetTokens == 1200 &&
		!policy.UseFTS &&
		!policy.UseMirror
}

func textMatchScore(query QueryAnalysis, searchText string) float64 {
	if query.Raw == "" {
		return 0
	}
	normalizedText := strings.ToLower(searchText)
	if strings.Contains(normalizedText, query.Normalized) {
		return 1
	}
	terms := textMatchTerms(query)
	if len(terms) == 0 {
		return 0
	}
	matches := 0
	for _, term := range terms {
		if strings.Contains(normalizedText, term) {
			matches++
		}
	}
	return float64(matches) / float64(len(terms))
}

func textMatchTerms(query QueryAnalysis) []string {
	seen := map[string]struct{}{}
	var terms []string
	add := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		terms = append(terms, value)
	}
	for _, term := range query.Terms {
		add(term)
	}
	for _, term := range cjkBigrams(query.Normalized) {
		add(term)
	}
	return terms
}

func cjkBigrams(value string) []string {
	var result []string
	var sequence []rune
	flush := func() {
		if len(sequence) < 2 {
			sequence = sequence[:0]
			return
		}
		for i := 0; i+1 < len(sequence); i++ {
			result = append(result, string(sequence[i:i+2]))
		}
		sequence = sequence[:0]
	}
	for _, r := range value {
		if isCJKRune(r) {
			sequence = append(sequence, r)
			continue
		}
		flush()
	}
	flush()
	return result
}

func isCJKRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
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

func lifecycleScoreMultiplier(status core.LifecycleStatus) float64 {
	switch status {
	case core.LifecycleActive:
		return 1.0
	case core.LifecycleConsolidated:
		return 0.92
	case core.LifecycleDormant:
		return 0.82
	case core.LifecycleArchived:
		return 0.55
	case core.LifecycleDeepArchived:
		return 0.35
	default:
		return 1.0
	}
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
