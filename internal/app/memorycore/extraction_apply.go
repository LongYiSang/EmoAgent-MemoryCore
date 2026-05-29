package memorycore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func ApplyAcceptedFacts(ctx context.Context, svc Service, db *sql.DB, req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult) ExtractionApplyResult {
	return ApplyAcceptedFactsWithSemanticDedup(ctx, svc, db, req, resp, gate, nil)
}

func ApplyAcceptedFactsWithSemanticDedup(ctx context.Context, svc Service, db *sql.DB, req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult, dedup *DedupDiagnostics) ExtractionApplyResult {
	result := ExtractionApplyResult{
		RequestID: req.RequestID,
		PersonaID: req.PersonaID,
		Status:    "nothing_applied",
		Results:   []FactApplyResult{},
		Failures:  []FactApplyFailure{},
	}
	if svc == nil || db == nil {
		result.Failures = append(result.Failures, FactApplyFailure{CandidateID: "pipeline", Reason: "service_and_db_required"})
		return result
	}
	if gate.Status == "blocked" || len(gate.ResponseDecisions) > 0 {
		result.Failures = append(result.Failures, FactApplyFailure{CandidateID: "response", Reason: "response_gate_blocked"})
		result.Status = "failed"
		return result
	}
	candidateFacts := map[string]string{}
	pinnedTargets := pinTargets(gate, resp)
	for _, fact := range resp.Facts {
		d, ok := decisionByID(gate.FactDecisions, fact.CandidateID)
		if !ok || d.Decision != decisionAccept {
			continue
		}
		reqCandidate, err := factToConsolidationCandidate(ctx, svc, db, req, resp, fact, pinnedTargets)
		if err != nil {
			var reviewErr entityResolutionNeedsReviewError
			if errors.As(err, &reviewErr) {
				result.Results = append(result.Results, needsReviewApplyResult(fact.CandidateID, reviewErr.Reason))
				continue
			}
			result.Failures = append(result.Failures, FactApplyFailure{CandidateID: fact.CandidateID, Reason: err.Error()})
			continue
		}
		if dedupDecision, ok := semanticDedupApplyDecision(dedup, fact.CandidateID); ok {
			consolidated, err := semanticDedupDiscardResult(ctx, db, req.PersonaID, dedupDecision)
			if err != nil {
				result.Failures = append(result.Failures, FactApplyFailure{CandidateID: fact.CandidateID, Reason: err.Error()})
				continue
			}
			result.Results = append(result.Results, FactApplyResult{CandidateID: fact.CandidateID, Status: consolidated.Status, Result: consolidated})
			continue
		}
		consolidated, err := svc.ConsolidateCandidate(ctx, reqCandidate)
		if err != nil {
			result.Failures = append(result.Failures, FactApplyFailure{CandidateID: fact.CandidateID, Reason: err.Error()})
			continue
		}
		if consolidated.Status == ConsolidationStatusRejected || consolidated.Status == ConsolidationStatusNeedsReview || consolidated.Fact == nil {
			reason := consolidated.RejectedReason
			if reason == "" {
				reason = consolidated.NeedsReviewReason
			}
			if reason == "" {
				reason = "consolidation produced no fact"
			}
			result.Failures = append(result.Failures, FactApplyFailure{CandidateID: fact.CandidateID, Reason: reason})
			result.Results = append(result.Results, FactApplyResult{CandidateID: fact.CandidateID, Status: consolidated.Status, Result: consolidated})
			continue
		}
		if extractionLinkFactEligible(*consolidated.Fact) {
			candidateFacts[fact.CandidateID] = consolidated.Fact.ID
		}
		if consolidationStatusApplied(consolidated.Status) {
			result.AppliedCount++
		}
		result.Results = append(result.Results, FactApplyResult{CandidateID: fact.CandidateID, Status: consolidated.Status, Result: consolidated})
	}
	if err := applyAcceptedLinks(ctx, db, req, resp, gate, candidateFacts); err != nil {
		result.Failures = append(result.Failures, FactApplyFailure{CandidateID: "links", Reason: err.Error()})
	}
	if len(result.Failures) > 0 {
		result.Status = "failed"
	} else if result.AppliedCount > 0 {
		result.Status = "applied"
	} else if len(result.Results) > 0 {
		result.Status = "skipped"
	}
	return result
}

func consolidationStatusApplied(status string) bool {
	switch status {
	case ConsolidationStatusInserted, ConsolidationStatusReinforced, ConsolidationStatusSuperseded, ConsolidationStatusCoexisted:
		return true
	default:
		return false
	}
}

type entityResolutionNeedsReviewError struct {
	Reason string
	err    error
}

func (e entityResolutionNeedsReviewError) Error() string {
	return e.Reason
}

func (e entityResolutionNeedsReviewError) Unwrap() error {
	return e.err
}

func needsReviewApplyResult(candidateID string, reason string) FactApplyResult {
	return FactApplyResult{
		CandidateID: candidateID,
		Status:      ConsolidationStatusNeedsReview,
		Result: &ConsolidationResult{
			Action:            ConsolidationActionNeedsReview,
			Status:            ConsolidationStatusNeedsReview,
			NeedsReviewReason: reason,
		},
	}
}

func entityNeedsReviewError(role string, result Result, err error) error {
	if result.Status != "needs_review" {
		return nil
	}
	reason := strings.Join(result.ReasonCodes, ",")
	if reason == "" && err != nil {
		reason = err.Error()
	}
	if reason == "" {
		reason = "entity_requires_review"
	}
	return entityResolutionNeedsReviewError{
		Reason: fmt.Sprintf("%s entity requires review: %s", role, reason),
		err:    err,
	}
}

func factToConsolidationCandidate(ctx context.Context, svc Service, db *sql.DB, req ExtractionRequest, resp ExtractionResponse, fact ExtractedFactCandidate, pinnedTargets map[string]struct{}) (ConsolidateCandidateRequest, error) {
	resolver := Resolver{Service: svc, DB: db}
	objectKind := predicateObjectKind(req.PredicateSchemas, fact.Predicate)
	subject, err := resolver.Resolve(ctx, Input{
		PersonaID:        req.PersonaID,
		KnownEntities:    req.KnownEntities,
		ResponseEntities: resp.Entities,
		CandidateID:      fact.SubjectEntityCandidateID,
		AllowSensitive:   req.Policy.AllowSensitiveExtraction,
	})
	if err != nil {
		if reviewErr := entityNeedsReviewError("subject", subject, err); reviewErr != nil {
			return ConsolidateCandidateRequest{}, reviewErr
		}
		return ConsolidateCandidateRequest{}, fmt.Errorf("resolve subject entity: %w", err)
	}
	var objectEntityID *string
	if fact.ObjectEntityCandidateID != nil && strings.TrimSpace(*fact.ObjectEntityCandidateID) != "" {
		object, err := resolver.Resolve(ctx, Input{
			PersonaID:        req.PersonaID,
			KnownEntities:    req.KnownEntities,
			ResponseEntities: resp.Entities,
			CandidateID:      *fact.ObjectEntityCandidateID,
			AllowSensitive:   req.Policy.AllowSensitiveExtraction,
			Predicate:        fact.Predicate,
			ObjectKind:       objectKind,
		})
		if err != nil {
			if reviewErr := entityNeedsReviewError("object", object, err); reviewErr != nil {
				return ConsolidateCandidateRequest{}, reviewErr
			}
			return ConsolidateCandidateRequest{}, fmt.Errorf("resolve object entity: %w", err)
		}
		objectEntityID = &object.EntityID
	}
	_, pinnedByIntent := pinnedTargets[fact.CandidateID]
	trigger := ConsolidationTriggerManual
	if req.Trigger == ExtractionTriggerWorkCandidate {
		trigger = ConsolidationTriggerWorkCandidate
	}
	return ConsolidateCandidateRequest{
		PersonaID:   req.PersonaID,
		SessionID:   req.SessionID,
		RequestID:   req.RequestID,
		CandidateID: fact.CandidateID,
		Trigger:     trigger,
		Candidate: ManualFactCandidate{
			SubjectEntityID:  subject.EntityID,
			Predicate:        fact.Predicate,
			ObjectEntityID:   objectEntityID,
			ObjectLiteral:    fact.ObjectLiteral,
			ContentSummary:   fact.ContentSummary,
			FactType:         fact.FactType,
			ValidFrom:        fact.ValidFrom,
			ValidTo:          fact.ValidTo,
			Confidence:       fact.ExtractionConfidence,
			ConfidenceScore:  fact.ExtractionConfidenceScore,
			Importance:       fact.Importance,
			Valence:          fact.Valence,
			Arousal:          fact.Arousal,
			Sensitivity:      fact.SensitivityLevel,
			SourceEpisodeIDs: append([]string(nil), fact.SourceEpisodeIDs...),
			Pinned:           fact.Pinned || pinnedByIntent,
			UserRequested:    fact.UserRequested || pinnedByIntent,
		},
		Policy: ConsolidationPolicy{
			Approved: true,
		},
	}, nil
}

func predicateObjectKind(schemas []ExtractionPredicateSchema, predicate string) string {
	for _, schema := range schemas {
		if schema.Predicate == predicate {
			return schema.ObjectKind
		}
	}
	return ""
}

func applyAcceptedLinks(ctx context.Context, db *sql.DB, req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult, candidateFacts map[string]string) error {
	for _, link := range resp.Links {
		d, ok := decisionByID(gate.LinkDecisions, link.CandidateID)
		if !ok || d.Decision != decisionAccept {
			continue
		}
		if !extractionFactLinkType(link.LinkType) {
			return fmt.Errorf("link %s type %s must be produced by consolidation", link.CandidateID, link.LinkType)
		}
		fromID, ok := candidateFacts[link.FromCandidateID]
		if !ok {
			return fmt.Errorf("link %s from candidate %s was not applied", link.CandidateID, link.FromCandidateID)
		}
		toID, ok := candidateFacts[link.ToCandidateID]
		if !ok {
			return fmt.Errorf("link %s to candidate %s was not applied", link.CandidateID, link.ToCandidateID)
		}
		linkID, created, err := ensureFactLink(ctx, db, req.PersonaID, fromID, link.LinkType, toID, link.Confidence, link.Reasoning)
		if err != nil {
			return err
		}
		if created {
			if err := enqueueLinkSync(ctx, db, req.PersonaID, linkID); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureFactLink(ctx context.Context, db *sql.DB, personaID string, fromFactID string, linkType string, toFactID string, confidence float64, reasoning *string) (string, bool, error) {
	if ok, err := visibleSearchableFact(ctx, db, personaID, fromFactID); err != nil {
		return "", false, err
	} else if !ok {
		return "", false, fmt.Errorf("from fact %s is not visible/searchable", fromFactID)
	}
	if ok, err := visibleSearchableFact(ctx, db, personaID, toFactID); err != nil {
		return "", false, err
	} else if !ok {
		return "", false, fmt.Errorf("to fact %s is not visible/searchable", toFactID)
	}
	var existingID string
	err := db.QueryRowContext(ctx, `
SELECT id
FROM memory_links
WHERE persona_id = ?
  AND from_node_type = 'fact'
  AND from_node_id = ?
  AND link_type = ?
  AND to_node_type = 'fact'
  AND to_node_id = ?`, personaID, fromFactID, linkType, toFactID).Scan(&existingID)
	if err == nil {
		return existingID, false, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", false, err
	}
	if confidence == 0 {
		confidence = 1
	}
	linkID := "link_" + uuid.NewString()
	_, err = db.ExecContext(ctx, `
INSERT INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    reasoning, created_by, visibility_status, searchable
) VALUES (?, ?, 'fact', ?, ?, 'fact', ?, 'forward', ?, 1.0, ?, 'consolidation', 'visible', 1)`,
		linkID,
		personaID,
		fromFactID,
		linkType,
		toFactID,
		confidence,
		nullableReasoning(reasoning),
	)
	if err != nil {
		return "", false, err
	}
	return linkID, true, nil
}

func extractionLinkFactEligible(fact Fact) bool {
	return fact.VisibilityStatus == VisibilityVisible &&
		fact.Searchable &&
		fact.ValidityStatus == ValidityValid
}

func extractionFactLinkType(linkType string) bool {
	switch strings.TrimSpace(linkType) {
	case "CAUSED_BY", "EXPLAINS", "CONTRADICTS", "SUPPORTS", "INHIBITS":
		return true
	default:
		return false
	}
}

func visibleSearchableFact(ctx context.Context, db *sql.DB, personaID string, factID string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM facts
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND validity_status = 'valid'
  AND searchable = 1`, personaID, factID).Scan(&count)
	return count > 0, err
}

func enqueueLinkSync(ctx context.Context, db *sql.DB, personaID string, linkID string) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (?, ?, 'memory_link', ?, 'upsert_edge')`,
		"queue_"+uuid.NewString(),
		personaID,
		linkID,
	)
	return err
}

func nullableReasoning(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return *value
}
