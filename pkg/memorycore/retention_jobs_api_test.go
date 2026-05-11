package memorycore_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceRunRetentionJobsDefaultsToDailyTTLExpiry(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我最近忙上线准备。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "is_busy_with", "上线准备", "用户近期忙于上线准备。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	retentionNow := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	setServiceFactValidTo(t, db, fact.ID, retentionNow.Add(-time.Hour))

	result, err := svc.RunRetentionJobs(ctx, memorycore.RunRetentionJobsRequest{Now: retentionNow})
	if err != nil {
		t.Fatalf("run retention jobs: %v", err)
	}
	requireRetentionJobNames(t, result, memorycore.RetentionJobDailyTTLExpiry)
	requireRetentionCounts(t, result.Retention, 1, 1, 1, 0, 1, 0)
	requireServiceFactState(t, db, fact.ID, "invalidated", "archived")
}

func TestServiceRunRetentionJobsDailyAndMonthlyCombineCounts(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	expiringEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我最近忙上线准备。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	expiring := consolidateLiteral(t, ctx, svc, userID, "is_busy_with", "上线准备", "用户近期忙于上线准备。", expiringEpisode.ID).Fact
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我以前参与过旧项目。", time.Date(2025, 1, 10, 9, 0, 0, 0, time.UTC))
	oldArchived := consolidateLiteral(t, ctx, svc, userID, "likes", "旧项目", "用户以前参与过旧项目。", oldEpisode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	retentionNow := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	setServiceFactValidTo(t, db, expiring.ID, retentionNow.Add(-time.Hour))
	setServiceFactArchivedAt(t, db, oldArchived.ID, retentionNow.AddDate(0, 0, -181))

	result, err := svc.RunRetentionJobs(ctx, memorycore.RunRetentionJobsRequest{
		Now:                  retentionNow,
		Jobs:                 []memorycore.RetentionJobName{memorycore.RetentionJobDailyTTLExpiry, memorycore.RetentionJobMonthlyDeepArchive},
		DeepArchiveAfterDays: 180,
	})
	if err != nil {
		t.Fatalf("run retention jobs: %v", err)
	}
	requireRetentionJobNames(t, result, memorycore.RetentionJobDailyTTLExpiry, memorycore.RetentionJobMonthlyDeepArchive)
	requireRetentionCounts(t, result.Retention, 2, 1, 1, 1, 2, 0)
	requireServiceFactState(t, db, expiring.ID, "invalidated", "archived")
	requireServiceFactState(t, db, oldArchived.ID, "valid", "deep_archived")
}

func TestServiceRunRetentionJobsRejectsMonthlyWithoutPositiveDeepArchiveDaysBeforeMutating(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我以前参与过旧项目。", time.Date(2025, 1, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "旧项目", "用户以前参与过旧项目。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	setServiceFactArchivedAt(t, db, fact.ID, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	_, err := svc.RunRetentionJobs(ctx, memorycore.RunRetentionJobsRequest{
		Now:  time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Jobs: []memorycore.RetentionJobName{memorycore.RetentionJobMonthlyDeepArchive},
	})
	if !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	requireServiceFactState(t, db, fact.ID, "valid", "archived")
}

func TestServiceRunRetentionJobsRejectsUnknownJobBeforeMutating(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我最近忙上线准备。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "is_busy_with", "上线准备", "用户近期忙于上线准备。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	retentionNow := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	setServiceFactValidTo(t, db, fact.ID, retentionNow.Add(-time.Hour))

	_, err := svc.RunRetentionJobs(ctx, memorycore.RunRetentionJobsRequest{
		Now:  retentionNow,
		Jobs: []memorycore.RetentionJobName{"weekly_unknown"},
	})
	if !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	requireServiceFactState(t, db, fact.ID, "valid", "active")
}

func TestServiceRunRetentionJobsDryRunAcrossSelectedJobsDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	expiringEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我最近忙上线准备。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	expiring := consolidateLiteral(t, ctx, svc, userID, "is_busy_with", "上线准备", "用户近期忙于上线准备。", expiringEpisode.ID).Fact
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我以前参与过旧项目。", time.Date(2025, 1, 10, 9, 0, 0, 0, time.UTC))
	oldArchived := consolidateLiteral(t, ctx, svc, userID, "likes", "旧项目", "用户以前参与过旧项目。", oldEpisode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	retentionNow := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	setServiceFactValidTo(t, db, expiring.ID, retentionNow.Add(-time.Hour))
	setServiceFactArchivedAt(t, db, oldArchived.ID, retentionNow.AddDate(0, 0, -181))

	result, err := svc.RunRetentionJobs(ctx, memorycore.RunRetentionJobsRequest{
		Now:                  retentionNow,
		DryRun:               true,
		Jobs:                 []memorycore.RetentionJobName{memorycore.RetentionJobDailyTTLExpiry, memorycore.RetentionJobMonthlyDeepArchive},
		DeepArchiveAfterDays: 180,
	})
	if err != nil {
		t.Fatalf("dry-run retention jobs: %v", err)
	}
	requireRetentionJobNames(t, result, memorycore.RetentionJobDailyTTLExpiry, memorycore.RetentionJobMonthlyDeepArchive)
	requireRetentionCounts(t, result.Retention, 2, 1, 1, 1, 0, 0)
	requireServiceFactState(t, db, expiring.ID, "valid", "active")
	requireServiceFactState(t, db, oldArchived.ID, "valid", "archived")
}

func requireRetentionJobNames(t *testing.T, result *memorycore.RunRetentionJobsResult, names ...memorycore.RetentionJobName) {
	t.Helper()

	if result == nil {
		t.Fatalf("result is nil")
	}
	if len(result.Jobs) != len(names) {
		t.Fatalf("job count = %d, want %d: %#v", len(result.Jobs), len(names), result.Jobs)
	}
	for i, name := range names {
		if result.Jobs[i].Name != name || result.Jobs[i].Skipped || result.Jobs[i].Reason != "" {
			t.Fatalf("job[%d] = %#v, want name %q not skipped", i, result.Jobs[i], name)
		}
	}
}

func requireRetentionCounts(t *testing.T, result memorycore.RunRetentionResult, evaluated, expired, archived, deepArchived, synced, enqueued int) {
	t.Helper()

	if result.EvaluatedFacts != evaluated ||
		result.ExpiredFacts != expired ||
		result.ArchivedFacts != archived ||
		result.DeepArchivedFacts != deepArchived ||
		result.SearchDocumentsSynced != synced ||
		result.MirrorUpdatesEnqueued != enqueued {
		t.Fatalf("retention result = %#v", result)
	}
}

func requireServiceFactState(t *testing.T, db *sql.DB, factID string, wantValidity string, wantLifecycle string) {
	t.Helper()

	var validity, lifecycle string
	if err := db.QueryRow(`
SELECT validity_status, lifecycle_status
FROM facts
WHERE id = ?`, factID).Scan(&validity, &lifecycle); err != nil {
		t.Fatalf("query fact state: %v", err)
	}
	if validity != wantValidity || lifecycle != wantLifecycle {
		t.Fatalf("fact state = %s/%s, want %s/%s", validity, lifecycle, wantValidity, wantLifecycle)
	}
}
