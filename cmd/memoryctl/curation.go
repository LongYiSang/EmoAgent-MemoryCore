package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type curationRunFlags struct {
	commonOptions
	Mode                   string
	Trigger                string
	SinceCreatedAt         string
	SinceFactID            string
	UntilCreatedAt         string
	UntilFactID            string
	UpdateCheckpoint       bool
	ProviderID             string
	ProviderKind           string
	Model                  string
	Temperature            float64
	MaxTokens              int
	Timeout                time.Duration
	Thinking               string
	RawLog                 bool
	RawLogDir              string
	MaxNewFacts            int
	CandidateLimitPerFact  int
	CandidateSource        string
	MirrorMinSimilarity    float64
	MaxFactsPerGroup       int
	MinAutoApplyConfidence float64
}

func runCurationRun(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("curation-run", stderr)
	flags := parseCurationRunFlags(fs)
	if !parseFlags(fs, args) {
		return 2
	}
	explicit := explicitFlagNames(fs)
	cfg, hasConfig, err := loadCommandConfig(flags.commonOptions)
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if hasConfig {
		applyCurationRunConfig(flags, &cfg, explicit, stderr)
		if err := cfg.Validate(); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	}
	if code := validateCurationRunFlags(stderr, fs, flags); code != 0 {
		return code
	}
	sinceCreatedAt, err := parseOptionalTimePtr(flags.SinceCreatedAt, "--since-created-at")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	untilCreatedAt, err := parseOptionalTimePtr(flags.UntilCreatedAt, "--until-created-at")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	svc, err := openCurationService(ctx, flags.commonOptions, flags, cfg, hasConfig)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		PersonaID:              flags.PersonaID,
		Mode:                   flags.Mode,
		Trigger:                flags.Trigger,
		SinceCreatedAt:         sinceCreatedAt,
		SinceFactID:            flags.SinceFactID,
		UntilCreatedAt:         untilCreatedAt,
		UntilFactID:            flags.UntilFactID,
		CandidateLimitPerFact:  flags.CandidateLimitPerFact,
		MaxNewFacts:            flags.MaxNewFacts,
		MaxFactsPerGroup:       flags.MaxFactsPerGroup,
		MinAutoApplyConfidence: flags.MinAutoApplyConfidence,
		CandidateRetrieval: &memorycore.CurationCandidateRetrievalOptions{
			Mode:                flags.CandidateSource,
			MirrorMinSimilarity: flags.MirrorMinSimilarity,
		},
		ProviderID:       flags.ProviderID,
		ProviderKind:     flags.ProviderKind,
		Model:            flags.Model,
		Temperature:      flags.Temperature,
		MaxTokens:        flags.MaxTokens,
		Timeout:          flags.Timeout,
		RawLog:           &memorycore.CurationRawLogOptions{Enabled: flags.RawLog, Directory: flags.RawLogDir},
		Force:            true,
		UpdateCheckpoint: flags.UpdateCheckpoint,
	})
	if err != nil {
		return runtimeError(stderr, "curation run: %v", err)
	}
	if flags.Format == formatJSON {
		return writeJSON(stdout, result, flags.Pretty)
	}
	fmt.Fprintf(stdout, "run_id=%s\n", result.RunID)
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	fmt.Fprintf(stdout, "mode=%s\n", result.Mode)
	fmt.Fprintf(stdout, "new_fact_count=%d\n", result.NewFactCount)
	fmt.Fprintf(stdout, "group_count=%d\n", result.GroupCount)
	fmt.Fprintf(stdout, "applied_group_count=%d\n", result.AppliedGroupCount)
	fmt.Fprintf(stdout, "review_group_count=%d\n", result.ReviewGroupCount)
	fmt.Fprintf(stdout, "noop_group_count=%d\n", result.NoopGroupCount)
	return 0
}

func parseCurationRunFlags(fs *flag.FlagSet) *curationRunFlags {
	flags := &curationRunFlags{
		Mode:                   "dry-run",
		Trigger:                "cli",
		Model:                  "memory-curator",
		MaxTokens:              4096,
		Timeout:                60 * time.Second,
		Thinking:               "disabled",
		MaxNewFacts:            100,
		CandidateLimitPerFact:  20,
		CandidateSource:        "mirror_first",
		MirrorMinSimilarity:    0.70,
		MaxFactsPerGroup:       8,
		MinAutoApplyConfidence: 0.88,
	}
	addCommonFlags(fs, &flags.commonOptions, formatText)
	addConfigFlag(fs, &flags.commonOptions)
	fs.StringVar(&flags.Mode, "mode", flags.Mode, "dry-run or apply")
	fs.StringVar(&flags.Trigger, "trigger", flags.Trigger, "manual, scheduled, cli, or test")
	fs.StringVar(&flags.SinceCreatedAt, "since-created-at", "", "RFC3339 lower created_at cursor")
	fs.StringVar(&flags.SinceFactID, "since-fact-id", "", "lower fact id cursor for equal created_at")
	fs.StringVar(&flags.UntilCreatedAt, "until-created-at", "", "RFC3339 upper created_at bound")
	fs.StringVar(&flags.UntilFactID, "until-fact-id", "", "upper fact id bound for equal created_at")
	fs.BoolVar(&flags.UpdateCheckpoint, "update-checkpoint", false, "advance curation checkpoint after successful apply")
	fs.StringVar(&flags.ProviderID, "provider-id", flags.ProviderID, "curation LLM provider id")
	fs.StringVar(&flags.ProviderKind, "provider-kind", flags.ProviderKind, "mock|openai-compatible|disabled")
	fs.StringVar(&flags.Model, "model", flags.Model, "curation LLM model")
	fs.Float64Var(&flags.Temperature, "temperature", flags.Temperature, "LLM temperature")
	fs.IntVar(&flags.MaxTokens, "max-tokens", flags.MaxTokens, "maximum output tokens")
	fs.DurationVar(&flags.Timeout, "timeout", flags.Timeout, "provider timeout")
	fs.StringVar(&flags.Thinking, "thinking", flags.Thinking, "disabled or enabled")
	fs.BoolVar(&flags.RawLog, "raw-log", false, "write one raw curation debug log JSON file per run")
	fs.StringVar(&flags.RawLogDir, "raw-log-dir", "", "directory for raw curation debug log JSON files")
	fs.IntVar(&flags.MaxNewFacts, "max-new-facts", flags.MaxNewFacts, "maximum new facts per curation run")
	fs.IntVar(&flags.CandidateLimitPerFact, "candidate-limit-per-fact", flags.CandidateLimitPerFact, "maximum comparable facts per new fact")
	fs.StringVar(&flags.CandidateSource, "candidate-source", flags.CandidateSource, "mirror_first, sqlite_only, or mirror_only")
	fs.Float64Var(&flags.MirrorMinSimilarity, "mirror-min-similarity", flags.MirrorMinSimilarity, "minimum mirror similarity for curation grouping")
	fs.IntVar(&flags.MaxFactsPerGroup, "max-facts-per-group", flags.MaxFactsPerGroup, "maximum facts in one semantic curation group")
	fs.Float64Var(&flags.MinAutoApplyConfidence, "min-auto-apply-confidence", flags.MinAutoApplyConfidence, "minimum confidence for automatic same/refinement apply")
	return flags
}

func validateCurationRunFlags(stderr io.Writer, fs *flag.FlagSet, flags *curationRunFlags) int {
	if !requireDB(stderr, fs, flags.DBPath) {
		return 2
	}
	if err := validateFormat(flags.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	normalizedMode := strings.ReplaceAll(strings.TrimSpace(flags.Mode), "-", "_")
	if err := validateOneOf("--mode", normalizedMode, "dry_run", "apply"); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	flags.Mode = normalizedMode
	if err := validateOneOf("--trigger", flags.Trigger, "manual", "scheduled", "cli", "test"); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if flags.ProviderID == memorycore.ExtractionProviderMock && strings.TrimSpace(flags.ProviderKind) == "" {
		flags.ProviderKind = memorycore.ExtractionProviderMock
	}
	if strings.TrimSpace(flags.ProviderKind) != "" {
		if err := validateOneOf("--provider-kind", flags.ProviderKind, memorycore.ExtractionProviderMock, memorycore.ExtractionProviderOpenAICompatible, memorycore.ExtractionProviderDisabled); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	}
	if flags.ProviderKind == memorycore.ExtractionProviderDisabled {
		return usageError(stderr, fs, "--provider-kind disabled cannot run curation")
	}
	if strings.TrimSpace(flags.RawLogDir) != "" {
		flags.RawLog = true
	}
	if flags.RawLog && strings.TrimSpace(flags.RawLogDir) == "" {
		return usageError(stderr, fs, "--raw-log-dir is required when --raw-log is set")
	}
	if err := validateOneOf("--thinking", flags.Thinking, "disabled", "enabled"); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if flags.MaxNewFacts <= 0 {
		return usageError(stderr, fs, "--max-new-facts must be positive")
	}
	if flags.CandidateLimitPerFact <= 0 {
		return usageError(stderr, fs, "--candidate-limit-per-fact must be positive")
	}
	if err := validateOneOf("--candidate-source", flags.CandidateSource, "mirror_first", "sqlite_only", "mirror_only"); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateFloatRange("--mirror-min-similarity", flags.MirrorMinSimilarity, 0, 1); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if flags.MirrorMinSimilarity == 0 {
		return usageError(stderr, fs, "--mirror-min-similarity must be greater than 0")
	}
	if flags.MaxFactsPerGroup <= 1 {
		return usageError(stderr, fs, "--max-facts-per-group must be greater than 1")
	}
	if flags.MaxTokens <= 0 {
		return usageError(stderr, fs, "--max-tokens must be positive")
	}
	if flags.Timeout <= 0 {
		return usageError(stderr, fs, "--timeout must be positive")
	}
	if err := validateFloatRange("--min-auto-apply-confidence", flags.MinAutoApplyConfidence, 0, 1); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateFloatRange("--temperature", flags.Temperature, 0, 2); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	return 0
}

func applyCurationRunConfig(flags *curationRunFlags, cfg *memconfig.Config, explicit map[string]bool, stderr io.Writer) {
	applyCommonConfig(&flags.commonOptions, cfg, explicit, stderr)
	curation := &cfg.SemanticOps.Curation
	if explicit["mode"] {
		warnConfigOverride(stderr, "mode", "semantic_ops.curation.mode")
		curation.Mode = strings.ReplaceAll(flags.Mode, "-", "_")
	} else {
		flags.Mode = curation.Mode
	}
	if explicit["max-new-facts"] {
		warnConfigOverride(stderr, "max-new-facts", "semantic_ops.curation.max_new_facts_per_run")
		curation.MaxNewFactsPerRun = flags.MaxNewFacts
	} else {
		flags.MaxNewFacts = curation.MaxNewFactsPerRun
	}
	if explicit["candidate-limit-per-fact"] {
		warnConfigOverride(stderr, "candidate-limit-per-fact", "semantic_ops.curation.candidate_limit_per_fact")
		curation.CandidateLimitPerFact = flags.CandidateLimitPerFact
	} else {
		flags.CandidateLimitPerFact = curation.CandidateLimitPerFact
	}
	if explicit["candidate-source"] {
		warnConfigOverride(stderr, "candidate-source", "semantic_ops.curation.candidate_retrieval.mode")
		curation.CandidateRetrieval.Mode = flags.CandidateSource
	} else {
		flags.CandidateSource = curation.CandidateRetrieval.Mode
	}
	if explicit["mirror-min-similarity"] {
		warnConfigOverride(stderr, "mirror-min-similarity", "semantic_ops.curation.candidate_retrieval.mirror_min_similarity")
		curation.CandidateRetrieval.MirrorMinSimilarity = flags.MirrorMinSimilarity
	} else {
		flags.MirrorMinSimilarity = curation.CandidateRetrieval.MirrorMinSimilarity
	}
	if explicit["max-facts-per-group"] {
		warnConfigOverride(stderr, "max-facts-per-group", "semantic_ops.curation.max_facts_per_group")
		curation.MaxFactsPerGroup = flags.MaxFactsPerGroup
	} else {
		flags.MaxFactsPerGroup = curation.MaxFactsPerGroup
	}
	if explicit["min-auto-apply-confidence"] {
		warnConfigOverride(stderr, "min-auto-apply-confidence", "semantic_ops.curation.min_auto_apply_confidence")
		curation.MinAutoApplyConfidence = flags.MinAutoApplyConfidence
	} else {
		flags.MinAutoApplyConfidence = curation.MinAutoApplyConfidence
	}
	if explicit["provider-id"] {
		warnConfigOverride(stderr, "provider-id", "semantic_ops.curation.llm.provider_id")
		curation.LLM.ProviderID = flags.ProviderID
	} else {
		flags.ProviderID = curation.LLM.ProviderID
	}
	if explicit["provider-kind"] {
		warnConfigOverride(stderr, "provider-kind", "semantic_ops.curation.llm.provider_kind")
		curation.LLM.ProviderKind = flags.ProviderKind
	} else {
		flags.ProviderKind = curation.LLM.ProviderKind
	}
	if explicit["model"] {
		warnConfigOverride(stderr, "model", "semantic_ops.curation.llm.model")
		curation.LLM.Model = flags.Model
	} else {
		flags.Model = curation.LLM.Model
	}
	if explicit["temperature"] {
		warnConfigOverride(stderr, "temperature", "semantic_ops.curation.llm.temperature")
		curation.LLM.Temperature = flags.Temperature
	} else {
		flags.Temperature = curation.LLM.Temperature
	}
	if explicit["max-tokens"] {
		warnConfigOverride(stderr, "max-tokens", "semantic_ops.curation.llm.max_tokens")
		curation.LLM.MaxTokens = flags.MaxTokens
	} else {
		flags.MaxTokens = curation.LLM.MaxTokens
	}
	if explicit["timeout"] {
		warnConfigOverride(stderr, "timeout", "semantic_ops.curation.llm.timeout_ms")
		curation.LLM.TimeoutMS = int(flags.Timeout / time.Millisecond)
	} else if curation.LLM.TimeoutMS > 0 {
		flags.Timeout = time.Duration(curation.LLM.TimeoutMS) * time.Millisecond
	}
	if explicit["thinking"] {
		warnConfigOverride(stderr, "thinking", "semantic_ops.curation.llm.thinking.type")
		curation.LLM.Thinking.Type = flags.Thinking
	} else if strings.TrimSpace(curation.LLM.Thinking.Type) != "" {
		flags.Thinking = curation.LLM.Thinking.Type
	}
	if explicit["raw-log"] {
		warnConfigOverride(stderr, "raw-log", "semantic_ops.curation.raw_log.enabled")
		curation.RawLog.Enabled = flags.RawLog
	} else {
		flags.RawLog = curation.RawLog.Enabled
	}
	if explicit["raw-log-dir"] {
		warnConfigOverride(stderr, "raw-log-dir", "semantic_ops.curation.raw_log.directory")
		flags.RawLog = true
		curation.RawLog.Enabled = true
		curation.RawLog.Directory = flags.RawLogDir
	} else {
		flags.RawLogDir = curation.RawLog.Directory
	}
	if flags.ProviderID == memorycore.ExtractionProviderMock && strings.TrimSpace(flags.ProviderKind) == "" {
		flags.ProviderKind = memorycore.ExtractionProviderMock
		curation.LLM.ProviderKind = memorycore.ExtractionProviderMock
	}
}

func openCurationService(ctx context.Context, opts commonOptions, flags *curationRunFlags, cfg memconfig.Config, hasConfig bool) (memorycore.Service, error) {
	if hasConfig {
		openOpts, err := cfg.ToOptions()
		if err != nil {
			return nil, err
		}
		return memorycore.Open(ctx, openOpts)
	}
	return memorycore.Open(ctx, memorycore.Options{
		DBPath:      opts.DBPath,
		PersonaID:   opts.PersonaID,
		AutoMigrate: opts.AutoMigrate,
		EnableFTS:   opts.EnableFTS,
		SemanticOps: memorycore.SemanticOpsOptions{
			Curation: memorycore.SemanticCurationOptions{
				Enabled:                true,
				Mode:                   flags.Mode,
				MaxNewFactsPerRun:      flags.MaxNewFacts,
				CandidateLimitPerFact:  flags.CandidateLimitPerFact,
				MaxFactsPerGroup:       flags.MaxFactsPerGroup,
				MinAutoApplyConfidence: flags.MinAutoApplyConfidence,
				CandidateRetrieval: memorycore.CurationCandidateRetrievalOptions{
					Mode:                flags.CandidateSource,
					MirrorMinSimilarity: flags.MirrorMinSimilarity,
				},
				LLM: memorycore.CurationLLMOptions{
					ProviderID:     flags.ProviderID,
					ProviderKind:   flags.ProviderKind,
					Model:          flags.Model,
					Temperature:    flags.Temperature,
					MaxTokens:      flags.MaxTokens,
					ResponseFormat: memorycore.ExtractionResponseFormatJSONObject,
					Timeout:        flags.Timeout,
					Thinking:       &memorycore.OpenAICompatibleThinkingOptions{Type: flags.Thinking},
				},
				RawLog: memorycore.CurationRawLogOptions{Enabled: flags.RawLog, Directory: flags.RawLogDir},
			},
		},
	})
}
