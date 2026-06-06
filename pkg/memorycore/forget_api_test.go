package memorycore_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func forgetExactNode(t *testing.T, ctx context.Context, svc *memorycore.Client, req memorycore.ForgetRequest) (*memorycore.ForgetResult, error) {
	t.Helper()

	previewReq := memorycore.ForgetPreviewRequest{
		PersonaID:      req.PersonaID,
		Actor:          req.Actor,
		RequestedLevel: req.Level,
		ScopeMode:      req.Target.ScopeMode,
		NodeType:       req.Target.NodeType,
		NodeID:         req.Target.NodeID,
	}
	preview, err := svc.Forget().PreviewForget(ctx, previewReq)
	if err != nil {
		return nil, err
	}

	confirmedTargets := make([]memorycore.ExactNodeRef, 0, len(preview.Targets))
	for _, target := range preview.Targets {
		confirmedTargets = append(confirmedTargets, memorycore.ExactNodeRef{
			NodeType: target.NodeType,
			NodeID:   target.NodeID,
		})
	}
	executed, err := svc.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		PersonaID:        req.PersonaID,
		Actor:            req.Actor,
		ReasonCode:       req.ReasonCode,
		Level:            req.Level,
		PreviewRequest:   previewReq,
		PreviewHash:      preview.PreviewHash,
		ConfirmedTargets: confirmedTargets,
		Confirmed:        true,
	})
	if err != nil {
		return nil, err
	}
	if len(executed.Results) == 0 {
		return &memorycore.ForgetResult{}, nil
	}
	return &executed.Results[0], nil
}

func TestServiceForgetPurgeFactIsNotRetrievableOrRebuiltAndScrubsSemanticContent(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我银行卡里有4111号。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "银行卡秘密", "用户提到银行卡卡号4111。", episode.ID).Fact

	result, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelPurge,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeFact,
			NodeID:    fact.ID,
		},
	})
	if err != nil {
		t.Fatalf("purge fact: %v", err)
	}

	retrieved, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "4111"})
	if err != nil {
		t.Fatalf("retrieve purged fact: %v", err)
	}
	requireNoMemoryItem(t, retrieved, fact.ID)

	if _, err := svc.Ops().RebuildSearchDocuments(ctx, memorycore.RebuildSearchDocumentsRequest{}); err != nil {
		t.Fatalf("rebuild search: %v", err)
	}
	retrievedAfterRebuild, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "4111"})
	if err != nil {
		t.Fatalf("retrieve after rebuild: %v", err)
	}
	requireNoMemoryItem(t, retrievedAfterRebuild, fact.ID)

	db := openSQLDB(t, dbPath)
	defer db.Close()
	var visibility string
	var searchable int
	var pinned int
	var predicate string
	var subjectEntityID sql.NullString
	var objectLiteral sql.NullString
	var summary string
	var reasoning sql.NullString
	if err := db.QueryRow(`
SELECT visibility_status, searchable, pinned, predicate, subject_entity_id,
       object_literal, content_summary, extraction_reasoning
FROM facts
WHERE id = ?`, fact.ID).Scan(
		&visibility, &searchable, &pinned, &predicate, &subjectEntityID, &objectLiteral, &summary, &reasoning); err != nil {
		t.Fatalf("query purged fact and deletion event: %v", err)
	}
	if visibility != string(memorycore.VisibilityPurged) || searchable != 0 || pinned != 0 {
		t.Fatalf("purged fact status = %s/%d/%d, want %s/0/0", visibility, searchable, pinned, memorycore.VisibilityPurged)
	}
	if subjectEntityID.Valid || objectLiteral.Valid || reasoning.Valid {
		t.Fatalf("subject/object/reasoning should be nulled for purged fact: subject=%v object=%v reasoning=%v", subjectEntityID, objectLiteral, reasoning)
	}
	if predicate == "likes" || strings.Contains(summary, "4111") || strings.Contains(summary, "银行卡") {
		t.Fatalf("purged fact leaked semantic content: predicate=%q summary=%q", predicate, summary)
	}

	var deletionLevel string
	var targetType string
	var targetID string
	var scopeJSON, cascadeSummary sql.NullString
	if err := db.QueryRow(`SELECT deletion_level, target_node_type, target_node_id, scope_json, cascade_summary_json FROM deletion_events WHERE id = ?`, result.DeletionEventID).Scan(
		&deletionLevel, &targetType, &targetID, &scopeJSON, &cascadeSummary); err != nil {
		t.Fatalf("query deletion event: %v", err)
	}
	if deletionLevel != memorycore.ForgetLevelPurge {
		t.Fatalf("deletion level = %q, want %q", deletionLevel, memorycore.ForgetLevelPurge)
	}
	if targetType != memorycore.ForgetNodeFact || targetID != fact.ID {
		t.Fatalf("deletion event target = %s/%s, want fact/%s", targetType, targetID, fact.ID)
	}
	if scopeJSON.Valid && strings.Contains(scopeJSON.String, "4111") {
		t.Fatalf("deletion event scope includes purged summary text: %s", scopeJSON.String)
	}
	if cascadeSummary.Valid && strings.Contains(cascadeSummary.String, "4111") {
		t.Fatalf("deletion event includes sensitive content: %s", cascadeSummary.String)
	}

	var docsCount, ftsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_search_documents WHERE node_type = 'fact' AND node_id = ?`, fact.ID).Scan(&docsCount); err != nil {
		t.Fatalf("count purged fact search docs: %v", err)
	}
	if docsCount != 0 {
		t.Fatalf("purged fact search docs = %d, want 0", docsCount)
	}
	if isFTSTablePresent(t, db) {
		if err := db.QueryRow(`SELECT COUNT(*) FROM memory_search_fts WHERE node_type = 'fact' AND node_id = ?`, fact.ID).Scan(&ftsCount); err != nil {
			t.Fatalf("count purged fact fts docs: %v", err)
		}
		if ftsCount != 0 {
			t.Fatalf("purged fact fts docs = %d, want 0", ftsCount)
		}
	}
}

func TestServiceForgetResolverPreviewsAndVerifiesEntityScope(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢安静的早晨。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "安静的早晨", "用户喜欢安静的早晨。", episode.ID).Fact

	previewReq := memorycore.ForgetPreviewRequest{
		RequestedLevel: memorycore.ForgetLevelHard,
		ScopeMode:      memorycore.ForgetScopeEntity,
		EntityID:       userID,
	}
	preview, err := svc.Forget().PreviewForget(ctx, previewReq)
	if err != nil {
		t.Fatalf("preview entity forget: %v", err)
	}
	if !preview.RequiresConfirmation || len(preview.Targets) != 1 || preview.Targets[0].NodeID != fact.ID {
		t.Fatalf("entity preview = %#v, want one confirmed fact target %s", preview, fact.ID)
	}

	if _, err := svc.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		Level:          memorycore.ForgetLevelHard,
		PreviewRequest: previewReq,
		Preview:        *preview,
	}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("execute entity forget without confirmation error = %v, want ErrInvalidRequest", err)
	}

	executed, err := svc.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		Level:            memorycore.ForgetLevelHard,
		PreviewRequest:   previewReq,
		PreviewHash:      preview.PreviewHash,
		ConfirmedTargets: []memorycore.ExactNodeRef{{NodeType: memorycore.ForgetNodeFact, NodeID: fact.ID}},
		Confirmed:        true,
	})
	if err != nil {
		t.Fatalf("execute entity forget: %v", err)
	}
	if executed.Executed != 1 {
		t.Fatalf("executed = %d, want 1", executed.Executed)
	}

	verified, err := svc.Forget().VerifyForget(ctx, memorycore.ForgetVerifyRequest{Targets: preview.Targets})
	if err != nil {
		t.Fatalf("verify entity forget: %v", err)
	}
	if !verified.Passed || len(verified.Targets) != 1 || !verified.Targets[0].Passed {
		t.Fatalf("verify entity forget = %#v, want passed target", verified)
	}
}

func TestServiceForgetResolverRecentPromptItemAndEpisodeWindow(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我住在新加坡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "新加坡", "用户喜欢新加坡。", episode.ID).Fact

	promptReq := memorycore.ForgetPreviewRequest{
		ScopeMode: memorycore.ForgetScopeRecentPromptItem,
		NodeType:  memorycore.ForgetNodeFact,
		NodeID:    fact.ID,
		RecentPromptItems: []memorycore.ForgetPromptItem{{
			NodeType: memorycore.ForgetNodeFact,
			NodeID:   fact.ID,
			Summary:  fact.ContentSummary,
		}},
	}
	promptPreview, err := svc.Forget().PreviewForget(ctx, promptReq)
	if err != nil {
		t.Fatalf("preview recent prompt item: %v", err)
	}
	if promptPreview.RequiresConfirmation || len(promptPreview.Targets) != 1 || promptPreview.Targets[0].NodeID != fact.ID {
		t.Fatalf("recent prompt preview = %#v, want direct fact target %s", promptPreview, fact.ID)
	}

	windowReq := memorycore.ForgetPreviewRequest{
		ScopeMode: memorycore.ForgetScopeRecentEpisodeWindow,
		SessionID: sessionID,
		Limit:     1,
	}
	windowPreview, err := svc.Forget().PreviewForget(ctx, windowReq)
	if err != nil {
		t.Fatalf("preview recent episode window: %v", err)
	}
	if !windowPreview.RequiresConfirmation || len(windowPreview.Targets) != 1 || windowPreview.Targets[0].NodeID != episode.ID {
		t.Fatalf("recent episode window preview = %#v, want confirmed episode target %s", windowPreview, episode.ID)
	}
}

func TestServiceForgetResolverBroadTopicPreviewIsSafeAndRequiresExactSelection(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我的银行卡尾号是4111。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "银行卡尾号4111", "用户的银行卡尾号是4111。", episode.ID).Fact

	previewReq := memorycore.ForgetPreviewRequest{
		RequestedLevel: memorycore.ForgetLevelHard,
		ScopeMode:      memorycore.ForgetScopeBroadTopic,
		Topic:          "4111",
		Limit:          5,
	}
	preview, err := svc.Forget().PreviewForget(ctx, previewReq)
	if err != nil {
		t.Fatalf("preview broad topic: %v", err)
	}
	if !preview.RequiresConfirmation || len(preview.Targets) != 1 || preview.Targets[0].NodeID != fact.ID {
		t.Fatalf("broad topic preview = %#v, want one confirmed fact target %s", preview, fact.ID)
	}
	if strings.Contains(preview.Targets[0].Summary, "4111") || strings.Contains(preview.Targets[0].SafeSummary, "4111") {
		t.Fatalf("broad topic preview leaked raw semantic text: %#v", preview.Targets[0])
	}
	executed, err := svc.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		Level:            memorycore.ForgetLevelHard,
		PreviewRequest:   previewReq,
		PreviewHash:      preview.PreviewHash,
		ConfirmedTargets: []memorycore.ExactNodeRef{{NodeType: memorycore.ForgetNodeFact, NodeID: fact.ID}},
		Confirmed:        true,
	})
	if err != nil {
		t.Fatalf("execute broad topic exact selection: %v", err)
	}
	if executed.Executed != 1 {
		t.Fatalf("executed = %d, want 1", executed.Executed)
	}
}

func TestServiceForgetExecuteRejectsForgedPreviewTargets(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡和茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	coffee := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	tea := consolidateLiteral(t, ctx, svc, userID, "likes", "茶", "用户喜欢茶。", episode.ID).Fact

	req := memorycore.ForgetPreviewRequest{
		RequestedLevel: memorycore.ForgetLevelHard,
		ScopeMode:      memorycore.ForgetScopeExactNode,
		NodeType:       memorycore.ForgetNodeFact,
		NodeID:         coffee.ID,
	}
	preview, err := svc.Forget().PreviewForget(ctx, req)
	if err != nil {
		t.Fatalf("preview exact forget: %v", err)
	}
	forged := *preview
	forged.Targets = []memorycore.ForgetResolvedTarget{{NodeType: memorycore.ForgetNodeFact, NodeID: tea.ID}}
	if _, err := svc.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		Level:            memorycore.ForgetLevelHard,
		PreviewRequest:   req,
		PreviewHash:      forged.PreviewHash,
		ConfirmedTargets: []memorycore.ExactNodeRef{{NodeType: memorycore.ForgetNodeFact, NodeID: tea.ID}},
		Confirmed:        true,
	}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("execute forged preview error = %v, want ErrInvalidRequest", err)
	}

	retrieved, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "茶"})
	if err != nil {
		t.Fatalf("retrieve tea after forged execute: %v", err)
	}
	requireMemoryItem(t, retrieved, tea.ID, tea.ContentSummary, "")
}

func TestServiceForgetExecuteRejectsChangedPreviewHashAndRequiresExactSelection(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡和茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	coffee := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	tea := consolidateLiteral(t, ctx, svc, userID, "likes", "茶", "用户喜欢茶。", episode.ID).Fact

	previewReq := memorycore.ForgetPreviewRequest{
		RequestID:      "req_preview_hash",
		RequestedLevel: memorycore.ForgetLevelSoft,
		ScopeMode:      memorycore.ForgetScopeEntity,
		EntityID:       userID,
	}
	preview, err := svc.Forget().PreviewForget(ctx, previewReq)
	if err != nil {
		t.Fatalf("preview entity forget: %v", err)
	}
	if preview.PreviewHash == "" {
		t.Fatal("preview hash is empty")
	}

	if _, err := svc.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		Level:            memorycore.ForgetLevelSoft,
		PreviewRequest:   previewReq,
		PreviewHash:      preview.PreviewHash,
		ConfirmedTargets: nil,
		Confirmed:        true,
	}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("execute without exact targets err = %v, want ErrInvalidRequest", err)
	}
	if _, err := svc.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		Level:            memorycore.ForgetLevelHard,
		PreviewRequest:   previewReq,
		PreviewHash:      preview.PreviewHash,
		ConfirmedTargets: []memorycore.ExactNodeRef{{NodeType: memorycore.ForgetNodeFact, NodeID: coffee.ID}},
		Confirmed:        true,
	}); !errors.Is(err, memorycore.ErrInvalidRequest) || !strings.Contains(err.Error(), "preview_level_mismatch") {
		t.Fatalf("execute level mismatch err = %v, want preview_level_mismatch", err)
	}

	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelSoft,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeFact,
			NodeID:    tea.ID,
		},
	}); err != nil {
		t.Fatalf("pre-change soft forget tea: %v", err)
	}

	if _, err := svc.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
		Level:            memorycore.ForgetLevelSoft,
		PreviewRequest:   previewReq,
		PreviewHash:      preview.PreviewHash,
		ConfirmedTargets: []memorycore.ExactNodeRef{{NodeType: memorycore.ForgetNodeFact, NodeID: coffee.ID}},
		Confirmed:        true,
	}); !errors.Is(err, memorycore.ErrInvalidRequest) || !strings.Contains(err.Error(), "preview_changed") {
		t.Fatalf("execute changed preview err = %v, want preview_changed", err)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	var auditRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM semantic_decision_audit WHERE request_id = ? AND decision_type = 'forget_preview'`, "req_preview_hash").Scan(&auditRows); err != nil {
		t.Fatalf("query semantic audit: %v", err)
	}
	if auditRows != 1 {
		t.Fatalf("forget preview audit rows = %d, want 1", auditRows)
	}
}

func isFTSTablePresent(t *testing.T, db *sql.DB) bool {
	t.Helper()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'memory_search_fts'`).Scan(&count); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return count > 0
}

func TestServiceForgetSoftForgetsFactFromRetrievalButKeepsSummary(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	result, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
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

	retrieved, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
	if err != nil {
		t.Fatalf("retrieve after soft forget: %v", err)
	}
	requireNoMemoryItem(t, retrieved, fact.ID)

	rebuild, err := svc.Ops().RebuildSearchDocuments(ctx, memorycore.RebuildSearchDocumentsRequest{})
	if err != nil {
		t.Fatalf("rebuild search after soft forget: %v", err)
	}
	if rebuild.Upserted != 0 {
		t.Fatalf("rebuild upserted = %d, want 0", rebuild.Upserted)
	}
	retrievedAfterRebuild, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
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
	inserted, err := svc.Writes().ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
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

	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
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

	retrieved, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "杭州"})
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

	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
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

	retrieved, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "乌龙茶"})
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

func TestServiceForgetPurgeEpisodeRemovesOnlyEvidenceFromRetrieval(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "secret: card 4111", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "oolong tea", "user likes oolong tea", episode.ID).Fact

	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelPurge,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeEpisode,
			NodeID:    episode.ID,
		},
	}); err != nil {
		t.Fatalf("purge episode: %v", err)
	}

	retrieved, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "oolong tea"})
	if err != nil {
		t.Fatalf("retrieve after purge episode: %v", err)
	}
	requireNoMemoryItem(t, retrieved, fact.ID)

	db := openSQLDB(t, dbPath)
	defer db.Close()
	var factVisibility string
	var factSearchable int
	if err := db.QueryRow(`SELECT visibility_status, searchable FROM facts WHERE id = ?`, fact.ID).Scan(&factVisibility, &factSearchable); err != nil {
		t.Fatalf("query fact after episode purge: %v", err)
	}
	if factVisibility != memorycore.VisibilityVisible || factSearchable != 1 {
		t.Fatalf("fact visibility/searchable after episode purge = %q/%d, want visible/1", factVisibility, factSearchable)
	}
	requireSearchDocumentCount(t, db, fact.ID, 0)

	if _, err := svc.Ops().RebuildSearchDocuments(ctx, memorycore.RebuildSearchDocumentsRequest{}); err != nil {
		t.Fatalf("rebuild search after purge episode: %v", err)
	}
	retrievedAfterRebuild, err := svc.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "oolong tea"})
	if err != nil {
		t.Fatalf("retrieve after purge episode rebuild: %v", err)
	}
	requireNoMemoryItem(t, retrievedAfterRebuild, fact.ID)

	if err := db.QueryRow(`SELECT visibility_status, searchable FROM facts WHERE id = ?`, fact.ID).Scan(&factVisibility, &factSearchable); err != nil {
		t.Fatalf("query fact after episode purge: %v", err)
	}
	if factVisibility != memorycore.VisibilityVisible || factSearchable != 1 {
		t.Fatalf("fact visibility/searchable after episode purge = %q/%d, want visible/1", factVisibility, factSearchable)
	}
	requireSearchDocumentCount(t, db, fact.ID, 0)
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
	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      memorycore.ForgetLevelSourceRedact,
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeFact,
			NodeID:    userID,
		},
	}); !errors.Is(err, memorycore.ErrNotFound) {
		t.Fatalf("source_redact missing fact err = %v, want ErrNotFound", err)
	}

	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
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

func TestServiceForgetPurgeValidationAndNotFound(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      "purge",
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeFact,
			NodeID:    "missing_fact_id",
		},
	}); !errors.Is(err, memorycore.ErrNotFound) {
		t.Fatalf("purge fact err = %v, want ErrNotFound", err)
	}

	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      "purge",
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  memorycore.ForgetNodeEpisode,
			NodeID:    "missing_episode_id",
		},
	}); !errors.Is(err, memorycore.ErrNotFound) {
		t.Fatalf("purge episode err = %v, want ErrNotFound", err)
	}

	if _, err := forgetExactNode(t, ctx, svc, memorycore.ForgetRequest{
		Actor:      memorycore.ForgetActorUser,
		ReasonCode: memorycore.ForgetReasonUserRequested,
		Level:      "purge",
		Target: memorycore.ForgetTarget{
			ScopeMode: memorycore.ForgetScopeExactNode,
			NodeType:  "entity",
			NodeID:    "missing_entity_id",
		},
	}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("purge entity err = %v, want ErrInvalidRequest", err)
	}
}
