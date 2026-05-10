package memorycore_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceRetrieveFindsConsolidatedFactByKeywordAndLogsAccess(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	coffee := "咖啡"
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", coffee, "用户喜欢咖啡。", episode.ID).Fact

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireAccessEvent(t, db, fact.ID, "retrieved")
}

func TestServiceRetrieveAppliesAuthorityFilters(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡、茶和果汁。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	visible := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	hidden := consolidateLiteral(t, ctx, svc, userID, "likes", "茶", "用户喜欢茶。", episode.ID).Fact
	forgotten := consolidateLiteral(t, ctx, svc, userID, "likes", "果汁", "用户喜欢果汁。", episode.ID).Fact
	unsearchable := consolidateLiteral(t, ctx, svc, userID, "likes", "汽水", "用户喜欢汽水。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, hidden.ID, "visibility_status", memorycore.VisibilityHidden)
	updateFactColumn(t, db, forgotten.ID, "visibility_status", memorycore.VisibilityForgotten)
	updateFactColumn(t, db, unsearchable.ID, "searchable", 0)

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "用户喜欢",
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	requireMemoryItem(t, contextResult, visible.ID, "用户喜欢咖啡。", "")
	requireNoMemoryItem(t, contextResult, hidden.ID)
	requireNoMemoryItem(t, contextResult, forgotten.ID)
	requireNoMemoryItem(t, contextResult, unsearchable.ID)
}

func TestServiceRetrieveHistoricalAndDeepArchivePolicy(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "请叫我 Long。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "以后叫我 Yi。", time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC))
	longName := "Long"
	yiName := "Yi"
	oldName, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "prefers_name",
			ObjectLiteral:    &longName,
			ContentSummary:   "用户偏好被称呼为 Long。",
			SourceEpisodeIDs: []string{firstEpisode.ID},
		},
	})
	if err != nil {
		t.Fatalf("consolidate old name: %v", err)
	}
	newName, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "prefers_name",
			ObjectLiteral:    &yiName,
			ContentSummary:   "用户偏好被称呼为 Yi。",
			SourceEpisodeIDs: []string{secondEpisode.ID},
		},
	})
	if err != nil {
		t.Fatalf("consolidate new name: %v", err)
	}

	currentOnly, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "Long"})
	if err != nil {
		t.Fatalf("retrieve current only: %v", err)
	}
	requireNoMemoryItem(t, currentOnly, oldName.Fact.ID)

	historical, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "Long",
		Policy: memorycore.RetrievalPolicy{
			AllowHistorical: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve historical: %v", err)
	}
	requireMemoryItem(t, historical, oldName.Fact.ID, "用户偏好被称呼为 Long。", "historical")

	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, newName.Fact.ID, "lifecycle_status", "archived")

	archivedDefault, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{QueryText: "Yi"})
	if err != nil {
		t.Fatalf("retrieve archived default: %v", err)
	}
	requireNoMemoryItem(t, archivedDefault, newName.Fact.ID)

	archivedHistorical, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "Yi",
		Policy: memorycore.RetrievalPolicy{
			AllowHistorical: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve archived historical: %v", err)
	}
	requireMemoryItem(t, archivedHistorical, newName.Fact.ID, "用户偏好被称呼为 Yi。", "")

	updateFactColumn(t, db, newName.Fact.ID, "lifecycle_status", "deep_archived")

	deepDefault, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "Yi"})
	if err != nil {
		t.Fatalf("retrieve deep default: %v", err)
	}
	requireNoMemoryItem(t, deepDefault, newName.Fact.ID)

	deepAllowed, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "Yi",
		Policy: memorycore.RetrievalPolicy{
			AllowDeepArchive: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve deep allowed: %v", err)
	}
	requireMemoryItem(t, deepAllowed, newName.Fact.ID, "用户偏好被称呼为 Yi。", "")
}

func TestServiceRetrieveSensitivityPermission(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "不要在晚上十点后提醒我工作。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	boundary := consolidateLiteral(t, ctx, svc, userID, "has_boundary", "晚上十点后不要提醒我工作", "用户不希望晚上十点后被提醒工作。", episode.ID).Fact

	normal, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "晚上十点",
	})
	if err != nil {
		t.Fatalf("retrieve normal: %v", err)
	}
	requireNoMemoryItem(t, normal, boundary.ID)

	sensitive, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "晚上十点",
		Policy: memorycore.RetrievalPolicy{
			SensitivityPermission: memorycore.SensitivitySensitive,
		},
	})
	if err != nil {
		t.Fatalf("retrieve sensitive: %v", err)
	}
	requireMemoryItem(t, sensitive, boundary.ID, "用户不希望晚上十点后被提醒工作。", "")
}

func TestServiceRetrieveFatigueSuppression(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	first, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
	if err != nil {
		t.Fatalf("first retrieve: %v", err)
	}
	requireMemoryItem(t, first, fact.ID, "用户喜欢咖啡。", "")

	second, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
	if err != nil {
		t.Fatalf("second retrieve: %v", err)
	}
	requireNoMemoryItem(t, second, fact.ID)
	if len(second.DoNotMention) != 1 || second.DoNotMention[0].NodeID != fact.ID {
		t.Fatalf("suppression = %#v, want fact %s", second.DoNotMention, fact.ID)
	}
}

func requireMemoryItem(t *testing.T, contextResult *memorycore.MemoryContext, nodeID string, summary string, guidanceContains string) {
	t.Helper()

	for _, block := range contextResult.Blocks {
		for _, item := range block.Items {
			if item.NodeID != nodeID {
				continue
			}
			if item.Summary != summary {
				t.Fatalf("item %s summary = %q, want %q", nodeID, item.Summary, summary)
			}
			if guidanceContains != "" && !strings.Contains(item.UsageGuidance, guidanceContains) {
				t.Fatalf("item %s guidance = %q, want contains %q", nodeID, item.UsageGuidance, guidanceContains)
			}
			return
		}
	}
	t.Fatalf("memory item %s not found in %#v", nodeID, contextResult)
}

func requireNoMemoryItem(t *testing.T, contextResult *memorycore.MemoryContext, nodeID string) {
	t.Helper()

	for _, block := range contextResult.Blocks {
		for _, item := range block.Items {
			if item.NodeID == nodeID {
				t.Fatalf("memory item %s found in %#v", nodeID, contextResult)
			}
		}
	}
}

func requireAccessEvent(t *testing.T, db *sql.DB, factID string, accessType string) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_access_events
WHERE node_type = 'fact' AND node_id = ? AND access_type = ?`, factID, accessType).Scan(&count); err != nil {
		t.Fatalf("count access events: %v", err)
	}
	if count == 0 {
		t.Fatalf("access event for %s/%s not found", factID, accessType)
	}
}

func updateFactColumn(t *testing.T, db *sql.DB, factID string, column string, value any) {
	t.Helper()

	switch column {
	case "visibility_status", "searchable", "lifecycle_status":
	default:
		t.Fatalf("unsupported fact column %q", column)
	}
	if _, err := db.Exec("UPDATE facts SET "+column+" = ? WHERE id = ?", value, factID); err != nil {
		t.Fatalf("update fact %s.%s: %v", factID, column, err)
	}
}
