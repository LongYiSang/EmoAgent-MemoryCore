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

func TestCompressionRepositoryApplyWritesNarrativeInsightLinksAndSearchDocuments(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	first := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_one", "用户压力大时希望先被理解。")
	second := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_two", "用户压力大时不喜欢直接给建议。")
	now := fixedCompressionNow()

	repo := memsqlite.NewCompressionRepository(db.SQLDB(), fixedCompressionIDs(), func() time.Time { return now })
	result, err := repo.Apply(ctx, memsqlite.CompressionRequest{
		PersonaID:     "default",
		SourceFactIDs: []string{first.ID, second.ID},
		Narrative: &memsqlite.NarrativeDraft{
			ID:               "narrative_compression",
			Scope:            "topic",
			ScopeRef:         "stress_support",
			Summary:          "用户在压力场景中更需要先被情绪承接。",
			EmotionalTone:    "stressed",
			Importance:       0.72,
			SensitivityLevel: string(core.SensitivityNormal),
		},
		Insights: []memsqlite.InsightDraft{
			{
				ID:               "insight_compression",
				InsightType:      "coping_strategy",
				Content:          "压力场景中先共情，再提供建议。",
				Confidence:       0.82,
				Importance:       0.76,
				Valence:          0.1,
				Arousal:          0.3,
				SensitivityLevel: string(core.SensitivityNormal),
			},
		},
	})
	if err != nil {
		t.Fatalf("apply compression: %v", err)
	}
	if result.NarrativeID != "narrative_compression" {
		t.Fatalf("narrative id = %q", result.NarrativeID)
	}
	if len(result.InsightIDs) != 1 || result.InsightIDs[0] != "insight_compression" {
		t.Fatalf("insight ids = %#v", result.InsightIDs)
	}
	if result.SourceFactsConsolidated != 2 || len(result.DerivedLinkIDs) != 4 || result.SearchDocumentsSynced != 4 || result.MirrorUpdatesEnqueued != 0 || result.DryRun {
		t.Fatalf("compression result = %#v", result)
	}

	requireNarrativeRow(t, db.SQLDB(), "narrative_compression", "用户在压力场景中更需要先被情绪承接。")
	requireInsightRow(t, db.SQLDB(), "insight_compression", "压力场景中先共情，再提供建议。")
	for _, fact := range []core.Fact{first, second} {
		requireCompressionFactState(t, db.SQLDB(), fact.ID, string(core.LifecycleConsolidated), now)
		requireCompressionSearchDocument(t, db.SQLDB(), core.NodeTypeFact, fact.ID, string(core.LifecycleConsolidated), string(core.SearchTierWarm), fact.ContentSummary)
		requireDerivedLink(t, db.SQLDB(), core.NodeTypeNarrative, "narrative_compression", fact.ID)
		requireDerivedLink(t, db.SQLDB(), core.NodeTypeInsight, "insight_compression", fact.ID)
	}
	requireCompressionSearchDocument(t, db.SQLDB(), core.NodeTypeNarrative, "narrative_compression", string(core.LifecycleActive), string(core.SearchTierHot), "stress_support")
	requireCompressionSearchDocument(t, db.SQLDB(), core.NodeTypeInsight, "insight_compression", string(core.LifecycleActive), string(core.SearchTierHot), "coping_strategy")
}

func TestCompressionRepositoryDryRunReportsCountsWithoutMutation(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	first := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_dry_one", "用户压力大时希望先被理解。")
	second := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_dry_two", "用户压力大时不喜欢直接给建议。")
	now := fixedCompressionNow()

	repo := memsqlite.NewCompressionRepository(db.SQLDB(), fixedCompressionIDs(), func() time.Time { return now })
	result, err := repo.Apply(ctx, memsqlite.CompressionRequest{
		PersonaID:     "default",
		SourceFactIDs: []string{first.ID, second.ID},
		Narrative: &memsqlite.NarrativeDraft{
			Scope:            "topic",
			ScopeRef:         "stress_support",
			Summary:          "用户在压力场景中更需要先被情绪承接。",
			Importance:       0.72,
			SensitivityLevel: string(core.SensitivityNormal),
		},
		Insights: []memsqlite.InsightDraft{
			{
				InsightType:      "coping_strategy",
				Content:          "压力场景中先共情，再提供建议。",
				Confidence:       0.82,
				Importance:       0.76,
				SensitivityLevel: string(core.SensitivityNormal),
			},
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry-run compression: %v", err)
	}
	if result.NarrativeID == "" || len(result.InsightIDs) != 1 || result.InsightIDs[0] == "" {
		t.Fatalf("dry-run generated ids = %#v", result)
	}
	if result.SourceFactsConsolidated != 2 || len(result.DerivedLinkIDs) != 4 || result.SearchDocumentsSynced != 4 || result.MirrorUpdatesEnqueued != 0 || !result.DryRun {
		t.Fatalf("dry-run result = %#v", result)
	}

	requireTableRowCount(t, db.SQLDB(), "narratives", 0)
	requireTableRowCount(t, db.SQLDB(), "insights", 0)
	requireDerivedLinkCount(t, db.SQLDB(), "", 0)
	requireCompressionFactState(t, db.SQLDB(), first.ID, string(core.LifecycleActive), time.Time{})
	requireCompressionFactState(t, db.SQLDB(), second.ID, string(core.LifecycleActive), time.Time{})
	requireCompressionSearchDocument(t, db.SQLDB(), core.NodeTypeFact, first.ID, string(core.LifecycleActive), string(core.SearchTierHot), first.ContentSummary)
	requireCompressionSearchDocument(t, db.SQLDB(), core.NodeTypeFact, second.ID, string(core.LifecycleActive), string(core.SearchTierHot), second.ContentSummary)
	requireSearchDocumentAbsent(t, db.SQLDB(), core.NodeTypeNarrative, result.NarrativeID)
	requireSearchDocumentAbsent(t, db.SQLDB(), core.NodeTypeInsight, result.InsightIDs[0])
	requireQueueCount(t, db.SQLDB(), "fact", first.ID, "upsert_node", 0)
	requireQueueCount(t, db.SQLDB(), "fact", second.ID, "upsert_node", 0)
}

func TestCompressionRepositoryRejectsIneligibleSourcesWithoutMutation(t *testing.T) {
	tests := []struct {
		name             string
		update           string
		wantBadValidity  string
		wantBadLifecycle string
	}{
		{name: "hidden", update: "visibility_status = 'hidden'"},
		{name: "forgotten", update: "visibility_status = 'forgotten'"},
		{name: "purged", update: "visibility_status = 'purged'"},
		{name: "unsearchable", update: "searchable = 0"},
		{name: "invalidated", update: "validity_status = 'invalidated'", wantBadValidity: string(core.ValidityInvalidated)},
		{name: "archived", update: "lifecycle_status = 'archived'", wantBadLifecycle: string(core.LifecycleArchived)},
		{name: "pinned", update: "pinned = 1"},
		{name: "core_identity", update: "fact_type = 'core_identity'"},
		{name: "commitment", update: "fact_type = 'commitment'"},
		{name: "ambiguous", update: "extraction_confidence = 'ambiguous'"},
		{name: "highly_sensitive", update: "sensitivity_level = 'highly_sensitive'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openMigratedDB(t, ctx)
			defer db.Close()
			seedConsolidationStoreGraph(t, ctx, db.SQLDB())

			good := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_good_"+tt.name, "用户压力大时希望先被理解。")
			bad := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_bad_"+tt.name, "用户压力大时不喜欢直接给建议。")
			updateCompressionFact(t, db.SQLDB(), bad.ID, tt.update)

			repo := memsqlite.NewCompressionRepository(db.SQLDB(), fixedCompressionIDs(), fixedCompressionNow)
			_, err := repo.Apply(ctx, memsqlite.CompressionRequest{
				PersonaID:     "default",
				SourceFactIDs: []string{good.ID, bad.ID},
				Narrative: &memsqlite.NarrativeDraft{
					Scope:            "topic",
					Summary:          "用户在压力场景中更需要先被情绪承接。",
					Importance:       0.72,
					SensitivityLevel: string(core.SensitivityNormal),
				},
			})
			if err == nil {
				t.Fatal("apply compression err is nil, want rejection")
			}
			if !strings.Contains(err.Error(), bad.ID) {
				t.Fatalf("error = %v, want bad source id", err)
			}

			requireTableRowCount(t, db.SQLDB(), "narratives", 0)
			requireTableRowCount(t, db.SQLDB(), "insights", 0)
			requireDerivedLinkCount(t, db.SQLDB(), "", 0)
			requireCompressionFactFullState(t, db.SQLDB(), good.ID, string(core.ValidityValid), string(core.LifecycleActive), time.Time{})
			wantBadValidity := tt.wantBadValidity
			if wantBadValidity == "" {
				wantBadValidity = string(core.ValidityValid)
			}
			wantBadLifecycle := tt.wantBadLifecycle
			if wantBadLifecycle == "" {
				wantBadLifecycle = string(core.LifecycleActive)
			}
			requireCompressionFactFullState(t, db.SQLDB(), bad.ID, wantBadValidity, wantBadLifecycle, time.Time{})
			requireSearchDocumentAbsent(t, db.SQLDB(), core.NodeTypeNarrative, "narrative_rejected")
			requireQueueCount(t, db.SQLDB(), "fact", good.ID, "upsert_node", 0)
			requireQueueCount(t, db.SQLDB(), "fact", bad.ID, "upsert_node", 0)
		})
	}
}

func TestCompressionRepositoryNamespacesNarrativeAndInsightSearchDocumentIDs(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	sharedID := "shared_search_doc_id"
	first := insertCompressionFact(t, ctx, db.SQLDB(), sharedID, "source one")
	second := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_namespace_second", "source two")

	repo := memsqlite.NewCompressionRepository(db.SQLDB(), fixedCompressionIDs(), fixedCompressionNow)
	_, err := repo.Apply(ctx, memsqlite.CompressionRequest{
		PersonaID:     "default",
		SourceFactIDs: []string{first.ID, second.ID},
		Narrative: &memsqlite.NarrativeDraft{
			ID:               sharedID,
			Scope:            "topic",
			Summary:          "namespaced narrative summary",
			Importance:       0.72,
			SensitivityLevel: string(core.SensitivityNormal),
		},
		Insights: []memsqlite.InsightDraft{
			{
				ID:               sharedID,
				InsightType:      "pattern",
				Content:          "namespaced insight content",
				Confidence:       0.82,
				Importance:       0.76,
				SensitivityLevel: string(core.SensitivityNormal),
			},
		},
	})
	if err != nil {
		t.Fatalf("apply compression with shared node ids: %v", err)
	}

	requireSearchDocumentID(t, db.SQLDB(), core.NodeTypeFact, sharedID, "search_"+sharedID)
	requireSearchDocumentID(t, db.SQLDB(), core.NodeTypeNarrative, sharedID, "search_narrative_"+sharedID)
	requireSearchDocumentID(t, db.SQLDB(), core.NodeTypeInsight, sharedID, "search_insight_"+sharedID)
}

func TestCompressionRepositoryEnqueuesMappedSourceFactsOnly(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	mapped := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_mapped", "用户压力大时希望先被理解。")
	unmapped := insertCompressionFact(t, ctx, db.SQLDB(), "fact_compression_unmapped", "用户压力大时不喜欢直接给建议。")
	insertIndexMap(t, db.SQLDB(), core.NodeTypeFact, mapped.ID)

	repo := memsqlite.NewCompressionRepository(db.SQLDB(), fixedCompressionIDs(), fixedCompressionNow)
	result, err := repo.Apply(ctx, memsqlite.CompressionRequest{
		PersonaID:     "default",
		SourceFactIDs: []string{mapped.ID, unmapped.ID},
		Narrative: &memsqlite.NarrativeDraft{
			ID:               "narrative_mapped",
			Scope:            "topic",
			Summary:          "用户在压力场景中更需要先被情绪承接。",
			Importance:       0.72,
			SensitivityLevel: string(core.SensitivityNormal),
		},
	})
	if err != nil {
		t.Fatalf("apply compression: %v", err)
	}
	if result.MirrorUpdatesEnqueued != 1 {
		t.Fatalf("mirror updates enqueued = %d, want 1", result.MirrorUpdatesEnqueued)
	}
	requireQueueCount(t, db.SQLDB(), "fact", mapped.ID, "upsert_node", 1)
	requireQueueCount(t, db.SQLDB(), "fact", unmapped.ID, "upsert_node", 0)
	requireQueueCount(t, db.SQLDB(), "narrative", "narrative_mapped", "upsert_node", 0)
}

func insertCompressionFact(t *testing.T, ctx context.Context, db *sql.DB, factID string, summary string) core.Fact {
	t.Helper()

	object := factID
	fact := core.Fact{
		ID:                   factID,
		PersonaID:            "default",
		SubjectEntityID:      ptr("ent_user"),
		Predicate:            "likes",
		ObjectLiteral:        &object,
		ContentSummary:       summary,
		FactType:             core.FactTypeStablePreference,
		ExtractionConfidence: core.ExtractionConfidenceExplicit,
		Importance:           0.7,
		LifecycleStatus:      core.LifecycleActive,
		Searchable:           true,
	}
	if err := memsqlite.NewFactRepository(db).Insert(ctx, fact); err != nil {
		t.Fatalf("insert compression fact: %v", err)
	}
	if err := memsqlite.NewSearchRepository(db).UpsertFactDocument(ctx, "default", fact.ID); err != nil {
		t.Fatalf("upsert compression fact search document: %v", err)
	}
	return fact
}

func updateCompressionFact(t *testing.T, db *sql.DB, factID string, setClause string) {
	t.Helper()

	if _, err := db.Exec("UPDATE facts SET "+setClause+" WHERE id = ?", factID); err != nil {
		t.Fatalf("update compression fact %s: %v", factID, err)
	}
}

func requireNarrativeRow(t *testing.T, db *sql.DB, narrativeID string, wantSummary string) {
	t.Helper()

	var summary string
	if err := db.QueryRow(`SELECT summary FROM narratives WHERE id = ?`, narrativeID).Scan(&summary); err != nil {
		t.Fatalf("query narrative: %v", err)
	}
	if summary != wantSummary {
		t.Fatalf("narrative summary = %q, want %q", summary, wantSummary)
	}
}

func requireInsightRow(t *testing.T, db *sql.DB, insightID string, wantContent string) {
	t.Helper()

	var content string
	if err := db.QueryRow(`SELECT content FROM insights WHERE id = ?`, insightID).Scan(&content); err != nil {
		t.Fatalf("query insight: %v", err)
	}
	if content != wantContent {
		t.Fatalf("insight content = %q, want %q", content, wantContent)
	}
}

func requireDerivedLink(t *testing.T, db *sql.DB, fromType core.NodeType, fromID string, sourceFactID string) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_links
WHERE from_node_type = ?
  AND from_node_id = ?
  AND link_type = 'DERIVED_FROM'
  AND to_node_type = 'fact'
  AND to_node_id = ?
  AND created_by = 'consolidation'
  AND visibility_status = 'visible'
  AND searchable = 1`, string(fromType), fromID, sourceFactID).Scan(&count); err != nil {
		t.Fatalf("count derived links: %v", err)
	}
	if count != 1 {
		t.Fatalf("derived link %s/%s -> %s count = %d, want 1", fromType, fromID, sourceFactID, count)
	}
}

func requireDerivedLinkCount(t *testing.T, db *sql.DB, fromNodeID string, want int) {
	t.Helper()

	query := `SELECT COUNT(*) FROM memory_links WHERE link_type = 'DERIVED_FROM'`
	args := []any{}
	if fromNodeID != "" {
		query += ` AND from_node_id = ?`
		args = append(args, fromNodeID)
	}
	var got int
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatalf("count derived links: %v", err)
	}
	if got != want {
		t.Fatalf("derived link count = %d, want %d", got, want)
	}
}

func requireCompressionFactState(t *testing.T, db *sql.DB, factID string, wantLifecycle string, wantUpdatedAt time.Time) {
	t.Helper()

	var lifecycle string
	var updatedAt sql.NullString
	if err := db.QueryRow(`SELECT lifecycle_status, updated_at FROM facts WHERE id = ?`, factID).Scan(&lifecycle, &updatedAt); err != nil {
		t.Fatalf("query compression fact state: %v", err)
	}
	if lifecycle != wantLifecycle {
		t.Fatalf("fact %s lifecycle = %q, want %q", factID, lifecycle, wantLifecycle)
	}
	if wantUpdatedAt.IsZero() {
		if updatedAt.Valid {
			t.Fatalf("fact %s updated_at = %q, want null", factID, updatedAt.String)
		}
		return
	}
	want := wantUpdatedAt.UTC().Format(time.RFC3339Nano)
	if !updatedAt.Valid || updatedAt.String != want {
		t.Fatalf("fact %s updated_at = %v, want %s", factID, updatedAt, want)
	}
}

func requireCompressionFactFullState(t *testing.T, db *sql.DB, factID string, wantValidity string, wantLifecycle string, wantUpdatedAt time.Time) {
	t.Helper()

	var validity, lifecycle string
	var updatedAt sql.NullString
	if err := db.QueryRow(`SELECT validity_status, lifecycle_status, updated_at FROM facts WHERE id = ?`, factID).Scan(&validity, &lifecycle, &updatedAt); err != nil {
		t.Fatalf("query compression fact full state: %v", err)
	}
	if validity != wantValidity || lifecycle != wantLifecycle {
		t.Fatalf("fact %s state = %s/%s, want %s/%s", factID, validity, lifecycle, wantValidity, wantLifecycle)
	}
	if wantUpdatedAt.IsZero() {
		if updatedAt.Valid {
			t.Fatalf("fact %s updated_at = %q, want null", factID, updatedAt.String)
		}
		return
	}
	want := wantUpdatedAt.UTC().Format(time.RFC3339Nano)
	if !updatedAt.Valid || updatedAt.String != want {
		t.Fatalf("fact %s updated_at = %v, want %s", factID, updatedAt, want)
	}
}

func requireCompressionSearchDocument(t *testing.T, db *sql.DB, nodeType core.NodeType, nodeID string, wantLifecycle string, wantTier string, wantTextPart string) {
	t.Helper()

	var lifecycle, tier, searchText string
	if err := db.QueryRow(`
SELECT lifecycle_status, search_tier, search_text
FROM memory_search_documents
WHERE node_type = ? AND node_id = ?`, string(nodeType), nodeID).Scan(&lifecycle, &tier, &searchText); err != nil {
		t.Fatalf("query search document %s/%s: %v", nodeType, nodeID, err)
	}
	if lifecycle != wantLifecycle || tier != wantTier {
		t.Fatalf("search document %s/%s lifecycle/tier = %s/%s, want %s/%s", nodeType, nodeID, lifecycle, tier, wantLifecycle, wantTier)
	}
	if !strings.Contains(searchText, wantTextPart) {
		t.Fatalf("search document %s/%s text = %q, want to contain %q", nodeType, nodeID, searchText, wantTextPart)
	}
}

func requireSearchDocumentID(t *testing.T, db *sql.DB, nodeType core.NodeType, nodeID string, wantID string) {
	t.Helper()

	var got string
	if err := db.QueryRow(`
SELECT id
FROM memory_search_documents
WHERE node_type = ? AND node_id = ?`, string(nodeType), nodeID).Scan(&got); err != nil {
		t.Fatalf("query search document id %s/%s: %v", nodeType, nodeID, err)
	}
	if got != wantID {
		t.Fatalf("search document %s/%s id = %q, want %q", nodeType, nodeID, got, wantID)
	}
}

func requireSearchDocumentAbsent(t *testing.T, db *sql.DB, nodeType core.NodeType, nodeID string) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_search_documents
WHERE node_type = ? AND node_id = ?`, string(nodeType), nodeID).Scan(&count); err != nil {
		t.Fatalf("count search document: %v", err)
	}
	if count != 0 {
		t.Fatalf("search document %s/%s count = %d, want 0", nodeType, nodeID, count)
	}
}

func requireTableRowCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s rows = %d, want %d", table, got, want)
	}
}

func fixedCompressionIDs() func() string {
	index := 0
	return func() string {
		index++
		return "compression_id_" + string(rune('0'+index))
	}
}

func fixedCompressionNow() time.Time {
	return time.Date(2026, 5, 12, 10, 30, 0, 0, time.UTC)
}
