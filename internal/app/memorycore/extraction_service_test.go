package memorycore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
