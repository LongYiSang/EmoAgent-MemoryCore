package sqlite

import (
	"sort"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func bestMMRCandidateIndex(candidates []scoredFact, selected []scoredFact) int {
	bestIndex := -1
	bestScore := 0.0
	for index, candidate := range candidates {
		score := mmrCandidateScore(candidate, selected)
		if bestIndex < 0 ||
			score > bestScore ||
			(score == bestScore && candidate.Score > candidates[bestIndex].Score) ||
			(score == bestScore && candidate.Score == candidates[bestIndex].Score && candidate.Fact.ID < candidates[bestIndex].Fact.ID) {
			bestIndex = index
			bestScore = score
		}
	}
	return bestIndex
}

func mmrCandidateScore(candidate scoredFact, selected []scoredFact) float64 {
	similarity := maxFactSimilarity(candidate, selected)
	return defaultMMRLambda*candidate.Score - (1-defaultMMRLambda)*similarity
}

func removeScoredFactAt(candidates []scoredFact, index int) []scoredFact {
	if index < 0 || index >= len(candidates) {
		return candidates
	}
	copy(candidates[index:], candidates[index+1:])
	return candidates[:len(candidates)-1]
}

func maxFactSimilarity(candidate scoredFact, selected []scoredFact) float64 {
	maxSimilarity := 0.0
	for _, item := range selected {
		similarity := factSimilarity(candidate, item)
		if similarity > maxSimilarity {
			maxSimilarity = similarity
		}
	}
	return maxSimilarity
}

func isMMRDuplicateCandidate(query QueryAnalysis, candidate scoredFact, selected []scoredFact) bool {
	if len(selected) == 0 {
		return false
	}
	if maxFactSimilarity(candidate, selected) > defaultDuplicateThreshold {
		return true
	}
	if !queryUsesPredicateObjectHardCap(query) {
		return false
	}
	candidateKey := predicateObjectKey(candidate.Fact)
	if candidateKey == "" {
		return false
	}
	for _, item := range selected {
		if predicateObjectKey(item.Fact) == candidateKey &&
			item.Fact.LifecycleStatus == candidate.Fact.LifecycleStatus {
			return true
		}
	}
	return false
}

func queryUsesPredicateObjectHardCap(query QueryAnalysis) bool {
	if queryWantsExplicitProvenanceMemory(query) ||
		query.EvidenceNeed == EvidenceNeedStateTransition ||
		query.EvidenceNeed == EvidenceNeedRelationshipTimeline ||
		hasQuerySignal(query, QuerySignalStateTransition) ||
		hasQuerySignal(query, QuerySignalRelationshipArc) {
		return false
	}
	return true
}

func predicateObjectKey(fact core.Fact) string {
	objectKey := canonicalObjectKey(fact.ObjectEntityID, fact.ObjectLiteral)
	if objectKey == "" || strings.TrimSpace(fact.Predicate) == "" {
		return ""
	}
	return strings.TrimSpace(fact.Predicate) + "\x1f" + objectKey
}

func factSimilarity(left scoredFact, right scoredFact) float64 {
	summarySimilarity := jaccard(summaryTerms(left.Fact.ContentSummary), summaryTerms(right.Fact.ContentSummary))
	weighted := 0.45*summarySimilarity +
		0.20*jaccard(linkedEntityIDs(left.Fact), linkedEntityIDs(right.Fact)) +
		0.15*predicateSimilarity(left.Fact.Predicate, right.Fact.Predicate) +
		0.10*jaccard(left.SourceEpisodeIDs, right.SourceEpisodeIDs)
	if weighted <= 0 {
		return 0
	}
	similarity := weighted / 0.90
	if summarySimilarity < 0.80 {
		return minFloat(similarity, 0.80)
	}
	if left.Fact.ValidityStatus != right.Fact.ValidityStatus || left.Fact.LifecycleStatus != right.Fact.LifecycleStatus {
		return minFloat(similarity, 0.80)
	}
	return similarity
}

func summaryTerms(summary string) []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
	}
	for _, term := range strings.Fields(summary) {
		add(term)
	}
	for _, term := range cjkBigrams(strings.ToLower(summary)) {
		add(term)
	}
	terms := make([]string, 0, len(seen))
	for term := range seen {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	return terms
}

func predicateSimilarity(left string, right string) float64 {
	if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
		return 0
	}
	if left == right {
		return 1
	}
	return 0
}

func jaccard(left []string, right []string) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	seen := map[string]struct{}{}
	for _, item := range left {
		item = strings.TrimSpace(item)
		if item != "" {
			seen[item] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return 0
	}
	intersection := 0
	union := len(seen)
	for _, item := range right {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			intersection++
			continue
		}
		union++
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func minFloat(left float64, right float64) float64 {
	if left < right {
		return left
	}
	return right
}
