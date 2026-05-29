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

func TestManualForgetRecentPromptAutoExecutesSoftForget(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	source := appendConsolidationEpisode(t, ctx, svc, sessionID, "我压力大时希望先被理解。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "以后别再提这件事。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "先理解", "用户压力大时希望先被理解。", source.ID).Fact

	directive, err := svc.AnalyzeManualMemoryDirective(ctx, memorycore.ManualForgetDirectiveRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "以后别再提这件事。",
		RecentPromptRefs: []memorycore.RecentPromptMemoryRef{{
			NodeType: memorycore.ForgetNodeFact,
			NodeID:   fact.ID,
			Summary:  fact.ContentSummary,
		}},
		RuleHint: &memorycore.ManualRuleHint{Kind: memorycore.ManualRuleHintForget, Prefix: "别再提"},
	})
	if err != nil {
		t.Fatalf("AnalyzeManualMemoryDirective: %v", err)
	}
	if directive.Intent != memorycore.ManualForgetIntentForget || directive.ForgetLevelHint != memorycore.ForgetLevelSoft {
		t.Fatalf("directive = %#v, want forget soft_forget", directive)
	}
	directive.RequiresLLMConfirm = true

	plan, err := svc.PlanManualForget(ctx, memorycore.PlanManualForgetRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "以后别再提这件事。",
		Directive:        *directive,
		RecentPromptRefs: []memorycore.RecentPromptMemoryRef{{
			NodeType: memorycore.ForgetNodeFact,
			NodeID:   fact.ID,
			Summary:  fact.ContentSummary,
		}},
	})
	if err != nil {
		t.Fatalf("PlanManualForget: %v", err)
	}
	if plan.Status != memorycore.ManualForgetStatusExecutable || plan.RecommendedAction != memorycore.ManualForgetActionAutoExecute {
		t.Fatalf("plan status/action = %s/%s, want executable/auto_execute", plan.Status, plan.RecommendedAction)
	}
	if plan.OperationID == "" || len(plan.Candidates) != 1 || plan.Candidates[0].TargetID != fact.ID {
		t.Fatalf("plan candidates = %#v", plan)
	}
	if plan.RequiresConfirmation {
		t.Fatalf("recent prompt soft forget requires confirmation: %#v", plan)
	}

	executed, err := svc.ExecuteManualForgetOperation(ctx, memorycore.ExecuteManualForgetOperationRequest{
		OperationID: plan.OperationID,
		Confirmed:   true,
		Actor:       memorycore.ForgetActorUser,
		ReasonCode:  memorycore.ForgetReasonUserRequested,
	})
	if err != nil {
		t.Fatalf("ExecuteManualForgetOperation: %v", err)
	}
	if executed.Status != memorycore.ManualForgetStatusExecuted || !executed.VerifyPassed {
		t.Fatalf("executed = %#v, want verified executed", executed)
	}
	if executed.UserFacingLLMContext == nil || executed.UserFacingLLMContext.Status != memorycore.ManualForgetStatusExecuted {
		t.Fatalf("execution LLM context = %#v", executed.UserFacingLLMContext)
	}

	retrieved, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "先理解"})
	if err != nil {
		t.Fatalf("Retrieve after manual forget: %v", err)
	}
	requireNoMemoryItem(t, retrieved, fact.ID)
}

func TestManualForgetRecentPromptRefsFilterByDirectiveTargetAndAskSelection(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	source := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢杭州的美食，也考虑过去杭州游玩。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "以后别再提我喜欢杭州的美食了。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	direct := consolidateLiteral(t, ctx, svc, userID, "likes", "杭州美食", "用户喜欢杭州的美食。", source.ID).Fact
	local := consolidateLiteral(t, ctx, svc, userID, "likes", "杭州本地美食", "用户喜欢杭州本地美食。", source.ID).Fact
	broad := consolidateLiteral(t, ctx, svc, userID, "likes", "杭州各地美食", "用户喜欢去杭州吃各地美食。", source.ID).Fact
	travel := consolidateLiteral(t, ctx, svc, userID, "has_boundary", "杭州游玩品尝美食", "用户正在考虑去杭州游玩和品尝美食。", source.ID).Fact

	directive, err := svc.AnalyzeManualMemoryDirective(ctx, memorycore.ManualForgetDirectiveRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "以后别再提我喜欢杭州的美食了。",
		RuleHint:         &memorycore.ManualRuleHint{Kind: memorycore.ManualRuleHintForget, Prefix: "别再提"},
	})
	if err != nil {
		t.Fatalf("AnalyzeManualMemoryDirective: %v", err)
	}

	refs := []memorycore.RecentPromptMemoryRef{
		{NodeType: memorycore.ForgetNodeFact, NodeID: broad.ID, Summary: broad.ContentSummary},
		{NodeType: memorycore.ForgetNodeFact, NodeID: local.ID, Summary: local.ContentSummary},
		{NodeType: memorycore.ForgetNodeFact, NodeID: direct.ID, Summary: direct.ContentSummary},
		{NodeType: memorycore.ForgetNodeFact, NodeID: travel.ID, Summary: travel.ContentSummary},
	}
	plan, err := svc.PlanManualForget(ctx, memorycore.PlanManualForgetRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "以后别再提我喜欢杭州的美食了。",
		Directive:        *directive,
		RecentPromptRefs: refs,
	})
	if err != nil {
		t.Fatalf("PlanManualForget: %v", err)
	}
	if plan.Status != memorycore.ManualForgetStatusNeedsConfirmation || !plan.RequiresConfirmation {
		t.Fatalf("plan = %#v, want needs_confirmation", plan)
	}
	if len(plan.Candidates) != 3 {
		t.Fatalf("candidate count = %d, want 3 after target filtering: %#v", len(plan.Candidates), plan.Candidates)
	}
	if containsCandidateTarget(plan.Candidates, travel.ID) {
		t.Fatalf("plan kept unrelated travel candidate %#v", plan.Candidates)
	}
	if plan.ConfirmationContext == nil || !plan.ConfirmationContext.AssistantGuidance.AskConfirmation {
		t.Fatalf("confirmation context = %#v, want ask_confirmation", plan.ConfirmationContext)
	}
	question := plan.ConfirmationContext.AssistantGuidance.SuggestedUserVisibleQuestion
	for _, want := range []string{"A", "B", "C", "处理前"} {
		if !strings.Contains(question, want) {
			t.Fatalf("question %q missing %q", question, want)
		}
	}
	if !containsAnyString(plan.ConfirmationContext.AssistantGuidance.DoNot, "已处理", "记住了") {
		t.Fatalf("guidance do_not = %#v, want explicit no completed-claim wording", plan.ConfirmationContext.AssistantGuidance.DoNot)
	}
}

func TestManualForgetBroadPurgeNeedsConfirmationAndDoesNotExecute(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我参与过 ProjectX，也因为它压力很大。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "ProjectX", "用户参与过 ProjectX。", episode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "has_boundary", "ProjectX 压力", "用户提到 ProjectX 带来压力。", episode.ID).Fact
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "彻底删除关于 ProjectX 的所有记忆。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))

	directive, err := svc.AnalyzeManualMemoryDirective(ctx, memorycore.ManualForgetDirectiveRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "彻底删除关于 ProjectX 的所有记忆。",
		RuleHint:         &memorycore.ManualRuleHint{Kind: memorycore.ManualRuleHintForget, Prefix: "删除"},
	})
	if err != nil {
		t.Fatalf("AnalyzeManualMemoryDirective: %v", err)
	}
	if directive.ForgetLevelHint != memorycore.ForgetLevelPurge {
		t.Fatalf("directive level = %q, want purge", directive.ForgetLevelHint)
	}

	plan, err := svc.PlanManualForget(ctx, memorycore.PlanManualForgetRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "彻底删除关于 ProjectX 的所有记忆。",
		Directive:        *directive,
	})
	if err != nil {
		t.Fatalf("PlanManualForget: %v", err)
	}
	if plan.Status != memorycore.ManualForgetStatusNeedsConfirmation || plan.RecommendedAction != memorycore.ManualForgetActionAskLLMConfirmation {
		t.Fatalf("plan = %#v, want needs_confirmation ask_llm_confirmation", plan)
	}
	if !plan.RequiresConfirmation || plan.OperationID == "" {
		t.Fatalf("plan confirmation/operation = %#v", plan)
	}
	if len(plan.Candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2: %#v", len(plan.Candidates), plan.Candidates)
	}
	if plan.ConfirmationContext == nil || plan.ConfirmationContext.Status != memorycore.ManualForgetStatusNeedsConfirmation {
		t.Fatalf("confirmation context = %#v", plan.ConfirmationContext)
	}
	if plan.ConfirmationContext.SafeCandidateCount > 5 {
		t.Fatalf("safe candidate count = %d, want <= 5", plan.ConfirmationContext.SafeCandidateCount)
	}
	if contextContains(plan.ConfirmationContext, "ProjectX") || contextContains(plan.ConfirmationContext, first.ID) || contextContains(plan.ConfirmationContext, second.ID) {
		t.Fatalf("confirmation context leaked raw target or internal id: %#v", plan.ConfirmationContext)
	}

	pending, err := svc.GetPendingManualForgetOperation(ctx, memorycore.GetPendingManualForgetOperationRequest{
		SessionID: &sessionID,
	})
	if err != nil {
		t.Fatalf("GetPendingManualForgetOperation: %v", err)
	}
	if pending == nil || pending.ID != plan.OperationID {
		t.Fatalf("pending = %#v, want operation %s", pending, plan.OperationID)
	}
	if !pending.RequiresConfirmation {
		t.Fatalf("pending requires_confirmation = false, want true: %#v", pending)
	}

	retrieved, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "ProjectX"})
	if err != nil {
		t.Fatalf("Retrieve before confirmation: %v", err)
	}
	requireMemoryItem(t, retrieved, first.ID, first.ContentSummary, "")
}

func TestManualForgetBroadPurgeRejectsWeakOrUnrelatedConfirmation(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我参与过 ProjectGuard。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	consolidateLiteral(t, ctx, svc, userID, "likes", "ProjectGuard", "用户参与过 ProjectGuard。", episode.ID)
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "彻底删除关于 ProjectGuard 的所有记忆。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	plan := planManualPurge(t, ctx, svc, sessionID, request.ID, "ProjectGuard")

	for _, reply := range []string{"嗯", "好的", "确认一下今天安排"} {
		decision, err := svc.ClassifyForgetConfirmation(ctx, memorycore.ClassifyForgetConfirmationRequest{
			SessionID:   &sessionID,
			OperationID: plan.OperationID,
			UserReply:   reply,
		})
		if err != nil {
			t.Fatalf("ClassifyForgetConfirmation(%q): %v", reply, err)
		}
		if decision.Decision == memorycore.ForgetConfirmationConfirm || decision.Decision == memorycore.ForgetConfirmationSelect {
			t.Fatalf("decision for %q = %#v, want non-executing decision", reply, decision)
		}
	}
}

func TestManualForgetRecentEpisodeHardForgetExecutes(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	source := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢只喝浅烘咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "浅烘咖啡", "用户喜欢只喝浅烘咖啡。", source.ID).Fact
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "别记这个偏好。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))

	directive, err := svc.AnalyzeManualMemoryDirective(ctx, memorycore.ManualForgetDirectiveRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "别记这个偏好。",
	})
	if err != nil {
		t.Fatalf("AnalyzeManualMemoryDirective: %v", err)
	}
	if directive.ForgetLevelHint != memorycore.ForgetLevelHard {
		t.Fatalf("directive = %#v, want hard_forget", directive)
	}

	plan, err := svc.PlanManualForget(ctx, memorycore.PlanManualForgetRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "别记这个偏好。",
		Directive:        *directive,
	})
	if err != nil {
		t.Fatalf("PlanManualForget: %v", err)
	}
	if plan.Status != memorycore.ManualForgetStatusExecutable || plan.RequiresConfirmation {
		t.Fatalf("plan = %#v, want executable without confirmation", plan)
	}
	if len(plan.Candidates) != 1 || plan.Candidates[0].TargetID != fact.ID {
		t.Fatalf("plan candidates = %#v, want recent episode fact %s", plan.Candidates, fact.ID)
	}

	executed, err := svc.ExecuteManualForgetOperation(ctx, memorycore.ExecuteManualForgetOperationRequest{
		OperationID: plan.OperationID,
		Confirmed:   true,
		Actor:       memorycore.ForgetActorUser,
		ReasonCode:  memorycore.ForgetReasonUserRequested,
	})
	if err != nil {
		t.Fatalf("ExecuteManualForgetOperation: %v", err)
	}
	if executed.Status != memorycore.ManualForgetStatusExecuted || !executed.VerifyPassed {
		t.Fatalf("executed = %#v, want verified executed", executed)
	}
	retrieved, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "浅烘咖啡"})
	if err != nil {
		t.Fatalf("Retrieve after hard forget: %v", err)
	}
	requireNoMemoryItem(t, retrieved, fact.ID)
}

func TestManualForgetConfirmExecutesExactTargetsAndVerifyPasses(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我参与过 ProjectY，也不想再保留它。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "ProjectY", "用户参与过 ProjectY。", episode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "has_boundary", "ProjectY", "用户不想再保留 ProjectY 相关内容。", episode.ID).Fact
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "彻底删除关于 ProjectY 的所有记忆。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	plan := planManualPurge(t, ctx, svc, sessionID, request.ID, "ProjectY")

	decision, err := svc.ClassifyForgetConfirmation(ctx, memorycore.ClassifyForgetConfirmationRequest{
		SessionID:   &sessionID,
		OperationID: plan.OperationID,
		UserReply:   "确认删除全部。",
	})
	if err != nil {
		t.Fatalf("ClassifyForgetConfirmation: %v", err)
	}
	if decision.Decision != memorycore.ForgetConfirmationConfirm || decision.Confidence < 0.9 {
		t.Fatalf("decision = %#v, want confident confirm", decision)
	}

	executed, err := svc.ExecuteManualForgetOperation(ctx, memorycore.ExecuteManualForgetOperationRequest{
		OperationID:        plan.OperationID,
		Confirmed:          true,
		ConfirmedTargetIDs: decision.SelectedTargetIDs,
		Actor:              memorycore.ForgetActorUser,
		ReasonCode:         memorycore.ForgetReasonUserRequested,
	})
	if err != nil {
		t.Fatalf("ExecuteManualForgetOperation: %v", err)
	}
	if executed.Status != memorycore.ManualForgetStatusExecuted || !executed.VerifyPassed {
		t.Fatalf("executed = %#v, want verified executed", executed)
	}
	if executed.DeletedCounts["facts"] != 2 {
		t.Fatalf("deleted counts = %#v, want two facts", executed.DeletedCounts)
	}

	retrieved, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "ProjectY"})
	if err != nil {
		t.Fatalf("Retrieve after confirmation: %v", err)
	}
	requireNoMemoryItem(t, retrieved, first.ID)
	requireNoMemoryItem(t, retrieved, second.ID)
}

func TestManualForgetDenyCancelsPendingWithoutExecuting(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我参与过 ProjectZ。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "ProjectZ", "用户参与过 ProjectZ。", episode.ID).Fact
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "彻底删除关于 ProjectZ 的所有记忆。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	plan := planManualPurge(t, ctx, svc, sessionID, request.ID, "ProjectZ")

	decision, err := svc.ClassifyForgetConfirmation(ctx, memorycore.ClassifyForgetConfirmationRequest{
		SessionID:   &sessionID,
		OperationID: plan.OperationID,
		UserReply:   "先不要删了。",
	})
	if err != nil {
		t.Fatalf("ClassifyForgetConfirmation: %v", err)
	}
	if decision.Decision != memorycore.ForgetConfirmationDeny {
		t.Fatalf("decision = %#v, want deny", decision)
	}
	cancelled, err := svc.ExecuteManualForgetOperation(ctx, memorycore.ExecuteManualForgetOperationRequest{
		OperationID: plan.OperationID,
		Confirmed:   false,
		Actor:       memorycore.ForgetActorUser,
		ReasonCode:  memorycore.ForgetReasonUserRequested,
	})
	if err != nil {
		t.Fatalf("cancel ExecuteManualForgetOperation: %v", err)
	}
	if cancelled.Status != memorycore.ManualForgetStatusCancelled {
		t.Fatalf("cancelled = %#v, want cancelled", cancelled)
	}

	retrieved, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "ProjectZ"})
	if err != nil {
		t.Fatalf("Retrieve after deny: %v", err)
	}
	requireMemoryItem(t, retrieved, fact.ID, fact.ContentSummary, "")
}

func TestManualForgetCurrentEpisodeSourceRedactExecutesAndVerifies(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, _ := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "这是刚才的一段原文，别长期保留。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "刚才那段不要保留原文。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))

	directive, err := svc.AnalyzeManualMemoryDirective(ctx, memorycore.ManualForgetDirectiveRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "刚才那段不要保留原文。",
	})
	if err != nil {
		t.Fatalf("AnalyzeManualMemoryDirective: %v", err)
	}
	if directive.ForgetLevelHint != memorycore.ForgetLevelSourceRedact {
		t.Fatalf("directive = %#v, want source_redact", directive)
	}
	directive.RequiresLLMConfirm = true
	plan, err := svc.PlanManualForget(ctx, memorycore.PlanManualForgetRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "刚才那段不要保留原文。",
		Directive:        *directive,
		SourceEpisodeID:  episode.ID,
	})
	if err != nil {
		t.Fatalf("PlanManualForget: %v", err)
	}
	if plan.Status != memorycore.ManualForgetStatusExecutable || plan.RequestedLevel != memorycore.ForgetLevelSourceRedact {
		t.Fatalf("plan = %#v, want executable source_redact", plan)
	}
	executed, err := svc.ExecuteManualForgetOperation(ctx, memorycore.ExecuteManualForgetOperationRequest{
		OperationID: plan.OperationID,
		Confirmed:   true,
		Actor:       memorycore.ForgetActorUser,
		ReasonCode:  memorycore.ForgetReasonUserRequested,
	})
	if err != nil {
		t.Fatalf("ExecuteManualForgetOperation: %v", err)
	}
	if executed.Status != memorycore.ManualForgetStatusExecuted || !executed.VerifyPassed || executed.DeletedCounts["episodes"] != 1 {
		t.Fatalf("executed = %#v, want one verified episode", executed)
	}

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
}

func TestManualForgetNoMatchDoesNotPersistRawTarget(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, _ := seedConsolidationSubject(t, ctx, svc)
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "忘掉那个不存在的 SecretTopic。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	directive, err := svc.AnalyzeManualMemoryDirective(ctx, memorycore.ManualForgetDirectiveRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "忘掉那个不存在的 SecretTopic。",
		RuleHint:         &memorycore.ManualRuleHint{Kind: memorycore.ManualRuleHintForget, Prefix: "忘"},
	})
	if err != nil {
		t.Fatalf("AnalyzeManualMemoryDirective: %v", err)
	}
	plan, err := svc.PlanManualForget(ctx, memorycore.PlanManualForgetRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "忘掉那个不存在的 SecretTopic。",
		Directive:        *directive,
	})
	if err != nil {
		t.Fatalf("PlanManualForget: %v", err)
	}
	if plan.Status != memorycore.ManualForgetStatusNoMatch || plan.OperationID != "" {
		t.Fatalf("plan = %#v, want no_match without pending operation", plan)
	}
	if plan.ResultContext == nil || plan.ResultContext.Status != memorycore.ManualForgetStatusNoMatch {
		t.Fatalf("result context = %#v, want no_match context", plan.ResultContext)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireNoRawTargetInManualForgetPending(t, db, "SecretTopic")
}

func TestManualForgetOperationLLMHandlesNaturalDirectiveAndConfirmation(t *testing.T) {
	ctx := context.Background()
	svc, _ := openManualForgetOperationLLMService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "ProjectLLM 让我压力很大。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "has_boundary", "ProjectLLM", "用户提到 ProjectLLM 带来压力。", episode.ID).Fact
	request := appendConsolidationEpisode(t, ctx, svc, sessionID, "把跟 ProjectLLM 相关的都清掉。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))

	directive, err := svc.AnalyzeManualMemoryDirective(ctx, memorycore.ManualForgetDirectiveRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "把跟 ProjectLLM 相关的都清掉。",
	})
	if err != nil {
		t.Fatalf("AnalyzeManualMemoryDirective: %v", err)
	}
	if directive.Intent != memorycore.ManualForgetIntentForget || directive.ForgetLevelHint != memorycore.ForgetLevelPurge {
		t.Fatalf("directive = %#v, want LLM forget purge", directive)
	}
	if !hasReasonCode(directive.ReasonCodes, "operation_llm") {
		t.Fatalf("directive reason_codes = %#v, want operation_llm", directive.ReasonCodes)
	}

	plan, err := svc.PlanManualForget(ctx, memorycore.PlanManualForgetRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: request.ID,
		UserText:         "把跟 ProjectLLM 相关的都清掉。",
		Directive:        *directive,
	})
	if err != nil {
		t.Fatalf("PlanManualForget: %v", err)
	}
	if plan.Status != memorycore.ManualForgetStatusNeedsConfirmation || len(plan.Candidates) != 1 || plan.Candidates[0].TargetID != fact.ID {
		t.Fatalf("plan = %#v, want one pending candidate", plan)
	}

	decision, err := svc.ClassifyForgetConfirmation(ctx, memorycore.ClassifyForgetConfirmationRequest{
		SessionID:   &sessionID,
		OperationID: plan.OperationID,
		UserReply:   "按刚才那组处理。",
	})
	if err != nil {
		t.Fatalf("ClassifyForgetConfirmation: %v", err)
	}
	if decision.Decision != memorycore.ForgetConfirmationConfirm || decision.Confidence < 0.9 || len(decision.SelectedTargetIDs) != 1 {
		t.Fatalf("decision = %#v, want LLM confirm selected target", decision)
	}
	if !hasReasonCode(decision.ReasonCodes, "operation_llm") {
		t.Fatalf("decision reason_codes = %#v, want operation_llm", decision.ReasonCodes)
	}
}

func planManualPurge(t *testing.T, ctx context.Context, svc memorycore.Service, sessionID string, requestEpisodeID string, topic string) *memorycore.PlanManualForgetResult {
	t.Helper()

	userText := "彻底删除关于 " + topic + " 的所有记忆。"
	directive, err := svc.AnalyzeManualMemoryDirective(ctx, memorycore.ManualForgetDirectiveRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: requestEpisodeID,
		UserText:         userText,
		RuleHint:         &memorycore.ManualRuleHint{Kind: memorycore.ManualRuleHintForget, Prefix: "删除"},
	})
	if err != nil {
		t.Fatalf("AnalyzeManualMemoryDirective: %v", err)
	}
	plan, err := svc.PlanManualForget(ctx, memorycore.PlanManualForgetRequest{
		SessionID:        &sessionID,
		RequestEpisodeID: requestEpisodeID,
		UserText:         userText,
		Directive:        *directive,
	})
	if err != nil {
		t.Fatalf("PlanManualForget: %v", err)
	}
	if plan.Status != memorycore.ManualForgetStatusNeedsConfirmation {
		t.Fatalf("plan status = %s, want needs_confirmation: %#v", plan.Status, plan)
	}
	return plan
}

func contextContains(ctx *memorycore.MemoryOperationLLMContext, value string) bool {
	if ctx == nil || value == "" {
		return false
	}
	for _, candidate := range ctx.SafeCandidates {
		if strings.Contains(candidate.SafeSummary, value) ||
			strings.Contains(candidate.DisplayID, value) ||
			strings.Contains(candidate.NodeTypeLabel, value) ||
			strings.Contains(candidate.EffectLabel, value) {
			return true
		}
	}
	return strings.Contains(ctx.SafeResultSummary, value)
}

func containsCandidateTarget(candidates []memorycore.ForgetCandidate, targetID string) bool {
	for _, candidate := range candidates {
		if candidate.TargetID == targetID {
			return true
		}
	}
	return false
}

func containsAnyString(values []string, needles ...string) bool {
	for _, value := range values {
		for _, needle := range needles {
			if strings.Contains(value, needle) {
				return true
			}
		}
	}
	return false
}

func requireNoRawTargetInManualForgetPending(t *testing.T, db *sql.DB, forbidden string) {
	t.Helper()

	rows, err := db.Query(`
SELECT candidates_json, confirmation_policy_json
FROM pending_manual_forget_operations`)
	if err != nil {
		t.Fatalf("query pending manual forget operations: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var candidatesJSON, policyJSON string
		if err := rows.Scan(&candidatesJSON, &policyJSON); err != nil {
			t.Fatalf("scan pending manual forget operation: %v", err)
		}
		if strings.Contains(candidatesJSON, forbidden) || strings.Contains(policyJSON, forbidden) {
			t.Fatalf("pending operation leaked raw target %q: candidates=%s policy=%s", forbidden, candidatesJSON, policyJSON)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pending manual forget operations: %v", err)
	}
}

func openManualForgetOperationLLMService(t *testing.T, ctx context.Context) (memorycore.Service, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		Extraction: memorycore.ExtractionOptions{
			Provider: memorycore.ExtractionProviderOptions{
				Kind: memorycore.ExtractionProviderMock,
				ID:   memorycore.ExtractionProviderMock,
			},
		},
		Now: func() time.Time {
			return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	return svc, dbPath
}

func hasReasonCode(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}
