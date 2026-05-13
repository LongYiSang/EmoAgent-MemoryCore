package sqlite_test

import (
	"context"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestLinkRepositoryInsertEnqueuesUpsertEdgeForMirrorEligibleLink(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	links := memsqlite.NewLinkRepository(db.SQLDB())
	link := core.MemoryLink{
		ID:           "link_enqueue_01",
		PersonaID:    "default",
		FromNodeType: core.NodeTypeEntity,
		FromNodeID:   "ent_user",
		LinkType:     core.LinkTypeAboutEntity,
		ToNodeType:   core.NodeTypeEntity,
		ToNodeID:     "ent_shanghai",
		CreatedBy:    core.LinkCreatedBySystem,
		Searchable:   true,
	}
	if err := links.Insert(ctx, link); err != nil {
		t.Fatalf("insert link: %v", err)
	}
	requireQueueCount(t, db.SQLDB(), "memory_link", link.ID, "upsert_edge", 1)

	if err := links.Insert(ctx, core.MemoryLink{
		ID:           "link_enqueue_02",
		PersonaID:    "default",
		FromNodeType: core.NodeTypeEntity,
		FromNodeID:   "ent_user",
		LinkType:     core.LinkTypeAboutEntity,
		ToNodeType:   core.NodeTypeEntity,
		ToNodeID:     "ent_shanghai",
		CreatedBy:    core.LinkCreatedBySystem,
		Searchable:   true,
	}); err != nil {
		t.Fatalf("insert duplicate link: %v", err)
	}
	requireQueueCount(t, db.SQLDB(), "memory_link", link.ID, "upsert_edge", 1)
}

func TestLinkRepositoryInsertSkipsUpsertEdgeForNonMirrorEligibleLink(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	links := memsqlite.NewLinkRepository(db.SQLDB())
	link := core.MemoryLink{
		ID:               "link_not_mirror_01",
		PersonaID:        "default",
		FromNodeType:     core.NodeTypeEntity,
		FromNodeID:       "ent_user",
		LinkType:         core.LinkTypeAboutEntity,
		ToNodeType:       core.NodeTypeEntity,
		ToNodeID:         "ent_hangzhou",
		CreatedBy:        core.LinkCreatedBySystem,
		VisibilityStatus: core.VisibilityHidden,
		Searchable:       false,
	}
	if err := links.Insert(ctx, link); err != nil {
		t.Fatalf("insert hidden link: %v", err)
	}
	requireQueueCount(t, db.SQLDB(), "memory_link", link.ID, "upsert_edge", 0)
}
