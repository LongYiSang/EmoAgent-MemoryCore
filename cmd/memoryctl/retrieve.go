package main

import (
	"context"
	"fmt"
	"io"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runRetrieve(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("retrieve", stderr)
	var opts commonOptions
	var query, sessionID, now, sensitivity, userMood, relationshipMood string
	var allowHistorical, allowDeepArchive, useFTS bool
	var finalCount, budget int
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&query, "query", "", "query text")
	fs.StringVar(&sessionID, "session", "", "session id")
	fs.StringVar(&now, "now", "", "RFC3339 now")
	fs.StringVar(&sensitivity, "sensitivity", memorycore.SensitivityNormal, "sensitivity permission")
	fs.StringVar(&sensitivity, "sensitivity-permission", memorycore.SensitivityNormal, "sensitivity permission")
	fs.BoolVar(&allowHistorical, "allow-historical", false, "allow historical facts")
	fs.BoolVar(&allowDeepArchive, "allow-deep-archive", false, "allow deep archive")
	fs.IntVar(&finalCount, "final-count", 8, "final memory count")
	fs.IntVar(&budget, "budget", 1500, "context budget tokens")
	fs.BoolVar(&useFTS, "use-fts", false, "use FTS")
	fs.StringVar(&userMood, "user-mood", "", "user mood label")
	fs.StringVar(&relationshipMood, "relationship-mood", "", "relationship mood label")
	if !parseFlags(fs, args) {
		return 2
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
