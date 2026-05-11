package memorycore_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceRunRetentionExpiresFactAndKeepsHistoricalRetrieval(t *testing.T) {
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

	result, err := svc.RunRetention(ctx, memorycore.RunRetentionRequest{Now: retentionNow})
	if err != nil {
		t.Fatalf("run retention: %v", err)
	}
	if result.EvaluatedFacts != 1 || result.ExpiredFacts != 1 || result.ArchivedFacts != 1 || result.SearchDocumentsSynced != 1 || result.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("retention result = %#v", result)
	}

	currentOnly, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "上线准备"})
	if err != nil {
		t.Fatalf("retrieve current only: %v", err)
	}
	requireNoMemoryItem(t, currentOnly, fact.ID)

	historical, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "上线准备",
		Policy: memorycore.RetrievalPolicy{
			AllowHistorical: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve historical: %v", err)
	}
	requireMemoryItem(t, historical, fact.ID, "用户近期忙于上线准备。", "historical")
}

func TestServiceRunRetentionDryRunReportsCountsWithoutChangingRetrieval(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	retentionNow := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	setServiceFactValidTo(t, db, fact.ID, retentionNow.Add(-time.Hour))

	result, err := svc.RunRetention(ctx, memorycore.RunRetentionRequest{Now: retentionNow, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run retention: %v", err)
	}
	if result.EvaluatedFacts != 1 || result.ExpiredFacts != 1 || result.ArchivedFacts != 1 || result.SearchDocumentsSynced != 0 || result.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("dry-run result = %#v", result)
	}

	current, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
	if err != nil {
		t.Fatalf("retrieve after dry-run: %v", err)
	}
	requireMemoryItem(t, current, fact.ID, "用户喜欢咖啡。", "")
}

func setServiceFactValidTo(t *testing.T, db *sql.DB, factID string, validTo time.Time) {
	t.Helper()

	if _, err := db.Exec(`
UPDATE facts
SET valid_to = ?
WHERE id = ?`, validTo.UTC().Format(time.RFC3339Nano), factID); err != nil {
		t.Fatalf("set fact valid_to: %v", err)
	}
}
