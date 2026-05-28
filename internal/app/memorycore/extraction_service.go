package memorycore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (s *service) RunExtraction(ctx context.Context, req RunExtractionRequest) (*ExtractionRunResult, error) {
	if s == nil || s.sqlDB == nil {
		return nil, extractionServiceError("service_not_ready", "memorycore service is not ready")
	}
	cfg := normalizeExtractionOptions(s.extraction)
	cfg.Provider = applyExtractionProviderOverride(cfg.Provider, req.Provider)
	cfg.Defaults = applyExtractionPolicyOverride(cfg.Defaults, req.Policy)
	cfg.Runtime = applyExtractionRuntimeOverride(cfg.Runtime, req.Runtime)
	rawLog := cfg.RawLog
	if req.RawLog != nil {
		rawLog = *req.RawLog
	}
	audit, err := normalizeExtractionAudit("", cfg.Audit.Enabled)
	if err != nil {
		return nil, extractionServiceError("invalid_audit", err.Error())
	}
	if req.Runtime.Audit != nil {
		audit, err = normalizeExtractionAudit(*req.Runtime.Audit, true)
		if err != nil {
			return nil, extractionServiceError("invalid_audit", err.Error())
		}
	}
	if !cfg.Enabled {
		return nil, extractionServiceError("extraction_disabled", "memory extraction is disabled")
	}
	if strings.TrimSpace(cfg.Provider.Kind) == "" || cfg.Provider.Kind == ExtractionProviderDisabled {
		return nil, extractionServiceError("extraction_provider_disabled", "memory extraction provider is disabled")
	}
	extractionReq, selector, err := s.resolveExtractionRequest(ctx, req, cfg)
	if err != nil {
		return nil, extractionServiceError("build_request_failed", "could not build extraction request")
	}
	llm, err := newExtractionLLM(cfg.Provider)
	if err != nil {
		return nil, err
	}
	var auditStore AuditStore
	if audit != ExtractionAuditOff {
		auditStore = NewSQLiteAuditStore(s.sqlDB)
	}
	mode := req.Mode
	if mode == "" {
		mode = cfg.Defaults.Mode
	}
	runReq := ExtractionRunRequest{
		Request:          *extractionReq,
		Mode:             mode,
		ProviderID:       firstNonEmpty(cfg.Provider.ID, cfg.Provider.Kind),
		ProviderKind:     cfg.Provider.Kind,
		Model:            cfg.Provider.Model,
		Temperature:      cfg.Provider.Temperature,
		MaxTokens:        cfg.Provider.MaxTokens,
		Timeout:          cfg.Provider.Timeout,
		ResponseFormat:   cfg.Provider.ResponseFormat,
		UsePreFilter:     cfg.Runtime.UsePreFilter,
		RepairEnabled:    cfg.Runtime.RepairEnabled,
		RequireCleanGate: cfg.Defaults.RequireCleanGate,
		Audit:            audit,
		Force:            req.Force || cfg.Audit.Force,
		RawLog:           rawLog,
		Window: ExtractionRunWindow{
			EpisodeIDs: selector.EpisodeIDs,
			Since:      selector.Since,
			Until:      selector.Until,
			Limit:      selector.Limit,
		},
	}
	if !cfg.Defaults.ApplyAcceptedFacts && runReq.Mode == ExtractionRunModeApply {
		runReq.Mode = ExtractionRunModeDryRun
	}
	runner := NewRunner(RunnerOptions{
		DB:         s.sqlDB,
		Service:    s,
		LLM:        llm,
		AuditStore: auditStore,
		Now:        s.now,
		PromptVersions: PromptVersions{
			Extraction: cfg.PromptVersions.Extraction,
			PreFilter:  cfg.PromptVersions.PreFilter,
			Repair:     cfg.PromptVersions.Repair,
		},
	})
	result, err := runner.Run(ctx, runReq)
	enrichExtractionRunResult(&result)
	if err != nil {
		code := result.SanitizedErrorCode
		if code == "" {
			code = "runner_failed"
		}
		message := result.SanitizedErrorMessage
		if message == "" {
			message = err.Error()
		}
		return &result, extractionServiceError(code, message)
	}
	return &result, nil
}

func (s *service) RunExtractionBatch(ctx context.Context, req ExtractionBatchRequest) (*ExtractionBatchResult, error) {
	if s == nil || s.sqlDB == nil {
		return nil, extractionServiceError("service_not_ready", "memorycore service is not ready")
	}
	cfg := normalizeExtractionOptions(s.extraction)
	if req.Mode == "" {
		req.Mode = cfg.Defaults.Mode
	}
	if req.Trigger == "" {
		req.Trigger = ExtractionTriggerSessionEnd
	}
	if req.Timezone == "" {
		req.Timezone = cfg.Defaults.Timezone
	}
	if req.EpisodeLimit == 0 {
		req.EpisodeLimit = 50
	}
	personaID := defaultString(req.PersonaID, s.persona)
	sessionIDs := append([]string(nil), req.SessionIDs...)
	var err error
	if len(sessionIDs) == 0 {
		sessionIDs, err = eligibleSessions(ctx, s.sqlDB, personaID, req)
		if err != nil {
			return &ExtractionBatchResult{Mode: req.Mode, Status: "failed"}, err
		}
	}
	result := ExtractionBatchResult{Mode: req.Mode, Status: "ok", Results: []ExtractionRunResult{}}
	for _, sessionID := range sessionIDs {
		sid := sessionID
		run, runErr := s.RunExtraction(ctx, RunExtractionRequest{
			PersonaID: personaID,
			SessionID: &sid,
			Trigger:   req.Trigger,
			Timezone:  req.Timezone,
			Mode:      req.Mode,
			Build: &ExtractionBuildSelector{
				SessionID: &sid,
				Since:     req.Since,
				Until:     req.Until,
				Limit:     req.EpisodeLimit,
			},
			Policy: ExtractionPolicyOverride{
				AllowSensitiveExtraction: trueBoolPtr(req.AllowSensitiveExtraction),
				AllowInference:           trueBoolPtr(req.AllowInference),
				ManualPin:                trueBoolPtr(req.ManualPin),
				ManualForget:             trueBoolPtr(req.ManualForget),
				MaxFacts:                 intPtr(req.MaxFacts),
				MaxLinks:                 intPtr(req.MaxLinks),
				RequireCleanGate:         trueBoolPtr(req.RequireCleanGate),
			},
			Runtime: ExtractionRuntimeOverride{
				UsePreFilter:  trueBoolPtr(req.UsePreFilter),
				RepairEnabled: trueBoolPtr(req.RepairEnabled),
				Audit:         stringPtrValue(req.Audit),
			},
			Provider: ExtractionProviderOverride{
				Kind:        req.ProviderKind,
				ID:          req.ProviderID,
				Model:       req.Model,
				Temperature: floatPtr(req.Temperature),
				MaxTokens:   intPtr(req.MaxTokens),
				Timeout:     req.Timeout,
			},
			Force:  req.Force,
			RawLog: extractionBatchRawLogOverride(req.RawLog),
		})
		if run == nil {
			run = &ExtractionRunResult{PersonaID: personaID, SessionID: &sid, Trigger: req.Trigger, Mode: req.Mode, Status: ExtractionRunStatusFailed}
		}
		if run.SkippedByFingerprint {
			result.SkippedCount++
		} else if runErr != nil || run.Status == ExtractionRunStatusFailed {
			result.FailedCount++
		} else {
			result.ProcessedCount++
		}
		result.Results = append(result.Results, *run)
		if runErr != nil && req.StopOnError {
			result.Status = "failed"
			return &result, runErr
		}
	}
	if result.FailedCount > 0 {
		result.Status = "partial_failure"
	}
	return &result, nil
}

func extractionBatchRawLogOverride(rawLog ExtractionRawLogOptions) *ExtractionRawLogOptions {
	if rawLog.Enabled || strings.TrimSpace(rawLog.Directory) != "" {
		return &rawLog
	}
	return nil
}

func (s *service) resolveExtractionRequest(ctx context.Context, req RunExtractionRequest, cfg ExtractionOptions) (*ExtractionRequest, ExtractionBuildSelector, error) {
	if req.Request != nil {
		cloned := cloneRequest(*req.Request)
		if cloned.PersonaID == "" {
			cloned.PersonaID = defaultString(req.PersonaID, s.persona)
		}
		if cloned.Trigger == "" {
			cloned.Trigger = defaultString(req.Trigger, ExtractionTriggerSessionEnd)
		}
		if req.RequestID != "" {
			cloned.RequestID = req.RequestID
		}
		if err := validateExplicitExtractionRequest(ctx, s.sqlDB, cloned); err != nil {
			return nil, ExtractionBuildSelector{}, err
		}
		return &cloned, ExtractionBuildSelector{EpisodeIDs: episodeIDsFromExtractionRequest(cloned), SessionID: cloned.SessionID, Limit: len(cloned.Episodes)}, nil
	}
	selector := ExtractionBuildSelector{}
	if req.Build != nil {
		selector = *req.Build
	}
	if selector.SessionID == nil {
		selector.SessionID = req.SessionID
	}
	if selector.Limit == 0 {
		selector.Limit = 50
	}
	built, err := BuildRequest(ctx, s.sqlDB, BuildRequestOptions{
		PersonaID:                defaultString(req.PersonaID, s.persona),
		SessionID:                selector.SessionID,
		EpisodeIDs:               append([]string(nil), selector.EpisodeIDs...),
		Trigger:                  defaultString(req.Trigger, ExtractionTriggerSessionEnd),
		Limit:                    selector.Limit,
		Since:                    selector.Since,
		Until:                    selector.Until,
		Timezone:                 defaultString(req.Timezone, cfg.Defaults.Timezone),
		AllowSensitiveExtraction: cfg.Defaults.AllowSensitiveExtraction,
		AllowInference:           cfg.Defaults.AllowInference,
		ManualPin:                boolValue(req.Policy.ManualPin),
		ManualForget:             boolValue(req.Policy.ManualForget),
		MaxFacts:                 cfg.Defaults.MaxFacts,
		MaxLinks:                 cfg.Defaults.MaxLinks,
		Now:                      s.now(),
	})
	if err != nil {
		return nil, selector, err
	}
	if req.RequestID != "" {
		built.RequestID = req.RequestID
	}
	return &built, selector, nil
}

func validateExplicitExtractionRequest(ctx context.Context, db *sql.DB, req ExtractionRequest) error {
	if req.SchemaVersion != ExtractionRequestSchemaVersion {
		return fmt.Errorf("schema_version must be %s", ExtractionRequestSchemaVersion)
	}
	if strings.TrimSpace(req.PersonaID) == "" {
		return fmt.Errorf("persona_id is required")
	}
	if strings.TrimSpace(req.RequestID) == "" {
		return fmt.Errorf("request_id is required")
	}
	if !validExtractionTrigger(req.Trigger) {
		return fmt.Errorf("trigger is invalid")
	}
	if len(req.Episodes) == 0 {
		return fmt.Errorf("episodes are required")
	}
	if req.Trigger == ExtractionTriggerManualForget && !req.Policy.ManualForget {
		return fmt.Errorf("manual_forget trigger requires manual_forget policy")
	}
	if req.Trigger == ExtractionTriggerManualPin && !req.Policy.ManualPin {
		return fmt.Errorf("manual_pin trigger requires manual_pin policy")
	}
	for _, episode := range req.Episodes {
		if episode.VisibilityStatus != VisibilityVisible {
			return fmt.Errorf("episode %s is not visible", episode.EpisodeID)
		}
		if db != nil {
			var count int
			err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM episodes
WHERE persona_id = ? AND id = ? AND visibility_status = 'visible' AND searchable = 1`, req.PersonaID, episode.EpisodeID).Scan(&count)
			if err != nil {
				return err
			}
			if count == 0 {
				return fmt.Errorf("episode %s is missing or ineligible", episode.EpisodeID)
			}
		}
	}
	return nil
}

func newExtractionLLM(provider ExtractionProviderOptions) (ExtractionLLM, error) {
	switch provider.Kind {
	case ExtractionProviderMock:
		return NewDeterministicMockLLM(), nil
	case ExtractionProviderOpenAICompatible:
		if strings.TrimSpace(provider.BaseURL) == "" || strings.TrimSpace(provider.Model) == "" || strings.TrimSpace(provider.APIKeyEnv) == "" {
			return nil, extractionServiceError("provider_misconfigured", "openai-compatible extraction provider requires base_url, model, and api_key_env")
		}
		return NewOpenAICompatibleLLM(OpenAICompatibleOptions{
			BaseURL:        provider.BaseURL,
			APIKeyEnv:      provider.APIKeyEnv,
			Model:          provider.Model,
			Timeout:        provider.Timeout,
			Temperature:    provider.Temperature,
			MaxTokens:      provider.MaxTokens,
			ResponseFormat: provider.ResponseFormat,
			Thinking:       provider.Thinking,
		}), nil
	default:
		return nil, extractionServiceError("unsupported_provider", "unsupported extraction provider")
	}
}

func enrichExtractionRunResult(result *ExtractionRunResult) {
	if result == nil {
		return
	}
	if result.DryRunResult != nil {
		result.RoutedDeletionIntents = append([]DeletionIntentRoute(nil), result.DryRunResult.RoutedDeletionIntents...)
		result.RoutedPinIntents = append([]PinIntentRoute(nil), result.DryRunResult.RoutedPinIntents...)
		return
	}
	if result.GateResult == nil {
		return
	}
	for _, decision := range result.GateResult.DeletionIntentDecisions {
		result.RoutedDeletionIntents = append(result.RoutedDeletionIntents, DeletionIntentRoute{
			CandidateID: decision.CandidateID,
			RouteTo:     "forget_manager",
			Decision:    decision.Decision,
		})
	}
	for _, decision := range result.GateResult.PinIntentDecisions {
		result.RoutedPinIntents = append(result.RoutedPinIntents, PinIntentRoute{
			CandidateID: decision.CandidateID,
			Decision:    decision.Decision,
		})
	}
}

func episodeIDsFromExtractionRequest(req ExtractionRequest) []string {
	ids := make([]string, 0, len(req.Episodes))
	for _, episode := range req.Episodes {
		ids = append(ids, episode.EpisodeID)
	}
	return ids
}

func intPtr(value int) *int {
	if value == 0 {
		return nil
	}
	return &value
}

func floatPtr(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

func stringPtrValue(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func trueBoolPtr(value bool) *bool {
	if !value {
		return nil
	}
	return &value
}
