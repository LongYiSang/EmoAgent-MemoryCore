package memorycore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestPreviewForgetSemanticQueryUsesSidecarCandidatesAndAuthorityFilter(t *testing.T) {
	ctx := context.Background()
	adapter := &semanticDedupTestAdapter{}
	svc := openExtractionSemanticTestService(t, ctx, ExtractionOptions{}, SemanticOpsOptions{
		Forget: SemanticForgetOptions{PreviewEnabled: true},
	}, adapter)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_semantic_forget", "ep_seed", "我不吃香菜。")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_session_semantic_forget", "香菜", "用户不吃香菜。", "ep_seed")
	adapter.deleteResult = &MirrorDeleteCandidatesResult{
		Status: "ok",
		Candidates: []MirrorDeleteCandidate{
			{NodeType: ForgetNodeFact, NodeID: inserted.Fact.ID, SafeSummary: "用户不吃香菜。", Score: 0.91},
			{NodeType: ForgetNodeFact, NodeID: "fact_stale", SafeSummary: "stale", Score: 0.99},
		},
	}

	query := "忘掉我之前说过的那个忌口"
	preview, err := svc.PreviewForget(ctx, ForgetPreviewRequest{
		RequestID:           "req_semantic_forget",
		RequestedLevel:      ForgetLevelSoft,
		ScopeMode:           ForgetScopeSemanticQuery,
		SemanticQuery:       &query,
		RequireConfirmation: true,
	})
	if err != nil {
		t.Fatalf("PreviewForget: %v", err)
	}
	if adapter.deleteCalls != 1 || adapter.lastDelete.Intent.RawText != query {
		t.Fatalf("delete candidate call = %d/%#v, want semantic query", adapter.deleteCalls, adapter.lastDelete)
	}
	if preview.SidecarStatus != "ok" || preview.PreviewHash == "" || !preview.RequiresConfirmation {
		t.Fatalf("preview metadata = %#v, want ok confirmed preview", preview)
	}
	if len(preview.Targets) != 1 || preview.Targets[0].NodeID != inserted.Fact.ID || preview.Targets[0].SafeSummary != "用户不吃香菜。" {
		t.Fatalf("preview targets = %#v, want authority-filtered sidecar candidate", preview.Targets)
	}
	verify, err := svc.VerifyForget(ctx, ForgetVerifyRequest{Targets: preview.Targets})
	if err != nil {
		t.Fatalf("VerifyForget: %v", err)
	}
	if verify.Passed {
		t.Fatalf("verify = %#v, semantic preview must not execute deletion", verify)
	}
}

func TestSemanticForgetPreviewExecuteRemovesFactFromRetrieval(t *testing.T) {
	ctx := context.Background()
	adapter := &semanticDedupTestAdapter{}
	svc := openExtractionSemanticTestService(t, ctx, ExtractionOptions{}, SemanticOpsOptions{
		Forget: SemanticForgetOptions{PreviewEnabled: true, ExecuteEnabled: true},
	}, adapter)
	defer svc.Close()
	sessionID := "session_semantic_forget_e2e"
	seedExtractionSession(t, ctx, svc, sessionID, "ep_seed", "我不吃香菜。")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_session_semantic_forget_e2e", "香菜", "用户不吃香菜。", "ep_seed")
	before, err := svc.Retrieve(ctx, RetrievalRequest{SessionID: &sessionID, QueryText: "香菜"})
	if err != nil {
		t.Fatalf("Retrieve before forget: %v", err)
	}
	if !memoryContextHasItem(before, inserted.Fact.ID) {
		t.Fatalf("retrieval before forget missing fact %s: %#v", inserted.Fact.ID, before)
	}
	adapter.deleteResult = &MirrorDeleteCandidatesResult{
		Status: "ok",
		Candidates: []MirrorDeleteCandidate{{
			NodeType:    ForgetNodeFact,
			NodeID:      inserted.Fact.ID,
			SafeSummary: "用户不吃香菜。",
			Score:       0.91,
		}},
	}
	query := "忘掉我之前说过的那个忌口"
	previewReq := ForgetPreviewRequest{
		RequestID:      "req_semantic_forget_e2e",
		RequestedLevel: ForgetLevelSoft,
		ScopeMode:      ForgetScopeSemanticQuery,
		SemanticQuery:  &query,
	}
	preview, err := svc.PreviewForget(ctx, previewReq)
	if err != nil {
		t.Fatalf("PreviewForget: %v", err)
	}
	executed, err := svc.ExecuteForget(ctx, ForgetExecuteRequest{
		Level:            ForgetLevelSoft,
		PreviewRequest:   previewReq,
		PreviewHash:      preview.PreviewHash,
		ConfirmedTargets: []ExactNodeRef{{NodeType: ForgetNodeFact, NodeID: inserted.Fact.ID}},
		Confirmed:        true,
	})
	if err != nil {
		t.Fatalf("ExecuteForget: %v", err)
	}
	if executed.Executed != 1 {
		t.Fatalf("executed = %#v, want one exact node", executed)
	}
	after, err := svc.Retrieve(ctx, RetrievalRequest{SessionID: &sessionID, QueryText: "香菜"})
	if err != nil {
		t.Fatalf("Retrieve after forget: %v", err)
	}
	if memoryContextHasItem(after, inserted.Fact.ID) {
		t.Fatalf("retrieval after forget still has fact %s: %#v", inserted.Fact.ID, after)
	}
}

func TestPreviewForgetSemanticQueryFallsBackToSQLiteWhenSidecarFails(t *testing.T) {
	ctx := context.Background()
	adapter := &semanticDedupTestAdapter{deleteErr: errors.New("sidecar down with private body")}
	svc := openExtractionSemanticTestService(t, ctx, ExtractionOptions{}, SemanticOpsOptions{
		Forget: SemanticForgetOptions{PreviewEnabled: true},
	}, adapter)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_semantic_forget_fallback", "ep_seed", "I like green tea.")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_session_semantic_forget_fallback", "green tea", "User likes green tea.", "ep_seed")

	query := "green tea"
	preview, err := svc.PreviewForget(ctx, ForgetPreviewRequest{
		RequestID:      "req_semantic_forget_fallback",
		RequestedLevel: ForgetLevelSoft,
		ScopeMode:      ForgetScopeSemanticQuery,
		SemanticQuery:  &query,
	})
	if err != nil {
		t.Fatalf("PreviewForget fallback: %v", err)
	}
	if preview.SidecarStatus != "failed" || preview.Status != "resolved" {
		t.Fatalf("preview status = %#v, want failed sidecar with resolved SQLite fallback", preview)
	}
	if len(preview.Targets) != 1 || preview.Targets[0].NodeID != inserted.Fact.ID {
		t.Fatalf("preview targets = %#v, want SQLite fallback fact", preview.Targets)
	}
}

func TestPreviewForgetSemanticQueryFallbackNormalizesUserPronouns(t *testing.T) {
	ctx := context.Background()
	adapter := &semanticDedupTestAdapter{deleteErr: errors.New("sidecar down")}
	svc := openExtractionSemanticTestService(t, ctx, ExtractionOptions{}, SemanticOpsOptions{
		Forget: SemanticForgetOptions{PreviewEnabled: true},
	}, adapter)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_semantic_forget_pronoun", "ep_seed", "我喜欢手冲咖啡。")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_session_semantic_forget_pronoun", "手冲咖啡", "用户喜欢手冲咖啡。", "ep_seed")

	query := "我喜欢手冲咖啡"
	preview, err := svc.PreviewForget(ctx, ForgetPreviewRequest{
		RequestID:      "req_semantic_forget_pronoun",
		RequestedLevel: ForgetLevelSoft,
		ScopeMode:      ForgetScopeSemanticQuery,
		SemanticQuery:  &query,
	})
	if err != nil {
		t.Fatalf("PreviewForget fallback: %v", err)
	}
	if preview.SidecarStatus != "failed" || preview.Status != "resolved" {
		t.Fatalf("preview status = %#v, want failed sidecar with resolved SQLite fallback", preview)
	}
	if len(preview.Targets) != 1 || preview.Targets[0].NodeID != inserted.Fact.ID {
		t.Fatalf("preview targets = %#v, want normalized SQLite fallback fact", preview.Targets)
	}
	verify, err := svc.VerifyForget(ctx, ForgetVerifyRequest{Targets: preview.Targets})
	if err != nil {
		t.Fatalf("VerifyForget: %v", err)
	}
	if verify.Passed {
		t.Fatalf("verify = %#v, preview fallback must not execute deletion", verify)
	}
}

func TestPreviewForgetSemanticQueryFallbackDoesNotBroadenEmptyOperation(t *testing.T) {
	ctx := context.Background()
	adapter := &semanticDedupTestAdapter{deleteErr: errors.New("sidecar down")}
	svc := openExtractionSemanticTestService(t, ctx, ExtractionOptions{}, SemanticOpsOptions{
		Forget: SemanticForgetOptions{PreviewEnabled: true},
	}, adapter)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_semantic_forget_empty", "ep_seed", "我喜欢手冲咖啡。")
	seedExtractionFact(t, ctx, svc, "ent_user_session_semantic_forget_empty", "手冲咖啡", "用户喜欢手冲咖啡。", "ep_seed")

	query := "忘记"
	preview, err := svc.PreviewForget(ctx, ForgetPreviewRequest{
		RequestID:      "req_semantic_forget_empty",
		RequestedLevel: ForgetLevelSoft,
		ScopeMode:      ForgetScopeSemanticQuery,
		SemanticQuery:  &query,
	})
	if err != nil {
		t.Fatalf("PreviewForget fallback: %v", err)
	}
	if preview.SidecarStatus != "failed" || preview.Status != "no_match" {
		t.Fatalf("preview status = %#v, want failed sidecar with no SQLite broadening", preview)
	}
	if len(preview.Targets) != 0 {
		t.Fatalf("preview targets = %#v, want no targets for empty operation query", preview.Targets)
	}
}

func TestManualForgetPreviewPersistsPendingOperation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	svc := openManualForgetPendingTestService(t, ctx, func() time.Time { return now })
	defer svc.Close()
	sessionID := "session_manual_forget_pending"
	seedExtractionSession(t, ctx, svc, sessionID, "ep_seed", "我喜欢手冲咖啡。")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_"+sessionID, "手冲咖啡", "用户喜欢手冲咖啡。", "ep_seed")

	preview, err := svc.PreviewForget(ctx, ForgetPreviewRequest{
		RequestID:           "req_manual_forget_pending",
		PersonaID:           "default",
		RequestedLevel:      ForgetLevelSoft,
		ScopeMode:           ForgetScopeExactNode,
		NodeType:            ForgetNodeFact,
		NodeID:              inserted.Fact.ID,
		SessionID:           sessionID,
		ChatSessionID:       "chat-pending",
		RequestEpisodeID:    "ep_seed",
		RequireConfirmation: true,
	})
	if err != nil {
		t.Fatalf("PreviewForget: %v", err)
	}
	if preview.OperationID == "" || preview.PreviewHash == "" {
		t.Fatalf("preview = %#v, want operation id and preview hash", preview)
	}
	pending, err := svc.GetPendingManualForgetOperation(ctx, GetPendingManualForgetOperationRequest{
		PersonaID:     "default",
		ChatSessionID: "chat-pending",
	})
	if err != nil {
		t.Fatalf("GetPendingManualForgetOperation: %v", err)
	}
	if pending == nil || pending.OperationID != preview.OperationID || pending.PreviewHash != preview.PreviewHash {
		t.Fatalf("pending = %#v, preview = %#v", pending, preview)
	}
	if pending.SessionID != sessionID || pending.RequestEpisodeID != "ep_seed" || pending.Status != ManualForgetOperationStatusPendingConfirmation {
		t.Fatalf("pending metadata = %#v", pending)
	}
	if len(pending.Targets) != 1 || pending.Targets[0].NodeID != inserted.Fact.ID {
		t.Fatalf("pending targets = %#v", pending.Targets)
	}
	if !pending.ExpiresAt.Equal(now.In(pending.ExpiresAt.Location()).Add(pendingManualForgetOperationTTL)) && pending.ExpiresAt.Sub(pending.CreatedAt) != pendingManualForgetOperationTTL {
		t.Fatalf("pending expiry = created:%s expires:%s", pending.CreatedAt, pending.ExpiresAt)
	}
}

func TestManualForgetPreviewWithoutChatSessionDoesNotPersistPendingOperation(t *testing.T) {
	ctx := context.Background()
	svc := openManualForgetPendingTestService(t, ctx, time.Now)
	defer svc.Close()
	sessionID := "session_manual_forget_no_chat"
	seedExtractionSession(t, ctx, svc, sessionID, "ep_seed", "我喜欢手冲咖啡。")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_"+sessionID, "手冲咖啡", "用户喜欢手冲咖啡。", "ep_seed")

	preview, err := svc.PreviewForget(ctx, ForgetPreviewRequest{
		RequestID:           "req_manual_forget_no_chat",
		PersonaID:           "default",
		RequestedLevel:      ForgetLevelSoft,
		ScopeMode:           ForgetScopeExactNode,
		NodeType:            ForgetNodeFact,
		NodeID:              inserted.Fact.ID,
		SessionID:           sessionID,
		RequireConfirmation: true,
	})
	if err != nil {
		t.Fatalf("PreviewForget: %v", err)
	}
	if preview.OperationID != "" {
		t.Fatalf("operation id = %q, want empty without chat session", preview.OperationID)
	}
	pending, err := svc.GetPendingManualForgetOperation(ctx, GetPendingManualForgetOperationRequest{
		PersonaID:     "default",
		ChatSessionID: "chat-missing",
	})
	if err != nil {
		t.Fatalf("GetPendingManualForgetOperation: %v", err)
	}
	if pending != nil {
		t.Fatalf("pending = %#v, want none", pending)
	}
}

func TestManualForgetPreviewAuditFailureFailsPendingOperation(t *testing.T) {
	ctx := context.Background()
	svc := openManualForgetPendingTestService(t, ctx, time.Now)
	defer svc.Close()
	sessionID := "session_manual_forget_audit_failure"
	seedExtractionSession(t, ctx, svc, sessionID, "ep_seed", "我喜欢手冲咖啡。")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_"+sessionID, "手冲咖啡", "用户喜欢手冲咖啡。", "ep_seed")
	coreSvc := svc.(*service)
	if _, err := coreSvc.sqlDB.ExecContext(ctx, `DROP TABLE semantic_decision_audit`); err != nil {
		t.Fatalf("drop audit table: %v", err)
	}
	if _, err := coreSvc.sqlDB.ExecContext(ctx, `CREATE TABLE semantic_decision_audit (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create malformed audit table: %v", err)
	}

	_, err := svc.PreviewForget(ctx, ForgetPreviewRequest{
		RequestID:           "req_manual_forget_audit_failure",
		PersonaID:           "default",
		RequestedLevel:      ForgetLevelSoft,
		ScopeMode:           ForgetScopeExactNode,
		NodeType:            ForgetNodeFact,
		NodeID:              inserted.Fact.ID,
		SessionID:           sessionID,
		ChatSessionID:       "chat-audit-failure",
		RequestEpisodeID:    "ep_seed",
		RequireConfirmation: true,
	})
	if err == nil {
		t.Fatal("PreviewForget error = nil, want audit failure")
	}
	var operationID, status string
	if err := coreSvc.sqlDB.QueryRowContext(ctx, `
SELECT id, status
FROM pending_manual_forget_operations
WHERE chat_session_id = ?`, "chat-audit-failure").Scan(&operationID, &status); err != nil {
		t.Fatalf("query pending operation: %v", err)
	}
	if operationID == "" || status != ManualForgetOperationStatusFailed {
		t.Fatalf("operation %q status = %q, want failed", operationID, status)
	}
}

func TestManualForgetExecuteOperationUpdatesPendingStatus(t *testing.T) {
	ctx := context.Background()
	svc := openManualForgetPendingTestService(t, ctx, time.Now)
	defer svc.Close()
	sessionID := "session_manual_forget_execute"
	seedExtractionSession(t, ctx, svc, sessionID, "ep_seed", "我喜欢手冲咖啡。")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_"+sessionID, "手冲咖啡", "用户喜欢手冲咖啡。", "ep_seed")
	preview := previewPendingManualForgetOperation(t, ctx, svc, sessionID, "chat-execute", inserted.Fact.ID)

	executed, err := svc.ExecuteForget(ctx, ForgetExecuteRequest{
		PersonaID:        "default",
		OperationID:      preview.OperationID,
		PreviewHash:      preview.PreviewHash,
		Level:            ForgetLevelSoft,
		ConfirmedTargets: []ExactNodeRef{{NodeType: ForgetNodeFact, NodeID: inserted.Fact.ID}},
		Confirmed:        true,
	})
	if err != nil {
		t.Fatalf("ExecuteForget: %v", err)
	}
	if executed.OperationID != preview.OperationID || executed.Executed != 1 {
		t.Fatalf("executed = %#v", executed)
	}
	if got := manualForgetOperationStatus(t, svc, preview.OperationID); got != ManualForgetOperationStatusExecuted {
		t.Fatalf("operation status = %q, want executed", got)
	}
	verify, err := svc.VerifyForget(ctx, ForgetVerifyRequest{Targets: preview.Targets})
	if err != nil {
		t.Fatalf("VerifyForget: %v", err)
	}
	if !verify.Passed {
		t.Fatalf("verify = %#v, want forget passed", verify)
	}
}

func TestManualForgetExecuteOperationRejectsPreviewHashMismatch(t *testing.T) {
	ctx := context.Background()
	svc := openManualForgetPendingTestService(t, ctx, time.Now)
	defer svc.Close()
	sessionID := "session_manual_forget_hash"
	seedExtractionSession(t, ctx, svc, sessionID, "ep_seed", "我喜欢手冲咖啡。")
	inserted := seedExtractionFact(t, ctx, svc, "ent_user_"+sessionID, "手冲咖啡", "用户喜欢手冲咖啡。", "ep_seed")
	preview := previewPendingManualForgetOperation(t, ctx, svc, sessionID, "chat-hash", inserted.Fact.ID)

	_, err := svc.ExecuteForget(ctx, ForgetExecuteRequest{
		PersonaID:        "default",
		OperationID:      preview.OperationID,
		PreviewHash:      "sha256:wrong",
		Level:            ForgetLevelSoft,
		ConfirmedTargets: []ExactNodeRef{{NodeType: ForgetNodeFact, NodeID: inserted.Fact.ID}},
		Confirmed:        true,
	})
	if err == nil {
		t.Fatal("ExecuteForget error = nil, want preview hash mismatch")
	}
	if got := manualForgetOperationStatus(t, svc, preview.OperationID); got != ManualForgetOperationStatusFailed {
		t.Fatalf("operation status = %q, want failed", got)
	}
}

func TestManualForgetCancelAndExpirePendingOperation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 6, 9, 0, 0, 0, time.UTC)
	svc := openManualForgetPendingTestService(t, ctx, func() time.Time { return now })
	defer svc.Close()
	sessionID := "session_manual_forget_cancel_expire"
	seedExtractionSession(t, ctx, svc, sessionID, "ep_seed", "我喜欢手冲咖啡。")
	first := seedExtractionFact(t, ctx, svc, "ent_user_"+sessionID, "手冲咖啡", "用户喜欢手冲咖啡。", "ep_seed")
	second := seedExtractionFact(t, ctx, svc, "ent_user_"+sessionID, "香菜", "用户不吃香菜。", "ep_seed")

	cancelPreview := previewPendingManualForgetOperation(t, ctx, svc, sessionID, "chat-cancel", first.Fact.ID)
	cancelled, err := svc.CancelPendingManualForgetOperation(ctx, CancelPendingManualForgetOperationRequest{
		PersonaID:     "default",
		OperationID:   cancelPreview.OperationID,
		ChatSessionID: "chat-cancel",
	})
	if err != nil {
		t.Fatalf("CancelPendingManualForgetOperation: %v", err)
	}
	if cancelled == nil || cancelled.Status != ManualForgetOperationStatusCancelled {
		t.Fatalf("cancelled = %#v", cancelled)
	}

	expirePreview := previewPendingManualForgetOperation(t, ctx, svc, sessionID, "chat-expire", second.Fact.ID)
	now = now.Add(pendingManualForgetOperationTTL + time.Second)
	expired, err := svc.GetPendingManualForgetOperation(ctx, GetPendingManualForgetOperationRequest{
		PersonaID:     "default",
		ChatSessionID: "chat-expire",
	})
	if err != nil {
		t.Fatalf("GetPendingManualForgetOperation expired: %v", err)
	}
	if expired == nil || expired.OperationID != expirePreview.OperationID || expired.Status != ManualForgetOperationStatusExpired {
		t.Fatalf("expired = %#v", expired)
	}
}

func openManualForgetPendingTestService(t *testing.T, ctx context.Context, now func() time.Time) Service {
	t.Helper()
	svc, err := Open(ctx, Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		AutoMigrate: true,
		EnableFTS:   false,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	return svc
}

func previewPendingManualForgetOperation(t *testing.T, ctx context.Context, svc Service, sessionID string, chatSessionID string, factID string) *ForgetPreviewResult {
	t.Helper()
	preview, err := svc.PreviewForget(ctx, ForgetPreviewRequest{
		RequestID:           "req_" + chatSessionID,
		PersonaID:           "default",
		RequestedLevel:      ForgetLevelSoft,
		ScopeMode:           ForgetScopeExactNode,
		NodeType:            ForgetNodeFact,
		NodeID:              factID,
		SessionID:           sessionID,
		ChatSessionID:       chatSessionID,
		RequestEpisodeID:    "ep_seed",
		RequireConfirmation: true,
	})
	if err != nil {
		t.Fatalf("PreviewForget: %v", err)
	}
	if preview.OperationID == "" || preview.PreviewHash == "" {
		t.Fatalf("preview = %#v, want operation id and preview hash", preview)
	}
	return preview
}

func manualForgetOperationStatus(t *testing.T, svc Service, operationID string) string {
	t.Helper()
	coreSvc := svc.(*service)
	var status string
	if err := coreSvc.sqlDB.QueryRow(`SELECT status FROM pending_manual_forget_operations WHERE id = ?`, operationID).Scan(&status); err != nil {
		t.Fatalf("query operation status: %v", err)
	}
	return status
}

func memoryContextHasItem(context *MemoryContext, nodeID string) bool {
	if context == nil {
		return false
	}
	for _, block := range context.Blocks {
		for _, item := range block.Items {
			if item.NodeID == nodeID {
				return true
			}
		}
	}
	return false
}
