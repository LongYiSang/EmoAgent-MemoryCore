package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runNaturalMemoryRun(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("natural-memory-run", stderr)
	var opts commonOptions
	var mode string
	var nowValue string
	var timezone string
	var localDate string
	var localTime string
	var force bool
	var dryRun bool
	var explain bool
	var markSleepCycle bool
	var maxCandidates int
	var maxWrites int
	var algorithmVersion string
	addCommonFlags(fs, &opts, formatText)
	addConfigFlag(fs, &opts)
	fs.StringVar(&mode, "mode", string(memorycore.NaturalMemoryRunManual), "run mode: sleep_cycle|manual|api|test")
	fs.StringVar(&nowValue, "now", "", "RFC3339 now")
	fs.StringVar(&timezone, "timezone", "", "IANA timezone")
	fs.StringVar(&localDate, "local-date", "", "local date YYYY-MM-DD")
	fs.StringVar(&localTime, "local-time", "", "local time HH:mm")
	fs.BoolVar(&force, "force", false, "bypass scheduler guards")
	fs.BoolVar(&dryRun, "dry-run", false, "preview natural memory changes without writing")
	fs.BoolVar(&explain, "explain", false, "include explain items")
	fs.BoolVar(&markSleepCycle, "mark-sleep-cycle", false, "manual run consumes sleep_cycle quota")
	fs.IntVar(&maxCandidates, "max-candidates", 0, "override natural_memory.limits.max_candidates_per_run")
	fs.IntVar(&maxWrites, "max-writes", 0, "override natural_memory.limits.max_writes_per_run")
	fs.StringVar(&algorithmVersion, "algorithm-version", "", "override natural memory algorithm version")
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
		applyNaturalMemoryFlagOverrides(&cfg, explicit, timezone, localTime, maxCandidates, maxWrites, algorithmVersion, stderr)
		if err := cfg.Validate(); err != nil {
			return usageError(stderr, fs, err.Error())
		}
	} else {
		cfg = memconfig.DefaultConfig()
		cfg.Core.DBPath = opts.DBPath
		cfg.Core.PersonaID = opts.PersonaID
		cfg.Core.AutoMigrate = opts.AutoMigrate
		cfg.Core.EnableFTS = opts.EnableFTS
		applyNaturalMemoryFlagOverrides(&cfg, explicit, timezone, localTime, maxCandidates, maxWrites, algorithmVersion, stderr)
	}
	if !requireDB(stderr, fs, opts.DBPath) {
		return 2
	}
	if err := validateFormat(opts.Format, formatText, formatJSON); err != nil {
		return usageError(stderr, fs, err.Error())
	}
	runKind, err := parseNaturalMemoryRunKind(mode)
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	parsedNow, err := parseOptionalTime(nowValue, "--now")
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if strings.TrimSpace(localDate) != "" {
		if _, err := time.Parse("2006-01-02", localDate); err != nil {
			return usageError(stderr, fs, "--local-date must be YYYY-MM-DD")
		}
	}
	if strings.TrimSpace(localTime) != "" {
		if _, err := time.Parse("15:04", localTime); err != nil {
			return usageError(stderr, fs, "--local-time must be HH:mm")
		}
	}

	runtime, err := cfg.Runtime()
	if err != nil {
		return runtimeError(stderr, "load memorycore runtime: %v", err)
	}
	openOpts := runtime.Options
	if !parsedNow.IsZero() {
		openOpts.Now = func() time.Time { return parsedNow }
	}
	ctx := context.Background()
	svc, err := memorycore.Open(ctx, openOpts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	var result *memorycore.RunNaturalMemoryCycleResult
	runOptions := cfg.NaturalMemoryOptions()
	if runKind == memorycore.NaturalMemoryRunSleepCycle {
		result, err = svc.Ops().RunNaturalMemoryTick(ctx, memorycore.RunNaturalMemoryTickRequest{
			PersonaID: opts.PersonaID,
			Now:       parsedNow,
			DryRun:    dryRun,
			Force:     force,
			Explain:   explain,
			LocalDate: localDate,
			LocalTime: firstNonEmptyCLI(localTime, cfg.NaturalMemory.SleepCycle.LocalTime),
			Timezone:  firstNonEmptyCLI(timezone, cfg.NaturalMemory.SleepCycle.Timezone, cfg.Core.Timezone),
			Options:   runOptions,
		})
	} else {
		result, err = svc.Ops().RunNaturalMemoryCycle(ctx, memorycore.RunNaturalMemoryCycleRequest{
			PersonaID:      opts.PersonaID,
			Now:            parsedNow,
			DryRun:         dryRun,
			Force:          force,
			Explain:        explain,
			RunKind:        runKind,
			LocalDate:      localDate,
			LocalTime:      firstNonEmptyCLI(localTime, cfg.NaturalMemory.SleepCycle.LocalTime),
			Timezone:       firstNonEmptyCLI(timezone, cfg.NaturalMemory.SleepCycle.Timezone, cfg.Core.Timezone),
			MarkSleepCycle: markSleepCycle,
			Options:        runOptions,
		})
	}
	if err != nil {
		return runtimeError(stderr, "natural memory run: %v", err)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "status=%s\n", result.Status)
	fmt.Fprintf(stdout, "run_kind=%s\n", result.RunKind)
	fmt.Fprintf(stdout, "dry_run=%s\n", boolText(result.DryRun))
	fmt.Fprintf(stdout, "evaluated_nodes=%d\n", result.EvaluatedNodes)
	fmt.Fprintf(stdout, "search_tier_updates=%d\n", result.SearchTierUpdates)
	fmt.Fprintf(stdout, "mirror_updates_enqueued=%d\n", result.MirrorUpdatesEnqueued)
	fmt.Fprintf(stdout, "compression_candidates=%d\n", result.CompressionCandidates)
	return 0
}

func parseNaturalMemoryRunKind(mode string) (memorycore.NaturalMemoryRunKind, error) {
	switch strings.TrimSpace(mode) {
	case string(memorycore.NaturalMemoryRunSleepCycle):
		return memorycore.NaturalMemoryRunSleepCycle, nil
	case string(memorycore.NaturalMemoryRunManual):
		return memorycore.NaturalMemoryRunManual, nil
	case string(memorycore.NaturalMemoryRunAPI):
		return memorycore.NaturalMemoryRunAPI, nil
	case string(memorycore.NaturalMemoryRunTest):
		return memorycore.NaturalMemoryRunTest, nil
	default:
		return "", fmt.Errorf("--mode must be one of sleep_cycle|manual|api|test")
	}
}

func applyNaturalMemoryFlagOverrides(cfg *memconfig.Config, explicit map[string]bool, timezone string, localTime string, maxCandidates int, maxWrites int, algorithmVersion string, stderr io.Writer) {
	if explicit["timezone"] {
		warnConfigOverride(stderr, "timezone", "natural_memory.sleep_cycle.timezone")
		cfg.NaturalMemory.SleepCycle.Timezone = timezone
	}
	if explicit["local-time"] {
		warnConfigOverride(stderr, "local-time", "natural_memory.sleep_cycle.local_time")
		cfg.NaturalMemory.SleepCycle.LocalTime = localTime
	}
	if explicit["max-candidates"] {
		warnConfigOverride(stderr, "max-candidates", "natural_memory.limits.max_candidates_per_run")
		cfg.NaturalMemory.Limits.MaxCandidatesPerRun = maxCandidates
	}
	if explicit["max-writes"] {
		warnConfigOverride(stderr, "max-writes", "natural_memory.limits.max_writes_per_run")
		cfg.NaturalMemory.Limits.MaxWritesPerRun = maxWrites
	}
	if explicit["algorithm-version"] {
		warnConfigOverride(stderr, "algorithm-version", "natural_memory.algorithm_version")
		cfg.NaturalMemory.AlgorithmVersion = algorithmVersion
	}
}
