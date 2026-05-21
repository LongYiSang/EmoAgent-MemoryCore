package sqlite

import (
	"math"
	"sort"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	RetrievalCorrectiveActionSemanticLight           = "semantic_light"
	RetrievalCorrectiveActionSQLiteFallback          = "sqlite_fallback"
	RetrievalCorrectiveActionSuppressMemoryInjection = "suppress_memory_injection"
	RetrievalHardFailureForbiddenCandidate           = "forbidden_candidate"
	RetrievalHardFailureTemporalInconsistency        = "temporal_inconsistency"
	retrievalConfidenceMinimumAuthorityPassRatio     = 0.70
	retrievalConfidenceMinimumAnchorCoverage         = 0.40
	retrievalConfidenceMinimumRequiredChainCoverage  = 0.50
	retrievalConfidenceDiversityNormalizationSources = 3
)

type RetrievalConfidence struct {
	CandidateRecallProxy  float64 `json:"candidate_recall_proxy"`
	SourceDiversity       float64 `json:"source_diversity"`
	AnchorCoverage        float64 `json:"anchor_coverage"`
	TopRankMargin         float64 `json:"top_rank_margin"`
	AuthorityPassRatio    float64 `json:"authority_pass_ratio"`
	TemporalConsistency   float64 `json:"temporal_consistency"`
	RequiredChainCoverage float64 `json:"required_chain_coverage"`
	MMRDiversity          float64 `json:"mmr_diversity"`
	SensitivitySafety     float64 `json:"sensitivity_safety"`
	Overall               float64 `json:"overall"`
	CorrectiveAction      string  `json:"corrective_action,omitempty"`
	HardFailureReason     string  `json:"hard_failure_reason,omitempty"`
}

func evaluateRetrievalConfidence(finalCandidates PreparedFinalCandidates, scored []scoredFact, selected []scoredFact, blocks []MemoryBlock, suppressions []MemorySuppression) RetrievalConfidence {
	confidence := RetrievalConfidence{
		CandidateRecallProxy:  candidateRecallProxy(finalCandidates.Policy, scored, selected),
		SourceDiversity:       sourceDiversity(selected),
		AnchorCoverage:        anchorCoverage(selected),
		TopRankMargin:         topRankMargin(selected),
		AuthorityPassRatio:    authorityPassRatio(finalCandidates.Request, scored),
		TemporalConsistency:   temporalConsistency(finalCandidates.Query, selected, blocks),
		RequiredChainCoverage: requiredChainCoverage(finalCandidates.Query, selected, blocks),
		MMRDiversity:          mmrDiversity(selected, suppressions),
		SensitivitySafety:     sensitivitySafety(finalCandidates.Policy, selected),
	}
	confidence.Overall = clamp01(
		0.18*confidence.AnchorCoverage +
			0.15*confidence.SourceDiversity +
			0.14*confidence.TopRankMargin +
			0.14*confidence.AuthorityPassRatio +
			0.12*confidence.TemporalConsistency +
			0.12*confidence.RequiredChainCoverage +
			0.08*confidence.MMRDiversity +
			0.07*confidence.SensitivitySafety,
	)
	return decideRetrievalCorrectiveAction(finalCandidates, confidence)
}

func decideRetrievalCorrectiveAction(finalCandidates PreparedFinalCandidates, confidence RetrievalConfidence) RetrievalConfidence {
	switch {
	case confidence.SensitivitySafety <= 0:
		confidence.CorrectiveAction = RetrievalCorrectiveActionSuppressMemoryInjection
		confidence.HardFailureReason = RetrievalHardFailureForbiddenCandidate
	case confidence.TemporalConsistency <= 0:
		confidence.CorrectiveAction = RetrievalCorrectiveActionSuppressMemoryInjection
		confidence.HardFailureReason = RetrievalHardFailureTemporalInconsistency
	case confidence.AuthorityPassRatio < retrievalConfidenceMinimumAuthorityPassRatio && authorityDroppedCandidateCount(finalCandidates.Request) > 0:
		confidence.CorrectiveAction = RetrievalCorrectiveActionSQLiteFallback
	case confidence.AnchorCoverage < retrievalConfidenceMinimumAnchorCoverage && queryAllowsSemanticCorrective(finalCandidates.Query):
		confidence.CorrectiveAction = RetrievalCorrectiveActionSemanticLight
	case confidence.RequiredChainCoverage < retrievalConfidenceMinimumRequiredChainCoverage && queryRequiresChainCoverage(finalCandidates.Query):
		confidence.CorrectiveAction = RetrievalCorrectiveActionSemanticLight
	}
	return confidence
}

func candidateRecallProxy(policy RetrievalPolicy, scored []scoredFact, selected []scoredFact) float64 {
	if len(scored) == 0 || len(selected) == 0 {
		return 0
	}
	limit := policy.FinalMemoryCount
	if limit <= 0 {
		limit = 8
	}
	desired := minInt(limit, len(scored))
	if desired <= 0 {
		return 0
	}
	return clamp01(float64(len(selected)) / float64(desired))
}

func authorityPassRatio(req RetrievalRequest, scored []scoredFact) float64 {
	passed := len(scored)
	dropped := authorityDroppedCandidateCount(req)
	total := passed + dropped
	if total == 0 {
		return 0
	}
	return clamp01(float64(passed) / float64(total))
}

func authorityDroppedCandidateCount(req RetrievalRequest) int {
	count := 0
	if req.MirrorDiagnostics != nil {
		for _, candidate := range req.MirrorDiagnostics.Candidates {
			if isAuthorityDropReason(candidate.DropReason) {
				count++
			}
		}
	}
	if req.GraphActivationDiagnostics != nil {
		for _, candidate := range req.GraphActivationDiagnostics.Candidates {
			if isAuthorityDropReason(candidate.DropReason) {
				count++
			}
		}
	}
	return count
}

func isAuthorityDropReason(reason string) bool {
	reason = strings.TrimSpace(reason)
	return reason == "dropped_by_authority_filter" || reason == RetrievalHardFailureForbiddenCandidate
}

func anchorCoverage(selected []scoredFact) float64 {
	if len(selected) == 0 {
		return 0
	}
	covered := 0
	for _, candidate := range selected {
		if candidateHasRetrievalAnchor(candidate) {
			covered++
		}
	}
	return clamp01(float64(covered) / float64(len(selected)))
}

func candidateHasRetrievalAnchor(candidate scoredFact) bool {
	return candidate.Breakdown.AnchorEnergy > 0 ||
		candidate.Breakdown.GraphEnergy > 0 ||
		candidate.Breakdown.CompletionSource != "" ||
		candidate.Breakdown.CompletionBonus > 0 ||
		candidate.Breakdown.LexicalCoverage > 0 ||
		candidate.Breakdown.SlotCoverage > 0 ||
		len(candidate.SourceBreakdown) > 0
}

func topRankMargin(selected []scoredFact) float64 {
	if len(selected) == 0 {
		return 0
	}
	scores := make([]float64, 0, len(selected))
	for _, candidate := range selected {
		if candidate.Score > 0 && !math.IsNaN(candidate.Score) && !math.IsInf(candidate.Score, 0) {
			scores = append(scores, candidate.Score)
		}
	}
	if len(scores) == 0 {
		return 0
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i] > scores[j] })
	if len(scores) == 1 {
		return 1
	}
	if scores[0] <= 0 {
		return 0
	}
	return clamp01((scores[0] - scores[1]) / scores[0])
}

func sourceDiversity(selected []scoredFact) float64 {
	if len(selected) == 0 {
		return 0
	}
	sources := map[string]struct{}{}
	for _, candidate := range selected {
		for _, item := range candidate.SourceBreakdown {
			if strings.TrimSpace(item.Source) != "" {
				sources[item.Source] = struct{}{}
			}
		}
		if candidate.Breakdown.GraphEnergy > 0 {
			sources["graph_activation"] = struct{}{}
		}
		if candidate.Breakdown.CompletionSource != "" {
			sources["completion:"+candidate.Breakdown.CompletionSource] = struct{}{}
		}
		if candidate.Breakdown.LexicalCoverage > 0 || candidate.Breakdown.SlotCoverage > 0 {
			sources["lexical"] = struct{}{}
		}
	}
	return clamp01(float64(len(sources)) / retrievalConfidenceDiversityNormalizationSources)
}

func temporalConsistency(query QueryAnalysis, selected []scoredFact, blocks []MemoryBlock) float64 {
	if query.TimeMode != "" && query.TimeMode != QueryTimeModeCurrent {
		return 1
	}
	if !queryHasStrictCurrentIntent(query) {
		return 1
	}
	for _, candidate := range selected {
		if factIsHistorical(candidate.Fact) {
			return 0
		}
	}
	for _, block := range blocks {
		for _, item := range block.Items {
			if item.HistoricalStatus != "" && item.HistoricalStatus != MemoryHistoricalStatusCurrent {
				return 0
			}
		}
	}
	return 1
}

func queryHasStrictCurrentIntent(query QueryAnalysis) bool {
	normalized := strings.TrimSpace(query.Normalized)
	if normalized == "" {
		normalized = strings.ToLower(strings.TrimSpace(query.Raw))
	}
	if query.EvidenceNeed == EvidenceNeedStateTransition ||
		query.MemoryAbility == MemoryAbilityHistorical ||
		query.MemoryAbility == MemoryAbilityRelationshipArc ||
		hasQuerySignal(query, QuerySignalStateTransition) {
		return false
	}
	return containsAny(normalized, "现在", "当前", "最新", "此刻", "current", "right now", "now")
}

func factIsHistorical(fact core.Fact) bool {
	return fact.ValidityStatus == core.ValidityInvalidated ||
		fact.LifecycleStatus == core.LifecycleArchived ||
		fact.LifecycleStatus == core.LifecycleDeepArchived
}

func requiredChainCoverage(query QueryAnalysis, selected []scoredFact, blocks []MemoryBlock) float64 {
	if !queryRequiresChainCoverage(query) {
		return 1
	}
	if len(selected) == 0 {
		return 0
	}
	covered := 0
	for _, candidate := range selected {
		if candidateSatisfiesRequiredChain(query, candidate) {
			covered++
		}
	}
	covered += blockChainCoverageCount(query, blocks)
	return clamp01(float64(covered) / float64(len(selected)))
}

func queryRequiresChainCoverage(query QueryAnalysis) bool {
	return query.MemoryAbility == MemoryAbilityCausalExplain ||
		query.MemoryAbility == MemoryAbilityProvenance ||
		query.MemoryAbility == MemoryAbilityPremiseCheck ||
		query.MemoryAbility == MemoryAbilityRelationshipArc ||
		query.EvidenceNeed == EvidenceNeedStateTransition ||
		query.EvidenceNeed == EvidenceNeedProvenanceSource ||
		query.EvidenceNeed == EvidenceNeedPremiseCounterexample ||
		query.EvidenceNeed == EvidenceNeedRelationshipTimeline ||
		hasQuerySignal(query, QuerySignalCausalChain) ||
		hasQuerySignal(query, QuerySignalProvenanceSource) ||
		hasQuerySignal(query, QuerySignalPremiseCounterexample)
}

func candidateSatisfiesRequiredChain(query QueryAnalysis, candidate scoredFact) bool {
	switch query.MemoryAbility {
	case MemoryAbilityCausalExplain:
		return candidate.Breakdown.CompletionSource == completionSourceCausal
	case MemoryAbilityProvenance:
		return candidate.Breakdown.CompletionSource == completionSourceProvenance
	case MemoryAbilityPremiseCheck:
		return candidate.Breakdown.CompletionSource == completionSourcePremiseCheck
	case MemoryAbilityRelationshipArc:
		return candidate.Breakdown.CompletionSource == completionSourceNarrative
	default:
		return candidate.Breakdown.CompletionSource != ""
	}
}

func blockChainCoverageCount(query QueryAnalysis, blocks []MemoryBlock) int {
	count := 0
	for _, block := range blocks {
		for _, item := range block.Items {
			if len(item.RelatedFacts) > 0 && (query.MemoryAbility == MemoryAbilityCausalExplain || query.EvidenceNeed == EvidenceNeedStateTransition) {
				count++
				continue
			}
			if len(item.SourceRefs) > 0 && (query.MemoryAbility == MemoryAbilityProvenance || query.EvidenceNeed == EvidenceNeedProvenanceSource) {
				count++
			}
		}
	}
	return count
}

func mmrDiversity(selected []scoredFact, suppressions []MemorySuppression) float64 {
	if len(selected) == 0 {
		return 0
	}
	duplicates := 0
	for _, suppression := range suppressions {
		if suppression.Reason == MemorySuppressionReasonMMRDuplicate {
			duplicates++
		}
	}
	return clamp01(1 - float64(duplicates)/float64(len(selected)+duplicates))
}

func sensitivitySafety(policy RetrievalPolicy, selected []scoredFact) float64 {
	for _, candidate := range selected {
		if selectedFactForbidden(candidate.Fact, policy) {
			return 0
		}
	}
	return 1
}

func selectedFactForbidden(fact core.Fact, policy RetrievalPolicy) bool {
	if fact.VisibilityStatus != core.VisibilityVisible || !fact.Searchable {
		return true
	}
	return sensitivityRank(fact.SensitivityLevel) > sensitivityRank(core.SensitivityLevel(policy.SensitivityPermission))
}

func queryHasLongTermMemoryIntent(query QueryAnalysis) bool {
	return strings.TrimSpace(query.Raw) != "" &&
		(query.MemoryAbility != "" || len(query.Signals) > 0 || len(query.EntityMentions) > 0)
}

func queryAllowsSemanticCorrective(query QueryAnalysis) bool {
	return queryHasLongTermMemoryIntent(query) &&
		!hasQuerySignal(query, QuerySignalForgetDelete) &&
		query.MemoryAbility != MemoryAbilityBoundary
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
