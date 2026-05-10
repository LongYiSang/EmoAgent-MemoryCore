package extraction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func ApplyAcceptedFacts(ctx context.Context, svc memorycore.Service, db *sql.DB, req memorycore.ExtractionRequest, resp memorycore.ExtractionResponse, gate memorycore.ExtractionGateResult) memorycore.ExtractionApplyResult {
	result := memorycore.ExtractionApplyResult{
		RequestID: req.RequestID,
		PersonaID: req.PersonaID,
		Status:    "nothing_applied",
		Results:   []memorycore.FactApplyResult{},
		Failures:  []memorycore.FactApplyFailure{},
	}
	if svc == nil || db == nil {
		result.Failures = append(result.Failures, memorycore.FactApplyFailure{CandidateID: "pipeline", Reason: "service_and_db_required"})
		return result
	}
	pinnedTargets := pinTargets(gate, resp)
	for _, fact := range resp.Facts {
		d, ok := decisionByID(gate.FactDecisions, fact.CandidateID)
		if !ok || d.Decision != decisionAccept {
			continue
		}
		reqCandidate, err := factToConsolidationCandidate(ctx, svc, db, req, resp, fact, pinnedTargets)
		if err != nil {
			result.Failures = append(result.Failures, memorycore.FactApplyFailure{CandidateID: fact.CandidateID, Reason: err.Error()})
			continue
		}
		consolidated, err := svc.ConsolidateCandidate(ctx, reqCandidate)
		if err != nil {
			result.Failures = append(result.Failures, memorycore.FactApplyFailure{CandidateID: fact.CandidateID, Reason: err.Error()})
			continue
		}
		if consolidated.Status == memorycore.ConsolidationStatusRejected || consolidated.Status == memorycore.ConsolidationStatusNeedsReview || consolidated.Fact == nil {
			reason := consolidated.RejectedReason
			if reason == "" {
				reason = consolidated.NeedsReviewReason
			}
			if reason == "" {
				reason = "consolidation produced no fact"
			}
			result.Failures = append(result.Failures, memorycore.FactApplyFailure{CandidateID: fact.CandidateID, Reason: reason})
			result.Results = append(result.Results, memorycore.FactApplyResult{CandidateID: fact.CandidateID, Status: consolidated.Status, Result: consolidated})
			continue
		}
		result.AppliedCount++
		result.Results = append(result.Results, memorycore.FactApplyResult{CandidateID: fact.CandidateID, Status: consolidated.Status, Result: consolidated})
	}
	if len(result.Failures) > 0 {
		result.Status = "failed"
	} else if result.AppliedCount > 0 {
		result.Status = "applied"
	}
	return result
}

func factToConsolidationCandidate(ctx context.Context, svc memorycore.Service, db *sql.DB, req memorycore.ExtractionRequest, resp memorycore.ExtractionResponse, fact memorycore.ExtractedFactCandidate, pinnedTargets map[string]struct{}) (memorycore.ConsolidateCandidateRequest, error) {
	subjectID, err := resolveEntityCandidate(ctx, svc, db, req.PersonaID, req.KnownEntities, resp.Entities, fact.SubjectEntityCandidateID)
	if err != nil {
		return memorycore.ConsolidateCandidateRequest{}, fmt.Errorf("resolve subject entity: %w", err)
	}
	var objectEntityID *string
	if fact.ObjectEntityCandidateID != nil && strings.TrimSpace(*fact.ObjectEntityCandidateID) != "" {
		resolved, err := resolveEntityCandidate(ctx, svc, db, req.PersonaID, req.KnownEntities, resp.Entities, *fact.ObjectEntityCandidateID)
		if err != nil {
			return memorycore.ConsolidateCandidateRequest{}, fmt.Errorf("resolve object entity: %w", err)
		}
		objectEntityID = &resolved
	}
	_, pinnedByIntent := pinnedTargets[fact.CandidateID]
	trigger := memorycore.ConsolidationTriggerManual
	if req.Trigger == memorycore.ExtractionTriggerWorkCandidate {
		trigger = memorycore.ConsolidationTriggerWorkCandidate
	}
	return memorycore.ConsolidateCandidateRequest{
		PersonaID: req.PersonaID,
		SessionID: req.SessionID,
		Trigger:   trigger,
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  subjectID,
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
		Policy: memorycore.ConsolidationPolicy{
			Approved: true,
		},
	}, nil
}

func resolveEntityCandidate(ctx context.Context, svc memorycore.Service, db *sql.DB, personaID string, known []memorycore.ExtractionKnownEntity, candidates []memorycore.ExtractedEntityCandidate, candidateID string) (string, error) {
	candidateID = strings.TrimSpace(candidateID)
	if candidateID == "user" {
		id, err := findSingleEntityByType(ctx, db, personaID, memorycore.EntityTypeUser)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		entity, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
			ID:            "ent_user",
			PersonaID:     personaID,
			CanonicalName: "User",
			EntityType:    memorycore.EntityTypeUser,
		})
		if err != nil {
			return "", err
		}
		return entity.ID, nil
	}
	if candidateID == "agent" {
		return "", fmt.Errorf("agent entity cannot be used for Phase2B fact apply")
	}
	for _, entity := range known {
		if entity.EntityID == candidateID {
			return entity.EntityID, nil
		}
	}
	var candidate *memorycore.ExtractedEntityCandidate
	for i := range candidates {
		if candidates[i].CandidateID == candidateID {
			candidate = &candidates[i]
			break
		}
	}
	if candidate == nil {
		return "", fmt.Errorf("entity candidate %s was not found", candidateID)
	}
	if candidate.KnownEntityID != nil && strings.TrimSpace(*candidate.KnownEntityID) != "" {
		if err := requireVisibleSearchableEntity(ctx, db, personaID, *candidate.KnownEntityID); err != nil {
			return "", err
		}
		return *candidate.KnownEntityID, nil
	}
	switch candidate.MergeHint {
	case "ambiguous":
		return "", fmt.Errorf("ambiguous entity candidate cannot apply")
	case "maybe_existing":
		matches, err := findEntitiesByExactNameOrAlias(ctx, db, personaID, candidate.CanonicalName)
		if err != nil {
			return "", err
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return "", fmt.Errorf("ambiguous entity candidate cannot apply")
		}
	}
	entityType := candidate.EntityType
	if strings.TrimSpace(entityType) == "" {
		entityType = memorycore.EntityTypeConcept
	}
	entity, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		ID:               "ent_" + uuid.NewString(),
		PersonaID:        personaID,
		CanonicalName:    candidate.CanonicalName,
		EntityType:       entityType,
		SensitivityLevel: candidate.SensitivityLevel,
		Aliases:          aliasesForEnsure(candidate.Aliases),
	})
	if err != nil {
		return "", err
	}
	return entity.ID, nil
}

func findSingleEntityByType(ctx context.Context, db *sql.DB, personaID string, entityType string) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, `
SELECT id
FROM entities
WHERE persona_id = ?
  AND entity_type = ?
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY created_at ASC
LIMIT 1`, personaID, entityType).Scan(&id)
	return id, err
}

func requireVisibleSearchableEntity(ctx context.Context, db *sql.DB, personaID string, entityID string) error {
	var id string
	return db.QueryRowContext(ctx, `
SELECT id
FROM entities
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`, personaID, entityID).Scan(&id)
}

func findEntitiesByExactNameOrAlias(ctx context.Context, db *sql.DB, personaID string, name string) ([]string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
SELECT id
FROM entities
WHERE persona_id = ?
  AND canonical_name = ?
  AND visibility_status = 'visible'
  AND searchable = 1
UNION
SELECT e.id
FROM entity_aliases a
JOIN entities e ON e.id = a.entity_id AND e.persona_id = a.persona_id
WHERE a.persona_id = ?
  AND a.alias = ?
  AND e.visibility_status = 'visible'
  AND e.searchable = 1`, personaID, name, personaID, name)
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
	return ids, rows.Err()
}

func aliasesForEnsure(values []string) []memorycore.EntityAliasInput {
	aliases := make([]memorycore.EntityAliasInput, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		aliases = append(aliases, memorycore.EntityAliasInput{
			ID:         "alias_" + uuid.NewString(),
			Alias:      value,
			AliasType:  memorycore.AliasTypeSurface,
			Confidence: 1,
		})
	}
	return aliases
}
