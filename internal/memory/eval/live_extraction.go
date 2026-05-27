package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
	"github.com/longyisang/emoagent-memorycore/internal/memory/extraction"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime"
)

type LiveExtractionRunnerOptions struct {
	TempDir   string
	ReportDir string
	Provider  string
	BaseURL   string
	Model     string
	APIKeyEnv string
	RawLogDir string
	Timeout   time.Duration
	MaxTokens int
	Thinking  bool
}

type LiveExtractionSuiteReport struct {
	Suite        string                     `json:"suite"`
	Mode         string                     `json:"mode"`
	FixtureCount int                        `json:"fixture_count"`
	Passed       int                        `json:"passed"`
	Failed       int                        `json:"failed"`
	ReportDir    string                     `json:"report_dir,omitempty"`
	Cases        []LiveExtractionCaseReport `json:"cases"`
}

type LiveExtractionCaseReport struct {
	CaseID           string                      `json:"case_id"`
	Path             string                      `json:"path,omitempty"`
	Status           string                      `json:"status"`
	Error            string                      `json:"error,omitempty"`
	DBPath           string                      `json:"db_path,omitempty"`
	RawLogPaths      []string                    `json:"raw_log_paths,omitempty"`
	Provider         string                      `json:"provider,omitempty"`
	Model            string                      `json:"model,omitempty"`
	RunMode          string                      `json:"run_mode,omitempty"`
	RunStatus        string                      `json:"run_status,omitempty"`
	AcceptedCount    int                         `json:"accepted_count"`
	ReviewCount      int                         `json:"review_count"`
	RejectedCount    int                         `json:"rejected_count"`
	Checks           []LiveExtractionCheckResult `json:"checks,omitempty"`
	Facts            []LiveExtractionFactReport  `json:"facts,omitempty"`
	SanitizedError   string                      `json:"sanitized_error,omitempty"`
	SanitizedMessage string                      `json:"sanitized_message,omitempty"`
}

type LiveExtractionCheckResult struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

type LiveExtractionFactReport struct {
	CandidateID     string   `json:"candidate_id,omitempty"`
	SubjectEntityID string   `json:"subject_entity_id,omitempty"`
	Predicate       string   `json:"predicate,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	Decision        string   `json:"decision,omitempty"`
	ReasonCodes     []string `json:"reason_codes,omitempty"`
}

type LiveExtractionRunner struct {
	opts LiveExtractionRunnerOptions
}

func NewLiveExtractionRunner(opts LiveExtractionRunnerOptions) *LiveExtractionRunner {
	return &LiveExtractionRunner{opts: opts}
}

func RunLiveExtractionFiles(ctx context.Context, paths []string, opts LiveExtractionRunnerOptions) LiveExtractionSuiteReport {
	report := LiveExtractionSuiteReport{
		Suite:     "quality_extract",
		Mode:      "live",
		ReportDir: opts.ReportDir,
		Cases:     make([]LiveExtractionCaseReport, 0, len(paths)),
	}
	runner := NewLiveExtractionRunner(opts)
	for _, path := range paths {
		fixture, err := LoadFixtureFile(path)
		if err != nil {
			caseID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			report.Cases = append(report.Cases, LiveExtractionCaseReport{
				CaseID: caseID,
				Path:   path,
				Status: "fail",
				Error:  err.Error(),
			})
			continue
		}
		report.Cases = append(report.Cases, runner.Run(ctx, path, fixture))
	}
	report.FixtureCount = len(report.Cases)
	for _, item := range report.Cases {
		if item.Status == "pass" {
			report.Passed++
		} else {
			report.Failed++
		}
	}
	return report
}

func (r *LiveExtractionRunner) Run(ctx context.Context, path string, fixture *Fixture) LiveExtractionCaseReport {
	caseReport := LiveExtractionCaseReport{Path: path, Status: "fail"}
	if fixture != nil {
		caseReport.CaseID = fixture.CaseID
	}
	if fixture == nil {
		caseReport.Error = "fixture is nil"
		return caseReport
	}
	if fixture.Suite != "quality_extract" {
		caseReport.Error = fmt.Sprintf("suite=%s, want quality_extract", fixture.Suite)
		return caseReport
	}
	if fixture.LiveExtraction == nil {
		caseReport.Error = "live_extraction is required"
		return caseReport
	}
	if err := fixture.ValidateStubPolicy(FixtureStubPolicyForbid); err != nil {
		caseReport.Error = err.Error()
		return caseReport
	}

	live := fixture.LiveExtraction
	caseReport.Provider = firstLiveString(r.opts.Provider, live.Provider, "openai-compatible")
	caseReport.Model = firstLiveString(r.opts.Model, live.Model, "")
	runMode := firstLiveString(live.Mode, string(appcore.ExtractionRunModeDryRun))
	caseReport.RunMode = runMode

	tempRoot, cleanup, err := r.tempRoot()
	if err != nil {
		caseReport.Error = err.Error()
		return caseReport
	}
	defer cleanup()
	if err := os.MkdirAll(tempRoot, 0o755); err != nil {
		caseReport.Error = fmt.Sprintf("create temp dir: %v", err)
		return caseReport
	}
	dbPath := filepath.Join(tempRoot, sanitizeFileName(fixture.CaseID)+".db")
	removeSQLiteFiles(dbPath)
	caseReport.DBPath = dbPath

	svc, err := appcore.Open(ctx, appcore.Options{
		DBPath:      dbPath,
		PersonaID:   defaultPersonaID,
		AutoMigrate: true,
		EnableFTS:   true,
		Now: func() time.Time {
			return fixedNow
		},
	})
	if err != nil {
		caseReport.Error = fmt.Sprintf("open service: %v", err)
		return caseReport
	}
	defer svc.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		caseReport.Error = fmt.Sprintf("open assertion db: %v", err)
		return caseReport
	}
	defer db.Close()

	state := &runState{
		fixture:  fixture,
		service:  svc,
		db:       db,
		refs:     map[string]string{},
		steps:    map[string]stepResult{},
		persona:  defaultPersonaID,
		caseID:   fixture.CaseID,
		tempRoot: tempRoot,
		dbPath:   dbPath,
	}
	if err := state.seed(ctx); err != nil {
		caseReport.Error = err.Error()
		return caseReport
	}

	request, err := r.buildRequest(ctx, db, state, fixture)
	if err != nil {
		caseReport.Error = err.Error()
		return caseReport
	}
	llm, err := r.buildLLM(caseReport.Provider, fixture)
	if err != nil {
		caseReport.Error = err.Error()
		return caseReport
	}
	capture := &capturingExtractionLLM{inner: llm, responses: map[string]appcore.ExtractionLLMResponse{}}
	rawLog := appcore.ExtractionRawLogOptions{Enabled: live.RawLog, Directory: r.rawLogDir(fixture)}
	beforeRawLogs := map[string]struct{}{}
	if rawLog.Enabled {
		beforeRawLogs = snapshotJSONFiles(rawLog.Directory)
	}
	runtime := extractionruntime.NewRunner(extractionruntime.RunnerOptions{
		DB:         db,
		Service:    svc,
		LLM:        capture,
		AuditStore: extractionruntime.NewSQLiteAuditStore(db),
		Now: func() time.Time {
			return fixedNow
		},
	})
	result, runErr := runtime.Run(ctx, appcore.ExtractionRunRequest{
		Request:          request,
		Mode:             appcore.ExtractionRunMode(runMode),
		ProviderID:       caseReport.Provider,
		ProviderKind:     caseReport.Provider,
		Model:            caseReport.Model,
		Temperature:      live.Temperature,
		MaxTokens:        firstLiveInt(live.MaxTokens, r.opts.MaxTokens, 4096),
		Timeout:          r.timeout(live),
		UsePreFilter:     live.UsePreFilter,
		RepairEnabled:    liveRepairEnabled(live),
		RequireCleanGate: live.RequireCleanGate,
		Audit:            firstLiveString(live.Audit, appcore.ExtractionAuditOn),
		Force:            live.Force,
		RawLog:           rawLog,
		Window: appcore.ExtractionRunWindow{
			EpisodeIDs: requestEpisodeIDs(request),
			Limit:      live.Limit,
		},
	})
	caseReport.RunStatus = string(result.Status)
	caseReport.AcceptedCount = result.AcceptedCount
	caseReport.ReviewCount = result.ReviewCount
	caseReport.RejectedCount = result.RejectedCount
	caseReport.SanitizedError = result.SanitizedErrorCode
	caseReport.SanitizedMessage = result.SanitizedErrorMessage
	if rawLog.Enabled {
		caseReport.RawLogPaths = diffJSONFiles(beforeRawLogs, snapshotJSONFiles(rawLog.Directory))
	}

	responseText := capture.finalExtractionText(result.Repaired)
	if strings.TrimSpace(responseText) != "" {
		resp, parseErr := extraction.ParseResponse(strings.NewReader(responseText))
		if parseErr != nil {
			caseReport.Error = fmt.Sprintf("parse captured extraction response: %v", parseErr)
		} else if result.GateResult != nil {
			caseReport.Facts = liveExtractionFacts(resp, *result.GateResult)
			caseReport.Checks = evaluateLiveExtractionExpect(fixture.Expect, result, caseReport.Facts, responseText)
		}
	}
	if result.GateResult == nil && caseReport.Error == "" {
		caseReport.Checks = append(caseReport.Checks, failedLiveCheck("run.gate", "gate_result present", "missing"))
	}
	if runErr != nil {
		caseReport.Error = runErr.Error()
	}
	if result.Status == appcore.ExtractionRunStatusFailed ||
		result.Status == appcore.ExtractionRunStatusBlocked ||
		result.Status == appcore.ExtractionRunStatusSkipped {
		caseReport.Checks = append(caseReport.Checks, failedLiveCheck("run.status", "dry_run/validated/applied", string(result.Status)))
	}
	if caseReport.Error == "" && liveChecksPassed(caseReport.Checks) {
		caseReport.Status = "pass"
	}
	return caseReport
}

func (r *LiveExtractionRunner) tempRoot() (string, func(), error) {
	if strings.TrimSpace(r.opts.TempDir) != "" {
		return r.opts.TempDir, func() {}, nil
	}
	if strings.TrimSpace(r.opts.ReportDir) != "" {
		return filepath.Join(r.opts.ReportDir, "tmp"), func() {}, nil
	}
	dir, err := os.MkdirTemp("", "memory-eval-live-extract-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir: %w", err)
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

func (r *LiveExtractionRunner) buildRequest(ctx context.Context, db *sql.DB, state *runState, fixture *Fixture) (appcore.ExtractionRequest, error) {
	live := fixture.LiveExtraction
	var sessionID *string
	if strings.TrimSpace(live.SessionID) != "" {
		value, err := state.resolveString(live.SessionID)
		if err != nil {
			return appcore.ExtractionRequest{}, err
		}
		sessionID = &value
	}
	episodeIDs := make([]string, 0, len(live.EpisodeIDs))
	for _, episodeID := range live.EpisodeIDs {
		value, err := state.resolveString(episodeID)
		if err != nil {
			return appcore.ExtractionRequest{}, err
		}
		episodeIDs = append(episodeIDs, value)
	}
	req, err := extractionruntime.BuildRequest(ctx, db, extractionruntime.BuildRequestOptions{
		PersonaID:                firstLiveString(live.PersonaID, defaultPersonaID),
		SessionID:                sessionID,
		EpisodeIDs:               episodeIDs,
		Trigger:                  firstLiveString(live.Trigger, appcore.ExtractionTriggerSessionEnd),
		Limit:                    live.Limit,
		Timezone:                 firstLiveString(live.Timezone, "Asia/Shanghai"),
		AllowSensitiveExtraction: live.AllowSensitiveExtraction,
		AllowInference:           live.AllowInference,
		ManualPin:                live.ManualPin,
		ManualForget:             live.ManualForget,
		MaxFacts:                 firstLiveInt(live.MaxFacts, 12),
		MaxLinks:                 firstLiveInt(live.MaxLinks, 20),
		Now:                      fixedNow,
	})
	if err != nil {
		return appcore.ExtractionRequest{}, fmt.Errorf("build extraction request: %w", err)
	}
	req.RequestID = firstLiveString(live.RequestID, fixture.CaseID)
	return req, nil
}

func (r *LiveExtractionRunner) buildLLM(provider string, fixture *Fixture) (appcore.ExtractionLLM, error) {
	live := fixture.LiveExtraction
	switch provider {
	case "mock":
		return extractionruntime.NewDeterministicMockLLM(), nil
	case "openai-compatible":
		apiKeyEnv := firstLiveString(r.opts.APIKeyEnv, live.APIKeyEnv, "MEMORYCORE_LLM_API_KEY")
		if strings.TrimSpace(os.Getenv(apiKeyEnv)) == "" {
			return nil, fmt.Errorf("api key env %s is not set", apiKeyEnv)
		}
		baseURL := firstLiveString(r.opts.BaseURL, live.BaseURL, "")
		if strings.TrimSpace(baseURL) == "" {
			return nil, fmt.Errorf("base_url is required for openai-compatible live extraction")
		}
		model := firstLiveString(r.opts.Model, live.Model, "")
		if strings.TrimSpace(model) == "" {
			return nil, fmt.Errorf("model is required for openai-compatible live extraction")
		}
		return extractionruntime.NewOpenAICompatibleLLM(extractionruntime.OpenAICompatibleOptions{
			BaseURL:   baseURL,
			APIKeyEnv: apiKeyEnv,
			Model:     model,
			Timeout:   r.timeout(live),
			MaxTokens: firstLiveInt(live.MaxTokens, r.opts.MaxTokens, 4096),
			Thinking:  &extractionruntime.OpenAICompatibleThinkingOptions{Type: liveExtractionThinkingType(r.opts.Thinking)},
		}), nil
	default:
		return nil, fmt.Errorf("unsupported live extraction provider %q", provider)
	}
}

func liveExtractionThinkingType(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func (r *LiveExtractionRunner) rawLogDir(fixture *Fixture) string {
	live := fixture.LiveExtraction
	dir := firstLiveString(r.opts.RawLogDir, live.RawLogDir, "")
	if strings.TrimSpace(dir) != "" {
		return dir
	}
	if strings.TrimSpace(r.opts.ReportDir) != "" {
		return filepath.Join(r.opts.ReportDir, "raw_logs", sanitizeFileName(fixture.CaseID))
	}
	if strings.TrimSpace(r.opts.TempDir) != "" {
		return filepath.Join(r.opts.TempDir, "raw_logs", sanitizeFileName(fixture.CaseID))
	}
	return filepath.Join(os.TempDir(), "memory-eval-live-extract-raw", sanitizeFileName(fixture.CaseID))
}

func (r *LiveExtractionRunner) timeout(live *LiveExtractionConfig) time.Duration {
	if strings.TrimSpace(live.Timeout) != "" {
		if parsed, err := time.ParseDuration(live.Timeout); err == nil {
			return parsed
		}
	}
	if r.opts.Timeout > 0 {
		return r.opts.Timeout
	}
	return 60 * time.Second
}

type capturingExtractionLLM struct {
	inner     appcore.ExtractionLLM
	responses map[string]appcore.ExtractionLLMResponse
}

func (c *capturingExtractionLLM) CompleteJSON(ctx context.Context, req appcore.ExtractionLLMRequest) (appcore.ExtractionLLMResponse, error) {
	resp, err := c.inner.CompleteJSON(ctx, req)
	c.responses[req.Purpose] = resp
	return resp, err
}

func (c *capturingExtractionLLM) finalExtractionText(repaired bool) string {
	if repaired {
		if resp, ok := c.responses[appcore.ExtractionLLMPurposeRepair]; ok && strings.TrimSpace(resp.Text) != "" {
			return resp.Text
		}
	}
	if resp, ok := c.responses[appcore.ExtractionLLMPurposeExtraction]; ok {
		return resp.Text
	}
	return ""
}

func liveExtractionFacts(resp appcore.ExtractionResponse, gate appcore.ExtractionGateResult) []LiveExtractionFactReport {
	decisions := map[string]appcore.CandidateGateDecision{}
	for _, item := range gate.FactDecisions {
		decisions[item.CandidateID] = item
	}
	out := make([]LiveExtractionFactReport, 0, len(resp.Facts))
	for _, fact := range resp.Facts {
		item := LiveExtractionFactReport{
			CandidateID:     fact.CandidateID,
			SubjectEntityID: fact.SubjectEntityCandidateID,
			Predicate:       fact.Predicate,
			Summary:         fact.ContentSummary,
		}
		if decision, ok := decisions[fact.CandidateID]; ok {
			item.Decision = decision.Decision
			item.ReasonCodes = append([]string(nil), decision.ReasonCodes...)
		}
		out = append(out, item)
	}
	return out
}

func evaluateLiveExtractionExpect(expect LiveExtractionExpect, result appcore.ExtractionRunResult, facts []LiveExtractionFactReport, rawResponse string) []LiveExtractionCheckResult {
	checks := []LiveExtractionCheckResult{}
	checks = append(checks, evaluateLiveGateExpect(expect.Gate, result)...)
	for index, expected := range expect.AcceptedFacts {
		name := fmt.Sprintf("accepted_facts[%d]", index)
		if liveFactExists(facts, expected, "accept") {
			checks = append(checks, passedLiveCheck(name))
			continue
		}
		checks = append(checks, failedLiveCheck(name, liveFactExpectationString(expected), liveFactActualString(facts)))
	}
	for index, expected := range expect.ReviewOrReject {
		name := fmt.Sprintf("review_or_reject[%d]", index)
		if liveReviewOrRejectExists(facts, expected) {
			checks = append(checks, passedLiveCheck(name))
			continue
		}
		checks = append(checks, failedLiveCheck(name, liveReviewExpectationString(expected), liveFactActualString(facts)))
	}
	for _, forbidden := range expect.Forbidden {
		name := "forbidden." + forbidden
		if liveForbiddenViolated(forbidden, facts, rawResponse) {
			checks = append(checks, failedLiveCheck(name, "absent", "present"))
			continue
		}
		checks = append(checks, passedLiveCheck(name))
	}
	return checks
}

func evaluateLiveGateExpect(expect LiveExtractionGateExpect, result appcore.ExtractionRunResult) []LiveExtractionCheckResult {
	checks := []LiveExtractionCheckResult{}
	if expect.MinAcceptedFacts > 0 {
		checks = append(checks, liveCountAtLeast("gate.min_accepted_facts", expect.MinAcceptedFacts, result.AcceptedCount))
	}
	if expect.MaxAcceptedFacts != nil {
		checks = append(checks, liveCountAtMost("gate.max_accepted_facts", *expect.MaxAcceptedFacts, result.AcceptedCount))
	}
	if expect.MinReview > 0 {
		checks = append(checks, liveCountAtLeast("gate.min_review", expect.MinReview, result.ReviewCount))
	}
	if expect.MaxReview != nil {
		checks = append(checks, liveCountAtMost("gate.max_review", *expect.MaxReview, result.ReviewCount))
	}
	if expect.MinRejected > 0 {
		checks = append(checks, liveCountAtLeast("gate.min_rejected", expect.MinRejected, result.RejectedCount))
	}
	if expect.MaxRejected != nil {
		checks = append(checks, liveCountAtMost("gate.max_rejected", *expect.MaxRejected, result.RejectedCount))
	}
	return checks
}

func liveFactExists(facts []LiveExtractionFactReport, expected LiveExtractionFactExpect, decision string) bool {
	for _, fact := range facts {
		if decision != "" && fact.Decision != decision {
			continue
		}
		if strings.TrimSpace(expected.SubjectEntityID) != "" && fact.SubjectEntityID != expected.SubjectEntityID {
			continue
		}
		if !livePredicateMatches(fact.Predicate, expected) {
			continue
		}
		if !liveSummaryMatches(fact.Summary, expected.SummaryContains, expected.SummaryContainsAny) {
			continue
		}
		return true
	}
	return false
}

func liveReviewOrRejectExists(facts []LiveExtractionFactReport, expected LiveExtractionReviewExpect) bool {
	for _, fact := range facts {
		if fact.Decision == "accept" {
			continue
		}
		if len(expected.ReasonAnyOf) > 0 && !anyStringInSet(expected.ReasonAnyOf, fact.ReasonCodes) {
			continue
		}
		if !liveSummaryMatches(fact.Summary, expected.SummaryContains, expected.SummaryContainsAny) {
			continue
		}
		return true
	}
	return false
}

func livePredicateMatches(predicate string, expected LiveExtractionFactExpect) bool {
	if strings.TrimSpace(expected.Predicate) != "" && predicate != expected.Predicate {
		return false
	}
	if len(expected.PredicateAnyOf) == 0 {
		return true
	}
	for _, want := range expected.PredicateAnyOf {
		if predicate == want {
			return true
		}
	}
	return false
}

func liveSummaryMatches(summary string, contains string, containsAny []string) bool {
	if strings.TrimSpace(contains) != "" && !strings.Contains(summary, contains) {
		return false
	}
	if len(containsAny) == 0 {
		return true
	}
	for _, want := range containsAny {
		if strings.Contains(summary, want) {
			return true
		}
	}
	return false
}

func liveForbiddenViolated(forbidden string, facts []LiveExtractionFactReport, rawResponse string) bool {
	needle := strings.TrimSpace(forbidden)
	if needle == "" {
		return false
	}
	lowerRaw := strings.ToLower(rawResponse)
	switch needle {
	case "raw_prompt_leak":
		for _, marker := range []string{"system_prompt", "developer_prompt", "known_entities", "predicate_schemas", "approved_work_candidates", "MemoryCore extraction runtime"} {
			if strings.Contains(lowerRaw, strings.ToLower(marker)) {
				return true
			}
		}
		return false
	case "agent_affect_user_fact":
		for _, fact := range facts {
			if fact.Decision != "accept" {
				continue
			}
			if fact.SubjectEntityID == "agent" || anyStringInSet([]string{"agent_affect_boundary"}, fact.ReasonCodes) {
				return true
			}
			if strings.Contains(fact.Summary, "助手感到") || strings.Contains(fact.Summary, "AI感到") {
				return true
			}
		}
		return false
	case "overgeneralize_hates_spicy_food":
		for _, fact := range facts {
			if fact.Decision == "accept" && strings.Contains(fact.Summary, "辣") && (strings.Contains(fact.Summary, "讨厌") || strings.Contains(fact.Summary, "不能吃") || fact.Predicate == "hates") {
				return true
			}
		}
		return false
	case "overgeneralize_never_work_after_9pm":
		for _, fact := range facts {
			if fact.Decision == "accept" && (strings.Contains(fact.Summary, "九点") || strings.Contains(fact.Summary, "9点")) && strings.Contains(fact.Summary, "工作") && (strings.Contains(fact.Summary, "永远") || strings.Contains(fact.Summary, "从不")) {
				return true
			}
		}
		return false
	default:
		if strings.Contains(rawResponse, needle) {
			return true
		}
		for _, fact := range facts {
			if fact.Decision == "accept" && strings.Contains(fact.Summary, needle) {
				return true
			}
		}
		return false
	}
}

func liveCountAtLeast(name string, want int, got int) LiveExtractionCheckResult {
	if got >= want {
		return passedLiveCheck(name)
	}
	return failedLiveCheck(name, fmt.Sprintf(">=%d", want), fmt.Sprintf("%d", got))
}

func liveCountAtMost(name string, want int, got int) LiveExtractionCheckResult {
	if got <= want {
		return passedLiveCheck(name)
	}
	return failedLiveCheck(name, fmt.Sprintf("<=%d", want), fmt.Sprintf("%d", got))
}

func passedLiveCheck(name string) LiveExtractionCheckResult {
	return LiveExtractionCheckResult{Name: name, Status: "pass"}
}

func failedLiveCheck(name string, expected string, actual string) LiveExtractionCheckResult {
	return LiveExtractionCheckResult{Name: name, Status: "fail", Expected: expected, Actual: actual}
}

func liveChecksPassed(checks []LiveExtractionCheckResult) bool {
	for _, check := range checks {
		if check.Status != "pass" {
			return false
		}
	}
	return true
}

func liveRepairEnabled(live *LiveExtractionConfig) bool {
	if live.Repair == nil {
		return true
	}
	return *live.Repair
}

func requestEpisodeIDs(req appcore.ExtractionRequest) []string {
	out := make([]string, 0, len(req.Episodes))
	for _, episode := range req.Episodes {
		out = append(out, episode.EpisodeID)
	}
	return out
}

func snapshotJSONFiles(dir string) map[string]struct{} {
	paths := map[string]struct{}{}
	if strings.TrimSpace(dir) == "" {
		return paths
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, path := range matches {
		paths[path] = struct{}{}
	}
	return paths
}

func diffJSONFiles(before map[string]struct{}, after map[string]struct{}) []string {
	paths := []string{}
	for path := range after {
		if _, ok := before[path]; !ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func removeSQLiteFiles(dbPath string) {
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		_ = os.Remove(path)
	}
}

func WriteLiveExtractionReports(reportDir string, report LiveExtractionSuiteReport) error {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(reportDir, "report.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(reportDir, "report.md"), []byte(FormatLiveExtractionReport(report)+"\n"), 0o644)
}

func FormatLiveExtractionReport(report LiveExtractionSuiteReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "live_extraction_report\n")
	fmt.Fprintf(&b, "suite: %s\n", report.Suite)
	fmt.Fprintf(&b, "mode: %s\n", report.Mode)
	fmt.Fprintf(&b, "summary: fixtures=%d passed=%d failed=%d\n", report.FixtureCount, report.Passed, report.Failed)
	for _, item := range report.Cases {
		b.WriteString("\n")
		fmt.Fprintf(&b, "case_id: %s\n", item.CaseID)
		if item.Path != "" {
			fmt.Fprintf(&b, "path: %s\n", item.Path)
		}
		fmt.Fprintf(&b, "status: %s\n", item.Status)
		fmt.Fprintf(&b, "provider: %s\n", item.Provider)
		if item.Model != "" {
			fmt.Fprintf(&b, "model: %s\n", item.Model)
		}
		fmt.Fprintf(&b, "run_status: %s accepted=%d review=%d rejected=%d\n", item.RunStatus, item.AcceptedCount, item.ReviewCount, item.RejectedCount)
		if item.Error != "" {
			fmt.Fprintf(&b, "error: %s\n", item.Error)
		}
		if len(item.RawLogPaths) > 0 {
			b.WriteString("raw_log_paths:\n")
			for _, path := range item.RawLogPaths {
				fmt.Fprintf(&b, "  - %s\n", path)
			}
		}
		if len(item.Checks) > 0 {
			b.WriteString("checks:\n")
			for _, check := range item.Checks {
				if check.Status == "pass" {
					fmt.Fprintf(&b, "  PASS %s\n", check.Name)
					continue
				}
				fmt.Fprintf(&b, "  FAIL %s expected=%s actual=%s\n", check.Name, check.Expected, check.Actual)
			}
		}
		if len(item.Facts) > 0 {
			b.WriteString("facts:\n")
			for _, fact := range item.Facts {
				fmt.Fprintf(&b, "  %s candidate=%s subject=%s predicate=%s summary=%s", fact.Decision, fact.CandidateID, fact.SubjectEntityID, fact.Predicate, fact.Summary)
				if len(fact.ReasonCodes) > 0 {
					fmt.Fprintf(&b, " reasons=%s", strings.Join(fact.ReasonCodes, ","))
				}
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func anyStringInSet(needles []string, haystack []string) bool {
	for _, needle := range needles {
		for _, value := range haystack {
			if value == needle {
				return true
			}
		}
	}
	return false
}

func liveFactExpectationString(expected LiveExtractionFactExpect) string {
	parts := []string{}
	if expected.SubjectEntityID != "" {
		parts = append(parts, "subject="+expected.SubjectEntityID)
	}
	if expected.Predicate != "" {
		parts = append(parts, "predicate="+expected.Predicate)
	}
	if len(expected.PredicateAnyOf) > 0 {
		parts = append(parts, "predicate_any_of="+strings.Join(expected.PredicateAnyOf, ","))
	}
	if expected.SummaryContains != "" {
		parts = append(parts, "summary_contains="+expected.SummaryContains)
	}
	if len(expected.SummaryContainsAny) > 0 {
		parts = append(parts, "summary_contains_any="+strings.Join(expected.SummaryContainsAny, ","))
	}
	return strings.Join(parts, " ")
}

func liveReviewExpectationString(expected LiveExtractionReviewExpect) string {
	parts := []string{}
	if len(expected.ReasonAnyOf) > 0 {
		parts = append(parts, "reason_any_of="+strings.Join(expected.ReasonAnyOf, ","))
	}
	if expected.SummaryContains != "" {
		parts = append(parts, "summary_contains="+expected.SummaryContains)
	}
	if len(expected.SummaryContainsAny) > 0 {
		parts = append(parts, "summary_contains_any="+strings.Join(expected.SummaryContainsAny, ","))
	}
	return strings.Join(parts, " ")
}

func liveFactActualString(facts []LiveExtractionFactReport) string {
	parts := make([]string, 0, len(facts))
	for _, fact := range facts {
		parts = append(parts, fmt.Sprintf("%s:%s:%s:%s", fact.Decision, fact.SubjectEntityID, fact.Predicate, fact.Summary))
	}
	return strings.Join(parts, " | ")
}

func firstLiveString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstLiveInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
