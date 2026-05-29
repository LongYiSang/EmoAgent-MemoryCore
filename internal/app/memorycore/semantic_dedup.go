package memorycore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	semanticDedupActionRelated = "related"
	semanticDedupActionNewFact = "new_fact"
)

type extractionSemanticDeduper interface {
	RunExtractionSemanticDedup(ctx context.Context, req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult, opts SemanticDedupOptions) *DedupDiagnostics
}

type semanticDedupThresholds struct {
	DiscardDuplicate float64
	ReviewOrMerge    float64
	Related          float64
}

func (s *service) RunExtractionSemanticDedup(ctx context.Context, req ExtractionRequest, resp ExtractionResponse, gate ExtractionGateResult, opts SemanticDedupOptions) *DedupDiagnostics {
	opts = normalizeSemanticDedupOptions(opts)
	if !opts.Enabled {
		return nil
	}
	shadow := opts.Shadow || !opts.Enforce
	diag := &DedupDiagnostics{
		Ran:           true,
		Shadow:        shadow,
		SidecarStatus: "ok",
	}
	adapter, ok := s.mirrorAdapter.(MirrorDedupSearchAdapter)
	if !ok {
		diag.Degraded = true
		diag.SidecarStatus = "skipped"
		diag.FallbackReason = "dedup_adapter_unavailable"
		_ = s.recordSemanticDedupAudit(ctx, req, diag, opts)
		return diag
	}
	pinnedTargets := pinTargets(gate, resp)
	thresholds := semanticDedupThresholdProfile(opts.ThresholdProfile)
	for _, fact := range resp.Facts {
		d, ok := decisionByID(gate.FactDecisions, fact.CandidateID)
		if !ok || d.Decision != decisionAccept {
			continue
		}
		candidate, err := factToConsolidationCandidate(ctx, s, s.sqlDB, req, resp, fact, pinnedTargets)
		if err != nil {
			diag.Decisions = append(diag.Decisions, DedupDecision{
				CandidateID: fact.CandidateID,
				Action:      ConsolidationActionNeedsReview,
				Reason:      "candidate_resolution_failed",
			})
			continue
		}
		result, err := adapter.DedupSearch(ctx, MirrorDedupSearchRequest{
			RequestID: req.RequestID + ":dedup:" + fact.CandidateID,
			PersonaID: req.PersonaID,
			Candidate: MirrorDedupCandidate{
				CandidateID:      fact.CandidateID,
				SafeSummary:      candidate.Candidate.ContentSummary,
				FactType:         candidate.Candidate.FactType,
				Predicate:        candidate.Candidate.Predicate,
				SubjectEntityID:  candidate.Candidate.SubjectEntityID,
				ObjectEntityID:   candidate.Candidate.ObjectEntityID,
				ObjectLiteral:    derefString(candidate.Candidate.ObjectLiteral),
				SourceEpisodeIDs: append([]string(nil), candidate.Candidate.SourceEpisodeIDs...),
			},
			Policy: MirrorDedupSearchPolicy{
				Limit:             opts.CandidateLimit,
				SameSubjectBoost:  true,
				SameFactTypeBoost: true,
				ThresholdProfile:  opts.ThresholdProfile,
				Shadow:            shadow,
			},
		})
		if err != nil {
			diag.Degraded = true
			diag.SidecarStatus = "failed"
			diag.FallbackReason = "dedup_sidecar_failed"
			diag.Decisions = append(diag.Decisions, DedupDecision{
				CandidateID: fact.CandidateID,
				Action:      semanticDedupActionNewFact,
				Reason:      "sidecar_failed_canonical_fallback",
			})
			continue
		}
		if result.Degraded {
			diag.Degraded = true
			diag.SidecarStatus = "degraded"
			if strings.TrimSpace(result.FallbackReason) != "" {
				diag.FallbackReason = strings.TrimSpace(result.FallbackReason)
			}
		}
		decision, authorityFound := s.semanticDedupDecision(ctx, req.PersonaID, candidate, result.Candidates, thresholds, opts)
		if authorityFound {
			diag.CandidateCount++
		}
		if decision.CandidateID == "" {
			decision = DedupDecision{CandidateID: fact.CandidateID, Action: semanticDedupActionNewFact, Reason: "no_authority_candidate"}
		}
		diag.Decisions = append(diag.Decisions, decision)
	}
	if err := s.recordSemanticDedupAudit(ctx, req, diag, opts); err != nil {
		diag.Degraded = true
		if diag.FallbackReason == "" {
			diag.FallbackReason = "semantic_audit_failed"
		}
	}
	return diag
}

func (s *service) semanticDedupDecision(ctx context.Context, personaID string, candidate ConsolidateCandidateRequest, matches []MirrorDedupSearchCandidate, thresholds semanticDedupThresholds, opts SemanticDedupOptions) (DedupDecision, bool) {
	for _, match := range matches {
		if match.NodeType != ForgetNodeFact || strings.TrimSpace(match.NodeID) == "" {
			continue
		}
		existing, err := loadSemanticDedupFact(ctx, s.sqlDB, personaID, match.NodeID)
		if err != nil {
			continue
		}
		action := semanticDedupActionFor(candidate, existing, match, thresholds, opts)
		reason := strings.TrimSpace(match.MatchReason)
		if reason == "" {
			reason = strings.TrimSpace(match.MatchClass)
		}
		if reason == "" {
			reason = "semantic_candidate"
		}
		return DedupDecision{
			CandidateID: candidate.CandidateID,
			NodeType:    match.NodeType,
			NodeID:      match.NodeID,
			Similarity:  match.Similarity,
			Action:      action,
			Reason:      reason,
		}, true
	}
	return DedupDecision{}, false
}

func semanticDedupActionFor(candidate ConsolidateCandidateRequest, existing Fact, match MirrorDedupSearchCandidate, thresholds semanticDedupThresholds, opts SemanticDedupOptions) string {
	if match.Similarity >= thresholds.DiscardDuplicate {
		if opts.Enforce && semanticDedupCompatible(candidate, existing) {
			return ConsolidationActionDiscardDuplicate
		}
		return "review_or_merge"
	}
	if match.Similarity >= thresholds.ReviewOrMerge {
		return "review_or_merge"
	}
	if match.Similarity >= thresholds.Related {
		return semanticDedupActionRelated
	}
	return semanticDedupActionNewFact
}

func semanticDedupCompatible(candidate ConsolidateCandidateRequest, existing Fact) bool {
	if candidate.Candidate.SubjectEntityID == "" || existing.SubjectEntityID == nil || candidate.Candidate.SubjectEntityID != *existing.SubjectEntityID {
		return false
	}
	return strings.TrimSpace(candidate.Candidate.Predicate) == strings.TrimSpace(existing.Predicate)
}

func semanticDedupThresholdProfile(profile string) semanticDedupThresholds {
	switch strings.TrimSpace(profile) {
	default:
		return semanticDedupThresholds{
			DiscardDuplicate: 0.94,
			ReviewOrMerge:    0.82,
			Related:          0.70,
		}
	}
}

func semanticDedupApplyDecision(diag *DedupDiagnostics, candidateID string) (DedupDecision, bool) {
	if diag == nil || diag.Shadow {
		return DedupDecision{}, false
	}
	for _, decision := range diag.Decisions {
		if decision.CandidateID == candidateID && decision.Action == ConsolidationActionDiscardDuplicate && decision.NodeType == ForgetNodeFact && strings.TrimSpace(decision.NodeID) != "" {
			return decision, true
		}
	}
	return DedupDecision{}, false
}

func loadSemanticDedupFact(ctx context.Context, db *sql.DB, personaID string, factID string) (Fact, error) {
	if db == nil {
		return Fact{}, sql.ErrNoRows
	}
	var fact Fact
	var subjectID, objectID, objectLiteral, validFrom, validTo, updatedAt sql.NullString
	var createdAt string
	var pinned, searchable int
	err := db.QueryRowContext(ctx, `
SELECT id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
       content_summary, fact_type, valid_from, valid_to, extraction_confidence,
       extraction_confidence_score, importance, valence, arousal, sensitivity_level,
       validity_status, visibility_status, lifecycle_status, pinned,
       reinforcement_count, searchable, created_at, updated_at
FROM facts
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND validity_status = 'valid'
  AND lifecycle_status = 'active'`, personaID, factID).Scan(
		&fact.ID,
		&fact.PersonaID,
		&subjectID,
		&fact.Predicate,
		&objectID,
		&objectLiteral,
		&fact.ContentSummary,
		&fact.FactType,
		&validFrom,
		&validTo,
		&fact.Confidence,
		&fact.ConfidenceScore,
		&fact.Importance,
		&fact.Valence,
		&fact.Arousal,
		&fact.Sensitivity,
		&fact.ValidityStatus,
		&fact.VisibilityStatus,
		&fact.LifecycleStatus,
		&pinned,
		&fact.ReinforcementCount,
		&searchable,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return Fact{}, err
	}
	fact.SubjectEntityID = nullStringPtr(subjectID)
	fact.ObjectEntityID = nullStringPtr(objectID)
	fact.ObjectLiteral = nullStringPtr(objectLiteral)
	if validFrom.Valid {
		parsed := parseTime(validFrom.String)
		fact.ValidFrom = &parsed
	}
	if validTo.Valid {
		parsed := parseTime(validTo.String)
		fact.ValidTo = &parsed
	}
	if updatedAt.Valid {
		parsed := parseTime(updatedAt.String)
		fact.UpdatedAt = &parsed
	}
	fact.CreatedAt = parseTime(createdAt)
	fact.Pinned = pinned == 1
	fact.Searchable = searchable == 1
	return fact, nil
}

func (s *service) recordSemanticDedupAudit(ctx context.Context, req ExtractionRequest, diag *DedupDiagnostics, opts SemanticDedupOptions) error {
	if diag == nil {
		return nil
	}
	selected := make([]ExactNodeRef, 0, len(diag.Decisions))
	scores := map[string]any{}
	for _, decision := range diag.Decisions {
		if strings.TrimSpace(decision.NodeType) != "" && strings.TrimSpace(decision.NodeID) != "" {
			selected = append(selected, ExactNodeRef{NodeType: decision.NodeType, NodeID: decision.NodeID})
		}
		if strings.TrimSpace(decision.CandidateID) != "" {
			scores[decision.CandidateID] = map[string]any{
				"node_type":  decision.NodeType,
				"node_id":    decision.NodeID,
				"similarity": decision.Similarity,
				"action":     decision.Action,
			}
		}
	}
	decisionType := "dedup_shadow"
	if opts.Enforce && !diag.Shadow {
		decisionType = "dedup_enforce"
	}
	return s.recordSemanticDecisionAudit(ctx, semanticDecisionAuditRecord{
		RequestID:       req.RequestID,
		PersonaID:       req.PersonaID,
		DecisionType:    decisionType,
		Actor:           ForgetActorSystem,
		ReasonCode:      "extraction_semantic_dedup",
		CandidateHash:   semanticDedupCandidateHash(req),
		SelectedNodeIDs: selected,
		PolicySnapshot: map[string]any{
			"enabled":           opts.Enabled,
			"shadow":            diag.Shadow,
			"enforce":           opts.Enforce,
			"candidate_limit":   opts.CandidateLimit,
			"threshold_profile": opts.ThresholdProfile,
		},
		SimilarityScores: scores,
		SidecarStatus:    defaultString(diag.SidecarStatus, "skipped"),
		DiagnosticsJSON: map[string]any{
			"degraded":        diag.Degraded,
			"fallback_reason": diag.FallbackReason,
			"candidate_count": diag.CandidateCount,
		},
	})
}

func semanticDedupCandidateHash(req ExtractionRequest) string {
	items := make([]map[string]string, 0, len(req.Episodes))
	for _, episode := range req.Episodes {
		items = append(items, map[string]string{
			"episode_id":        episode.EpisodeID,
			"sensitivity_level": episode.SensitivityLevel,
		})
	}
	return hashJSON(map[string]any{
		"request_id": req.RequestID,
		"persona_id": req.PersonaID,
		"trigger":    req.Trigger,
		"episodes":   items,
	})
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func semanticDedupDiscardResult(ctx context.Context, db *sql.DB, personaID string, decision DedupDecision) (*ConsolidationResult, error) {
	fact, err := loadSemanticDedupFact(ctx, db, personaID, decision.NodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("semantic duplicate authority fact not found")
		}
		return nil, err
	}
	return &ConsolidationResult{
		Action:       ConsolidationActionDiscardDuplicate,
		Status:       ConsolidationStatusDiscarded,
		Fact:         &fact,
		ExistingFact: &fact,
	}, nil
}
