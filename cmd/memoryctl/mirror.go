package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	internalmirror "github.com/longyisang/emoagent-memorycore/internal/mirror"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runMirrorSync(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("mirror-sync-run", stderr)
	var opts commonOptions
	var limit int
	var fakeAdapter bool
	var sidecarURL string
	addCommonFlags(fs, &opts, formatText)
	fs.IntVar(&limit, "limit", 100, "maximum queue rows to process")
	fs.BoolVar(&fakeAdapter, "fake-adapter", false, "use the Phase 4A deterministic fake mirror adapter")
	fs.StringVar(&sidecarURL, "sidecar-url", "", "loopback HTTP URL for the Python mirror sidecar")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if limit <= 0 {
		return usageError(stderr, fs, "--limit must be positive")
	}
	sidecarURL = strings.TrimSpace(sidecarURL)
	if !fakeAdapter && sidecarURL == "" {
		return usageError(stderr, fs, "--fake-adapter or --sidecar-url is required")
	}
	if fakeAdapter && sidecarURL != "" {
		return usageError(stderr, fs, "choose only one of --fake-adapter or --sidecar-url")
	}
	if sidecarURL != "" {
		if err := internalmirror.ValidateLoopbackURL(sidecarURL); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	}

	adapter := memorycore.NewFakeMirrorAdapter()
	if sidecarURL != "" {
		adapter = memorycore.NewSidecarMirrorAdapter(sidecarURL)
	}

	ctx := context.Background()
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:        opts.DBPath,
		PersonaID:     opts.PersonaID,
		AutoMigrate:   opts.AutoMigrate,
		EnableFTS:     opts.EnableFTS,
		MirrorAdapter: adapter,
	})
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{
		PersonaID: opts.PersonaID,
		Limit:     limit,
	})
	if err != nil {
		return runtimeError(stderr, "mirror sync run: %v", err)
	}
	if result.Failed > 0 {
		return runtimeError(stderr, "mirror sync failed rows: claimed=%d completed=%d failed=%d skipped=%d", result.Claimed, result.Completed, result.Failed, result.Skipped)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "claimed=%d\n", result.Claimed)
	fmt.Fprintf(stdout, "completed=%d\n", result.Completed)
	fmt.Fprintf(stdout, "failed=%d\n", result.Failed)
	fmt.Fprintf(stdout, "skipped=%d\n", result.Skipped)
	return 0
}

func runMirrorRebuild(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("mirror-rebuild", stderr)
	var opts commonOptions
	var sidecarURL string
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&sidecarURL, "sidecar-url", "", "loopback HTTP URL for the Python mirror sidecar")
	if !parseFlags(fs, args) {
		return 2
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	sidecarURL = strings.TrimSpace(sidecarURL)
	if sidecarURL == "" {
		return usageError(stderr, fs, "--sidecar-url is required")
	}
	if err := internalmirror.ValidateLoopbackURL(sidecarURL); err != nil {
		return usageError(stderr, fs, err.Error())
	}

	ctx := context.Background()
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:        opts.DBPath,
		PersonaID:     opts.PersonaID,
		AutoMigrate:   opts.AutoMigrate,
		EnableFTS:     opts.EnableFTS,
		MirrorAdapter: memorycore.NewSidecarMirrorAdapter(sidecarURL),
	})
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{PersonaID: opts.PersonaID})
	if err != nil {
		return runtimeError(stderr, "mirror rebuild: %v", err)
	}
	if result.Failed > 0 {
		return runtimeError(stderr, "mirror rebuild failed rows: nodes_upserted=%d edges_upserted=%d failed=%d skipped=%d", result.NodesUpserted, result.EdgesUpserted, result.Failed, result.Skipped)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "nodes_upserted=%d\n", result.NodesUpserted)
	fmt.Fprintf(stdout, "edges_upserted=%d\n", result.EdgesUpserted)
	fmt.Fprintf(stdout, "failed=%d\n", result.Failed)
	fmt.Fprintf(stdout, "skipped=%d\n", result.Skipped)
	return 0
}
