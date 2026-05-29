package memorycore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultPromptVersion          = "phase2d.extraction.v1"
	defaultPreFilterPromptVersion = "phase2d.prefilter.v1"
	defaultRepairPromptVersion    = "phase2c.repair.v1"
)

type PromptVersions struct {
	Extraction string
	PreFilter  string
	Repair     string
}

type RunnerOptions struct {
	DB             *sql.DB
	Service        Service
	LLM            ExtractionLLM
	AuditStore     AuditStore
	Now            func() time.Time
	PromptVersions PromptVersions
}

type AuditStore interface {
	FindSuccessfulRun(ctx context.Context, fingerprint string, mode ExtractionRunMode) (*ExtractionRunAuditRecord, error)
	RecordRun(ctx context.Context, record ExtractionRunAuditRecord) error
}

type Runner struct {
	db             *sql.DB
	service        Service
	llm            ExtractionLLM
	audit          AuditStore
	now            func() time.Time
	promptVersions PromptVersions
}

func NewRunner(opts RunnerOptions) *Runner {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	versions := opts.PromptVersions
	if versions.Extraction == "" {
		versions.Extraction = defaultPromptVersion
	}
	if versions.PreFilter == "" {
		versions.PreFilter = defaultPreFilterPromptVersion
	}
	if versions.Repair == "" {
		versions.Repair = defaultRepairPromptVersion
	}
	return &Runner{
		db:             opts.DB,
		service:        opts.Service,
		llm:            opts.LLM,
		audit:          opts.AuditStore,
		now:            now,
		promptVersions: versions,
	}
}

func (r *Runner) Run(ctx context.Context, runReq ExtractionRunRequest) (ExtractionRunResult, error) {
	start := r.now()
	if runReq.Mode == "" {
		runReq.Mode = ExtractionRunModeDryRun
	}
	if runReq.Audit == "" {
		runReq.Audit = ExtractionAuditOn
	}
	result := ExtractionRunResult{
		RequestID:            runReq.Request.RequestID,
		PersonaID:            runReq.Request.PersonaID,
		SessionID:            runReq.Request.SessionID,
		Trigger:              runReq.Request.Trigger,
		Mode:                 runReq.Mode,
		Status:               ExtractionRunStatusFailed,
		OriginalEpisodeCount: len(runReq.Request.Episodes),
		KeptEpisodeCount:     len(runReq.Request.Episodes),
	}
	trace := newRawLogTrace(start, runReq)
	finish := func(result ExtractionRunResult, promptHash string, responseHash string, repairedHash string, prefilterHash string, usage *LLMUsage, safe *safeError) (ExtractionRunResult, error) {
		return r.finish(ctx, start, runReq, result, promptHash, responseHash, repairedHash, prefilterHash, usage, safe, trace)
	}
	if runReq.RawLog.Enabled && strings.TrimSpace(runReq.RawLog.Directory) == "" {
		return finish(result, "", "", "", "", nil, sanitizedError("raw_log_directory_required", "raw log directory is required"))
	}
	if r.llm == nil {
		return finish(result, "", "", "", "", nil, sanitizedError("llm_required", "LLM is required"))
	}
	fingerprint, err := r.fingerprint(ctx, runReq)
	if err != nil {
		safe := sanitizedError("fingerprint_failed", "could not compute extraction fingerprint")
		return finish(result, "", "", "", "", nil, safe)
	}
	result.Fingerprint = fingerprint
	if runReq.Audit != ExtractionAuditOff && r.audit != nil && !runReq.Force {
		previous, err := r.audit.FindSuccessfulRun(ctx, fingerprint, runReq.Mode)
		if err != nil {
			safe := sanitizedError("audit_lookup_failed", "could not read extraction audit state")
			return finish(result, "", "", "", "", nil, safe)
		}
		if previous != nil {
			result.Status = ExtractionRunStatusSkipped
			result.SkippedByFingerprint = true
			return finish(result, "", "", "", "", nil, nil)
		}
	}

	req := cloneRequest(runReq.Request)
	var prefilterHash string
	var usage LLMUsage
	if runReq.UsePreFilter {
		filtered, pfHash, pfUsage, pfReview, pfErr := r.runPreFilter(ctx, req, runReq, trace)
		prefilterHash = pfHash
		usage = addUsage(usage, pfUsage)
		result.PreFilterReviewCount = pfReview
		if pfErr != nil {
			safe := sanitizedError("prefilter_failed", "prefilter response was not usable")
			return finish(result, "", "", "", prefilterHash, &usage, safe)
		}
		req = filtered
		result.KeptEpisodeCount = len(req.Episodes)
		result.SkippedEpisodeCount = result.OriginalEpisodeCount - result.KeptEpisodeCount
		if len(req.Episodes) == 0 {
			result.Status = ExtractionRunStatusSkipped
			return finish(result, "", "", "", prefilterHash, &usage, nil)
		}
	}

	llmReq := r.buildExtractionLLMRequest(req, runReq)
	trace.recordExtractionRequest(llmReq)
	promptHash := hashText(llmReq.SystemPrompt + "\n" + llmReq.DeveloperPrompt + "\n" + llmReq.UserPrompt)
	raw, err := r.llm.CompleteJSON(ctx, llmReq)
	trace.recordExtractionResponse(raw)
	usage = addUsage(usage, raw.Usage)
	if err != nil {
		safe := sanitizedError("provider_failed", sanitizeProviderMessage(err))
		return finish(result, promptHash, "", "", prefilterHash, &usage, safe)
	}
	responseHash := hashText(raw.Text)
	resp, _, parseErr := ParseResponseWithRepairReport(strings.NewReader(raw.Text))
	var repairedHash string
	if parseErr != nil && runReq.RepairEnabled {
		trace.recordExtractionParseError(parseErr)
		repairReq := r.buildRepairLLMRequest(raw.Text, parseErr, runReq)
		trace.recordRepairRequest(repairReq)
		repairRaw, repairErr := r.llm.CompleteJSON(ctx, repairReq)
		trace.recordRepairResponse(repairRaw)
		usage = addUsage(usage, repairRaw.Usage)
		if repairErr != nil {
			safe := sanitizedError("repair_provider_failed", sanitizeProviderMessage(repairErr))
			return finish(result, promptHash, responseHash, "", prefilterHash, &usage, safe)
		}
		repairedHash = hashText(repairRaw.Text)
		resp, _, parseErr = ParseResponseWithRepairReport(strings.NewReader(repairRaw.Text))
		trace.recordRepairParseError(parseErr)
		result.Repaired = true
	}
	if parseErr != nil {
		trace.recordExtractionParseError(parseErr)
		safe := sanitizedError("parse_failed", "model response was not valid extraction JSON")
		return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, safe)
	}

	gate := ValidateExtraction(req, resp)
	result.QualityFlags = append([]string(nil), resp.QualityFlags...)
	result.GateResult = &gate
	result.AcceptedCount = gate.Summary.AcceptedFactCount
	result.ReviewCount = gate.Summary.NeedsReviewCount
	result.RejectedCount = gate.Summary.RejectedCount
	result.RoutedCount = gate.Summary.RoutedCount
	result.NotAppliedCount = gate.Summary.NotAppliedCount
	result.Usage = usage
	if gate.Status == "blocked" {
		result.Status = ExtractionRunStatusBlocked
		return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, nil)
	}
	result.RoutedForgetPreviews = previewExtractionDeletionIntents(ctx, r.service, req, resp, gate)

	switch runReq.Mode {
	case ExtractionRunModeValidate:
		result.Status = ExtractionRunStatusValidated
	case ExtractionRunModeDryRun:
		dry := DryRun(req, resp, gate)
		dry.RoutedForgetPreviews = append([]RoutedForgetPreview(nil), result.RoutedForgetPreviews...)
		result.DryRunResult = &dry
		result.Status = ExtractionRunStatusDryRun
	case ExtractionRunModeApply:
		if runReq.RequireCleanGate && (gate.Summary.NeedsReviewCount > 0 || gate.Summary.RejectedCount > 0) {
			result.Status = ExtractionRunStatusFailed
			safe := sanitizedError("unclean_gate", "gate contains review or rejected candidates")
			return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, safe)
		}
		apply := ApplyAcceptedFacts(ctx, r.service, r.db, req, resp, gate)
		result.ApplyResult = &apply
		result.AppliedCount = apply.AppliedCount
		result.FailureCount = len(apply.Failures)
		if runReq.ExecuteDeletionIntents {
			var forgetExecuted int
			var forgetFailures int
			result.RoutedForgetPreviews, forgetExecuted, forgetFailures = executeExtractionDeletionIntents(ctx, r.service, result.RoutedForgetPreviews)
			result.ForgetExecutedCount = forgetExecuted
			result.ForgetFailureCount = forgetFailures
			result.FailureCount += forgetFailures
		}
		switch {
		case result.ForgetFailureCount > 0:
			result.Status = ExtractionRunStatusFailed
		case apply.Status == "applied":
			result.Status = ExtractionRunStatusApplied
		case result.ForgetExecutedCount > 0:
			result.Status = ExtractionRunStatusApplied
		case apply.Status == "nothing_applied":
			result.Status = ExtractionRunStatusNothingApplied
		default:
			result.Status = ExtractionRunStatusFailed
		}
	default:
		safe := sanitizedError("invalid_mode", "mode must be validate, dry-run, or apply")
		return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, safe)
	}
	return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, nil)
}

func (r *Runner) finish(ctx context.Context, start time.Time, runReq ExtractionRunRequest, result ExtractionRunResult, promptHash string, responseHash string, repairedHash string, prefilterHash string, usage *LLMUsage, safe *safeError, trace *rawLogTrace) (ExtractionRunResult, error) {
	result.DurationMS = time.Since(start).Milliseconds()
	if usage != nil {
		result.Usage = *usage
	}
	if safe != nil {
		result.SanitizedErrorCode = safe.Code
		result.SanitizedErrorMessage = safe.Message
		if result.Status == "" {
			result.Status = ExtractionRunStatusFailed
		}
	}
	if runReq.Audit != ExtractionAuditOff && r.audit != nil && result.Fingerprint != "" {
		record := ExtractionRunAuditRecord{
			RequestID:              result.RequestID,
			PersonaID:              result.PersonaID,
			SessionID:              result.SessionID,
			Trigger:                result.Trigger,
			Mode:                   result.Mode,
			Status:                 result.Status,
			Fingerprint:            result.Fingerprint,
			ProviderID:             runReq.ProviderID,
			ProviderKind:           runReq.ProviderKind,
			Model:                  runReq.Model,
			PromptVersion:          r.promptVersions.Extraction,
			PreFilterPromptVersion: r.promptVersions.PreFilter,
			RepairPromptVersion:    r.promptVersions.Repair,
			OriginalEpisodeCount:   result.OriginalEpisodeCount,
			KeptEpisodeCount:       result.KeptEpisodeCount,
			SkippedEpisodeCount:    result.SkippedEpisodeCount,
			AcceptedCount:          result.AcceptedCount,
			ReviewCount:            result.ReviewCount,
			RejectedCount:          result.RejectedCount,
			RoutedCount:            result.RoutedCount,
			NotAppliedCount:        result.NotAppliedCount,
			AppliedCount:           result.AppliedCount,
			FailureCount:           result.FailureCount,
			PromptHash:             promptHash,
			ResponseHash:           responseHash,
			RepairedResponseHash:   repairedHash,
			PreFilterHash:          prefilterHash,
			Usage:                  result.Usage,
			DurationMS:             result.DurationMS,
			SanitizedErrorCode:     result.SanitizedErrorCode,
			SanitizedErrorMessage:  result.SanitizedErrorMessage,
			CreatedAt:              start,
			UpdatedAt:              r.now(),
		}
		if err := r.audit.RecordRun(ctx, record); err != nil && safe == nil {
			result.Status = ExtractionRunStatusFailed
			result.SanitizedErrorCode = "audit_write_failed"
			result.SanitizedErrorMessage = "could not write extraction audit state"
			safe = sanitizedError(result.SanitizedErrorCode, result.SanitizedErrorMessage)
		}
	}
	if runReq.RawLog.Enabled && strings.TrimSpace(runReq.RawLog.Directory) != "" {
		audit := rawLogAudit{
			Fingerprint:          result.Fingerprint,
			PromptHash:           promptHash,
			ResponseHash:         responseHash,
			RepairedResponseHash: repairedHash,
			PreFilterHash:        prefilterHash,
		}
		if err := writeRawLog(runReq.RawLog.Directory, result, trace, audit); err != nil {
			result.Status = ExtractionRunStatusFailed
			result.SanitizedErrorCode = "raw_log_write_failed"
			result.SanitizedErrorMessage = "could not write extraction raw log"
			return result, errors.New(result.SanitizedErrorMessage)
		}
	}
	if safe != nil {
		return result, errors.New(safe.Message)
	}
	return result, nil
}

func (r *Runner) fingerprint(ctx context.Context, req ExtractionRunRequest) (string, error) {
	hashes, err := episodeContentHashes(ctx, r.db, req.Request.PersonaID, req.Request.Episodes)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"request_schema":           ExtractionRequestSchemaVersion,
		"response_schema":          ExtractionResponseSchemaVersion,
		"persona_id":               req.Request.PersonaID,
		"session_id":               req.Request.SessionID,
		"trigger":                  req.Request.Trigger,
		"episodes":                 hashes,
		"window":                   req.Window,
		"policy":                   req.Request.Policy,
		"predicate_schema_hash":    hashJSON(req.Request.PredicateSchemas),
		"prompt_version":           r.promptVersions.Extraction,
		"prefilter_prompt_version": r.promptVersions.PreFilter,
		"repair_prompt_version":    r.promptVersions.Repair,
		"admission_policy_version": extractionAdmissionPolicyVersion,
		"prompt_hash":              hashText(extractionSystemPrompt(r.promptVersions.Extraction) + "\n" + extractionDeveloperPrompt()),
		"use_prefilter":            req.UsePreFilter,
		"repair_enabled":           req.RepairEnabled,
		"require_clean_gate":       req.RequireCleanGate,
		"provider_id":              req.ProviderID,
		"provider_kind":            req.ProviderKind,
		"model":                    req.Model,
		"provider_params": map[string]any{
			"temperature":     req.Temperature,
			"max_tokens":      req.MaxTokens,
			"timeout":         req.Timeout.String(),
			"response_format": req.ResponseFormat,
		},
		"mode": req.Mode,
	}
	if req.UsePreFilter {
		payload["prefilter_prompt_hash"] = hashText(prefilterSystemPrompt(r.promptVersions.PreFilter) + "\n" + prefilterDeveloperPrompt())
	}
	return hashJSON(payload), nil
}

func episodeContentHashes(ctx context.Context, db *sql.DB, personaID string, episodes []ExtractionEpisode) ([]map[string]string, error) {
	out := make([]map[string]string, 0, len(episodes))
	for _, episode := range episodes {
		contentHash := ""
		if db != nil {
			_ = db.QueryRowContext(ctx, `SELECT content_hash FROM episodes WHERE persona_id = ? AND id = ?`, personaID, episode.EpisodeID).Scan(&contentHash)
		}
		if contentHash == "" {
			contentHash = hashText(episode.Content)
		}
		out = append(out, map[string]string{"episode_id": episode.EpisodeID, "content_hash": contentHash})
	}
	return out, nil
}

func cloneRequest(req ExtractionRequest) ExtractionRequest {
	req.Episodes = append([]ExtractionEpisode(nil), req.Episodes...)
	req.KnownEntities = append([]ExtractionKnownEntity(nil), req.KnownEntities...)
	req.PredicateSchemas = append([]ExtractionPredicateSchema(nil), req.PredicateSchemas...)
	req.ApprovedWorkCandidates = append([]ExtractionWorkCandidate(nil), req.ApprovedWorkCandidates...)
	return req
}

func hashJSON(value any) string {
	data, _ := json.Marshal(value)
	return hashText(string(data))
}

func hashText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

type safeError struct {
	Code    string
	Message string
}

func sanitizedError(code string, message string) *safeError {
	return &safeError{Code: code, Message: message}
}

func sanitizeProviderMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "api key env ") && strings.HasSuffix(msg, " is not set"):
		return msg
	case strings.Contains(msg, "timeout"):
		return "provider request timed out"
	default:
		return "provider request failed"
	}
}

func addUsage(a LLMUsage, b LLMUsage) LLMUsage {
	a.PromptTokens += b.PromptTokens
	a.CompletionTokens += b.CompletionTokens
	a.TotalTokens += b.TotalTokens
	return a
}

func (r *Runner) buildExtractionLLMRequest(req ExtractionRequest, runReq ExtractionRunRequest) ExtractionLLMRequest {
	requestJSON, _ := json.Marshal(req)
	return ExtractionLLMRequest{
		Purpose:         ExtractionLLMPurposeExtraction,
		ProviderID:      runReq.ProviderID,
		ProviderKind:    runReq.ProviderKind,
		Model:           runReq.Model,
		SystemPrompt:    extractionSystemPrompt(r.promptVersions.Extraction),
		DeveloperPrompt: extractionDeveloperPrompt(),
		UserPrompt:      string(requestJSON),
		Temperature:     runReq.Temperature,
		MaxTokens:       runReq.MaxTokens,
		Timeout:         runReq.Timeout,
		ResponseFormat:  runReq.ResponseFormat,
		Metadata:        requestMetadata(ExtractionLLMPurposeExtraction, req.RequestID, r.promptVersions.Extraction, ExtractionResponseSchemaVersion),
	}
}

func (r *Runner) buildRepairLLMRequest(raw string, parseErr error, runReq ExtractionRunRequest) ExtractionLLMRequest {
	return ExtractionLLMRequest{
		Purpose:         ExtractionLLMPurposeRepair,
		ProviderID:      runReq.ProviderID,
		ProviderKind:    runReq.ProviderKind,
		Model:           runReq.Model,
		SystemPrompt:    repairSystemPrompt(r.promptVersions.Repair),
		DeveloperPrompt: repairDeveloperPrompt(parseErr),
		UserPrompt:      raw,
		Temperature:     runReq.Temperature,
		MaxTokens:       runReq.MaxTokens,
		Timeout:         runReq.Timeout,
		ResponseFormat:  runReq.ResponseFormat,
		Metadata:        requestMetadata(ExtractionLLMPurposeRepair, "", r.promptVersions.Repair, ExtractionResponseSchemaVersion),
	}
}

func requestMetadata(purpose string, requestID string, promptVersion string, schemaVersion string) map[string]string {
	metadata := map[string]string{
		"purpose":        purpose,
		"prompt_version": promptVersion,
		"schema_version": schemaVersion,
	}
	if requestID != "" {
		metadata["request_id"] = requestID
	}
	return metadata
}

func extractionSystemPrompt(version string) string {
	return fmt.Sprintf(`MemoryCore extraction runtime %s. Extract candidate JSON only. Go gates decide validity and persistence.
Return exactly one JSON object and no prose, markdown, code fences, or wrapper text.
FORMAT ONLY JSON EXAMPLE:
{"schema_version":"%s","request_id":"req_example","persona_id":"default","session_id":null,"trigger":"session_end","source_window":{"episode_ids":["ep_1"],"started_at":null,"ended_at":null},"entities":[],"facts":[{"candidate_id":"f1","subject_entity_candidate_id":"user","predicate":"likes","object_entity_candidate_id":null,"object_literal":"手冲咖啡","content_summary":"用户喜欢手冲咖啡。","fact_type":"stable_preference","valid_from":null,"valid_to":null,"temporal_precision":"unknown","extraction_confidence":"explicit","extraction_confidence_score":0.95,"importance":0.7,"valence":0.2,"arousal":0.2,"sensitivity_level":"normal","source_episode_ids":["ep_1"],"evidence_notes":"用户直接表达喜欢手冲咖啡。","reasoning":null,"operation_hint":"insert_candidate","pinned":false,"user_requested":false,"searchable_hint":true,"quality_decision":"accept_for_consolidation","quality_reasons":["explicit_user_statement"]}],"links":[],"affect_events":[],"deletion_intents":[],"pin_intents":[],"correction_hints":[],"rejected_candidates":[],"quality_flags":[],"gate_summary":{"accepted_fact_count":1,"needs_review_count":0,"rejected_count":0,"has_deletion_intent":false,"has_pin_intent":false,"requires_human_review":false,"notes":"通过"}}`, version, ExtractionResponseSchemaVersion)
}

func extractionDeveloperPrompt() string {
	return "Return strict JSON matching schema " + ExtractionResponseSchemaVersion + ". Top-level fields must include schema_version, request_id, persona_id, session_id, trigger, source_window, entities, facts, links, affect_events, deletion_intents, pin_intents, correction_hints, rejected_candidates, quality_flags, and gate_summary. Preserve IDs from the ExtractionRequest JSON in the user message.\n" + extractionAdmissionPromptContract() + "\n" + extractionFieldContract()
}

func repairSystemPrompt(version string) string {
	return fmt.Sprintf("MemoryCore JSON repair %s. Repair formatting/schema JSON only. Do not infer or add evidence.", version)
}

func repairDeveloperPrompt(parseErr error) string {
	message := ""
	if parseErr != nil {
		message = " Parser error to fix: " + parseErr.Error()
	}
	return "Return only one strict JSON object for schema " + ExtractionResponseSchemaVersion + ". Do not include markdown fences." + message + "\n" + extractionFieldContract()
}

func extractionFieldContract() string {
	entityTypes := strings.Join(AllowedExtractionEntityTypes(), ", ")
	mergeHints := strings.Join(AllowedExtractionMergeHints(), ", ")
	confidenceLabels := strings.Join(AllowedExtractionConfidenceLabels(), ", ")
	return fmt.Sprintf(`Field contract:
- Use Chinese for human-readable summaries and notes when the source conversation is Chinese: content_summary, evidence_notes, reasoning, gate_summary.notes, pin_reason, target_description, corrected_topic, and rejection reasons. Keep JSON field names, IDs, enum values, fact_type, facts.operation_hint, quality_decision, sensitivity_level, and confidence labels as protocol values.
- Do not copy request.known_entities into response.entities. entity_id is input-only. Do not return entity_id; use "known_entity_id" for an existing entity or omit entities when using special ids "user" or "agent".
- response.entities fields only: "candidate_id", "canonical_name", "entity_type", "aliases", "description", "confidence", "source_episode_ids", "merge_hint", "known_entity_id", "sensitivity_level", "reasoning".
- response.entities.entity_type must be exactly one of: %s.
- Do not output entity_type = pet, cat, dog, animal, or project. For a named pet such as 小橘, output entity_type = object.
- response.entities.confidence must be a number from 0.0 to 1.0. Do not put explicit, inferred, ambiguous, sure, or other strings in entity confidence.
- response.entities.merge_hint must be exactly one of: %s. Do not output merge_hint = new; use new_entity.
- response.facts.extraction_confidence must be exactly one of: %s.
- For a named pet relationship, output an object entity and a has_pet fact: subject_entity_candidate_id = "user", predicate = "has_pet", object_entity_candidate_id points to the pet entity, and object_literal = null.
- response.affect_events fields only: "candidate_id", "scope", "label", "valence", "arousal", "source_episode_ids", "confidence", "reasoning". Do not return subject_entity_candidate_id, affect_type, intensity, trigger, context, operation_hint, quality_decision, or quality_reasons in affect_events.
- response.facts fields only: "candidate_id", "subject_entity_candidate_id", "predicate", "object_entity_candidate_id", "object_literal", "content_summary", "fact_type", "valid_from", "valid_to", "temporal_precision", "extraction_confidence", "extraction_confidence_score", "importance", "valence", "arousal", "sensitivity_level", "source_episode_ids", "evidence_notes", "reasoning", "operation_hint", "pinned", "user_requested", "searchable_hint", "quality_decision", "quality_reasons".
- response.deletion_intents fields only: "candidate_id", "forget_level", "target_description", "target_node_type_hint", "source_episode_id", "confidence", "reasoning", "requires_confirmation". Do not output target_candidate_id, target_predicate, target_object_literal, target_episode_ids, source_episode_ids, scope, operation_hint, quality_decision, or quality_reasons in deletion_intents.
- response.correction_hints fields only: "candidate_id", "corrected_topic", "new_candidate_id", "old_memory_ref", "confidence", "reasoning". Do not output kind, target_*, corrected_value, source_episode_ids, or raw episode text in correction_hints.
- response.rejected_candidates fields only: "candidate_id", "kind", "reasons". Put reason codes in the reasons array. Do not output reason, reason_code, reason_codes, source_episode_ids, or raw episode text in rejected_candidates.
- Named pet example: {"entities":[{"candidate_id":"e_pet_xiaoju","canonical_name":"小橘","entity_type":"object","aliases":["小橘猫"],"description":"用户提到的宠物。","confidence":0.95,"source_episode_ids":["ep_1"],"merge_hint":"new_entity","known_entity_id":null,"sensitivity_level":"normal","reasoning":null}],"facts":[{"candidate_id":"f_has_pet_xiaoju","subject_entity_candidate_id":"user","predicate":"has_pet","object_entity_candidate_id":"e_pet_xiaoju","object_literal":null,"content_summary":"用户有一只叫小橘的宠物。","fact_type":"core_identity","valid_from":null,"valid_to":null,"temporal_precision":"unknown","extraction_confidence":"explicit","extraction_confidence_score":0.95,"importance":0.65,"valence":0.2,"arousal":0.2,"sensitivity_level":"normal","source_episode_ids":["ep_1"],"evidence_notes":"用户直接提到小橘。","reasoning":null,"operation_hint":"insert_candidate","pinned":false,"user_requested":false,"searchable_hint":true,"quality_decision":"accept_for_consolidation","quality_reasons":[]}]}.
- Repair schema contract only. Allowed local-style rewrites are pet/cat/dog/animal entity_type to object, entity confidence labels to numeric scores, and merge_hint new to new_entity. Do not invent memories or add facts unless the original response already implies the relationship and the source episodes support it.`, entityTypes, mergeHints, confidenceLabels)
}

func extractionAdmissionPromptContract() string {
	return `Memory admission rules:
- Only write user-owned, explicit, durable, allowed memories.
- Before emitting any fact, answer internally: is this claim owned by the user, explicit or strongly user-confirmed, durable/useful for long-term memory, and allowed by the user to be remembered and recalled? Emit a fact only when all answers are yes.
- For "别记这个 / 不要记 / don't remember this / do not save", do not emit facts for the same content. If it only refers to the current window, output no deletion_intent; if it points to old memory, emit deletion_intents only.
- For "不要再提 / 别再提 / forget / delete / remove", emit deletion_intents only for the old memory target. Default forget_level to "soft_forget" unless the user explicitly asks for permanent/source deletion. target_description must name the concrete old memory topic, not generic phrases such as "old memory", "related memory", or "旧记忆". Do not turn the control intent itself into an ordinary user fact.
- If the same turn also contains a correction/new preference, you may emit a normal fact for the corrected content, but never emit a fact for the old target the user asked not to mention.
- For corrections such as "不是北京，是上海", emit correction_hints and an optional clearly supported correction_candidate fact for the new explicit value. Do not re-emit the stale value as an ordinary fact.
- Do not emit facts for assistant guesses, assistant suggestions, tool outputs, search results, command logs, stack traces, work progress logs, hypothetical statements, conditional plans, roleplay-only statements, or ephemeral chitchat.
- Negative examples: "如果我以后搬去东京" is not "用户住在东京"; "你可能不喜欢早会" is assistant speculation; "你可以试试周末运动" is assistant suggestion; "npm install failed" is work-log noise; "这句别记" blocks current fact writing; "不要再提早会" is a soft_forget deletion intent.
- Use rejected_candidates for visible rejected candidates when useful for audit, with reason codes such as hypothetical_scenario, assistant_speculation_not_user_fact, assistant_suggestion_not_user_fact, tool_noise, work_log_noise, do_not_remember, do_not_mention, deletion_intent_only, correction_hint_only, weak_inference, sensitive_inference, or no_durable_value.
- gate_summary is advisory only. Do not inflate accepted_fact_count. The Go gate is the persistence authority.`
}
