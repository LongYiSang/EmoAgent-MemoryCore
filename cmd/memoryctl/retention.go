package main

import (
	"context"
	"fmt"
	"io"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runRetention(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("retention-run", stderr)
	var opts commonOptions
	var now string
	var dryRun bool
	var deepArchiveAfterDays int
	addCommonFlags(fs, &opts, formatText)
	addConfigFlag(fs, &opts)
	fs.StringVar(&now, "now", "", "RFC3339 now")
	fs.BoolVar(&dryRun, "dry-run", false, "preview retention changes without mutating")
	fs.IntVar(&deepArchiveAfterDays, "deep-archive-after-days", 0, "move archived facts older than this many days to deep archive; 0 disables")
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
		if explicit["deep-archive-after-days"] {
			warnConfigOverride(stderr, "deep-archive-after-days", "retention.deep_archive_after_days")
			cfg.Retention.DeepArchiveAfterDays = deepArchiveAfterDays
		} else {
			deepArchiveAfterDays = cfg.Retention.DeepArchiveAfterDays
		}
		if err := cfg.Validate(); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	parsedNow, err := parseOptionalTime(now, "--now")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if deepArchiveAfterDays < 0 {
		return usageError(stderr, fs, "--deep-archive-after-days must be >= 0")
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.RunRetention(ctx, memorycore.RunRetentionRequest{
		PersonaID:            opts.PersonaID,
		Now:                  parsedNow,
		DryRun:               dryRun,
		DeepArchiveAfterDays: deepArchiveAfterDays,
	})
	if err != nil {
		return runtimeError(stderr, "retention run: %v", err)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "evaluated_facts=%d\n", result.EvaluatedFacts)
	fmt.Fprintf(stdout, "expired_facts=%d\n", result.ExpiredFacts)
	fmt.Fprintf(stdout, "archived_facts=%d\n", result.ArchivedFacts)
	fmt.Fprintf(stdout, "deep_archived_facts=%d\n", result.DeepArchivedFacts)
	fmt.Fprintf(stdout, "search_documents_synced=%d\n", result.SearchDocumentsSynced)
	fmt.Fprintf(stdout, "mirror_updates_enqueued=%d\n", result.MirrorUpdatesEnqueued)
	return 0
}
