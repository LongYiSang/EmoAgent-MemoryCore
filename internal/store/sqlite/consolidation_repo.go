package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	ConsolidationTriggerManual        = "manual"
	ConsolidationTriggerAgentAffect   = "agent_affect"
	ConsolidationTriggerWorkCandidate = "work_candidate"

	ConsolidationActionInsert           = "insert"
	ConsolidationActionDiscardDuplicate = "discard_duplicate"
	ConsolidationActionReinforce        = "reinforce"
	ConsolidationActionSupersede        = "supersede"
	ConsolidationActionCoexist          = "coexist"
	ConsolidationActionMergeBaseline    = "merge_baseline"
	ConsolidationActionReject           = "reject"
	ConsolidationActionNeedsReview      = "needs_review"
	ConsolidationActionDuplicateApply   = "duplicate_apply"
	ConsolidationActionReinforceNoop    = "reinforce_noop"

	ConsolidationStatusInserted    = "inserted"
	ConsolidationStatusDiscarded   = "discarded"
	ConsolidationStatusReinforced  = "reinforced"
	ConsolidationStatusSuperseded  = "superseded"
	ConsolidationStatusCoexisted   = "coexisted"
	ConsolidationStatusRejected    = "rejected"
	ConsolidationStatusNeedsReview = "needs_review"
	ConsolidationStatusSkipped     = "skipped"
)

type ConsolidationRepository struct {
	db    *sql.DB
	newID func() string
	now   func() time.Time
}

type ConsolidateCandidateRequest struct {
	PersonaID   string
	SessionID   *string
	RequestID   string
	CandidateID string
	Trigger     string
	Candidate   ManualFactCandidate
	Policy      ConsolidationPolicy
}

type ManualFactCandidate struct {
	SubjectEntityID  string
	Predicate        string
	ObjectEntityID   *string
	ObjectLiteral    *string
	ContentSummary   string
	FactType         string
	ValidFrom        *time.Time
	ValidTo          *time.Time
	Confidence       string
	ConfidenceScore  float64
	Importance       float64
	Valence          float64
	Arousal          float64
	Sensitivity      string
	SourceEpisodeIDs []string
	Pinned           bool
	UserRequested    bool
}

type ConsolidationPolicy struct {
	Action                      string
	Approved                    bool
	AllowManualPinWithoutSource bool
}

type ConsolidationResult struct {
	Action            string
	Status            string
	Fact              *core.Fact
	ExistingFact      *core.Fact
	SupersededFactIDs []string
	LinkIDs           []string
	RejectedReason    string
	NeedsReviewReason string
}

type sourceEpisode struct {
	ID         string
	OccurredAt time.Time
}

func NewConsolidationRepository(db *sql.DB, newID func() string, now func() time.Time) *ConsolidationRepository {
	if newID == nil {
		counter := 0
		newID = func() string {
			counter++
			return fmt.Sprintf("generated_%d", counter)
		}
	}
	if now == nil {
		now = time.Now
	}
	return &ConsolidationRepository{db: db, newID: newID, now: now}
}

func (r *ConsolidationRepository) ConsolidateCandidate(ctx context.Context, req ConsolidateCandidateRequest) (ConsolidationResult, error) {
	if strings.TrimSpace(req.PersonaID) == "" {
		return rejectResult("persona_id is required"), nil
	}
	trigger := defaultConsolidationTrigger(req.Trigger)
	if trigger == ConsolidationTriggerAgentAffect {
		return rejectResult("agent affect candidates cannot write facts"), nil
	}
	if trigger == ConsolidationTriggerWorkCandidate && !req.Policy.Approved {
		return rejectResult("work candidate is not approved by memory policy"), nil
	}
	switch req.Policy.Action {
	case ConsolidationActionReject:
		return rejectResult("policy rejected candidate"), nil
	case ConsolidationActionNeedsReview:
		return needsReviewResult("policy requires review"), nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ConsolidationResult{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := r.consolidateCandidateTx(ctx, tx, req)
	if err != nil {
		return ConsolidationResult{}, err
	}
	if result.Status == ConsolidationStatusRejected || result.Status == ConsolidationStatusNeedsReview {
		if commitErr := tx.Commit(); commitErr != nil {
			return ConsolidationResult{}, commitErr
		}
		return result, nil
	}
	if err = tx.Commit(); err != nil {
		return ConsolidationResult{}, err
	}
	return result, nil
}

func (r *ConsolidationRepository) consolidateCandidateTx(ctx context.Context, tx *sql.Tx, req ConsolidateCandidateRequest) (ConsolidationResult, error) {
	candidate := req.Candidate
	schema, err := getPredicateSchemaTx(ctx, tx, candidate.Predicate)
	if errors.Is(err, sql.ErrNoRows) {
		return rejectResult("predicate schema does not exist"), nil
	}
	if err != nil {
		return ConsolidationResult{}, err
	}
	if reason := validateCandidateShape(candidate, schema, req.Policy); reason != "" {
		return rejectResult(reason), nil
	}
	if err := requireVisibleEntityTx(ctx, tx, req.PersonaID, candidate.SubjectEntityID); errors.Is(err, sql.ErrNoRows) {
		return rejectResult("subject entity does not exist"), nil
	} else if err != nil {
		return ConsolidationResult{}, err
	}
	if candidate.ObjectEntityID != nil {
		if err := requireVisibleEntityTx(ctx, tx, req.PersonaID, *candidate.ObjectEntityID); errors.Is(err, sql.ErrNoRows) {
			return rejectResult("object entity does not exist"), nil
		} else if err != nil {
			return ConsolidationResult{}, err
		}
	}

	sources, err := loadSourceEpisodesTx(ctx, tx, req.PersonaID, candidate.SourceEpisodeIDs)
	if err != nil {
		return ConsolidationResult{}, err
	}
	if len(candidate.SourceEpisodeIDs) > 0 && len(sources) != len(candidate.SourceEpisodeIDs) {
		return rejectResult("source episode does not exist or is not visible"), nil
	}
	if len(candidate.SourceEpisodeIDs) == 0 && !manualPinSourceException(candidate, req.Policy) {
		return rejectResult("source_episode_ids are required"), nil
	}

	if schema.ConflictPolicy == core.ConflictPolicyLLMCheck {
		return needsReviewResult("predicate requires llm_check review"), nil
	}
	var reviewReason string
	candidate, reviewReason = applyExpireByTimePolicy(candidate, schema, sources, r.now())
	if reviewReason != "" {
		return needsReviewResult(reviewReason), nil
	}
	fingerprint := consolidationApplyFingerprint(req.PersonaID, candidate)
	if previous, err := findApplyFingerprintTx(ctx, tx, req.PersonaID, fingerprint); err == nil {
		if previous.FactID != "" {
			stored, err := getFactTx(ctx, tx, req.PersonaID, previous.FactID)
			if err != nil {
				return ConsolidationResult{}, err
			}
			return ConsolidationResult{
				Action:       ConsolidationActionDuplicateApply,
				Status:       ConsolidationStatusSkipped,
				Fact:         &stored,
				ExistingFact: &stored,
			}, nil
		}
		return ConsolidationResult{Action: ConsolidationActionDuplicateApply, Status: ConsolidationStatusSkipped}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ConsolidationResult{}, err
	}

	existing, err := activeFactsForCandidateTx(ctx, tx, req.PersonaID, candidate)
	if err != nil {
		return ConsolidationResult{}, err
	}
	if duplicate := matchingCanonicalFact(existing, candidate); duplicate != nil {
		if req.Policy.Action == ConsolidationActionDiscardDuplicate {
			return ConsolidationResult{
				Action:       ConsolidationActionDiscardDuplicate,
				Status:       ConsolidationStatusDiscarded,
				Fact:         duplicate,
				ExistingFact: duplicate,
			}, nil
		}
		if hasAll, err := factHasAllSourceEvidenceTx(ctx, tx, req.PersonaID, duplicate.ID, candidate.SourceEpisodeIDs); err != nil {
			return ConsolidationResult{}, err
		} else if hasAll {
			stored, err := getFactTx(ctx, tx, req.PersonaID, duplicate.ID)
			if err != nil {
				return ConsolidationResult{}, err
			}
			if err := insertApplyFingerprintTx(ctx, tx, req.PersonaID, fingerprint, duplicate.ID, ConsolidationActionReinforceNoop, req); err != nil {
				return ConsolidationResult{}, err
			}
			return ConsolidationResult{
				Action:       ConsolidationActionReinforceNoop,
				Status:       ConsolidationStatusSkipped,
				Fact:         &stored,
				ExistingFact: duplicate,
			}, nil
		}
		result, err := r.reinforceFactTx(ctx, tx, req.PersonaID, req.SessionID, *duplicate, candidate)
		if err != nil {
			return ConsolidationResult{}, err
		}
		if result.Fact != nil {
			if err := insertApplyFingerprintTx(ctx, tx, req.PersonaID, fingerprint, result.Fact.ID, result.Action, req); err != nil {
				return ConsolidationResult{}, err
			}
		}
		return result, nil
	}
	action := insertionAction(schema, len(existing))
	fact := buildFact(req.PersonaID, candidate, schema, r.newID())
	if err := insertFactTx(ctx, tx, fact); err != nil {
		return ConsolidationResult{}, err
	}
	linkIDs, err := r.writeFactLinksTx(ctx, tx, req.PersonaID, fact, candidate, sources)
	if err != nil {
		return ConsolidationResult{}, err
	}
	if err := upsertFactSearchDocumentTx(ctx, tx, req.PersonaID, fact.ID); err != nil {
		return ConsolidationResult{}, err
	}
	if err := r.enqueueIndexSyncTx(ctx, tx, req.PersonaID, string(core.NodeTypeFact), fact.ID, "upsert_node"); err != nil {
		return ConsolidationResult{}, err
	}
	if _, err := recordSessionFactWriteTx(ctx, tx, req.PersonaID, req.SessionID, fact.ID); err != nil {
		return ConsolidationResult{}, err
	}

	var superseded []string
	if schema.ConflictPolicy == core.ConflictPolicySupersede && len(existing) > 0 {
		invalidationTime := invalidationTimeFor(candidate, sources, r.now())
		for _, oldFact := range existing {
			if err := invalidateFactTx(ctx, tx, req.PersonaID, oldFact.ID, invalidationTime); err != nil {
				return ConsolidationResult{}, err
			}
			if err := upsertFactSearchDocumentTx(ctx, tx, req.PersonaID, oldFact.ID); err != nil {
				return ConsolidationResult{}, err
			}
			superseded = append(superseded, oldFact.ID)
			linkID, created, err := r.ensureLinkTx(ctx, tx, core.MemoryLink{
				ID:           r.newID(),
				PersonaID:    req.PersonaID,
				FromNodeType: core.NodeTypeFact,
				FromNodeID:   fact.ID,
				LinkType:     core.LinkTypeSupersedes,
				ToNodeType:   core.NodeTypeFact,
				ToNodeID:     oldFact.ID,
				CreatedBy:    core.LinkCreatedByConsolidation,
			})
			if err != nil {
				return ConsolidationResult{}, err
			}
			if created {
				linkIDs = append(linkIDs, linkID)
				if err := r.enqueueIndexSyncTx(ctx, tx, req.PersonaID, "memory_link", linkID, "upsert_edge"); err != nil {
					return ConsolidationResult{}, err
				}
			}
			if err := r.enqueueIndexSyncTx(ctx, tx, req.PersonaID, string(core.NodeTypeFact), oldFact.ID, "upsert_node"); err != nil {
				return ConsolidationResult{}, err
			}
		}
		action = ConsolidationActionSupersede
	}

	stored, err := getFactTx(ctx, tx, req.PersonaID, fact.ID)
	if err != nil {
		return ConsolidationResult{}, err
	}
	if err := insertApplyFingerprintTx(ctx, tx, req.PersonaID, fingerprint, stored.ID, action, req); err != nil {
		return ConsolidationResult{}, err
	}
	return ConsolidationResult{
		Action:            action,
		Status:            statusForAction(action),
		Fact:              &stored,
		SupersededFactIDs: superseded,
		LinkIDs:           linkIDs,
	}, nil
}

func (r *ConsolidationRepository) reinforceFactTx(ctx context.Context, tx *sql.Tx, personaID string, sessionID *string, fact core.Fact, candidate ManualFactCandidate) (ConsolidationResult, error) {
	importance := fact.Importance
	if candidate.Importance > importance {
		importance = candidate.Importance
	}
	linkIDs, err := r.writeFactLinksTx(ctx, tx, personaID, fact, candidate, nil)
	if err != nil {
		return ConsolidationResult{}, err
	}
	canReinforce, err := recordSessionFactWriteTx(ctx, tx, personaID, sessionID, fact.ID)
	if err != nil {
		return ConsolidationResult{}, err
	}
	if canReinforce {
		if err := reinforceFactTx(ctx, tx, personaID, fact.ID, importance); err != nil {
			return ConsolidationResult{}, err
		}
	}
	if err := upsertFactSearchDocumentTx(ctx, tx, personaID, fact.ID); err != nil {
		return ConsolidationResult{}, err
	}
	if err := r.enqueueIndexSyncTx(ctx, tx, personaID, string(core.NodeTypeFact), fact.ID, "upsert_node"); err != nil {
		return ConsolidationResult{}, err
	}
	stored, err := getFactTx(ctx, tx, personaID, fact.ID)
	if err != nil {
		return ConsolidationResult{}, err
	}
	action := ConsolidationActionReinforce
	status := ConsolidationStatusReinforced
	if !canReinforce {
		action = ConsolidationActionReinforceNoop
		status = ConsolidationStatusSkipped
	}
	return ConsolidationResult{
		Action:       action,
		Status:       status,
		Fact:         &stored,
		ExistingFact: &fact,
		LinkIDs:      linkIDs,
	}, nil
}

func (r *ConsolidationRepository) writeFactLinksTx(ctx context.Context, tx *sql.Tx, personaID string, fact core.Fact, candidate ManualFactCandidate, sources []sourceEpisode) ([]string, error) {
	var linkIDs []string
	for _, sourceID := range candidate.SourceEpisodeIDs {
		linkID, created, err := r.ensureLinkTx(ctx, tx, core.MemoryLink{
			ID:           r.newID(),
			PersonaID:    personaID,
			FromNodeType: core.NodeTypeFact,
			FromNodeID:   fact.ID,
			LinkType:     core.LinkTypeEvidencedBy,
			ToNodeType:   core.NodeTypeEpisode,
			ToNodeID:     sourceID,
			CreatedBy:    core.LinkCreatedByConsolidation,
		})
		if err != nil {
			return nil, err
		}
		if created {
			linkIDs = append(linkIDs, linkID)
			if err := r.enqueueIndexSyncTx(ctx, tx, personaID, "memory_link", linkID, "upsert_edge"); err != nil {
				return nil, err
			}
		}
	}
	linkID, created, err := r.ensureLinkTx(ctx, tx, core.MemoryLink{
		ID:           r.newID(),
		PersonaID:    personaID,
		FromNodeType: core.NodeTypeFact,
		FromNodeID:   fact.ID,
		LinkType:     core.LinkTypeAboutEntity,
		ToNodeType:   core.NodeTypeEntity,
		ToNodeID:     candidate.SubjectEntityID,
		CreatedBy:    core.LinkCreatedByConsolidation,
	})
	if err != nil {
		return nil, err
	}
	if created {
		linkIDs = append(linkIDs, linkID)
		if err := r.enqueueIndexSyncTx(ctx, tx, personaID, "memory_link", linkID, "upsert_edge"); err != nil {
			return nil, err
		}
	}
	if candidate.ObjectEntityID != nil {
		linkID, created, err := r.ensureLinkTx(ctx, tx, core.MemoryLink{
			ID:           r.newID(),
			PersonaID:    personaID,
			FromNodeType: core.NodeTypeFact,
			FromNodeID:   fact.ID,
			LinkType:     core.LinkTypeAboutEntity,
			ToNodeType:   core.NodeTypeEntity,
			ToNodeID:     *candidate.ObjectEntityID,
			CreatedBy:    core.LinkCreatedByConsolidation,
		})
		if err != nil {
			return nil, err
		}
		if created {
			linkIDs = append(linkIDs, linkID)
			if err := r.enqueueIndexSyncTx(ctx, tx, personaID, "memory_link", linkID, "upsert_edge"); err != nil {
				return nil, err
			}
		}
	}
	_ = sources
	return linkIDs, nil
}

func (r *ConsolidationRepository) ensureLinkTx(ctx context.Context, tx *sql.Tx, link core.MemoryLink) (string, bool, error) {
	link = normalizeLink(link)
	if err := requireNodeExists(ctx, tx, link.PersonaID, link.FromNodeType, link.FromNodeID); err != nil {
		return "", false, err
	}
	if err := requireNodeExists(ctx, tx, link.PersonaID, link.ToNodeType, link.ToNodeID); err != nil {
		return "", false, err
	}

	var existingID string
	err := tx.QueryRowContext(ctx, `
SELECT id
FROM memory_links
WHERE persona_id = ?
  AND from_node_type = ?
  AND from_node_id = ?
  AND link_type = ?
  AND to_node_type = ?
  AND to_node_id = ?`,
		link.PersonaID,
		string(link.FromNodeType),
		link.FromNodeID,
		string(link.LinkType),
		string(link.ToNodeType),
		link.ToNodeID,
	).Scan(&existingID)
	if err == nil {
		return existingID, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    reasoning, created_by, visibility_status, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		link.ID,
		link.PersonaID,
		string(link.FromNodeType),
		link.FromNodeID,
		string(link.LinkType),
		string(link.ToNodeType),
		link.ToNodeID,
		string(link.Direction),
		link.Confidence,
		link.Weight,
		nullableString(link.Reasoning),
		string(link.CreatedBy),
		string(link.VisibilityStatus),
		boolInt(link.Searchable),
	)
	if err != nil {
		return "", false, err
	}
	return link.ID, true, nil
}

func (r *ConsolidationRepository) enqueueIndexSyncTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType string, nodeID string, operation string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (?, ?, ?, ?, ?)`,
		r.newID(),
		personaID,
		nodeType,
		nodeID,
		operation,
	)
	return err
}

func getPredicateSchemaTx(ctx context.Context, tx *sql.Tx, predicate string) (core.PredicateSchema, error) {
	var schema core.PredicateSchema
	var label, defaultFactType sql.NullString
	var tau sql.NullFloat64
	var allowInference, sensitiveByDefault int
	err := tx.QueryRowContext(ctx, `
SELECT predicate, canonical_label, default_fact_type, cardinality, conflict_policy,
       temporal_behavior, object_kind, default_tau_days, default_importance,
       allow_inference, sensitive_by_default
FROM predicate_schemas
WHERE predicate = ?`, predicate).Scan(
		&schema.Predicate,
		&label,
		&defaultFactType,
		&schema.Cardinality,
		&schema.ConflictPolicy,
		&schema.TemporalBehavior,
		&schema.ObjectKind,
		&tau,
		&schema.DefaultImportance,
		&allowInference,
		&sensitiveByDefault,
	)
	if err != nil {
		return core.PredicateSchema{}, err
	}
	schema.CanonicalLabel = stringPtr(label)
	if defaultFactType.Valid {
		value := core.FactType(defaultFactType.String)
		schema.DefaultFactType = &value
	}
	if tau.Valid {
		schema.DefaultTauDays = &tau.Float64
	}
	schema.AllowInference = intBool(allowInference)
	schema.SensitiveByDefault = intBool(sensitiveByDefault)
	return schema, nil
}

func validateCandidateShape(candidate ManualFactCandidate, schema core.PredicateSchema, policy ConsolidationPolicy) string {
	if strings.TrimSpace(candidate.SubjectEntityID) == "" {
		return "subject_entity_id is required"
	}
	if strings.TrimSpace(candidate.Predicate) == "" {
		return "predicate is required"
	}
	if strings.TrimSpace(candidate.ContentSummary) == "" {
		return "content_summary is required"
	}
	hasObjectEntity := candidate.ObjectEntityID != nil && strings.TrimSpace(*candidate.ObjectEntityID) != ""
	hasObjectLiteral := candidate.ObjectLiteral != nil && strings.TrimSpace(*candidate.ObjectLiteral) != ""
	switch schema.ObjectKind {
	case "entity":
		if !hasObjectEntity {
			return "predicate requires object_entity_id"
		}
	case "literal":
		if !hasObjectLiteral {
			return "predicate requires object_literal"
		}
	default:
		if !hasObjectEntity && !hasObjectLiteral {
			return "predicate requires an object"
		}
	}
	if len(candidate.SourceEpisodeIDs) == 0 && !manualPinSourceException(candidate, policy) {
		return "source_episode_ids are required"
	}
	return ""
}

func requireVisibleEntityTx(ctx context.Context, tx *sql.Tx, personaID string, entityID string) error {
	var visibility string
	var searchable int
	if err := tx.QueryRowContext(ctx, `
SELECT visibility_status, searchable
FROM entities
WHERE persona_id = ? AND id = ?`, personaID, entityID).Scan(&visibility, &searchable); err != nil {
		return err
	}
	if visibility != string(core.VisibilityVisible) || searchable != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func loadSourceEpisodesTx(ctx context.Context, tx *sql.Tx, personaID string, sourceIDs []string) ([]sourceEpisode, error) {
	sources := make([]sourceEpisode, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		var occurredAt, visibility string
		var searchable int
		err := tx.QueryRowContext(ctx, `
SELECT occurred_at, visibility_status, searchable
FROM episodes
WHERE persona_id = ? AND id = ?`, personaID, sourceID).Scan(&occurredAt, &visibility, &searchable)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if visibility != string(core.VisibilityVisible) || searchable != 1 {
			continue
		}
		sources = append(sources, sourceEpisode{ID: sourceID, OccurredAt: parseTime(occurredAt)})
	}
	return sources, nil
}

func activeFactsForCandidateTx(ctx context.Context, tx *sql.Tx, personaID string, candidate ManualFactCandidate) ([]core.Fact, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
       content_summary, fact_type, valid_from, valid_to,
       extraction_confidence, extraction_confidence_score, extraction_reasoning,
       importance, valence, arousal, sensitivity_level,
       validity_status, visibility_status, lifecycle_status,
       pinned, pin_reason, pin_actor, reinforcement_count, searchable, created_at, updated_at
FROM facts
WHERE persona_id = ?
  AND subject_entity_id = ?
  AND predicate = ?
  AND validity_status IN ('valid', 'uncertain')
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY created_at ASC`, personaID, candidate.SubjectEntityID, candidate.Predicate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var facts []core.Fact
	for rows.Next() {
		fact, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return facts, nil
}

func matchingCanonicalFact(facts []core.Fact, candidate ManualFactCandidate) *core.Fact {
	candidateKey := canonicalObjectKey(candidate.ObjectEntityID, candidate.ObjectLiteral)
	for i := range facts {
		factKey := canonicalObjectKey(facts[i].ObjectEntityID, facts[i].ObjectLiteral)
		if factKey == candidateKey {
			return &facts[i]
		}
	}
	return nil
}

func buildFact(personaID string, candidate ManualFactCandidate, schema core.PredicateSchema, id string) core.Fact {
	factType := core.FactType(candidate.FactType)
	if factType == "" && schema.DefaultFactType != nil {
		factType = *schema.DefaultFactType
	}
	if factType == "" {
		factType = core.FactTypeStablePreference
	}
	confidence := core.ExtractionConfidence(candidate.Confidence)
	if confidence == "" {
		confidence = core.ExtractionConfidenceExplicit
	}
	confidenceScore := candidate.ConfidenceScore
	if confidenceScore == 0 {
		confidenceScore = 0.5
	}
	importance := candidate.Importance
	if importance == 0 {
		importance = schema.DefaultImportance
	}
	if importance == 0 {
		importance = 0.5
	}
	sensitivity := core.SensitivityLevel(candidate.Sensitivity)
	if sensitivity == "" {
		if schema.SensitiveByDefault {
			sensitivity = core.SensitivitySensitive
		} else {
			sensitivity = core.SensitivityNormal
		}
	}
	var pinActor *string
	var pinReason *string
	if candidate.Pinned {
		actor := "user"
		reason := "manual pin"
		pinActor = &actor
		pinReason = &reason
	}
	return core.Fact{
		ID:                        id,
		PersonaID:                 personaID,
		SubjectEntityID:           &candidate.SubjectEntityID,
		Predicate:                 candidate.Predicate,
		ObjectEntityID:            candidate.ObjectEntityID,
		ObjectLiteral:             candidate.ObjectLiteral,
		ContentSummary:            candidate.ContentSummary,
		FactType:                  factType,
		ValidFrom:                 candidate.ValidFrom,
		ValidTo:                   candidate.ValidTo,
		ExtractionConfidence:      confidence,
		ExtractionConfidenceScore: confidenceScore,
		Importance:                importance,
		Valence:                   candidate.Valence,
		Arousal:                   candidate.Arousal,
		SensitivityLevel:          sensitivity,
		ValidityStatus:            core.ValidityValid,
		VisibilityStatus:          core.VisibilityVisible,
		LifecycleStatus:           core.LifecycleActive,
		Pinned:                    candidate.Pinned,
		PinReason:                 pinReason,
		PinActor:                  pinActor,
		Searchable:                true,
	}
}

func insertFactTx(ctx context.Context, tx *sql.Tx, fact core.Fact) error {
	fact = normalizeFact(fact)
	_, err := tx.ExecContext(ctx, `
INSERT INTO facts (
    id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
    content_summary, fact_type, valid_from, valid_to,
    extraction_confidence, extraction_confidence_score, extraction_reasoning,
    importance, valence, arousal, sensitivity_level,
    validity_status, visibility_status, lifecycle_status,
    pinned, pin_reason, pin_actor, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fact.ID,
		fact.PersonaID,
		nullableString(fact.SubjectEntityID),
		fact.Predicate,
		nullableString(fact.ObjectEntityID),
		nullableString(fact.ObjectLiteral),
		fact.ContentSummary,
		string(fact.FactType),
		nullableTime(fact.ValidFrom),
		nullableTime(fact.ValidTo),
		string(fact.ExtractionConfidence),
		fact.ExtractionConfidenceScore,
		nullableString(fact.ExtractionReasoning),
		fact.Importance,
		fact.Valence,
		fact.Arousal,
		string(fact.SensitivityLevel),
		string(fact.ValidityStatus),
		string(fact.VisibilityStatus),
		string(fact.LifecycleStatus),
		boolInt(fact.Pinned),
		nullableString(fact.PinReason),
		nullableString(fact.PinActor),
		boolInt(fact.Searchable),
	)
	return err
}

func getFactTx(ctx context.Context, tx *sql.Tx, personaID string, factID string) (core.Fact, error) {
	row := tx.QueryRowContext(ctx, `
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

func reinforceFactTx(ctx context.Context, tx *sql.Tx, personaID string, factID string, importance float64) error {
	_, err := tx.ExecContext(ctx, `
UPDATE facts
SET reinforcement_count = reinforcement_count + 1,
    importance = CASE WHEN importance > ? THEN importance ELSE ? END,
    updated_at = CURRENT_TIMESTAMP
WHERE persona_id = ? AND id = ?`,
		importance,
		importance,
		personaID,
		factID,
	)
	return err
}

func recordSessionFactWriteTx(ctx context.Context, tx *sql.Tx, personaID string, sessionID *string, factID string) (bool, error) {
	if sessionID == nil || strings.TrimSpace(*sessionID) == "" || strings.TrimSpace(factID) == "" {
		return true, nil
	}
	result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO consolidation_session_fact_writes (
    persona_id, session_id, fact_id
) VALUES (?, ?, ?)`,
		personaID,
		strings.TrimSpace(*sessionID),
		factID,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func invalidateFactTx(ctx context.Context, tx *sql.Tx, personaID string, factID string, validTo time.Time) error {
	_, err := tx.ExecContext(ctx, `
UPDATE facts
SET validity_status = 'invalidated',
    valid_to = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE persona_id = ? AND id = ?`,
		formatTime(validTo),
		personaID,
		factID,
	)
	return err
}

func defaultConsolidationTrigger(trigger string) string {
	if strings.TrimSpace(trigger) == "" {
		return ConsolidationTriggerManual
	}
	return trigger
}

func manualPinSourceException(candidate ManualFactCandidate, policy ConsolidationPolicy) bool {
	return candidate.Pinned && candidate.UserRequested && policy.AllowManualPinWithoutSource
}

func canonicalObjectKey(entityID *string, literal *string) string {
	if entityID != nil && strings.TrimSpace(*entityID) != "" {
		return "entity:" + strings.TrimSpace(*entityID)
	}
	if literal != nil {
		return "literal:" + normalizeLiteral(*literal)
	}
	return ""
}

func normalizeLiteral(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(strings.ToLower(value))), " ")
}

func insertionAction(schema core.PredicateSchema, existingCount int) string {
	if existingCount == 0 {
		return ConsolidationActionInsert
	}
	switch schema.ConflictPolicy {
	case core.ConflictPolicyCoexist:
		return ConsolidationActionCoexist
	case core.ConflictPolicyMerge:
		return ConsolidationActionMergeBaseline
	default:
		return ConsolidationActionInsert
	}
}

func applyExpireByTimePolicy(candidate ManualFactCandidate, schema core.PredicateSchema, sources []sourceEpisode, now time.Time) (ManualFactCandidate, string) {
	if schema.ConflictPolicy != core.ConflictPolicyExpireByTime || candidate.ValidTo != nil {
		return candidate, ""
	}
	if schema.DefaultTauDays == nil {
		return candidate, "expire_by_time predicate has no default_tau_days"
	}
	base := expiryBaseTime(candidate, sources, now)
	validTo := base.Add(time.Duration(*schema.DefaultTauDays * float64(24*time.Hour)))
	candidate.ValidTo = &validTo
	return candidate, ""
}

func expiryBaseTime(candidate ManualFactCandidate, sources []sourceEpisode, now time.Time) time.Time {
	if candidate.ValidFrom != nil && !candidate.ValidFrom.IsZero() {
		return *candidate.ValidFrom
	}
	if len(sources) > 0 && !sources[0].OccurredAt.IsZero() {
		return sources[0].OccurredAt
	}
	return now
}

func statusForAction(action string) string {
	switch action {
	case ConsolidationActionSupersede:
		return ConsolidationStatusSuperseded
	case ConsolidationActionCoexist:
		return ConsolidationStatusCoexisted
	case ConsolidationActionMergeBaseline:
		return ConsolidationStatusInserted
	case ConsolidationActionDuplicateApply, ConsolidationActionReinforceNoop:
		return ConsolidationStatusSkipped
	default:
		return ConsolidationStatusInserted
	}
}

type applyFingerprintRow struct {
	FactID string
	Action string
}

func consolidationApplyFingerprint(personaID string, candidate ManualFactCandidate) string {
	sourceIDs := uniqueSorted(candidate.SourceEpisodeIDs)
	parts := []string{
		personaID,
		strings.TrimSpace(candidate.SubjectEntityID),
		strings.TrimSpace(candidate.Predicate),
		canonicalObjectKey(candidate.ObjectEntityID, candidate.ObjectLiteral),
		strings.TrimSpace(candidate.FactType),
		optionalTimeKey(candidate.ValidFrom),
		optionalTimeKey(candidate.ValidTo),
		strings.Join(sourceIDs, ","),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func findApplyFingerprintTx(ctx context.Context, tx *sql.Tx, personaID string, fingerprint string) (applyFingerprintRow, error) {
	var row applyFingerprintRow
	var factID sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT fact_id, action
FROM consolidation_apply_fingerprints
WHERE persona_id = ? AND fingerprint = ?`, personaID, fingerprint).Scan(&factID, &row.Action)
	if err != nil {
		return applyFingerprintRow{}, err
	}
	if factID.Valid {
		row.FactID = factID.String
	}
	return row, nil
}

func insertApplyFingerprintTx(ctx context.Context, tx *sql.Tx, personaID string, fingerprint string, factID string, action string, req ConsolidateCandidateRequest) error {
	var sessionID any
	if req.SessionID != nil && strings.TrimSpace(*req.SessionID) != "" {
		sessionID = strings.TrimSpace(*req.SessionID)
	}
	_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO consolidation_apply_fingerprints (
    persona_id, fingerprint, fact_id, candidate_id, request_id, session_id, action
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		personaID,
		fingerprint,
		nullableNonEmptyString(factID),
		nullableNonEmptyString(req.CandidateID),
		nullableNonEmptyString(req.RequestID),
		sessionID,
		action,
	)
	return err
}

func factHasAllSourceEvidenceTx(ctx context.Context, tx *sql.Tx, personaID string, factID string, sourceIDs []string) (bool, error) {
	for _, sourceID := range uniqueSorted(sourceIDs) {
		var count int
		err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_links
WHERE persona_id = ?
  AND from_node_type = 'fact'
  AND from_node_id = ?
  AND link_type = 'EVIDENCED_BY'
  AND to_node_type = 'episode'
  AND to_node_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`, personaID, factID, sourceID).Scan(&count)
		if err != nil {
			return false, err
		}
		if count == 0 {
			return false, nil
		}
	}
	return true, nil
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func optionalTimeKey(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func invalidationTimeFor(candidate ManualFactCandidate, sources []sourceEpisode, now time.Time) time.Time {
	if candidate.ValidFrom != nil && !candidate.ValidFrom.IsZero() {
		return *candidate.ValidFrom
	}
	if len(sources) > 0 && !sources[0].OccurredAt.IsZero() {
		return sources[0].OccurredAt
	}
	return now
}

func rejectResult(reason string) ConsolidationResult {
	return ConsolidationResult{
		Action:         ConsolidationActionReject,
		Status:         ConsolidationStatusRejected,
		RejectedReason: reason,
	}
}

func needsReviewResult(reason string) ConsolidationResult {
	return ConsolidationResult{
		Action:            ConsolidationActionNeedsReview,
		Status:            ConsolidationStatusNeedsReview,
		NeedsReviewReason: reason,
	}
}
