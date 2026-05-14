package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
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

func TestRetrievalRepositoryQueryAnalysisUsesEntityMentionsForCandidates(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	if _, err := memsqlite.NewEntityRepository(db.SQLDB()).EnsureAlias(ctx, core.EntityAlias{
		ID:         "alias_longyi",
		PersonaID:  "default",
		EntityID:   "ent_user",
		Alias:      "LongYi",
		AliasType:  core.AliasTypeNickname,
		Confidence: 0.9,
	}); err != nil {
		t.Fatalf("ensure alias: %v", err)
	}
	insertSearchFact(t, ctx, db.SQLDB(), "fact_alias_only", "用户喜欢不在查询中的乌龙茶。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_alias_only", "fact_alias_only")

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalID, fixedRetrievalNow)
	result, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: "LongYi",
		Policy: memsqlite.RetrievalPolicy{
			UseFTS:           true,
			FinalMemoryCount: 5,
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if result.QueryAnalysis == nil {
		t.Fatalf("query analysis is nil")
	}
	if len(result.QueryAnalysis.EntityMentions) != 1 || result.QueryAnalysis.EntityMentions[0].EntityID != "ent_user" || result.QueryAnalysis.EntityMentions[0].MatchText != "LongYi" {
		t.Fatalf("entity_mentions = %#v, want LongYi alias for ent_user", result.QueryAnalysis.EntityMentions)
	}
	if len(result.Blocks) != 1 || len(result.Blocks[0].Items) != 1 || result.Blocks[0].Items[0].NodeID != "fact_alias_only" {
		t.Fatalf("retrieval result = %#v, want alias-only fact", result)
	}
}

func TestRetrievalRepositoryAnchorFusionUsesMirrorRankBeforeRawScore(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_rank_one", "用户提到 rank one mirror candidate。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_rank_one", "fact_rank_one")
	insertSearchFact(t, ctx, db.SQLDB(), "fact_rank_two", "用户提到 rank two mirror candidate。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_rank_two", "fact_rank_two")

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalIDs(), fixedRetrievalNow)
	result, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: "mirror-only",
		Policy: memsqlite.RetrievalPolicy{
			FinalMemoryCount: 2,
			UseMirror:        true,
		},
		Mirror: []memsqlite.RetrievalMirrorCandidate{
			{FactID: "fact_rank_one", TriviumNodeID: 7001, Score: 0.10, Source: "trivium_dense", Rank: 1},
			{FactID: "fact_rank_two", TriviumNodeID: 7002, Score: 0.99, Source: "trivium_dense", Rank: 2},
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if result.AnchorFusion == nil {
		t.Fatalf("anchor fusion diagnostics is nil")
	}
	requireAnchorSeed(t, result.AnchorFusion, core.NodeTypeFact, "fact_rank_one", "trivium_dense", 1)
	requireAnchorSeed(t, result.AnchorFusion, core.NodeTypeFact, "fact_rank_two", "trivium_dense", 2)
	if len(result.Blocks) != 1 || len(result.Blocks[0].Items) != 2 {
		t.Fatalf("retrieval result = %#v, want two mirror facts", result.Blocks)
	}
	if result.Blocks[0].Items[0].NodeID != "fact_rank_one" {
		t.Fatalf("first item = %s, want rank-one mirror candidate before higher raw score", result.Blocks[0].Items[0].NodeID)
	}
}

func TestRetrievalRepositoryNarrativeInsightAnchorsAreGatedDiagnosticsOnly(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_causal", "用户说早会导致焦虑。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_fact_causal", "fact_causal")
	insertSearchNarrative(t, ctx, db.SQLDB(), "narrative_causal", "工作压力有周期性模式。")
	insertSearchNarrative(t, ctx, db.SQLDB(), "narrative_unrelated", "咖啡豆库存需要每周盘点。")
	insertSearchInsight(t, ctx, db.SQLDB(), "insight_causal", "早会是压力触发点。")
	insertSearchInsight(t, ctx, db.SQLDB(), "insight_unrelated", "旅行计划适合提前订票。")

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalIDs(), fixedRetrievalNow)
	direct, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: "早会",
		Policy: memsqlite.RetrievalPolicy{
			UseFTS:           true,
			FinalMemoryCount: 5,
		},
	})
	if err != nil {
		t.Fatalf("direct retrieve: %v", err)
	}
	if direct.AnchorFusion != nil && hasAnchorSeed(direct.AnchorFusion, core.NodeTypeNarrative, "narrative_causal") {
		t.Fatalf("direct_fact anchor fusion included narrative seed: %#v", direct.AnchorFusion)
	}
	if direct.AnchorFusion != nil && hasAnchorSeed(direct.AnchorFusion, core.NodeTypeInsight, "insight_causal") {
		t.Fatalf("direct_fact anchor fusion included insight seed: %#v", direct.AnchorFusion)
	}

	causal, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: "为什么工作压力和早会让我焦虑",
		Policy: memsqlite.RetrievalPolicy{
			UseFTS:           true,
			FinalMemoryCount: 5,
		},
	})
	if err != nil {
		t.Fatalf("causal retrieve: %v", err)
	}
	if causal.AnchorFusion == nil {
		t.Fatalf("causal anchor fusion diagnostics is nil")
	}
	requireAnchorSeed(t, causal.AnchorFusion, core.NodeTypeNarrative, "narrative_causal", "narrative_insight", 2)
	requireAnchorSeed(t, causal.AnchorFusion, core.NodeTypeInsight, "insight_causal", "narrative_insight", 1)
	if hasAnchorSeed(causal.AnchorFusion, core.NodeTypeNarrative, "narrative_unrelated") {
		t.Fatalf("causal anchor fusion included unrelated narrative seed: %#v", causal.AnchorFusion)
	}
	if hasAnchorSeed(causal.AnchorFusion, core.NodeTypeInsight, "insight_unrelated") {
		t.Fatalf("causal anchor fusion included unrelated insight seed: %#v", causal.AnchorFusion)
	}
	for _, block := range causal.Blocks {
		for _, item := range block.Items {
			if item.NodeType == string(core.NodeTypeNarrative) || item.NodeType == string(core.NodeTypeInsight) {
				t.Fatalf("non-fact diagnostics seed entered facts block: %#v", item)
			}
		}
	}
}

func TestRetrievalRepositoryWritesScoreBreakdownWithAnchorAndGraphEnergy(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_ranked", "用户喜欢咖啡。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_ranked_evidence", "fact_ranked")

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalIDs(), fixedRetrievalNow)
	result, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: "coffee",
		Policy: memsqlite.RetrievalPolicy{
			FinalMemoryCount: 1,
			UseMirror:        true,
		},
		Mirror: []memsqlite.RetrievalMirrorCandidate{
			{FactID: "fact_ranked", TriviumNodeID: 7001, Score: 0.91, Source: "trivium_dense", Rank: 1},
		},
		GraphActivation: []memsqlite.RetrievalActivationCandidate{
			{FactID: "fact_ranked", TriviumNodeID: 7001, Score: 0.42, Source: "graph_activation", Rank: 1},
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(result.Blocks) != 1 || len(result.Blocks[0].Items) != 1 || result.Blocks[0].Items[0].NodeID != "fact_ranked" {
		t.Fatalf("retrieval result = %#v, want fact_ranked", result.Blocks)
	}

	breakdown := requireScoreBreakdown(t, db.SQLDB(), "fact_ranked", "retrieved")
	requireBreakdownNumber(t, breakdown, "anchor_energy", 1)
	requireBreakdownNumber(t, breakdown, "graph_energy", 0.42)
	for _, key := range []string{
		"importance",
		"recency",
		"fact_type_prior",
		"pinned",
		"evidence_strength",
		"lifecycle_multiplier",
		"fatigue_penalty",
		"sensitivity_penalty",
		"final_score",
	} {
		if _, ok := breakdown[key].(float64); !ok {
			t.Fatalf("score breakdown missing numeric key %q: %#v", key, breakdown)
		}
	}
}

func TestRetrievalRepositoryMMRSuppressesDuplicateWithoutDoNotMention(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_meeting_primary", "用户讨厌早会，因为早会让他焦虑。", core.LifecycleActive)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_meeting_duplicate", "用户讨厌早会，因为早会让他焦虑。", core.LifecycleActive)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_coffee_distinct", "用户喜欢咖啡，咖啡能帮助他恢复精力。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_mmr_primary", "fact_meeting_primary")
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_mmr_duplicate", "fact_meeting_duplicate")
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_mmr_distinct", "fact_coffee_distinct")

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalIDs(), fixedRetrievalNow)
	result, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: "mirror-only",
		Policy: memsqlite.RetrievalPolicy{
			FinalMemoryCount: 2,
			UseMirror:        true,
		},
		Mirror: []memsqlite.RetrievalMirrorCandidate{
			{FactID: "fact_meeting_primary", TriviumNodeID: 7101, Score: 0.99, Source: "trivium_dense", Rank: 1},
			{FactID: "fact_meeting_duplicate", TriviumNodeID: 7102, Score: 0.98, Source: "trivium_dense", Rank: 2},
			{FactID: "fact_coffee_distinct", TriviumNodeID: 7103, Score: 0.97, Source: "trivium_dense", Rank: 3},
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(result.DoNotMention) != 0 {
		t.Fatalf("do_not_mention = %#v, want empty for MMR duplicate suppression", result.DoNotMention)
	}
	if len(result.Blocks) != 1 || len(result.Blocks[0].Items) != 2 {
		t.Fatalf("retrieval result = %#v, want two selected facts", result.Blocks)
	}
	items := result.Blocks[0].Items
	if items[0].NodeID != "fact_meeting_primary" || items[1].NodeID != "fact_coffee_distinct" {
		t.Fatalf("selected items = [%s, %s], want primary meeting and distinct coffee", items[0].NodeID, items[1].NodeID)
	}
	requireAccessEventRow(t, db.SQLDB(), "fact_meeting_duplicate", "suppressed", -1)
	breakdown := requireScoreBreakdown(t, db.SQLDB(), "fact_meeting_duplicate", "suppressed")
	if got := breakdown["suppression_reason"]; got != "mmr_duplicate" {
		t.Fatalf("suppression_reason = %#v, want mmr_duplicate", got)
	}
}

func TestRetrievalRepositoryContextBudgetSkipsLongCandidate(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	longSummary := strings.Repeat("预算很长的候选内容", 16)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_long_budget", longSummary, core.LifecycleActive)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_short_budget", "用户喜欢咖啡。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_long_budget", "fact_long_budget")
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_short_budget", "fact_short_budget")

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalIDs(), fixedRetrievalNow)
	result, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
		PersonaID: "default",
		QueryText: "mirror-only",
		Policy: memsqlite.RetrievalPolicy{
			FinalMemoryCount:    1,
			ContextBudgetTokens: 20,
			UseMirror:           true,
		},
		Mirror: []memsqlite.RetrievalMirrorCandidate{
			{FactID: "fact_long_budget", TriviumNodeID: 7201, Score: 0.99, Source: "trivium_dense", Rank: 1},
			{FactID: "fact_short_budget", TriviumNodeID: 7202, Score: 0.98, Source: "trivium_dense", Rank: 2},
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(result.DoNotMention) != 0 {
		t.Fatalf("do_not_mention = %#v, want empty for context budget suppression", result.DoNotMention)
	}
	if len(result.Blocks) != 1 || len(result.Blocks[0].Items) != 1 || result.Blocks[0].Items[0].NodeID != "fact_short_budget" {
		t.Fatalf("retrieval result = %#v, want short budget fact selected after long candidate skipped", result.Blocks)
	}
	breakdown := requireScoreBreakdown(t, db.SQLDB(), "fact_long_budget", "suppressed")
	if got := breakdown["suppression_reason"]; got != "context_budget" {
		t.Fatalf("suppression_reason = %#v, want context_budget", got)
	}
}

func TestRetrievalRepositoryFatigueSuppressionWritesScoreBreakdown(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_fatigue_breakdown", "用户喜欢咖啡。", core.LifecycleActive)
	insertRetrievalEvidenceLink(t, ctx, db.SQLDB(), "link_fatigue_breakdown", "fact_fatigue_breakdown")
	if err := memsqlite.NewSearchRepository(db.SQLDB()).UpsertFactDocument(ctx, "default", "fact_fatigue_breakdown"); err != nil {
		t.Fatalf("upsert fact search document: %v", err)
	}

	retrieval := memsqlite.NewRetrievalRepository(db.SQLDB(), fixedRetrievalIDs(), fixedRetrievalNow)
	for i := 0; i < 2; i++ {
		if _, err := retrieval.Retrieve(ctx, memsqlite.RetrievalRequest{
			PersonaID: "default",
			SessionID: ptr("s1"),
			QueryText: "咖啡",
			Policy: memsqlite.RetrievalPolicy{
				UseFTS:           true,
				FinalMemoryCount: 1,
			},
		}); err != nil {
			t.Fatalf("retrieve %d: %v", i+1, err)
		}
	}
	breakdown := requireScoreBreakdown(t, db.SQLDB(), "fact_fatigue_breakdown", "suppressed")
	if got := breakdown["suppression_reason"]; got != "fatigue" {
		t.Fatalf("suppression_reason = %#v, want fatigue", got)
	}
	requireBreakdownNumber(t, breakdown, "fatigue_penalty", 0.6)
}

func TestSearchDocumentsForRetrievalRanksFTSByRelevanceBeforeRecency(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_weak_recent", "coffee note", core.LifecycleActive)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_strong_old", "coffee laptop laptop setup", core.LifecycleActive)
	search := memsqlite.NewSearchRepository(db.SQLDB())
	if err := search.UpsertFactDocument(ctx, "default", "fact_weak_recent"); err != nil {
		t.Fatalf("upsert weak fact search document: %v", err)
	}
	if err := search.UpsertFactDocument(ctx, "default", "fact_strong_old"); err != nil {
		t.Fatalf("upsert strong fact search document: %v", err)
	}
	setSearchDocumentUpdatedAt(t, db.SQLDB(), "fact_weak_recent", "2026-05-12T12:00:00Z")
	setSearchDocumentUpdatedAt(t, db.SQLDB(), "fact_strong_old", "2026-05-01T12:00:00Z")

	docs, err := search.SearchDocumentsForRetrieval(ctx, "default", "coffee laptop", true, 2, memsqlite.RetrievalPolicy{
		FinalMemoryCount: 2,
		UseFTS:           true,
	})
	if err != nil {
		t.Fatalf("search documents for retrieval: %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("docs = %#v, want two matches", docs)
	}
	if docs[0].NodeID != "fact_strong_old" {
		t.Fatalf("first doc = %s, want stronger text relevance before newer weak match", docs[0].NodeID)
	}
}

func TestSearchDocumentsForRetrievalSparseUsesTermMatchCountBeforeRecency(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	insertSearchFact(t, ctx, db.SQLDB(), "fact_sparse_weak_recent", "coffee note", core.LifecycleActive)
	insertSearchFact(t, ctx, db.SQLDB(), "fact_sparse_strong_old", "coffee laptop setup", core.LifecycleActive)
	search := memsqlite.NewSearchRepository(db.SQLDB())
	if err := search.UpsertFactDocument(ctx, "default", "fact_sparse_weak_recent"); err != nil {
		t.Fatalf("upsert weak fact search document: %v", err)
	}
	if err := search.UpsertFactDocument(ctx, "default", "fact_sparse_strong_old"); err != nil {
		t.Fatalf("upsert strong fact search document: %v", err)
	}
	setSearchDocumentUpdatedAt(t, db.SQLDB(), "fact_sparse_weak_recent", "2026-05-12T12:00:00Z")
	setSearchDocumentUpdatedAt(t, db.SQLDB(), "fact_sparse_strong_old", "2026-05-01T12:00:00Z")

	docs, err := search.SearchDocumentsForRetrieval(ctx, "default", "coffee laptop", false, 2, memsqlite.RetrievalPolicy{
		FinalMemoryCount: 2,
	})
	if err != nil {
		t.Fatalf("search documents for retrieval: %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("docs = %#v, want two term matches", docs)
	}
	if docs[0].NodeID != "fact_sparse_strong_old" {
		t.Fatalf("first doc = %s, want higher term match count before newer weak match", docs[0].NodeID)
	}
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
	query := `
SELECT COUNT(*)
FROM memory_access_events
WHERE node_type = 'fact' AND node_id = ? AND access_type = ?`
	args := []any{factID, accessType}
	if rank >= 0 {
		query += ` AND rank_position = ?`
		args = append(args, rank)
	} else {
		query += ` AND rank_position IS NULL`
	}
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count access events: %v", err)
	}
	if count != 1 {
		t.Fatalf("access event count = %d, want 1", count)
	}
}

func requireScoreBreakdown(t *testing.T, db *sql.DB, factID string, accessType string) map[string]any {
	t.Helper()

	var raw string
	if err := db.QueryRow(`
SELECT score_breakdown_json
FROM memory_access_events
WHERE node_type = 'fact' AND node_id = ? AND access_type = ?
ORDER BY created_at DESC, id DESC
LIMIT 1`, factID, accessType).Scan(&raw); err != nil {
		t.Fatalf("query score breakdown for %s/%s: %v", factID, accessType, err)
	}
	if strings.TrimSpace(raw) == "" {
		t.Fatalf("score breakdown for %s/%s is empty", factID, accessType)
	}
	var breakdown map[string]any
	if err := json.Unmarshal([]byte(raw), &breakdown); err != nil {
		t.Fatalf("decode score breakdown %q: %v", raw, err)
	}
	return breakdown
}

func requireBreakdownNumber(t *testing.T, breakdown map[string]any, key string, want float64) {
	t.Helper()

	got, ok := breakdown[key].(float64)
	if !ok {
		t.Fatalf("breakdown[%s] = %#v, want number", key, breakdown[key])
	}
	if got != want {
		t.Fatalf("breakdown[%s] = %v, want %v", key, got, want)
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

func insertSearchNarrative(t *testing.T, ctx context.Context, db *sql.DB, narrativeID string, summary string) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO narratives (
    id, persona_id, scope, scope_ref, summary, importance,
    visibility_status, sensitivity_level, lifecycle_status, searchable
) VALUES (?, 'default', 'topic', 'work', ?, 0.8, 'visible', 'normal', 'active', 1)`, narrativeID, summary); err != nil {
		t.Fatalf("insert narrative %s: %v", narrativeID, err)
	}
	if err := memsqlite.NewSearchRepository(db).UpsertNarrativeDocument(ctx, "default", narrativeID); err != nil {
		t.Fatalf("upsert narrative search document %s: %v", narrativeID, err)
	}
}

func insertSearchInsight(t *testing.T, ctx context.Context, db *sql.DB, insightID string, content string) {
	t.Helper()

	if _, err := db.ExecContext(ctx, `
INSERT INTO insights (
    id, persona_id, insight_type, content, confidence, importance,
    visibility_status, sensitivity_level, lifecycle_status, searchable
) VALUES (?, 'default', 'pattern', ?, 0.8, 0.8, 'visible', 'normal', 'active', 1)`, insightID, content); err != nil {
		t.Fatalf("insert insight %s: %v", insightID, err)
	}
	if err := memsqlite.NewSearchRepository(db).UpsertInsightDocument(ctx, "default", insightID); err != nil {
		t.Fatalf("upsert insight search document %s: %v", insightID, err)
	}
}

func requireAnchorSeed(t *testing.T, diagnostics *memsqlite.AnchorFusionDiagnostics, nodeType core.NodeType, nodeID string, source string, rank int) {
	t.Helper()

	for _, seed := range diagnostics.Seeds {
		if seed.NodeType != nodeType || seed.NodeID != nodeID {
			continue
		}
		if seed.FusedAnchorScore <= 0 || seed.SeedEnergy <= 0 {
			t.Fatalf("seed %#v has non-positive fused score or energy", seed)
		}
		for _, breakdown := range seed.SourceBreakdown {
			if breakdown.Source == source && breakdown.Rank == rank {
				return
			}
		}
		t.Fatalf("seed %#v missing source=%s rank=%d", seed, source, rank)
	}
	t.Fatalf("anchor seed %s/%s not found in %#v", nodeType, nodeID, diagnostics)
}

func hasAnchorSeed(diagnostics *memsqlite.AnchorFusionDiagnostics, nodeType core.NodeType, nodeID string) bool {
	for _, seed := range diagnostics.Seeds {
		if seed.NodeType == nodeType && seed.NodeID == nodeID {
			return true
		}
	}
	return false
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
