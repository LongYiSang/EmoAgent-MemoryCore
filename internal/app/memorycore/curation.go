package memorycore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

type curationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func (s *service) RunCuration(ctx context.Context, req RunCurationRequest) (*RunCurationResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	mode := normalizeCurationMode(req.Mode)
	if mode == "" {
		return nil, fmt.Errorf("%w: mode must be dry-run, dry_run, or apply", ErrInvalidRequest)
	}
	trigger := defaultString(req.Trigger, "manual")
	if !isCurationTrigger(trigger) {
		return nil, fmt.Errorf("%w: trigger must be manual, scheduled, cli, or test", ErrInvalidRequest)
	}
	if !req.Force && !s.semanticOps.Curation.Enabled {
		return nil, extractionServiceError("curation_disabled", "memory curation is disabled")
	}
	rawLog := s.semanticOps.Curation.RawLog
	if req.RawLog != nil {
		rawLog = *req.RawLog
	}
	if err := validateCurationRawLogOptions(rawLog); err != nil {
		return nil, err
	}
	trace := newCurationRawLogTrace(s.now(), req, rawLog)

	limit := firstPositive(req.MaxNewFacts, s.semanticOps.Curation.MaxNewFactsPerRun, 100)
	candidateLimit := firstPositive(req.CandidateLimitPerFact, s.semanticOps.Curation.CandidateLimitPerFact, 20)
	maxFactsPerGroup := firstPositive(req.MaxFactsPerGroup, s.semanticOps.Curation.MaxFactsPerGroup, 8)
	minConfidence := firstFloat(req.MinAutoApplyConfidence, s.semanticOps.Curation.MinAutoApplyConfidence)
	if minConfidence <= 0 {
		minConfidence = 0.88
	}
	candidateRetrieval := curationCandidateRetrievalOptions(s.semanticOps.Curation.CandidateRetrieval, req.CandidateRetrieval)
	trace.recordCandidateRetrievalStart(candidateRetrieval, candidateLimit)

	provider := curationProviderOptions(s.semanticOps.Curation.LLM, req)
	llm, err := newExtractionLLM(provider)
	if err != nil {
		return nil, err
	}

	deltaFacts, err := s.curation.LoadDeltaFacts(ctx, memsqlite.CurationDeltaQuery{
		PersonaID:        personaID,
		SinceCreatedAt:   req.SinceCreatedAt,
		SinceFactID:      req.SinceFactID,
		UntilCreatedAt:   req.UntilCreatedAt,
		UntilFactID:      req.UntilFactID,
		MaxNewFacts:      limit,
		IncludeFactTypes: s.semanticOps.Curation.IncludeFactTypes,
		ExcludeFactTypes: s.semanticOps.Curation.ExcludeFactTypes,
	})
	if err != nil {
		return nil, err
	}
	candidates := map[string][]memsqlite.CurationComparableCandidate{}
	for _, fact := range deltaFacts {
		found, candidateTrace, err := s.retrieveCurationComparableCandidates(ctx, personaID, fact, candidateLimit, candidateRetrieval)
		if err != nil {
			return nil, err
		}
		trace.recordCandidateRetrievalDelta(candidateTrace)
		candidates[fact.ID] = found
	}
	groups := s.curation.BuildGroupsWithCandidateSources(deltaFacts, candidates, maxFactsPerGroup, candidateRetrieval.MirrorMinSimilarity)
	prepared := make([]memsqlite.CurationPreparedGroup, 0, len(groups))
	groupResults := make([]CurationGroupResult, 0, len(groups))
	for _, group := range groups {
		decision, err := s.analyzeCurationGroup(ctx, llm, provider, personaID, group, trace)
		if err != nil {
			result := failedRunCurationResult(mode, len(deltaFacts), len(groups), groupResults, trace)
			if rawLogErr := writeCurationRawLog(rawLog.Directory, result, trace); rawLogErr != nil {
				return result, extractionServiceError("raw_log_write_failed", "could not write curation raw log")
			}
			return result, err
		}
		prepared = append(prepared, memsqlite.CurationPreparedGroup{
			ID:       group.ID,
			Facts:    group.Facts,
			Decision: decision,
		})
		groupResults = append(groupResults, curationGroupResultFromDecision(group.ID, decision, mode, minConfidence))
	}

	cursorToCreatedAt, cursorToFactID := curationCursorTo(deltaFacts)
	storeResult, err := s.curation.ApplyDecisions(ctx, memsqlite.CurationApplyRequest{
		PersonaID:              personaID,
		Mode:                   mode,
		Trigger:                trigger,
		CursorFromCreatedAt:    req.SinceCreatedAt,
		CursorFromFactID:       req.SinceFactID,
		CursorToCreatedAt:      cursorToCreatedAt,
		CursorToFactID:         cursorToFactID,
		NewFactCount:           len(deltaFacts),
		ProviderID:             provider.ID,
		ProviderKind:           provider.Kind,
		Model:                  provider.Model,
		Groups:                 prepared,
		MinAutoApplyConfidence: minConfidence,
		UpdateCheckpoint:       req.UpdateCheckpoint,
	})
	if err != nil {
		return nil, err
	}
	for i := range groupResults {
		if status := storeResult.GroupStatuses[groupResults[i].GroupID]; status != "" {
			groupResults[i].GroupStatus = status
		}
	}
	result := &RunCurationResult{
		RunID:               storeResult.RunID,
		Status:              storeResult.Status,
		Mode:                storeResult.Mode,
		NewFactCount:        storeResult.NewFactCount,
		GroupCount:          storeResult.GroupCount,
		LLMGroupCount:       storeResult.LLMGroupCount,
		AppliedGroupCount:   storeResult.AppliedGroupCount,
		ReviewGroupCount:    storeResult.ReviewGroupCount,
		NoopGroupCount:      storeResult.NoopGroupCount,
		ErrorCount:          storeResult.ErrorCount,
		CursorFromCreatedAt: req.SinceCreatedAt,
		CursorFromFactID:    req.SinceFactID,
		CursorToCreatedAt:   cursorToCreatedAt,
		CursorToFactID:      cursorToFactID,
		Groups:              groupResults,
	}
	if err := writeCurationRawLog(rawLog.Directory, result, trace); err != nil {
		return result, extractionServiceError("raw_log_write_failed", "could not write curation raw log")
	}
	return result, nil
}

func (s *service) analyzeCurationGroup(ctx context.Context, llm ExtractionLLM, provider ExtractionProviderOptions, personaID string, group memsqlite.CurationCandidateGroup, trace *curationRawLogTrace) (memsqlite.CurationDecision, error) {
	payload, err := s.buildCurationPayload(ctx, personaID, group)
	if err != nil {
		return memsqlite.CurationDecision{}, err
	}
	data, _ := json.Marshal(payload)
	llmReq := ExtractionLLMRequest{
		Purpose:         ExtractionLLMPurposeCuration,
		ProviderID:      provider.ID,
		ProviderKind:    provider.Kind,
		Model:           provider.Model,
		SystemPrompt:    curationSystemPrompt(),
		DeveloperPrompt: curationDeveloperPrompt(),
		UserPrompt:      string(data),
		Temperature:     provider.Temperature,
		MaxTokens:       provider.MaxTokens,
		Timeout:         provider.Timeout,
		ResponseFormat:  provider.ResponseFormat,
		Metadata: map[string]string{
			"schema_version": CurationResponseSchemaVersion,
			"group_id":       group.ID,
		},
	}
	resp, err := llm.CompleteJSON(ctx, llmReq)
	if err != nil {
		if trace != nil {
			trace.recordGroup(group, payload, llmReq, resp, memsqlite.CurationDecision{}, err)
		}
		return memsqlite.CurationDecision{}, err
	}
	decision, parseErr := parseCurationLLMResponse(resp.Text)
	if parseErr == nil {
		decision = normalizeCurationDecisionForGroup(decision, group)
	}
	if trace != nil {
		trace.recordGroup(group, payload, llmReq, resp, decision, parseErr)
	}
	return decision, parseErr
}

func (s *service) buildCurationPayload(ctx context.Context, personaID string, group memsqlite.CurationCandidateGroup) (curationLLMRequestPayload, error) {
	facts := make([]curationPromptFact, 0, len(group.Facts))
	for _, groupFact := range group.Facts {
		fact, err := loadCurationPromptFact(ctx, s.sqlDB, personaID, groupFact.FactID)
		if err != nil {
			return curationLLMRequestPayload{}, err
		}
		fact.Role = groupFact.Role
		facts = append(facts, fact)
	}
	return curationLLMRequestPayload{
		SchemaVersion: CurationRequestSchemaVersion,
		PersonaID:     personaID,
		GroupID:       group.ID,
		Facts:         facts,
		Policy: map[string]any{
			"auto_apply_allowed_relations":    []string{"same", "refinement"},
			"auto_apply_allowed_answer_gain":  []string{"none", "small"},
			"do_not_merge_complement_as_same": true,
		},
	}, nil
}

func loadCurationPromptFact(ctx context.Context, db curationQueryer, personaID string, factID string) (curationPromptFact, error) {
	var fact curationPromptFact
	var subjectEntityID, objectLiteral sql.NullString
	var pinned int
	err := db.QueryRowContext(ctx, `
SELECT id, content_summary, fact_type, predicate, subject_entity_id, object_literal,
       extraction_confidence, importance, sensitivity_level, pinned
FROM facts
WHERE persona_id = ? AND id = ?`, personaID, factID).Scan(
		&fact.FactID, &fact.ContentSummary, &fact.FactType, &fact.Predicate, &subjectEntityID, &objectLiteral,
		&fact.ExtractionConfidence, &fact.Importance, &fact.SensitivityLevel, &pinned,
	)
	if err != nil {
		return curationPromptFact{}, err
	}
	fact.SubjectEntityID = nullStringValue(subjectEntityID)
	fact.ObjectLiteral = nullStringValue(objectLiteral)
	fact.Pinned = pinned == 1
	refs, err := loadCurationPromptEvidence(ctx, db, personaID, factID)
	if err != nil {
		return curationPromptFact{}, err
	}
	fact.SourceEpisodeRefs = refs
	return fact, nil
}

func loadCurationPromptEvidence(ctx context.Context, db curationQueryer, personaID string, factID string) ([]curationSourceEpisodeRef, error) {
	rows, err := db.QueryContext(ctx, `
SELECT e.id, e.occurred_at
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.from_node_id = ?
  AND l.link_type = 'EVIDENCED_BY'
  AND e.visibility_status = 'visible'
ORDER BY e.occurred_at DESC`, personaID, factID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []curationSourceEpisodeRef
	for rows.Next() {
		var ref curationSourceEpisodeRef
		if err := rows.Scan(&ref.EpisodeID, &ref.OccurredAt); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func normalizeCurationMode(value string) string {
	switch strings.TrimSpace(value) {
	case "", "dry-run", "dry_run":
		return memsqlite.CurationModeDryRun
	case "apply":
		return memsqlite.CurationModeApply
	default:
		return ""
	}
}

func isCurationTrigger(value string) bool {
	switch value {
	case "manual", "scheduled", "cli", "test":
		return true
	default:
		return false
	}
}

func curationProviderOptions(defaults CurationLLMOptions, req RunCurationRequest) ExtractionProviderOptions {
	provider := defaults.Provider
	if provider.Kind == "" && defaults.ProviderKind != "" {
		provider.Kind = defaults.ProviderKind
	}
	if provider.ID == "" {
		provider.ID = defaults.ProviderID
	}
	if provider.Model == "" {
		provider.Model = defaults.Model
	}
	if provider.Temperature == 0 {
		provider.Temperature = defaults.Temperature
	}
	if provider.MaxTokens == 0 {
		provider.MaxTokens = defaults.MaxTokens
	}
	if provider.ResponseFormat == "" {
		provider.ResponseFormat = defaults.ResponseFormat
	}
	if provider.Timeout == 0 {
		provider.Timeout = defaults.Timeout
	}
	if provider.Thinking == nil {
		provider.Thinking = defaults.Thinking
	}
	if req.ProviderKind != "" {
		provider.Kind = req.ProviderKind
	}
	if req.ProviderID != "" {
		provider.ID = req.ProviderID
	}
	if req.ProviderID == ExtractionProviderMock && provider.Kind == "" {
		provider.Kind = ExtractionProviderMock
	}
	if req.Model != "" {
		provider.Model = req.Model
	}
	if req.Temperature != 0 {
		provider.Temperature = req.Temperature
	}
	if req.MaxTokens != 0 {
		provider.MaxTokens = req.MaxTokens
	}
	if req.Timeout != 0 {
		provider.Timeout = req.Timeout
	}
	if provider.Kind == "" {
		provider.Kind = ExtractionProviderDisabled
	}
	if provider.Kind == ExtractionProviderMock && provider.ID == "" {
		provider.ID = ExtractionProviderMock
	}
	if provider.MaxTokens == 0 {
		provider.MaxTokens = 4096
	}
	if provider.ResponseFormat == "" {
		provider.ResponseFormat = ExtractionResponseFormatJSONObject
	}
	return normalizeExtractionOptions(ExtractionOptions{Provider: provider}).Provider
}

func curationCursorTo(facts []core.Fact) (*time.Time, string) {
	if len(facts) == 0 {
		return nil, ""
	}
	last := facts[len(facts)-1]
	return &last.CreatedAt, last.ID
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func nullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func curationGroupResultFromDecision(groupID string, decision memsqlite.CurationDecision, mode string, minConfidence float64) CurationGroupResult {
	return CurationGroupResult{
		GroupID:          groupID,
		GroupStatus:      curationGroupResultStatus(mode, decision, minConfidence),
		Decision:         decision.Decision,
		SemanticRelation: decision.SemanticRelation,
		AnswerGain:       decision.AnswerGain,
		Confidence:       decision.Confidence,
		CanonicalFactID:  decision.CanonicalFactID,
		SourceFactIDs:    append([]string(nil), decision.SourceFactIDs...),
		ReasonCodes:      append([]string(nil), decision.ReasonCodes...),
	}
}

func curationGroupResultStatus(mode string, decision memsqlite.CurationDecision, minConfidence float64) string {
	if mode == memsqlite.CurationModeDryRun {
		return "noop"
	}
	if decision.Decision == "needs_review" || decision.Decision == "conflict_needs_review" || decision.RequiresReview {
		return "needs_review"
	}
	if decision.Confidence >= minConfidence && (decision.Decision == "merge_into_existing" || decision.Decision == "reinforce_existing" || decision.Decision == "create_canonical_fact") {
		return "applied"
	}
	return "noop"
}
