package memorycore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRunExtractionFacadeDisabledAndProviderDisabled(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{})
	defer svc.Close()

	_, err := svc.RunExtraction(ctx, RunExtractionRequest{})
	requireExtractionServiceError(t, err, "extraction_disabled")

	svc = openExtractionTestService(t, ctx, ExtractionOptions{
		Enabled:  true,
		Provider: ExtractionProviderOptions{Kind: ExtractionProviderDisabled},
	})
	defer svc.Close()

	_, err = svc.RunExtraction(ctx, RunExtractionRequest{})
	requireExtractionServiceError(t, err, "extraction_provider_disabled")
}

func TestRunExtractionFacadeMockDryRunApplyAndRawLogDefault(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, mockExtractionOptions())
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_seed", "ep_seed", "我喜欢手冲咖啡。")

	dry, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: stringPtrValue("session_seed"),
		Mode:      ExtractionRunModeDryRun,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if dry.Status != ExtractionRunStatusDryRun || dry.AppliedCount != 0 {
		t.Fatalf("dry-run status/applied = %q/%d", dry.Status, dry.AppliedCount)
	}
	if got := extractionFactCount(t, svc); got != 0 {
		t.Fatalf("dry-run fact count = %d, want 0", got)
	}

	apply, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: stringPtrValue("session_seed"),
		Mode:      ExtractionRunModeApply,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if apply.Status != ExtractionRunStatusApplied || apply.AppliedCount != 1 {
		t.Fatalf("apply status/applied = %q/%d", apply.Status, apply.AppliedCount)
	}
	if got := extractionFactCount(t, svc); got != 1 {
		t.Fatalf("apply fact count = %d, want 1", got)
	}
}

func TestRunExtractionFacadeMinimalOptionsUseSafeDefaults(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{
		Enabled:  true,
		Provider: ExtractionProviderOptions{Kind: ExtractionProviderMock},
	})
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_seed", "ep_seed", "我喜欢手冲咖啡。")

	result, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: stringPtrValue("session_seed"),
		Mode:      ExtractionRunModeApply,
		Force:     true,
	})
	if err != nil {
		t.Fatalf("apply with minimal options: %v", err)
	}
	if result.Status != ExtractionRunStatusApplied || result.AppliedCount != 1 {
		t.Fatalf("apply status/applied = %q/%d", result.Status, result.AppliedCount)
	}
	if got := extractionFactCount(t, svc); got != 1 {
		t.Fatalf("apply fact count = %d, want 1", got)
	}
}

func TestRunExtractionSemanticDedupShadowRecordsDiagnosticsWithoutChangingApply(t *testing.T) {
	ctx := context.Background()
	adapter := &semanticDedupTestAdapter{}
	svc := openExtractionSemanticTestService(t, ctx, mockExtractionOptions(), SemanticOpsOptions{
		Dedup: SemanticDedupOptions{Enabled: true, Shadow: true, CandidateLimit: 12, ThresholdProfile: "default_v0"},
	}, adapter)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_semantic_shadow", "ep_seed", "我讨厌晨间会议。")
	existing := seedExtractionFact(t, ctx, svc, "ent_user_session_semantic_shadow", "晨间会议", "用户讨厌晨间会议。", "ep_seed")
	adapter.dedupResult = &MirrorDedupSearchResult{
		Status: "ok",
		Candidates: []MirrorDedupSearchCandidate{{
			NodeType:    ForgetNodeFact,
			NodeID:      existing.Fact.ID,
			Similarity:  0.97,
			MatchClass:  "near_duplicate",
			MatchReason: "same_subject_similar_object",
		}},
	}
	if _, err := svc.AppendEpisode(ctx, AppendEpisodeRequest{ID: "ep_shadow", SessionID: "session_semantic_shadow", Role: RoleUser, Content: "我不喜欢早上八点开会。", SourceType: SourceTypeChat}); err != nil {
		t.Fatalf("append episode: %v", err)
	}

	sessionID := "session_semantic_shadow"
	result, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: &sessionID,
		Mode:      ExtractionRunModeApply,
		Build:     &ExtractionBuildSelector{EpisodeIDs: []string{"ep_shadow"}, SessionID: &sessionID, Limit: 1},
		Force:     true,
	})
	if err != nil {
		t.Fatalf("RunExtraction: %v", err)
	}
	if result.DedupDiagnostics == nil || !result.DedupDiagnostics.Ran || !result.DedupDiagnostics.Shadow || result.DedupDiagnostics.CandidateCount != 1 {
		t.Fatalf("dedup diagnostics = %#v, want shadow candidate", result.DedupDiagnostics)
	}
	if len(result.DedupDiagnostics.Decisions) != 1 || result.DedupDiagnostics.Decisions[0].Action != "review_or_merge" {
		t.Fatalf("dedup decisions = %#v, want shadow review_or_merge", result.DedupDiagnostics.Decisions)
	}
	if result.AppliedCount != 1 || extractionFactCount(t, svc) != 2 {
		t.Fatalf("applied/facts = %d/%d, want shadow to keep normal apply", result.AppliedCount, extractionFactCount(t, svc))
	}
}

func TestRunExtractionSemanticDedupEnforceDiscardsAuthorityCheckedDuplicate(t *testing.T) {
	ctx := context.Background()
	adapter := &semanticDedupTestAdapter{}
	svc := openExtractionSemanticTestService(t, ctx, mockExtractionOptions(), SemanticOpsOptions{
		Dedup: SemanticDedupOptions{Enabled: true, Enforce: true, CandidateLimit: 12, ThresholdProfile: "default_v0"},
	}, adapter)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_semantic_enforce", "ep_seed", "我讨厌晨间会议。")
	existing := seedExtractionFact(t, ctx, svc, "ent_user_session_semantic_enforce", "晨间会议", "用户讨厌晨间会议。", "ep_seed")
	adapter.dedupResult = &MirrorDedupSearchResult{
		Status: "ok",
		Candidates: []MirrorDedupSearchCandidate{{
			NodeType:    ForgetNodeFact,
			NodeID:      existing.Fact.ID,
			Similarity:  0.97,
			MatchClass:  "near_duplicate",
			MatchReason: "same_subject_similar_object",
		}},
	}
	if _, err := svc.AppendEpisode(ctx, AppendEpisodeRequest{ID: "ep_enforce", SessionID: "session_semantic_enforce", Role: RoleUser, Content: "我不喜欢早上八点开会。", SourceType: SourceTypeChat}); err != nil {
		t.Fatalf("append episode: %v", err)
	}

	sessionID := "session_semantic_enforce"
	result, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: &sessionID,
		Mode:      ExtractionRunModeApply,
		Build:     &ExtractionBuildSelector{EpisodeIDs: []string{"ep_enforce"}, SessionID: &sessionID, Limit: 1},
		Force:     true,
	})
	if err != nil {
		t.Fatalf("RunExtraction: %v", err)
	}
	if result.DedupDiagnostics == nil || result.DedupDiagnostics.Shadow || len(result.DedupDiagnostics.Decisions) != 1 {
		t.Fatalf("dedup diagnostics = %#v, want enforce decision", result.DedupDiagnostics)
	}
	if result.DedupDiagnostics.Decisions[0].Action != ConsolidationActionDiscardDuplicate {
		t.Fatalf("dedup action = %#v, want discard_duplicate", result.DedupDiagnostics.Decisions)
	}
	if result.AppliedCount != 0 || extractionFactCount(t, svc) != 1 {
		t.Fatalf("applied/facts = %d/%d, want semantic duplicate discarded", result.AppliedCount, extractionFactCount(t, svc))
	}
	if result.Status != ExtractionRunStatusNothingApplied {
		t.Fatalf("status = %q, want nothing_applied", result.Status)
	}
}

func TestRunExtractionFacadeRawLogEnabledWithoutDirectoryFails(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, mockExtractionOptions())
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_seed", "ep_seed", "我喜欢手冲咖啡。")

	result, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: stringPtrValue("session_seed"),
		Mode:      ExtractionRunModeDryRun,
		RawLog:    &ExtractionRawLogOptions{Enabled: true},
	})
	requireExtractionServiceError(t, err, "raw_log_directory_required")
	if result == nil || result.SanitizedErrorCode != "raw_log_directory_required" {
		t.Fatalf("result error = %#v", result)
	}
}

func TestRunExtractionFacadeManualForgetRoutesOnlyAndDoesNotWriteFact(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, mockExtractionOptions())
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_seed", "ep_seed", "不要再提我讨厌早上八点开会这件事。")

	result, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: stringPtrValue("session_seed"),
		Trigger:   ExtractionTriggerManualForget,
		Mode:      ExtractionRunModeApply,
		Force:     true,
		Policy: ExtractionPolicyOverride{
			ManualForget: boolPtr(true),
		},
	})
	if err != nil {
		t.Fatalf("manual forget: %v", err)
	}
	if len(result.RoutedDeletionIntents) == 0 || result.RoutedDeletionIntents[0].Decision != "route_only" {
		t.Fatalf("routed deletion intents = %#v", result.RoutedDeletionIntents)
	}
	if result.GateResult == nil || len(result.GateResult.DeletionIntentDecisions) == 0 || result.GateResult.DeletionIntentDecisions[0].Notes != deletionIntentRouteNote {
		t.Fatalf("deletion intent gate note = %#v, want current routed preview/execution note", result.GateResult)
	}
	if len(result.RoutedForgetPreviews) == 0 {
		t.Fatalf("routed forget previews missing: %#v", result)
	}
	if got := result.RoutedForgetPreviews[0]; !got.PreviewOnly || got.ScopeMode != ForgetScopeBroadTopic || got.Preview == nil {
		t.Fatalf("routed forget preview = %#v, want preview-only broad topic preview", got)
	} else if got.Preview.Status == "no_match" && got.ErrorCode != "forget_preview_no_match" {
		t.Fatalf("no-match preview should be surfaced explicitly: %#v", got)
	}
	if result.AcceptedCount != 0 || result.RejectedCount == 0 {
		t.Fatalf("counts accepted/rejected = %d/%d", result.AcceptedCount, result.RejectedCount)
	}
	if got := extractionFactCount(t, svc); got != 0 {
		t.Fatalf("manual forget fact count = %d, want 0", got)
	}
}

func TestNormalizeExtractionOptionsKeepsDeletionIntentsPreviewOnlyByDefault(t *testing.T) {
	opts := normalizeExtractionOptions(ExtractionOptions{})
	if opts.Defaults.ExecuteDeletionIntents {
		t.Fatal("ExecuteDeletionIntents = true, want false by default")
	}
}

func TestExtractionGateKeepsHighSensitiveFactsInReviewByDefault(t *testing.T) {
	req := ExtractionRequest{
		SchemaVersion: ExtractionRequestSchemaVersion,
		RequestID:     "req_sensitive",
		PersonaID:     "default",
		Trigger:       ExtractionTriggerSessionEnd,
		Episodes: []ExtractionEpisode{{
			EpisodeID:        "ep_sensitive",
			Role:             RoleUser,
			Content:          "高敏内容",
			VisibilityStatus: VisibilityVisible,
			SensitivityLevel: SensitivityHighlySensitive,
		}},
		PredicateSchemas: []ExtractionPredicateSchema{{
			Predicate:        "likes",
			Cardinality:      "many",
			ConflictPolicy:   "coexist",
			TemporalBehavior: "static",
			ObjectKind:       "literal",
			AllowInference:   true,
		}},
		Policy: ExtractionPolicy{AllowInference: true, AllowSensitiveExtraction: false},
	}
	resp := ExtractionResponse{
		SchemaVersion: ExtractionResponseSchemaVersion,
		RequestID:     req.RequestID,
		PersonaID:     req.PersonaID,
		Trigger:       req.Trigger,
		SourceWindow:  ExtractionSourceWindow{EpisodeIDs: []string{"ep_sensitive"}},
		Facts: []ExtractedFactCandidate{{
			CandidateID:               "f_sensitive",
			SubjectEntityCandidateID:  "user",
			Predicate:                 "likes",
			ObjectLiteral:             stringPtrValue("高敏偏好"),
			ContentSummary:            "用户提到高敏偏好。",
			FactType:                  FactTypeStablePreference,
			ExtractionConfidence:      ConfidenceExplicit,
			ExtractionConfidenceScore: 0.9,
			Importance:                0.7,
			SensitivityLevel:          SensitivityHighlySensitive,
			SourceEpisodeIDs:          []string{"ep_sensitive"},
			QualityDecision:           "accept_for_consolidation",
		}},
	}

	gate := ValidateExtraction(req, resp)
	requireDecisionForTest(t, gate.FactDecisions, "f_sensitive", "needs_review", "highly_sensitive_requires_review")
}

func TestPreviewExtractionDeletionIntentsBroadTopicUsesSafeTargets(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{})
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_sensitive", "ep_sensitive_source", "我的银行卡尾号是4111。")
	object := "银行卡尾号4111"
	inserted, err := svc.ConsolidateCandidate(ctx, ConsolidateCandidateRequest{
		Candidate: ManualFactCandidate{
			SubjectEntityID:  "ent_user_session_sensitive",
			Predicate:        "likes",
			ObjectLiteral:    &object,
			ContentSummary:   "用户的银行卡尾号是4111。",
			SourceEpisodeIDs: []string{"ep_sensitive_source"},
			Confidence:       ConfidenceExplicit,
			Importance:       0.8,
		},
		Policy: ConsolidationPolicy{Approved: true},
	})
	if err != nil {
		t.Fatalf("seed sensitive fact: %v", err)
	}
	if inserted.Fact == nil {
		t.Fatal("seeded fact is nil")
	}

	req := admissionApplyRequest("以后不要再提银行卡尾号4111。", RoleUser, SourceTypeChat)
	resp := ExtractionResponse{
		SchemaVersion: ExtractionResponseSchemaVersion,
		RequestID:     req.RequestID,
		PersonaID:     req.PersonaID,
		SessionID:     req.SessionID,
		Trigger:       req.Trigger,
		SourceWindow:  ExtractionSourceWindow{EpisodeIDs: []string{"ep_admission"}},
		DeletionIntents: []ExtractedDeletionIntent{{
			CandidateID:          "d_sensitive",
			ForgetLevel:          ForgetLevelSoft,
			TargetDescription:    "4111",
			TargetNodeTypeHint:   stringPtrValue(ForgetNodeFact),
			SourceEpisodeID:      "ep_admission",
			Confidence:           0.95,
			RequiresConfirmation: true,
		}},
	}
	gate := ValidateExtraction(req, resp)
	routes := svc.(*service).PreviewExtractionDeletionIntents(ctx, req, resp, gate)
	if len(routes) != 1 {
		t.Fatalf("routes = %#v, want one route", routes)
	}
	route := routes[0]
	if route.ErrorCode != "" || route.Preview == nil {
		t.Fatalf("route = %#v, want successful preview", route)
	}
	if route.ScopeMode != ForgetScopeBroadTopic || !route.Preview.RequiresConfirmation {
		t.Fatalf("preview route/result = %#v / %#v, want confirmed broad topic", route, route.Preview)
	}
	if len(route.Preview.Targets) != 1 || route.Preview.Targets[0].NodeID != inserted.Fact.ID {
		t.Fatalf("preview targets = %#v, want fact %s", route.Preview.Targets, inserted.Fact.ID)
	}
	target := route.Preview.Targets[0]
	if target.Summary == "4111" || target.SafeSummary == "4111" || target.Summary == inserted.Fact.ContentSummary || target.SafeSummary == inserted.Fact.ContentSummary {
		t.Fatalf("preview target leaked sensitive summary: %#v", target)
	}
	encoded, err := json.Marshal(route)
	if err != nil {
		t.Fatalf("marshal route: %v", err)
	}
	if strings.Contains(string(encoded), "4111") || strings.Contains(string(encoded), inserted.Fact.ContentSummary) {
		t.Fatalf("routed forget preview leaked sensitive deletion target: %s", string(encoded))
	}
}

func TestPreviewExtractionDeletionIntentsSourceRedactUsesExactEpisode(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{})
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_redact", "ep_redact", "这段原文不要保留。")

	req := admissionApplyRequest("刚才这段不要保留原文。", RoleUser, SourceTypeChat)
	sessionID := "session_redact"
	req.SessionID = &sessionID
	req.Episodes[0].EpisodeID = "ep_redact"
	resp := ExtractionResponse{
		SchemaVersion: ExtractionResponseSchemaVersion,
		RequestID:     req.RequestID,
		PersonaID:     req.PersonaID,
		SessionID:     req.SessionID,
		Trigger:       req.Trigger,
		SourceWindow:  ExtractionSourceWindow{EpisodeIDs: []string{"ep_redact"}},
		DeletionIntents: []ExtractedDeletionIntent{{
			CandidateID:          "d_redact",
			ForgetLevel:          ForgetLevelSourceRedact,
			TargetDescription:    "刚才这段不要保留原文",
			TargetNodeTypeHint:   stringPtrValue(ForgetNodeEpisode),
			SourceEpisodeID:      "ep_redact",
			Confidence:           0.95,
			RequiresConfirmation: true,
		}},
	}
	gate := ValidateExtraction(req, resp)
	routes := svc.(*service).PreviewExtractionDeletionIntents(ctx, req, resp, gate)
	if len(routes) != 1 {
		t.Fatalf("routes = %#v, want one route", routes)
	}
	route := routes[0]
	if route.ErrorCode != "" || route.ScopeMode != ForgetScopeExactNode || route.NodeType != ForgetNodeEpisode || route.NodeID != "ep_redact" {
		t.Fatalf("route = %#v, want exact episode source_redact preview", route)
	}
	if route.Preview == nil || !route.Preview.RequiresConfirmation || len(route.Preview.Targets) != 1 || route.Preview.Targets[0].NodeID != "ep_redact" {
		t.Fatalf("preview = %#v, want confirmed exact episode target", route.Preview)
	}
}

func TestRunExtractionSessionEndDeletionIntentSoftForgetsMatchedFacts(t *testing.T) {
	ctx := context.Background()
	options := mockExtractionOptions()
	options.Defaults.Mode = ExtractionRunModeApply
	options.Defaults.ExecuteDeletionIntents = true
	svc := openExtractionTestService(t, ctx, options)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_forget_soft", "ep_seed", "I like green tea.")
	inserted, err := svc.ConsolidateCandidate(ctx, ConsolidateCandidateRequest{
		Candidate: ManualFactCandidate{
			SubjectEntityID:  "ent_user_session_forget_soft",
			Predicate:        "likes",
			ObjectLiteral:    stringPtrValue("green tea"),
			ContentSummary:   "User likes green tea.",
			SourceEpisodeIDs: []string{"ep_seed"},
			Confidence:       ConfidenceExplicit,
			Importance:       0.8,
		},
		Policy: ConsolidationPolicy{Approved: true},
	})
	if err != nil {
		t.Fatalf("seed fact: %v", err)
	}
	if _, err := svc.AppendEpisode(ctx, AppendEpisodeRequest{ID: "ep_forget", SessionID: "session_forget_soft", Role: RoleUser, Content: "Please do not mention green tea again.", SourceType: SourceTypeChat}); err != nil {
		t.Fatalf("append forget episode: %v", err)
	}
	sessionID := "session_forget_soft"
	result, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: &sessionID,
		Trigger:   ExtractionTriggerSessionEnd,
		Mode:      ExtractionRunModeApply,
		Build: &ExtractionBuildSelector{
			EpisodeIDs: []string{"ep_forget"},
			SessionID:  &sessionID,
			Limit:      1,
		},
	})
	if err != nil {
		t.Fatalf("RunExtraction: %v", err)
	}
	if result.Status != ExtractionRunStatusApplied || result.AppliedCount != 0 || result.ForgetExecutedCount != 1 || result.ForgetFailureCount != 0 {
		t.Fatalf("result = %#v, want applied forget only", result)
	}
	if len(result.RoutedForgetPreviews) != 1 || result.RoutedForgetPreviews[0].PreviewOnly || result.RoutedForgetPreviews[0].ExecutedCount != 1 {
		t.Fatalf("routed forget previews = %#v, want executed route", result.RoutedForgetPreviews)
	}
	verify, err := svc.VerifyForget(ctx, ForgetVerifyRequest{
		PersonaID: "default",
		Targets: []ForgetResolvedTarget{{
			NodeType: ForgetNodeFact,
			NodeID:   inserted.Fact.ID,
		}},
	})
	if err != nil {
		t.Fatalf("VerifyForget: %v", err)
	}
	if verify == nil || !verify.Passed {
		t.Fatalf("verify = %#v, want passed", verify)
	}
}

func TestRunExtractionSessionEndDeletionIntentNoMatchDoesNotFail(t *testing.T) {
	ctx := context.Background()
	options := mockExtractionOptions()
	options.Defaults.Mode = ExtractionRunModeApply
	options.Defaults.ExecuteDeletionIntents = true
	svc := openExtractionTestService(t, ctx, options)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_forget_nomatch", "ep_forget", "Please do not mention green tea again.")

	sessionID := "session_forget_nomatch"
	result, err := svc.RunExtraction(ctx, RunExtractionRequest{
		SessionID: &sessionID,
		Trigger:   ExtractionTriggerSessionEnd,
		Mode:      ExtractionRunModeApply,
		Build: &ExtractionBuildSelector{
			EpisodeIDs: []string{"ep_forget"},
			SessionID:  &sessionID,
			Limit:      1,
		},
	})
	if err != nil {
		t.Fatalf("RunExtraction: %v", err)
	}
	if result.Status != ExtractionRunStatusNothingApplied || result.ForgetExecutedCount != 0 || result.ForgetFailureCount != 0 {
		t.Fatalf("result = %#v, want nothing applied", result)
	}
	if len(result.RoutedForgetPreviews) != 1 || result.RoutedForgetPreviews[0].ErrorCode != "forget_preview_no_match" || result.RoutedForgetPreviews[0].SkipReason != "forget_preview_no_match" {
		t.Fatalf("routed forget previews = %#v, want no-match skip", result.RoutedForgetPreviews)
	}
}

func TestExtractionDeletionIntentExecutionSkipsNonSoftLevels(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{})
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_forget_hard", "ep_seed", "I like green tea.")
	inserted, err := svc.ConsolidateCandidate(ctx, ConsolidateCandidateRequest{
		Candidate: ManualFactCandidate{
			SubjectEntityID:  "ent_user_session_forget_hard",
			Predicate:        "likes",
			ObjectLiteral:    stringPtrValue("green tea"),
			ContentSummary:   "User likes green tea.",
			SourceEpisodeIDs: []string{"ep_seed"},
			Confidence:       ConfidenceExplicit,
			Importance:       0.8,
		},
		Policy: ConsolidationPolicy{Approved: true},
	})
	if err != nil {
		t.Fatalf("seed fact: %v", err)
	}
	req := admissionApplyRequest("Please delete green tea.", RoleUser, SourceTypeChat)
	resp := ExtractionResponse{
		SchemaVersion: ExtractionResponseSchemaVersion,
		RequestID:     req.RequestID,
		PersonaID:     req.PersonaID,
		SessionID:     req.SessionID,
		Trigger:       req.Trigger,
		SourceWindow:  ExtractionSourceWindow{EpisodeIDs: []string{"ep_admission"}},
		DeletionIntents: []ExtractedDeletionIntent{{
			CandidateID:          "d_hard",
			ForgetLevel:          ForgetLevelHard,
			TargetDescription:    "green tea",
			TargetNodeTypeHint:   stringPtrValue(ForgetNodeFact),
			SourceEpisodeID:      "ep_admission",
			Confidence:           0.95,
			RequiresConfirmation: true,
		}},
	}
	gate := ValidateExtraction(req, resp)
	routes := svc.(*service).PreviewExtractionDeletionIntents(ctx, req, resp, gate)
	routes, executed, failures := executeExtractionDeletionIntents(ctx, svc, routes)
	if executed != 0 || failures != 0 || len(routes) != 1 || routes[0].SkipReason != "unsupported_forget_level" {
		t.Fatalf("routes/executed/failures = %#v/%d/%d, want hard skip", routes, executed, failures)
	}
	verify, err := svc.VerifyForget(ctx, ForgetVerifyRequest{
		PersonaID: "default",
		Targets: []ForgetResolvedTarget{{
			NodeType: ForgetNodeFact,
			NodeID:   inserted.Fact.ID,
		}},
	})
	if err != nil {
		t.Fatalf("VerifyForget: %v", err)
	}
	if verify == nil || verify.Passed {
		t.Fatalf("verify = %#v, want fact still visible", verify)
	}
}

func TestAdmissionFalseMemoryApplyWritesZeroFacts(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{})
	defer svc.Close()
	coreSvc := svc.(*service)

	cases := []struct {
		name       string
		content    string
		role       string
		sourceType string
		modify     func(*ExtractionRequest, *ExtractedFactCandidate)
		reason     string
	}{
		{
			name:    "hypothetical",
			content: "如果我以后搬去东京，你要提醒我整理资料。",
			reason:  "hypothetical_scenario",
			modify: func(req *ExtractionRequest, fact *ExtractedFactCandidate) {
				fact.ObjectLiteral = stringPtrValue("东京")
				fact.ContentSummary = "用户住在东京。"
			},
		},
		{
			name:    "assistant guess",
			content: "你可能是不喜欢早会。",
			role:    RoleAssistant,
			reason:  "assistant_speculation_not_user_fact",
		},
		{
			name:    "assistant suggestion",
			content: "你可以试试周末运动。",
			role:    RoleAssistant,
			reason:  "assistant_suggestion_not_user_fact",
		},
		{
			name:       "tool noise",
			content:    "search result: npm install failed with stack trace",
			role:       RoleToolSummary,
			sourceType: SourceTypePlugin,
			reason:     "tool_noise",
		},
		{
			name:    "do not remember",
			content: "我其实很讨厌早会，但这句别记。",
			reason:  "do_not_remember",
		},
		{
			name:    "sensitive inference",
			content: "我最近状态不太好。",
			reason:  "sensitive_inference",
			modify: func(req *ExtractionRequest, fact *ExtractedFactCandidate) {
				fact.ExtractionConfidence = ConfidenceInferred
				fact.ObjectLiteral = stringPtrValue("长期焦虑")
				fact.ContentSummary = "用户长期焦虑。"
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := admissionApplyRequest(tc.content, tc.role, tc.sourceType)
			resp := admissionApplyResponse(req)
			if tc.modify != nil {
				tc.modify(&req, &resp.Facts[0])
			}
			gate := ValidateExtraction(req, resp)
			if tc.reason == "sensitive_inference" {
				requireDecisionForTest(t, gate.FactDecisions, "f_false", "needs_review", tc.reason)
			} else {
				requireDecisionForTest(t, gate.FactDecisions, "f_false", "reject", tc.reason)
			}
			applied := ApplyAcceptedFacts(ctx, svc, coreSvc.sqlDB, req, resp, gate)
			if applied.AppliedCount != 0 {
				t.Fatalf("applied count = %d, want 0: %#v", applied.AppliedCount, applied)
			}
			if got := extractionFactCount(t, svc); got != 0 {
				t.Fatalf("fact count after %s = %d, want 0", tc.name, got)
			}
		})
	}
}

func TestRunExtractionBatchFacadeUsesServiceDB(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, mockExtractionOptions())
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_one", "ep_one", "我喜欢手冲咖啡。")
	seedExtractionSession(t, ctx, svc, "session_two", "ep_two", "我不喜欢早上八点开会。")

	result, err := svc.RunExtractionBatch(ctx, ExtractionBatchRequest{
		Mode:         ExtractionRunModeDryRun,
		ProviderKind: ExtractionProviderMock,
		ProviderID:   ExtractionProviderMock,
		Limit:        10,
		EpisodeLimit: 50,
		Audit:        ExtractionAuditOff,
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if result.ProcessedCount != 2 || result.FailedCount != 0 {
		t.Fatalf("batch counts = processed:%d failed:%d result:%#v", result.ProcessedCount, result.FailedCount, result)
	}
}

func admissionApplyRequest(content string, role string, sourceType string) ExtractionRequest {
	if role == "" {
		role = RoleUser
	}
	if sourceType == "" {
		sourceType = SourceTypeChat
	}
	sessionID := "session_admission"
	now := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	return ExtractionRequest{
		SchemaVersion: ExtractionRequestSchemaVersion,
		RequestID:     "req_admission",
		PersonaID:     "default",
		SessionID:     &sessionID,
		Trigger:       ExtractionTriggerSessionEnd,
		Now:           now,
		Timezone:      "Asia/Shanghai",
		Episodes: []ExtractionEpisode{{
			EpisodeID:        "ep_admission",
			Role:             role,
			Content:          content,
			OccurredAt:       now,
			SourceType:       sourceType,
			VisibilityStatus: VisibilityVisible,
			SensitivityLevel: SensitivityNormal,
		}},
		PredicateSchemas: []ExtractionPredicateSchema{{
			Predicate:        "dislikes",
			Cardinality:      "many",
			ConflictPolicy:   "coexist",
			TemporalBehavior: "preference",
			ObjectKind:       "literal",
			AllowInference:   true,
		}},
		Policy: ExtractionPolicy{AllowInference: true, MaxFacts: 12, MaxLinks: 20},
	}
}

func admissionApplyResponse(req ExtractionRequest) ExtractionResponse {
	return ExtractionResponse{
		SchemaVersion: ExtractionResponseSchemaVersion,
		RequestID:     req.RequestID,
		PersonaID:     req.PersonaID,
		SessionID:     req.SessionID,
		Trigger:       req.Trigger,
		SourceWindow:  ExtractionSourceWindow{EpisodeIDs: []string{"ep_admission"}},
		Facts: []ExtractedFactCandidate{{
			CandidateID:               "f_false",
			SubjectEntityCandidateID:  "user",
			Predicate:                 "dislikes",
			ObjectLiteral:             stringPtrValue("早会"),
			ContentSummary:            "用户不喜欢早会。",
			FactType:                  FactTypeStablePreference,
			TemporalPrecision:         "unknown",
			ExtractionConfidence:      ConfidenceExplicit,
			ExtractionConfidenceScore: 0.9,
			Importance:                0.7,
			SensitivityLevel:          SensitivityNormal,
			SourceEpisodeIDs:          []string{"ep_admission"},
			OperationHint:             "insert_candidate",
			SearchableHint:            true,
			QualityDecision:           "accept_for_consolidation",
		}},
	}
}

func TestRunExtractionBatchFacadeKeepsServiceRuntimeDefaults(t *testing.T) {
	ctx := context.Background()
	rawDir := t.TempDir()
	options := mockExtractionOptions()
	options.Runtime.UsePreFilter = true
	options.RawLog = ExtractionRawLogOptions{Enabled: true, Directory: rawDir}
	svc := openExtractionTestService(t, ctx, options)
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_one", "ep_one", "我喜欢手冲咖啡。")

	result, err := svc.RunExtractionBatch(ctx, ExtractionBatchRequest{
		Mode:         ExtractionRunModeDryRun,
		ProviderKind: ExtractionProviderMock,
		ProviderID:   ExtractionProviderMock,
		Limit:        10,
		EpisodeLimit: 50,
		Audit:        ExtractionAuditOff,
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if result.ProcessedCount != 1 || result.FailedCount != 0 {
		t.Fatalf("batch counts = processed:%d failed:%d result:%#v", result.ProcessedCount, result.FailedCount, result)
	}
	artifact := readSingleExtractionRawLog(t, rawDir)
	llm, ok := artifact["llm"].(map[string]any)
	if !ok || llm["prefilter"] == nil {
		t.Fatalf("raw log llm = %#v, want prefilter call from service runtime default", artifact["llm"])
	}
}

func TestBuildRequestKnownEntitiesKeepsAliasesGroupedByEntity(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{})
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_aliases", "ep_aliases", "我提到了 Alpha 和 Beta。")

	if _, err := svc.EnsureEntity(ctx, EnsureEntityRequest{
		ID:            "ent_alpha",
		CanonicalName: "Alpha",
		EntityType:    EntityTypeConcept,
	}); err != nil {
		t.Fatalf("ensure alpha: %v", err)
	}
	if _, err := svc.EnsureEntity(ctx, EnsureEntityRequest{
		ID:            "ent_beta",
		CanonicalName: "Beta",
		EntityType:    EntityTypeConcept,
	}); err != nil {
		t.Fatalf("ensure beta: %v", err)
	}

	coreSvc := svc.(*service)
	if _, err := coreSvc.sqlDB.ExecContext(ctx, `
INSERT INTO entities(id, persona_id, canonical_name, entity_type, visibility_status, searchable)
VALUES ('ent_hidden_alias', 'default', 'Hidden', 'concept', 'hidden', 1)`); err != nil {
		t.Fatalf("insert hidden entity: %v", err)
	}
	for _, alias := range []struct {
		id        string
		entityID  string
		value     string
		createdAt string
	}{
		{"alias_beta_old", "ent_beta", "B-old", "2026-05-31T01:00:00Z"},
		{"alias_alpha_old", "ent_alpha", "A-old", "2026-05-31T02:00:00Z"},
		{"alias_beta_new", "ent_beta", "B-new", "2026-05-31T03:00:00Z"},
		{"alias_alpha_new", "ent_alpha", "A-new", "2026-05-31T04:00:00Z"},
		{"alias_hidden", "ent_hidden_alias", "hidden-alias", "2026-05-31T05:00:00Z"},
	} {
		if _, err := coreSvc.sqlDB.ExecContext(ctx, `
INSERT INTO entity_aliases(id, persona_id, entity_id, alias, alias_type, confidence, created_at)
VALUES (?, 'default', ?, ?, 'surface', 1.0, ?)`, alias.id, alias.entityID, alias.value, alias.createdAt); err != nil {
			t.Fatalf("insert alias %s: %v", alias.id, err)
		}
	}

	sessionID := "session_aliases"
	req, err := BuildRequest(ctx, coreSvc.sqlDB, BuildRequestOptions{
		SessionID: &sessionID,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if len(req.KnownEntities) < 2 {
		t.Fatalf("known entities = %#v, want alpha and beta", req.KnownEntities)
	}

	gotAliases := map[string][]string{}
	for _, entity := range req.KnownEntities {
		for _, alias := range entity.Aliases {
			gotAliases[entity.EntityID] = append(gotAliases[entity.EntityID], alias.Alias)
		}
	}
	if want := []string{"A-old", "A-new"}; !slices.Equal(gotAliases["ent_alpha"], want) {
		t.Fatalf("alpha aliases = %#v, want %#v", gotAliases["ent_alpha"], want)
	}
	if want := []string{"B-old", "B-new"}; !slices.Equal(gotAliases["ent_beta"], want) {
		t.Fatalf("beta aliases = %#v, want %#v", gotAliases["ent_beta"], want)
	}
	for _, entity := range req.KnownEntities {
		if entity.EntityID == "ent_hidden_alias" {
			t.Fatalf("hidden entity included in known entities: %#v", entity)
		}
	}
}

func TestBuildRequestDefaultsTimezoneToShanghai(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{})
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_timezone_default", "ep_timezone_default", "我喜欢手冲咖啡。")

	coreSvc := svc.(*service)
	sessionID := "session_timezone_default"
	req, err := BuildRequest(ctx, coreSvc.sqlDB, BuildRequestOptions{
		SessionID: &sessionID,
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q, want Asia/Shanghai", req.Timezone)
	}
}

func TestBuildRequestUsesSubsecondWindowForEpisodes(t *testing.T) {
	ctx := context.Background()
	svc := openExtractionTestService(t, ctx, ExtractionOptions{})
	defer svc.Close()
	seedExtractionSession(t, ctx, svc, "session_subsecond_window", "ep_subsecond_window", "我喜欢绿茶。")

	coreSvc := svc.(*service)
	if _, err := coreSvc.sqlDB.ExecContext(ctx, `UPDATE episodes SET occurred_at = ? WHERE id = ?`, "2026-06-04T16:00:00.900Z", "ep_subsecond_window"); err != nil {
		t.Fatalf("set episode occurred_at: %v", err)
	}
	sessionID := "session_subsecond_window"
	until := time.Date(2026, 6, 4, 16, 0, 0, 100_000_000, time.UTC)
	_, err := BuildRequest(ctx, coreSvc.sqlDB, BuildRequestOptions{
		SessionID: &sessionID,
		Until:     &until,
		Limit:     1,
	})
	if err == nil {
		t.Fatalf("BuildRequest accepted episode after subsecond until boundary")
	}
}

func requireDecisionForTest(t *testing.T, decisions []CandidateGateDecision, candidateID string, decision string, reason string) {
	t.Helper()
	for _, got := range decisions {
		if got.CandidateID != candidateID {
			continue
		}
		if got.Decision != decision {
			t.Fatalf("decision for %s = %q, want %q", candidateID, got.Decision, decision)
		}
		for _, gotReason := range got.ReasonCodes {
			if gotReason == reason {
				return
			}
		}
		t.Fatalf("decision reasons for %s = %#v, want %q", candidateID, got.ReasonCodes, reason)
	}
	t.Fatalf("decision for %s not found in %#v", candidateID, decisions)
}

func openExtractionTestService(t *testing.T, ctx context.Context, extraction ExtractionOptions) Service {
	t.Helper()
	svc, err := Open(ctx, Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		AutoMigrate: true,
		EnableFTS:   false,
		Extraction:  extraction,
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	return svc
}

func openExtractionSemanticTestService(t *testing.T, ctx context.Context, extraction ExtractionOptions, semantic SemanticOpsOptions, adapter MirrorAdapter) Service {
	t.Helper()
	svc, err := Open(ctx, Options{
		DBPath:        filepath.Join(t.TempDir(), "memory.db"),
		AutoMigrate:   true,
		EnableFTS:     false,
		MirrorAdapter: adapter,
		Extraction:    extraction,
		SemanticOps:   semantic,
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	return svc
}

func seedExtractionFact(t *testing.T, ctx context.Context, svc Service, subjectEntityID string, object string, summary string, sourceEpisodeID string) *ConsolidationResult {
	t.Helper()
	inserted, err := svc.ConsolidateCandidate(ctx, ConsolidateCandidateRequest{
		Candidate: ManualFactCandidate{
			SubjectEntityID:  subjectEntityID,
			Predicate:        "dislikes",
			ObjectLiteral:    stringPtrValue(object),
			ContentSummary:   summary,
			SourceEpisodeIDs: []string{sourceEpisodeID},
			Confidence:       ConfidenceExplicit,
			Importance:       0.8,
		},
		Policy: ConsolidationPolicy{Approved: true},
	})
	if err != nil {
		t.Fatalf("seed fact: %v", err)
	}
	if inserted.Fact == nil {
		t.Fatalf("seed fact result = %#v", inserted)
	}
	return inserted
}

type semanticDedupTestAdapter struct {
	dedupCalls  int
	dedupResult *MirrorDedupSearchResult
	dedupErr    error
	lastDedup   MirrorDedupSearchRequest

	deleteCalls  int
	deleteResult *MirrorDeleteCandidatesResult
	deleteErr    error
	lastDelete   MirrorDeleteCandidatesRequest
}

func (a *semanticDedupTestAdapter) UpsertNode(ctx context.Context, payload MirrorNodePayload) (MirrorNodeUpsertResult, error) {
	return MirrorNodeUpsertResult{}, nil
}

func (a *semanticDedupTestAdapter) DeleteNode(ctx context.Context, ref MirrorNodeRef) error {
	return nil
}

func (a *semanticDedupTestAdapter) UpsertEdge(ctx context.Context, payload MirrorEdgePayload) error {
	return nil
}

func (a *semanticDedupTestAdapter) DeleteEdge(ctx context.Context, ref MirrorEdgeRef) error {
	return nil
}

func (a *semanticDedupTestAdapter) DedupSearch(ctx context.Context, req MirrorDedupSearchRequest) (*MirrorDedupSearchResult, error) {
	a.dedupCalls++
	a.lastDedup = req
	if a.dedupErr != nil {
		return nil, a.dedupErr
	}
	if a.dedupResult == nil {
		return &MirrorDedupSearchResult{Status: "ok"}, nil
	}
	return a.dedupResult, nil
}

func (a *semanticDedupTestAdapter) DeleteCandidates(ctx context.Context, req MirrorDeleteCandidatesRequest) (*MirrorDeleteCandidatesResult, error) {
	a.deleteCalls++
	a.lastDelete = req
	if a.deleteErr != nil {
		return nil, a.deleteErr
	}
	if a.deleteResult == nil {
		return &MirrorDeleteCandidatesResult{Status: "ok"}, nil
	}
	return a.deleteResult, nil
}

func mockExtractionOptions() ExtractionOptions {
	return ExtractionOptions{
		Enabled: true,
		Provider: ExtractionProviderOptions{
			Kind:      ExtractionProviderMock,
			ID:        ExtractionProviderMock,
			MaxTokens: 4096,
			Timeout:   60,
		},
		Defaults: ExtractionDefaults{
			Configured:         true,
			Mode:               ExtractionRunModeDryRun,
			Timezone:           "Asia/Singapore",
			AllowInference:     true,
			MaxFacts:           12,
			MaxLinks:           20,
			ApplyAcceptedFacts: true,
		},
		Runtime: ExtractionRuntimeOptions{Configured: true, RepairEnabled: true},
		Audit:   ExtractionAuditOptions{Configured: true, Enabled: false},
	}
}

func seedExtractionSession(t *testing.T, ctx context.Context, svc Service, sessionID string, episodeID string, content string) {
	t.Helper()
	if _, err := svc.StartSession(ctx, StartSessionRequest{ID: sessionID}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if _, err := svc.AppendEpisode(ctx, AppendEpisodeRequest{ID: episodeID, SessionID: sessionID, Content: content}); err != nil {
		t.Fatalf("append episode: %v", err)
	}
	if _, err := svc.EnsureEntity(ctx, EnsureEntityRequest{ID: "ent_user_" + sessionID, CanonicalName: "User", EntityType: EntityTypeUser}); err != nil {
		t.Fatalf("ensure user: %v", err)
	}
}

func extractionFactCount(t *testing.T, svc Service) int {
	t.Helper()
	coreSvc := svc.(*service)
	var count int
	if err := coreSvc.sqlDB.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&count); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	return count
}

func requireExtractionServiceError(t *testing.T, err error, code string) {
	t.Helper()
	var svcErr *ExtractionServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("err = %v, want ExtractionServiceError", err)
	}
	if svcErr.Code != code {
		t.Fatalf("error code = %q, want %q", svcErr.Code, code)
	}
	var coded interface{ ErrorCode() string }
	if !errors.As(err, &coded) {
		t.Fatalf("err = %v, want ErrorCode interface", err)
	}
	if coded.ErrorCode() != code {
		t.Fatalf("ErrorCode() = %q, want %q", coded.ErrorCode(), code)
	}
}

func readSingleExtractionRawLog(t *testing.T, dir string) map[string]any {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read raw log dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("raw log entries = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read raw log artifact: %v", err)
	}
	var artifact map[string]any
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("decode raw log artifact: %v", err)
	}
	return artifact
}
