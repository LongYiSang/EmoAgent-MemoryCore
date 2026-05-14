package sqlite_test

import (
	"context"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestMirrorCandidateRepositoryMapsOnlyIndexedFactCandidates(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedMirrorPayloadFixture(t, ctx, db.SQLDB())
	mustExecMirrorPayload(t, ctx, db.SQLDB(), `
INSERT INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES
  ('map_fact', 'default', 'fact', 'fact_visible', 1001, 'indexed'),
  ('map_deleted', 'default', 'fact', 'fact_deleted', 1002, 'deleted'),
  ('map_entity', 'default', 'entity', 'ent_user', 1003, 'indexed')`)

	repo := memsqlite.NewMirrorCandidateRepository(db.SQLDB())
	mapped, err := repo.MapFactCandidates(ctx, "default", []memsqlite.MirrorCandidate{
		{TriviumNodeID: 1001, Score: 0.8, Source: "fake_sparse"},
		{TriviumNodeID: 1001, Score: 1.7, Source: "high_score"},
		{TriviumNodeID: -1, Score: 0.99, Source: "bad_id"},
		{TriviumNodeID: 1002, Score: 0.9, Source: "stale"},
		{TriviumNodeID: 1003, Score: 0.7, Source: "entity"},
		{TriviumNodeID: 9999, Score: 1.0, Source: "missing"},
	})
	if err != nil {
		t.Fatalf("map mirror candidates: %v", err)
	}
	if len(mapped) != 1 {
		t.Fatalf("mapped candidates = %#v, want one fact", mapped)
	}
	if mapped[0].FactID != "fact_visible" || mapped[0].Score != 0.8 || mapped[0].Source != "fake_sparse" || mapped[0].Rank != 1 {
		t.Fatalf("mapped candidate = %#v", mapped[0])
	}
}

func TestMirrorCandidateRepositoryMapFactCandidatesWithDiagnostics(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedMirrorPayloadFixture(t, ctx, db.SQLDB())
	mustExecMirrorPayload(t, ctx, db.SQLDB(), `
INSERT INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES
  ('map_indexed', 'default', 'fact', 'fact_visible', 1001, 'indexed'),
  ('map_deleted', 'default', 'fact', 'fact_deleted', 1002, 'deleted')`)

	repo := memsqlite.NewMirrorCandidateRepository(db.SQLDB())
	report, err := repo.MapFactCandidatesWithDiagnostics(ctx, "default", []memsqlite.MirrorCandidate{
		{TriviumNodeID: 1001, Score: 0.8, Source: "mirror_sparse"},
		{TriviumNodeID: 1002, Score: 0.9, Source: "mirror_stale"},
		{TriviumNodeID: 9999, Score: 0.5, Source: "mirror_missing"},
	})
	if err != nil {
		t.Fatalf("MapFactCandidatesWithDiagnostics: %v", err)
	}
	if len(report.Mapped) != 1 || report.Mapped[0].FactID != "fact_visible" {
		t.Fatalf("mapped = %#v", report.Mapped)
	}
	if report.SidecarCandidateCount != 3 || report.MappedCandidateCount != 1 || report.DroppedCandidateCount != 2 {
		t.Fatalf("counts = %#v", report)
	}
	var deletedSeen, unmappedSeen bool
	for _, d := range report.Diagnostics {
		if d.TriviumNodeID == 1002 && d.DropReason == "stale_mapping_status_deleted" {
			deletedSeen = true
		}
		if d.TriviumNodeID == 9999 && d.DropReason == "unmapped_trivium_node" {
			unmappedSeen = true
		}
	}
	if !deletedSeen || !unmappedSeen {
		t.Fatalf("diagnostics = %#v", report.Diagnostics)
	}
}

func TestMirrorCandidateRepositoryDedupesMappedFactByRank(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedMirrorPayloadFixture(t, ctx, db.SQLDB())
	mustExecMirrorPayload(t, ctx, db.SQLDB(), `
INSERT INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES
  ('map_first', 'default', 'fact', 'fact_visible', 2001, 'indexed')`)

	repo := memsqlite.NewMirrorCandidateRepository(db.SQLDB())
	mapped, err := repo.MapFactCandidates(ctx, "default", []memsqlite.MirrorCandidate{
		{TriviumNodeID: 2001, Score: 0.2, Source: "rank_one", Rank: 1},
		{TriviumNodeID: 2001, Score: 1.0, Source: "rank_two_high_score", Rank: 2},
	})
	if err != nil {
		t.Fatalf("map mirror candidates: %v", err)
	}
	if len(mapped) != 1 {
		t.Fatalf("mapped candidates = %#v, want one fact", mapped)
	}
	if mapped[0].FactID != "fact_visible" || mapped[0].TriviumNodeID != 2001 || mapped[0].Rank != 1 || mapped[0].Source != "rank_one" {
		t.Fatalf("mapped candidate = %#v, want first ranked hit", mapped[0])
	}
}
