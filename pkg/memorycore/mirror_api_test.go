package memorycore_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	_ "modernc.org/sqlite"
)

func TestServiceRunMirrorSyncProcessesQueuedFactAndEdges(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openMirrorService(t, ctx, memorycore.NewFakeMirrorAdapter())
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	result, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 10})
	if err != nil {
		t.Fatalf("run mirror sync: %v", err)
	}
	if result.Claimed != 3 || result.Completed != 3 || result.Failed != 0 || result.Skipped != 1 {
		t.Fatalf("mirror sync result = %#v", result)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireMirrorQueueDoneForNode(t, db, "fact", fact.ID, "upsert_node")
	requireMirrorIndexForFact(t, db, fact.ID, "indexed")
}

func TestServiceRunMirrorSyncSkipsFactAfterSourceRedactedOnlyEvidence(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openMirrorService(t, ctx, memorycore.NewFakeMirrorAdapter())
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢乌龙茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "乌龙茶", "用户喜欢乌龙茶。", episode.ID).Fact

	if _, err := svc.Forget(ctx, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelSourceRedact,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeEpisode,
			NodeID:    episode.ID,
		},
	}); err != nil {
		t.Fatalf("source redact only evidence: %v", err)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	prioritizeMirrorQueueRow(t, db, "fact", fact.ID, "upsert_node")

	result, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1})
	if err != nil {
		t.Fatalf("run mirror sync: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 1 || result.Skipped != 1 || result.Failed != 0 {
		t.Fatalf("mirror sync result = %#v", result)
	}
	requireMirrorIndexMapCount(t, db, fact.ID, 0)
}

func TestServiceForgetSourceRedactEnqueuesMirrorDeleteForIndexedFactWithOnlyEvidence(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openMirrorService(t, ctx, memorycore.NewFakeMirrorAdapter())
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢普洱茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "普洱茶", "用户喜欢普洱茶。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	prioritizeMirrorQueueRow(t, db, "fact", fact.ID, "upsert_node")
	if _, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1}); err != nil {
		t.Fatalf("index fact: %v", err)
	}
	requireMirrorIndexForFact(t, db, fact.ID, "indexed")

	if _, err := svc.Forget(ctx, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelSourceRedact,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeEpisode,
			NodeID:    episode.ID,
		},
	}); err != nil {
		t.Fatalf("source redact only evidence: %v", err)
	}
	requireMirrorQueueCountForNode(t, db, "fact", fact.ID, "delete_node", 1)

	prioritizeMirrorQueueRow(t, db, "fact", fact.ID, "delete_node")
	result, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1})
	if err != nil {
		t.Fatalf("delete unsafe mirrored fact: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 1 || result.Failed != 0 {
		t.Fatalf("mirror delete result = %#v", result)
	}
	requireMirrorIndexForFactStatusOnly(t, db, fact.ID, "deleted")
}

func TestServiceRunMirrorSyncFailsRowWithoutUpdatingIndexMap(t *testing.T) {
	ctx := context.Background()
	adapter := failingPublicMirrorAdapter{err: errors.New("sidecar failed\nprivate payload should not leak")}
	svc, dbPath := openMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢乌龙茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "乌龙茶", "用户喜欢乌龙茶。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	prioritizeMirrorQueueRow(t, db, "fact", fact.ID, "upsert_node")

	result, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1})
	if err != nil {
		t.Fatalf("run mirror sync: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 0 || result.Failed != 1 {
		t.Fatalf("mirror sync result = %#v", result)
	}

	var status, errorMessage string
	var attempts int
	if err := db.QueryRow(`
SELECT status, attempts, COALESCE(error_message, '')
FROM index_sync_queue
WHERE node_type = 'fact' AND node_id = ? AND operation = 'upsert_node'`, fact.ID).Scan(&status, &attempts, &errorMessage); err != nil {
		t.Fatalf("query queue row: %v", err)
	}
	if status != "failed" || attempts != 1 {
		t.Fatalf("queue status/attempts = %s/%d, want failed/1", status, attempts)
	}
	if strings.ContainsAny(errorMessage, "\r\n\t") {
		t.Fatalf("error_message contains raw control whitespace: %q", errorMessage)
	}
	requireMirrorIndexMapCount(t, db, fact.ID, 0)
}

func TestServiceRunMirrorSyncDeleteNodeMarksMapDeletedIdempotently(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openMirrorService(t, ctx, memorycore.NewFakeMirrorAdapter())
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢普洱茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "普洱茶", "用户喜欢普洱茶。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	prioritizeMirrorQueueRow(t, db, "fact", fact.ID, "upsert_node")

	if _, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1}); err != nil {
		t.Fatalf("index fact: %v", err)
	}
	requireMirrorIndexForFact(t, db, fact.ID, "indexed")
	enqueueMirrorDeleteForFact(t, db, fact.ID, "delete_fact_once")

	result, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1})
	if err != nil {
		t.Fatalf("delete mirrored fact: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 1 || result.Failed != 0 {
		t.Fatalf("delete result = %#v", result)
	}
	requireMirrorIndexForFactStatusOnly(t, db, fact.ID, "deleted")

	enqueueMirrorDeleteForFact(t, db, "missing_mapped_fact", "delete_fact_twice")
	result, err = svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1})
	if err != nil {
		t.Fatalf("delete absent mapped fact: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 1 || result.Failed != 0 {
		t.Fatalf("idempotent delete result = %#v", result)
	}
}

func TestServiceRunMirrorSyncRequiresExplicitAdapter(t *testing.T) {
	ctx := context.Background()
	svc, _ := openMirrorService(t, ctx, nil)
	defer svc.Close()

	if _, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1}); !errors.Is(err, memorycore.ErrInvalidOptions) {
		t.Fatalf("RunMirrorSync err = %v, want ErrInvalidOptions", err)
	}
}

func prioritizeMirrorQueueRow(t *testing.T, db *sql.DB, nodeType string, nodeID string, operation string) {
	t.Helper()

	if _, err := db.Exec(`UPDATE index_sync_queue SET priority = 9`); err != nil {
		t.Fatalf("deprioritize queue rows: %v", err)
	}
	if _, err := db.Exec(`
UPDATE index_sync_queue
SET priority = 0
WHERE node_type = ? AND node_id = ? AND operation = ?`, nodeType, nodeID, operation); err != nil {
		t.Fatalf("prioritize queue row: %v", err)
	}
}

func openMirrorService(t *testing.T, ctx context.Context, adapter memorycore.MirrorAdapter) (memorycore.Service, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:        dbPath,
		AutoMigrate:   true,
		MirrorAdapter: adapter,
		Now: func() time.Time {
			return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	return svc, dbPath
}

type failingPublicMirrorAdapter struct {
	err error
}

func (f failingPublicMirrorAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	return memorycore.MirrorNodeUpsertResult{}, f.err
}

func (f failingPublicMirrorAdapter) DeleteNode(ctx context.Context, ref memorycore.MirrorNodeRef) error {
	return f.err
}

func (f failingPublicMirrorAdapter) UpsertEdge(ctx context.Context, payload memorycore.MirrorEdgePayload) error {
	return f.err
}

func (f failingPublicMirrorAdapter) DeleteEdge(ctx context.Context, ref memorycore.MirrorEdgeRef) error {
	return f.err
}

func requireMirrorQueueDoneForNode(t *testing.T, db *sql.DB, nodeType string, nodeID string, operation string) {
	t.Helper()

	var status string
	if err := db.QueryRow(`
SELECT status
FROM index_sync_queue
WHERE node_type = ? AND node_id = ? AND operation = ?`, nodeType, nodeID, operation).Scan(&status); err != nil {
		t.Fatalf("query mirror queue row: %v", err)
	}
	if status != "done" {
		t.Fatalf("mirror queue status = %q, want done", status)
	}
}

func requireMirrorQueueCountForNode(t *testing.T, db *sql.DB, nodeType string, nodeID string, operation string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM index_sync_queue
WHERE node_type = ? AND node_id = ? AND operation = ?`, nodeType, nodeID, operation).Scan(&got); err != nil {
		t.Fatalf("count mirror queue rows: %v", err)
	}
	if got != want {
		t.Fatalf("mirror queue count for %s/%s/%s = %d, want %d", nodeType, nodeID, operation, got, want)
	}
}

func requireMirrorIndexForFact(t *testing.T, db *sql.DB, factID string, wantStatus string) {
	t.Helper()

	var triviumID int64
	var status string
	if err := db.QueryRow(`
SELECT trivium_node_id, index_status
FROM memory_index_map
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = ?`, factID).Scan(&triviumID, &status); err != nil {
		t.Fatalf("query mirror index: %v", err)
	}
	if triviumID <= 0 || status != wantStatus {
		t.Fatalf("mirror index = %d/%s, want positive/%s", triviumID, status, wantStatus)
	}
}

func requireMirrorIndexForFactStatusOnly(t *testing.T, db *sql.DB, factID string, wantStatus string) {
	t.Helper()

	var status string
	if err := db.QueryRow(`
SELECT index_status
FROM memory_index_map
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = ?`, factID).Scan(&status); err != nil {
		t.Fatalf("query mirror index status: %v", err)
	}
	if status != wantStatus {
		t.Fatalf("mirror index status = %q, want %q", status, wantStatus)
	}
}

func enqueueMirrorDeleteForFact(t *testing.T, db *sql.DB, factID string, queueID string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation, priority)
VALUES (?, 'default', 'fact', ?, 'delete_node', 0)`, queueID, factID); err != nil {
		t.Fatalf("enqueue mirror delete: %v", err)
	}
}

func requireMirrorIndexMapCount(t *testing.T, db *sql.DB, factID string, want int) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_index_map
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = ?`, factID).Scan(&count); err != nil {
		t.Fatalf("count mirror index map: %v", err)
	}
	if count != want {
		t.Fatalf("mirror index map count = %d, want %d", count, want)
	}
}
