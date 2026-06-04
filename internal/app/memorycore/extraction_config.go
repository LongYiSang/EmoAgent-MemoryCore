package memorycore

import (
	"fmt"
	"strings"
	"time"
)

const (
	ExtractionProviderDisabled         = "disabled"
	ExtractionProviderMock             = "mock"
	ExtractionProviderOpenAICompatible = "openai-compatible"
)

type ExtractionServiceError struct {
	Code    string
	Message string
}

func (e *ExtractionServiceError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ExtractionServiceError) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func extractionServiceError(code string, message string) *ExtractionServiceError {
	return &ExtractionServiceError{Code: code, Message: message}
}

func normalizeExtractionOptions(opts ExtractionOptions) ExtractionOptions {
	if !opts.Defaults.Configured {
		opts.Defaults.AllowInference = true
		opts.Defaults.ApplyAcceptedFacts = true
	}
	if !opts.Runtime.Configured {
		opts.Runtime.RepairEnabled = true
	}
	if !opts.Audit.Configured {
		opts.Audit.Enabled = true
	}
	if strings.TrimSpace(opts.Provider.Kind) == "" {
		opts.Provider.Kind = ExtractionProviderDisabled
	}
	if strings.TrimSpace(opts.Provider.APIKeyEnv) == "" {
		opts.Provider.APIKeyEnv = "MEMORYCORE_LLM_API_KEY"
	}
	if opts.Provider.MaxTokens == 0 {
		opts.Provider.MaxTokens = 6000
	}
	if opts.Provider.Timeout == 0 {
		opts.Provider.Timeout = 60 * time.Second
	}
	if opts.Provider.ResponseFormat == "" {
		opts.Provider.ResponseFormat = ExtractionResponseFormatJSONSchema
	}
	if opts.Defaults.Mode == "" {
		opts.Defaults.Mode = ExtractionRunModeDryRun
	}
	if strings.TrimSpace(opts.Defaults.Timezone) == "" {
		opts.Defaults.Timezone = "Asia/Shanghai"
	}
	if opts.Defaults.MaxFacts == 0 {
		opts.Defaults.MaxFacts = 12
	}
	if opts.Defaults.MaxLinks == 0 {
		opts.Defaults.MaxLinks = 20
	}
	if opts.PromptVersions.Extraction == "" {
		opts.PromptVersions.Extraction = defaultPromptVersion
	}
	if opts.PromptVersions.PreFilter == "" {
		opts.PromptVersions.PreFilter = defaultPreFilterPromptVersion
	}
	if opts.PromptVersions.Repair == "" {
		opts.PromptVersions.Repair = defaultRepairPromptVersion
	}
	return opts
}

func applyExtractionProviderOverride(opts ExtractionProviderOptions, override ExtractionProviderOverride) ExtractionProviderOptions {
	if strings.TrimSpace(override.Kind) != "" {
		opts.Kind = override.Kind
	}
	if strings.TrimSpace(override.ID) != "" {
		opts.ID = override.ID
	}
	if strings.TrimSpace(override.BaseURL) != "" {
		opts.BaseURL = override.BaseURL
	}
	if strings.TrimSpace(override.APIKeyEnv) != "" {
		opts.APIKeyEnv = override.APIKeyEnv
	}
	if strings.TrimSpace(override.Model) != "" {
		opts.Model = override.Model
	}
	if override.Temperature != nil {
		opts.Temperature = *override.Temperature
	}
	if override.MaxTokens != nil {
		opts.MaxTokens = *override.MaxTokens
	}
	if override.Timeout != 0 {
		opts.Timeout = override.Timeout
	}
	if override.ResponseFormat != "" {
		opts.ResponseFormat = override.ResponseFormat
	}
	if override.Thinking != nil {
		opts.Thinking = override.Thinking
	}
	return opts
}

func applyExtractionPolicyOverride(opts ExtractionDefaults, override ExtractionPolicyOverride) ExtractionDefaults {
	if override.AllowSensitiveExtraction != nil {
		opts.AllowSensitiveExtraction = *override.AllowSensitiveExtraction
	}
	if override.AllowInference != nil {
		opts.AllowInference = *override.AllowInference
	}
	if override.MaxFacts != nil {
		opts.MaxFacts = *override.MaxFacts
	}
	if override.MaxLinks != nil {
		opts.MaxLinks = *override.MaxLinks
	}
	if override.RequireCleanGate != nil {
		opts.RequireCleanGate = *override.RequireCleanGate
	}
	if override.ApplyAcceptedFacts != nil {
		opts.ApplyAcceptedFacts = *override.ApplyAcceptedFacts
	}
	if override.ExecuteDeletionIntents != nil {
		opts.ExecuteDeletionIntents = *override.ExecuteDeletionIntents
	}
	return opts
}

func applyExtractionRuntimeOverride(opts ExtractionRuntimeOptions, override ExtractionRuntimeOverride) ExtractionRuntimeOptions {
	if override.UsePreFilter != nil {
		opts.UsePreFilter = *override.UsePreFilter
	}
	if override.RepairEnabled != nil {
		opts.RepairEnabled = *override.RepairEnabled
	}
	return opts
}

func normalizeExtractionAudit(value string, enabled bool) (string, error) {
	if strings.TrimSpace(value) == "" {
		if enabled {
			return ExtractionAuditOn, nil
		}
		return ExtractionAuditOff, nil
	}
	switch value {
	case ExtractionAuditOn, ExtractionAuditOff:
		return value, nil
	default:
		return "", fmt.Errorf("audit must be on or off")
	}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}
