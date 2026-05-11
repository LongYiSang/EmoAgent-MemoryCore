package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime"
)

type extractionRuntimeFlags struct {
	commonOptions
	SessionID        string
	Trigger          string
	SinceValue       string
	UntilValue       string
	Timezone         string
	EpisodeIDs       stringList
	Limit            int
	MaxFacts         int
	MaxLinks         int
	AllowSensitive   bool
	AllowInference   bool
	ManualPin        bool
	ManualForget     bool
	Mode             string
	Provider         string
	BaseURL          string
	APIKeyEnv        string
	Model            string
	Temperature      float64
	MaxTokens        int
	Timeout          time.Duration
	UsePreFilter     bool
	Repair           bool
	Audit            string
	Force            bool
	StopOnError      bool
	RequireCleanGate bool
}

func runExtractRun(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("extract-run", stderr)
	flags := parseExtractionRuntimeFlags(fs, formatJSON)
	if !parseFlags(fs, args) {
		return 2
	}
	if code := validateExtractionRuntimeFlags(stderr, fs, flags, false); code != 0 {
		return code
	}
	ctx := context.Background()
	db, svc, cleanup, err := openRuntimeDBAndService(ctx, flags.commonOptions)
	if err != nil {
		return runtimeError(stderr, "%v", err)
	}
	defer cleanup()
	req, code := buildCLIExtractionRequest(ctx, stderr, fs, db.SQLDB(), flags)
	if code != 0 {
		return code
	}
	llm, err := llmFromFlags(flags)
	if err != nil {
		return runtimeError(stderr, "%v", err)
	}
	audit := extractionruntime.NewSQLiteAuditStore(db.SQLDB())
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db.SQLDB(), Service: svc, LLM: llm, AuditStore: audit})
	result, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:          req,
		Mode:             memorycore.ExtractionRunMode(flags.Mode),
		ProviderID:       flags.Provider,
		ProviderKind:     flags.Provider,
		Model:            flags.Model,
		Temperature:      flags.Temperature,
		MaxTokens:        flags.MaxTokens,
		Timeout:          flags.Timeout,
		UsePreFilter:     flags.UsePreFilter,
		RepairEnabled:    flags.Repair,
		RequireCleanGate: flags.RequireCleanGate,
		Audit:            flags.Audit,
		Force:            flags.Force,
		Window: memorycore.ExtractionRunWindow{
			EpisodeIDs: []string(flags.EpisodeIDs),
			Limit:      flags.Limit,
		},
	})
	if flags.Format == formatJSON {
		writeJSON(stdout, result, flags.Pretty)
	} else {
		writeExtractionRunText(stdout, result)
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
	if code := validateExtractionRuntimeFlags(stderr, fs, flags, true); code != 0 {
		return code
	}
	ctx := context.Background()
	db, svc, cleanup, err := openRuntimeDBAndService(ctx, flags.commonOptions)
	if err != nil {
		return runtimeError(stderr, "%v", err)
	}
	defer cleanup()
	llm, err := llmFromFlags(flags)
	if err != nil {
		return runtimeError(stderr, "%v", err)
	}
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
	audit := extractionruntime.NewSQLiteAuditStore(db.SQLDB())
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db.SQLDB(), Service: svc, LLM: llm, AuditStore: audit})
	result, err := runner.RunBatch(ctx, memorycore.ExtractionBatchRequest{
		PersonaID:        flags.PersonaID,
		SessionIDs:       sessionIDs,
		Trigger:          flags.Trigger,
		Mode:             memorycore.ExtractionRunMode(flags.Mode),
		ProviderID:       flags.Provider,
		ProviderKind:     flags.Provider,
		Model:            flags.Model,
		Temperature:      flags.Temperature,
		MaxTokens:        flags.MaxTokens,
		Timeout:          flags.Timeout,
		Limit:            flags.Limit,
		Since:            since,
		Until:            until,
		UsePreFilter:     flags.UsePreFilter,
		RepairEnabled:    flags.Repair,
		RequireCleanGate: flags.RequireCleanGate,
		Audit:            flags.Audit,
		Force:            flags.Force,
		StopOnError:      flags.StopOnError,
	})
	if flags.Format == formatJSON {
		writeJSON(stdout, result, flags.Pretty)
	} else {
		fmt.Fprintf(stdout, "status=%s processed_count=%d skipped_count=%d failed_count=%d\n", result.Status, result.ProcessedCount, result.SkippedCount, result.FailedCount)
	}
	if err != nil || result.Status == "failed" {
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
		}
		return 1
	}
	return 0
}

func parseExtractionRuntimeFlags(fs *flag.FlagSet, defaultFormat string) *extractionRuntimeFlags {
	flags := &extractionRuntimeFlags{Mode: string(memorycore.ExtractionRunModeDryRun), Provider: "mock", Trigger: memorycore.ExtractionTriggerSessionEnd, Timezone: "Asia/Singapore", Limit: 50, MaxFacts: 12, MaxLinks: 20, AllowInference: true, Repair: true, Audit: memorycore.ExtractionAuditOn, APIKeyEnv: "MEMORYCORE_LLM_API_KEY", Timeout: 60 * time.Second, MaxTokens: 4096}
	addCommonFlags(fs, &flags.commonOptions, defaultFormat)
	fs.StringVar(&flags.SessionID, "session", "", "session id")
	fs.Var(&flags.EpisodeIDs, "episode", "episode id; repeatable")
	fs.StringVar(&flags.Trigger, "trigger", memorycore.ExtractionTriggerSessionEnd, "extraction trigger")
	fs.IntVar(&flags.Limit, "limit", 50, "maximum episodes or sessions")
	fs.StringVar(&flags.SinceValue, "since", "", "RFC3339 lower occurrence bound")
	fs.StringVar(&flags.UntilValue, "until", "", "RFC3339 upper occurrence bound")
	fs.StringVar(&flags.Timezone, "timezone", "Asia/Singapore", "request timezone")
	fs.BoolVar(&flags.AllowSensitive, "allow-sensitive-extraction", false, "allow highly sensitive extraction without review")
	fs.BoolVar(&flags.AllowInference, "allow-inference", true, "allow inferred candidates")
	fs.BoolVar(&flags.ManualPin, "manual-pin", false, "mark request policy as manual pin")
	fs.BoolVar(&flags.ManualForget, "manual-forget", false, "mark request policy as manual forget")
	fs.IntVar(&flags.MaxFacts, "max-facts", 12, "maximum fact candidates")
	fs.IntVar(&flags.MaxLinks, "max-links", 20, "maximum link candidates")
	fs.StringVar(&flags.Mode, "mode", string(memorycore.ExtractionRunModeDryRun), "validate|dry-run|apply")
	fs.StringVar(&flags.Provider, "provider", "mock", "mock|openai-compatible")
	fs.StringVar(&flags.BaseURL, "base-url", "", "OpenAI-compatible base URL")
	fs.StringVar(&flags.APIKeyEnv, "api-key-env", "MEMORYCORE_LLM_API_KEY", "environment variable containing provider API key")
	fs.StringVar(&flags.Model, "model", "", "model name")
	fs.Float64Var(&flags.Temperature, "temperature", 0, "LLM temperature")
	fs.IntVar(&flags.MaxTokens, "max-tokens", 4096, "maximum output tokens")
	fs.DurationVar(&flags.Timeout, "timeout", 60*time.Second, "provider timeout")
	fs.BoolVar(&flags.UsePreFilter, "prefilter", false, "run extraction prefilter before extractor")
	fs.BoolVar(&flags.Repair, "repair", true, "repair invalid JSON once")
	fs.StringVar(&flags.Audit, "audit", memorycore.ExtractionAuditOn, "on|off; dry-run does not write memory but may write audit rows")
	fs.BoolVar(&flags.Force, "force", false, "rerun even if the fingerprint already succeeded")
	fs.BoolVar(&flags.StopOnError, "stop-on-error", false, "stop batch at first error")
	fs.BoolVar(&flags.RequireCleanGate, "require-clean-gate", false, "apply only if gate has no review or rejected candidates")
	return flags
}

func validateExtractionRuntimeFlags(stderr io.Writer, fs *flag.FlagSet, flags *extractionRuntimeFlags, batch bool) int {
	if !requireDB(stderr, fs, flags.DBPath) {
		return 2
	}
	if err := validateFormat(flags.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--mode", flags.Mode, string(memorycore.ExtractionRunModeValidate), string(memorycore.ExtractionRunModeDryRun), string(memorycore.ExtractionRunModeApply)); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--provider", flags.Provider, "mock", "openai-compatible"); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--audit", flags.Audit, memorycore.ExtractionAuditOn, memorycore.ExtractionAuditOff); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateExtractionTriggerFlag(flags.Trigger); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if !batch && strings.TrimSpace(flags.SessionID) == "" && len(flags.EpisodeIDs) == 0 {
		return usageError(stderr, fs, "--session or --episode is required")
	}
	return 0
}

func openRuntimeDBAndService(ctx context.Context, opts commonOptions) (*memsqlite.DB, memorycore.Service, func(), error) {
	db, err := memsqlite.Open(ctx, opts.DBPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open db: %w", err)
	}
	if opts.AutoMigrate {
		if err := db.MigrateWithOptions(ctx, memsqlite.MigrateOptions{EnableFTS: opts.EnableFTS}); err != nil {
			_ = db.Close()
			return nil, nil, nil, fmt.Errorf("migrate db: %w", err)
		}
	}
	svc, err := openService(ctx, opts)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, fmt.Errorf("open memorycore: %w", err)
	}
	cleanup := func() {
		_ = svc.Close()
		_ = db.Close()
	}
	return db, svc, cleanup, nil
}

func buildCLIExtractionRequest(ctx context.Context, stderr io.Writer, fs *flag.FlagSet, db *sql.DB, flags *extractionRuntimeFlags) (memorycore.ExtractionRequest, int) {
	since, err := parseOptionalTimePtr(flags.SinceValue, "--since")
	if err != nil {
		return memorycore.ExtractionRequest{}, usageError(stderr, fs, err.Error())
	}
	until, err := parseOptionalTimePtr(flags.UntilValue, "--until")
	if err != nil {
		return memorycore.ExtractionRequest{}, usageError(stderr, fs, err.Error())
	}
	req, err := extractionruntime.BuildRequest(ctx, db, extractionruntime.BuildRequestOptions{
		PersonaID:                flags.PersonaID,
		SessionID:                stringPtr(flags.SessionID),
		EpisodeIDs:               []string(flags.EpisodeIDs),
		Trigger:                  flags.Trigger,
		Limit:                    flags.Limit,
		Since:                    since,
		Until:                    until,
		Timezone:                 flags.Timezone,
		AllowSensitiveExtraction: flags.AllowSensitive,
		AllowInference:           flags.AllowInference,
		ManualPin:                flags.ManualPin,
		ManualForget:             flags.ManualForget,
		MaxFacts:                 flags.MaxFacts,
		MaxLinks:                 flags.MaxLinks,
	})
	if err != nil {
		return memorycore.ExtractionRequest{}, runtimeError(stderr, "build extraction request: %v", err)
	}
	return req, 0
}

func llmFromFlags(flags *extractionRuntimeFlags) (memorycore.ExtractionLLM, error) {
	switch flags.Provider {
	case "mock":
		return extractionruntime.NewDeterministicMockLLM(), nil
	case "openai-compatible":
		return extractionruntime.NewOpenAICompatibleLLM(extractionruntime.OpenAICompatibleOptions{
			BaseURL:     flags.BaseURL,
			APIKeyEnv:   flags.APIKeyEnv,
			Model:       flags.Model,
			Timeout:     flags.Timeout,
			Temperature: flags.Temperature,
			MaxTokens:   flags.MaxTokens,
		}), nil
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
}

func writeExtractionRunText(stdout io.Writer, result memorycore.ExtractionRunResult) {
	fmt.Fprintf(stdout, "request_id=%s\n", result.RequestID)
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	fmt.Fprintf(stdout, "accepted_count=%d\n", result.AcceptedCount)
	fmt.Fprintf(stdout, "review_count=%d\n", result.ReviewCount)
	fmt.Fprintf(stdout, "rejected_count=%d\n", result.RejectedCount)
	fmt.Fprintf(stdout, "applied_count=%d\n", result.AppliedCount)
}
