package memorycore

import (
	"context"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const (
	curationCandidateSourceMirrorFirst = "mirror_first"
	curationCandidateSourceSQLiteOnly  = "sqlite_only"
	curationCandidateSourceMirrorOnly  = "mirror_only"
)

func curationCandidateRetrievalOptions(defaults CurationCandidateRetrievalOptions, override *CurationCandidateRetrievalOptions) (CurationCandidateRetrievalOptions, error) {
	opts := defaults
	if override != nil {
		if strings.TrimSpace(override.Mode) != "" {
			opts.Mode = strings.TrimSpace(override.Mode)
		}
		if override.MirrorMinSimilarity != 0 {
			opts.MirrorMinSimilarity = override.MirrorMinSimilarity
		}
	}
	opts = normalizeCurationCandidateRetrievalOptions(opts)
	switch opts.Mode {
	case curationCandidateSourceMirrorFirst, curationCandidateSourceSQLiteOnly, curationCandidateSourceMirrorOnly:
	default:
		return CurationCandidateRetrievalOptions{}, fmt.Errorf("%w: candidate_retrieval.mode must be mirror_first, sqlite_only, or mirror_only", ErrInvalidRequest)
	}
	if opts.MirrorMinSimilarity <= 0 || opts.MirrorMinSimilarity > 1 {
		return CurationCandidateRetrievalOptions{}, fmt.Errorf("%w: candidate_retrieval.mirror_min_similarity must be within (0, 1]", ErrInvalidRequest)
	}
	return opts, nil
}

func (s *service) retrieveCurationComparableCandidates(ctx context.Context, personaID string, delta core.Fact, limit int, opts CurationCandidateRetrievalOptions) ([]memsqlite.CurationComparableCandidate, curationRawLogCandidateDelta, error) {
	item := curationRawLogCandidateDelta{
		DeltaFactID: delta.ID,
		Strategy:    opts.Mode,
	}
	if opts.Mode == curationCandidateSourceSQLiteOnly {
		candidates, ids, err := s.retrieveSQLCurationComparableCandidates(ctx, personaID, delta.ID, limit)
		item.SQLCandidateFactIDs = ids
		item.AuthorityCandidateFactIDs = curationComparableCandidateIDs(candidates)
		return candidates, item, err
	}

	mirrorCandidates, mirrorFailed, fallbackReason, err := s.retrieveMirrorCurationComparableCandidates(ctx, personaID, delta, limit, &item)
	if err != nil {
		return nil, item, err
	}
	if !mirrorFailed {
		item.AuthorityCandidateFactIDs = curationComparableCandidateIDs(mirrorCandidates)
		return mirrorCandidates, item, nil
	}
	item.FallbackReason = fallbackReason
	if opts.Mode == curationCandidateSourceMirrorOnly {
		item.AuthorityCandidateFactIDs = nil
		return nil, item, nil
	}

	sqlCandidates, ids, err := s.retrieveSQLCurationComparableCandidates(ctx, personaID, delta.ID, limit)
	item.FallbackSQLUsed = true
	item.SQLCandidateFactIDs = ids
	item.AuthorityCandidateFactIDs = curationComparableCandidateIDs(sqlCandidates)
	return sqlCandidates, item, err
}

func (s *service) retrieveSQLCurationComparableCandidates(ctx context.Context, personaID string, deltaFactID string, limit int) ([]memsqlite.CurationComparableCandidate, []string, error) {
	facts, err := s.curation.RetrieveComparableFacts(ctx, memsqlite.CurationComparableQuery{
		PersonaID:             personaID,
		DeltaFactID:           deltaFactID,
		CandidateLimitPerFact: limit,
	})
	if err != nil {
		return nil, nil, err
	}
	candidates := make([]memsqlite.CurationComparableCandidate, 0, len(facts))
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		candidates = append(candidates, memsqlite.CurationComparableCandidate{
			Fact:   fact,
			Source: memsqlite.CurationComparableSourceSQL,
		})
		ids = append(ids, fact.ID)
	}
	return candidates, ids, nil
}

func (s *service) retrieveMirrorCurationComparableCandidates(ctx context.Context, personaID string, delta core.Fact, limit int, item *curationRawLogCandidateDelta) ([]memsqlite.CurationComparableCandidate, bool, string, error) {
	if delta.SubjectEntityID == nil || strings.TrimSpace(*delta.SubjectEntityID) == "" {
		item.MirrorStatus = "delta_subject_missing"
		return nil, true, "delta_subject_missing", nil
	}
	ready, err := s.mirrorState.IsReady(ctx, personaID)
	if err != nil {
		return nil, false, "", err
	}
	if !ready {
		item.MirrorStatus = "persona_not_ready"
		return nil, true, "persona_not_ready", nil
	}
	adapter, ok := s.mirrorAdapter.(MirrorDedupSearchAdapter)
	if !ok || adapter == nil {
		item.MirrorStatus = "adapter_missing"
		return nil, true, "adapter_missing", nil
	}
	if s.sidecarBreaker != nil && !s.sidecarBreaker.allow(personaID, sidecarStageMirror) {
		item.MirrorStatus = "breaker_open"
		item.MirrorDegraded = true
		return nil, true, "sidecar_breaker_open", nil
	}

	totalCtx, totalCancel := sidecarTotalContext(ctx, s.sidecarResilience.Timeouts.Total)
	defer totalCancel()
	stageCtx, stageCancel, stageOK := sidecarStageContext(ctx, totalCtx, sidecarStageTimeout(s.sidecarResilience, sidecarStageMirror))
	if !stageOK {
		item.MirrorStatus = sidecarStatusSkippedByBudget
		item.MirrorDegraded = true
		s.recordSidecarStage(personaID, sidecarStageMirror, item.MirrorStatus, "sidecar_timeout")
		return nil, true, "sidecar_timeout", nil
	}
	defer stageCancel()

	result, err := adapter.DedupSearch(stageCtx, MirrorDedupSearchRequest{
		RequestID: "curation:" + delta.ID,
		PersonaID: personaID,
		Candidate: MirrorDedupCandidate{
			CandidateID:     delta.ID,
			SafeSummary:     delta.ContentSummary,
			FactType:        string(delta.FactType),
			Predicate:       delta.Predicate,
			SubjectEntityID: derefCoreString(delta.SubjectEntityID),
			ObjectEntityID:  delta.ObjectEntityID,
			ObjectLiteral:   derefCoreString(delta.ObjectLiteral),
		},
		Policy: MirrorDedupSearchPolicy{
			Limit:             limit,
			SameSubjectBoost:  true,
			SameFactTypeBoost: true,
			ThresholdProfile:  "default_v0",
			Shadow:            true,
		},
	})
	if err != nil || result == nil {
		status, topErr := classifySidecarStageError(ctx, stageCtx, err)
		if topErr != nil {
			return nil, false, "", topErr
		}
		item.MirrorStatus = status
		item.MirrorDegraded = true
		fallback := sanitizeSidecarFallbackReason(status)
		s.recordSidecarStage(personaID, sidecarStageMirror, item.MirrorStatus, fallback)
		return nil, true, fallback, nil
	}
	item.MirrorStatus = strings.TrimSpace(result.Status)
	if item.MirrorStatus == "" {
		item.MirrorStatus = "ok"
	}
	item.MirrorDegraded = result.Degraded
	item.MirrorFallbackReason = sanitizeSidecarFallbackReason(result.FallbackReason)
	if result.Degraded {
		if item.MirrorFallbackReason == "" {
			item.MirrorFallbackReason = "sidecar_degraded"
		}
		s.recordSidecarStage(personaID, sidecarStageMirror, "sidecar_degraded", item.MirrorFallbackReason)
		return nil, true, item.MirrorFallbackReason, nil
	}

	candidates := make([]memsqlite.CurationComparableCandidate, 0, len(result.Candidates))
	seen := map[string]struct{}{}
	for _, match := range result.Candidates {
		raw := curationRawLogMirrorCandidate{
			NodeType:    match.NodeType,
			NodeID:      match.NodeID,
			Source:      "mirror_dedup",
			Similarity:  match.Similarity,
			MatchClass:  match.MatchClass,
			MatchReason: match.MatchReason,
			MergeHint:   match.MergeHint,
		}
		if match.NodeType != ForgetNodeFact {
			raw.AuthorityDropReason = "node_type_not_fact"
			item.MirrorCandidates = append(item.MirrorCandidates, raw)
			continue
		}
		fact, accepted, dropReason, err := s.curation.LoadComparableFactCandidate(ctx, personaID, delta, match.NodeID)
		if err != nil {
			return nil, false, "", err
		}
		raw.MappedFactID = fact.ID
		if !accepted {
			raw.AuthorityDropReason = dropReason
			item.MirrorCandidates = append(item.MirrorCandidates, raw)
			continue
		}
		if _, ok := seen[fact.ID]; !ok {
			candidates = append(candidates, memsqlite.CurationComparableCandidate{
				Fact:        fact,
				Source:      memsqlite.CurationComparableSourceMirror,
				Similarity:  match.Similarity,
				MatchClass:  match.MatchClass,
				MatchReason: match.MatchReason,
			})
			seen[fact.ID] = struct{}{}
		}
		item.MirrorCandidates = append(item.MirrorCandidates, raw)
	}
	s.recordSidecarStage(personaID, sidecarStageMirror, item.MirrorStatus, item.MirrorFallbackReason)
	return candidates, false, "", nil
}

func curationComparableCandidateIDs(candidates []memsqlite.CurationComparableCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Fact.ID)
	}
	return out
}

func derefCoreString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
