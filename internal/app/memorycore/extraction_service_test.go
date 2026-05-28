package memorycore

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
