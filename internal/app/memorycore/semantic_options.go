package memorycore

import (
	"strings"
	"time"
)

func normalizeSemanticOpsOptions(opts SemanticOpsOptions) SemanticOpsOptions {
	opts.Dedup = normalizeSemanticDedupOptions(opts.Dedup)
	opts.Curation = normalizeSemanticCurationOptions(opts.Curation)
	return opts
}

func normalizeSemanticDedupOptions(opts SemanticDedupOptions) SemanticDedupOptions {
	if opts.CandidateLimit <= 0 {
		opts.CandidateLimit = 12
	}
	if strings.TrimSpace(opts.ThresholdProfile) == "" {
		opts.ThresholdProfile = "default_v0"
	}
	return opts
}

func semanticDedupOverrideConfigured(opts SemanticDedupOptions) bool {
	return opts.Enabled ||
		opts.Shadow ||
		opts.Enforce ||
		opts.CandidateLimit != 0 ||
		strings.TrimSpace(opts.ThresholdProfile) != ""
}

func normalizeSemanticCurationOptions(opts SemanticCurationOptions) SemanticCurationOptions {
	if strings.TrimSpace(opts.Mode) == "" {
		opts.Mode = "dry_run"
	}
	if opts.MaxNewFactsPerRun <= 0 {
		opts.MaxNewFactsPerRun = 100
	}
	if opts.CandidateLimitPerFact <= 0 {
		opts.CandidateLimitPerFact = 20
	}
	if opts.MaxFactsPerGroup <= 0 {
		opts.MaxFactsPerGroup = 8
	}
	if opts.MinAutoApplyConfidence <= 0 {
		opts.MinAutoApplyConfidence = 0.88
	}
	opts.CandidateRetrieval = normalizeCurationCandidateRetrievalOptions(opts.CandidateRetrieval)
	if len(opts.IncludeFactTypes) == 0 {
		opts.IncludeFactTypes = []string{
			FactTypeStablePreference,
			FactTypeRelationalState,
			FactTypeTransientContext,
			FactTypeTaskRelevantContext,
		}
	}
	if len(opts.ExcludeFactTypes) == 0 {
		opts.ExcludeFactTypes = []string{FactTypeCoreIdentity, FactTypeCommitment}
	}
	if opts.LLM.Provider.MaxTokens <= 0 && opts.LLM.MaxTokens <= 0 {
		opts.LLM.MaxTokens = 4096
	}
	if opts.LLM.Timeout <= 0 && opts.LLM.Provider.Timeout <= 0 {
		opts.LLM.Timeout = 60 * time.Second
	}
	if opts.LLM.Thinking == nil && opts.LLM.Provider.Thinking == nil {
		opts.LLM.Thinking = &OpenAICompatibleThinkingOptions{Type: "disabled"}
	}
	return opts
}

func normalizeCurationCandidateRetrievalOptions(opts CurationCandidateRetrievalOptions) CurationCandidateRetrievalOptions {
	if strings.TrimSpace(opts.Mode) == "" {
		opts.Mode = "mirror_first"
	}
	opts.Mode = strings.TrimSpace(opts.Mode)
	if opts.MirrorMinSimilarity <= 0 {
		opts.MirrorMinSimilarity = 0.70
	}
	return opts
}
