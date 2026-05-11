package sqlite_test

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestConsolidationRepositoryUpsertsSearchDocumentAndFTS(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	repo := memsqlite.NewConsolidationRepository(db.SQLDB(), fixedConsolidationIDs(), fixedConsolidationNow)
	result, err := repo.ConsolidateCandidate(ctx, memsqlite.ConsolidateCandidateRequest{
		PersonaID: "default",
		Trigger:   "manual",
		Candidate: memsqlite.ManualFactCandidate{
			SubjectEntityID:  "ent_user",
			Predicate:        "likes",
			ObjectLiteral:    ptr("咖啡"),
			ContentSummary:   "用户喜欢咖啡。",
			SourceEpisodeIDs: []string{"ep_visible"},
			Confidence:       string(core.ExtractionConfidenceExplicit),
			Importance:       0.7,
		},
	})
	if err != nil {
		t.Fatalf("consolidate candidate: %v", err)
	}

	requireSearchDocument(t, db.SQLDB(), result.Fact.ID, "用户喜欢咖啡")
	requireFTSDocument(t, db.SQLDB(), result.Fact.ID, "用户喜欢咖啡")
}

func TestSearchRepositoryRebuildSearchDocumentsBackfillsFacts(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	facts := memsqlite.NewFactRepository(db.SQLDB())
	object := "咖啡"
	if err := facts.Insert(ctx, core.Fact{
		ID:                   "fact_pr3",
		PersonaID:            "default",
		SubjectEntityID:      ptr("ent_user"),
		Predicate:            "likes",
		ObjectLiteral:        &object,
		ContentSummary:       "用户喜欢咖啡。",
		FactType:             core.FactTypeStablePreference,
		ExtractionConfidence: core.ExtractionConfidenceExplicit,
		Importance:           0.7,
	}); err != nil {
		t.Fatalf("insert fact: %v", err)
	}

	search := memsqlite.NewSearchRepository(db.SQLDB())
	result, err := search.RebuildSearchDocuments(ctx, "default")
	if err != nil {
		t.Fatalf("rebuild search documents: %v", err)
	}
	if result.Upserted != 1 {
		t.Fatalf("rebuild upserted = %d, want 1", result.Upserted)
	}
	requireSearchDocument(t, db.SQLDB(), "fact_pr3", "用户喜欢咖啡")
}

func TestSearchRepositoryUpsertFactDocumentDeletesInvisibleOrUnsearchableFacts(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	search := memsqlite.NewSearchRepository(db.SQLDB())
	for _, tt := range []struct {
		name       string
		factID     string
		visibility core.VisibilityStatus
		searchable bool
	}{
		{name: "missing", factID: "fact_missing", visibility: core.VisibilityVisible, searchable: true},
		{name: "hidden", factID: "fact_hidden", visibility: core.VisibilityHidden, searchable: true},
		{name: "forgotten", factID: "fact_forgotten", visibility: core.VisibilityForgotten, searchable: true},
		{name: "purged", factID: "fact_purged", visibility: core.VisibilityPurged, searchable: true},
		{name: "unsearchable", factID: "fact_unsearchable", visibility: core.VisibilityVisible, searchable: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name != "missing" {
				insertSearchFact(t, ctx, db.SQLDB(), tt.factID, "用户喜欢"+tt.name+"。", core.LifecycleActive)
			}
			if err := search.UpsertDocument(ctx, core.SearchDocument{
				ID:               "search_" + tt.factID,
				PersonaID:        "default",
				NodeType:         core.NodeTypeFact,
				NodeID:           tt.factID,
				SearchText:       "stale private text " + tt.name,
				SearchTier:       core.SearchTierHot,
				VisibilityStatus: core.VisibilityVisible,
				SensitivityLevel: core.SensitivityNormal,
				LifecycleStatus:  core.LifecycleActive,
				Searchable:       true,
			}); err != nil {
				t.Fatalf("seed stale search document: %v", err)
			}
			if tt.name != "missing" {
				if _, err := db.SQLDB().ExecContext(ctx, `
UPDATE facts
SET visibility_status = ?, searchable = ?
WHERE id = ?`, string(tt.visibility), boolIntTest(tt.searchable), tt.factID); err != nil {
					t.Fatalf("update fact gate: %v", err)
				}
			}

			if err := search.UpsertFactDocument(ctx, "default", tt.factID); err != nil {
				t.Fatalf("upsert fact document: %v", err)
			}

			requireSearchRowCount(t, db.SQLDB(), core.NodeTypeFact, tt.factID, 0)
			requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, tt.factID, 0)
		})
	}
}

func TestSearchRepositoryUpsertDocumentDeletesInvisibleOrUnsearchableDocuments(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	search := memsqlite.NewSearchRepository(db.SQLDB())
	for _, tt := range []struct {
		name       string
		visibility core.VisibilityStatus
		searchable bool
	}{
		{name: "hidden", visibility: core.VisibilityHidden, searchable: true},
		{name: "forgotten", visibility: core.VisibilityForgotten, searchable: true},
		{name: "purged", visibility: core.VisibilityPurged, searchable: true},
		{name: "unsearchable", visibility: core.VisibilityVisible, searchable: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			factID := "fact_doc_" + tt.name
			if err := search.UpsertDocument(ctx, core.SearchDocument{
				ID:               "search_" + factID,
				PersonaID:        "default",
				NodeType:         core.NodeTypeFact,
				NodeID:           factID,
				SearchText:       "private stale text " + tt.name,
				SearchTier:       core.SearchTierHot,
				VisibilityStatus: core.VisibilityVisible,
				SensitivityLevel: core.SensitivityNormal,
				LifecycleStatus:  core.LifecycleActive,
				Searchable:       true,
			}); err != nil {
				t.Fatalf("seed visible search document: %v", err)
			}

			if err := search.UpsertDocument(ctx, core.SearchDocument{
				ID:               "search_" + factID,
				PersonaID:        "default",
				NodeType:         core.NodeTypeFact,
				NodeID:           factID,
				SearchText:       "private stale text " + tt.name,
				SearchTier:       core.SearchTierHot,
				VisibilityStatus: tt.visibility,
				SensitivityLevel: core.SensitivityNormal,
				LifecycleStatus:  core.LifecycleActive,
				Searchable:       tt.searchable,
			}); err != nil {
				t.Fatalf("upsert ineligible search document: %v", err)
			}

			requireSearchRowCount(t, db.SQLDB(), core.NodeTypeFact, factID, 0)
			requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, factID, 0)
		})
	}
}

func TestSearchRepositoryRebuildSearchDocumentsDropsStaleAndSkipsInvisibleFacts(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_visible", "用户喜欢咖啡。", core.LifecycleActive)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_hidden", "用户喜欢隐藏茶。", core.LifecycleActive)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_unsearchable", "用户喜欢隐藏果汁。", core.LifecycleActive)
	if _, err := db.SQLDB().ExecContext(ctx, `
UPDATE facts
SET visibility_status = 'hidden'
WHERE id = 'fact_hidden'`); err != nil {
		t.Fatalf("hide fact: %v", err)
	}
	if _, err := db.SQLDB().ExecContext(ctx, `
UPDATE facts
SET searchable = 0
WHERE id = 'fact_unsearchable'`); err != nil {
		t.Fatalf("mark fact unsearchable: %v", err)
	}

	search := memsqlite.NewSearchRepository(db.SQLDB())
	for _, doc := range []core.SearchDocument{
		{ID: "search_fact_visible", PersonaID: "default", NodeType: core.NodeTypeFact, NodeID: "fact_visible", SearchText: "old visible", VisibilityStatus: core.VisibilityVisible, Searchable: true},
		{ID: "search_fact_hidden", PersonaID: "default", NodeType: core.NodeTypeFact, NodeID: "fact_hidden", SearchText: "stale hidden tea", VisibilityStatus: core.VisibilityVisible, Searchable: true},
		{ID: "search_fact_unsearchable", PersonaID: "default", NodeType: core.NodeTypeFact, NodeID: "fact_unsearchable", SearchText: "stale hidden juice", VisibilityStatus: core.VisibilityVisible, Searchable: true},
		{ID: "search_fact_deleted", PersonaID: "default", NodeType: core.NodeTypeFact, NodeID: "fact_deleted", SearchText: "deleted stale fact", VisibilityStatus: core.VisibilityVisible, Searchable: true},
		{ID: "search_ep_visible", PersonaID: "default", NodeType: core.NodeTypeEpisode, NodeID: "ep_visible", SearchText: "episode search survives", VisibilityStatus: core.VisibilityVisible, Searchable: true},
	} {
		if err := search.UpsertDocument(ctx, doc); err != nil {
			t.Fatalf("seed search document %s: %v", doc.ID, err)
		}
	}

	result, err := search.RebuildSearchDocuments(ctx, "default")
	if err != nil {
		t.Fatalf("rebuild search documents: %v", err)
	}
	if result.Upserted != 1 {
		t.Fatalf("rebuild upserted = %d, want 1", result.Upserted)
	}

	requireSearchDocument(t, db.SQLDB(), "fact_visible", "用户喜欢咖啡")
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeFact, "fact_hidden", 0)
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeFact, "fact_unsearchable", 0)
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeFact, "fact_deleted", 0)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, "fact_hidden", 0)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, "fact_unsearchable", 0)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, "fact_deleted", 0)
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeEpisode, "ep_visible", 1)
}

func TestSearchRepositoryBuildsFactSearchTierFromLifecycle(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	search := memsqlite.NewSearchRepository(db.SQLDB())
	for _, tt := range []struct {
		lifecycle core.LifecycleStatus
		want      core.SearchTier
	}{
		{lifecycle: core.LifecycleActive, want: core.SearchTierHot},
		{lifecycle: core.LifecycleDormant, want: core.SearchTierWarm},
		{lifecycle: core.LifecycleConsolidated, want: core.SearchTierWarm},
		{lifecycle: core.LifecycleArchived, want: core.SearchTierCold},
		{lifecycle: core.LifecycleDeepArchived, want: core.SearchTierDeepCold},
	} {
		factID := "fact_" + string(tt.lifecycle)
		insertSearchFact(t, ctx, db.SQLDB(), factID, "用户生命周期 "+string(tt.lifecycle), tt.lifecycle)
		if err := search.UpsertFactDocument(ctx, "default", factID); err != nil {
			t.Fatalf("upsert fact document %s: %v", factID, err)
		}
		var got string
		if err := db.SQLDB().QueryRow(`
SELECT search_tier
FROM memory_search_documents
WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&got); err != nil {
			t.Fatalf("query search tier: %v", err)
		}
		if got != string(tt.want) {
			t.Fatalf("lifecycle %s search tier = %s, want %s", tt.lifecycle, got, tt.want)
		}
	}
}

func TestRetrievalLifecycleMultiplierRanksActiveBeforeArchived(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_a_archived", "用户喜欢咖啡。", core.LifecycleArchived)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_z_active", "用户喜欢咖啡。", core.LifecycleActive)
	if _, err := db.SQLDB().ExecContext(ctx, `
UPDATE facts
SET created_at = ?
WHERE id IN ('fact_a_archived', 'fact_z_active')`, fixedRetrievalNow().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("fix fact timestamps: %v", err)
	}
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_archived_evidence", "fact_a_archived")
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_active_evidence", "fact_z_active")

	search := memsqlite.NewSearchRepository(db.SQLDB())
	for _, factID := range []string{"fact_a_archived", "fact_z_active"} {
		if err := search.UpsertFactDocument(ctx, "default", factID); err != nil {
			t.Fatalf("upsert fact document %s: %v", factID, err)
		}
	}

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalIDs(), fixedRetrievalNow)
	result, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Policy: memsqlite.RetrievalPolicy{
			AllowHistorical:  true,
			FinalMemoryCount: 2,
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(result.Blocks) != 1 || len(result.Blocks[0].Items) != 2 {
		t.Fatalf("retrieval items = %#v, want both active and archived facts", result.Blocks)
	}
	items := result.Blocks[0].Items
	if items[0].NodeID != "fact_z_active" || items[1].NodeID != "fact_a_archived" {
		t.Fatalf("retrieval order = [%s, %s], want active before archived", items[0].NodeID, items[1].NodeID)
	}
}

func TestRetrievalFiltersExpiredSearchDocsBeforeCandidateLimit(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	const query = "咖啡"
	currentID := "fact_current_coffee"
	insertSearchFact(t, ctx, db.SQLDB(), currentID, "用户当前喜欢咖啡。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_current_coffee", currentID)

	search := memsqlite.NewSearchRepository(db.SQLDB())
	if err := search.UpsertFactDocument(ctx, "default", currentID); err != nil {
		t.Fatalf("upsert current fact document: %v", err)
	}
	setSearchDocumentUpdatedAt(t, db.SQLDB(), currentID, "2026-05-01T00:00:00Z")

	for i := 0; i < 33; i++ {
		factID := "fact_expired_coffee_" + strconv.Itoa(i)
		insertSearchFact(t, ctx, db.SQLDB(), factID, "用户过去喜欢咖啡。", core.LifecycleActive)
		insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_expired_coffee_"+strconv.Itoa(i), factID)
		setFactRetrievalGate(t, db.SQLDB(), factID, string(core.ValidityInvalidated), string(core.LifecycleArchived))
		if err := search.UpsertFactDocument(ctx, "default", factID); err != nil {
			t.Fatalf("upsert expired fact document %s: %v", factID, err)
		}
		setSearchDocumentUpdatedAt(t, db.SQLDB(), factID, "2026-06-01T00:00:00Z")
	}

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalIDs(), fixedRetrievalNow)
	result, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: query,
		Policy: memsqlite.RetrievalPolicy{
			UseFTS:           true,
			FinalMemoryCount: 8,
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(result.Blocks) != 1 || len(result.Blocks[0].Items) != 1 || result.Blocks[0].Items[0].NodeID != currentID {
		t.Fatalf("retrieval result = %#v, want only current fact %s", result, currentID)
	}
}

func TestRetrievalRepositoryFallsBackToLIKEAndLogsAccessEvents(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/memory.db"
	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.MigrateWithOptions(ctx, memsqlite.MigrateOptions{EnableFTS: false}); err != nil {
		t.Fatalf("migrate without fts: %v", err)
	}
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	search := memsqlite.NewSearchRepository(db.SQLDB())
	object := "咖啡"
	if err := memsqlite.NewFactRepository(db.SQLDB()).Insert(ctx, core.Fact{
		ID:                   "fact_like_coffee",
		PersonaID:            "default",
		SubjectEntityID:      ptr("ent_user"),
		Predicate:            "likes",
		ObjectLiteral:        &object,
		ContentSummary:       "用户喜欢咖啡。",
		FactType:             core.FactTypeStablePreference,
		ExtractionConfidence: core.ExtractionConfidenceExplicit,
		Importance:           0.8,
		Pinned:               true,
	}); err != nil {
		t.Fatalf("insert fact: %v", err)
	}
	if _, err := search.RebuildSearchDocuments(ctx, "default"); err != nil {
		t.Fatalf("rebuild search documents: %v", err)
	}

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalID, fixedRetrievalNow)
	result, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		SessionID: ptr("s1"),
		QueryText: "咖啡",
		Policy: memsqlite.RetrievalPolicy{
			UseFTS:           true,
			FinalMemoryCount: 5,
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(result.Blocks) != 1 || len(result.Blocks[0].Items) != 1 || result.Blocks[0].Items[0].NodeID != "fact_like_coffee" {
		t.Fatalf("retrieval result = %#v, want fact_like_coffee", result)
	}
	requireAccessEventRow(t, db.SQLDB(), "fact_like_coffee", "retrieved", 0)
}

func requireSearchDocument(t *testing.T, db *sql.DB, factID string, wantText string) {
	t.Helper()

	var searchText string
	if err := db.QueryRow(`
SELECT search_text
FROM memory_search_documents
WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&searchText); err != nil {
		t.Fatalf("query search document: %v", err)
	}
	if !contains(searchText, wantText) {
		t.Fatalf("search text = %q, want contains %q", searchText, wantText)
	}
}

func requireFTSDocument(t *testing.T, db *sql.DB, factID string, wantText string) {
	t.Helper()

	var searchText string
	if err := db.QueryRow(`
SELECT search_text
FROM memory_search_fts
WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&searchText); err != nil {
		t.Fatalf("query fts document: %v", err)
	}
	if !contains(searchText, wantText) {
		t.Fatalf("fts text = %q, want contains %q", searchText, wantText)
	}
}

func requireAccessEventRow(t *testing.T, db *sql.DB, factID string, accessType string, rank int) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_access_events
WHERE node_type = 'fact' AND node_id = ? AND access_type = ? AND rank_position = ?`, factID, accessType, rank).Scan(&count); err != nil {
		t.Fatalf("count access events: %v", err)
	}
	if count != 1 {
		t.Fatalf("access event count = %d, want 1", count)
	}
}

func insertSearchFact(t *testing.T, ctx context.Context, db *sql.DB, factID string, summary string, lifecycle core.LifecycleStatus) {
	t.Helper()

	object := summary
	if err := memsqlite.NewFactRepository(db).Insert(ctx, core.Fact{
		ID:                   factID,
		PersonaID:            "default",
		SubjectEntityID:      ptr("ent_user"),
		Predicate:            "likes",
		ObjectLiteral:        &object,
		ContentSummary:       summary,
		FactType:             core.FactTypeStablePreference,
		ExtractionConfidence: core.ExtractionConfidenceExplicit,
		Importance:           0.7,
		LifecycleStatus:      lifecycle,
	}); err != nil {
		t.Fatalf("insert fact %s: %v", factID, err)
	}
}

func setFactRetrievalGate(t *testing.T, db *sql.DB, factID string, validity string, lifecycle string) {
	t.Helper()

	if _, err := db.Exec(`
UPDATE facts
SET validity_status = ?, lifecycle_status = ?
WHERE id = ?`, validity, lifecycle, factID); err != nil {
		t.Fatalf("set fact retrieval gate: %v", err)
	}
}

func setSearchDocumentUpdatedAt(t *testing.T, db *sql.DB, factID string, updatedAt string) {
	t.Helper()

	if _, err := db.Exec(`
UPDATE memory_search_documents
SET updated_at = ?
WHERE node_type = 'fact' AND node_id = ?`, updatedAt, factID); err != nil {
		t.Fatalf("set search document updated_at: %v", err)
	}
}

func insertRetrievalEvidenceLink(t *testing.T, ctx context.Context, db *sql.DB, linkID string, factID string) {
	t.Helper()

	if err := memsqlite.NewLinkRepository(db).Insert(ctx, core.MemoryLink{
		ID:           linkID,
		PersonaID:    "default",
		FromNodeType: core.NodeTypeFact,
		FromNodeID:   factID,
		LinkType:     core.LinkTypeEvidencedBy,
		ToNodeType:   core.NodeTypeEpisode,
		ToNodeID:     "ep_visible",
	}); err != nil {
		t.Fatalf("insert evidence link %s: %v", linkID, err)
	}
}

func boolIntTest(value bool) int {
	if value {
		return 1
	}
	return 0
}

func fixedRetrievalID() string {
	return "retrieval_event_id"
}

func fixedRetrievalIDs() func() string {
	index := 0
	return func() string {
		index++
		return "retrieval_event_id_" + strconv.Itoa(index)
	}
}

func fixedRetrievalNow() time.Time {
	return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
}

func contains(value string, needle string) bool {
	return strings.Contains(value, needle)
}
