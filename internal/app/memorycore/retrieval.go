package memorycore

import (
	"context"
	"sort"
	"strings"
	"time"
	"unicode"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const (
	maxRerankQueryTextRune               = 160
	selectiveRerankTopN                  = 12
	semanticCorrectionMaxOverallDrop     = 0.05
	semanticCorrectionMinDimensionGain   = 0.10
	retrievalConfidenceComparisonEpsilon = 0.000001
)

func (s *service) Retrieve(ctx context.Context, req RetrievalRequest) (*MemoryContext, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	policy := req.Policy
	now := req.Now
	if now.IsZero() {
		now = s.now()
	}
	queryReq := QueryAnalysisRequest{
		PersonaID: personaID,
		SessionID: req.SessionID,
		QueryText: req.QueryText,
		Now:       now,
		Policy:    policy,
	}
	ruleAnalysis, err := s.analyzeRetrievalRuleQuery(ctx, queryReq)
	if err != nil {
		return nil, err
	}
	attempt, err := s.runRetrievalAttempt(ctx, req, queryReq, personaID, now, policy, ruleAnalysis, retrievalAttemptOptions{})
	if err != nil {
		return nil, err
	}
	preview, err := s.completeRetrievalAttempt(ctx, attempt, false)
	if err != nil {
		return nil, err
	}
	action := retrievalCorrectiveAction(preview)
	if action == "" || action == memsqlite.RetrievalCorrectiveActionSuppressMemoryInjection {
		return s.completeRetrievalAttempt(ctx, attempt, true)
	}
	if action == memsqlite.RetrievalCorrectiveActionSemanticLight &&
		preview.QueryAnalysis != nil &&
		preview.QueryAnalysis.Source == QueryAnalysisSourceSemanticFallback {
		return s.completeRetrievalAttempt(ctx, attempt, true)
	}
	corrected, ok, err := s.runCorrectiveRetrievalAttempt(ctx, req, queryReq, personaID, now, policy, ruleAnalysis, action)
	if err != nil {
		return nil, err
	}
	if !ok {
		return s.completeRetrievalAttempt(ctx, attempt, true)
	}
	correctedPreview, err := s.completeRetrievalAttempt(ctx, corrected, false)
	if err != nil {
		return nil, err
	}
	if !shouldUseCorrectedRetrieval(preview, correctedPreview) {
		return s.completeRetrievalAttempt(ctx, attempt, true)
	}
	result, err := s.completeRetrievalAttemptWithAction(ctx, corrected, true, action)
	if err != nil {
		return nil, err
	}
	if action == memsqlite.RetrievalCorrectiveActionSQLiteFallback {
		preserveOriginalRetrievalDiagnostics(result, preview)
	}
	annotateCorrectiveAction(result, action)
	return result, nil
}

type retrievalAttemptOptions struct {
	SQLiteFallback bool
	Analysis       *memsqlite.QueryAnalysis
}

type retrievalAttemptResult struct {
	finalCandidates   memsqlite.PreparedFinalCandidates
	rerankResults     []memsqlite.RerankResultItem
	rerankDiagnostics *memsqlite.RerankDiagnostics
}

func (s *service) runCorrectiveRetrievalAttempt(ctx context.Context, req RetrievalRequest, queryReq QueryAnalysisRequest, personaID string, now time.Time, policy RetrievalPolicy, ruleAnalysis memsqlite.QueryAnalysis, action string) (retrievalAttemptResult, bool, error) {
	switch action {
	case memsqlite.RetrievalCorrectiveActionSQLiteFallback:
		result, err := s.runRetrievalAttempt(ctx, req, queryReq, personaID, now, policy, ruleAnalysis, retrievalAttemptOptions{SQLiteFallback: true})
		return result, true, err
	case memsqlite.RetrievalCorrectiveActionSemanticLight:
		semantic, attempted, ok := s.correctiveSemanticLight(ctx, queryReq, ruleAnalysis)
		if ok {
			result, err := s.runRetrievalAttempt(ctx, req, queryReq, personaID, now, policy, ruleAnalysis, retrievalAttemptOptions{Analysis: &semantic})
			if err == nil {
				return result, true, nil
			}
		}
		if !attempted {
			return retrievalAttemptResult{}, false, nil
		}
		result, err := s.runRetrievalAttempt(ctx, req, queryReq, personaID, now, policy, ruleAnalysis, retrievalAttemptOptions{SQLiteFallback: true})
		return result, true, err
	default:
		return retrievalAttemptResult{}, false, nil
	}
}

func (s *service) correctiveSemanticLight(ctx context.Context, req QueryAnalysisRequest, rule memsqlite.QueryAnalysis) (memsqlite.QueryAnalysis, bool, bool) {
	if s.queryPipeline.semantic == nil ||
		s.queryPipeline.options.Provider != QueryAnalysisProviderSidecar ||
		rule.Source == memsqlite.QueryAnalysisSourceSemanticFallback {
		return memsqlite.QueryAnalysis{}, false, false
	}
	semanticReq := s.queryPipeline.semanticRequestForRule(req, rule, memsqlite.RetrievalCorrectiveActionSemanticLight)
	stageCtx := ctx
	cancel := func() {}
	if s.queryPipeline.options.Timeout > 0 {
		stageCtx, cancel = context.WithTimeout(ctx, s.queryPipeline.options.Timeout)
	}
	defer cancel()
	semantic, err := s.queryPipeline.analyzeSemantic(stageCtx, semanticReq)
	if err != nil || semantic == nil || semantic.Status != "ok" || semantic.Degraded {
		return memsqlite.QueryAnalysis{}, true, false
	}
	return mergeSemanticQueryAnalysis(rule, *semantic, s.queryPipeline.options, semanticReq.VisibleEntityHints), true, true
}

func retrievalCorrectiveAction(result *MemoryContext) string {
	if result == nil || result.RetrievalConfidence == nil {
		return ""
	}
	return result.RetrievalConfidence.CorrectiveAction
}

func annotateCorrectiveAction(result *MemoryContext, action string) {
	if result == nil {
		return
	}
	if result.RetrievalConfidence == nil {
		result.RetrievalConfidence = &RetrievalConfidence{}
	}
	result.RetrievalConfidence.CorrectiveAction = action
}

func preserveOriginalRetrievalDiagnostics(result *MemoryContext, original *MemoryContext) {
	if result == nil || original == nil {
		return
	}
	result.Mirror = original.Mirror
	result.GraphActivation = original.GraphActivation
	result.Rerank = original.Rerank
	result.AnchorFusion = original.AnchorFusion
}

func shouldUseCorrectedRetrieval(original *MemoryContext, corrected *MemoryContext) bool {
	if corrected == nil {
		return false
	}
	if original == nil || original.RetrievalConfidence == nil || corrected.RetrievalConfidence == nil {
		return true
	}
	if corrected.RetrievalConfidence.CorrectiveAction == memsqlite.RetrievalCorrectiveActionSuppressMemoryInjection {
		return true
	}
	action := original.RetrievalConfidence.CorrectiveAction
	originalIDs := memoryContextNodeIDs(original)
	correctedIDs := memoryContextNodeIDs(corrected)
	if len(originalIDs) == 0 && len(correctedIDs) > 0 {
		return true
	}
	if len(originalIDs) == 0 && len(correctedIDs) == 0 {
		return false
	}
	if len(originalIDs) > 0 && len(correctedIDs) == 0 {
		return false
	}
	if action == memsqlite.RetrievalCorrectiveActionSemanticLight {
		return semanticCorrectionImproves(original.RetrievalConfidence, corrected.RetrievalConfidence)
	}
	if !sameStringSet(originalIDs, correctedIDs) {
		return false
	}
	if action == memsqlite.RetrievalCorrectiveActionSQLiteFallback {
		return sqliteFallbackCorrectionImproves(original.RetrievalConfidence, corrected.RetrievalConfidence)
	}
	return corrected.RetrievalConfidence.Overall+0.000001 >= original.RetrievalConfidence.Overall
}

func semanticCorrectionImproves(original *RetrievalConfidence, corrected *RetrievalConfidence) bool {
	if corrected.Overall+retrievalConfidenceComparisonEpsilon >= original.Overall {
		return true
	}
	if corrected.Overall+semanticCorrectionMaxOverallDrop < original.Overall {
		return false
	}
	return corrected.AnchorCoverage >= original.AnchorCoverage+semanticCorrectionMinDimensionGain ||
		corrected.RequiredChainCoverage >= original.RequiredChainCoverage+semanticCorrectionMinDimensionGain ||
		corrected.SourceDiversity >= original.SourceDiversity+semanticCorrectionMinDimensionGain ||
		corrected.MMRDiversity >= original.MMRDiversity+semanticCorrectionMinDimensionGain
}

func sqliteFallbackCorrectionImproves(original *RetrievalConfidence, corrected *RetrievalConfidence) bool {
	return corrected.AuthorityPassRatio > original.AuthorityPassRatio+retrievalConfidenceComparisonEpsilon &&
		corrected.Overall+0.10 >= original.Overall
}

func memoryContextNodeIDs(context *MemoryContext) []string {
	if context == nil {
		return nil
	}
	var ids []string
	for _, block := range context.Blocks {
		for _, item := range block.Items {
			if strings.TrimSpace(item.NodeID) != "" {
				ids = append(ids, item.NodeID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (s *service) runRetrievalAttempt(ctx context.Context, req RetrievalRequest, queryReq QueryAnalysisRequest, personaID string, now time.Time, policy RetrievalPolicy, ruleAnalysis memsqlite.QueryAnalysis, options retrievalAttemptOptions) (retrievalAttemptResult, error) {
	analysis := ruleAnalysis
	if options.Analysis != nil {
		analysis = *options.Analysis
	}
	if options.SQLiteFallback {
		policy.UseMirror = false
		policy.UseFTS = true
	} else if options.Analysis == nil {
		semanticLane := s.startSemanticQueryAnalysisLane(ctx, queryReq, ruleAnalysis)
		if semanticLane != nil {
			defer semanticLane.cancel()
		}
		if semantic, ok := s.softJoinSemanticQueryAnalysisLane(ctx, semanticLane, ruleAnalysis); ok {
			analysis = semantic
		}
	}
	sidecarCtx, sidecarCancel := sidecarTotalContext(ctx, s.sidecarResilience.Timeouts.Total)
	defer sidecarCancel()
	var mirrorCandidates []memsqlite.RetrievalMirrorCandidate
	mirrorDiagnostics := &memsqlite.MirrorDiagnostics{Status: "disabled_by_corrective_sqlite_fallback", Degraded: true, FallbackReason: "observed_confidence_sqlite_fallback"}
	var err error
	if !options.SQLiteFallback {
		mirrorCandidates, mirrorDiagnostics, err = s.mirrorFactCandidates(ctx, sidecarCtx, personaID, req.QueryText, ruleAnalysis, policy, false)
		if err != nil {
			return retrievalAttemptResult{}, err
		}
		if analysis.Source == memsqlite.QueryAnalysisSourceMerged {
			semanticCandidates, semanticDiagnostics, err := s.mirrorFactCandidates(ctx, sidecarCtx, personaID, req.QueryText, analysis, policy, true)
			if err != nil {
				return retrievalAttemptResult{}, err
			}
			mirrorCandidates = mergeRetrievalMirrorCandidates(mirrorCandidates, semanticCandidates)
			mirrorDiagnostics = mergeMirrorDiagnostics(mirrorDiagnostics, semanticDiagnostics)
		}
	}
	prepared, err := s.retrieve.Prepare(ctx, memsqlite.RetrievalRequest{
		PersonaID:                personaID,
		SessionID:                req.SessionID,
		QueryText:                req.QueryText,
		Now:                      now,
		PrecomputedQueryAnalysis: &analysis,
		RawRuleQueryAnalysis:     &ruleAnalysis,
		Policy: memsqlite.RetrievalPolicy{
			SensitivityPermission: policy.SensitivityPermission,
			AllowHistorical:       policy.AllowHistorical,
			AllowDeepArchive:      policy.AllowDeepArchive,
			FinalMemoryCount:      policy.FinalMemoryCount,
			ContextBudgetTokens:   policy.ContextBudgetTokens,
			UseFTS:                policy.UseFTS,
			UseMirror:             policy.UseMirror,
		},
		Context: memsqlite.RetrievalAffectContext{
			UserMoodLabel:         req.Context.UserMoodLabel,
			RelationshipMoodLabel: req.Context.RelationshipMoodLabel,
		},
		Mirror:            mirrorCandidates,
		MirrorDiagnostics: mirrorDiagnostics,
	})
	if err != nil {
		return retrievalAttemptResult{}, err
	}
	graphCandidates, graphDiagnostics, err := s.graphActivationCandidates(ctx, sidecarCtx, personaID, prepared)
	if err != nil {
		return retrievalAttemptResult{}, err
	}
	finalCandidates, safeRerankCandidates, err := s.retrieve.BuildRerankCandidates(ctx, prepared, graphCandidates, graphDiagnostics)
	if err != nil {
		return retrievalAttemptResult{}, err
	}
	rerankResults, rerankDiagnostics, err := s.rerankCandidates(ctx, sidecarCtx, personaID, prepared, safeRerankCandidates, graphDiagnostics)
	if err != nil {
		return retrievalAttemptResult{}, err
	}
	return retrievalAttemptResult{
		finalCandidates:   finalCandidates,
		rerankResults:     rerankResults,
		rerankDiagnostics: rerankDiagnostics,
	}, nil
}

func (s *service) completeRetrievalAttempt(ctx context.Context, attempt retrievalAttemptResult, logAccess bool) (*MemoryContext, error) {
	return s.completeRetrievalAttemptWithAction(ctx, attempt, logAccess, "")
}

func (s *service) completeRetrievalAttemptWithAction(ctx context.Context, attempt retrievalAttemptResult, logAccess bool, correctiveAction string) (*MemoryContext, error) {
	var result memsqlite.MemoryContext
	var err error
	if logAccess && correctiveAction != "" {
		result, err = s.retrieve.CompleteFinalWithCorrectiveAction(ctx, attempt.finalCandidates, attempt.rerankResults, attempt.rerankDiagnostics, correctiveAction)
	} else if logAccess {
		result, err = s.retrieve.CompleteFinal(ctx, attempt.finalCandidates, attempt.rerankResults, attempt.rerankDiagnostics)
	} else {
		result, err = s.retrieve.CompleteFinalPreview(ctx, attempt.finalCandidates, attempt.rerankResults, attempt.rerankDiagnostics)
	}
	if err != nil {
		return nil, err
	}
	return memoryContextFromStore(result), nil
}

func (s *service) analyzeRetrievalQuery(ctx context.Context, req QueryAnalysisRequest) (memsqlite.QueryAnalysis, error) {
	if s.queryAnalyzer != nil {
		return s.queryAnalyzer.AnalyzeQuery(ctx, req)
	}
	return s.retrieve.AnalyzeQuery(ctx, req.PersonaID, req.QueryText, retrievalPolicyToStore(req.Policy))
}

func (s *service) analyzeRetrievalRuleQuery(ctx context.Context, req QueryAnalysisRequest) (memsqlite.QueryAnalysis, error) {
	if s.queryPipeline.rule != nil {
		return s.queryPipeline.AnalyzeRuleQuery(ctx, req)
	}
	return s.analyzeRetrievalQuery(ctx, req)
}

type semanticQueryAnalysisLane struct {
	ch     <-chan memsqlite.QueryAnalysis
	cancel context.CancelFunc
}

func (s *service) startSemanticQueryAnalysisLane(ctx context.Context, req QueryAnalysisRequest, rule memsqlite.QueryAnalysis) *semanticQueryAnalysisLane {
	if s.queryPipeline.rule == nil || !s.queryPipeline.shouldUseSemantic(rule) {
		return nil
	}
	laneCtx, cancel := context.WithCancel(ctx)
	ch := make(chan memsqlite.QueryAnalysis, 1)
	go func() {
		ch <- s.queryPipeline.AnalyzeSemanticForRule(laneCtx, req, rule)
	}()
	return &semanticQueryAnalysisLane{ch: ch, cancel: cancel}
}

func (s *service) softJoinSemanticQueryAnalysisLane(ctx context.Context, lane *semanticQueryAnalysisLane, rule memsqlite.QueryAnalysis) (memsqlite.QueryAnalysis, bool) {
	if lane == nil {
		return memsqlite.QueryAnalysis{}, false
	}
	select {
	case analysis := <-lane.ch:
		lane.cancel()
		return analysis, true
	default:
	}
	timeout := queryAnalysisSoftJoinTimeout(s.queryPipeline.options)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case analysis := <-lane.ch:
		lane.cancel()
		return analysis, true
	case <-timer.C:
		latencyMs := int64(timeout / time.Millisecond)
		lane.cancel()
		return semanticRuleFallback(rule, "semantic_soft_timeout", SemanticQueryAnalysisResult{LatencyMs: latencyMs}), true
	case <-ctx.Done():
		lane.cancel()
		return semanticRuleFallback(rule, "go_context_timeout", SemanticQueryAnalysisResult{}), true
	}
}

func queryAnalysisSoftJoinTimeout(options QueryAnalysisOptions) time.Duration {
	options = normalizeQueryAnalysisOptions(options)
	if options.SoftJoinTimeout > 0 {
		return options.SoftJoinTimeout
	}
	return options.Timeout
}

func (s *service) mirrorFactCandidates(ctx context.Context, sidecarCtx context.Context, personaID string, queryText string, analysis memsqlite.QueryAnalysis, policy RetrievalPolicy, semanticOnly bool) ([]memsqlite.RetrievalMirrorCandidate, *memsqlite.MirrorDiagnostics, error) {
	diagnostics := &memsqlite.MirrorDiagnostics{Status: "disabled_by_config"}
	if !policy.UseMirror {
		return nil, diagnostics, nil
	}
	diagnostics.Status = "persona_not_ready"
	ready, err := s.mirrorState.IsReady(ctx, personaID)
	if err != nil {
		return nil, nil, err
	}
	if !ready {
		return nil, diagnostics, nil
	}
	diagnostics.Status = "adapter_missing"
	candidateAdapter, ok := s.mirrorAdapter.(MirrorCandidateAdapter)
	if !ok || candidateAdapter == nil {
		return nil, diagnostics, nil
	}
	if s.sidecarBreaker != nil && !s.sidecarBreaker.allow(personaID, sidecarStageMirror) {
		diagnostics.Status = "breaker_open"
		diagnostics.Degraded = true
		diagnostics.FallbackReason = "sidecar_breaker_open"
		return nil, diagnostics, nil
	}
	limit := policy.FinalMemoryCount
	if limit <= 0 {
		limit = 8
	}
	started := time.Now()
	stageCtx, cancel, ok := sidecarStageContext(ctx, sidecarCtx, sidecarStageTimeout(s.sidecarResilience, sidecarStageMirror))
	if !ok {
		diagnostics.Status = sidecarStatusSkippedByBudget
		diagnostics.Degraded = true
		diagnostics.FallbackReason = "sidecar_timeout"
		return nil, diagnostics, nil
	}
	defer cancel()
	result, err := candidateAdapter.FindCandidates(stageCtx, MirrorCandidateRequest{
		PersonaID: personaID,
		QueryText: queryText,
		Query:     *queryAnalysisFromStore(&analysis),
		Limit:     limit * 4,
	})
	diagnostics.LatencyMs = time.Since(started).Milliseconds()
	if err != nil || result == nil {
		status, topErr := classifySidecarStageError(ctx, stageCtx, err)
		if topErr != nil {
			return nil, diagnostics, topErr
		}
		diagnostics.Status = status
		diagnostics.Degraded = true
		diagnostics.FallbackReason = sanitizeSidecarFallbackReason(status)
		s.recordSidecarStage(personaID, sidecarStageMirror, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	diagnostics.EmbeddingCacheHits = result.EmbeddingCacheHits
	diagnostics.EmbeddingCacheMisses = result.EmbeddingCacheMisses
	diagnostics.EmbeddingLiveCallCount = result.EmbeddingLiveCallCount
	diagnostics.QueryCount = result.Diagnostics.QueryCount
	diagnostics.RawQueryCount = result.Diagnostics.RawQueryCount
	diagnostics.RewriteQueryCount = result.Diagnostics.RewriteQueryCount
	diagnostics.AnchorQueryCount = result.Diagnostics.AnchorQueryCount
	diagnostics.MergedCandidateCount = result.Diagnostics.MergedCandidateCount
	diagnostics.QueryTrimCount = result.Diagnostics.QueryTrimCount
	diagnostics.DenseEmbeddingWallLatencyMs = result.Diagnostics.DenseEmbeddingWallLatencyMs
	diagnostics.DenseEmbeddingBatchLatencyMs = result.Diagnostics.DenseEmbeddingBatchLatencyMs
	diagnostics.DenseSearchTotalLatencyMs = result.Diagnostics.DenseSearchTotalLatencyMs
	diagnostics.QueryCountTrimmedByBudget = result.Diagnostics.QueryCountTrimmedByBudget
	diagnostics.PerQuery = mirrorPerQueryDiagnosticsToStore(result.Diagnostics.PerQuery)
	if result.Degraded {
		diagnostics.Status = "sidecar_degraded"
		diagnostics.Degraded = true
		diagnostics.FallbackReason = sanitizeRetrievalSidecarFallbackReason(result.FallbackReason)
		s.recordSidecarStage(personaID, sidecarStageMirror, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	diagnostics.SidecarCandidateCount = len(result.Candidates)
	if len(result.Candidates) == 0 {
		diagnostics.Status = "no_candidates"
		s.recordSidecarStage(personaID, sidecarStageMirror, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	candidates := make([]memsqlite.MirrorCandidate, 0, len(result.Candidates))
	var operationTargetDiagnostics []memsqlite.MirrorCandidateDiagnostic
	for idx, candidate := range result.Candidates {
		if semanticOnly && isRawMirrorCandidateSource(candidate.Source) {
			continue
		}
		rank := candidate.Rank
		if rank <= 0 {
			rank = idx + 1
		}
		storeCandidate := memsqlite.MirrorCandidate{
			TriviumNodeID:   candidate.TriviumNodeID,
			Score:           candidate.Score,
			Source:          candidate.Source,
			PrimaryPurpose:  candidate.PrimaryPurpose,
			Rank:            rank,
			HitCount:        candidate.HitCount,
			SourceBreakdown: mirrorCandidateSourceBreakdownToStore(candidate.SourceBreakdown),
		}
		if isForgetDeleteMirrorCandidate(analysis) {
			operationTargetDiagnostics = append(operationTargetDiagnostics, memsqlite.MirrorCandidateDiagnostic{
				TriviumNodeID:  storeCandidate.TriviumNodeID,
				Score:          storeCandidate.Score,
				Source:         storeCandidate.Source,
				PrimaryPurpose: storeCandidate.PrimaryPurpose,
				Rank:           storeCandidate.Rank,
				HitCount:       storeCandidate.HitCount,
				DropReason:     forgetDeleteMirrorDropReason(candidate),
			})
			continue
		}
		candidates = append(candidates, storeCandidate)
	}
	if len(candidates) == 0 {
		diagnostics.Candidates = operationTargetDiagnostics
		diagnostics.DroppedCandidateCount = len(operationTargetDiagnostics)
		if len(operationTargetDiagnostics) > 0 {
			diagnostics.Status = "forget_delete_not_context"
		} else {
			diagnostics.Status = "no_candidates"
		}
		s.recordSidecarStage(personaID, sidecarStageMirror, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	report, err := s.mirrorMap.MapFactCandidatesWithDiagnostics(ctx, personaID, candidates)
	if err != nil {
		return nil, nil, err
	}
	diagnostics.SidecarCandidateCount = report.SidecarCandidateCount
	diagnostics.MappedCandidateCount = report.MappedCandidateCount
	diagnostics.DroppedCandidateCount = report.DroppedCandidateCount + len(operationTargetDiagnostics)
	diagnostics.Candidates = make([]memsqlite.MirrorCandidateDiagnostic, 0, len(operationTargetDiagnostics)+len(report.Diagnostics))
	diagnostics.Candidates = append(diagnostics.Candidates, operationTargetDiagnostics...)
	for _, item := range report.Diagnostics {
		diagnostics.Candidates = append(diagnostics.Candidates, memsqlite.MirrorCandidateDiagnostic{
			TriviumNodeID:  item.TriviumNodeID,
			SQLiteFactID:   item.SQLiteFactID,
			Score:          item.Score,
			Source:         item.Source,
			PrimaryPurpose: item.PrimaryPurpose,
			Rank:           item.Rank,
			HitCount:       item.HitCount,
			DropReason:     item.DropReason,
		})
	}
	if diagnostics.MappedCandidateCount == 0 && diagnostics.SidecarCandidateCount > 0 {
		diagnostics.Status = "candidates_unmapped_or_stale"
	} else {
		diagnostics.Status = "used"
	}
	s.recordSidecarStage(personaID, sidecarStageMirror, diagnostics.Status, diagnostics.FallbackReason)
	return report.Mapped, diagnostics, nil
}

func mirrorCandidateSourceBreakdownToStore(values []MirrorCandidateSourceBreakdown) []memsqlite.MirrorCandidateSourceBreakdown {
	result := make([]memsqlite.MirrorCandidateSourceBreakdown, 0, len(values))
	for _, value := range values {
		result = append(result, memsqlite.MirrorCandidateSourceBreakdown{
			Source:  value.Source,
			Purpose: value.Purpose,
			Rank:    value.Rank,
			Score:   value.Score,
			Weight:  value.Weight,
		})
	}
	return result
}

func (s *service) graphActivationCandidates(ctx context.Context, sidecarCtx context.Context, personaID string, prepared memsqlite.PreparedRetrieval) ([]memsqlite.RetrievalActivationCandidate, *memsqlite.GraphActivationDiagnostics, error) {
	diagnostics := &memsqlite.GraphActivationDiagnostics{Status: "disabled_by_config"}
	if !prepared.Policy.UseMirror {
		return nil, diagnostics, nil
	}
	if hasStoreQuerySignal(prepared.Query.Signals, memsqlite.QuerySignalForgetDelete) {
		diagnostics.Status = "skipped_for_forget_delete"
		return nil, diagnostics, nil
	}
	diagnostics.Status = "persona_not_ready"
	ready, err := s.mirrorState.IsReady(ctx, personaID)
	if err != nil {
		return nil, nil, err
	}
	if !ready {
		return nil, diagnostics, nil
	}
	diagnostics.Status = "adapter_missing"
	activationAdapter, ok := s.mirrorAdapter.(MirrorActivationAdapter)
	if !ok || activationAdapter == nil {
		return nil, diagnostics, nil
	}
	seeds, err := s.mirrorMap.MapActivationSeeds(ctx, personaID, prepared.FusedAnchors)
	if err != nil {
		return nil, nil, err
	}
	if len(seeds) == 0 {
		diagnostics.Status = "no_seeds"
		return nil, diagnostics, nil
	}
	if s.sidecarBreaker != nil && !s.sidecarBreaker.allow(personaID, sidecarStageActivation) {
		diagnostics.Status = "breaker_open"
		diagnostics.Degraded = true
		diagnostics.FallbackReason = "sidecar_breaker_open"
		return nil, diagnostics, nil
	}
	started := time.Now()
	stageCtx, cancel, ok := sidecarStageContext(ctx, sidecarCtx, sidecarStageTimeout(s.sidecarResilience, sidecarStageActivation))
	if !ok {
		diagnostics.Status = sidecarStatusSkippedByBudget
		diagnostics.Degraded = true
		diagnostics.FallbackReason = "sidecar_timeout"
		return nil, diagnostics, nil
	}
	defer cancel()
	result, err := activationAdapter.ActivateGraph(stageCtx, MirrorActivationRequest{
		PersonaID: personaID,
		Seeds:     mirrorActivationSeedsFromStore(seeds),
		Params:    s.defaultMirrorActivationParams(),
	})
	diagnostics.LatencyMs = time.Since(started).Milliseconds()
	if err != nil || result == nil {
		status, topErr := classifySidecarStageError(ctx, stageCtx, err)
		if topErr != nil {
			return nil, diagnostics, topErr
		}
		diagnostics.Status = status
		diagnostics.Degraded = true
		diagnostics.FallbackReason = sanitizeSidecarFallbackReason(status)
		s.recordSidecarStage(personaID, sidecarStageActivation, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	diagnostics.SidecarCandidateCount = len(result.Candidates)
	diagnostics.Degraded = result.Degraded
	diagnostics.FallbackReason = sanitizeRetrievalSidecarFallbackReason(result.FallbackReason)
	if result.Degraded {
		diagnostics.Status = "sidecar_degraded"
		if diagnostics.FallbackReason != "activation_budget_exceeded" || len(result.Candidates) == 0 {
			s.recordSidecarStage(personaID, sidecarStageActivation, diagnostics.Status, diagnostics.FallbackReason)
			return nil, diagnostics, nil
		}
	}
	if len(result.Candidates) == 0 {
		diagnostics.Status = "no_candidates"
		s.recordSidecarStage(personaID, sidecarStageActivation, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	report, err := s.mirrorMap.MapActivationCandidatesWithDiagnostics(ctx, personaID, activationCandidatesToStore(result.Candidates))
	if err != nil {
		return nil, nil, err
	}
	diagnostics.SidecarCandidateCount = report.SidecarCandidateCount
	diagnostics.MappedCandidateCount = report.MappedCandidateCount
	diagnostics.DroppedCandidateCount = report.DroppedCandidateCount
	diagnostics.Candidates = make([]memsqlite.GraphActivationCandidateDiagnostic, 0, len(report.Diagnostics))
	for _, item := range report.Diagnostics {
		diagnostics.Candidates = append(diagnostics.Candidates, memsqlite.GraphActivationCandidateDiagnostic{
			TriviumNodeID: item.TriviumNodeID,
			SQLiteNodeID:  item.SQLiteNodeID,
			NodeType:      item.NodeType,
			Score:         item.Score,
			Source:        item.Source,
			Rank:          item.Rank,
			DropReason:    item.DropReason,
			Paths:         graphActivationPathsToStore(item.Paths),
		})
	}
	if result.Degraded {
		diagnostics.Status = "sidecar_degraded"
	} else if diagnostics.MappedCandidateCount == 0 && diagnostics.SidecarCandidateCount > 0 {
		diagnostics.Status = "candidates_unmapped_or_stale"
	} else {
		diagnostics.Status = "used"
	}
	s.recordSidecarStage(personaID, sidecarStageActivation, diagnostics.Status, diagnostics.FallbackReason)
	return report.Mapped, diagnostics, nil
}

func (s *service) rerankCandidates(ctx context.Context, sidecarCtx context.Context, personaID string, prepared memsqlite.PreparedRetrieval, candidates []memsqlite.RerankCandidate, graphDiagnostics *memsqlite.GraphActivationDiagnostics) ([]memsqlite.RerankResultItem, *memsqlite.RerankDiagnostics, error) {
	diagnostics := &memsqlite.RerankDiagnostics{
		Status:             "disabled_by_config",
		InputCount:         len(candidates),
		SafeCandidateCount: len(candidates),
	}
	if !prepared.Policy.UseMirror {
		return nil, diagnostics, nil
	}
	if len(candidates) == 0 {
		diagnostics.Status = "no_candidates"
		return nil, diagnostics, nil
	}
	diagnostics.Status = "adapter_missing"
	rerankAdapter, ok := s.mirrorAdapter.(MirrorRerankAdapter)
	if !ok || rerankAdapter == nil {
		return nil, diagnostics, nil
	}
	if shouldSkipSelectiveRerank(prepared.Query, candidates, graphDiagnostics) {
		diagnostics.Status = "skipped"
		diagnostics.SkippedReason = "direct_raw_exact_margin_high"
		s.recordSidecarStage(personaID, sidecarStageRerank, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	candidates = capSelectiveRerankCandidates(candidates)
	diagnostics.InputCount = len(candidates)
	if s.sidecarBreaker != nil && !s.sidecarBreaker.allow(personaID, sidecarStageRerank) {
		diagnostics.Status = "breaker_open"
		diagnostics.Degraded = true
		diagnostics.FallbackReason = "sidecar_breaker_open"
		return nil, diagnostics, nil
	}
	started := time.Now()
	stageCtx, cancel, ok := sidecarStageContext(ctx, sidecarCtx, sidecarStageTimeout(s.sidecarResilience, sidecarStageRerank))
	if !ok {
		diagnostics.Status = sidecarStatusSkippedByBudget
		diagnostics.Degraded = true
		diagnostics.FallbackReason = "sidecar_timeout"
		return nil, diagnostics, nil
	}
	defer cancel()
	result, err := rerankAdapter.Rerank(stageCtx, MirrorRerankRequest{
		PersonaID:  personaID,
		QueryText:  safeRerankQueryText(prepared.Query),
		Candidates: rerankCandidatesFromStore(candidates),
	})
	diagnostics.LatencyMs = time.Since(started).Milliseconds()
	if err != nil || result == nil {
		status, topErr := classifySidecarStageError(ctx, stageCtx, err)
		if topErr != nil {
			return nil, diagnostics, topErr
		}
		diagnostics.Status = status
		diagnostics.Degraded = true
		diagnostics.FallbackReason = sanitizeSidecarFallbackReason(status)
		s.recordSidecarStage(personaID, sidecarStageRerank, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	diagnostics.Degraded = result.Degraded
	diagnostics.FallbackReason = sanitizeRetrievalSidecarFallbackReason(result.FallbackReason)
	if result.Degraded {
		diagnostics.Status = "sidecar_degraded"
		s.recordSidecarStage(personaID, sidecarStageRerank, diagnostics.Status, diagnostics.FallbackReason)
		return nil, diagnostics, nil
	}
	diagnostics.Status = "used"
	diagnostics.ResultCount = len(result.Items)
	s.recordSidecarStage(personaID, sidecarStageRerank, diagnostics.Status, diagnostics.FallbackReason)
	return rerankItemsToStore(result.Items), diagnostics, nil
}

func shouldSkipSelectiveRerank(query memsqlite.QueryAnalysis, candidates []memsqlite.RerankCandidate, graphDiagnostics *memsqlite.GraphActivationDiagnostics) bool {
	if !isDirectSelectiveRerankQuery(query) || queryRequiresLiveRerank(query) {
		return false
	}
	if graphDiagnostics != nil && (graphDiagnostics.MappedCandidateCount > 0 || graphDiagnostics.SidecarCandidateCount > 0) {
		return false
	}
	if len(candidates) == 0 {
		return false
	}
	if hasRerankCandidateActivationOrSemanticNoise(candidates) {
		return false
	}
	if query.Confidence > 0 && query.Confidence < 0.40 {
		return false
	}
	top := candidates[0]
	if top.CurrentScore < 0.45 || !hasRawExactOrLexicalRerankEvidence(top) {
		return false
	}
	if len(candidates) > 1 && top.CurrentScore-candidates[1].CurrentScore < 0.15 {
		return false
	}
	return true
}

func hasRerankCandidateActivationOrSemanticNoise(candidates []memsqlite.RerankCandidate) bool {
	for _, candidate := range candidates {
		if candidate.GraphEnergy > 0 {
			return true
		}
		for source := range candidate.SourceScores {
			if strings.HasPrefix(source, "semantic_") ||
				source == "graph_activation" ||
				source == "anchor_fusion" ||
				source == "narrative_insight" {
				return true
			}
		}
	}
	return false
}

func isDirectSelectiveRerankQuery(query memsqlite.QueryAnalysis) bool {
	return query.MemoryAbility == memsqlite.MemoryAbilityDirectFact ||
		hasStoreQuerySignal(query.Signals, memsqlite.QuerySignalPastEventDirectFact)
}

func queryRequiresLiveRerank(query memsqlite.QueryAnalysis) bool {
	switch query.MemoryAbility {
	case memsqlite.MemoryAbilityPremiseCheck, memsqlite.MemoryAbilityGotcha,
		memsqlite.MemoryAbilityProvenance, memsqlite.MemoryAbilityCausalExplain,
		memsqlite.MemoryAbilityRelationshipArc:
		return true
	}
	for _, signal := range query.Signals {
		switch signal {
		case memsqlite.QuerySignalPremiseCounterexample, memsqlite.QuerySignalPremiseCheck,
			memsqlite.QuerySignalProvenanceSource, memsqlite.QuerySignalProvenance,
			memsqlite.QuerySignalCausalChain, memsqlite.QuerySignalCausal,
			memsqlite.QuerySignalRelationshipArc, memsqlite.QuerySignalReflectionSummary:
			return true
		}
	}
	switch query.EvidenceNeed {
	case memsqlite.EvidenceNeedPremiseCounterexample, memsqlite.EvidenceNeedProvenanceSource:
		return true
	}
	return false
}

func hasRawExactOrLexicalRerankEvidence(candidate memsqlite.RerankCandidate) bool {
	if candidate.SourceScores == nil {
		return false
	}
	if candidate.SourceScores["lexical_coverage"] >= 0.75 ||
		candidate.SourceScores["raw_exact"] > 0 ||
		candidate.SourceScores["sqlite_fts"] > 0 ||
		candidate.SourceScores["sqlite_sparse"] > 0 {
		return true
	}
	if candidate.SourceScores["raw_query"] >= 0.85 || candidate.SourceScores["raw_dense"] >= 0.95 {
		return true
	}
	return false
}

func capSelectiveRerankCandidates(candidates []memsqlite.RerankCandidate) []memsqlite.RerankCandidate {
	if len(candidates) <= selectiveRerankTopN {
		return candidates
	}
	return candidates[:selectiveRerankTopN]
}

func safeRerankQueryText(query memsqlite.QueryAnalysis) string {
	parts := make([]string, 0, 6)
	if normalized := boundedRerankQueryText(query.Normalized); normalized != "" {
		parts = append(parts, "query="+normalized)
	}
	parts = append(parts,
		"memory_domain="+string(query.MemoryDomain),
		"memory_ability="+string(query.MemoryAbility),
		"evidence_need="+string(query.EvidenceNeed),
		"time_mode="+string(query.TimeMode),
	)
	if len(query.Signals) > 0 {
		signals := make([]string, 0, len(query.Signals))
		for _, signal := range query.Signals {
			signals = append(signals, string(signal))
		}
		parts = append(parts, "signals="+strings.Join(signals, ","))
	}
	return strings.Join(parts, " ")
}

func isForgetDeleteMirrorCandidate(analysis memsqlite.QueryAnalysis) bool {
	return hasStoreQuerySignal(analysis.Signals, memsqlite.QuerySignalForgetDelete)
}

func forgetDeleteMirrorDropReason(candidate MirrorCandidate) string {
	if strings.TrimSpace(candidate.PrimaryPurpose) == "operation_target" {
		return "operation_target_not_context"
	}
	return "forget_delete_not_context"
}

func isRawMirrorCandidateSource(source string) bool {
	switch strings.TrimSpace(source) {
	case "", "raw_dense", "raw_query":
		return true
	default:
		return false
	}
}

func mergeRetrievalMirrorCandidates(raw []memsqlite.RetrievalMirrorCandidate, semantic []memsqlite.RetrievalMirrorCandidate) []memsqlite.RetrievalMirrorCandidate {
	if len(raw) == 0 {
		return append([]memsqlite.RetrievalMirrorCandidate(nil), semantic...)
	}
	if len(semantic) == 0 {
		return append([]memsqlite.RetrievalMirrorCandidate(nil), raw...)
	}
	result := make([]memsqlite.RetrievalMirrorCandidate, 0, len(raw)+len(semantic))
	byFact := map[string]int{}
	add := func(candidate memsqlite.RetrievalMirrorCandidate) {
		if strings.TrimSpace(candidate.FactID) == "" {
			return
		}
		if idx, ok := byFact[candidate.FactID]; ok {
			existing := result[idx]
			result[idx] = mergeRetrievalMirrorCandidate(existing, candidate)
			return
		}
		byFact[candidate.FactID] = len(result)
		result = append(result, candidate)
	}
	for _, candidate := range raw {
		add(candidate)
	}
	for _, candidate := range semantic {
		add(candidate)
	}
	return result
}

func mergeRetrievalMirrorCandidate(existing memsqlite.RetrievalMirrorCandidate, candidate memsqlite.RetrievalMirrorCandidate) memsqlite.RetrievalMirrorCandidate {
	mergedBreakdown := mergeMirrorSourceBreakdown(existing.SourceBreakdown, candidate.SourceBreakdown)
	if len(mergedBreakdown) == 0 {
		mergedBreakdown = mergeMirrorSourceBreakdown(
			mirrorCandidatePrimarySourceBreakdown(existing),
			mirrorCandidatePrimarySourceBreakdown(candidate),
		)
	}
	keep := existing
	if isRawMirrorCandidateSource(existing.Source) && !isRawMirrorCandidateSource(candidate.Source) {
		keep = existing
	} else if !isRawMirrorCandidateSource(existing.Source) && isRawMirrorCandidateSource(candidate.Source) {
		keep = candidate
	} else if candidate.Rank > 0 && (existing.Rank <= 0 || candidate.Rank < existing.Rank || (candidate.Rank == existing.Rank && candidate.Score > existing.Score)) {
		keep = candidate
	}
	keep.SourceBreakdown = mergedBreakdown
	keep.HitCount = existing.HitCount + candidate.HitCount
	return keep
}

func mirrorCandidatePrimarySourceBreakdown(candidate memsqlite.RetrievalMirrorCandidate) []memsqlite.MirrorCandidateSourceBreakdown {
	source := strings.TrimSpace(candidate.Source)
	if source == "" {
		return nil
	}
	return []memsqlite.MirrorCandidateSourceBreakdown{{
		Source:  source,
		Purpose: candidate.PrimaryPurpose,
		Rank:    candidate.Rank,
		Score:   candidate.Score,
		Weight:  1,
	}}
}

func mergeMirrorSourceBreakdown(left []memsqlite.MirrorCandidateSourceBreakdown, right []memsqlite.MirrorCandidateSourceBreakdown) []memsqlite.MirrorCandidateSourceBreakdown {
	bySource := map[string]memsqlite.MirrorCandidateSourceBreakdown{}
	add := func(item memsqlite.MirrorCandidateSourceBreakdown) {
		source := strings.TrimSpace(item.Source)
		if source == "" {
			return
		}
		item.Source = source
		existing, ok := bySource[source]
		if !ok || item.Rank > 0 && (existing.Rank <= 0 || item.Rank < existing.Rank || (item.Rank == existing.Rank && item.Score > existing.Score)) || item.Score > existing.Score {
			bySource[source] = item
		}
	}
	for _, item := range left {
		add(item)
	}
	for _, item := range right {
		add(item)
	}
	result := make([]memsqlite.MirrorCandidateSourceBreakdown, 0, len(bySource))
	for _, item := range bySource {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		leftRaw := isRawMirrorCandidateSource(result[i].Source)
		rightRaw := isRawMirrorCandidateSource(result[j].Source)
		if leftRaw != rightRaw {
			return leftRaw
		}
		if result[i].Rank == result[j].Rank {
			return result[i].Source < result[j].Source
		}
		return result[i].Rank < result[j].Rank
	})
	return result
}

func mergeMirrorDiagnostics(raw *memsqlite.MirrorDiagnostics, semantic *memsqlite.MirrorDiagnostics) *memsqlite.MirrorDiagnostics {
	if raw == nil {
		return semantic
	}
	if semantic == nil {
		return raw
	}
	out := *raw
	out.LatencyMs += semantic.LatencyMs
	out.SidecarCandidateCount += semantic.SidecarCandidateCount
	out.MappedCandidateCount += semantic.MappedCandidateCount
	out.DroppedCandidateCount += semantic.DroppedCandidateCount
	out.EmbeddingCacheHits += semantic.EmbeddingCacheHits
	out.EmbeddingCacheMisses += semantic.EmbeddingCacheMisses
	out.EmbeddingLiveCallCount += semantic.EmbeddingLiveCallCount
	semanticQueryCount := semantic.QueryCount - semantic.RawQueryCount
	if semanticQueryCount < 0 {
		semanticQueryCount = 0
	}
	out.QueryCount += semanticQueryCount
	out.RewriteQueryCount += semantic.RewriteQueryCount
	out.AnchorQueryCount += semantic.AnchorQueryCount
	out.MergedCandidateCount += semantic.MergedCandidateCount
	out.QueryTrimCount += semantic.QueryTrimCount
	if semantic.DenseEmbeddingWallLatencyMs > out.DenseEmbeddingWallLatencyMs {
		out.DenseEmbeddingWallLatencyMs = semantic.DenseEmbeddingWallLatencyMs
	}
	if semantic.DenseEmbeddingBatchLatencyMs > out.DenseEmbeddingBatchLatencyMs {
		out.DenseEmbeddingBatchLatencyMs = semantic.DenseEmbeddingBatchLatencyMs
	}
	out.DenseSearchTotalLatencyMs += semantic.DenseSearchTotalLatencyMs
	out.QueryCountTrimmedByBudget += semantic.QueryCountTrimmedByBudget
	out.PerQuery = append([]memsqlite.MirrorCandidatePerQueryDiagnostic(nil), raw.PerQuery...)
	for _, item := range semantic.PerQuery {
		if isRawMirrorCandidateSource(item.Source) {
			continue
		}
		out.PerQuery = append(out.PerQuery, item)
	}
	out.Candidates = append(append([]memsqlite.MirrorCandidateDiagnostic(nil), raw.Candidates...), semantic.Candidates...)
	if raw.Status != "used" && semantic.Status != "" {
		out.Status = semantic.Status
	}
	if semantic.Degraded {
		out.Degraded = true
		if out.FallbackReason == "" {
			out.FallbackReason = semantic.FallbackReason
		}
	}
	return &out
}

func sanitizeRetrievalSidecarFallbackReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "provider_budget_exhausted", "sidecar_provider_timeout":
		return strings.TrimSpace(reason)
	default:
		return sanitizeSidecarFallbackReason(reason)
	}
}

func mirrorPerQueryDiagnosticsToStore(values []MirrorCandidatePerQueryDiagnostic) []memsqlite.MirrorCandidatePerQueryDiagnostic {
	result := make([]memsqlite.MirrorCandidatePerQueryDiagnostic, 0, len(values))
	for _, value := range values {
		result = append(result, memsqlite.MirrorCandidatePerQueryDiagnostic{
			Source:    value.Source,
			Purpose:   value.Purpose,
			Count:     value.Count,
			LatencyMs: value.LatencyMs,
		})
	}
	return result
}

func boundedRerankQueryText(value string) string {
	var builder strings.Builder
	previousSpace := true
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			if !previousSpace {
				builder.WriteByte(' ')
				previousSpace = true
			}
			continue
		}
		builder.WriteRune(r)
		previousSpace = false
	}
	sanitized := strings.TrimSpace(builder.String())
	if sanitized == "" {
		return ""
	}
	runes := []rune(sanitized)
	if len(runes) > maxRerankQueryTextRune {
		return string(runes[:maxRerankQueryTextRune])
	}
	return sanitized
}

func (s *service) defaultMirrorActivationParams() MirrorActivationParams {
	budget := s.sidecarResilience.ActivationBudget
	return MirrorActivationParams{
		MaxHops:                   2,
		HopDecay:                  0.70,
		MinEnergy:                 0.01,
		MaxActiveNodes:            80,
		HubSuppressionPower:       0.50,
		IncludePaths:              true,
		MaxEdgesScannedPerRequest: budget.MaxEdgesScannedPerRequest,
		MaxNeighborsPerNode:       budget.MaxNeighborsPerNode,
		MaxActivationWallMs:       float64(budget.MaxActivationWall / time.Millisecond),
	}
}

func mirrorActivationSeedsFromStore(seeds []memsqlite.ActivationSeed) []MirrorActivationSeed {
	result := make([]MirrorActivationSeed, 0, len(seeds))
	for _, seed := range seeds {
		result = append(result, MirrorActivationSeed{
			TriviumNodeID: seed.TriviumNodeID,
			SQLiteNodeID:  seed.NodeID,
			NodeType:      string(seed.NodeType),
			SeedEnergy:    seed.SeedEnergy,
		})
	}
	return result
}

func activationCandidatesToStore(candidates []MirrorActivationCandidate) []memsqlite.ActivationCandidate {
	result := make([]memsqlite.ActivationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		source := candidate.Source
		if strings.TrimSpace(source) == "" {
			source = "graph_activation"
		}
		result = append(result, memsqlite.ActivationCandidate{
			TriviumNodeID: candidate.TriviumNodeID,
			Score:         candidate.Score,
			Source:        source,
			Rank:          candidate.Rank,
			Paths:         graphActivationPathsToStorePublic(candidate.Paths),
		})
	}
	return result
}

func rerankCandidatesFromStore(candidates []memsqlite.RerankCandidate) []MirrorRerankCandidate {
	result := make([]MirrorRerankCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, MirrorRerankCandidate{
			NodeID:       candidate.NodeID,
			NodeType:     candidate.NodeType,
			SafeSummary:  candidate.SafeSummary,
			CurrentScore: candidate.CurrentScore,
			AnchorEnergy: candidate.AnchorEnergy,
			GraphEnergy:  candidate.GraphEnergy,
			SourceScores: cloneFloatMap(candidate.SourceScores),
		})
	}
	return result
}

func cloneFloatMap(values map[string]float64) map[string]float64 {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]float64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func rerankItemsToStore(items []MirrorRerankItem) []memsqlite.RerankResultItem {
	result := make([]memsqlite.RerankResultItem, 0, len(items))
	for _, item := range items {
		result = append(result, memsqlite.RerankResultItem{
			NodeID:      item.NodeID,
			NodeType:    item.NodeType,
			RerankScore: item.RerankScore,
			DebugReason: item.DebugReason,
		})
	}
	return result
}

func graphActivationPathsToStorePublic(paths []MirrorActivationPath) []memsqlite.GraphActivationPath {
	result := make([]memsqlite.GraphActivationPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, memsqlite.GraphActivationPath{
			TriviumNodeIDs: append([]int64(nil), path.TriviumNodeIDs...),
			LinkTypes:      append([]string(nil), path.LinkTypes...),
		})
	}
	return result
}

func graphActivationPathsToStore(paths []memsqlite.GraphActivationPath) []memsqlite.GraphActivationPath {
	result := make([]memsqlite.GraphActivationPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, memsqlite.GraphActivationPath{
			TriviumNodeIDs: append([]int64(nil), path.TriviumNodeIDs...),
			LinkTypes:      append([]string(nil), path.LinkTypes...),
		})
	}
	return result
}

func (s *service) RebuildSearchDocuments(ctx context.Context, req RebuildSearchDocumentsRequest) (*RebuildSearchDocumentsResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	result, err := s.search.RebuildSearchDocuments(ctx, personaID)
	if err != nil {
		return nil, err
	}
	return &RebuildSearchDocumentsResult{Upserted: result.Upserted}, nil
}
