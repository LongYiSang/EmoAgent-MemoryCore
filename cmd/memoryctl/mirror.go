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
	addConfigFlag(fs, &opts)
	fs.IntVar(&limit, "limit", 100, "maximum queue rows to process")
	fs.BoolVar(&fakeAdapter, "fake-adapter", false, "use the Phase 4A deterministic fake mirror adapter")
	fs.StringVar(&sidecarURL, "sidecar-url", "", "loopback HTTP URL for the Python mirror sidecar")
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
		if explicit["limit"] {
			warnConfigOverride(stderr, "limit", "mirror.sync_limit")
			cfg.Mirror.SyncLimit = limit
		} else {
			limit = cfg.Mirror.SyncLimit
		}
		if explicit["fake-adapter"] {
			warnConfigOverride(stderr, "fake-adapter", "sidecar.adapter")
			cfg.Sidecar.Enabled = true
			cfg.Sidecar.Adapter = "fake"
			cfg.Sidecar.URL = ""
		} else if explicit["sidecar-url"] {
			warnConfigOverride(stderr, "sidecar-url", "sidecar.url")
			cfg.Sidecar.Enabled = true
			cfg.Sidecar.Adapter = "trivium"
			cfg.Sidecar.URL = sidecarURL
		} else if cfg.Sidecar.Enabled {
			fakeAdapter = cfg.Sidecar.Adapter == "fake"
			if fakeAdapter {
				sidecarURL = ""
				cfg.Sidecar.URL = ""
			} else {
				sidecarURL = cfg.Sidecar.URL
			}
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
	addConfigFlag(fs, &opts)
	fs.StringVar(&sidecarURL, "sidecar-url", "", "loopback HTTP URL for the Python mirror sidecar")
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
		if explicit["sidecar-url"] {
			warnConfigOverride(stderr, "sidecar-url", "sidecar.url")
			cfg.Sidecar.Enabled = true
			cfg.Sidecar.Adapter = "trivium"
			cfg.Sidecar.URL = sidecarURL
		} else if cfg.Sidecar.Enabled {
			sidecarURL = cfg.Sidecar.URL
			if cfg.Sidecar.Adapter == "fake" {
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
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	sidecarURL = strings.TrimSpace(sidecarURL)
	useFakeMirrorConfig := hasConfig && cfg.Sidecar.Enabled && cfg.Sidecar.Adapter == "fake" && !explicit["sidecar-url"]
	if sidecarURL == "" && !useFakeMirrorConfig {
		return usageError(stderr, fs, "--sidecar-url is required")
	}
	if sidecarURL != "" {
		if err := internalmirror.ValidateLoopbackURL(sidecarURL); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	}

	adapter := memorycore.NewSidecarMirrorAdapter(sidecarURL)
	if useFakeMirrorConfig {
		adapter = memorycore.NewFakeMirrorAdapter()
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
