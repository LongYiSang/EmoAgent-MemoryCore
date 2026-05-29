package memorycore

import "strings"

func normalizeSemanticOpsOptions(opts SemanticOpsOptions) SemanticOpsOptions {
	opts.Dedup = normalizeSemanticDedupOptions(opts.Dedup)
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
