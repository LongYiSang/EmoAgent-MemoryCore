package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	internalmirror "github.com/longyisang/emoagent-memorycore/internal/mirror"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runRetrieve(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("retrieve", stderr)
	var opts commonOptions
	var query, sessionID, now, sensitivity, userMood, relationshipMood, sidecarURL string
	var allowHistorical, allowDeepArchive, useFTS, useMirror bool
	var finalCount, budget int
	addCommonFlags(fs, &opts, formatText)
	addConfigFlag(fs, &opts)
	fs.StringVar(&query, "query", "", "query text")
	fs.StringVar(&sessionID, "session", "", "session id")
	fs.StringVar(&now, "now", "", "RFC3339 now")
	fs.StringVar(&sensitivity, "sensitivity", memorycore.SensitivityNormal, "sensitivity permission")
	fs.StringVar(&sensitivity, "sensitivity-permission", memorycore.SensitivityNormal, "sensitivity permission")
	fs.BoolVar(&allowHistorical, "allow-historical", false, "allow historical facts")
	fs.BoolVar(&allowDeepArchive, "allow-deep-archive", false, "allow deep archive")
	fs.IntVar(&finalCount, "final-count", 8, "final memory count")
	fs.IntVar(&budget, "budget", 1200, "context budget tokens")
	fs.BoolVar(&useFTS, "use-fts", false, "use FTS")
	fs.BoolVar(&useMirror, "use-mirror", false, "use Python sidecar mirror candidates")
	fs.StringVar(&sidecarURL, "sidecar-url", "", "loopback HTTP URL for the Python mirror sidecar")
	fs.StringVar(&userMood, "user-mood", "", "user mood label")
	fs.StringVar(&relationshipMood, "relationship-mood", "", "relationship mood label")
	if !parseFlags(fs, args) {
		return 2
	}
	explicit := explicitFlagNames(fs)
	cfg, hasConfig, err := loadCommandConfig(opts)
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if hasConfig {
		applyCommonConfig(&opts, &cfg, explicit, stderr)
		if explicit["sensitivity"] {
			warnConfigOverride(stderr, "sensitivity", "retrieval.sensitivity_permission")
			cfg.Retrieval.SensitivityPermission = sensitivity
		} else if explicit["sensitivity-permission"] {
			warnConfigOverride(stderr, "sensitivity-permission", "retrieval.sensitivity_permission")
			cfg.Retrieval.SensitivityPermission = sensitivity
		} else {
			sensitivity = cfg.Retrieval.SensitivityPermission
		}
		if explicit["allow-historical"] {
			warnConfigOverride(stderr, "allow-historical", "retrieval.allow_historical")
			cfg.Retrieval.AllowHistorical = allowHistorical
		} else {
			allowHistorical = cfg.Retrieval.AllowHistorical
		}
		if explicit["allow-deep-archive"] {
			warnConfigOverride(stderr, "allow-deep-archive", "retrieval.allow_deep_archive")
			cfg.Retrieval.AllowDeepArchive = allowDeepArchive
		} else {
			allowDeepArchive = cfg.Retrieval.AllowDeepArchive
		}
		if explicit["final-count"] {
			warnConfigOverride(stderr, "final-count", "retrieval.final_memory_count")
			cfg.Retrieval.FinalMemoryCount = finalCount
		} else {
			finalCount = cfg.Retrieval.FinalMemoryCount
		}
		if explicit["budget"] {
			warnConfigOverride(stderr, "budget", "retrieval.context_budget_tokens")
			cfg.Retrieval.ContextBudgetTokens = budget
		} else {
			budget = cfg.Retrieval.ContextBudgetTokens
		}
		if explicit["use-fts"] {
			warnConfigOverride(stderr, "use-fts", "retrieval.use_fts")
			cfg.Retrieval.UseFTS = useFTS
		} else {
			useFTS = cfg.Retrieval.UseFTS
		}
		if explicit["use-mirror"] {
			warnConfigOverride(stderr, "use-mirror", "retrieval.use_mirror")
			cfg.Retrieval.UseMirror = useMirror
		} else {
			useMirror = cfg.Retrieval.UseMirror
		}
		if explicit["sidecar-url"] {
			warnConfigOverride(stderr, "sidecar-url", "sidecar.url")
			cfg.Sidecar.Enabled = true
			cfg.Sidecar.URL = sidecarURL
		} else {
			sidecarURL = cfg.Sidecar.URL
			if cfg.Sidecar.Enabled && cfg.Sidecar.Adapter == "fake" {
				sidecarURL = ""
				cfg.Sidecar.URL = ""
			}
		}
		if err := cfg.Validate(); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if query == "" {
		return usageError(stderr, fs, "--query is required")
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if err := validateOneOf("--sensitivity-permission", sensitivity, memorycore.SensitivityNormal, memorycore.SensitivitySensitive, memorycore.SensitivityHighlySensitive); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if finalCount <= 0 {
		return usageError(stderr, fs, "--final-count must be positive")
	}
	if budget <= 0 {
		return usageError(stderr, fs, "--budget must be positive")
	}
	sidecarURL = strings.TrimSpace(sidecarURL)
	useFakeMirrorConfig := hasConfig && cfg.Sidecar.Enabled && cfg.Sidecar.Adapter == "fake" && !explicit["sidecar-url"]
	if useMirror && sidecarURL == "" && !useFakeMirrorConfig {
		return usageError(stderr, fs, "--sidecar-url is required when --use-mirror is set")
	}
	if useMirror && sidecarURL != "" && !useFakeMirrorConfig {
		if err := internalmirror.ValidateLoopbackURL(sidecarURL); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	}
	parsedNow, err := parseOptionalTime(now, "--now")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	openOpts := memorycore.Options{
		DBPath:      opts.DBPath,
		PersonaID:   opts.PersonaID,
		AutoMigrate: opts.AutoMigrate,
		EnableFTS:   opts.EnableFTS,
	}
	if hasConfig && cfg.Sidecar.Enabled && !explicit["sidecar-url"] {
		adapter, err := cfg.NewMirrorAdapter()
		if err != nil {
			return usageError(stderr, fs, err.Error())
		}
		openOpts.MirrorAdapter = adapter
	} else if useMirror && sidecarURL != "" {
		openOpts.MirrorAdapter = memorycore.NewSidecarMirrorAdapter(sidecarURL)
	}
	svc, err := memorycore.Open(ctx, openOpts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		PersonaID: opts.PersonaID,
		SessionID: stringPtr(sessionID),
		QueryText: query,
		Now:       parsedNow,
		Policy: memorycore.RetrievalPolicy{
			SensitivityPermission: sensitivity,
			AllowHistorical:       allowHistorical,
			AllowDeepArchive:      allowDeepArchive,
			FinalMemoryCount:      finalCount,
			ContextBudgetTokens:   budget,
			UseFTS:                useFTS,
			UseMirror:             useMirror,
		},
		Context: memorycore.RetrievalAffectContext{
			UserMoodLabel:         userMood,
			RelationshipMoodLabel: relationshipMood,
		},
	})
	if err != nil {
		return runtimeError(stderr, "retrieve: %v", err)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	for _, block := range result.Blocks {
		fmt.Fprintf(stdout, "block=%s\n", block.BlockType)
		for _, item := range block.Items {
			fmt.Fprintf(stdout, "- %s %s confidence=%s\n", item.NodeType, item.NodeID, trimFloat(item.Confidence))
			fmt.Fprintf(stdout, "  summary: %s\n", item.Summary)
			if item.UsageGuidance != "" {
				fmt.Fprintf(stdout, "  guidance: %s\n", item.UsageGuidance)
			}
		}
	}
	if len(result.DoNotMention) > 0 {
		fmt.Fprintln(stdout, "do_not_mention:")
		for _, item := range result.DoNotMention {
			fmt.Fprintf(stdout, "- %s %s reason=%s\n", item.NodeType, item.NodeID, item.Reason)
		}
	}
	if result.Mirror != nil {
		fmt.Fprintf(stdout, "mirror_status=%s\n", result.Mirror.Status)
		fmt.Fprintf(stdout, "mirror_candidates sidecar=%d mapped=%d dropped=%d\n",
			result.Mirror.SidecarCandidateCount,
			result.Mirror.MappedCandidateCount,
			result.Mirror.DroppedCandidateCount,
		)
	}
	fmt.Fprintf(stdout, "token_estimate=%d\n", result.TokenEstimate)
	return 0
}

func runRebuildSearch(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("rebuild-search", stderr)
	var opts commonOptions
	addCommonFlags(fs, &opts, formatText)
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.RebuildSearchDocuments(ctx, memorycore.RebuildSearchDocumentsRequest{PersonaID: opts.PersonaID})
	if err != nil {
		return runtimeError(stderr, "rebuild search: %v", err)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "upserted=%d\n", result.Upserted)
	return 0
}
