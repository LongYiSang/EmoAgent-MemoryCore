package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func runRetentionJobs(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newFlagSet("retention-jobs-run", stderr)
	var opts commonOptions
	var now string
	var jobsValue string
	var dryRun bool
	var deepArchiveAfterDays int
	addCommonFlags(fs, &opts, formatText)
	fs.StringVar(&now, "now", "", "RFC3339 now")
	fs.StringVar(&jobsValue, "jobs", "", "comma-separated retention jobs")
	fs.BoolVar(&dryRun, "dry-run", false, "preview retention job changes without mutating")
	fs.IntVar(&deepArchiveAfterDays, "deep-archive-after-days", 0, "required positive day threshold for monthly_deep_archive")
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
	if deepArchiveAfterDays < 0 {
		return usageError(stderr, fs, "--deep-archive-after-days must be >= 0")
	}
	jobs, err := parseRetentionJobNames(jobsValue)
	if err != nil {
		return usageError(stderr, fs, err.Error())
	}
	if hasRetentionJob(jobs, memorycore.RetentionJobMonthlyDeepArchive) && deepArchiveAfterDays <= 0 {
		return usageError(stderr, fs, "monthly_deep_archive requires --deep-archive-after-days > 0")
	}

	ctx := context.Background()
	svc, err := openService(ctx, opts)
	if err != nil {
		return runtimeError(stderr, "open memorycore: %v", err)
	}
	defer svc.Close()

	result, err := svc.RunRetentionJobs(ctx, memorycore.RunRetentionJobsRequest{
		PersonaID:            opts.PersonaID,
		Now:                  parsedNow,
		DryRun:               dryRun,
		Jobs:                 jobs,
		DeepArchiveAfterDays: deepArchiveAfterDays,
	})
	if err != nil {
		if errors.Is(err, memorycore.ErrInvalidRequest) {
			return usageError(stderr, fs, err.Error())
		}
		return runtimeError(stderr, "retention jobs run: %v", err)
	}
	if opts.Format == formatJSON {
		return writeJSON(stdout, result, opts.Pretty)
	}
	fmt.Fprintf(stdout, "jobs=%s\n", retentionJobNamesText(result.Jobs))
	fmt.Fprintf(stdout, "evaluated_facts=%d\n", result.Retention.EvaluatedFacts)
	fmt.Fprintf(stdout, "expired_facts=%d\n", result.Retention.ExpiredFacts)
	fmt.Fprintf(stdout, "archived_facts=%d\n", result.Retention.ArchivedFacts)
	fmt.Fprintf(stdout, "deep_archived_facts=%d\n", result.Retention.DeepArchivedFacts)
	fmt.Fprintf(stdout, "search_documents_synced=%d\n", result.Retention.SearchDocumentsSynced)
	fmt.Fprintf(stdout, "mirror_updates_enqueued=%d\n", result.Retention.MirrorUpdatesEnqueued)
	return 0
}

func parseRetentionJobNames(value string) ([]memorycore.RetentionJobName, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	jobs := make([]memorycore.RetentionJobName, 0, len(parts))
	for _, part := range parts {
		name := memorycore.RetentionJobName(strings.TrimSpace(part))
		switch name {
		case memorycore.RetentionJobDailyTTLExpiry, memorycore.RetentionJobMonthlyDeepArchive:
			jobs = append(jobs, name)
		default:
			return nil, fmt.Errorf("unknown retention job %q", name)
		}
	}
	return jobs, nil
}

func hasRetentionJob(jobs []memorycore.RetentionJobName, target memorycore.RetentionJobName) bool {
	for _, job := range jobs {
		if job == target {
			return true
		}
	}
	return false
}

func retentionJobNamesText(jobs []memorycore.RetentionJobResult) string {
	names := make([]string, 0, len(jobs))
	for _, job := range jobs {
		names = append(names, string(job.Name))
	}
	return strings.Join(names, ",")
}
