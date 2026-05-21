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
	MemoryBlockTypeFacts                      = "facts"
	MemoryBlockTypeRelevantCausalMemory       = "relevant_causal_memory"
	MemoryBlockTypeHistoricalTransitionMemory = "historical_transition_memory"
	MemoryBlockTypeProvenanceMemory           = "provenance_memory"
	MemoryBlockTypePremiseCheckMemory         = "premise_check_memory"
	MemoryBlockTypeRelationshipArcMemory      = "relationship_arc_memory"
	MemoryBlockTypeSupportiveMemory           = "supportive_memory"
	MemoryBlockTypeExperienceContext          = "experience_context"

	MemoryBlockTypeCausalContext     = MemoryBlockTypeRelevantCausalMemory
	MemoryBlockTypeHistoricalContext = MemoryBlockTypeHistoricalTransitionMemory
	MemoryBlockTypeProvenanceContext = MemoryBlockTypeProvenanceMemory
	MemoryBlockTypeSupportiveContext = MemoryBlockTypeSupportiveMemory

	MemoryHistoricalStatusCurrent    = "current"
	MemoryHistoricalStatusHistorical = "historical"
	MemoryHistoricalStatusSuperseded = "superseded"

	MemorySuppressionReasonFatigue       = core.MemorySuppressionReasonFatigue
	MemorySuppressionReasonMMRDuplicate  = core.MemorySuppressionReasonMMRDuplicate
	MemorySuppressionReasonContextBudget = core.MemorySuppressionReasonContextBudget

	defaultMMRLambda          = 0.72
	defaultDuplicateThreshold = 0.88
	defaultMinFinalScore      = 0.20
	rerankBoostWeight         = 0.08
	defaultRerankTopN         = 30
	rawFloorRerankTopN        = 12
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
	PrecomputedQueryAnalysis   *QueryAnalysis
	RawRuleQueryAnalysis       *QueryAnalysis
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
	Blocks              []MemoryBlock
	DoNotMention        []MemorySuppression
	TokenEstimate       int
	Mirror              *MirrorDiagnostics
	GraphActivation     *GraphActivationDiagnostics
	Rerank              *RerankDiagnostics
	QueryAnalysis       *QueryAnalysis
	AnchorFusion        *AnchorFusionDiagnostics
	RetrievalConfidence *RetrievalConfidence
}

type MirrorDiagnostics struct {
	Status                       string
	Degraded                     bool
	FallbackReason               string
	LatencyMs                    int64
	SidecarCandidateCount        int
	MappedCandidateCount         int
	DroppedCandidateCount        int
	EmbeddingCacheHits           int
	EmbeddingCacheMisses         int
	EmbeddingLiveCallCount       int
	QueryCount                   int
	RawQueryCount                int
	RewriteQueryCount            int
	AnchorQueryCount             int
	MergedCandidateCount         int
	QueryTrimCount               int
	DenseEmbeddingWallLatencyMs  int64
	DenseEmbeddingBatchLatencyMs int64
	DenseSearchTotalLatencyMs    int64
	QueryCountTrimmedByBudget    int
	PerQuery                     []MirrorCandidatePerQueryDiagnostic
	Candidates                   []MirrorCandidateDiagnostic
}

type MirrorCandidatePerQueryDiagnostic struct {
	Source    string
	Purpose   string
	Count     int
	LatencyMs int64
}

type GraphActivationDiagnostics struct {
	Status                string
	Degraded              bool
	FallbackReason        string
	LatencyMs             int64
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
	SkippedReason      string
	InputCount         int
	SafeCandidateCount int
	ResultCount        int
	Degraded           bool
	FallbackReason     string
	LatencyMs          int64
}

type RerankCandidate struct {
	NodeID       string
	NodeType     string
	SafeSummary  string
	CurrentScore float64
	AnchorEnergy float64
	GraphEnergy  float64
	SourceScores map[string]float64
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

func appendSuppression(suppressions []MemorySuppression, next MemorySuppression) []MemorySuppression {
	for _, existing := range suppressions {
		if existing.NodeType == next.NodeType && existing.NodeID == next.NodeID && existing.Reason == next.Reason {
			return suppressions
		}
	}
	return append(suppressions, next)
}

type retrievalCandidate struct {
	FactID             string
	FusedAnchorScore   float64
	AnchorEnergy       float64
	GraphEnergy        float64
	SourceBreakdown    []AnchorSourceBreakdown
	CompletionSource   string
	CompletionLinkType string
	CompletionBonus    float64
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
	RawRuleQuery QueryAnalysis
	Policy       RetrievalPolicy
	Now          time.Time
	FusedAnchors []FusedAnchor
}

type PreparedFinalCandidates struct {
	Request      RetrievalRequest
	Query        QueryAnalysis
	RawRuleQuery QueryAnalysis
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
	SourceBreakdown  []AnchorSourceBreakdown
	SourceEpisodeIDs []string
}

type retrievalScoreBreakdown struct {
	ActivationScore     float64                          `json:"activation_score"`
	FusionScore         float64                          `json:"fusion_score,omitempty"`
	AnchorEnergy        float64                          `json:"anchor_energy"`
	GraphEnergy         float64                          `json:"graph_energy"`
	Importance          float64                          `json:"importance"`
	Recency             float64                          `json:"recency"`
	FactTypePrior       float64                          `json:"fact_type_prior"`
	Pinned              float64                          `json:"pinned"`
	EvidenceStrength    float64                          `json:"evidence_strength"`
	LifecycleMultiplier float64                          `json:"lifecycle_multiplier"`
	FatiguePenalty      float64                          `json:"fatigue_penalty"`
	SensitivityPenalty  float64                          `json:"sensitivity_penalty"`
	RerankScore         float64                          `json:"rerank_score"`
	RerankBoost         float64                          `json:"rerank_boost"`
	RerankStatus        string                           `json:"rerank_status,omitempty"`
	RerankDebugReason   string                           `json:"rerank_debug_reason,omitempty"`
	CompletionBonus     float64                          `json:"completion_bonus,omitempty"`
	CompletionSource    string                           `json:"completion_source,omitempty"`
	CompletionLinkType  string                           `json:"completion_link_type,omitempty"`
	LexicalCoverage     float64                          `json:"lexical_coverage,omitempty"`
	SlotCoverage        float64                          `json:"slot_coverage,omitempty"`
	ReflectionBoost     float64                          `json:"reflection_boost,omitempty"`
	HubSuppression      float64                          `json:"hub_suppression,omitempty"`
	PremiseRestatement  float64                          `json:"premise_restatement_penalty,omitempty"`
	FinalScore          float64                          `json:"final_score"`
	SuppressionReason   string                           `json:"suppression_reason,omitempty"`
	QueryAnalysis       *retrievalQueryAnalysisBreakdown `json:"query_analysis,omitempty"`
	ObservedConfidence  *RetrievalConfidence             `json:"observed_confidence,omitempty"`
}

type retrievalQueryAnalysisBreakdown struct {
	RuleFit                     float64                        `json:"rule_fit"`
	AnchorReadiness             float64                        `json:"anchor_readiness"`
	SemanticNeed                float64                        `json:"semantic_need"`
	ExpectedRetrievalConfidence float64                        `json:"expected_retrieval_confidence"`
	Scores                      retrievalQueryAnalysisScores   `json:"scores"`
	Decision                    retrievalQueryAnalysisDecision `json:"decision"`
	SemanticMode                string                         `json:"semantic_mode,omitempty"`
	RetrievalMode               string                         `json:"retrieval_mode,omitempty"`
}

type retrievalQueryAnalysisScores struct {
	RuleFit                     float64 `json:"rule_fit"`
	AnchorReadiness             float64 `json:"anchor_readiness"`
	SemanticNeed                float64 `json:"semantic_need"`
	ExpectedRetrievalConfidence float64 `json:"expected_retrieval_confidence"`
}

type retrievalQueryAnalysisDecision struct {
	SemanticMode  string `json:"semantic_mode,omitempty"`
	RetrievalMode string `json:"retrieval_mode,omitempty"`
}

type pendingAccessEvent struct {
	fact             core.Fact
	accessType       string
	score            float64
	rank             int
	contextBlockType string
	breakdown        retrievalScoreBreakdown
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
	var query QueryAnalysis
	if req.PrecomputedQueryAnalysis != nil {
		query = cloneQueryAnalysis(*req.PrecomputedQueryAnalysis)
	} else {
		var err error
		query, err = r.analyzeQuery(ctx, req.PersonaID, req.QueryText, basePolicy)
		if err != nil {
			return PreparedRetrieval{}, err
		}
	}
	rawRuleQuery := query
	if req.RawRuleQueryAnalysis != nil {
		rawRuleQuery = cloneQueryAnalysis(*req.RawRuleQueryAnalysis)
	}
	policy := effectiveRetrievalPolicy(basePolicy, query)

	fusedAnchors, err := r.collectFusedAnchors(ctx, req.PersonaID, query, policy, req.Mirror, req.MirrorDiagnostics)
	if err != nil {
		return PreparedRetrieval{}, err
	}
	req.Policy = basePolicy
	req.Now = now
	req.PrecomputedQueryAnalysis = nil
	req.RawRuleQueryAnalysis = nil
	return PreparedRetrieval{
		Request:      req,
		Query:        query,
		RawRuleQuery: rawRuleQuery,
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
	rawRuleQuery := prepared.RawRuleQuery
	policy := prepared.Policy
	now := prepared.Now
	fusedAnchors := prepared.FusedAnchors
	candidates := factCandidatesFromAnchors(fusedAnchors)
	mergeActivationCandidates(candidates, graphCandidates)
	scored, suppressions, err := r.scoreCandidates(ctx, req, query, policy, now, candidates)
	if err != nil {
		return PreparedFinalCandidates{}, nil, err
	}
	completionCandidates, err := r.completeLinkedCandidates(ctx, req, query, policy, scored)
	if err != nil {
		return PreparedFinalCandidates{}, nil, err
	}
	mergeRetrievalCompletionCandidates(candidates, completionCandidates)
	if len(completionCandidates) > 0 {
		scored, suppressions, err = r.scoreCandidates(ctx, req, query, policy, now, candidates)
		if err != nil {
			return PreparedFinalCandidates{}, nil, err
		}
	}
	finalCandidates := PreparedFinalCandidates{
		Request:      req,
		Query:        query,
		RawRuleQuery: rawRuleQuery,
		Policy:       policy,
		Now:          now,
		FusedAnchors: fusedAnchors,
		Scored:       scored,
		Suppressions: suppressions,
	}
	rawFloorQuery := prepared.RawRuleQuery
	if rawFloorQuery.Raw == "" {
		rawFloorQuery = query
	}
	return finalCandidates, safeRerankCandidates(scored, rawFloorQuery), nil
}

func (r *RetrievalRepository) CompleteFinal(ctx context.Context, finalCandidates PreparedFinalCandidates, rerankResults []RerankResultItem, rerankDiagnostics *RerankDiagnostics) (MemoryContext, error) {
	return r.completeFinal(ctx, finalCandidates, rerankResults, rerankDiagnostics, true, "")
}

func (r *RetrievalRepository) CompleteFinalWithCorrectiveAction(ctx context.Context, finalCandidates PreparedFinalCandidates, rerankResults []RerankResultItem, rerankDiagnostics *RerankDiagnostics, correctiveAction string) (MemoryContext, error) {
	return r.completeFinal(ctx, finalCandidates, rerankResults, rerankDiagnostics, true, correctiveAction)
}

func (r *RetrievalRepository) CompleteFinalPreview(ctx context.Context, finalCandidates PreparedFinalCandidates, rerankResults []RerankResultItem, rerankDiagnostics *RerankDiagnostics) (MemoryContext, error) {
	return r.completeFinal(ctx, finalCandidates, rerankResults, rerankDiagnostics, false, "")
}

func (r *RetrievalRepository) completeFinal(ctx context.Context, finalCandidates PreparedFinalCandidates, rerankResults []RerankResultItem, rerankDiagnostics *RerankDiagnostics, logAccess bool, correctiveAction string) (MemoryContext, error) {
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
	var accessLogs []pendingAccessEvent
	selectable := make([]scoredFact, 0, len(scored))
	for _, candidate := range scored {
		if candidate.Suppressed {
			candidate.Breakdown.SuppressionReason = candidate.Suppression
			accessLogs = append(accessLogs, pendingAccessEvent{
				fact:             candidate.Fact,
				accessType:       "suppressed",
				score:            candidate.Score,
				contextBlockType: MemoryBlockTypeFacts,
				breakdown:        candidate.Breakdown,
			})
			continue
		}
		if candidate.Score < defaultMinFinalScore {
			continue
		}
		selectable = append(selectable, candidate)
	}
	rawFloorQuery := finalCandidates.RawRuleQuery
	if rawFloorQuery.Raw == "" {
		rawFloorQuery = query
	}
	protected := rawFloorCandidates(selectable, rawFloorQuery)
	selectedByFact := map[string]struct{}{}
	var selected []scoredFact
	for _, candidate := range protected {
		if len(selected) >= policy.FinalMemoryCount {
			break
		}
		if contextResult.TokenEstimate+candidate.TokenCost > policy.ContextBudgetTokens {
			candidate.Breakdown.SuppressionReason = MemorySuppressionReasonContextBudget
			contextResult.DoNotMention = appendSuppression(contextResult.DoNotMention, MemorySuppression{
				NodeType: string(core.NodeTypeFact),
				NodeID:   candidate.Fact.ID,
				Reason:   MemorySuppressionReasonContextBudget,
			})
			accessLogs = append(accessLogs, pendingAccessEvent{
				fact:             candidate.Fact,
				accessType:       "suppressed",
				score:            candidate.Score,
				contextBlockType: MemoryBlockTypeFacts,
				breakdown:        candidate.Breakdown,
			})
			continue
		}
		selected = append(selected, candidate)
		selectedByFact[candidate.Fact.ID] = struct{}{}
		contextResult.TokenEstimate += candidate.TokenCost
	}
	remaining := make([]scoredFact, 0, len(selectable))
	for _, candidate := range selectable {
		if _, ok := selectedByFact[candidate.Fact.ID]; ok {
			continue
		}
		remaining = append(remaining, candidate)
	}
	for len(selected) < policy.FinalMemoryCount && len(remaining) > 0 {
		bestIndex := bestMMRCandidateIndex(remaining, selected)
		if bestIndex < 0 {
			break
		}
		candidate := remaining[bestIndex]
		remaining = removeScoredFactAt(remaining, bestIndex)

		if maxFactSimilarity(candidate, selected) > defaultDuplicateThreshold {
			candidate.Breakdown.SuppressionReason = MemorySuppressionReasonMMRDuplicate
			contextResult.DoNotMention = appendSuppression(contextResult.DoNotMention, MemorySuppression{
				NodeType: string(core.NodeTypeFact),
				NodeID:   candidate.Fact.ID,
				Reason:   MemorySuppressionReasonMMRDuplicate,
			})
			accessLogs = append(accessLogs, pendingAccessEvent{
				fact:             candidate.Fact,
				accessType:       "suppressed",
				score:            candidate.Score,
				contextBlockType: MemoryBlockTypeFacts,
				breakdown:        candidate.Breakdown,
			})
			continue
		}
		if contextResult.TokenEstimate+candidate.TokenCost > policy.ContextBudgetTokens {
			candidate.Breakdown.SuppressionReason = MemorySuppressionReasonContextBudget
			contextResult.DoNotMention = appendSuppression(contextResult.DoNotMention, MemorySuppression{
				NodeType: string(core.NodeTypeFact),
				NodeID:   candidate.Fact.ID,
				Reason:   MemorySuppressionReasonContextBudget,
			})
			accessLogs = append(accessLogs, pendingAccessEvent{
				fact:             candidate.Fact,
				accessType:       "suppressed",
				score:            candidate.Score,
				contextBlockType: MemoryBlockTypeFacts,
				breakdown:        candidate.Breakdown,
			})
			continue
		}
		selected = append(selected, candidate)
		contextResult.TokenEstimate += candidate.TokenCost
	}
	for _, candidate := range remaining {
		if maxFactSimilarity(candidate, selected) <= defaultDuplicateThreshold {
			continue
		}
		candidate.Breakdown.SuppressionReason = MemorySuppressionReasonMMRDuplicate
		contextResult.DoNotMention = appendSuppression(contextResult.DoNotMention, MemorySuppression{
			NodeType: string(core.NodeTypeFact),
			NodeID:   candidate.Fact.ID,
			Reason:   MemorySuppressionReasonMMRDuplicate,
		})
		accessLogs = append(accessLogs, pendingAccessEvent{
			fact:             candidate.Fact,
			accessType:       "suppressed",
			score:            candidate.Score,
			contextBlockType: MemoryBlockTypeFacts,
			breakdown:        candidate.Breakdown,
		})
	}
	selected, err := r.ensureSelectedHistoricalSupersedesCompletions(ctx, req, query, policy, finalCandidates.Now, selected, scored)
	if err != nil {
		return MemoryContext{}, err
	}
	blocks, blockTypeByFactID, tokenEstimate, err := r.reconstructMemoryBlocks(ctx, req, query, policy, selected)
	if err != nil {
		return MemoryContext{}, err
	}
	contextResult.Blocks = blocks
	if tokenEstimate > 0 {
		contextResult.TokenEstimate = tokenEstimate
	}
	confidence := evaluateRetrievalConfidence(finalCandidates, scored, selected, blocks, contextResult.DoNotMention)
	if correctiveAction != "" && confidence.CorrectiveAction != RetrievalCorrectiveActionSuppressMemoryInjection {
		confidence.CorrectiveAction = correctiveAction
	}
	contextResult.RetrievalConfidence = &confidence
	if confidence.CorrectiveAction == RetrievalCorrectiveActionSuppressMemoryInjection {
		for _, candidate := range selected {
			reason := confidence.HardFailureReason
			if reason == "" {
				reason = confidence.CorrectiveAction
			}
			candidate.Breakdown.SuppressionReason = reason
			contextResult.DoNotMention = appendSuppression(contextResult.DoNotMention, MemorySuppression{
				NodeType: string(core.NodeTypeFact),
				NodeID:   candidate.Fact.ID,
				Reason:   reason,
			})
			accessLogs = append(accessLogs, pendingAccessEvent{
				fact:             candidate.Fact,
				accessType:       "suppressed",
				score:            candidate.Score,
				contextBlockType: MemoryBlockTypeFacts,
				breakdown:        candidate.Breakdown,
			})
		}
		contextResult.Blocks = nil
		contextResult.TokenEstimate = 0
	} else {
		for rank, candidate := range selected {
			blockType := blockTypeByFactID[candidate.Fact.ID]
			if blockType == "" {
				blockType = MemoryBlockTypeFacts
			}
			accessLogs = append(accessLogs, pendingAccessEvent{
				fact:             candidate.Fact,
				accessType:       "retrieved",
				score:            candidate.Score,
				rank:             rank + 1,
				contextBlockType: blockType,
				breakdown:        candidate.Breakdown,
			})
		}
	}
	if logAccess {
		if err := r.logAccessEvents(ctx, req, query, &confidence, accessLogs); err != nil {
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
	pf, err := r.buildScoringPrefetch(ctx, req, policy, candidates)
	if err != nil {
		return nil, nil, err
	}
	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.FactID)
	}
	searchTextByFact, err := r.loadFactSearchTextByFactID(ctx, req.PersonaID, candidateIDs, policy)
	if err != nil {
		return nil, nil, err
	}
	for _, factID := range uniqueSortedStrings(candidateIDs) {
		candidate := candidates[factID]
		fact, ok := pf.facts[candidate.FactID]
		if !ok {
			continue
		}
		if !authorityAllowsFromPrefetch(fact, policy, pf) {
			if req.MirrorDiagnostics != nil {
				if idx, ok := mirrorCandidateIndexByFact[fact.ID]; ok {
					if req.MirrorDiagnostics.Candidates[idx].DropReason == "" {
						req.MirrorDiagnostics.Candidates[idx].DropReason = "dropped_by_authority_filter"
						req.MirrorDiagnostics.DroppedCandidateCount++
					}
				} else if mirror, ok := mirrorByFact[fact.ID]; ok {
					req.MirrorDiagnostics.Candidates = append(req.MirrorDiagnostics.Candidates, MirrorCandidateDiagnostic{
						TriviumNodeID:  mirror.TriviumNodeID,
						SQLiteFactID:   fact.ID,
						Score:          mirror.Score,
						Source:         mirror.Source,
						PrimaryPurpose: mirror.PrimaryPurpose,
						Rank:           mirror.Rank,
						HitCount:       mirror.HitCount,
						DropReason:     "dropped_by_authority_filter",
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
		fatigue := pf.fatigue[fact.ID]
		evidenceStrength, sourceEpisodeIDs := evidenceStrengthFromPrefetch(fact, pf)
		recency := recencyScore(fact, now)
		typePrior := factTypePrior(fact.FactType)
		pinned := pinnedScore(fact)
		fatiguePenalty := fatiguePenalty(fatigue)
		sensitivityPenalty := sensitivityPenalty(fact.SensitivityLevel)
		lifecycleMultiplier := lifecycleScoreMultiplier(fact.LifecycleStatus)
		searchText := searchTextByFact[fact.ID]
		completionBonus, completionSource, completionLinkType := retrievalCandidateCompletionBonus(query, fact, searchText, candidate, mirrorByFact[fact.ID])
		lexicalCoverage := textMatchScore(query, searchText)
		slotCoverage := discriminatingSlotCoverage(query, searchText)
		slotBoost := directSlotCoverageBoost(query, slotCoverage)
		reflectionBoost := reflectionSummaryBoost(query, fact, searchText, candidate)
		hubSuppression := broadHubSuppression(query, fact, candidate, completionSource, lexicalCoverage, slotCoverage)
		premiseRestatement := premiseRestatementPenalty(query, fact, searchText)
		baseScore := 0.55*candidate.AnchorEnergy +
			0.25*candidate.GraphEnergy +
			0.20*fact.Importance +
			0.10*recency +
			0.10*typePrior +
			0.10*evidenceStrength +
			0.05*pinned +
			0.12*lexicalCoverage +
			slotBoost +
			reflectionBoost -
			hubSuppression +
			completionBonus -
			premiseRestatement -
			fatiguePenalty -
			sensitivityPenalty
		score := baseScore * lifecycleMultiplier
		breakdown := retrievalScoreBreakdown{
			ActivationScore:     candidate.AnchorEnergy,
			FusionScore:         candidate.FusedAnchorScore,
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
			CompletionBonus:     completionBonus,
			CompletionSource:    completionSource,
			CompletionLinkType:  completionLinkType,
			LexicalCoverage:     lexicalCoverage,
			SlotCoverage:        slotCoverage,
			ReflectionBoost:     reflectionBoost,
			HubSuppression:      hubSuppression,
			PremiseRestatement:  premiseRestatement,
			FinalScore:          score,
		}
		item := scoredFact{
			Fact:             fact,
			Score:            score,
			TokenCost:        estimateTokens(fact.ContentSummary),
			Breakdown:        breakdown,
			SourceBreakdown:  cloneAnchorSourceBreakdown(candidate.SourceBreakdown),
			SourceEpisodeIDs: sourceEpisodeIDs,
		}
		if fatigue > 0 {
			item.Suppressed = true
			item.Suppression = MemorySuppressionReasonFatigue
			suppressions = appendSuppression(suppressions, MemorySuppression{
				NodeType: string(core.NodeTypeFact),
				NodeID:   fact.ID,
				Reason:   MemorySuppressionReasonFatigue,
			})
		}
		if len(query.Terms) == 0 && candidate.AnchorEnergy == 0 && candidate.GraphEnergy == 0 && candidate.FusedAnchorScore == 0 && completionBonus == 0 {
			continue
		}
		scored = append(scored, item)
	}
	return scored, suppressions, nil
}

func safeRerankCandidates(scored []scoredFact, query QueryAnalysis) []RerankCandidate {
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
	if protected := rawFloorCandidates(safe, query); len(protected) > 0 {
		safe = mergeRawFloorIntoPrefix(safe, protected, rawFloorRerankTopN)
	}
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
			SourceScores: sourceScoresFromBreakdown(candidate.SourceBreakdown, candidate.Breakdown.LexicalCoverage),
		})
	}
	return result
}

func rawFloorCandidates(scored []scoredFact, query QueryAnalysis) []scoredFact {
	if !queryProtectsRawFloor(query) {
		return nil
	}
	var protected []scoredFact
	protected = append(protected, topSupportedRawSourceCandidates(scored, query, "raw_dense", 4)...)
	protected = append(protected, topSupportedRawSourceCandidates(scored, query, "raw_query", 4-len(protected))...)
	if query.EvidenceNeed == EvidenceNeedExactObservation {
		protected = append(protected, topRawExactCandidates(scored, 2)...)
	}
	return uniqueScoredFactsByID(protected)
}

func queryProtectsRawFloor(query QueryAnalysis) bool {
	return query.MemoryAbility == MemoryAbilityDirectFact ||
		hasQuerySignal(query, QuerySignalPastEventDirectFact)
}

func mergeRawFloorIntoPrefix(candidates []scoredFact, protected []scoredFact, limit int) []scoredFact {
	if limit <= 0 || len(protected) == 0 || len(candidates) <= limit {
		return candidates
	}
	prefix := mergeRawFloorIntoTopN(candidates[:limit], protected, limit)
	result := make([]scoredFact, 0, len(candidates))
	seen := make(map[string]struct{}, len(prefix))
	for _, candidate := range prefix {
		seen[candidate.Fact.ID] = struct{}{}
		result = append(result, candidate)
	}
	for _, candidate := range candidates {
		if _, ok := seen[candidate.Fact.ID]; ok {
			continue
		}
		seen[candidate.Fact.ID] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func topSupportedRawSourceCandidates(scored []scoredFact, query QueryAnalysis, source string, limit int) []scoredFact {
	if limit <= 0 {
		return nil
	}
	candidates := topRawSourceCandidates(scored, source, len(scored))
	supported := make([]scoredFact, 0, len(candidates))
	for _, candidate := range candidates {
		if !rawFloorCandidateHasDirectSupport(query, candidate) {
			continue
		}
		supported = append(supported, candidate)
	}
	if len(supported) > limit {
		supported = supported[:limit]
	}
	return supported
}

func rawFloorCandidateHasDirectSupport(query QueryAnalysis, candidate scoredFact) bool {
	if candidate.Breakdown.HubSuppression > 0 {
		return false
	}
	if candidate.Breakdown.LexicalCoverage > 0 || candidate.Breakdown.SlotCoverage > 0 {
		return true
	}
	fallbackText := strings.Join(nonEmptyStrings(
		candidate.Fact.ContentSummary,
		string(candidate.Fact.Predicate),
		stringFromPtr(candidate.Fact.ObjectLiteral),
	), " ")
	if textMatchScore(query, fallbackText) > 0 || discriminatingSlotCoverage(query, fallbackText) > 0 {
		return true
	}
	if hasAnchorSource(candidate.SourceBreakdown, "raw_exact") ||
		hasAnchorSource(candidate.SourceBreakdown, AnchorSourceSQLiteFTS) ||
		hasAnchorSource(candidate.SourceBreakdown, AnchorSourceSQLiteSparse) {
		return true
	}
	return false
}

func topRawSourceCandidates(scored []scoredFact, source string, limit int) []scoredFact {
	if limit <= 0 {
		return nil
	}
	var candidates []scoredFact
	for _, candidate := range scored {
		if hasAnchorSource(candidate.SourceBreakdown, source) {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		leftSuppressed := candidates[i].Breakdown.HubSuppression > 0
		rightSuppressed := candidates[j].Breakdown.HubSuppression > 0
		if leftSuppressed != rightSuppressed {
			return !leftSuppressed
		}
		leftRank := bestAnchorSourceRank(candidates[i].SourceBreakdown, source)
		rightRank := bestAnchorSourceRank(candidates[j].SourceBreakdown, source)
		if leftRank == rightRank {
			if candidates[i].Score == candidates[j].Score {
				return candidates[i].Fact.ID < candidates[j].Fact.ID
			}
			return candidates[i].Score > candidates[j].Score
		}
		return leftRank < rightRank
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func topRawExactCandidates(scored []scoredFact, limit int) []scoredFact {
	if limit <= 0 {
		return nil
	}
	var candidates []scoredFact
	for _, candidate := range scored {
		if candidate.Breakdown.LexicalCoverage >= 0.999 ||
			hasAnchorSource(candidate.SourceBreakdown, "raw_exact") ||
			hasAnchorSource(candidate.SourceBreakdown, AnchorSourceSQLiteFTS) ||
			hasAnchorSource(candidate.SourceBreakdown, AnchorSourceSQLiteSparse) {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		leftSuppressed := candidates[i].Breakdown.HubSuppression > 0
		rightSuppressed := candidates[j].Breakdown.HubSuppression > 0
		if leftSuppressed != rightSuppressed {
			return !leftSuppressed
		}
		if candidates[i].Breakdown.LexicalCoverage == candidates[j].Breakdown.LexicalCoverage {
			if candidates[i].Score == candidates[j].Score {
				return candidates[i].Fact.ID < candidates[j].Fact.ID
			}
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Breakdown.LexicalCoverage > candidates[j].Breakdown.LexicalCoverage
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func mergeRawFloorIntoTopN(top []scoredFact, protected []scoredFact, limit int) []scoredFact {
	if limit <= 0 || len(protected) == 0 {
		return top
	}
	result := make([]scoredFact, 0, limit)
	seen := map[string]struct{}{}
	for _, candidate := range protected {
		if _, ok := seen[candidate.Fact.ID]; ok {
			continue
		}
		seen[candidate.Fact.ID] = struct{}{}
		result = append(result, candidate)
		if len(result) >= limit {
			return result
		}
	}
	for _, candidate := range top {
		if _, ok := seen[candidate.Fact.ID]; ok {
			continue
		}
		seen[candidate.Fact.ID] = struct{}{}
		result = append(result, candidate)
		if len(result) >= limit {
			return result
		}
	}
	return result
}

func uniqueScoredFactsByID(scored []scoredFact) []scoredFact {
	result := make([]scoredFact, 0, len(scored))
	seen := map[string]struct{}{}
	for _, candidate := range scored {
		if candidate.Fact.ID == "" {
			continue
		}
		if _, ok := seen[candidate.Fact.ID]; ok {
			continue
		}
		seen[candidate.Fact.ID] = struct{}{}
		result = append(result, candidate)
	}
	return result
}

func hasAnchorSource(breakdown []AnchorSourceBreakdown, source string) bool {
	for _, item := range breakdown {
		if item.Source == source {
			return true
		}
	}
	return false
}

func bestAnchorSourceRank(breakdown []AnchorSourceBreakdown, source string) int {
	best := int(^uint(0) >> 1)
	for _, item := range breakdown {
		if item.Source != source {
			continue
		}
		rank := item.Rank
		if rank <= 0 {
			rank = best
		}
		if rank < best {
			best = rank
		}
	}
	return best
}

func sourceScoresFromBreakdown(breakdown []AnchorSourceBreakdown, lexicalCoverage float64) map[string]float64 {
	scores := map[string]float64{}
	for _, item := range breakdown {
		source := strings.TrimSpace(item.Source)
		if source == "" {
			continue
		}
		if item.RawScore > scores[source] {
			scores[source] = item.RawScore
		}
	}
	if lexicalCoverage > 0 {
		scores["lexical_coverage"] = lexicalCoverage
	}
	if len(scores) == 0 {
		return nil
	}
	return scores
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
	return r.provenanceAllows(ctx, fact, policy)
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

func (r *RetrievalRepository) provenanceAllows(ctx context.Context, fact core.Fact, policy RetrievalPolicy) (bool, error) {
	var evidenceCount int
	var visibleEvidenceCount int
	allowedSensitivityRank := sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*),
       COALESCE(SUM(CASE WHEN l.visibility_status = 'visible'
                            AND l.searchable = 1
                            AND e.visibility_status = 'visible'
                            AND e.searchable = 1
                            AND CASE e.sensitivity_level
                                WHEN 'normal' THEN 0
                                WHEN 'sensitive' THEN 1
                                WHEN 'highly_sensitive' THEN 2
                                ELSE 3
                            END <= ?
                         THEN 1 ELSE 0 END), 0)
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.from_node_id = ?
  AND l.link_type = 'EVIDENCED_BY'
  AND l.to_node_type = 'episode'`, allowedSensitivityRank, fact.PersonaID, fact.ID).Scan(&evidenceCount, &visibleEvidenceCount)
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

func (r *RetrievalRepository) logAccessEvents(ctx context.Context, req RetrievalRequest, query QueryAnalysis, confidence *RetrievalConfidence, events []pendingAccessEvent) error {
	for _, event := range events {
		breakdown := enrichScoreBreakdown(event.breakdown, query, confidence)
		var rank *int
		if event.rank > 0 {
			value := event.rank
			rank = &value
		}
		if err := r.logAccessEvent(ctx, req, event.fact, event.accessType, event.score, rank, event.contextBlockType, breakdown); err != nil {
			return err
		}
	}
	return nil
}

func enrichScoreBreakdown(breakdown retrievalScoreBreakdown, query QueryAnalysis, confidence *RetrievalConfidence) retrievalScoreBreakdown {
	breakdown.QueryAnalysis = &retrievalQueryAnalysisBreakdown{
		RuleFit:                     query.Scores.RuleFit,
		AnchorReadiness:             query.Scores.AnchorReadiness,
		SemanticNeed:                query.Scores.SemanticNeed,
		ExpectedRetrievalConfidence: query.Scores.ExpectedRetrievalConfidence,
		SemanticMode:                query.Decision.SemanticMode,
		RetrievalMode:               query.Decision.RetrievalMode,
		Scores: retrievalQueryAnalysisScores{
			RuleFit:                     query.Scores.RuleFit,
			AnchorReadiness:             query.Scores.AnchorReadiness,
			SemanticNeed:                query.Scores.SemanticNeed,
			ExpectedRetrievalConfidence: query.Scores.ExpectedRetrievalConfidence,
		},
		Decision: retrievalQueryAnalysisDecision{
			SemanticMode:  query.Decision.SemanticMode,
			RetrievalMode: query.Decision.RetrievalMode,
		},
	}
	if confidence != nil {
		copy := *confidence
		breakdown.ObservedConfidence = &copy
	}
	return breakdown
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
	if analysis.TimeMode == QueryTimeModeHistorical || analysis.EvidenceNeed == EvidenceNeedStateTransition {
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
	normalizedQuery := strings.TrimSpace(strings.ToLower(query.Normalized))
	if normalizedQuery != "" && strings.Contains(normalizedText, normalizedQuery) {
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
	for _, term := range premiseCounterexampleExpansionsForQuery(query) {
		add(term)
	}
	for _, term := range cjkBigrams(query.Normalized) {
		add(term)
	}
	return terms
}

func directSlotCoverageBoost(query QueryAnalysis, slotCoverage float64) float64 {
	if !queryProtectsRawFloor(query) || slotCoverage <= 0 {
		return 0
	}
	return 0.16 * slotCoverage
}

func discriminatingSlotCoverage(query QueryAnalysis, searchText string) float64 {
	terms := discriminatingSlotTerms(query)
	if len(terms) == 0 {
		return 0
	}
	normalizedText := strings.ToLower(searchText)
	matches := 0
	for _, term := range terms {
		if strings.Contains(normalizedText, term) {
			matches++
		}
	}
	return float64(matches) / float64(len(terms))
}

func discriminatingSlotTerms(query QueryAnalysis) []string {
	seen := map[string]struct{}{}
	var terms []string
	add := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if !isDiscriminatingSlotTerm(value) {
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
	for _, term := range premiseCounterexampleExpansionsForQuery(query) {
		add(term)
	}
	if len(terms) == 0 {
		for _, term := range cjkBigrams(query.Normalized) {
			add(term)
		}
	}
	return terms
}

func isDiscriminatingSlotTerm(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return false
	}
	if _, ok := lowDiscriminationQueryTerms[value]; ok {
		return false
	}
	if len([]rune(value)) < 2 {
		return false
	}
	hasLetterOrNumber := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			hasLetterOrNumber = true
			continue
		}
		if unicode.IsSpace(r) {
			continue
		}
		return false
	}
	return hasLetterOrNumber
}

var lowDiscriminationQueryTerms = map[string]struct{}{
	"什么": {}, "什么事": {}, "什么时候": {}, "哪里": {}, "哪次": {}, "谁": {}, "多久": {},
	"怎么": {}, "为什么": {}, "是否": {}, "是不是": {}, "有没有": {}, "有无": {},
	"上次": {}, "那天": {}, "那次": {}, "最近": {}, "最近一次": {}, "一次": {},
	"以前": {}, "过去": {}, "现在": {}, "后来": {}, "当前": {},
	"我": {}, "你": {}, "他": {}, "她": {}, "它": {}, "我们": {}, "他们": {},
	"the": {}, "and": {}, "or": {}, "who": {}, "what": {}, "when": {}, "where": {}, "why": {}, "how": {},
}

func broadHubSuppression(query QueryAnalysis, fact core.Fact, candidate retrievalCandidate, completionSource string, lexicalCoverage float64, slotCoverage float64) float64 {
	if !queryProtectsRawFloor(query) || hasQuerySignal(query, QuerySignalReflectionSummary) {
		return 0
	}
	if completionSource == completionSourceEventBundle || slotCoverage >= 0.50 || lexicalCoverage >= 0.75 {
		return 0
	}
	if !looksLikeBroadHubSummary(fact, candidate) {
		return 0
	}
	return 0.45
}

func looksLikeBroadHubSummary(fact core.Fact, candidate retrievalCandidate) bool {
	if fact.FactType == core.FactTypeRelationalState || fact.FactType == core.FactTypeCoreIdentity {
		return true
	}
	text := strings.ToLower(strings.Join(nonEmptyStrings(
		fact.ContentSummary,
		string(fact.Predicate),
	), " "))
	for _, marker := range []string{
		"summary", "overall", "relationship", "pattern", "theme", "stable",
		"总结", "整体", "关系", "模式", "主题", "长期", "稳定", "通常", "经常", "倾向",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	for _, item := range candidate.SourceBreakdown {
		switch item.Source {
		case "semantic_rewrite_dense", "semantic_anchor_dense", AnchorSourceNarrativeInsight, AnchorSourceRecentImportant:
			return true
		}
	}
	return false
}

func reflectionSummaryBoost(query QueryAnalysis, fact core.Fact, searchText string, candidate retrievalCandidate) float64 {
	if !hasQuerySignal(query, QuerySignalReflectionSummary) {
		return 0
	}
	text := strings.ToLower(strings.Join(nonEmptyStrings(
		fact.ContentSummary,
		string(fact.Predicate),
		string(fact.FactType),
		searchText,
	), " "))
	for _, marker := range []string{
		"reflection", "growth", "progress", "self_reflection", "growth_summary",
		"反思", "复盘", "成长", "进步", "变化", "调整", "改善", "主动",
	} {
		if strings.Contains(text, marker) {
			return 0.30
		}
	}
	for _, item := range candidate.SourceBreakdown {
		if item.Source == AnchorSourceNarrativeInsight {
			return 0.20
		}
	}
	return 0
}

func premiseRestatementPenalty(query QueryAnalysis, fact core.Fact, searchText string) float64 {
	if !queryWantsPremiseCounterexample(query) {
		return 0
	}
	text := strings.ToLower(strings.Join(nonEmptyStrings(
		fact.ContentSummary,
		string(fact.Predicate),
		string(fact.FactType),
		stringFromPtr(fact.ObjectLiteral),
		searchText,
	), " "))
	if strings.TrimSpace(text) == "" {
		return 0
	}
	if containsAny(text, premiseCounterexamplePositiveMarkers...) ||
		containsAny(text, "不再", "不是所有", "反例", "例外", "恢复", "解决", "和解", "不能暴露", "不能把", "禁止", "不要", "不得", "不允许") {
		return 0
	}
	if !containsAny(text, "不会", "不能", "没有", "没", "不是", "不", "从来", "完全", "一点", "任何", "每个", "所有", "只有", "never", "not ", "no ") {
		return 0
	}
	queryTerms := premiseCounterexampleConceptTerms(strings.Join(nonEmptyStrings(query.Raw, query.Normalized), " "))
	if len(queryTerms) == 0 || textMatchAny(text, queryTerms) {
		return 1.0
	}
	return 0
}

func textMatchAny(text string, terms []string) bool {
	for _, term := range terms {
		term = strings.TrimSpace(strings.ToLower(term))
		if term != "" && strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func premiseCounterexampleExpansionsForQuery(query QueryAnalysis) []string {
	if !queryAllowsPremiseCounterexampleExpansion(query) {
		return nil
	}
	return deterministicPremiseCounterexampleExpansions(strings.Join(nonEmptyStrings(query.Raw, query.Normalized), " "))
}

func queryAllowsPremiseCounterexampleExpansion(query QueryAnalysis) bool {
	return queryWantsPremiseCounterexample(query) ||
		hasQuerySignal(query, QuerySignalPremiseCounterexample)
}

func deterministicPremiseCounterexampleExpansions(query string) []string {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return nil
	}
	if !looksLikeNegatedPremiseQuery(normalized) {
		return nil
	}
	expansions := append([]string{}, premiseCounterexamplePositiveMarkers...)
	concepts := premiseCounterexampleConceptTerms(normalized)
	expansions = append(expansions, concepts...)
	for _, concept := range concepts {
		expansions = append(expansions, cjkBigrams(concept)...)
	}
	for _, group := range premiseCounterexampleConceptGroups {
		if containsAny(normalized, group.triggers...) || containsAny(strings.Join(concepts, " "), group.triggers...) {
			expansions = append(expansions, group.expansions...)
		}
	}
	return uniqueOrderedStrings(expansions)
}

func looksLikeNegatedPremiseQuery(query string) bool {
	return containsAny(query,
		"不会", "不能", "不再", "没有", "没", "不是", "不", "从来", "完全", "一点", "任何", "每个", "所有", "只有", "再也",
		"never", "no ", "not ", "nothing", "only ", "always ",
	)
}

func premiseCounterexampleConceptTerms(query string) []string {
	var concepts []string
	for _, marker := range []string{"从来没有", "从来没", "从来不", "完全没有", "完全没", "完全不", "再也没有", "再也没", "再也不", "不会", "不能", "没有", "不是", "不再", "没", "不"} {
		concepts = append(concepts, extractCounterexampleConceptAfter(query, marker)...)
	}
	for _, term := range cjkBigrams(removePremiseCounterexampleNoise(query)) {
		concepts = append(concepts, term)
	}
	return uniqueOrderedStrings(concepts)
}

func extractCounterexampleConceptAfter(query, marker string) []string {
	idx := strings.Index(query, marker)
	if idx < 0 {
		return nil
	}
	rest := strings.TrimSpace(query[idx+len(marker):])
	if rest == "" {
		return nil
	}
	var runes []rune
	for _, r := range rest {
		if unicode.IsSpace(r) || strings.ContainsRune("，。？！；,.?!;:：、（）()[]【】\"'“”‘’", r) {
			break
		}
		runes = append(runes, r)
		if len(runes) >= 8 {
			break
		}
	}
	concept := strings.TrimSpace(string(runes))
	concept = removePremiseCounterexampleNoise(concept)
	if concept == "" {
		return nil
	}
	terms := []string{concept}
	conceptRunes := []rune(concept)
	if len(conceptRunes) >= 2 {
		terms = append(terms, cjkBigrams(concept)...)
	}
	if len(conceptRunes) >= 1 {
		terms = append(terms, string(conceptRunes[0])+"了", string(conceptRunes[0])+"过")
	}
	return terms
}

func removePremiseCounterexampleNoise(value string) string {
	replacer := strings.NewReplacer(
		"是不是", "", "是否", "", "完全", "", "从来", "", "一直", "", "再也", "",
		"任何", "", "所有", "", "每个", "", "只有", "", "还是", "", "都", "",
		"吗", "", "呢", "", "啊", "", "？", "", "?", "",
	)
	return strings.TrimSpace(replacer.Replace(value))
}

type premiseCounterexampleConceptGroup struct {
	triggers   []string
	expansions []string
}

var premiseCounterexamplePositiveMarkers = []string{
	"后来", "现在", "已经", "开始", "尝试", "做到", "做了", "做过", "变得", "改善", "好转", "恢复", "坚持", "主动", "成功", "可以", "能够",
	"counterexample", "exception", "now", "later", "started", "improved", "resolved", "able",
}

var premiseCounterexampleConceptGroups = []premiseCounterexampleConceptGroup{
	{
		triggers:   []string{"做饭", "做菜", "下厨", "厨房", "cooking", "cook"},
		expansions: []string{"做饭", "做菜", "下厨", "做了", "做过", "会做", "在家做"},
	},
	{
		triggers:   []string{"运动", "锻炼", "健身", "exercise", "workout"},
		expansions: []string{"运动", "锻炼", "健身", "开始运动", "坚持锻炼", "运动习惯"},
	},
	{
		triggers:   []string{"改变", "变化", "老样子", "change", "same"},
		expansions: []string{"改变", "变化", "改善", "调整", "进步", "翻篇"},
	},
	{
		triggers:   []string{"朋友", "关系", "矛盾", "吵架", "人际", "friend", "relationship", "conflict"},
		expansions: []string{"和解", "道歉", "互相理解", "关系修复", "支持", "一起"},
	},
	{
		triggers:   []string{"睡", "失眠", "睡眠", "sleep", "insomnia"},
		expansions: []string{"睡着", "睡眠改善", "睡得", "好转"},
	},
}

func uniqueOrderedStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
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
