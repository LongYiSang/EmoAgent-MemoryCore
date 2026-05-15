package memorycore

import (
	"context"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func (s *service) ConsolidateCandidate(ctx context.Context, req ConsolidateCandidateRequest) (*ConsolidationResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	if err := s.ensurePersona(ctx, personaID); err != nil {
		return nil, err
	}

	result, err := s.facts.ConsolidateCandidate(ctx, memsqlite.ConsolidateCandidateRequest{
		PersonaID: personaID,
		SessionID: req.SessionID,
		Trigger:   defaultString(req.Trigger, ConsolidationTriggerManual),
		Candidate: memsqlite.ManualFactCandidate{
			SubjectEntityID:  req.Candidate.SubjectEntityID,
			Predicate:        req.Candidate.Predicate,
			ObjectEntityID:   req.Candidate.ObjectEntityID,
			ObjectLiteral:    req.Candidate.ObjectLiteral,
			ContentSummary:   req.Candidate.ContentSummary,
			FactType:         req.Candidate.FactType,
			ValidFrom:        req.Candidate.ValidFrom,
			ValidTo:          req.Candidate.ValidTo,
			Confidence:       req.Candidate.Confidence,
			ConfidenceScore:  req.Candidate.ConfidenceScore,
			Importance:       req.Candidate.Importance,
			Valence:          req.Candidate.Valence,
			Arousal:          req.Candidate.Arousal,
			Sensitivity:      req.Candidate.Sensitivity,
			SourceEpisodeIDs: req.Candidate.SourceEpisodeIDs,
			Pinned:           req.Candidate.Pinned,
			UserRequested:    req.Candidate.UserRequested,
		},
		Policy: memsqlite.ConsolidationPolicy{
			Action:                      req.Policy.Action,
			Approved:                    req.Policy.Approved,
			AllowManualPinWithoutSource: req.Policy.AllowManualPinWithoutSource,
		},
	})
	if err != nil {
		return nil, err
	}
	return consolidationResultFromCore(result), nil
}
