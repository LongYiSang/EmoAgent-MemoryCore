package sqlite_test

import (
	"context"
	"database/sql"
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
