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
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&now, "now", "", "RFC3339 now")
	fs.BoolVar(&dryRun, "dry-run", false, "preview retention changes without mutating")
	if !parseFlags(fs, args) {
		return 2
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

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.RunRetention(ctx, memorycore.RunRetentionRequest{
		PersonaID: opts.PersonaID,
		Now:       parsedNow,
		DryRun:    dryRun,
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
	fmt.Fprintf(stdout, "search_documents_synced=%d\n", result.SearchDocumentsSynced)
	fmt.Fprintf(stdout, "mirror_updates_enqueued=%d\n", result.MirrorUpdatesEnqueued)
	return 0
}
