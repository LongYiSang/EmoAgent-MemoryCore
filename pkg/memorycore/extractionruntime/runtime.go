package extractionruntime

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

	"github.com/longyisang/emoagent-memorycore/internal/memory/extraction"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

const (
	defaultPromptVersion          = "phase2c.extraction.v1"
	defaultPreFilterPromptVersion = "phase2c.prefilter.v1"
	defaultRepairPromptVersion    = "phase2c.repair.v1"
)

type BuildRequestOptions struct {
	PersonaID                string
	SessionID                *string
	EpisodeIDs               []string
	Trigger                  string
	Limit                    int
	Since                    *time.Time
	Until                    *time.Time
	Timezone                 string
	AllowSensitiveExtraction bool
	AllowInference           bool
	ManualPin                bool
	ManualForget             bool
	MaxFacts                 int
	MaxLinks                 int
	Now                      time.Time
}

type PromptVersions struct {
	Extraction string
	PreFilter  string
	Repair     string
}

type RunnerOptions struct {
	DB             *sql.DB
	Service        memorycore.Service
	LLM            memorycore.ExtractionLLM
	AuditStore     AuditStore
	Now            func() time.Time
	PromptVersions PromptVersions
}

type AuditStore interface {
	FindSuccessfulRun(ctx context.Context, fingerprint string, mode memorycore.ExtractionRunMode) (*memorycore.ExtractionRunAuditRecord, error)
	RecordRun(ctx context.Context, record memorycore.ExtractionRunAuditRecord) error
}

type Runner struct {
	db             *sql.DB
	service        memorycore.Service
	llm            memorycore.ExtractionLLM
	audit          AuditStore
	now            func() time.Time
	promptVersions PromptVersions
}

func BuildRequest(ctx context.Context, db *sql.DB, opts BuildRequestOptions) (memorycore.ExtractionRequest, error) {
	return extraction.BuildRequest(ctx, db, extraction.BuildRequestOptions{
		PersonaID:                opts.PersonaID,
		SessionID:                opts.SessionID,
		EpisodeIDs:               opts.EpisodeIDs,
		Trigger:                  opts.Trigger,
		Limit:                    opts.Limit,
		Since:                    opts.Since,
		Until:                    opts.Until,
		Timezone:                 opts.Timezone,
		AllowSensitiveExtraction: opts.AllowSensitiveExtraction,
		AllowInference:           opts.AllowInference,
		ManualPin:                opts.ManualPin,
		ManualForget:             opts.ManualForget,
		MaxFacts:                 opts.MaxFacts,
		MaxLinks:                 opts.MaxLinks,
		Now:                      opts.Now,
	})
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

func (r *Runner) Run(ctx context.Context, runReq memorycore.ExtractionRunRequest) (memorycore.ExtractionRunResult, error) {
	start := r.now()
	if runReq.Mode == "" {
		runReq.Mode = memorycore.ExtractionRunModeDryRun
	}
	if runReq.Audit == "" {
		runReq.Audit = memorycore.ExtractionAuditOn
	}
	result := memorycore.ExtractionRunResult{
		RequestID:            runReq.Request.RequestID,
		PersonaID:            runReq.Request.PersonaID,
		SessionID:            runReq.Request.SessionID,
		Trigger:              runReq.Request.Trigger,
		Mode:                 runReq.Mode,
		Status:               memorycore.ExtractionRunStatusFailed,
		OriginalEpisodeCount: len(runReq.Request.Episodes),
		KeptEpisodeCount:     len(runReq.Request.Episodes),
	}
	trace := newRawLogTrace(start, runReq)
	finish := func(result memorycore.ExtractionRunResult, promptHash string, responseHash string, repairedHash string, prefilterHash string, usage *memorycore.LLMUsage, safe *safeError) (memorycore.ExtractionRunResult, error) {
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
	if runReq.Audit != memorycore.ExtractionAuditOff && r.audit != nil && !runReq.Force {
		previous, err := r.audit.FindSuccessfulRun(ctx, fingerprint, runReq.Mode)
		if err != nil {
			safe := sanitizedError("audit_lookup_failed", "could not read extraction audit state")
			return finish(result, "", "", "", "", nil, safe)
		}
		if previous != nil {
			result.Status = memorycore.ExtractionRunStatusSkipped
			result.SkippedByFingerprint = true
			return finish(result, "", "", "", "", nil, nil)
		}
	}

	req := cloneRequest(runReq.Request)
	var prefilterHash string
	var usage memorycore.LLMUsage
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
			result.Status = memorycore.ExtractionRunStatusSkipped
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
	resp, parseErr := extraction.ParseResponse(strings.NewReader(raw.Text))
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
		resp, parseErr = extraction.ParseResponse(strings.NewReader(repairRaw.Text))
		trace.recordRepairParseError(parseErr)
		result.Repaired = true
	}
	if parseErr != nil {
		trace.recordExtractionParseError(parseErr)
		safe := sanitizedError("parse_failed", "model response was not valid extraction JSON")
		return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, safe)
	}

	gate := extraction.ValidateExtraction(req, resp)
	result.GateResult = &gate
	result.AcceptedCount = gate.Summary.AcceptedFactCount
	result.ReviewCount = gate.Summary.NeedsReviewCount
	result.RejectedCount = gate.Summary.RejectedCount
	result.RoutedCount = gate.Summary.RoutedCount
	result.NotAppliedCount = gate.Summary.NotAppliedCount
	result.Usage = usage
	if gate.Status == "blocked" {
		result.Status = memorycore.ExtractionRunStatusBlocked
		return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, nil)
	}

	switch runReq.Mode {
	case memorycore.ExtractionRunModeValidate:
		result.Status = memorycore.ExtractionRunStatusValidated
	case memorycore.ExtractionRunModeDryRun:
		dry := extraction.DryRun(req, resp, gate)
		result.DryRunResult = &dry
		result.Status = memorycore.ExtractionRunStatusDryRun
	case memorycore.ExtractionRunModeApply:
		if runReq.RequireCleanGate && (gate.Summary.NeedsReviewCount > 0 || gate.Summary.RejectedCount > 0) {
			result.Status = memorycore.ExtractionRunStatusFailed
			safe := sanitizedError("unclean_gate", "gate contains review or rejected candidates")
			return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, safe)
		}
		apply := extraction.ApplyAcceptedFacts(ctx, r.service, r.db, req, resp, gate)
		result.ApplyResult = &apply
		result.AppliedCount = apply.AppliedCount
		result.FailureCount = len(apply.Failures)
		switch {
		case apply.Status == "applied":
			result.Status = memorycore.ExtractionRunStatusApplied
		case apply.Status == "nothing_applied":
			result.Status = memorycore.ExtractionRunStatusNothingApplied
		default:
			result.Status = memorycore.ExtractionRunStatusFailed
		}
	default:
		safe := sanitizedError("invalid_mode", "mode must be validate, dry-run, or apply")
		return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, safe)
	}
	return finish(result, promptHash, responseHash, repairedHash, prefilterHash, &usage, nil)
}

func (r *Runner) finish(ctx context.Context, start time.Time, runReq memorycore.ExtractionRunRequest, result memorycore.ExtractionRunResult, promptHash string, responseHash string, repairedHash string, prefilterHash string, usage *memorycore.LLMUsage, safe *safeError, trace *rawLogTrace) (memorycore.ExtractionRunResult, error) {
	result.DurationMS = time.Since(start).Milliseconds()
	if usage != nil {
		result.Usage = *usage
	}
	if safe != nil {
		result.SanitizedErrorCode = safe.Code
		result.SanitizedErrorMessage = safe.Message
		if result.Status == "" {
			result.Status = memorycore.ExtractionRunStatusFailed
		}
	}
	if runReq.Audit != memorycore.ExtractionAuditOff && r.audit != nil && result.Fingerprint != "" {
		record := memorycore.ExtractionRunAuditRecord{
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
			result.Status = memorycore.ExtractionRunStatusFailed
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
			result.Status = memorycore.ExtractionRunStatusFailed
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

func (r *Runner) fingerprint(ctx context.Context, req memorycore.ExtractionRunRequest) (string, error) {
	hashes, err := episodeContentHashes(ctx, r.db, req.Request.PersonaID, req.Request.Episodes)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"request_schema":           memorycore.ExtractionRequestSchemaVersion,
		"response_schema":          memorycore.ExtractionResponseSchemaVersion,
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
		"use_prefilter":            req.UsePreFilter,
		"repair_enabled":           req.RepairEnabled,
		"require_clean_gate":       req.RequireCleanGate,
		"provider_id":              req.ProviderID,
		"provider_kind":            req.ProviderKind,
		"model":                    req.Model,
		"provider_params": map[string]any{
			"temperature": req.Temperature,
			"max_tokens":  req.MaxTokens,
			"timeout":     req.Timeout.String(),
		},
		"mode": req.Mode,
	}
	return hashJSON(payload), nil
}

func episodeContentHashes(ctx context.Context, db *sql.DB, personaID string, episodes []memorycore.ExtractionEpisode) ([]map[string]string, error) {
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

func cloneRequest(req memorycore.ExtractionRequest) memorycore.ExtractionRequest {
	req.Episodes = append([]memorycore.ExtractionEpisode(nil), req.Episodes...)
	req.KnownEntities = append([]memorycore.ExtractionKnownEntity(nil), req.KnownEntities...)
	req.PredicateSchemas = append([]memorycore.ExtractionPredicateSchema(nil), req.PredicateSchemas...)
	req.ApprovedWorkCandidates = append([]memorycore.ExtractionWorkCandidate(nil), req.ApprovedWorkCandidates...)
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

func addUsage(a memorycore.LLMUsage, b memorycore.LLMUsage) memorycore.LLMUsage {
	a.PromptTokens += b.PromptTokens
	a.CompletionTokens += b.CompletionTokens
	a.TotalTokens += b.TotalTokens
	return a
}

func (r *Runner) buildExtractionLLMRequest(req memorycore.ExtractionRequest, runReq memorycore.ExtractionRunRequest) memorycore.ExtractionLLMRequest {
	requestJSON, _ := json.Marshal(req)
	return memorycore.ExtractionLLMRequest{
		Purpose:         memorycore.ExtractionLLMPurposeExtraction,
		ProviderID:      runReq.ProviderID,
		ProviderKind:    runReq.ProviderKind,
		Model:           runReq.Model,
		SystemPrompt:    extractionSystemPrompt(r.promptVersions.Extraction),
		DeveloperPrompt: extractionDeveloperPrompt(),
		UserPrompt:      string(requestJSON),
		Temperature:     runReq.Temperature,
		MaxTokens:       runReq.MaxTokens,
		Timeout:         runReq.Timeout,
		Metadata:        requestMetadata(memorycore.ExtractionLLMPurposeExtraction, req.RequestID, r.promptVersions.Extraction, memorycore.ExtractionResponseSchemaVersion),
	}
}

func (r *Runner) buildRepairLLMRequest(raw string, parseErr error, runReq memorycore.ExtractionRunRequest) memorycore.ExtractionLLMRequest {
	return memorycore.ExtractionLLMRequest{
		Purpose:         memorycore.ExtractionLLMPurposeRepair,
		ProviderID:      runReq.ProviderID,
		ProviderKind:    runReq.ProviderKind,
		Model:           runReq.Model,
		SystemPrompt:    repairSystemPrompt(r.promptVersions.Repair),
		DeveloperPrompt: repairDeveloperPrompt(parseErr),
		UserPrompt:      raw,
		Temperature:     runReq.Temperature,
		MaxTokens:       runReq.MaxTokens,
		Timeout:         runReq.Timeout,
		Metadata:        requestMetadata(memorycore.ExtractionLLMPurposeRepair, "", r.promptVersions.Repair, memorycore.ExtractionResponseSchemaVersion),
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
{"schema_version":"%s","request_id":"req_example","persona_id":"default","session_id":null,"trigger":"session_end","source_window":{"episode_ids":["ep_1"],"started_at":null,"ended_at":null},"entities":[],"facts":[{"candidate_id":"f1","subject_entity_candidate_id":"user","predicate":"likes","object_entity_candidate_id":null,"object_literal":"手冲咖啡","content_summary":"用户喜欢手冲咖啡。","fact_type":"stable_preference","valid_from":null,"valid_to":null,"temporal_precision":"unknown","extraction_confidence":"explicit","extraction_confidence_score":0.95,"importance":0.7,"valence":0.2,"arousal":0.2,"sensitivity_level":"normal","source_episode_ids":["ep_1"],"evidence_notes":"用户直接表达喜欢手冲咖啡。","reasoning":null,"operation_hint":"insert_candidate","pinned":false,"user_requested":false,"searchable_hint":true,"quality_decision":"accept_for_consolidation","quality_reasons":["explicit_user_statement"]}],"links":[],"affect_events":[],"deletion_intents":[],"pin_intents":[],"correction_hints":[],"rejected_candidates":[],"quality_flags":[],"gate_summary":{"accepted_fact_count":1,"needs_review_count":0,"rejected_count":0,"has_deletion_intent":false,"has_pin_intent":false,"requires_human_review":false,"notes":"通过"}}`, version, memorycore.ExtractionResponseSchemaVersion)
}

func extractionDeveloperPrompt() string {
	return "Return strict JSON matching schema " + memorycore.ExtractionResponseSchemaVersion + ". Top-level fields must include schema_version, request_id, persona_id, session_id, trigger, source_window, entities, facts, links, affect_events, deletion_intents, pin_intents, correction_hints, rejected_candidates, quality_flags, and gate_summary. Preserve IDs from the ExtractionRequest JSON in the user message.\n" + extractionFieldContract()
}

func repairSystemPrompt(version string) string {
	return fmt.Sprintf("MemoryCore JSON repair %s. Repair formatting/schema JSON only. Do not infer or add evidence.", version)
}

func repairDeveloperPrompt(parseErr error) string {
	message := ""
	if parseErr != nil {
		message = " Parser error to fix: " + parseErr.Error()
	}
	return "Return only one strict JSON object for schema " + memorycore.ExtractionResponseSchemaVersion + ". Do not include markdown fences." + message + "\n" + extractionFieldContract()
}

func extractionFieldContract() string {
	return `Field contract:
- Use Chinese for human-readable summaries and notes when the source conversation is Chinese: content_summary, evidence_notes, reasoning, gate_summary.notes, pin_reason, target_description, corrected_topic, and rejection reasons. Keep JSON field names, IDs, enum values, fact_type, operation_hint, quality_decision, sensitivity_level, and confidence labels as protocol values.
- Do not copy request.known_entities into response.entities. entity_id is input-only. Do not return entity_id; use "known_entity_id" for an existing entity or omit entities when using special ids "user" or "agent".
- response.entities fields only: "candidate_id", "canonical_name", "entity_type", "aliases", "description", "confidence", "source_episode_ids", "merge_hint", "known_entity_id", "sensitivity_level", "reasoning".
- response.affect_events fields only: "candidate_id", "scope", "label", "valence", "arousal", "source_episode_ids", "confidence", "reasoning". Do not return subject_entity_candidate_id, affect_type, intensity, trigger, context, operation_hint, quality_decision, or quality_reasons in affect_events.
- response.facts fields only: "candidate_id", "subject_entity_candidate_id", "predicate", "object_entity_candidate_id", "object_literal", "content_summary", "fact_type", "valid_from", "valid_to", "temporal_precision", "extraction_confidence", "extraction_confidence_score", "importance", "valence", "arousal", "sensitivity_level", "source_episode_ids", "evidence_notes", "reasoning", "operation_hint", "pinned", "user_requested", "searchable_hint", "quality_decision", "quality_reasons".`
}
