package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	CurationModeDryRun = "dry_run"
	CurationModeApply  = "apply"
)

var ErrInvalidCurationRequest = errors.New("invalid curation request")

type CurationRepository struct {
	db    *sql.DB
	newID func() string
	now   func() time.Time
}

type CurationDeltaQuery struct {
	PersonaID        string
	SinceCreatedAt   *time.Time
	SinceFactID      string
	UntilCreatedAt   *time.Time
	UntilFactID      string
	MaxNewFacts      int
	IncludeFactTypes []string
	ExcludeFactTypes []string
}

type CurationComparableQuery struct {
	PersonaID             string
	DeltaFactID           string
	CandidateLimitPerFact int
}

type CurationGroupFact struct {
	FactID           string
	Role             string
	LatestEvidenceAt *time.Time
}

type CurationCandidateGroup struct {
	ID    string
	Facts []CurationGroupFact
}

type CurationDecision struct {
	Decision                 string
	SemanticRelation         string
	AnswerGain               string
	Confidence               float64
	CanonicalFactID          string
	SourceFactIDs            []string
	MergedContentSummary     string
	CanonicalSubjectEntityID string
	CanonicalPredicate       string
	CanonicalObjectLiteral   string
	CanonicalObjectEntityID  string
	CanonicalFactType        string
	ReasonCodes              []string
	LLMResponseHash          string
	RequiresReview           bool
}

type CurationPreparedGroup struct {
	ID       string
	Facts    []CurationGroupFact
	Decision CurationDecision
}

type CurationApplyRequest struct {
	PersonaID              string
	Mode                   string
	Trigger                string
	CursorFromCreatedAt    *time.Time
	CursorFromFactID       string
	CursorToCreatedAt      *time.Time
	CursorToFactID         string
	NewFactCount           int
	ProviderID             string
	ProviderKind           string
	Model                  string
	UsageJSON              string
	Groups                 []CurationPreparedGroup
	MinAutoApplyConfidence float64
	UpdateCheckpoint       bool
}

type CurationApplyResult struct {
	RunID             string
	Status            string
	Mode              string
	NewFactCount      int
	GroupCount        int
	LLMGroupCount     int
	AppliedGroupCount int
	ReviewGroupCount  int
	NoopGroupCount    int
	ErrorCount        int
	GroupStatuses     map[string]string
}

func NewCurationRepository(db *sql.DB, newID func() string, now func() time.Time) *CurationRepository {
	if newID == nil {
		counter := 0
		newID = func() string {
			counter++
			return fmt.Sprintf("curation_id_%d", counter)
		}
	}
	if now == nil {
		now = time.Now
	}
	return &CurationRepository{db: db, newID: newID, now: now}
}

func (r *CurationRepository) LoadDeltaFacts(ctx context.Context, query CurationDeltaQuery) ([]core.Fact, error) {
	personaID := strings.TrimSpace(query.PersonaID)
	if personaID == "" {
		return nil, invalidCuration("persona_id is required")
	}
	limit := query.MaxNewFacts
	if limit <= 0 {
		limit = 100
	}
	sinceCreatedAt := query.SinceCreatedAt
	sinceFactID := strings.TrimSpace(query.SinceFactID)
	if sinceCreatedAt == nil {
		checkpoint, factID, err := loadCurationCheckpoint(ctx, r.db, personaID)
		if err != nil {
			return nil, err
		}
		sinceCreatedAt = checkpoint
		sinceFactID = factID
	}

	includeTypes := normalizedCurationFactTypeSet(query.IncludeFactTypes, defaultCurationIncludeFactTypes())
	excludeTypes := normalizedCurationFactTypeSet(query.ExcludeFactTypes, defaultCurationExcludeFactTypes())
	args := []any{personaID}
	where := []string{
		"persona_id = ?",
		"visibility_status = 'visible'",
		"searchable = 1",
		"validity_status IN ('valid', 'uncertain')",
		"lifecycle_status = 'active'",
		"sensitivity_level != 'highly_sensitive'",
		"extraction_confidence != 'ambiguous'",
	}
	if sinceCreatedAt != nil && !sinceCreatedAt.IsZero() {
		where = append(where, "(created_at > ? OR (created_at = ? AND id > ?))")
		formatted := formatTime(*sinceCreatedAt)
		args = append(args, formatted, formatted, sinceFactID)
	}
	if query.UntilCreatedAt != nil && !query.UntilCreatedAt.IsZero() {
		formatted := formatTime(*query.UntilCreatedAt)
		untilFactID := strings.TrimSpace(query.UntilFactID)
		if untilFactID == "" {
			where = append(where, "created_at <= ?")
			args = append(args, formatted)
		} else {
			where = append(where, "(created_at < ? OR (created_at = ? AND id <= ?))")
			args = append(args, formatted, formatted, untilFactID)
		}
	}
	if len(includeTypes) > 0 {
		where = append(where, "fact_type IN ("+curationPlaceholders(len(includeTypes))+")")
		for _, value := range includeTypes {
			args = append(args, value)
		}
	}
	if len(excludeTypes) > 0 {
		where = append(where, "fact_type NOT IN ("+curationPlaceholders(len(excludeTypes))+")")
		for _, value := range excludeTypes {
			args = append(args, value)
		}
	}
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, `
SELECT id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
       content_summary, fact_type, valid_from, valid_to,
       extraction_confidence, extraction_confidence_score, extraction_reasoning,
       importance, valence, arousal, sensitivity_level,
       validity_status, visibility_status, lifecycle_status,
       pinned, pin_reason, pin_actor, reinforcement_count, searchable, created_at, updated_at
FROM facts
WHERE `+strings.Join(where, "\n  AND ")+`
ORDER BY created_at ASC, id ASC
LIMIT ?`, args...)
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
	return facts, rows.Err()
}

func (r *CurationRepository) RetrieveComparableFacts(ctx context.Context, query CurationComparableQuery) ([]core.Fact, error) {
	personaID := strings.TrimSpace(query.PersonaID)
	if personaID == "" {
		return nil, invalidCuration("persona_id is required")
	}
	limit := query.CandidateLimitPerFact
	if limit <= 0 {
		limit = 20
	}
	delta, err := loadCurationFact(ctx, r.db, personaID, query.DeltaFactID)
	if err != nil {
		return nil, err
	}
	if delta.SubjectEntityID == nil || strings.TrimSpace(*delta.SubjectEntityID) == "" {
		return nil, nil
	}
	predicates := compatibleCurationPredicates(delta.Predicate)
	args := []any{personaID, delta.ID, *delta.SubjectEntityID}
	for _, predicate := range predicates {
		args = append(args, predicate)
	}
	args = append(args, string(delta.FactType), limit)

	rows, err := r.db.QueryContext(ctx, `
SELECT id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
       content_summary, fact_type, valid_from, valid_to,
       extraction_confidence, extraction_confidence_score, extraction_reasoning,
       importance, valence, arousal, sensitivity_level,
       validity_status, visibility_status, lifecycle_status,
       pinned, pin_reason, pin_actor, reinforcement_count, searchable, created_at, updated_at
FROM facts
WHERE persona_id = ?
  AND id != ?
  AND subject_entity_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1
  AND validity_status IN ('valid', 'uncertain')
  AND lifecycle_status IN ('active', 'dormant')
  AND sensitivity_level != 'highly_sensitive'
  AND extraction_confidence != 'ambiguous'
  AND fact_type = ?
  AND predicate IN (`+curationPlaceholders(len(predicates))+`)
ORDER BY updated_at DESC, created_at DESC, id ASC
LIMIT ?`, reorderComparableArgs(args, len(predicates))...)
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
	return facts, rows.Err()
}

func (r *CurationRepository) BuildGroups(deltaFacts []core.Fact, candidates map[string][]core.Fact, maxFactsPerGroup int) []CurationCandidateGroup {
	if maxFactsPerGroup <= 0 {
		maxFactsPerGroup = 8
	}
	parent := map[string]string{}
	factsByID := map[string]core.Fact{}
	deltaSet := map[string]struct{}{}
	var order []string
	addFact := func(fact core.Fact) {
		if strings.TrimSpace(fact.ID) == "" {
			return
		}
		if _, ok := factsByID[fact.ID]; !ok {
			order = append(order, fact.ID)
			parent[fact.ID] = fact.ID
		}
		factsByID[fact.ID] = fact
	}
	var find func(string) string
	find = func(id string) string {
		if parent[id] != id {
			parent[id] = find(parent[id])
		}
		return parent[id]
	}
	union := func(a, b string) {
		if _, ok := parent[a]; !ok {
			return
		}
		if _, ok := parent[b]; !ok {
			return
		}
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for _, fact := range deltaFacts {
		addFact(fact)
		deltaSet[fact.ID] = struct{}{}
	}
	for deltaID, comparable := range candidates {
		for _, fact := range comparable {
			addFact(fact)
			if curationFactsComparable(factsByID[deltaID], fact) {
				union(deltaID, fact.ID)
			}
		}
	}
	grouped := map[string][]string{}
	for _, id := range order {
		grouped[find(id)] = append(grouped[find(id)], id)
	}
	roots := make([]string, 0, len(grouped))
	for root, ids := range grouped {
		if len(ids) > 1 {
			roots = append(roots, root)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		return earliestFactTime(grouped[roots[i]], factsByID).Before(earliestFactTime(grouped[roots[j]], factsByID))
	})
	result := make([]CurationCandidateGroup, 0, len(roots))
	for _, root := range roots {
		ids := grouped[root]
		sort.Slice(ids, func(i, j int) bool {
			left, right := factsByID[ids[i]], factsByID[ids[j]]
			if left.CreatedAt.Equal(right.CreatedAt) {
				return left.ID < right.ID
			}
			return left.CreatedAt.Before(right.CreatedAt)
		})
		if len(ids) > maxFactsPerGroup {
			ids = ids[:maxFactsPerGroup]
		}
		group := CurationCandidateGroup{ID: r.newID(), Facts: make([]CurationGroupFact, 0, len(ids))}
		for _, id := range ids {
			role := "existing_candidate"
			if _, ok := deltaSet[id]; ok {
				role = "new_delta"
			}
			group.Facts = append(group.Facts, CurationGroupFact{FactID: id, Role: role})
		}
		result = append(result, group)
	}
	return result
}

func (r *CurationRepository) ApplyDecisions(ctx context.Context, req CurationApplyRequest) (result CurationApplyResult, err error) {
	req, err = normalizeCurationApplyRequest(req)
	if err != nil {
		return CurationApplyResult{}, err
	}
	runID := r.newID()
	result = CurationApplyResult{
		RunID:         runID,
		Status:        "succeeded",
		Mode:          req.Mode,
		NewFactCount:  req.NewFactCount,
		GroupCount:    len(req.Groups),
		LLMGroupCount: len(req.Groups),
		GroupStatuses: map[string]string{},
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CurationApplyResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err = insertCurationRunTx(ctx, tx, runID, req, "running", result); err != nil {
		return CurationApplyResult{}, err
	}
	for _, group := range req.Groups {
		groupStatus := curationGroupStatus(req, group)
		if groupStatus == "applied" {
			var preflightErr error
			group, groupStatus, preflightErr = preflightCurationAutoApplyTx(ctx, tx, req.PersonaID, group)
			if preflightErr != nil {
				return CurationApplyResult{}, preflightErr
			}
		}
		switch groupStatus {
		case "applied":
			if err = applyCurationGroupTx(ctx, tx, r, req.PersonaID, group); err != nil {
				return CurationApplyResult{}, err
			}
			result.AppliedGroupCount++
		case "needs_review", "failed":
			result.ReviewGroupCount++
		default:
			result.NoopGroupCount++
		}
		if err = insertCurationGroupTx(ctx, tx, r, runID, req.PersonaID, group, groupStatus); err != nil {
			return CurationApplyResult{}, err
		}
		result.GroupStatuses[group.ID] = groupStatus
	}
	if result.ErrorCount > 0 {
		result.Status = "partially_failed"
	}
	if len(req.Groups) == 0 && req.NewFactCount == 0 {
		result.Status = "skipped"
	}
	if err = updateCurationRunFinishedTx(ctx, tx, runID, result, r.now()); err != nil {
		return CurationApplyResult{}, err
	}
	if req.Mode == CurationModeApply && req.UpdateCheckpoint && result.Status == "succeeded" && result.ReviewGroupCount == 0 && req.CursorToCreatedAt != nil {
		if err = upsertCurationCheckpointTx(ctx, tx, req.PersonaID, runID, *req.CursorToCreatedAt, req.CursorToFactID, r.now()); err != nil {
			return CurationApplyResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return CurationApplyResult{}, err
	}
	committed = true
	return result, nil
}

func normalizeCurationApplyRequest(req CurationApplyRequest) (CurationApplyRequest, error) {
	req.PersonaID = strings.TrimSpace(req.PersonaID)
	if req.PersonaID == "" {
		return CurationApplyRequest{}, invalidCuration("persona_id is required")
	}
	switch req.Mode {
	case CurationModeDryRun, CurationModeApply:
	default:
		return CurationApplyRequest{}, invalidCuration("mode must be dry_run or apply")
	}
	if strings.TrimSpace(req.Trigger) == "" {
		req.Trigger = "manual"
	}
	switch req.Trigger {
	case "manual", "scheduled", "cli", "test":
	default:
		return CurationApplyRequest{}, invalidCuration("trigger must be manual, scheduled, cli, or test")
	}
	if req.MinAutoApplyConfidence <= 0 {
		req.MinAutoApplyConfidence = 0.88
	}
	return req, nil
}

func curationGroupStatus(req CurationApplyRequest, group CurationPreparedGroup) string {
	if req.Mode == CurationModeDryRun {
		return "noop"
	}
	if isCurationAutoApplyAllowed(req, group.Decision) {
		return "applied"
	}
	switch group.Decision.Decision {
	case "needs_review", "conflict_needs_review":
		return "needs_review"
	default:
		return "noop"
	}
}

func isCurationAutoApplyAllowed(req CurationApplyRequest, decision CurationDecision) bool {
	switch decision.Decision {
	case "reinforce_existing", "merge_into_existing", "create_canonical_fact":
	default:
		return false
	}
	if decision.RequiresReview {
		return false
	}
	switch decision.SemanticRelation {
	case "same", "refinement":
	default:
		return false
	}
	switch decision.AnswerGain {
	case "none", "small":
	default:
		return false
	}
	return decision.Confidence >= req.MinAutoApplyConfidence
}

func preflightCurationAutoApplyTx(ctx context.Context, tx *sql.Tx, personaID string, group CurationPreparedGroup) (CurationPreparedGroup, string, error) {
	reason, err := curationAutoApplyBlockReasonTx(ctx, tx, personaID, group.Decision)
	if err != nil {
		return group, "", err
	}
	if reason == "" {
		return group, "applied", nil
	}
	group.Decision.RequiresReview = true
	group.Decision.ReasonCodes = appendUniqueString(group.Decision.ReasonCodes, reason)
	return group, "needs_review", nil
}

func curationAutoApplyBlockReasonTx(ctx context.Context, tx *sql.Tx, personaID string, decision CurationDecision) (string, error) {
	sourceIDs := uniqueStrings(decision.SourceFactIDs)
	if len(sourceIDs) < 2 {
		return "invalid_source_fact_ids", nil
	}
	canonicalID := strings.TrimSpace(decision.CanonicalFactID)
	if decision.Decision != "create_canonical_fact" && canonicalID == "" {
		return "missing_canonical_fact", nil
	}
	if decision.Decision != "create_canonical_fact" {
		if _, err := getFactTx(ctx, tx, personaID, canonicalID); err != nil {
			return "", err
		}
	}
	for _, sourceID := range sourceIDs {
		source, err := getFactTx(ctx, tx, personaID, sourceID)
		if err != nil {
			return "", err
		}
		if source.ID == canonicalID {
			continue
		}
		if reason := curationAutoApplySourceBlockReason(source); reason != "" {
			return reason, nil
		}
	}
	return "", nil
}

func applyCurationGroupTx(ctx context.Context, tx *sql.Tx, r *CurationRepository, personaID string, group CurationPreparedGroup) error {
	decision := group.Decision
	sourceIDs := uniqueStrings(decision.SourceFactIDs)
	if len(sourceIDs) < 2 {
		return invalidCuration("group %s source_fact_ids must contain at least two ids", group.ID)
	}
	if decision.Decision == "create_canonical_fact" {
		return applyCreateCanonicalFactTx(ctx, tx, r, personaID, group)
	}
	canonicalID := strings.TrimSpace(decision.CanonicalFactID)
	if canonicalID == "" {
		return invalidCuration("group %s canonical_fact_id is required", group.ID)
	}
	if _, err := getFactTx(ctx, tx, personaID, canonicalID); err != nil {
		return err
	}
	if err := validateCurationSourcesForAutoApply(ctx, tx, personaID, canonicalID, sourceIDs); err != nil {
		return err
	}
	if decision.Decision == "reinforce_existing" {
		if err := reinforceFactTx(ctx, tx, personaID, canonicalID, 0); err != nil {
			return err
		}
	} else {
		if err := updateCanonicalFactTx(ctx, tx, personaID, canonicalID, decision, sourceIDs); err != nil {
			return err
		}
	}
	linkIDs, err := copyCurationEvidenceTx(ctx, tx, r, personaID, canonicalID, sourceIDs)
	if err != nil {
		return err
	}
	derivedIDs, err := writeCurationDerivedLinksTx(ctx, tx, r, personaID, canonicalID, sourceIDs)
	if err != nil {
		return err
	}
	linkIDs = append(linkIDs, derivedIDs...)
	for _, sourceID := range sourceIDs {
		if sourceID == canonicalID {
			continue
		}
		if err := consolidateCurationSourceTx(ctx, tx, personaID, sourceID, r.now()); err != nil {
			return err
		}
		if err := deleteSearchDocument(ctx, tx, personaID, core.NodeTypeFact, sourceID); err != nil {
			return err
		}
		if err := enqueueCurationIndexSyncTx(ctx, tx, r.newID(), personaID, string(core.NodeTypeFact), sourceID, "delete_node"); err != nil {
			return err
		}
	}
	if err := upsertFactSearchDocumentTx(ctx, tx, personaID, canonicalID); err != nil {
		return err
	}
	if err := enqueueCurationIndexSyncTx(ctx, tx, r.newID(), personaID, string(core.NodeTypeFact), canonicalID, "upsert_node"); err != nil {
		return err
	}
	for _, linkID := range linkIDs {
		if err := enqueueCurationIndexSyncTx(ctx, tx, r.newID(), personaID, "memory_link", linkID, "upsert_edge"); err != nil {
			return err
		}
	}
	return nil
}

func applyCreateCanonicalFactTx(ctx context.Context, tx *sql.Tx, r *CurationRepository, personaID string, group CurationPreparedGroup) error {
	decision := group.Decision
	sourceIDs := uniqueStrings(decision.SourceFactIDs)
	template, err := getFactTx(ctx, tx, personaID, sourceIDs[0])
	if err != nil {
		return err
	}
	canonicalID := strings.TrimSpace(decision.CanonicalFactID)
	if canonicalID == "" || containsStringValue(sourceIDs, canonicalID) {
		canonicalID = r.newID()
	}
	if err := validateCurationSourcesForAutoApply(ctx, tx, personaID, "", sourceIDs); err != nil {
		return err
	}
	canonical := template
	canonical.ID = canonicalID
	canonical.ContentSummary = strings.TrimSpace(decision.MergedContentSummary)
	if canonical.ContentSummary == "" {
		canonical.ContentSummary = template.ContentSummary
	}
	if strings.TrimSpace(decision.CanonicalSubjectEntityID) != "" {
		canonical.SubjectEntityID = ptrString(decision.CanonicalSubjectEntityID)
	}
	if strings.TrimSpace(decision.CanonicalPredicate) != "" {
		canonical.Predicate = strings.TrimSpace(decision.CanonicalPredicate)
	}
	if strings.TrimSpace(decision.CanonicalObjectLiteral) != "" {
		canonical.ObjectLiteral = ptrString(decision.CanonicalObjectLiteral)
		canonical.ObjectEntityID = nil
	}
	if strings.TrimSpace(decision.CanonicalObjectEntityID) != "" {
		canonical.ObjectEntityID = ptrString(decision.CanonicalObjectEntityID)
		canonical.ObjectLiteral = nil
	}
	if strings.TrimSpace(decision.CanonicalFactType) != "" {
		canonical.FactType = core.FactType(decision.CanonicalFactType)
	}
	canonical.LifecycleStatus = core.LifecycleActive
	canonical.VisibilityStatus = core.VisibilityVisible
	canonical.ValidityStatus = core.ValidityValid
	canonical.Searchable = true
	canonical.Pinned = false
	canonical.PinActor = nil
	canonical.PinReason = nil
	canonical.ReinforcementCount = len(sourceIDs)
	canonical.CreatedAt = time.Time{}
	canonical.UpdatedAt = nil
	if err := insertFactTx(ctx, tx, canonical); err != nil {
		return err
	}
	if err := copyAllCurationEvidenceTx(ctx, tx, r, personaID, canonicalID, sourceIDs); err != nil {
		return err
	}
	derivedIDs, err := writeCurationDerivedLinksTx(ctx, tx, r, personaID, canonicalID, sourceIDs)
	if err != nil {
		return err
	}
	for _, sourceID := range sourceIDs {
		if err := consolidateCurationSourceTx(ctx, tx, personaID, sourceID, r.now()); err != nil {
			return err
		}
		if err := deleteSearchDocument(ctx, tx, personaID, core.NodeTypeFact, sourceID); err != nil {
			return err
		}
		if err := enqueueCurationIndexSyncTx(ctx, tx, r.newID(), personaID, string(core.NodeTypeFact), sourceID, "delete_node"); err != nil {
			return err
		}
	}
	if err := upsertFactSearchDocumentTx(ctx, tx, personaID, canonicalID); err != nil {
		return err
	}
	if err := enqueueCurationIndexSyncTx(ctx, tx, r.newID(), personaID, string(core.NodeTypeFact), canonicalID, "upsert_node"); err != nil {
		return err
	}
	for _, linkID := range derivedIDs {
		if err := enqueueCurationIndexSyncTx(ctx, tx, r.newID(), personaID, "memory_link", linkID, "upsert_edge"); err != nil {
			return err
		}
	}
	return nil
}

func updateCanonicalFactTx(ctx context.Context, tx *sql.Tx, personaID string, canonicalID string, decision CurationDecision, sourceIDs []string) error {
	summary := strings.TrimSpace(decision.MergedContentSummary)
	objectLiteral := strings.TrimSpace(decision.CanonicalObjectLiteral)
	predicate := strings.TrimSpace(decision.CanonicalPredicate)
	factType := strings.TrimSpace(decision.CanonicalFactType)
	if summary == "" {
		summary = decision.MergedContentSummary
	}
	_, err := tx.ExecContext(ctx, `
UPDATE facts
SET content_summary = CASE WHEN ? != '' THEN ? ELSE content_summary END,
    object_literal = CASE WHEN ? != '' THEN ? ELSE object_literal END,
    predicate = CASE WHEN ? != '' THEN ? ELSE predicate END,
    fact_type = CASE WHEN ? != '' THEN ? ELSE fact_type END,
    importance = (
        SELECT MAX(importance)
        FROM facts
        WHERE persona_id = ? AND id IN (`+curationPlaceholders(len(sourceIDs))+`)
    ),
    reinforcement_count = reinforcement_count + ?,
    lifecycle_status = 'active',
    searchable = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE persona_id = ? AND id = ?`,
		append([]any{summary, summary, objectLiteral, objectLiteral, predicate, predicate, factType, factType, personaID}, appendStringsAsAny(sourceIDs, len(sourceIDs)-1, personaID, canonicalID)...)...)
	return err
}

func appendStringsAsAny(values []string, extra ...any) []any {
	args := make([]any, 0, len(values)+len(extra))
	for _, value := range values {
		args = append(args, value)
	}
	args = append(args, extra...)
	return args
}

func validateCurationSourcesForAutoApply(ctx context.Context, tx *sql.Tx, personaID string, canonicalID string, sourceIDs []string) error {
	for _, sourceID := range sourceIDs {
		source, err := getFactTx(ctx, tx, personaID, sourceID)
		if err != nil {
			return err
		}
		if sourceID == canonicalID {
			continue
		}
		if reason := curationAutoApplySourceBlockReason(source); reason != "" {
			return invalidCuration("source fact %s requires review: %s", sourceID, reason)
		}
	}
	return nil
}

func curationAutoApplySourceBlockReason(source core.Fact) string {
	if source.VisibilityStatus != core.VisibilityVisible {
		return "visibility_status=" + string(source.VisibilityStatus)
	}
	if !source.Searchable {
		return "searchable=0"
	}
	if source.ValidityStatus == core.ValidityInvalidated {
		return "validity_status=invalidated"
	}
	if source.LifecycleStatus != core.LifecycleActive && source.LifecycleStatus != core.LifecycleDormant {
		return "lifecycle_status=" + string(source.LifecycleStatus)
	}
	if source.Pinned {
		return "pinned=1"
	}
	switch source.FactType {
	case core.FactTypeCoreIdentity, core.FactTypeCommitment:
		return "fact_type=" + string(source.FactType)
	}
	if source.ExtractionConfidence == core.ExtractionConfidenceAmbiguous {
		return "extraction_confidence=ambiguous"
	}
	if source.SensitivityLevel == core.SensitivityHighlySensitive {
		return "sensitivity_level=highly_sensitive"
	}
	return ""
}

func copyCurationEvidenceTx(ctx context.Context, tx *sql.Tx, r *CurationRepository, personaID string, canonicalID string, sourceIDs []string) ([]string, error) {
	var created []string
	for _, sourceID := range sourceIDs {
		if sourceID == canonicalID {
			continue
		}
		linkIDs, err := copyEvidenceFromSourceTx(ctx, tx, r, personaID, canonicalID, sourceID)
		if err != nil {
			return nil, err
		}
		created = append(created, linkIDs...)
	}
	return created, nil
}

func copyAllCurationEvidenceTx(ctx context.Context, tx *sql.Tx, r *CurationRepository, personaID string, canonicalID string, sourceIDs []string) error {
	for _, sourceID := range sourceIDs {
		if _, err := copyEvidenceFromSourceTx(ctx, tx, r, personaID, canonicalID, sourceID); err != nil {
			return err
		}
	}
	return nil
}

func copyEvidenceFromSourceTx(ctx context.Context, tx *sql.Tx, r *CurationRepository, personaID string, canonicalID string, sourceID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT to_node_id
FROM memory_links
WHERE persona_id = ?
  AND from_node_type = 'fact'
  AND from_node_id = ?
  AND link_type = 'EVIDENCED_BY'
  AND to_node_type = 'episode'
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY created_at ASC`, personaID, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var created []string
	for rows.Next() {
		var episodeID string
		if err := rows.Scan(&episodeID); err != nil {
			return nil, err
		}
		linkID, wasCreated, err := ensureCurationLinkTx(ctx, tx, r, core.MemoryLink{
			ID:           r.newID(),
			PersonaID:    personaID,
			FromNodeType: core.NodeTypeFact,
			FromNodeID:   canonicalID,
			LinkType:     core.LinkTypeEvidencedBy,
			ToNodeType:   core.NodeTypeEpisode,
			ToNodeID:     episodeID,
			CreatedBy:    core.LinkCreatedByConsolidation,
		})
		if err != nil {
			return nil, err
		}
		if wasCreated {
			created = append(created, linkID)
		}
	}
	return created, rows.Err()
}

func writeCurationDerivedLinksTx(ctx context.Context, tx *sql.Tx, r *CurationRepository, personaID string, canonicalID string, sourceIDs []string) ([]string, error) {
	var created []string
	for _, sourceID := range sourceIDs {
		if sourceID == canonicalID {
			continue
		}
		linkID, wasCreated, err := ensureCurationLinkTx(ctx, tx, r, core.MemoryLink{
			ID:           r.newID(),
			PersonaID:    personaID,
			FromNodeType: core.NodeTypeFact,
			FromNodeID:   canonicalID,
			LinkType:     core.LinkTypeDerivedFrom,
			ToNodeType:   core.NodeTypeFact,
			ToNodeID:     sourceID,
			CreatedBy:    core.LinkCreatedByConsolidation,
		})
		if err != nil {
			return nil, err
		}
		if wasCreated {
			created = append(created, linkID)
		}
	}
	return created, nil
}

func ensureCurationLinkTx(ctx context.Context, tx *sql.Tx, r *CurationRepository, link core.MemoryLink) (string, bool, error) {
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
		link.PersonaID, string(link.FromNodeType), link.FromNodeID, string(link.LinkType), string(link.ToNodeType), link.ToNodeID).Scan(&existingID)
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
		link.ID, link.PersonaID, string(link.FromNodeType), link.FromNodeID, string(link.LinkType),
		string(link.ToNodeType), link.ToNodeID, string(link.Direction), link.Confidence, link.Weight,
		nullableString(link.Reasoning), string(link.CreatedBy), string(link.VisibilityStatus), boolInt(link.Searchable))
	if err != nil {
		return "", false, err
	}
	return link.ID, true, nil
}

func consolidateCurationSourceTx(ctx context.Context, tx *sql.Tx, personaID string, sourceID string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `
UPDATE facts
SET lifecycle_status = 'consolidated',
    searchable = 0,
    updated_at = ?
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`, formatTime(now), personaID, sourceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return invalidCuration("source fact %s is no longer eligible for curation", sourceID)
	}
	return nil
}

func insertCurationRunTx(ctx context.Context, tx *sql.Tx, runID string, req CurationApplyRequest, status string, result CurationApplyResult) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO memory_curation_runs (
    id, persona_id, mode, trigger, status,
    cursor_from_created_at, cursor_from_fact_id, cursor_to_created_at, cursor_to_fact_id,
    new_fact_count, group_count, llm_group_count, applied_group_count, review_group_count,
    noop_group_count, error_count, provider_id, provider_kind, model, usage_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		runID, req.PersonaID, req.Mode, req.Trigger, status,
		nullableTime(req.CursorFromCreatedAt), nullableTrimmed(req.CursorFromFactID),
		nullableTime(req.CursorToCreatedAt), nullableTrimmed(req.CursorToFactID),
		result.NewFactCount, result.GroupCount, result.LLMGroupCount, result.AppliedGroupCount,
		result.ReviewGroupCount, result.NoopGroupCount, result.ErrorCount,
		nullableTrimmed(req.ProviderID), nullableTrimmed(req.ProviderKind), nullableTrimmed(req.Model), nullableTrimmed(req.UsageJSON))
	return err
}

func updateCurationRunFinishedTx(ctx context.Context, tx *sql.Tx, runID string, result CurationApplyResult, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
UPDATE memory_curation_runs
SET status = ?,
    applied_group_count = ?,
    review_group_count = ?,
    noop_group_count = ?,
    error_count = ?,
    finished_at = ?
WHERE id = ?`,
		result.Status, result.AppliedGroupCount, result.ReviewGroupCount, result.NoopGroupCount, result.ErrorCount, formatTime(now), runID)
	return err
}

func insertCurationGroupTx(ctx context.Context, tx *sql.Tx, r *CurationRepository, runID string, personaID string, group CurationPreparedGroup, status string) error {
	decision := withDefaultCurationDecision(group.Decision)
	reasonCodesJSON, _ := json.Marshal(decision.ReasonCodes)
	_, err := tx.ExecContext(ctx, `
INSERT INTO memory_curation_groups (
    id, run_id, persona_id, group_status, decision, semantic_relation, answer_gain,
    confidence, canonical_fact_id, merged_content_summary, canonical_subject_entity_id,
    canonical_predicate, canonical_object_literal, canonical_object_entity_id, fact_type,
    reason_codes_json, llm_response_hash, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		group.ID, runID, personaID, status, decision.Decision, decision.SemanticRelation, decision.AnswerGain,
		decision.Confidence, nullableTrimmed(decision.CanonicalFactID), nullableTrimmed(decision.MergedContentSummary),
		nullableTrimmed(decision.CanonicalSubjectEntityID), nullableTrimmed(decision.CanonicalPredicate),
		nullableTrimmed(decision.CanonicalObjectLiteral), nullableTrimmed(decision.CanonicalObjectEntityID),
		nullableTrimmed(decision.CanonicalFactType), string(reasonCodesJSON), nullableTrimmed(decision.LLMResponseHash),
		formatTime(r.now()))
	if err != nil {
		return err
	}
	for _, fact := range group.Facts {
		if err := insertCurationGroupFactTx(ctx, tx, r.newID(), group.ID, personaID, fact); err != nil {
			return err
		}
	}
	if decision.CanonicalFactID != "" {
		if err := insertCurationGroupFactTx(ctx, tx, r.newID(), group.ID, personaID, CurationGroupFact{FactID: decision.CanonicalFactID, Role: "canonical"}); err != nil {
			return err
		}
	}
	for _, sourceID := range decision.SourceFactIDs {
		if sourceID == decision.CanonicalFactID {
			continue
		}
		if err := insertCurationGroupFactTx(ctx, tx, r.newID(), group.ID, personaID, CurationGroupFact{FactID: sourceID, Role: "source"}); err != nil {
			return err
		}
	}
	return nil
}

func insertCurationGroupFactTx(ctx context.Context, tx *sql.Tx, id string, groupID string, personaID string, fact CurationGroupFact) error {
	if strings.TrimSpace(fact.FactID) == "" || strings.TrimSpace(fact.Role) == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO memory_curation_group_facts (
    id, group_id, persona_id, fact_id, role, latest_evidence_at
) VALUES (?, ?, ?, ?, ?, ?)`,
		id, groupID, personaID, fact.FactID, fact.Role, nullableTime(fact.LatestEvidenceAt))
	return err
}

func upsertCurationCheckpointTx(ctx context.Context, tx *sql.Tx, personaID string, runID string, cursorCreatedAt time.Time, cursorFactID string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO memory_curation_checkpoints (
    persona_id, last_successful_run_id, cursor_created_at, cursor_fact_id, updated_at
) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(persona_id) DO UPDATE SET
    last_successful_run_id = excluded.last_successful_run_id,
    cursor_created_at = excluded.cursor_created_at,
    cursor_fact_id = excluded.cursor_fact_id,
    updated_at = excluded.updated_at`,
		personaID, runID, formatTime(cursorCreatedAt), cursorFactID, formatTime(now))
	return err
}

func loadCurationCheckpoint(ctx context.Context, runner queryer, personaID string) (*time.Time, string, error) {
	var createdAt, factID sql.NullString
	err := runner.QueryRowContext(ctx, `
SELECT cursor_created_at, cursor_fact_id
FROM memory_curation_checkpoints
WHERE persona_id = ?`, personaID).Scan(&createdAt, &factID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if !createdAt.Valid {
		return nil, stringPtrValue(factID), nil
	}
	parsed := parseTime(createdAt.String)
	return &parsed, stringPtrValue(factID), nil
}

func loadCurationFact(ctx context.Context, runner queryer, personaID string, factID string) (core.Fact, error) {
	return scanFact(runner.QueryRowContext(ctx, `
SELECT id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
       content_summary, fact_type, valid_from, valid_to,
       extraction_confidence, extraction_confidence_score, extraction_reasoning,
       importance, valence, arousal, sensitivity_level,
       validity_status, visibility_status, lifecycle_status,
       pinned, pin_reason, pin_actor, reinforcement_count, searchable, created_at, updated_at
FROM facts
WHERE persona_id = ? AND id = ?`, personaID, factID))
}

func withDefaultCurationDecision(decision CurationDecision) CurationDecision {
	if decision.Decision == "" {
		decision.Decision = "no_op"
	}
	if decision.SemanticRelation == "" {
		decision.SemanticRelation = "unclear"
	}
	if decision.AnswerGain == "" {
		decision.AnswerGain = "unknown"
	}
	return decision
}

func defaultCurationIncludeFactTypes() []string {
	return []string{
		string(core.FactTypeStablePreference),
		string(core.FactTypeRelationalState),
		string(core.FactTypeTransientContext),
		string(core.FactTypeTaskRelevantContext),
	}
}

func defaultCurationExcludeFactTypes() []string {
	return []string{string(core.FactTypeCoreIdentity), string(core.FactTypeCommitment)}
}

func normalizedCurationFactTypeSet(values []string, fallback []string) []string {
	if len(values) == 0 {
		values = fallback
	}
	seen := map[string]struct{}{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func compatibleCurationPredicates(predicate string) []string {
	switch predicate {
	case "likes":
		return []string{"likes"}
	case "dislikes":
		return []string{"dislikes"}
	case "prefers_communication_style":
		return []string{"prefers_communication_style"}
	case "has_boundary":
		return []string{"has_boundary"}
	case "uses_coping_strategy":
		return []string{"uses_coping_strategy"}
	default:
		return []string{predicate}
	}
}

func curationFactsComparable(left core.Fact, right core.Fact) bool {
	if strings.TrimSpace(left.ID) == "" || strings.TrimSpace(right.ID) == "" {
		return false
	}
	if left.SubjectEntityID == nil || right.SubjectEntityID == nil || strings.TrimSpace(*left.SubjectEntityID) != strings.TrimSpace(*right.SubjectEntityID) {
		return false
	}
	if left.Predicate != right.Predicate || left.FactType != right.FactType {
		return false
	}
	leftObject := normalizeCurationComparableText(derefStringPtr(left.ObjectLiteral))
	rightObject := normalizeCurationComparableText(derefStringPtr(right.ObjectLiteral))
	if leftObject != "" && leftObject == rightObject {
		return true
	}
	leftTags := curationComparableTags(left)
	rightTags := curationComparableTags(right)
	for tag := range leftTags {
		if _, ok := rightTags[tag]; ok {
			return true
		}
	}
	leftTerms := curationComparableTerms(left)
	rightTerms := curationComparableTerms(right)
	overlap := 0
	for term := range leftTerms {
		if _, ok := rightTerms[term]; ok {
			overlap++
			if overlap >= 2 {
				return true
			}
		}
	}
	return false
}

func curationComparableTags(fact core.Fact) map[string]struct{} {
	text := curationFactComparableText(fact)
	tags := map[string]struct{}{}
	if strings.Contains(text, "无糖") || strings.Contains(text, "没有糖") || strings.Contains(text, "不加糖") {
		tags["no_sugar"] = struct{}{}
	}
	if strings.Contains(text, "不甜") || strings.Contains(text, "低甜") || strings.Contains(text, "少甜") {
		tags["low_sweet"] = struct{}{}
	}
	if strings.Contains(text, "代糖") {
		tags["sweetener"] = struct{}{}
	}
	return tags
}

func curationComparableTerms(fact core.Fact) map[string]struct{} {
	text := curationFactComparableText(fact)
	terms := map[string]struct{}{}
	for _, term := range curationCJKBigrams(text) {
		if _, generic := genericCurationComparableTerms[term]; generic {
			continue
		}
		terms[term] = struct{}{}
	}
	return terms
}

func curationFactComparableText(fact core.Fact) string {
	return normalizeCurationComparableText(fact.ContentSummary + " " + derefStringPtr(fact.ObjectLiteral))
}

func normalizeCurationComparableText(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(strings.ToLower(value)) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || r == '，' || r == '。' || r == '、' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func curationCJKBigrams(value string) []string {
	var runes []rune
	flush := func(out *[]string) {
		for i := 0; i+1 < len(runes); i++ {
			*out = append(*out, string(runes[i:i+2]))
		}
		runes = nil
	}
	var terms []string
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			runes = append(runes, r)
			continue
		}
		flush(&terms)
	}
	flush(&terms)
	return terms
}

var genericCurationComparableTerms = map[string]struct{}{
	"用户": {},
	"户喜": {},
	"喜欢": {},
	"欢喝": {},
	"不喜": {},
	"讨厌": {},
	"偏好": {},
	"饮料": {},
	"口味": {},
	"东西": {},
	"内容": {},
	"记忆": {},
	"的饮": {},
	"做的": {},
}

func derefStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func reorderComparableArgs(args []any, predicateCount int) []any {
	base := args[:3]
	predicates := args[3 : 3+predicateCount]
	factTypeAndLimit := args[3+predicateCount:]
	out := make([]any, 0, len(args))
	out = append(out, base...)
	out = append(out, factTypeAndLimit[0])
	out = append(out, predicates...)
	out = append(out, factTypeAndLimit[1])
	return out
}

func earliestFactTime(ids []string, facts map[string]core.Fact) time.Time {
	var earliest time.Time
	for _, id := range ids {
		created := facts[id].CreatedAt
		if earliest.IsZero() || created.Before(earliest) {
			earliest = created
		}
	}
	return earliest
}

func curationPlaceholders(count int) string {
	if count <= 0 {
		return "NULL"
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func containsStringValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ptrString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func nullableTrimmed(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func enqueueCurationIndexSyncTx(ctx context.Context, tx *sql.Tx, id string, personaID string, nodeType string, nodeID string, operation string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (?, ?, ?, ?, ?)`, id, personaID, nodeType, nodeID, operation)
	return err
}

func invalidCuration(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCurationRequest, fmt.Sprintf(format, args...))
}
