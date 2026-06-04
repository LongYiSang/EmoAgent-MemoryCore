package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type extractionRuntimeFlags struct {
	commonOptions
	SessionID           string
	Trigger             string
	SinceValue          string
	UntilValue          string
	Timezone            string
	EpisodeIDs          stringList
	Limit               int
	SessionLimit        int
	EpisodeLimit        int
	MaxFacts            int
	MaxLinks            int
	AllowSensitive      bool
	AllowInference      bool
	ManualPin           bool
	ManualForget        bool
	Mode                string
	Provider            string
	ProviderID          string
	BaseURL             string
	APIKeyEnv           string
	Model               string
	Temperature         float64
	MaxTokens           int
	Timeout             time.Duration
	ResponseFormat      string
	UsePreFilter        bool
	Repair              bool
	Audit               string
	Force               bool
	RawLog              bool
	RawLogDir           string
	StopOnError         bool
	AllowPartialFailure bool
	RequireCleanGate    bool
	ExtractionEnabled   bool
}

func runExtractRun(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("extract-run", stderr)
	flags := parseExtractionRuntimeFlags(fs, formatJSON)
	if !parseFlags(fs, args) {
		return 2
	}
	explicit := explicitFlagNames(fs)
	cfg, hasConfig, err := loadCommandConfig(flags.commonOptions)
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if hasConfig {
		applyExtractionRuntimeConfig(flags, &cfg, explicit, stderr)
		if err := cfg.Validate(); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	} else if explicit["raw-log-dir"] {
		flags.RawLog = true
	}
	if code := validateExtractionRuntimeFlags(stderr, fs, flags, false); code != 0 {
		return code
	}
	ctx := context.Background()
	since, err := parseOptionalTimePtr(flags.SinceValue, "--since")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	until, err := parseOptionalTimePtr(flags.UntilValue, "--until")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	svc, cleanup, err := openExtractionService(ctx, flags.commonOptions, extractionOptionsFromFlags(flags))
	if err != nil {
		return runtimeError(stderr, "%v", err)
	}
	defer cleanup()
	result, err := svc.RunExtraction(ctx, memorycore.RunExtractionRequest{
		PersonaID: flags.PersonaID,
		SessionID: stringPtr(flags.SessionID),
		Trigger:   flags.Trigger,
		Timezone:  flags.Timezone,
		Mode:      memorycore.ExtractionRunMode(flags.Mode),
		Build: &memorycore.ExtractionBuildSelector{
			EpisodeIDs: []string(flags.EpisodeIDs),
			SessionID:  stringPtr(flags.SessionID),
			Since:      since,
			Until:      until,
			Limit:      flags.Limit,
		},
		Policy: extractionPolicyOverrideFromFlags(flags),
		Runtime: memorycore.ExtractionRuntimeOverride{
			UsePreFilter:  boolPtr(flags.UsePreFilter),
			RepairEnabled: boolPtr(flags.Repair),
			Audit:         stringPtr(flags.Audit),
		},
		Provider: extractionProviderOverrideFromFlags(flags),
		Force:    flags.Force,
		RawLog:   &memorycore.ExtractionRawLogOptions{Enabled: flags.RawLog, Directory: flags.RawLogDir},
	})
	if result == nil {
		result = &memorycore.ExtractionRunResult{
			PersonaID: flags.PersonaID,
			SessionID: stringPtr(flags.SessionID),
			Trigger:   flags.Trigger,
			Mode:      memorycore.ExtractionRunMode(flags.Mode),
			Status:    memorycore.ExtractionRunStatusFailed,
		}
	}
	if flags.Format == formatJSON {
		writeJSON(stdout, *result, flags.Pretty)
	} else {
		writeExtractionRunText(stdout, *result)
	}
	if err != nil || result.Status == memorycore.ExtractionRunStatusFailed || result.Status == memorycore.ExtractionRunStatusBlocked {
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
		}
		return 1
	}
	return 0
}

func runExtractBatch(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("extract-batch", stderr)
	flags := parseExtractionRuntimeFlags(fs, formatJSON)
	if !parseFlags(fs, args) {
		return 2
	}
	explicit := explicitFlagNames(fs)
	cfg, hasConfig, err := loadCommandConfig(flags.commonOptions)
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if hasConfig {
		applyExtractionRuntimeConfig(flags, &cfg, explicit, stderr)
		if err := cfg.Validate(); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	} else if explicit["raw-log-dir"] {
		flags.RawLog = true
	}
	if code := validateExtractionRuntimeFlags(stderr, fs, flags, true); code != 0 {
		return code
	}
	ctx := context.Background()
	since, err := parseOptionalTimePtr(flags.SinceValue, "--since")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	until, err := parseOptionalTimePtr(flags.UntilValue, "--until")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	sessionIDs := []string{}
	if strings.TrimSpace(flags.SessionID) != "" {
		sessionIDs = append(sessionIDs, flags.SessionID)
	}
	svc, cleanup, err := openExtractionService(ctx, flags.commonOptions, extractionOptionsFromFlags(flags))
	if err != nil {
		return runtimeError(stderr, "%v", err)
	}
	defer cleanup()
	result, err := svc.RunExtractionBatch(ctx, memorycore.ExtractionBatchRequest{
		PersonaID:                flags.PersonaID,
		SessionIDs:               sessionIDs,
		Trigger:                  flags.Trigger,
		Mode:                     memorycore.ExtractionRunMode(flags.Mode),
		ProviderID:               firstNonEmptyCLI(flags.ProviderID, flags.Provider),
		ProviderKind:             flags.Provider,
		Model:                    flags.Model,
		Temperature:              flags.Temperature,
		MaxTokens:                flags.MaxTokens,
		Timeout:                  flags.Timeout,
		Limit:                    flags.SessionLimit,
		EpisodeLimit:             flags.EpisodeLimit,
		Timezone:                 flags.Timezone,
		AllowSensitiveExtraction: flags.AllowSensitive,
		AllowInference:           flags.AllowInference,
		ManualPin:                flags.ManualPin,
		ManualForget:             flags.ManualForget,
		MaxFacts:                 flags.MaxFacts,
		MaxLinks:                 flags.MaxLinks,
		Since:                    since,
		Until:                    until,
		UsePreFilter:             flags.UsePreFilter,
		RepairEnabled:            flags.Repair,
		RequireCleanGate:         flags.RequireCleanGate,
		Audit:                    flags.Audit,
		Force:                    flags.Force,
		StopOnError:              flags.StopOnError,
		AllowPartialFailure:      flags.AllowPartialFailure,
		RawLog:                   memorycore.ExtractionRawLogOptions{Enabled: flags.RawLog, Directory: flags.RawLogDir},
	})
	if result == nil {
		result = &memorycore.ExtractionBatchResult{Mode: memorycore.ExtractionRunMode(flags.Mode), Status: "failed"}
	}
	if flags.Format == formatJSON {
		writeJSON(stdout, *result, flags.Pretty)
	} else {
		fmt.Fprintf(stdout, "status=%s processed_count=%d skipped_count=%d failed_count=%d\n", result.Status, result.ProcessedCount, result.SkippedCount, result.FailedCount)
	}
	if err != nil || result.Status == "failed" || (result.Status == "partial_failure" && !flags.AllowPartialFailure) {
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
		}
		return 1
	}
	return 0
}

func parseExtractionRuntimeFlags(fs *flag.FlagSet, defaultFormat string) *extractionRuntimeFlags {
	flags := &extractionRuntimeFlags{Mode: string(memorycore.ExtractionRunModeDryRun), Provider: "mock", ProviderID: "mock", Trigger: memorycore.ExtractionTriggerSessionEnd, Timezone: "Asia/Shanghai", Limit: 50, SessionLimit: 50, EpisodeLimit: 50, MaxFacts: 12, MaxLinks: 20, AllowInference: true, Repair: true, Audit: memorycore.ExtractionAuditOn, APIKeyEnv: "MEMORYCORE_LLM_API_KEY", Timeout: 60 * time.Second, MaxTokens: 4096, ResponseFormat: string(memorycore.ExtractionResponseFormatJSONSchema), ExtractionEnabled: true}
	addCommonFlags(fs, &flags.commonOptions, defaultFormat)
	addConfigFlag(fs, &flags.commonOptions)
	fs.StringVar(&flags.SessionID, "session", "", "session id")
	fs.Var(&flags.EpisodeIDs, "episode", "episode id; repeatable")
	fs.StringVar(&flags.Trigger, "trigger", memorycore.ExtractionTriggerSessionEnd, "extraction trigger")
	fs.IntVar(&flags.Limit, "limit", 50, "single-run episode limit")
	fs.IntVar(&flags.SessionLimit, "session-limit", 50, "maximum sessions for extract-batch")
	fs.IntVar(&flags.EpisodeLimit, "episode-limit", 50, "maximum episodes per session for extract-batch")
	fs.StringVar(&flags.SinceValue, "since", "", "RFC3339 lower occurrence bound")
	fs.StringVar(&flags.UntilValue, "until", "", "RFC3339 upper occurrence bound")
	fs.StringVar(&flags.Timezone, "timezone", "Asia/Shanghai", "request timezone")
	fs.BoolVar(&flags.AllowSensitive, "allow-sensitive-extraction", false, "allow highly sensitive extraction without review")
	fs.BoolVar(&flags.AllowInference, "allow-inference", true, "allow inferred candidates")
	fs.BoolVar(&flags.ManualPin, "manual-pin", false, "mark request policy as manual pin")
	fs.BoolVar(&flags.ManualForget, "manual-forget", false, "mark request policy as manual forget")
	fs.IntVar(&flags.MaxFacts, "max-facts", 12, "maximum fact candidates")
	fs.IntVar(&flags.MaxLinks, "max-links", 20, "maximum link candidates")
	fs.StringVar(&flags.Mode, "mode", string(memorycore.ExtractionRunModeDryRun), "validate|dry-run|apply")
	fs.StringVar(&flags.Provider, "provider", "mock", "mock|openai-compatible|disabled")
	fs.StringVar(&flags.BaseURL, "base-url", "", "OpenAI-compatible base URL")
	fs.StringVar(&flags.APIKeyEnv, "api-key-env", "MEMORYCORE_LLM_API_KEY", "environment variable containing provider API key")
	fs.StringVar(&flags.Model, "model", "", "model name")
	fs.Float64Var(&flags.Temperature, "temperature", 0, "LLM temperature")
	fs.IntVar(&flags.MaxTokens, "max-tokens", 4096, "maximum output tokens")
	fs.DurationVar(&flags.Timeout, "timeout", 60*time.Second, "provider timeout")
	fs.StringVar(&flags.ResponseFormat, "response-format", string(memorycore.ExtractionResponseFormatJSONSchema), "json_object|json_schema")
	fs.BoolVar(&flags.UsePreFilter, "prefilter", false, "run extraction prefilter before extractor")
	fs.BoolVar(&flags.Repair, "repair", true, "repair invalid JSON once")
	fs.StringVar(&flags.Audit, "audit", memorycore.ExtractionAuditOn, "on|off; dry-run does not write memory but may write audit rows")
	fs.BoolVar(&flags.Force, "force", false, "rerun even if the fingerprint already succeeded")
	fs.BoolVar(&flags.RawLog, "raw-log", false, "write one raw extraction debug log JSON file per run")
	fs.StringVar(&flags.RawLogDir, "raw-log-dir", "", "directory for raw extraction debug log JSON files")
	fs.BoolVar(&flags.StopOnError, "stop-on-error", false, "stop batch at first error")
	fs.BoolVar(&flags.AllowPartialFailure, "allow-partial-failure", false, "return zero for extract-batch partial_failure")
	fs.BoolVar(&flags.RequireCleanGate, "require-clean-gate", false, "apply only if gate has no review or rejected candidates")
	return flags
}

func validateExtractionRuntimeFlags(stderr io.Writer, fs *flag.FlagSet, flags *extractionRuntimeFlags, batch bool) int {
	if !requireDB(stderr, fs, flags.DBPath) {
		return 2
	}
	if strings.TrimSpace(flags.RawLogDir) != "" {
		flags.RawLog = true
	}
	if flags.RawLog && strings.TrimSpace(flags.RawLogDir) == "" {
		return usageError(stderr, fs, "--raw-log-dir is required when --raw-log is set")
	}
	if err := validateFormat(flags.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--mode", flags.Mode, string(memorycore.ExtractionRunModeValidate), string(memorycore.ExtractionRunModeDryRun), string(memorycore.ExtractionRunModeApply)); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--provider", flags.Provider, "mock", "openai-compatible", "disabled"); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if flags.Provider == "openai-compatible" {
		if strings.TrimSpace(flags.BaseURL) == "" {
			return usageError(stderr, fs, "--base-url is required")
		}
		if strings.TrimSpace(flags.Model) == "" {
			return usageError(stderr, fs, "--model is required")
		}
		if strings.TrimSpace(flags.APIKeyEnv) == "" {
			return usageError(stderr, fs, "--api-key-env is required")
		}
		if strings.TrimSpace(os.Getenv(flags.APIKeyEnv)) == "" {
			return usageError(stderr, fs, fmt.Sprintf("api key env %s is not set", flags.APIKeyEnv))
		}
	}
	if err := validateOneOf("--audit", flags.Audit, memorycore.ExtractionAuditOn, memorycore.ExtractionAuditOff); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--response-format", flags.ResponseFormat, string(memorycore.ExtractionResponseFormatJSONObject), string(memorycore.ExtractionResponseFormatJSONSchema)); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if batch && explicitFlagNames(fs)["limit"] {
		return usageError(stderr, fs, "--limit is only supported by extract-run; use --session-limit for extract-batch")
	}
	if err := validateExtractionTriggerFlag(flags.Trigger); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if !batch && strings.TrimSpace(flags.SessionID) == "" && len(flags.EpisodeIDs) == 0 {
		return usageError(stderr, fs, "--session or --episode is required")
	}
	return 0
}

func applyExtractionRuntimeConfig(flags *extractionRuntimeFlags, cfg *memconfig.Config, explicit map[string]bool, stderr io.Writer) {
	applyCommonConfig(&flags.commonOptions, cfg, explicit, stderr)
	flags.ExtractionEnabled = cfg.Pipelines.Extraction.Enabled
	if !cfg.Pipelines.Extraction.Enabled && !explicit["provider"] {
		flags.Provider = memorycore.ExtractionProviderDisabled
		flags.ProviderID = cfg.Pipelines.Extraction.ProviderID
	} else if !explicit["provider"] && strings.TrimSpace(cfg.Pipelines.Extraction.ProviderID) != "" {
		flags.ProviderID = cfg.Pipelines.Extraction.ProviderID
		if provider := cfg.ProviderByID(cfg.Pipelines.Extraction.ProviderID); provider != nil {
			flags.Provider = cliExtractionProviderKind(*provider)
			flags.BaseURL = provider.BaseURL
			flags.APIKeyEnv = firstNonEmptyCLI(provider.APIKeyEnv, flags.APIKeyEnv)
			if provider.TimeoutMS > 0 {
				flags.Timeout = time.Duration(provider.TimeoutMS) * time.Millisecond
			}
		}
	}
	if explicit["provider"] {
		if strings.TrimSpace(cfg.Pipelines.Extraction.ProviderID) != "" {
			warnConfigOverride(stderr, "provider", "pipelines.extraction.provider_id")
		}
		flags.ProviderID = flags.Provider
		flags.ExtractionEnabled = flags.Provider != memorycore.ExtractionProviderDisabled
	}
	if explicit["mode"] {
		warnConfigOverride(stderr, "mode", "pipelines.extraction.mode")
	} else if strings.TrimSpace(cfg.Pipelines.Extraction.Mode) != "" {
		flags.Mode = cliExtractionConfigMode(cfg.Pipelines.Extraction.Mode)
	}
	if explicit["base-url"] {
		warnConfigOverride(stderr, "base-url", "providers.llm[].base_url")
	} else if strings.TrimSpace(flags.BaseURL) == "" {
		if provider := cfg.ProviderByID(cfg.Pipelines.Extraction.ProviderID); provider != nil {
			flags.BaseURL = provider.BaseURL
		}
	}
	if explicit["api-key-env"] {
		warnConfigOverride(stderr, "api-key-env", "providers.llm[].api_key_env")
	} else if provider := cfg.ProviderByID(cfg.Pipelines.Extraction.ProviderID); provider != nil && strings.TrimSpace(provider.APIKeyEnv) != "" {
		flags.APIKeyEnv = provider.APIKeyEnv
	}
	if explicit["model"] {
		warnConfigOverride(stderr, "model", "pipelines.extraction.model")
	} else if strings.TrimSpace(cfg.Pipelines.Extraction.Model) != "" {
		flags.Model = cfg.Pipelines.Extraction.Model
	}
	if explicit["temperature"] {
		warnConfigOverride(stderr, "temperature", "pipelines.extraction.params.temperature")
	} else {
		flags.Temperature = cfg.Pipelines.Extraction.Params.Temperature
	}
	if explicit["max-tokens"] {
		warnConfigOverride(stderr, "max-tokens", "pipelines.extraction.params.max_output_tokens")
	} else if cfg.Pipelines.Extraction.Params.MaxOutputTokens > 0 {
		flags.MaxTokens = cfg.Pipelines.Extraction.Params.MaxOutputTokens
	}
	if explicit["timeout"] {
		warnConfigOverride(stderr, "timeout", "providers.llm[].timeout_ms")
	}
	if explicit["response-format"] {
		warnConfigOverride(stderr, "response-format", "pipelines.extraction.params.response_format")
	} else if strings.TrimSpace(cfg.Pipelines.Extraction.Params.ResponseFormat) != "" {
		flags.ResponseFormat = cfg.Pipelines.Extraction.Params.ResponseFormat
	}
	if explicit["timezone"] {
		warnConfigOverride(stderr, "timezone", "core.timezone")
	} else if strings.TrimSpace(cfg.Core.Timezone) != "" {
		flags.Timezone = cfg.Core.Timezone
	}
	if explicit["allow-sensitive-extraction"] {
		warnConfigOverride(stderr, "allow-sensitive-extraction", "write_policy.extraction.allow_sensitive_extraction")
	} else {
		flags.AllowSensitive = cfg.WritePolicy.Extraction.AllowSensitiveExtraction
	}
	if explicit["allow-inference"] {
		warnConfigOverride(stderr, "allow-inference", "write_policy.extraction.allow_inference")
	} else {
		flags.AllowInference = cfg.WritePolicy.Extraction.AllowInference
	}
	if explicit["max-facts"] {
		warnConfigOverride(stderr, "max-facts", "write_policy.extraction.max_facts_per_request")
	} else if cfg.WritePolicy.Extraction.MaxFactsPerRequest > 0 {
		flags.MaxFacts = cfg.WritePolicy.Extraction.MaxFactsPerRequest
	}
	if explicit["max-links"] {
		warnConfigOverride(stderr, "max-links", "write_policy.extraction.max_links_per_request")
	} else if cfg.WritePolicy.Extraction.MaxLinksPerRequest > 0 {
		flags.MaxLinks = cfg.WritePolicy.Extraction.MaxLinksPerRequest
	}
	if explicit["prefilter"] {
		warnConfigOverride(stderr, "prefilter", "pipelines.prefilter.enabled")
	} else {
		flags.UsePreFilter = cfg.Pipelines.Prefilter.Enabled
	}
	if explicit["repair"] {
		warnConfigOverride(stderr, "repair", "pipelines.extraction_repair.enabled")
	} else {
		flags.Repair = cfg.Pipelines.ExtractionRepair.Enabled || cfg.Pipelines.Extraction.RetryOnSchemaFailure > 0
	}
	if cfg.Pipelines.Extraction.Audit.Enabled && !explicit["audit"] {
		flags.Audit = memorycore.ExtractionAuditOn
	} else if !cfg.Pipelines.Extraction.Audit.Enabled && !explicit["audit"] {
		flags.Audit = memorycore.ExtractionAuditOff
	}
	if !explicit["force"] {
		flags.Force = cfg.Pipelines.Extraction.Audit.Force
	}
	if explicit["raw-log"] {
		warnConfigOverride(stderr, "raw-log", "pipelines.extraction.raw_log.enabled")
		cfg.Pipelines.Extraction.RawLog.Enabled = flags.RawLog
	} else {
		flags.RawLog = cfg.Pipelines.Extraction.RawLog.Enabled
	}
	if explicit["raw-log-dir"] {
		warnConfigOverride(stderr, "raw-log-dir", "pipelines.extraction.raw_log.directory")
		flags.RawLog = true
		cfg.Pipelines.Extraction.RawLog.Enabled = true
		cfg.Pipelines.Extraction.RawLog.Directory = flags.RawLogDir
	} else {
		flags.RawLogDir = cfg.Pipelines.Extraction.RawLog.Directory
	}
}

func openExtractionService(ctx context.Context, opts commonOptions, extraction memorycore.ExtractionOptions) (memorycore.Service, func(), error) {
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      opts.DBPath,
		PersonaID:   opts.PersonaID,
		AutoMigrate: opts.AutoMigrate,
		EnableFTS:   opts.EnableFTS,
		Extraction:  extraction,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("open memorycore: %w", err)
	}
	return svc, func() { _ = svc.Close() }, nil
}

func extractionOptionsFromFlags(flags *extractionRuntimeFlags) memorycore.ExtractionOptions {
	return memorycore.ExtractionOptions{
		Enabled: flags.ExtractionEnabled,
		Provider: memorycore.ExtractionProviderOptions{
			Kind:           flags.Provider,
			ID:             firstNonEmptyCLI(flags.ProviderID, flags.Provider),
			BaseURL:        flags.BaseURL,
			APIKeyEnv:      flags.APIKeyEnv,
			Model:          flags.Model,
			Temperature:    flags.Temperature,
			MaxTokens:      flags.MaxTokens,
			Timeout:        flags.Timeout,
			ResponseFormat: memorycore.ExtractionResponseFormat(flags.ResponseFormat),
		},
		Defaults: memorycore.ExtractionDefaults{
			Configured:               true,
			Mode:                     memorycore.ExtractionRunMode(flags.Mode),
			Timezone:                 flags.Timezone,
			AllowSensitiveExtraction: flags.AllowSensitive,
			AllowInference:           flags.AllowInference,
			MaxFacts:                 flags.MaxFacts,
			MaxLinks:                 flags.MaxLinks,
			RequireCleanGate:         flags.RequireCleanGate,
			ApplyAcceptedFacts:       true,
			ExecuteDeletionIntents:   false,
		},
		Runtime: memorycore.ExtractionRuntimeOptions{
			Configured:    true,
			UsePreFilter:  flags.UsePreFilter,
			RepairEnabled: flags.Repair,
		},
		Audit: memorycore.ExtractionAuditOptions{
			Configured: true,
			Enabled:    flags.Audit != memorycore.ExtractionAuditOff,
			Force:      flags.Force,
		},
		RawLog: memorycore.ExtractionRawLogOptions{
			Enabled:   flags.RawLog,
			Directory: flags.RawLogDir,
		},
	}
}

func extractionPolicyOverrideFromFlags(flags *extractionRuntimeFlags) memorycore.ExtractionPolicyOverride {
	return memorycore.ExtractionPolicyOverride{
		AllowSensitiveExtraction: boolPtr(flags.AllowSensitive),
		AllowInference:           boolPtr(flags.AllowInference),
		ManualPin:                boolPtr(flags.ManualPin),
		ManualForget:             boolPtr(flags.ManualForget),
		MaxFacts:                 intPtr(flags.MaxFacts),
		MaxLinks:                 intPtr(flags.MaxLinks),
		RequireCleanGate:         boolPtr(flags.RequireCleanGate),
	}
}

func extractionProviderOverrideFromFlags(flags *extractionRuntimeFlags) memorycore.ExtractionProviderOverride {
	return memorycore.ExtractionProviderOverride{
		Kind:           flags.Provider,
		ID:             firstNonEmptyCLI(flags.ProviderID, flags.Provider),
		BaseURL:        flags.BaseURL,
		APIKeyEnv:      flags.APIKeyEnv,
		Model:          flags.Model,
		Temperature:    floatPtr(flags.Temperature),
		MaxTokens:      intPtr(flags.MaxTokens),
		Timeout:        flags.Timeout,
		ResponseFormat: memorycore.ExtractionResponseFormat(flags.ResponseFormat),
	}
}

func cliExtractionProviderKind(provider memconfig.ProviderConfig) string {
	kind := strings.TrimSpace(strings.ToLower(strings.ReplaceAll(provider.Provider, "_", "-")))
	switch kind {
	case "mock":
		return memorycore.ExtractionProviderMock
	case "openai-compatible", "openai":
		return memorycore.ExtractionProviderOpenAICompatible
	case "disabled", "":
		if provider.Enabled && strings.EqualFold(provider.Protocol, "openai_compatible") {
			return memorycore.ExtractionProviderOpenAICompatible
		}
		return memorycore.ExtractionProviderDisabled
	default:
		if strings.EqualFold(provider.Protocol, "openai_compatible") {
			return memorycore.ExtractionProviderOpenAICompatible
		}
		return provider.Provider
	}
}

func cliExtractionConfigMode(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "dry_run") {
		return string(memorycore.ExtractionRunModeDryRun)
	}
	return mode
}

func firstNonEmptyCLI(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func writeExtractionRunText(stdout io.Writer, result memorycore.ExtractionRunResult) {
	fmt.Fprintf(stdout, "request_id=%s\n", result.RequestID)
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	fmt.Fprintf(stdout, "accepted_count=%d\n", result.AcceptedCount)
	fmt.Fprintf(stdout, "review_count=%d\n", result.ReviewCount)
	fmt.Fprintf(stdout, "rejected_count=%d\n", result.RejectedCount)
	fmt.Fprintf(stdout, "applied_count=%d\n", result.AppliedCount)
}
