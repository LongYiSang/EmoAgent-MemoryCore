package memorycore_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceForgetSoftForgetsFactFromRetrievalButKeepsSummary(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	result, err := svc.Forget(ctx, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelSoft,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeFact,
			NodeID:    fact.ID,
		},
	})
	if err != nil {
		t.Fatalf("soft forget: %v", err)
	}
	if result.DeletionEventID == "" {
		t.Fatal("deletion event id is empty")
	}

	retrieved, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
	if err != nil {
		t.Fatalf("retrieve after soft forget: %v", err)
	}
	requireNoMemoryItem(t, retrieved, fact.ID)

	rebuild, err := svc.RebuildSearchDocuments(ctx, memorycore.RebuildSearchDocumentsRequest{})
	if err != nil {
		t.Fatalf("rebuild search after soft forget: %v", err)
	}
	if rebuild.Upserted != 0 {
		t.Fatalf("rebuild upserted = %d, want 0", rebuild.Upserted)
	}
	retrievedAfterRebuild, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
	if err != nil {
		t.Fatalf("retrieve after soft forget rebuild: %v", err)
	}
	requireNoMemoryItem(t, retrievedAfterRebuild, fact.ID)

	db := openSQLDB(t, dbPath)
	defer db.Close()
	var summary, visibility string
	if err := db.QueryRow(`SELECT content_summary, visibility_status FROM facts WHERE id = ?`, fact.ID).Scan(&summary, &visibility); err != nil {
		t.Fatalf("query soft-forgotten fact: %v", err)
	}
	if summary != "用户喜欢咖啡。" || visibility != memorycore.VisibilityHidden {
		t.Fatalf("soft-forgotten fact summary/visibility = %q/%q", summary, visibility)
	}
	requireSearchDocumentCount(t, db, fact.ID, 0)
}

func TestServiceForgetHardForgetsPinnedFactAndClearsSemanticContent(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我住在杭州。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	object := "杭州"
	inserted, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "likes",
			ObjectLiteral:    &object,
			ContentSummary:   "用户喜欢杭州。",
			SourceEpisodeIDs: []string{episode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.8,
			Pinned:           true,
		},
	})
	if err != nil {
		t.Fatalf("consolidate pinned fact: %v", err)
	}
	fact := inserted.Fact
	if fact == nil {
		t.Fatal("inserted fact is nil")
	}

	if _, err := svc.Forget(ctx, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelHard,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeFact,
			NodeID:    fact.ID,
		},
	}); err != nil {
		t.Fatalf("hard forget: %v", err)
	}

	retrieved, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "杭州"})
	if err != nil {
		t.Fatalf("retrieve after hard forget: %v", err)
	}
	requireNoMemoryItem(t, retrieved, fact.ID)

	db := openSQLDB(t, dbPath)
	defer db.Close()
	var summary, predicate, visibility string
	var pinned int
	var objectLiteral sql.NullString
	if err := db.QueryRow(`
SELECT content_summary, predicate, visibility_status, pinned, object_literal
FROM facts
WHERE id = ?`, fact.ID).Scan(&summary, &predicate, &visibility, &pinned, &objectLiteral); err != nil {
		t.Fatalf("query hard-forgotten fact: %v", err)
	}
	if summary != memorycore.ForgottenPlaceholder || predicate != memorycore.ForgottenPlaceholder || visibility != memorycore.VisibilityForgotten || pinned != 0 || objectLiteral.Valid {
		t.Fatalf("hard-forgotten fact = summary:%q predicate:%q visibility:%q pinned:%d object:%v", summary, predicate, visibility, pinned, objectLiteral)
	}
}

func TestServiceForgetSourceRedactEpisodeRemovesOnlyEvidenceFromRetrieval(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢乌龙茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "乌龙茶", "用户喜欢乌龙茶。", episode.ID).Fact

	if _, err := svc.Forget(ctx, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelSourceRedact,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeEpisode,
			NodeID:    episode.ID,
		},
	}); err != nil {
		t.Fatalf("source redact: %v", err)
	}

	retrieved, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "乌龙茶"})
	if err != nil {
		t.Fatalf("retrieve after source redact: %v", err)
	}
	requireNoMemoryItem(t, retrieved, fact.ID)

	db := openSQLDB(t, dbPath)
	defer db.Close()
	var content, visibility string
	var searchable int
	if err := db.QueryRow(`SELECT content, visibility_status, searchable FROM episodes WHERE id = ?`, episode.ID).Scan(&content, &visibility, &searchable); err != nil {
		t.Fatalf("query redacted episode: %v", err)
	}
	if content != memorycore.RedactedPlaceholder || visibility != memorycore.VisibilityRedacted || searchable != 0 {
		t.Fatalf("redacted episode = %q/%q/%d", content, visibility, searchable)
	}
	var factVisibility string
	var factSearchable int
	if err := db.QueryRow(`SELECT visibility_status, searchable FROM facts WHERE id = ?`, fact.ID).Scan(&factVisibility, &factSearchable); err != nil {
		t.Fatalf("query source-redacted derived fact: %v", err)
	}
	if factVisibility != memorycore.VisibilityVisible || factSearchable != 1 {
		t.Fatalf("source-redacted derived fact visibility/searchable = %q/%d, want visible/1", factVisibility, factSearchable)
	}
	var tombstones int
	if err := db.QueryRow(`SELECT COUNT(*) FROM episode_tombstones WHERE episode_id = ?`, episode.ID).Scan(&tombstones); err != nil {
		t.Fatalf("count tombstones: %v", err)
	}
	if tombstones != 1 {
		t.Fatalf("tombstones = %d, want 1", tombstones)
	}
}

func requireSearchDocumentCount(t *testing.T, db *sql.DB, factID string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_search_documents
WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&got); err != nil {
		t.Fatalf("count search documents: %v", err)
	}
	if got != want {
		t.Fatalf("search document count for %s = %d, want %d", factID, got, want)
	}
}

func TestServiceForgetValidationAndNotFound(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	_, userID := seedConsolidationSubject(t, ctx, svc)
	if _, err := svc.Forget(ctx, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelSourceRedact,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeFact,
			NodeID:    userID,
		},
	}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("source_redact fact err = %v, want ErrInvalidRequest", err)
	}

	if _, err := svc.Forget(ctx, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelSoft,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeFact,
			NodeID:    "missing_fact",
		},
	}); !errors.Is(err, memorycore.ErrNotFound) {
		t.Fatalf("missing fact err = %v, want ErrNotFound", err)
	}
}
