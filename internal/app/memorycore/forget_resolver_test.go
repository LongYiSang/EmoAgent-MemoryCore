package memorycore

import (
	"context"
	"errors"
	"testing"
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
