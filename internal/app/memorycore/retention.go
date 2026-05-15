package memorycore

import (
	"context"
	"fmt"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func (s *service) RunRetention(ctx context.Context, req RunRetentionRequest) (*RunRetentionResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	result, err := s.retention.Run(ctx, memsqlite.RetentionRequest{
		PersonaID:            personaID,
		Now:                  req.Now,
		DryRun:               req.DryRun,
		DeepArchiveAfterDays: req.DeepArchiveAfterDays,
	})
	if err != nil {
		return nil, err
	}
	return retentionResultFromStore(result), nil
}

func (s *service) RunRetentionJobs(ctx context.Context, req RunRetentionJobsRequest) (*RunRetentionJobsResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	jobs := normalizeRetentionJobs(req.Jobs)
	if err := validateRetentionJobs(jobs, req.DeepArchiveAfterDays); err != nil {
		return nil, err
	}

	result := &RunRetentionJobsResult{
		Jobs: make([]RetentionJobResult, 0, len(jobs)),
	}
	for _, job := range jobs {
		retention, err := s.runRetentionJob(ctx, personaID, req, job)
		if err != nil {
			return nil, err
		}
		result.Jobs = append(result.Jobs, RetentionJobResult{Name: job})
		addRetentionResult(&result.Retention, *retention)
	}
	return result, nil
}

func (s *service) runRetentionJob(ctx context.Context, personaID string, req RunRetentionJobsRequest, job RetentionJobName) (*RunRetentionResult, error) {
	if job == RetentionJobMonthlyDeepArchive {
		result, err := s.retention.Run(ctx, memsqlite.RetentionRequest{
			PersonaID:            personaID,
			Now:                  req.Now,
			DryRun:               req.DryRun,
			DeepArchiveAfterDays: req.DeepArchiveAfterDays,
			SkipExpiredFacts:     true,
		})
		if err != nil {
			return nil, err
		}
		return retentionResultFromStore(result), nil
	}
	return s.RunRetention(ctx, RunRetentionRequest{
		PersonaID: personaID,
		Now:       req.Now,
		DryRun:    req.DryRun,
	})
}

func normalizeRetentionJobs(jobs []RetentionJobName) []RetentionJobName {
	if len(jobs) == 0 {
		return []RetentionJobName{RetentionJobDailyTTLExpiry}
	}
	return append([]RetentionJobName(nil), jobs...)
}

func validateRetentionJobs(jobs []RetentionJobName, deepArchiveAfterDays int) error {
	for _, job := range jobs {
		switch job {
		case RetentionJobDailyTTLExpiry:
		case RetentionJobMonthlyDeepArchive:
			if deepArchiveAfterDays <= 0 {
				return fmt.Errorf("%w: monthly_deep_archive requires DeepArchiveAfterDays > 0", ErrInvalidRequest)
			}
		default:
			return fmt.Errorf("%w: unknown retention job %q", ErrInvalidRequest, job)
		}
	}
	return nil
}

func addRetentionResult(total *RunRetentionResult, next RunRetentionResult) {
	total.EvaluatedFacts += next.EvaluatedFacts
	total.ExpiredFacts += next.ExpiredFacts
	total.ArchivedFacts += next.ArchivedFacts
	total.DeepArchivedFacts += next.DeepArchivedFacts
	total.SearchDocumentsSynced += next.SearchDocumentsSynced
	total.MirrorUpdatesEnqueued += next.MirrorUpdatesEnqueued
}
