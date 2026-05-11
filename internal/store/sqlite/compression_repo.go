package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

var ErrInvalidCompressionRequest = errors.New("invalid compression request")

type CompressionRepository struct {
	db    *sql.DB
	newID func() string
	now   func() time.Time
}

type CompressionRequest struct {
	PersonaID     string
	SourceFactIDs []string
	Narrative     *NarrativeDraft
	Insights      []InsightDraft
	Now           time.Time
	DryRun        bool
}

type NarrativeDraft struct {
	ID               string
	Scope            string
	ScopeRef         string
	Summary          string
	EmotionalTone    string
	ValenceAvg       *float64
	ArousalAvg       *float64
	Importance       float64
	ValidFrom        *time.Time
	ValidTo          *time.Time
	SensitivityLevel string
}

type InsightDraft struct {
	ID               string
	InsightType      string
	Content          string
	Confidence       float64
	Importance       float64
	Valence          float64
	Arousal          float64
	SensitivityLevel string
}

type CompressionResult struct {
	NarrativeID             string
	InsightIDs              []string
	SourceFactsConsolidated int
	DerivedLinkIDs          []string
	SearchDocumentsSynced   int
	MirrorUpdatesEnqueued   int
	DryRun                  bool
}

type compressionSourceFact struct {
	ID                   string
	VisibilityStatus     string
	ValidityStatus       string
	Searchable           bool
	LifecycleStatus      string
	Pinned               bool
	FactType             string
	ExtractionConfidence string
	SensitivityLevel     string
}

func NewCompressionRepository(db *sql.DB, newID func() string, now func() time.Time) *CompressionRepository {
	if newID == nil {
		counter := 0
		newID = func() string {
			counter++
			return fmt.Sprintf("compression_id_%d", counter)
		}
	}
	if now == nil {
		now = time.Now
	}
	return &CompressionRepository{db: db, newID: newID, now: now}
}

func (r *CompressionRepository) Apply(ctx context.Context, req CompressionRequest) (CompressionResult, error) {
	prepared, result, err := r.prepare(req)
	if err != nil {
		return CompressionResult{}, err
	}
	if prepared.DryRun {
		if _, err := validateCompressionSources(ctx, r.db, prepared.PersonaID, prepared.SourceFactIDs); err != nil {
			return CompressionResult{}, err
		}
		mapped, err := countMappedCompressionSources(ctx, r.db, prepared.PersonaID, prepared.SourceFactIDs)
		if err != nil {
			return CompressionResult{}, err
		}
		result.MirrorUpdatesEnqueued = mapped
		return result, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CompressionResult{}, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = validateCompressionSources(ctx, tx, prepared.PersonaID, prepared.SourceFactIDs); err != nil {
		return CompressionResult{}, err
	}
	if prepared.Narrative != nil {
		if err = insertNarrativeTx(ctx, tx, prepared.PersonaID, *prepared.Narrative, prepared.Now); err != nil {
			return CompressionResult{}, err
		}
	}
	for _, insight := range prepared.Insights {
		if err = insertInsightTx(ctx, tx, prepared.PersonaID, insight, prepared.Now); err != nil {
			return CompressionResult{}, err
		}
	}
	if err = insertDerivedLinksTx(ctx, tx, prepared, result.DerivedLinkIDs); err != nil {
		return CompressionResult{}, err
	}
	if err = consolidateCompressionSourcesTx(ctx, tx, prepared.PersonaID, prepared.SourceFactIDs, prepared.Now); err != nil {
		return CompressionResult{}, err
	}

	result.SearchDocumentsSynced = 0
	for _, factID := range prepared.SourceFactIDs {
		if err = upsertFactSearchDocumentTx(ctx, tx, prepared.PersonaID, factID); err != nil {
			return CompressionResult{}, err
		}
		result.SearchDocumentsSynced++
	}
	if prepared.Narrative != nil {
		if err = upsertNarrativeSearchDocumentTx(ctx, tx, prepared.PersonaID, prepared.Narrative.ID); err != nil {
			return CompressionResult{}, err
		}
		result.SearchDocumentsSynced++
	}
	for _, insight := range prepared.Insights {
		if err = upsertInsightSearchDocumentTx(ctx, tx, prepared.PersonaID, insight.ID); err != nil {
			return CompressionResult{}, err
		}
		result.SearchDocumentsSynced++
	}

	result.MirrorUpdatesEnqueued = 0
	for _, factID := range prepared.SourceFactIDs {
		mapped, err := factIndexMapExistsTx(ctx, tx, prepared.PersonaID, factID)
		if err != nil {
			return CompressionResult{}, err
		}
		if !mapped {
			continue
		}
		if err = enqueueCompressionIndexSyncTx(ctx, tx, r.newID(), prepared.PersonaID, factID); err != nil {
			return CompressionResult{}, err
		}
		result.MirrorUpdatesEnqueued++
	}
	if err = tx.Commit(); err != nil {
		return CompressionResult{}, err
	}
	return result, nil
}

func (r *CompressionRepository) prepare(req CompressionRequest) (CompressionRequest, CompressionResult, error) {
	req.PersonaID = strings.TrimSpace(req.PersonaID)
	if req.PersonaID == "" {
		return CompressionRequest{}, CompressionResult{}, invalidCompression("persona_id is required")
	}
	now := req.Now
	if now.IsZero() {
		now = r.now()
	}
	sourceIDs, err := normalizeCompressionSourceIDs(req.SourceFactIDs)
	if err != nil {
		return CompressionRequest{}, CompressionResult{}, err
	}
	if req.Narrative == nil && len(req.Insights) == 0 {
		return CompressionRequest{}, CompressionResult{}, invalidCompression("at least one narrative or insight draft is required")
	}

	prepared := CompressionRequest{
		PersonaID:     req.PersonaID,
		SourceFactIDs: sourceIDs,
		Now:           now,
		DryRun:        req.DryRun,
	}
	result := CompressionResult{
		SourceFactsConsolidated: len(sourceIDs),
		DryRun:                  req.DryRun,
	}
	generatedNodeCount := 0
	if req.Narrative != nil {
		narrative := *req.Narrative
		narrative.ID = strings.TrimSpace(narrative.ID)
		if narrative.ID == "" {
			narrative.ID = r.newID()
		}
		narrative.SensitivityLevel = defaultCompressionSensitivity(narrative.SensitivityLevel)
		if err := validateNarrativeDraft(narrative); err != nil {
			return CompressionRequest{}, CompressionResult{}, err
		}
		prepared.Narrative = &narrative
		result.NarrativeID = narrative.ID
		generatedNodeCount++
	}
	if len(req.Insights) > 0 {
		prepared.Insights = make([]InsightDraft, 0, len(req.Insights))
		result.InsightIDs = make([]string, 0, len(req.Insights))
		for index, input := range req.Insights {
			insight := input
			insight.ID = strings.TrimSpace(insight.ID)
			if insight.ID == "" {
				insight.ID = r.newID()
			}
			insight.SensitivityLevel = defaultCompressionSensitivity(insight.SensitivityLevel)
			if err := validateInsightDraft(index, insight); err != nil {
				return CompressionRequest{}, CompressionResult{}, err
			}
			prepared.Insights = append(prepared.Insights, insight)
			result.InsightIDs = append(result.InsightIDs, insight.ID)
			generatedNodeCount++
		}
	}
	result.DerivedLinkIDs = make([]string, 0, generatedNodeCount*len(sourceIDs))
	for i := 0; i < generatedNodeCount*len(sourceIDs); i++ {
		result.DerivedLinkIDs = append(result.DerivedLinkIDs, r.newID())
	}
	result.SearchDocumentsSynced = len(sourceIDs) + generatedNodeCount
	return prepared, result, nil
}

func normalizeCompressionSourceIDs(sourceIDs []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			return nil, invalidCompression("source_fact_ids cannot contain empty ids")
		}
		if _, exists := seen[sourceID]; exists {
			return nil, invalidCompression("source_fact_ids must contain at least two unique ids")
		}
		seen[sourceID] = struct{}{}
		result = append(result, sourceID)
	}
	if len(result) < 2 {
		return nil, invalidCompression("source_fact_ids must contain at least two unique ids")
	}
	return result, nil
}

func validateNarrativeDraft(draft NarrativeDraft) error {
	if strings.TrimSpace(draft.Summary) == "" {
		return invalidCompression("narrative summary is required")
	}
	switch draft.Scope {
	case "day", "week", "month", "topic", "relationship_phase", "project":
	default:
		return invalidCompression("narrative scope %q is not supported", draft.Scope)
	}
	if err := validateCompressionSensitivity("narrative sensitivity_level", draft.SensitivityLevel); err != nil {
		return err
	}
	if err := validateCompressionRange("narrative importance", draft.Importance, 0, 1); err != nil {
		return err
	}
	if draft.ValenceAvg != nil {
		if err := validateCompressionRange("narrative valence_avg", *draft.ValenceAvg, -1, 1); err != nil {
			return err
		}
	}
	if draft.ArousalAvg != nil {
		if err := validateCompressionRange("narrative arousal_avg", *draft.ArousalAvg, 0, 1); err != nil {
			return err
		}
	}
	if draft.ValidFrom != nil && draft.ValidTo != nil && draft.ValidTo.Before(*draft.ValidFrom) {
		return invalidCompression("narrative valid_to must be >= valid_from")
	}
	return nil
}

func validateInsightDraft(index int, draft InsightDraft) error {
	if strings.TrimSpace(draft.Content) == "" {
		return invalidCompression("insight %d content is required", index)
	}
	switch draft.InsightType {
	case "pattern", "trait", "preference", "boundary", "coping_strategy", "risk_signal":
	default:
		return invalidCompression("insight %d type %q is not supported", index, draft.InsightType)
	}
	if err := validateCompressionSensitivity(fmt.Sprintf("insight %d sensitivity_level", index), draft.SensitivityLevel); err != nil {
		return err
	}
	for _, check := range []struct {
		name     string
		value    float64
		min, max float64
	}{
		{name: "confidence", value: draft.Confidence, min: 0, max: 1},
		{name: "importance", value: draft.Importance, min: 0, max: 1},
		{name: "valence", value: draft.Valence, min: -1, max: 1},
		{name: "arousal", value: draft.Arousal, min: 0, max: 1},
	} {
		if err := validateCompressionRange(fmt.Sprintf("insight %d %s", index, check.name), check.value, check.min, check.max); err != nil {
			return err
		}
	}
	return nil
}

func defaultCompressionSensitivity(value string) string {
	if strings.TrimSpace(value) == "" {
		return string(core.SensitivityNormal)
	}
	return strings.TrimSpace(value)
}

func validateCompressionSensitivity(name string, value string) error {
	switch value {
	case string(core.SensitivityNormal), string(core.SensitivitySensitive):
		return nil
	case string(core.SensitivityHighlySensitive):
		return invalidCompression("%s highly_sensitive is not supported for compression MVP", name)
	default:
		return invalidCompression("%s must be normal or sensitive", name)
	}
}

func validateCompressionRange(name string, value float64, min float64, max float64) error {
	if value < min || value > max {
		return invalidCompression("%s must be between %g and %g", name, min, max)
	}
	return nil
}

func validateCompressionSources(ctx context.Context, runner queryer, personaID string, sourceIDs []string) ([]compressionSourceFact, error) {
	sources := make([]compressionSourceFact, 0, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		source, err := loadCompressionSource(ctx, runner, personaID, sourceID)
		if err != nil {
			return nil, err
		}
		if reason := compressionSourceIneligibleReason(source); reason != "" {
			return nil, invalidCompression("source fact %s is not eligible for compression: %s", sourceID, reason)
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func loadCompressionSource(ctx context.Context, runner queryer, personaID string, sourceID string) (compressionSourceFact, error) {
	var source compressionSourceFact
	var searchable, pinned int
	err := runner.QueryRowContext(ctx, `
SELECT id, visibility_status, validity_status, searchable, lifecycle_status, pinned,
       fact_type, extraction_confidence, sensitivity_level
FROM facts
WHERE persona_id = ? AND id = ?`, personaID, sourceID).Scan(
		&source.ID,
		&source.VisibilityStatus,
		&source.ValidityStatus,
		&searchable,
		&source.LifecycleStatus,
		&pinned,
		&source.FactType,
		&source.ExtractionConfidence,
		&source.SensitivityLevel,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return compressionSourceFact{}, invalidCompression("source fact %s does not exist for persona %s", sourceID, personaID)
	}
	if err != nil {
		return compressionSourceFact{}, err
	}
	source.Searchable = intBool(searchable)
	source.Pinned = intBool(pinned)
	return source, nil
}

func compressionSourceIneligibleReason(source compressionSourceFact) string {
	if source.VisibilityStatus != string(core.VisibilityVisible) {
		return "visibility_status=" + source.VisibilityStatus
	}
	if !source.Searchable {
		return "searchable=0"
	}
	if source.ValidityStatus == string(core.ValidityInvalidated) {
		return "validity_status=invalidated"
	}
	if source.LifecycleStatus == string(core.LifecycleArchived) {
		return "lifecycle_status=archived"
	}
	if source.LifecycleStatus == string(core.LifecycleDeepArchived) {
		return "lifecycle_status=deep_archived"
	}
	if source.Pinned {
		return "pinned=1"
	}
	switch source.FactType {
	case string(core.FactTypeCoreIdentity), string(core.FactTypeCommitment):
		return "fact_type=" + source.FactType
	}
	if source.ExtractionConfidence == string(core.ExtractionConfidenceAmbiguous) {
		return "extraction_confidence=ambiguous"
	}
	if source.SensitivityLevel == string(core.SensitivityHighlySensitive) {
		return "sensitivity_level=highly_sensitive"
	}
	return ""
}

func insertNarrativeTx(ctx context.Context, tx *sql.Tx, personaID string, draft NarrativeDraft, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO narratives (
    id, persona_id, scope, scope_ref, summary, emotional_tone,
    valence_avg, arousal_avg, importance, valid_from, valid_to,
    generated_at, visibility_status, lifecycle_status, sensitivity_level, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'visible', 'active', ?, 1)`,
		draft.ID,
		personaID,
		draft.Scope,
		nullableText(draft.ScopeRef),
		draft.Summary,
		nullableText(draft.EmotionalTone),
		nullableFloat(draft.ValenceAvg),
		nullableFloat(draft.ArousalAvg),
		draft.Importance,
		nullableTime(draft.ValidFrom),
		nullableTime(draft.ValidTo),
		formatTime(now),
		draft.SensitivityLevel,
	)
	return err
}

func insertInsightTx(ctx context.Context, tx *sql.Tx, personaID string, draft InsightDraft, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO insights (
    id, persona_id, insight_type, content, confidence, importance,
    valence, arousal, created_at, visibility_status, lifecycle_status,
    sensitivity_level, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'visible', 'active', ?, 1)`,
		draft.ID,
		personaID,
		draft.InsightType,
		draft.Content,
		draft.Confidence,
		draft.Importance,
		draft.Valence,
		draft.Arousal,
		formatTime(now),
		draft.SensitivityLevel,
	)
	return err
}

func insertDerivedLinksTx(ctx context.Context, tx *sql.Tx, req CompressionRequest, linkIDs []string) error {
	linkIndex := 0
	if req.Narrative != nil {
		for _, sourceID := range req.SourceFactIDs {
			if err := insertDerivedLinkTx(ctx, tx, linkIDs[linkIndex], req.PersonaID, core.NodeTypeNarrative, req.Narrative.ID, sourceID); err != nil {
				return err
			}
			linkIndex++
		}
	}
	for _, insight := range req.Insights {
		for _, sourceID := range req.SourceFactIDs {
			if err := insertDerivedLinkTx(ctx, tx, linkIDs[linkIndex], req.PersonaID, core.NodeTypeInsight, insight.ID, sourceID); err != nil {
				return err
			}
			linkIndex++
		}
	}
	return nil
}

func insertDerivedLinkTx(ctx context.Context, tx *sql.Tx, linkID string, personaID string, fromType core.NodeType, fromID string, sourceFactID string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    created_by, visibility_status, searchable
) VALUES (?, ?, ?, ?, 'DERIVED_FROM', 'fact', ?, 'forward', 1.0, 1.0, 'consolidation', 'visible', 1)`,
		linkID,
		personaID,
		string(fromType),
		fromID,
		sourceFactID,
	)
	return err
}

func consolidateCompressionSourcesTx(ctx context.Context, tx *sql.Tx, personaID string, sourceIDs []string, now time.Time) error {
	updatedAt := formatTime(now)
	for _, sourceID := range sourceIDs {
		result, err := tx.ExecContext(ctx, `
UPDATE facts
SET lifecycle_status = 'consolidated',
    updated_at = ?
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND validity_status != 'invalidated'
  AND searchable = 1
  AND lifecycle_status NOT IN ('archived', 'deep_archived')
  AND pinned = 0
  AND fact_type NOT IN ('core_identity', 'commitment')
  AND extraction_confidence != 'ambiguous'
  AND sensitivity_level != 'highly_sensitive'`,
			updatedAt,
			personaID,
			sourceID,
		)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return invalidCompression("source fact %s is no longer eligible for compression", sourceID)
		}
	}
	return nil
}

func countMappedCompressionSources(ctx context.Context, runner queryer, personaID string, sourceIDs []string) (int, error) {
	count := 0
	for _, sourceID := range sourceIDs {
		mapped, err := factIndexMapExists(ctx, runner, personaID, sourceID)
		if err != nil {
			return 0, err
		}
		if mapped {
			count++
		}
	}
	return count, nil
}

func factIndexMapExists(ctx context.Context, runner queryer, personaID string, factID string) (bool, error) {
	var count int
	if err := runner.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_index_map
WHERE persona_id = ?
  AND node_type = 'fact'
  AND node_id = ?`, personaID, factID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func enqueueCompressionIndexSyncTx(ctx context.Context, tx *sql.Tx, id string, personaID string, factID string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (?, ?, 'fact', ?, 'upsert_node')`, id, personaID, factID)
	return err
}

func nullableText(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullableFloat(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func invalidCompression(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidCompressionRequest, fmt.Sprintf(format, args...))
}
