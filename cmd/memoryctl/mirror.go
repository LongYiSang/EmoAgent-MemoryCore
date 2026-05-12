package main

import (
	"context"
	"fmt"
	"io"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runMirrorSync(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("mirror-sync-run", stderr)
	var opts commonOptions
	var limit int
	var fakeAdapter bool
	addCommonFlags(fs, &opts, formatText)
	fs.IntVar(&limit, "limit", 100, "maximum queue rows to process")
	fs.BoolVar(&fakeAdapter, "fake-adapter", false, "use the Phase 4A deterministic fake mirror adapter")
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
	if !fakeAdapter {
		return usageError(stderr, fs, "--fake-adapter is required in Phase 4A")
	}

	ctx := context.Background()
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:        opts.DBPath,
		PersonaID:     opts.PersonaID,
		AutoMigrate:   opts.AutoMigrate,
		EnableFTS:     opts.EnableFTS,
		MirrorAdapter: memorycore.NewFakeMirrorAdapter(),
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
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "claimed=%d\n", result.Claimed)
	fmt.Fprintf(stdout, "completed=%d\n", result.Completed)
	fmt.Fprintf(stdout, "failed=%d\n", result.Failed)
	fmt.Fprintf(stdout, "skipped=%d\n", result.Skipped)
	return 0
}
