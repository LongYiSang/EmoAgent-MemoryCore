package memorycore_test

import (
	"context"
	"database/sql"
	"path/filepath"
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

func TestServiceRetrieveUseMirrorAddsValidatedCandidate(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7001, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7001, Score: 0.88, Source: "fake_sparse"}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "espresso-only",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with mirror: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")
	if contextResult.Mirror == nil || contextResult.Mirror.Status != "used" {
		t.Fatalf("mirror diagnostics = %#v, want used", contextResult.Mirror)
	}
	if contextResult.Mirror.SidecarCandidateCount != 1 || contextResult.Mirror.MappedCandidateCount != 1 || contextResult.Mirror.DroppedCandidateCount != 0 {
		t.Fatalf("mirror counts = %#v", contextResult.Mirror)
	}
}

func TestServiceRetrieveUseMirrorFiltersPurgedCandidate(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7001, "indexed")
	updateFactColumn(t, db, fact.ID, "visibility_status", memorycore.VisibilityPurged)
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7001, Score: 0.88, Source: "stale"}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "espresso-only",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with stale mirror: %v", err)
	}
	requireNoMemoryItem(t, contextResult, fact.ID)
	if contextResult.Mirror == nil || contextResult.Mirror.Status != "used" {
		t.Fatalf("mirror diagnostics = %#v, want used", contextResult.Mirror)
	}
	if contextResult.Mirror.DroppedCandidateCount == 0 {
		t.Fatalf("mirror dropped count = %#v, want > 0", contextResult.Mirror)
	}
	var sawAuthorityDrop bool
	for _, candidate := range contextResult.Mirror.Candidates {
		if candidate.SQLiteFactID == fact.ID && candidate.DropReason == "dropped_by_authority_filter" {
			sawAuthorityDrop = true
		}
	}
	if !sawAuthorityDrop {
		t.Fatalf("mirror candidates = %#v, want dropped_by_authority_filter for %s", contextResult.Mirror.Candidates, fact.ID)
	}
}

func TestServiceRetrieveUseMirrorFallsBackWhenAdapterFails(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{err: sql.ErrConnDone}
	svc, _ := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with failing mirror: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")
}

func TestServiceRetrieveUseMirrorFallsBackWhenMirrorDegraded(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{degraded: true}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7001, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7001, Score: 0.88, Source: "degraded"}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "espresso-only",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with degraded mirror: %v", err)
	}
	requireNoMemoryItem(t, contextResult, fact.ID)
}

func TestServiceRetrieveUseMirrorFallsBackWhenPersonaMirrorStateNotReady(t *testing.T) {
	for _, state := range []string{"rebuilding", "degraded"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			adapter := &retrievalMirrorAdapter{}
			svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
			defer svc.Close()

			sessionID, userID := seedConsolidationSubject(t, ctx, svc)
			episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡和乌龙茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
			coffee := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
			tea := consolidateLiteral(t, ctx, svc, userID, "likes", "乌龙茶", "用户喜欢乌龙茶。", episode.ID).Fact
			db := openSQLDB(t, dbPath)
			defer db.Close()
			insertMirrorMapForFact(t, db, tea.ID, 7002, "indexed")
			setMirrorPersonaState(t, db, "default", state)
			adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7002, Score: 0.99, Source: "mirror_only"}}

			contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
				SessionID: &sessionID,
				QueryText: "咖啡",
				Policy: memorycore.RetrievalPolicy{
					UseMirror: true,
				},
			})
			if err != nil {
				t.Fatalf("retrieve with %s persona mirror state: %v", state, err)
			}
			if adapter.calls != 0 {
				t.Fatalf("mirror adapter calls = %d, want 0 for state %s", adapter.calls, state)
			}
			if contextResult.Mirror == nil || contextResult.Mirror.Status != "persona_not_ready" {
				t.Fatalf("mirror diagnostics = %#v, want persona_not_ready", contextResult.Mirror)
			}
			requireMemoryItem(t, contextResult, coffee.ID, "用户喜欢咖啡。", "")
			requireNoMemoryItem(t, contextResult, tea.ID)
		})
	}
}

func TestServiceRetrieveMirrorDiagnosticsDisabledByConfig(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, _ := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{QueryText: "coffee"})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if result.Mirror == nil || result.Mirror.Status != "disabled_by_config" {
		t.Fatalf("mirror diagnostics = %#v, want disabled_by_config", result.Mirror)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter calls = %d, want 0", adapter.calls)
	}
}

func TestServiceRetrieveMirrorDiagnosticsAdapterMissing(t *testing.T) {
	ctx := context.Background()
	svc, _ := openRetrievalMirrorService(t, ctx, nil)
	defer svc.Close()

	result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "coffee",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if result.Mirror == nil || result.Mirror.Status != "adapter_missing" {
		t.Fatalf("mirror diagnostics = %#v, want adapter_missing", result.Mirror)
	}
}

func TestServiceRetrieveMirrorDiagnosticsSidecarErrorAndDegradedAndNoCandidates(t *testing.T) {
	t.Run("sidecar_error", func(t *testing.T) {
		ctx := context.Background()
		adapter := &retrievalMirrorAdapter{err: sql.ErrConnDone}
		svc, _ := openRetrievalMirrorService(t, ctx, adapter)
		defer svc.Close()

		result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
			QueryText: "coffee",
			Policy:    memorycore.RetrievalPolicy{UseMirror: true},
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if result.Mirror == nil || result.Mirror.Status != "sidecar_error" {
			t.Fatalf("mirror diagnostics = %#v, want sidecar_error", result.Mirror)
		}
	})

	t.Run("sidecar_degraded", func(t *testing.T) {
		ctx := context.Background()
		adapter := &retrievalMirrorAdapter{degraded: true}
		svc, _ := openRetrievalMirrorService(t, ctx, adapter)
		defer svc.Close()

		result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
			QueryText: "coffee",
			Policy:    memorycore.RetrievalPolicy{UseMirror: true},
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if result.Mirror == nil || result.Mirror.Status != "sidecar_degraded" {
			t.Fatalf("mirror diagnostics = %#v, want sidecar_degraded", result.Mirror)
		}
	})

	t.Run("no_candidates", func(t *testing.T) {
		ctx := context.Background()
		adapter := &retrievalMirrorAdapter{}
		svc, _ := openRetrievalMirrorService(t, ctx, adapter)
		defer svc.Close()

		result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
			QueryText: "coffee",
			Policy:    memorycore.RetrievalPolicy{UseMirror: true},
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if result.Mirror == nil || result.Mirror.Status != "no_candidates" {
			t.Fatalf("mirror diagnostics = %#v, want no_candidates", result.Mirror)
		}
	})
}

type retrievalMirrorAdapter struct {
	candidates []memorycore.MirrorCandidate
	err        error
	degraded   bool
	calls      int
}

func (a *retrievalMirrorAdapter) FindCandidates(ctx context.Context, req memorycore.MirrorCandidateRequest) (*memorycore.MirrorCandidateResult, error) {
	a.calls++
	if a.err != nil {
		return nil, a.err
	}
	return &memorycore.MirrorCandidateResult{Candidates: append([]memorycore.MirrorCandidate(nil), a.candidates...), Degraded: a.degraded}, nil
}

func (a *retrievalMirrorAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	return memorycore.MirrorNodeUpsertResult{}, nil
}

func (a *retrievalMirrorAdapter) DeleteNode(ctx context.Context, ref memorycore.MirrorNodeRef) error {
	return nil
}

func (a *retrievalMirrorAdapter) UpsertEdge(ctx context.Context, payload memorycore.MirrorEdgePayload) error {
	return nil
}

func (a *retrievalMirrorAdapter) DeleteEdge(ctx context.Context, ref memorycore.MirrorEdgeRef) error {
	return nil
}

func openRetrievalMirrorService(t *testing.T, ctx context.Context, adapter memorycore.MirrorAdapter) (memorycore.Service, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:        dbPath,
		AutoMigrate:   true,
		MirrorAdapter: adapter,
		Now: func() time.Time {
			return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	return svc, dbPath
}

func insertMirrorMapForFact(t *testing.T, db *sql.DB, factID string, triviumNodeID int64, status string) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES (?, 'default', 'fact', ?, ?, ?)`, "map_"+factID, factID, triviumNodeID, status); err != nil {
		t.Fatalf("insert mirror map: %v", err)
	}
}

func setMirrorPersonaState(t *testing.T, db *sql.DB, personaID string, state string) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO mirror_persona_state (persona_id, state, reason, updated_at)
VALUES (?, ?, 'test state', CURRENT_TIMESTAMP)
ON CONFLICT(persona_id) DO UPDATE SET
    state = excluded.state,
    reason = excluded.reason,
    updated_at = excluded.updated_at`, personaID, state); err != nil {
		t.Fatalf("set mirror persona state: %v", err)
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
