package sqlite_test

import (
	"context"
	"database/sql"
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

func fixedRetrievalID() string {
	return "retrieval_event_id"
}

func fixedRetrievalNow() time.Time {
	return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
}

func contains(value string, needle string) bool {
	return strings.Contains(value, needle)
}
