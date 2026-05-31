package memorycore_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceRunCurationApplyMergesEquivalentDrinkPreferences(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openCurationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	newEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝不甜的没有糖的饮料。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢喝无糖饮料。", oldEpisode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "likes", "不甜的没有糖的饮料", "用户喜欢喝不甜的没有糖的饮料。", newEpisode.ID).Fact

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		ProviderKind:           memorycore.ExtractionProviderMock,
		ProviderID:             "mock",
		Model:                  "memory-curator",
		MinAutoApplyConfidence: 0.88,
		UpdateCheckpoint:       true,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation: %v", err)
	}
	if result.Status != "succeeded" || result.AppliedGroupCount != 1 || result.GroupCount != 1 {
		t.Fatalf("curation result = %#v", result)
	}
	if len(result.Groups) != 1 || result.Groups[0].CanonicalFactID == "" {
		t.Fatalf("curation groups = %#v", result.Groups)
	}
	canonicalID := result.Groups[0].CanonicalFactID
	sourceID := first.ID
	if canonicalID == first.ID {
		sourceID = second.ID
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireCurationAPIFactState(t, db, canonicalID, "active", 1)
	requireCurationAPIFactState(t, db, sourceID, "consolidated", 0)
	requireCurationAPIFactSummary(t, db, canonicalID, "用户在饮料上偏好无糖、口味不甜。")
	requireCurationAPISearchDocument(t, db, canonicalID, true)
	requireCurationAPISearchDocument(t, db, sourceID, false)
	requireCurationAPILink(t, db, canonicalID, "EVIDENCED_BY", oldEpisode.ID)
	requireCurationAPILink(t, db, canonicalID, "EVIDENCED_BY", newEpisode.ID)
	requireCurationAPILink(t, db, canonicalID, "DERIVED_FROM", sourceID)
	requireCurationAPIEvidenceOrder(t, db, canonicalID, []string{newEpisode.ID, oldEpisode.ID})
	requireCurationAPIQueue(t, db, "fact", canonicalID, "upsert_node")
	requireCurationAPIQueue(t, db, "fact", sourceID, "delete_node")
	requireCurationAPICheckpoint(t, db, "default", result.RunID)

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "无糖饮料",
		Policy: memorycore.RetrievalPolicy{
			UseFTS:           true,
			UseMirror:        false,
			FinalMemoryCount: 5,
		},
	})
	if err != nil {
		t.Fatalf("retrieve after curation: %v", err)
	}
	requireMemoryItem(t, contextResult, canonicalID, "用户在饮料上偏好无糖、口味不甜。", "")
	requireMemoryItemAbsent(t, contextResult, sourceID)
}

func TestServiceRunCurationDoesNotMergeUnrelatedSamePredicateSources(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openCurationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	noSugarEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	lowSweetEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝不甜的没有糖的饮料。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	coconutEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝椰子水。", time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC))
	grandmaEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢外婆做的家常菜。", time.Date(2026, 5, 31, 13, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢喝无糖饮料。", noSugarEpisode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "likes", "不甜的没有糖的饮料", "用户喜欢喝不甜的没有糖的饮料。", lowSweetEpisode.ID).Fact
	coconut := consolidateLiteral(t, ctx, svc, userID, "likes", "椰子水", "用户喜欢喝椰子水。", coconutEpisode.ID).Fact
	grandma := consolidateLiteral(t, ctx, svc, userID, "likes", "外婆做的家常菜", "用户喜欢外婆做的家常菜。", grandmaEpisode.ID).Fact

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		ProviderKind:           memorycore.ExtractionProviderMock,
		ProviderID:             "mock",
		MinAutoApplyConfidence: 0.88,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation unrelated same predicate: %v", err)
	}
	if result.AppliedGroupCount != 1 {
		t.Fatalf("applied group count = %d, want 1; result=%#v", result.AppliedGroupCount, result)
	}

	canonicalID := result.Groups[0].CanonicalFactID
	sugarSourceID := first.ID
	if canonicalID == first.ID {
		sugarSourceID = second.ID
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireCurationAPIFactState(t, db, canonicalID, "active", 1)
	requireCurationAPIFactState(t, db, sugarSourceID, "consolidated", 0)
	requireCurationAPIFactState(t, db, coconut.ID, "active", 1)
	requireCurationAPIFactState(t, db, grandma.ID, "active", 1)
}

func TestServiceRunCurationDoesNotAutoMergeComplementFacts(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openCurationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	likeEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	dislikeEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我讨厌代糖味。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	likeFact := consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢无糖饮料。", likeEpisode.ID).Fact
	dislikeFact := consolidateLiteral(t, ctx, svc, userID, "dislikes", "代糖味", "用户讨厌代糖味。", dislikeEpisode.ID).Fact

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		ProviderKind:           memorycore.ExtractionProviderMock,
		ProviderID:             "mock",
		MinAutoApplyConfidence: 0.88,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation complement: %v", err)
	}
	if result.AppliedGroupCount != 0 {
		t.Fatalf("applied complement groups = %d, want 0", result.AppliedGroupCount)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireCurationAPIFactState(t, db, likeFact.ID, "active", 1)
	requireCurationAPIFactState(t, db, dislikeFact.ID, "active", 1)
}

func TestServiceRunCurationPinnedSourceRequiresReview(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openCurationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	oldEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝无糖饮料。", time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC))
	newEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢喝不甜的没有糖的饮料。", time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "无糖饮料", "用户喜欢喝无糖饮料。", oldEpisode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "likes", "不甜的没有糖的饮料", "用户喜欢喝不甜的没有糖的饮料。", newEpisode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	if _, err := db.Exec(`UPDATE facts SET created_at = ? WHERE id = ?`, "2026-05-31T10:00:00Z", first.ID); err != nil {
		t.Fatalf("set canonical candidate created_at: %v", err)
	}
	if _, err := db.Exec(`UPDATE facts SET pinned = 1, pin_actor = 'user', pin_reason = 'manual', created_at = ? WHERE id = ?`, "2026-05-31T11:00:00Z", second.ID); err != nil {
		t.Fatalf("pin source fact: %v", err)
	}

	result, err := svc.RunCuration(ctx, memorycore.RunCurationRequest{
		Mode:                   "apply",
		Trigger:                "test",
		ProviderKind:           memorycore.ExtractionProviderMock,
		ProviderID:             "mock",
		MinAutoApplyConfidence: 0.88,
		UpdateCheckpoint:       true,
		Force:                  true,
	})
	if err != nil {
		t.Fatalf("run curation pinned source: %v", err)
	}
	if result.AppliedGroupCount != 0 || result.ReviewGroupCount != 1 {
		t.Fatalf("pinned curation result = %#v", result)
	}
	if len(result.Groups) != 1 || result.Groups[0].GroupStatus != "needs_review" {
		t.Fatalf("pinned group result = %#v", result.Groups)
	}
	requireCurationAPIFactState(t, db, first.ID, "active", 1)
	requireCurationAPIFactState(t, db, second.ID, "active", 1)
	requireCurationAPINoCheckpoint(t, db, "default")
}

func openCurationService(t *testing.T, ctx context.Context) (memorycore.Service, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		EnableFTS:   true,
		Now: func() time.Time {
			return time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("open curation service: %v", err)
	}
	return svc, dbPath
}

func requireMemoryItemAbsent(t *testing.T, contextResult *memorycore.MemoryContext, nodeID string) {
	t.Helper()
	for _, block := range contextResult.Blocks {
		for _, item := range block.Items {
			if item.NodeID == nodeID {
				t.Fatalf("memory item %s unexpectedly present in %#v", nodeID, contextResult.Blocks)
			}
		}
	}
}

func requireCurationAPIFactState(t *testing.T, db *sql.DB, factID string, wantLifecycle string, wantSearchable int) {
	t.Helper()
	var lifecycle string
	var searchable int
	if err := db.QueryRow(`SELECT lifecycle_status, searchable FROM facts WHERE id = ?`, factID).Scan(&lifecycle, &searchable); err != nil {
		t.Fatalf("query fact state: %v", err)
	}
	if lifecycle != wantLifecycle || searchable != wantSearchable {
		t.Fatalf("fact %s state = %s/%d, want %s/%d", factID, lifecycle, searchable, wantLifecycle, wantSearchable)
	}
}

func requireCurationAPIFactSummary(t *testing.T, db *sql.DB, factID string, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT content_summary FROM facts WHERE id = ?`, factID).Scan(&got); err != nil {
		t.Fatalf("query fact summary: %v", err)
	}
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func requireCurationAPISearchDocument(t *testing.T, db *sql.DB, factID string, wantPresent bool) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_search_documents WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&count); err != nil {
		t.Fatalf("count search document: %v", err)
	}
	if (count > 0) != wantPresent {
		t.Fatalf("search document %s present = %t, want %t", factID, count > 0, wantPresent)
	}
}

func requireCurationAPILink(t *testing.T, db *sql.DB, fromID string, linkType string, toID string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_links WHERE from_node_id = ? AND link_type = ? AND to_node_id = ?`, fromID, linkType, toID).Scan(&count); err != nil {
		t.Fatalf("count curation link: %v", err)
	}
	if count != 1 {
		t.Fatalf("link %s -%s-> %s count = %d, want 1", fromID, linkType, toID, count)
	}
}

func requireCurationAPIEvidenceOrder(t *testing.T, db *sql.DB, factID string, want []string) {
	t.Helper()
	rows, err := db.Query(`
SELECT e.id
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.from_node_type = 'fact'
  AND l.from_node_id = ?
  AND l.link_type = 'EVIDENCED_BY'
  AND e.visibility_status = 'visible'
ORDER BY e.occurred_at DESC`, factID)
	if err != nil {
		t.Fatalf("query evidence order: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan evidence order: %v", err)
		}
		got = append(got, id)
	}
	if len(got) != len(want) {
		t.Fatalf("evidence order = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("evidence order = %#v, want %#v", got, want)
		}
	}
}

func requireCurationAPIQueue(t *testing.T, db *sql.DB, nodeType string, nodeID string, operation string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM index_sync_queue WHERE node_type = ? AND node_id = ? AND operation = ?`, nodeType, nodeID, operation).Scan(&count); err != nil {
		t.Fatalf("count queue: %v", err)
	}
	if count == 0 {
		t.Fatalf("queue row %s/%s/%s missing", nodeType, nodeID, operation)
	}
}

func requireCurationAPICheckpoint(t *testing.T, db *sql.DB, personaID string, runID string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT last_successful_run_id FROM memory_curation_checkpoints WHERE persona_id = ?`, personaID).Scan(&got); err != nil {
		t.Fatalf("query curation checkpoint: %v", err)
	}
	if got != runID {
		t.Fatalf("checkpoint run id = %q, want %q", got, runID)
	}
}

func requireCurationAPINoCheckpoint(t *testing.T, db *sql.DB, personaID string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_curation_checkpoints WHERE persona_id = ?`, personaID).Scan(&count); err != nil {
		t.Fatalf("count curation checkpoint: %v", err)
	}
	if count != 0 {
		t.Fatalf("checkpoint count = %d, want 0", count)
	}
}
