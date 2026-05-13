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
	if result.Claimed != 4 || result.Completed != 4 || result.Failed != 0 || result.Skipped != 1 {
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

func TestServiceRunMirrorSyncFailsThinDeleteEdgeQueueRow(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openMirrorService(t, ctx, memorycore.NewFakeMirrorAdapter())
	defer svc.Close()

	db := openSQLDB(t, dbPath)
	defer db.Close()
	enqueueThinDeleteEdge(t, db, "delete_edge_thin_01", "link_missing_payload")

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
WHERE id = 'delete_edge_thin_01'`).Scan(&status, &attempts, &errorMessage); err != nil {
		t.Fatalf("query queue row: %v", err)
	}
	if status != "failed" || attempts != 1 {
		t.Fatalf("queue status/attempts = %s/%d, want failed/1", status, attempts)
	}
	if strings.TrimSpace(errorMessage) == "" {
		t.Fatal("thin delete_edge row should keep an explicit failure message")
	}
}

func TestServiceRunMirrorSyncFailsUnsupportedLegacyRebuildPersonaRow(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openMirrorService(t, ctx, memorycore.NewFakeMirrorAdapter())
	defer svc.Close()

	db := openSQLDB(t, dbPath)
	defer db.Close()
	if _, err := db.Exec(`
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation, priority)
VALUES ('legacy_rebuild_persona_01', 'default', 'persona', 'default', 'rebuild_persona', 0)`); err != nil {
		t.Fatalf("insert legacy rebuild_persona row: %v", err)
	}

	result, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1})
	if err != nil {
		t.Fatalf("run mirror sync: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 0 || result.Failed != 1 {
		t.Fatalf("mirror sync result = %#v", result)
	}
	requireMirrorQueueRowStateByID(t, db, "legacy_rebuild_persona_01", "failed", 1, "mirror queue operation failed")
}

func TestServiceRunMirrorSyncBlocksWhenPersonaMirrorStateNotReady(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openMirrorService(t, ctx, memorycore.NewFakeMirrorAdapter())
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢黑咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	_ = consolidateLiteral(t, ctx, svc, userID, "likes", "黑咖啡", "用户喜欢黑咖啡。", episode.ID)

	db := openSQLDB(t, dbPath)
	defer db.Close()
	setMirrorPersonaStateForMirrorTest(t, db, "default", "rebuilding")

	_, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 10})
	if !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("run mirror sync err = %v, want ErrInvalidRequest", err)
	}
	requireMirrorQueueNoClaimedRows(t, db)
}

func TestServiceRunMirrorSyncRequiresExplicitAdapter(t *testing.T) {
	ctx := context.Background()
	svc, _ := openMirrorService(t, ctx, nil)
	defer svc.Close()

	if _, err := svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{Limit: 1}); !errors.Is(err, memorycore.ErrInvalidOptions) {
		t.Fatalf("RunMirrorSync err = %v, want ErrInvalidOptions", err)
	}
}

func TestServiceRebuildMirrorClearsNamespaceAndReindexesEligibleNodes(t *testing.T) {
	ctx := context.Background()
	adapter := &rebuildPublicMirrorAdapter{nodeMirrorID: 9001}
	svc, dbPath := openMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()

	result, err := svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{})
	if err != nil {
		t.Fatalf("rebuild mirror: %v", err)
	}
	if result.NodesUpserted != 2 || result.EdgesUpserted != 1 || result.Failed != 0 {
		t.Fatalf("rebuild result = %#v", result)
	}
	if len(adapter.cleared) != 1 || adapter.cleared[0] != "default" {
		t.Fatalf("cleared namespaces = %#v, want default", adapter.cleared)
	}
	requireMirrorPersonaState(t, db, "default", "ready")
	requireMirrorIndexForFact(t, db, fact.ID, "indexed")

	updateFactColumn(t, db, fact.ID, "visibility_status", memorycore.VisibilityPurged)
	result, err = svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{})
	if err != nil {
		t.Fatalf("rebuild after purge: %v", err)
	}
	if result.NodesUpserted != 1 || result.Failed != 0 {
		t.Fatalf("rebuild after purge result = %#v", result)
	}
	requireMirrorIndexForFactStatusOnly(t, db, fact.ID, "deleted")
}

func TestServiceRebuildMirrorRecordsFailedExistingNode(t *testing.T) {
	ctx := context.Background()
	adapter := &rebuildPublicMirrorAdapter{nodeMirrorID: 9001}
	svc, dbPath := openMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	if _, err := svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{}); err != nil {
		t.Fatalf("initial rebuild: %v", err)
	}

	adapter.failNodeID = fact.ID
	result, err := svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{})
	if err != nil {
		t.Fatalf("failing rebuild: %v", err)
	}
	if result.Failed != 1 {
		t.Fatalf("rebuild failed count = %d, want 1", result.Failed)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireMirrorPersonaState(t, db, "default", "degraded")
	requireMirrorIndexForFactStatusOnly(t, db, fact.ID, "failed")
}

func TestServiceRebuildMirrorMarksPersonaDegradedWhenClearFails(t *testing.T) {
	ctx := context.Background()
	adapter := &rebuildPublicMirrorAdapter{clearErr: errors.New("sidecar unavailable")}
	svc, dbPath := openMirrorService(t, ctx, adapter)
	defer svc.Close()

	if _, err := svc.StartSession(ctx, memorycore.StartSessionRequest{}); err != nil {
		t.Fatalf("start session: %v", err)
	}

	if _, err := svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{}); err == nil {
		t.Fatalf("rebuild err = nil, want clear namespace failure")
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireMirrorPersonaState(t, db, "default", "degraded")
}

func TestServiceRebuildMirrorRefusesWhenActiveProcessingRowsRemain(t *testing.T) {
	ctx := context.Background()
	adapter := &rebuildPublicMirrorAdapter{nodeMirrorID: 9001}
	svc, dbPath := openMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢绿茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	_ = consolidateLiteral(t, ctx, svc, userID, "likes", "绿茶", "用户喜欢绿茶。", episode.ID)

	db := openSQLDB(t, dbPath)
	defer db.Close()

	now := time.Now().UTC()
	staleUpdatedAt := now.Add(-20 * time.Minute).Format(time.RFC3339Nano)
	freshUpdatedAt := now.Add(-5 * time.Minute).Format(time.RFC3339Nano)
	if _, err := db.Exec(`
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation, priority, status, attempts, created_at, updated_at, error_message)
VALUES
('q_stale_processing', 'default', 'fact', 'fact_stale_processing', 'upsert_node', 0, 'processing', 2, ?, ?, 'worker interrupted'),
('q_fresh_processing', 'default', 'fact', 'fact_fresh_processing', 'upsert_node', 1, 'processing', 1, ?, ?, 'still leased')`,
		now.Add(-time.Hour).Format(time.RFC3339Nano), staleUpdatedAt,
		now.Add(-time.Hour).Format(time.RFC3339Nano), freshUpdatedAt,
	); err != nil {
		t.Fatalf("insert processing queue rows: %v", err)
	}

	_, err := svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{})
	if err == nil {
		t.Fatal("rebuild err = nil, want refusal when active processing rows remain")
	}
	if !strings.Contains(err.Error(), "processing") {
		t.Fatalf("rebuild err = %v, want explicit processing refusal", err)
	}
	if len(adapter.cleared) != 0 {
		t.Fatalf("clear namespace called unexpectedly: %#v", adapter.cleared)
	}

	requireMirrorQueueRowStateByID(t, db, "q_stale_processing", "failed", 3, "mirror queue lease expired")
	requireMirrorQueueRowStateByID(t, db, "q_fresh_processing", "processing", 1, "still leased")
	requireNoMirrorPersonaState(t, db, "default")
}

func TestServiceRebuildMirrorSupersedesPendingAndFailedRows(t *testing.T) {
	ctx := context.Background()
	adapter := &rebuildPublicMirrorAdapter{nodeMirrorID: 9001}
	svc, dbPath := openMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢茉莉花茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	_ = consolidateLiteral(t, ctx, svc, userID, "likes", "茉莉花茶", "用户喜欢茉莉花茶。", episode.ID)

	db := openSQLDB(t, dbPath)
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation, priority, status, attempts, created_at, updated_at, error_message)
VALUES
('q_pending_old', 'default', 'fact', 'fact_pending_old', 'upsert_node', 5, 'pending', 0, ?, ?, NULL),
('q_failed_old', 'default', 'fact', 'fact_failed_old', 'delete_node', 6, 'failed', 3, ?, ?, 'previous failure')`,
		now, now, now, now,
	); err != nil {
		t.Fatalf("insert pending/failed queue rows: %v", err)
	}

	result, err := svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{})
	if err != nil {
		t.Fatalf("rebuild mirror: %v", err)
	}
	if result.Failed != 0 {
		t.Fatalf("rebuild failed count = %d, want 0", result.Failed)
	}

	requireMirrorQueueRowStateByID(t, db, "q_pending_old", "done", 0, "superseded by mirror rebuild")
	requireMirrorQueueRowStateByID(t, db, "q_failed_old", "done", 3, "superseded by mirror rebuild")
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

type rebuildPublicMirrorAdapter struct {
	nodeMirrorID int64
	failNodeID   string
	clearErr     error
	cleared      []string
	nextOffset   int64
}

func (f *rebuildPublicMirrorAdapter) ClearNamespace(ctx context.Context, personaID string) error {
	f.cleared = append(f.cleared, personaID)
	return f.clearErr
}

func (f *rebuildPublicMirrorAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	if payload.SQLiteNodeID == f.failNodeID {
		return memorycore.MirrorNodeUpsertResult{}, errors.New("sidecar unavailable")
	}
	f.nextOffset++
	return memorycore.MirrorNodeUpsertResult{MirrorNodeID: f.nodeMirrorID + f.nextOffset}, nil
}

func (f *rebuildPublicMirrorAdapter) DeleteNode(ctx context.Context, ref memorycore.MirrorNodeRef) error {
	return nil
}

func (f *rebuildPublicMirrorAdapter) UpsertEdge(ctx context.Context, payload memorycore.MirrorEdgePayload) error {
	return nil
}

func (f *rebuildPublicMirrorAdapter) DeleteEdge(ctx context.Context, ref memorycore.MirrorEdgeRef) error {
	return nil
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

func requireMirrorPersonaState(t *testing.T, db *sql.DB, personaID string, wantState string) {
	t.Helper()

	var state string
	if err := db.QueryRow(`
SELECT state
FROM mirror_persona_state
WHERE persona_id = ?`, personaID).Scan(&state); err != nil {
		t.Fatalf("query mirror persona state: %v", err)
	}
	if state != wantState {
		t.Fatalf("mirror persona state = %q, want %q", state, wantState)
	}
}

func requireNoMirrorPersonaState(t *testing.T, db *sql.DB, personaID string) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM mirror_persona_state
WHERE persona_id = ?`, personaID).Scan(&count); err != nil {
		t.Fatalf("count mirror persona state: %v", err)
	}
	if count != 0 {
		t.Fatalf("mirror persona state row count = %d, want 0", count)
	}
}

func setMirrorPersonaStateForMirrorTest(t *testing.T, db *sql.DB, personaID string, state string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO mirror_persona_state (persona_id, state, reason, updated_at)
VALUES (?, ?, 'test state', CURRENT_TIMESTAMP)
ON CONFLICT(persona_id) DO UPDATE SET state = excluded.state, reason = excluded.reason, updated_at = excluded.updated_at`,
		personaID,
		state,
	); err != nil {
		t.Fatalf("set mirror persona state: %v", err)
	}
}

func requireMirrorQueueNoClaimedRows(t *testing.T, db *sql.DB) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM index_sync_queue
WHERE status <> 'pending'`).Scan(&count); err != nil {
		t.Fatalf("count claimed mirror queue rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("claimed mirror queue row count = %d, want 0", count)
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

func enqueueThinDeleteEdge(t *testing.T, db *sql.DB, queueID string, edgeID string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation, priority)
VALUES (?, 'default', 'memory_link', ?, 'delete_edge', 0)`, queueID, edgeID); err != nil {
		t.Fatalf("enqueue thin delete_edge row: %v", err)
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

func requireMirrorQueueRowStateByID(t *testing.T, db *sql.DB, queueID string, wantStatus string, wantAttempts int, wantErrorMessage string) {
	t.Helper()

	var status, errorMessage string
	var attempts int
	if err := db.QueryRow(`
SELECT status, attempts, COALESCE(error_message, '')
FROM index_sync_queue
WHERE id = ?`, queueID).Scan(&status, &attempts, &errorMessage); err != nil {
		t.Fatalf("query queue row %s: %v", queueID, err)
	}
	if status != wantStatus || attempts != wantAttempts || errorMessage != wantErrorMessage {
		t.Fatalf("queue row %s = (%s,%d,%q), want (%s,%d,%q)", queueID, status, attempts, errorMessage, wantStatus, wantAttempts, wantErrorMessage)
	}
}
