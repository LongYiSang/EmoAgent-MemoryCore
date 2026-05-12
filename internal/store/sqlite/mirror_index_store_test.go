package sqlite_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestMirrorIndexRepositoryRecordsNodeIndexed(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	repo := memsqlite.NewMirrorIndexRepository(db.SQLDB(), func() string { return "map_fact_visible" })
	if err := repo.RecordNodeIndexed(ctx, memsqlite.MirrorIndexedNode{
		PersonaID:     "default",
		NodeType:      string(core.NodeTypeFact),
		NodeID:        "fact_visible",
		TriviumNodeID: 1001,
	}); err != nil {
		t.Fatalf("record node indexed: %v", err)
	}
	requireMirrorIndexMap(t, db.SQLDB(), "fact_visible", 1001, "indexed")

	if err := repo.RecordNodeIndexed(ctx, memsqlite.MirrorIndexedNode{
		PersonaID:     "default",
		NodeType:      string(core.NodeTypeFact),
		NodeID:        "fact_visible",
		TriviumNodeID: 1002,
	}); err != nil {
		t.Fatalf("record node reindexed: %v", err)
	}
	requireMirrorIndexMap(t, db.SQLDB(), "fact_visible", 1002, "indexed")
}

func TestMirrorIndexRepositoryMarkNodeDeletedIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	repo := memsqlite.NewMirrorIndexRepository(db.SQLDB(), func() string { return "map_fact_visible" })
	if err := repo.MarkNodeDeleted(ctx, "default", string(core.NodeTypeFact), "fact_absent"); err != nil {
		t.Fatalf("mark absent node deleted: %v", err)
	}

	if err := repo.RecordNodeIndexed(ctx, memsqlite.MirrorIndexedNode{
		PersonaID:     "default",
		NodeType:      string(core.NodeTypeFact),
		NodeID:        "fact_visible",
		TriviumNodeID: 1001,
	}); err != nil {
		t.Fatalf("record node indexed: %v", err)
	}
	if err := repo.MarkNodeDeleted(ctx, "default", string(core.NodeTypeFact), "fact_visible"); err != nil {
		t.Fatalf("mark node deleted: %v", err)
	}
	requireMirrorIndexMap(t, db.SQLDB(), "fact_visible", 1001, "deleted")
}

func TestMirrorIndexRepositoryMarkNodeFailedCreatesSanitizedRow(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	repo := memsqlite.NewMirrorIndexRepository(db.SQLDB(), func() string { return "map_fact_failed" })
	err := repo.MarkNodeFailed(ctx, "default", string(core.NodeTypeFact), "fact_failed", "sidecar status 503: 用户喜欢咖啡 api_key=secret C:\\secret\\path")
	if err != nil {
		t.Fatalf("mark node failed: %v", err)
	}

	var status, message string
	var triviumID int64
	if err := db.SQLDB().QueryRow(`
SELECT trivium_node_id, index_status, COALESCE(error_message, '')
FROM memory_index_map
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = 'fact_failed'`).Scan(&triviumID, &status, &message); err != nil {
		t.Fatalf("query failed map row: %v", err)
	}
	if triviumID >= 0 || status != "failed" {
		t.Fatalf("failed map row = %d/%s, want negative/failed", triviumID, status)
	}
	for _, forbidden := range []string{"用户喜欢咖啡", "api_key", "secret", "C:\\"} {
		if strings.Contains(message, forbidden) {
			t.Fatalf("sanitized message %q still contains %q", message, forbidden)
		}
	}
	if !strings.Contains(message, "sidecar unavailable") || !strings.Contains(message, "status 503") {
		t.Fatalf("sanitized message = %q, want sidecar/status categories", message)
	}
}

func TestMirrorIndexRepositoryRecordNodeIndexedReusesDeletedTriviumID(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	repo := memsqlite.NewMirrorIndexRepository(db.SQLDB(), func() string { return "map_fact_old" })
	if err := repo.RecordNodeIndexed(ctx, memsqlite.MirrorIndexedNode{
		PersonaID:     "default",
		NodeType:      string(core.NodeTypeFact),
		NodeID:        "fact_old",
		TriviumNodeID: 1001,
	}); err != nil {
		t.Fatalf("record old node indexed: %v", err)
	}
	if err := repo.MarkPersonaDeleted(ctx, "default"); err != nil {
		t.Fatalf("mark persona deleted: %v", err)
	}

	repo = memsqlite.NewMirrorIndexRepository(db.SQLDB(), func() string { return "map_fact_new" })
	if err := repo.RecordNodeIndexed(ctx, memsqlite.MirrorIndexedNode{
		PersonaID:     "default",
		NodeType:      string(core.NodeTypeFact),
		NodeID:        "fact_new",
		TriviumNodeID: 1001,
	}); err != nil {
		t.Fatalf("record new node with reused trivium id: %v", err)
	}
	requireMirrorIndexMap(t, db.SQLDB(), "fact_new", 1001, "indexed")
	var oldCount int
	if err := db.SQLDB().QueryRow(`
SELECT COUNT(*)
FROM memory_index_map
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = 'fact_old'`).Scan(&oldCount); err != nil {
		t.Fatalf("query old map count: %v", err)
	}
	if oldCount != 0 {
		t.Fatalf("old deleted map count = %d, want 0", oldCount)
	}
}

func requireMirrorIndexMap(t *testing.T, db *sql.DB, nodeID string, wantTriviumID int64, wantStatus string) {
	t.Helper()

	var triviumID int64
	var status string
	if err := db.QueryRow(`
SELECT trivium_node_id, index_status
FROM memory_index_map
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = ?`, nodeID).Scan(&triviumID, &status); err != nil {
		t.Fatalf("query mirror index map: %v", err)
	}
	if triviumID != wantTriviumID || status != wantStatus {
		t.Fatalf("index map = %d/%s, want %d/%s", triviumID, status, wantTriviumID, wantStatus)
	}
}
