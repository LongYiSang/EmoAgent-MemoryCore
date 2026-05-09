package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestForgetRepositorySoftForgetsFactAndWritesSafeAudit(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	fact := insertForgetFact(t, ctx, db.SQLDB(), "fact_soft", "咖啡", "用户喜欢咖啡。", true)
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeFact, fact.ID, 1)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, fact.ID, 1)
	insertIndexMap(t, db.SQLDB(), core.NodeTypeFact, fact.ID)

	repo := memsqlite.NewForgetRepository(db.SQLDB(), fixedForgetIDs(), fixedForgetNow)
	result, err := repo.Forget(ctx, memsqlite.ForgetRequest{
		PersonaID:  "default",
		Actor:      memsqlite.ForgetActorUser,
		ReasonCode: memsqlite.ForgetReasonUserRequested,
		Level:      memsqlite.ForgetLevelSoft,
		Target: memsqlite.ForgetTarget{
			ScopeMode: memsqlite.ForgetScopeExactNode,
			NodeType:  core.NodeTypeFact,
			NodeID:    fact.ID,
		},
	})
	if err != nil {
		t.Fatalf("soft forget: %v", err)
	}
	if result.DeletionEventID == "" {
		t.Fatal("deletion event id is empty")
	}
	if result.FTSRowsDeleted != 1 {
		t.Fatalf("reported fts rows deleted = %d, want 1", result.FTSRowsDeleted)
	}

	var visibility, summary string
	var searchable, pinned int
	var pinReason, pinActor, objectLiteral sql.NullString
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT visibility_status, searchable, pinned, pin_reason, pin_actor, content_summary, object_literal
FROM facts
WHERE id = ?`, fact.ID).Scan(&visibility, &searchable, &pinned, &pinReason, &pinActor, &summary, &objectLiteral); err != nil {
		t.Fatalf("query soft-forgotten fact: %v", err)
	}
	if visibility != string(core.VisibilityHidden) || searchable != 0 || pinned != 0 {
		t.Fatalf("soft-forgotten fact visibility/searchable/pinned = %s/%d/%d", visibility, searchable, pinned)
	}
	if pinReason.Valid || pinActor.Valid {
		t.Fatalf("pin metadata remains after soft forget: %v/%v", pinReason, pinActor)
	}
	if summary != "用户喜欢咖啡。" || !objectLiteral.Valid || objectLiteral.String != "咖啡" {
		t.Fatalf("soft forget changed semantic content: summary=%q object=%v", summary, objectLiteral)
	}
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeFact, fact.ID, 0)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, fact.ID, 0)
	requireQueueCount(t, db.SQLDB(), "fact", fact.ID, "delete_node", 1)
	requireSafeDeletionEvent(t, db.SQLDB(), result.DeletionEventID, memsqlite.ForgetLevelSoft, fact.ID, "咖啡", "用户喜欢")
}

func TestForgetRepositoryHardForgetsFactAndClearsSemanticResidue(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	fact := insertForgetFact(t, ctx, db.SQLDB(), "fact_hard", "杭州", "用户住在杭州。", true)
	if _, err := db.SQLDB().ExecContext(ctx, `
UPDATE facts
SET extraction_reasoning = ?
WHERE id = ?`, "secret reasoning: 用户住在杭州", fact.ID); err != nil {
		t.Fatalf("seed extraction reasoning: %v", err)
	}
	if _, err := db.SQLDB().ExecContext(ctx, `
UPDATE memory_links
SET reasoning = ?
WHERE from_node_type = 'fact' AND from_node_id = ?`, "secret link reasoning: 杭州", fact.ID); err != nil {
		t.Fatalf("seed link reasoning: %v", err)
	}
	insertIndexMap(t, db.SQLDB(), core.NodeTypeFact, fact.ID)

	repo := memsqlite.NewForgetRepository(db.SQLDB(), fixedForgetIDs(), fixedForgetNow)
	result, err := repo.Forget(ctx, memsqlite.ForgetRequest{
		PersonaID:  "default",
		Actor:      memsqlite.ForgetActorUser,
		ReasonCode: memsqlite.ForgetReasonUserRequested,
		Level:      memsqlite.ForgetLevelHard,
		Target: memsqlite.ForgetTarget{
			ScopeMode: memsqlite.ForgetScopeExactNode,
			NodeType:  core.NodeTypeFact,
			NodeID:    fact.ID,
		},
	})
	if err != nil {
		t.Fatalf("hard forget: %v", err)
	}
	if result.FTSRowsDeleted != 1 {
		t.Fatalf("reported fts rows deleted = %d, want 1", result.FTSRowsDeleted)
	}

	var visibility, predicate, summary string
	var searchable, pinned int
	var subjectID, objectID, objectLiteral, reasoning sql.NullString
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT visibility_status, searchable, pinned, subject_entity_id, predicate, object_entity_id,
       object_literal, content_summary, extraction_reasoning
FROM facts
WHERE id = ?`, fact.ID).Scan(&visibility, &searchable, &pinned, &subjectID, &predicate, &objectID, &objectLiteral, &summary, &reasoning); err != nil {
		t.Fatalf("query hard-forgotten fact: %v", err)
	}
	if visibility != string(core.VisibilityForgotten) || searchable != 0 || pinned != 0 {
		t.Fatalf("hard-forgotten fact visibility/searchable/pinned = %s/%d/%d", visibility, searchable, pinned)
	}
	if subjectID.Valid || objectID.Valid || objectLiteral.Valid || reasoning.Valid {
		t.Fatalf("hard forget left semantic nullable fields: subject=%v object=%v literal=%v reasoning=%v", subjectID, objectID, objectLiteral, reasoning)
	}
	if predicate != memsqlite.ForgottenPlaceholder || summary != memsqlite.ForgottenPlaceholder {
		t.Fatalf("hard forget placeholders = %q/%q", predicate, summary)
	}
	var linkReasoningCount int
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT COUNT(*)
FROM memory_links
WHERE (from_node_id = ? OR to_node_id = ?) AND reasoning IS NOT NULL`, fact.ID, fact.ID).Scan(&linkReasoningCount); err != nil {
		t.Fatalf("count link reasoning: %v", err)
	}
	if linkReasoningCount != 0 {
		t.Fatalf("link reasoning count = %d, want 0", linkReasoningCount)
	}
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeFact, fact.ID, 0)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, fact.ID, 0)
	requireQueueCount(t, db.SQLDB(), "fact", fact.ID, "delete_node", 1)
	requireSafeDeletionEvent(t, db.SQLDB(), result.DeletionEventID, memsqlite.ForgetLevelHard, fact.ID, "杭州", "用户住在")
}

func TestForgetRepositorySourceRedactsEpisodeAndTombstonesWithoutRawContent(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	search := memsqlite.NewSearchRepository(db.SQLDB())
	if err := search.UpsertDocument(ctx, core.SearchDocument{
		ID:               "search_ep_visible",
		PersonaID:        "default",
		NodeType:         core.NodeTypeEpisode,
		NodeID:           "ep_visible",
		SearchText:       "我喜欢咖啡。",
		SearchTier:       core.SearchTierHot,
		VisibilityStatus: core.VisibilityVisible,
		SensitivityLevel: core.SensitivityNormal,
		LifecycleStatus:  core.LifecycleActive,
		Searchable:       true,
	}); err != nil {
		t.Fatalf("upsert episode search document: %v", err)
	}
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeEpisode, "ep_visible", 1)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeEpisode, "ep_visible", 1)
	insertIndexMap(t, db.SQLDB(), core.NodeTypeEpisode, "ep_visible")

	var originalHash string
	if err := db.SQLDB().QueryRowContext(ctx, `SELECT content_hash FROM episodes WHERE id = 'ep_visible'`).Scan(&originalHash); err != nil {
		t.Fatalf("query original content hash: %v", err)
	}
	repo := memsqlite.NewForgetRepository(db.SQLDB(), fixedForgetIDs(), fixedForgetNow)
	result, err := repo.Forget(ctx, memsqlite.ForgetRequest{
		PersonaID:  "default",
		Actor:      memsqlite.ForgetActorUser,
		ReasonCode: memsqlite.ForgetReasonUserRequested,
		Level:      memsqlite.ForgetLevelSourceRedact,
		Target: memsqlite.ForgetTarget{
			ScopeMode: memsqlite.ForgetScopeExactNode,
			NodeType:  core.NodeTypeEpisode,
			NodeID:    "ep_visible",
		},
	})
	if err != nil {
		t.Fatalf("source redact: %v", err)
	}
	if result.FTSRowsDeleted != 1 {
		t.Fatalf("reported fts rows deleted = %d, want 1", result.FTSRowsDeleted)
	}

	var content, contentHash, visibility string
	var searchable int
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT content, content_hash, visibility_status, searchable
FROM episodes
WHERE id = 'ep_visible'`).Scan(&content, &contentHash, &visibility, &searchable); err != nil {
		t.Fatalf("query redacted episode: %v", err)
	}
	if content != memsqlite.RedactedPlaceholder || contentHash != sha256HexForgetting(memsqlite.RedactedPlaceholder) {
		t.Fatalf("redacted episode content/hash = %q/%q", content, contentHash)
	}
	if visibility != string(core.VisibilityRedacted) || searchable != 0 {
		t.Fatalf("redacted episode visibility/searchable = %s/%d", visibility, searchable)
	}

	var tombstoneHash, level, actor, reason string
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT content_hash_before_redaction, redaction_level, redaction_actor, redaction_reason_code
FROM episode_tombstones
WHERE episode_id = 'ep_visible'`).Scan(&tombstoneHash, &level, &actor, &reason); err != nil {
		t.Fatalf("query episode tombstone: %v", err)
	}
	if tombstoneHash != originalHash || level != memsqlite.ForgetLevelSourceRedact || actor != memsqlite.ForgetActorUser || reason != memsqlite.ForgetReasonUserRequested {
		t.Fatalf("tombstone = %q/%q/%q/%q", tombstoneHash, level, actor, reason)
	}
	requireSearchRowCount(t, db.SQLDB(), core.NodeTypeEpisode, "ep_visible", 0)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeEpisode, "ep_visible", 0)
	requireQueueCount(t, db.SQLDB(), "episode", "ep_visible", "delete_node", 1)
	requireSafeDeletionEvent(t, db.SQLDB(), result.DeletionEventID, memsqlite.ForgetLevelSourceRedact, "ep_visible", "我喜欢咖啡", "咖啡")
}

func insertForgetFact(t *testing.T, ctx context.Context, db *sql.DB, factID string, object string, summary string, pinned bool) core.Fact {
	t.Helper()

	repo := memsqlite.NewConsolidationRepository(db, fixedConsolidationIDs(), fixedConsolidationNow)
	result, err := repo.ConsolidateCandidate(ctx, memsqlite.ConsolidateCandidateRequest{
		PersonaID: "default",
		Trigger:   "manual",
		Candidate: memsqlite.ManualFactCandidate{
			SubjectEntityID:  "ent_user",
			Predicate:        "likes",
			ObjectLiteral:    ptr(object),
			ContentSummary:   summary,
			SourceEpisodeIDs: []string{"ep_visible"},
			Confidence:       string(core.ExtractionConfidenceExplicit),
			Importance:       0.8,
			Pinned:           pinned,
		},
	})
	if err != nil {
		t.Fatalf("consolidate forget fact: %v", err)
	}
	if result.Fact == nil {
		t.Fatalf("consolidation result fact is nil: %#v", result)
	}
	if _, err := db.ExecContext(ctx, `UPDATE facts SET id = ? WHERE id = ?`, factID, result.Fact.ID); err != nil {
		t.Fatalf("rename fact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE memory_links SET from_node_id = ? WHERE from_node_id = ?`, factID, result.Fact.ID); err != nil {
		t.Fatalf("rename fact links: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE memory_search_documents SET node_id = ?, id = ? WHERE node_type = 'fact' AND node_id = ?`, factID, "search_"+factID, result.Fact.ID); err != nil {
		t.Fatalf("rename fact search document: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE memory_search_fts SET node_id = ? WHERE node_type = 'fact' AND node_id = ?`, factID, result.Fact.ID); err != nil {
		t.Fatalf("rename fact fts document: %v", err)
	}
	result.Fact.ID = factID
	return *result.Fact
}

func insertIndexMap(t *testing.T, db *sql.DB, nodeType core.NodeType, nodeID string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status, indexed_at)
VALUES (?, 'default', ?, ?, 1001, 'indexed', ?)`,
		"index_"+nodeID,
		string(nodeType),
		nodeID,
		fixedForgetNow().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert index map: %v", err)
	}
}

func requireSearchRowCount(t *testing.T, db *sql.DB, nodeType core.NodeType, nodeID string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_search_documents
WHERE node_type = ? AND node_id = ?`, string(nodeType), nodeID).Scan(&got); err != nil {
		t.Fatalf("count search rows: %v", err)
	}
	if got != want {
		t.Fatalf("search rows for %s/%s = %d, want %d", nodeType, nodeID, got, want)
	}
}

func requireFTSRowCount(t *testing.T, db *sql.DB, nodeType core.NodeType, nodeID string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_search_fts
WHERE node_type = ? AND node_id = ?`, string(nodeType), nodeID).Scan(&got); err != nil {
		t.Fatalf("count fts rows: %v", err)
	}
	if got != want {
		t.Fatalf("fts rows for %s/%s = %d, want %d", nodeType, nodeID, got, want)
	}
}

func requireSafeDeletionEvent(t *testing.T, db *sql.DB, eventID string, level string, targetID string, forbidden ...string) {
	t.Helper()

	var gotLevel, targetNodeID, status string
	var scopeJSON, cascadeJSON, auditNote sql.NullString
	if err := db.QueryRow(`
SELECT deletion_level, target_node_id, status, scope_json, cascade_summary_json, audit_note
FROM deletion_events
WHERE id = ?`, eventID).Scan(&gotLevel, &targetNodeID, &status, &scopeJSON, &cascadeJSON, &auditNote); err != nil {
		t.Fatalf("query deletion event: %v", err)
	}
	if gotLevel != level || targetNodeID != targetID || status != "completed" {
		t.Fatalf("deletion event = %q/%q/%q", gotLevel, targetNodeID, status)
	}
	combined := strings.Join([]string{scopeJSON.String, cascadeJSON.String, auditNote.String}, " ")
	for _, value := range forbidden {
		if value != "" && strings.Contains(combined, value) {
			t.Fatalf("deletion event contains forbidden value %q in %q", value, combined)
		}
	}
}

func fixedForgetIDs() func() string {
	index := 0
	return func() string {
		index++
		return fmt.Sprintf("forget_id_%02d", index)
	}
}

func fixedForgetNow() time.Time {
	return time.Date(2026, 5, 10, 13, 0, 0, 0, time.UTC)
}

func sha256HexForgetting(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
