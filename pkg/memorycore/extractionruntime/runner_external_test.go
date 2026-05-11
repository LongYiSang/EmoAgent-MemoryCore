package extractionruntime_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime"
	_ "modernc.org/sqlite"
)

func TestExternalAdapterCanRunDryRunAndApplyWithoutInternalImports(t *testing.T) {
	ctx := context.Background()
	svc, db := seedRuntimeDB(t, ctx, "我不喜欢早上八点开会，这会让我很痛苦。")
	defer svc.Close()
	defer db.Close()

	req := buildRuntimeRequest(t, ctx, db)
	llm := &fakeExtractionLLM{extractText: validRuntimeResponse(t, req)}
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{
		DB:      db,
		Service: svc,
		LLM:     llm,
	})

	dry, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:       req,
		Mode:          memorycore.ExtractionRunModeDryRun,
		Audit:         memorycore.ExtractionAuditOff,
		ProviderKind:  "mock",
		ProviderID:    "fake",
		Model:         "fake-model",
		RepairEnabled: true,
	})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if dry.Status != memorycore.ExtractionRunStatusDryRun {
		t.Fatalf("dry-run status = %q", dry.Status)
	}
	if dry.ApplyResult != nil {
		t.Fatalf("dry-run unexpectedly returned apply result")
	}
	assertFactCount(t, db, 0)

	apply, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:       req,
		Mode:          memorycore.ExtractionRunModeApply,
		Audit:         memorycore.ExtractionAuditOff,
		ProviderKind:  "mock",
		ProviderID:    "fake",
		Model:         "fake-model",
		RepairEnabled: true,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if apply.Status != memorycore.ExtractionRunStatusApplied || apply.AppliedCount != 1 {
		t.Fatalf("apply status/count = %q/%d", apply.Status, apply.AppliedCount)
	}
	assertFactCount(t, db, 1)
}

func TestRunnerRepairPolicyAndBlockedEnvelope(t *testing.T) {
	ctx := context.Background()
	svc, db := seedRuntimeDB(t, ctx, "我不喜欢早上八点开会。")
	defer svc.Close()
	defer db.Close()

	req := buildRuntimeRequest(t, ctx, db)
	llm := &fakeExtractionLLM{
		extractText: "```json\n{}\n```",
		repairText:  validRuntimeResponse(t, req),
	}
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	repaired, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:       req,
		Mode:          memorycore.ExtractionRunModeDryRun,
		Audit:         memorycore.ExtractionAuditOff,
		ProviderKind:  "mock",
		RepairEnabled: true,
	})
	if err != nil {
		t.Fatalf("repair success: %v", err)
	}
	if !repaired.Repaired || llm.repairCalls != 1 || repaired.Status != memorycore.ExtractionRunStatusDryRun {
		t.Fatalf("repair result repaired=%v repairCalls=%d status=%q", repaired.Repaired, llm.repairCalls, repaired.Status)
	}

	blockedResp := responseMap(t, validRuntimeResponse(t, req))
	blockedResp["request_id"] = "wrong-request"
	blockedText := mustJSON(t, blockedResp)
	llm = &fakeExtractionLLM{extractText: blockedText, repairText: validRuntimeResponse(t, req)}
	runner = extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	blocked, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:       req,
		Mode:          memorycore.ExtractionRunModeApply,
		Audit:         memorycore.ExtractionAuditOff,
		ProviderKind:  "mock",
		RepairEnabled: true,
	})
	if err != nil {
		t.Fatalf("blocked run: %v", err)
	}
	if blocked.Status != memorycore.ExtractionRunStatusBlocked || llm.repairCalls != 0 {
		t.Fatalf("blocked status=%q repairCalls=%d", blocked.Status, llm.repairCalls)
	}
	assertFactCount(t, db, 0)

	llm = &fakeExtractionLLM{extractText: "{", repairText: "{"}
	runner = extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	failed, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:       req,
		Mode:          memorycore.ExtractionRunModeApply,
		Audit:         memorycore.ExtractionAuditOff,
		ProviderKind:  "mock",
		RepairEnabled: true,
	})
	if err == nil || failed.Status != memorycore.ExtractionRunStatusFailed {
		t.Fatalf("repair failure status=%q err=%v", failed.Status, err)
	}
	assertFactCount(t, db, 0)
}

func TestPreFilterSkipAndSafetyDefaults(t *testing.T) {
	ctx := context.Background()
	svc, db := seedRuntimeDB(t, ctx, "闲聊一下天气。")
	defer svc.Close()
	defer db.Close()

	req := buildRuntimeRequest(t, ctx, db)
	llm := &fakeExtractionLLM{prefilterText: prefilterResponse(t, req, map[string]prefilterDecision{
		req.Episodes[0].EpisodeID: {Keep: false, RoutingHint: "skip"},
	})}
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	skipped, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:      req,
		Mode:         memorycore.ExtractionRunModeDryRun,
		Audit:        memorycore.ExtractionAuditOff,
		UsePreFilter: true,
	})
	if err != nil {
		t.Fatalf("prefilter skip: %v", err)
	}
	if skipped.Status != memorycore.ExtractionRunStatusSkipped || llm.extractCalls != 0 {
		t.Fatalf("skip status=%q extractor calls=%d", skipped.Status, llm.extractCalls)
	}

	req.Policy.ManualForget = true
	req.Trigger = memorycore.ExtractionTriggerManualForget
	llm = &fakeExtractionLLM{
		prefilterText: prefilterResponse(t, req, map[string]prefilterDecision{
			req.Episodes[0].EpisodeID: {Keep: false, RoutingHint: "skip"},
		}),
		extractText: manualForgetResponse(t, req),
	}
	runner = extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	kept, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:      req,
		Mode:         memorycore.ExtractionRunModeDryRun,
		Audit:        memorycore.ExtractionAuditOff,
		UsePreFilter: true,
	})
	if err != nil {
		t.Fatalf("manual forget prefilter: %v", err)
	}
	if kept.KeptEpisodeCount != 1 || llm.extractCalls != 1 || kept.GateResult.Summary.RoutedCount == 0 {
		t.Fatalf("manual forget kept=%d calls=%d routed=%d", kept.KeptEpisodeCount, llm.extractCalls, kept.GateResult.Summary.RoutedCount)
	}

	req.Trigger = memorycore.ExtractionTriggerSessionEnd
	req.Policy.ManualForget = false
	llm = &fakeExtractionLLM{
		prefilterText: prefilterResponse(t, req, map[string]prefilterDecision{}),
		extractText:   validRuntimeResponse(t, req),
	}
	runner = extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	missing, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:      req,
		Mode:         memorycore.ExtractionRunModeDryRun,
		Audit:        memorycore.ExtractionAuditOff,
		UsePreFilter: true,
	})
	if err != nil {
		t.Fatalf("missing prefilter decision: %v", err)
	}
	if missing.KeptEpisodeCount != 1 || missing.PreFilterReviewCount == 0 {
		t.Fatalf("missing decision kept=%d review=%d", missing.KeptEpisodeCount, missing.PreFilterReviewCount)
	}
}

func TestPreFilterRoutingHintsKeepExceptSkip(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		hint       string
		wantKeep   bool
		wantReview bool
	}{
		{name: "forget manager", hint: "forget_manager", wantKeep: true, wantReview: true},
		{name: "pin manager", hint: "pin_manager", wantKeep: true, wantReview: true},
		{name: "review", hint: "review", wantKeep: true, wantReview: true},
		{name: "legacy route", hint: "route", wantKeep: true, wantReview: true},
		{name: "skip", hint: "skip", wantKeep: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, db := seedRuntimeDB(t, ctx, "闲聊一下天气。")
			defer svc.Close()
			defer db.Close()

			req := buildRuntimeRequest(t, ctx, db)
			llm := &fakeExtractionLLM{
				prefilterText: prefilterResponse(t, req, map[string]prefilterDecision{
					req.Episodes[0].EpisodeID: {Keep: false, RoutingHint: tc.hint},
				}),
				extractText: validRuntimeResponse(t, req),
			}
			runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
			result, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
				Request:      req,
				Mode:         memorycore.ExtractionRunModeDryRun,
				Audit:        memorycore.ExtractionAuditOff,
				UsePreFilter: true,
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if tc.wantKeep {
				if result.KeptEpisodeCount != 1 || llm.extractCalls != 1 || result.Status != memorycore.ExtractionRunStatusDryRun {
					t.Fatalf("kept=%d calls=%d status=%q", result.KeptEpisodeCount, llm.extractCalls, result.Status)
				}
				if tc.wantReview && result.PreFilterReviewCount == 0 {
					t.Fatalf("prefilter review count = 0 for %s", tc.hint)
				}
				return
			}
			if result.Status != memorycore.ExtractionRunStatusSkipped || result.KeptEpisodeCount != 0 || llm.extractCalls != 0 {
				t.Fatalf("skip status=%q kept=%d calls=%d", result.Status, result.KeptEpisodeCount, llm.extractCalls)
			}
		})
	}
}

func TestAuditSanitizesSensitiveTextAndSeparatesDryRunApplyFingerprints(t *testing.T) {
	ctx := context.Background()
	secret := "UNIQUE_PHASE2C_SECRET_9f4e5a"
	svc, db := seedRuntimeDB(t, ctx, "我不喜欢早上八点开会。"+secret)
	defer svc.Close()
	defer db.Close()

	req := buildRuntimeRequest(t, ctx, db)
	audit := extractionruntime.NewSQLiteAuditStore(db)
	llm := &fakeExtractionLLM{extractText: validRuntimeResponse(t, req)}
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm, AuditStore: audit})

	firstDry, err := runner.Run(ctx, memorycore.ExtractionRunRequest{Request: req, Mode: memorycore.ExtractionRunModeDryRun, Audit: memorycore.ExtractionAuditOn, ProviderKind: "mock", ProviderID: "fake", Model: "m"})
	if err != nil {
		t.Fatalf("first dry-run: %v", err)
	}
	secondDry, err := runner.Run(ctx, memorycore.ExtractionRunRequest{Request: req, Mode: memorycore.ExtractionRunModeDryRun, Audit: memorycore.ExtractionAuditOn, ProviderKind: "mock", ProviderID: "fake", Model: "m"})
	if err != nil {
		t.Fatalf("second dry-run: %v", err)
	}
	apply, err := runner.Run(ctx, memorycore.ExtractionRunRequest{Request: req, Mode: memorycore.ExtractionRunModeApply, Audit: memorycore.ExtractionAuditOn, ProviderKind: "mock", ProviderID: "fake", Model: "m"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	forced, err := runner.Run(ctx, memorycore.ExtractionRunRequest{Request: req, Mode: memorycore.ExtractionRunModeDryRun, Audit: memorycore.ExtractionAuditOn, ProviderKind: "mock", ProviderID: "fake", Model: "m", Force: true})
	if err != nil {
		t.Fatalf("forced dry-run: %v", err)
	}
	if !secondDry.SkippedByFingerprint || firstDry.Fingerprint != secondDry.Fingerprint {
		t.Fatalf("dry-run idempotency failed: skipped=%v first=%s second=%s", secondDry.SkippedByFingerprint, firstDry.Fingerprint, secondDry.Fingerprint)
	}
	if apply.SkippedByFingerprint || apply.Fingerprint == firstDry.Fingerprint {
		t.Fatalf("apply should not reuse dry-run fingerprint: apply skipped=%v dry=%s apply=%s", apply.SkippedByFingerprint, firstDry.Fingerprint, apply.Fingerprint)
	}
	if forced.SkippedByFingerprint {
		t.Fatalf("force should rerun")
	}
	assertAuditDoesNotContain(t, db, secret)
}

func TestFingerprintIncludesRuntimeTogglesAndSkippedDryRunIsReusable(t *testing.T) {
	ctx := context.Background()
	svc, db := seedRuntimeDB(t, ctx, "闲聊一下天气。")
	defer svc.Close()
	defer db.Close()

	req := buildRuntimeRequest(t, ctx, db)
	llm := &fakeExtractionLLM{
		prefilterText: prefilterResponse(t, req, map[string]prefilterDecision{
			req.Episodes[0].EpisodeID: {Keep: true, RoutingHint: "extract"},
		}),
		extractText: validRuntimeResponse(t, req),
	}
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	withoutPreFilter, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request: req,
		Mode:    memorycore.ExtractionRunModeDryRun,
		Audit:   memorycore.ExtractionAuditOff,
	})
	if err != nil {
		t.Fatalf("without prefilter: %v", err)
	}
	withPreFilter, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:      req,
		Mode:         memorycore.ExtractionRunModeDryRun,
		Audit:        memorycore.ExtractionAuditOff,
		UsePreFilter: true,
	})
	if err != nil {
		t.Fatalf("with prefilter: %v", err)
	}
	if withoutPreFilter.Fingerprint == withPreFilter.Fingerprint {
		t.Fatalf("prefilter toggle did not change fingerprint: %s", withoutPreFilter.Fingerprint)
	}

	cleanGateOff, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:          req,
		Mode:             memorycore.ExtractionRunModeDryRun,
		Audit:            memorycore.ExtractionAuditOff,
		RequireCleanGate: false,
	})
	if err != nil {
		t.Fatalf("clean gate off: %v", err)
	}
	cleanGateOn, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:          req,
		Mode:             memorycore.ExtractionRunModeDryRun,
		Audit:            memorycore.ExtractionAuditOff,
		RequireCleanGate: true,
	})
	if err != nil {
		t.Fatalf("clean gate on: %v", err)
	}
	if cleanGateOff.Fingerprint == cleanGateOn.Fingerprint {
		t.Fatalf("require-clean-gate toggle did not change fingerprint: %s", cleanGateOff.Fingerprint)
	}

	audit := extractionruntime.NewSQLiteAuditStore(db)
	skipLLM := &fakeExtractionLLM{prefilterText: prefilterResponse(t, req, map[string]prefilterDecision{
		req.Episodes[0].EpisodeID: {Keep: false, RoutingHint: "skip"},
	})}
	runner = extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: skipLLM, AuditStore: audit})
	firstSkip, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:      req,
		Mode:         memorycore.ExtractionRunModeDryRun,
		Audit:        memorycore.ExtractionAuditOn,
		UsePreFilter: true,
	})
	if err != nil {
		t.Fatalf("first skip: %v", err)
	}
	secondSkip, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:      req,
		Mode:         memorycore.ExtractionRunModeDryRun,
		Audit:        memorycore.ExtractionAuditOn,
		UsePreFilter: true,
	})
	if err != nil {
		t.Fatalf("second skip: %v", err)
	}
	if firstSkip.Status != memorycore.ExtractionRunStatusSkipped || !secondSkip.SkippedByFingerprint {
		t.Fatalf("skip idempotency status=%q skipped_by_fingerprint=%v", firstSkip.Status, secondSkip.SkippedByFingerprint)
	}
}

func TestLLMRequestMetadataDoesNotContainEpisodeContent(t *testing.T) {
	ctx := context.Background()
	secret := "UNIQUE_METADATA_SECRET_2f3db0"
	svc, db := seedRuntimeDB(t, ctx, "我不喜欢早上八点开会。"+secret)
	defer svc.Close()
	defer db.Close()

	req := buildRuntimeRequest(t, ctx, db)
	llm := &fakeExtractionLLM{
		prefilterText: prefilterResponse(t, req, map[string]prefilterDecision{
			req.Episodes[0].EpisodeID: {Keep: true, RoutingHint: "extract"},
		}),
		extractText: "{",
		repairText:  validRuntimeResponse(t, req),
	}
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	result, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:       req,
		Mode:          memorycore.ExtractionRunModeDryRun,
		Audit:         memorycore.ExtractionAuditOff,
		UsePreFilter:  true,
		RepairEnabled: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !result.Repaired {
		t.Fatalf("run was not repaired")
	}
	if len(llm.requests) < 3 {
		t.Fatalf("captured request count = %d, want prefilter/extraction/repair", len(llm.requests))
	}
	for _, captured := range llm.requests {
		if _, ok := captured.Metadata["request_json"]; ok {
			t.Fatalf("metadata contains request_json for purpose %s", captured.Purpose)
		}
		for key, value := range captured.Metadata {
			if strings.Contains(value, secret) || strings.Contains(value, "早上八点") {
				t.Fatalf("metadata %s for purpose %s leaked content: %q", key, captured.Purpose, value)
			}
		}
	}
}

func TestDeterministicMockLLMParsesRequestFromUserPromptWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	svc, db := seedRuntimeDB(t, ctx, "我喜欢手冲咖啡。")
	defer svc.Close()
	defer db.Close()

	req := buildRuntimeRequest(t, ctx, db)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	llm := extractionruntime.NewDeterministicMockLLM()
	resp, err := llm.CompleteJSON(ctx, memorycore.ExtractionLLMRequest{
		Purpose:    memorycore.ExtractionLLMPurposeExtraction,
		UserPrompt: string(body),
		Metadata:   map[string]string{"request_id": req.RequestID, "prompt_version": "test"},
	})
	if err != nil {
		t.Fatalf("mock complete: %v", err)
	}
	if !strings.Contains(resp.Text, req.RequestID) || !strings.Contains(resp.Text, "用户喜欢手冲咖啡。") {
		t.Fatalf("mock response did not use UserPrompt request: %s", resp.Text)
	}
}

func TestRunBatchPropagatesBuildRequestOptions(t *testing.T) {
	ctx := context.Background()
	svc, db := seedRuntimeDB(t, ctx, "我不喜欢早上八点开会。")
	defer svc.Close()
	defer db.Close()

	llm := &capturingBatchLLM{t: t}
	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{DB: db, Service: svc, LLM: llm})
	_, err := runner.RunBatch(ctx, memorycore.ExtractionBatchRequest{
		PersonaID:                "default",
		SessionIDs:               []string{"session_seed"},
		Trigger:                  memorycore.ExtractionTriggerSessionEnd,
		Mode:                     memorycore.ExtractionRunModeDryRun,
		Audit:                    memorycore.ExtractionAuditOff,
		Timezone:                 "UTC",
		AllowSensitiveExtraction: true,
		AllowInference:           false,
		ManualPin:                true,
		ManualForget:             true,
		MaxFacts:                 3,
		MaxLinks:                 4,
		EpisodeLimit:             1,
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if len(llm.requests) != 1 {
		t.Fatalf("captured requests = %d, want 1", len(llm.requests))
	}
	req := llm.requests[0]
	if req.Timezone != "UTC" {
		t.Fatalf("timezone = %q, want UTC", req.Timezone)
	}
	if !req.Policy.AllowSensitiveExtraction || req.Policy.AllowInference || !req.Policy.ManualPin || !req.Policy.ManualForget {
		t.Fatalf("policy flags not propagated: %+v", req.Policy)
	}
	if req.Policy.MaxFacts != 3 || req.Policy.MaxLinks != 4 {
		t.Fatalf("policy limits = %d/%d, want 3/4", req.Policy.MaxFacts, req.Policy.MaxLinks)
	}
	if len(req.Episodes) != 1 {
		t.Fatalf("episode count = %d, want 1", len(req.Episodes))
	}
}

type fakeExtractionLLM struct {
	prefilterText  string
	extractText    string
	repairText     string
	prefilterCalls int
	extractCalls   int
	repairCalls    int
	requests       []memorycore.ExtractionLLMRequest
}

func (f *fakeExtractionLLM) CompleteJSON(ctx context.Context, req memorycore.ExtractionLLMRequest) (memorycore.ExtractionLLMResponse, error) {
	f.requests = append(f.requests, req)
	switch req.Purpose {
	case memorycore.ExtractionLLMPurposePreFilter:
		f.prefilterCalls++
		return memorycore.ExtractionLLMResponse{Text: f.prefilterText, Model: "fake"}, nil
	case memorycore.ExtractionLLMPurposeRepair:
		f.repairCalls++
		return memorycore.ExtractionLLMResponse{Text: f.repairText, Model: "fake"}, nil
	default:
		f.extractCalls++
		return memorycore.ExtractionLLMResponse{Text: f.extractText, Model: "fake"}, nil
	}
}

type capturingBatchLLM struct {
	t        *testing.T
	requests []memorycore.ExtractionRequest
}

func (c *capturingBatchLLM) CompleteJSON(ctx context.Context, req memorycore.ExtractionLLMRequest) (memorycore.ExtractionLLMResponse, error) {
	var extractReq memorycore.ExtractionRequest
	if err := json.Unmarshal([]byte(req.UserPrompt), &extractReq); err != nil {
		c.t.Fatalf("decode user prompt: %v", err)
	}
	c.requests = append(c.requests, extractReq)
	return memorycore.ExtractionLLMResponse{Text: validRuntimeResponse(c.t, extractReq), Model: "capture"}, nil
}

func seedRuntimeDB(t *testing.T, ctx context.Context, content string) (memorycore.Service, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{DBPath: dbPath, AutoMigrate: true, EnableFTS: false})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	if _, err := svc.StartSession(ctx, memorycore.StartSessionRequest{ID: "session_seed"}); err != nil {
		t.Fatalf("start session: %v", err)
	}
	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{ID: "ep_seed", SessionID: "session_seed", Content: content}); err != nil {
		t.Fatalf("append episode: %v", err)
	}
	if _, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{ID: "ent_user", CanonicalName: "User", EntityType: memorycore.EntityTypeUser}); err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	db.SetMaxOpenConns(1)
	return svc, db
}

func buildRuntimeRequest(t *testing.T, ctx context.Context, db *sql.DB) memorycore.ExtractionRequest {
	t.Helper()
	sessionID := "session_seed"
	req, err := extractionruntime.BuildRequest(ctx, db, extractionruntime.BuildRequestOptions{
		PersonaID:      "default",
		SessionID:      &sessionID,
		Trigger:        memorycore.ExtractionTriggerSessionEnd,
		Limit:          50,
		AllowInference: true,
		Timezone:       "Asia/Singapore",
		MaxFacts:       12,
		MaxLinks:       20,
	})
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return req
}

func validRuntimeResponse(t *testing.T, req memorycore.ExtractionRequest) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"schema_version": memorycore.ExtractionResponseSchemaVersion,
		"request_id":     req.RequestID,
		"persona_id":     req.PersonaID,
		"session_id":     req.SessionID,
		"trigger":        req.Trigger,
		"source_window": map[string]any{
			"episode_ids": []string{req.Episodes[0].EpisodeID},
			"started_at":  nil,
			"ended_at":    nil,
		},
		"entities": []any{},
		"facts": []any{map[string]any{
			"candidate_id":                "f1",
			"subject_entity_candidate_id": "user",
			"predicate":                   "dislikes",
			"object_entity_candidate_id":  nil,
			"object_literal":              "早上八点开会",
			"content_summary":             "用户不喜欢早上八点开会。",
			"fact_type":                   "stable_preference",
			"valid_from":                  nil,
			"valid_to":                    nil,
			"temporal_precision":          "unknown",
			"extraction_confidence":       "explicit",
			"extraction_confidence_score": 0.95,
			"importance":                  0.7,
			"valence":                     -0.55,
			"arousal":                     0.35,
			"sensitivity_level":           "normal",
			"source_episode_ids":          []string{req.Episodes[0].EpisodeID},
			"evidence_notes":              "用户直接表达。",
			"reasoning":                   nil,
			"operation_hint":              "insert_candidate",
			"pinned":                      false,
			"user_requested":              false,
			"searchable_hint":             true,
			"quality_decision":            "accept_for_consolidation",
			"quality_reasons":             []string{"explicit_user_statement"},
		}},
		"links":               []any{},
		"affect_events":       []any{},
		"deletion_intents":    []any{},
		"pin_intents":         []any{},
		"correction_hints":    []any{},
		"rejected_candidates": []any{},
		"quality_flags":       []any{},
		"gate_summary": map[string]any{
			"accepted_fact_count":   1,
			"needs_review_count":    0,
			"rejected_count":        0,
			"has_deletion_intent":   false,
			"has_pin_intent":        false,
			"requires_human_review": false,
			"notes":                 "ok",
		},
	})
}

func manualForgetResponse(t *testing.T, req memorycore.ExtractionRequest) string {
	t.Helper()
	return mustJSON(t, map[string]any{
		"schema_version": memorycore.ExtractionResponseSchemaVersion,
		"request_id":     req.RequestID,
		"persona_id":     req.PersonaID,
		"session_id":     req.SessionID,
		"trigger":        req.Trigger,
		"source_window":  map[string]any{"episode_ids": []string{req.Episodes[0].EpisodeID}, "started_at": nil, "ended_at": nil},
		"entities":       []any{},
		"facts":          []any{},
		"links":          []any{},
		"affect_events":  []any{},
		"deletion_intents": []any{map[string]any{
			"candidate_id":          "d1",
			"forget_level":          "soft_forget",
			"target_description":    "用户要求不要再提的内容",
			"target_node_type_hint": "fact",
			"source_episode_id":     req.Episodes[0].EpisodeID,
			"confidence":            0.9,
			"reasoning":             nil,
			"requires_confirmation": true,
		}},
		"pin_intents":         []any{},
		"correction_hints":    []any{},
		"rejected_candidates": []any{},
		"quality_flags":       []any{},
		"gate_summary":        map[string]any{"accepted_fact_count": 0, "needs_review_count": 0, "rejected_count": 0, "has_deletion_intent": true, "has_pin_intent": false, "requires_human_review": false, "notes": "route"},
	})
}

type prefilterDecision struct {
	Keep        bool
	RoutingHint string
}

func prefilterResponse(t *testing.T, req memorycore.ExtractionRequest, decisions map[string]prefilterDecision) string {
	t.Helper()
	items := make([]any, 0, len(decisions))
	for id, d := range decisions {
		items = append(items, map[string]any{
			"episode_id":   id,
			"keep":         d.Keep,
			"routing_hint": d.RoutingHint,
			"reason_codes": []string{"test"},
		})
	}
	return mustJSON(t, map[string]any{
		"schema_version": memorycore.ExtractionPreFilterSchemaVersion,
		"request_id":     req.RequestID,
		"persona_id":     req.PersonaID,
		"session_id":     req.SessionID,
		"trigger":        req.Trigger,
		"episodes":       items,
		"quality_flags":  []string{},
	})
}

func assertFactCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM facts`).Scan(&got); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if got != want {
		t.Fatalf("fact count = %d, want %d", got, want)
	}
}

func assertAuditDoesNotContain(t *testing.T, db *sql.DB, secret string) {
	t.Helper()
	rows, err := db.Query(`SELECT id, request_id, persona_id, COALESCE(session_id,''), trigger, mode, status, fingerprint, provider_id, provider_kind, COALESCE(model,''), prompt_version, prefilter_prompt_version, repair_prompt_version, COALESCE(prompt_hash,''), COALESCE(response_hash,''), COALESCE(repaired_response_hash,''), COALESCE(prefilter_hash,''), COALESCE(sanitized_error_code,''), COALESCE(sanitized_error_message,'') FROM extraction_runs`)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()
	values := make([]sql.NullString, 20)
	scan := make([]any, len(values))
	for i := range values {
		scan[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(scan...); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		for _, value := range values {
			if strings.Contains(value.String, secret) {
				t.Fatalf("audit leaked secret in %q", value.String)
			}
		}
	}
}

func responseMap(t *testing.T, body string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
}

var _ = time.Time{}
