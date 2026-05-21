package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

const matrixTestPlanVersion = "memory_eval_matrix.v0.2"

type MatrixRunnerOptions struct {
	TempDir                  string
	Profiles                 []Profile
	MirrorAdapter            memorycore.MirrorAdapter
	SidecarURL               string
	Strict                   bool
	AllowSkipMissingProvider bool
	MirrorArtifactDir        string
	ReuseMirror              string
	EmbeddingCacheMode       string
	ReportDir                string
	QueryAnalysis            memorycore.QueryAnalysisOptions
	SidecarResilience        memorycore.SidecarResilienceOptions
}

type MatrixRunner struct {
	opts MatrixRunnerOptions
}

type MatrixReport struct {
	TestPlanVersion string                `json:"test_plan_version"`
	CaseID          string                `json:"case_id"`
	Profiles        []ProfileMatrixReport `json:"profiles"`
	Deltas          map[string]float64    `json:"deltas,omitempty"`
}

type ProfileMatrixReport struct {
	Profile        Profile              `json:"profile"`
	Status         ProfileStatus        `json:"status"`
	Capability     CapabilityReport     `json:"capability"`
	Metrics        MatrixMetrics        `json:"metrics"`
	MirrorArtifact MirrorArtifactReport `json:"mirror_artifact,omitempty"`
	Error          string               `json:"error,omitempty"`
	Report         Report               `json:"-"`
}

type MatrixMetrics struct {
	RequiredHitRate                     float64            `json:"required_hit_rate"`
	CandidateRecallAt20                 float64            `json:"candidate_recall_at_20"`
	CandidateRecallAt80                 float64            `json:"candidate_recall_at_80"`
	SelectedRecallAt8                   float64            `json:"selected_recall_at_8"`
	PrecisionAt8                        float64            `json:"precision_at_8"`
	MRR                                 float64            `json:"mrr"`
	NDCGAt8                             float64            `json:"ndcg_at_8"`
	CausalChainCoverage                 float64            `json:"causal_chain_coverage"`
	ContextPrecision                    float64            `json:"context_precision"`
	ForbiddenRecallRate                 float64            `json:"forbidden_recall_rate"`
	AuthorityFilterViolationCount       int                `json:"authority_filter_violation_count"`
	SidecarDegradedCount                int                `json:"sidecar_degraded_count"`
	FallbackCount                       int                `json:"fallback_count"`
	GraphActivationUsedCount            int                `json:"graph_activation_used_count"`
	GraphRequiredButNotUsedCount        int                `json:"graph_required_but_not_used_count"`
	MirrorUsedCount                     int                `json:"mirror_used_count"`
	RerankLiveCallCount                 int                `json:"rerank_live_call_count"`
	RerankRequiredButNotUsedCount       int                `json:"rerank_required_but_not_used_count"`
	EmbeddingCacheHits                  int                `json:"embedding_cache_hits"`
	EmbeddingCacheMisses                int                `json:"embedding_cache_misses"`
	EmbeddingLiveCallCount              int                `json:"embedding_live_call_count"`
	QueryAnalysisUsedCount              int                `json:"query_analysis_used_count"`
	QueryAnalysisFallbackCount          int                `json:"query_analysis_fallback_count"`
	QueryAnalysisInvalidJSONCount       int                `json:"query_analysis_invalid_json_count"`
	QueryAnalysisValidationFailedCount  int                `json:"query_analysis_validation_failed_count"`
	RuleOnlyNoSemanticCallCount         int                `json:"rule_only_no_semantic_call_count"`
	SemanticTimeoutRuleFallbackCount    int                `json:"semantic_timeout_rule_fallback_count"`
	SemanticValidationRuleFallbackCount int                `json:"semantic_validation_failed_rule_fallback_count"`
	SemanticDriftCount                  int                `json:"semantic_drift_count"`
	ProviderTimeoutCount                int                `json:"provider_timeout_count"`
	SidecarProviderTimeoutCount         int                `json:"sidecar_provider_timeout_count"`
	ProviderBudgetExhaustedCount        int                `json:"provider_budget_exhausted_count"`
	QueryAnalysisLatencyP50             int64              `json:"query_analysis_latency_p50"`
	QueryAnalysisLatencyP95             int64              `json:"query_analysis_latency_p95"`
	SemanticLatencyP95                  int64              `json:"semantic_latency_p95"`
	FieldAccuracyTimeMode               float64            `json:"field_accuracy_time_mode"`
	FieldAccuracyMemoryAbility          float64            `json:"field_accuracy_memory_ability"`
	FieldAccuracyMemoryDomain           float64            `json:"field_accuracy_memory_domain"`
	FieldAccuracyEvidenceNeed           float64            `json:"field_accuracy_evidence_need"`
	SemanticTriggerPrecision            float64            `json:"semantic_trigger_precision"`
	SemanticTriggerRecall               float64            `json:"semantic_trigger_recall"`
	FalseSkipSemanticRate               float64            `json:"false_skip_semantic_rate"`
	UnnecessarySemanticCallRate         float64            `json:"unnecessary_semantic_call_rate"`
	SemanticModeAccuracy                float64            `json:"semantic_mode_accuracy"`
	ForgetRouteAccuracy                 float64            `json:"forget_route_accuracy"`
	TemporalCorrectnessHardFailures     int                `json:"temporal_correctness_hard_failures"`
	SemanticCallsPer1000Queries         float64            `json:"semantic_calls_per_1000_queries"`
	SemanticCostPer1000Queries          float64            `json:"semantic_cost_per_1000_queries"`
	RetrievalLatencyP95                 int64              `json:"retrieval_latency_p95"`
	PostEvalCorrectiveActionRate        float64            `json:"post_eval_corrective_action_rate"`
	RedundancyRate                      float64            `json:"redundancy_rate"`
	RestraintViolationRate              float64            `json:"restraint_violation_rate"`
	EnglishRewriteCount                 int                `json:"english_rewrite_count"`
	DroppedRewriteCount                 int                `json:"dropped_rewrite_count"`
	SemanticRewriteDenseCount           int                `json:"semantic_rewrite_dense_count"`
	CandidateQueryCount                 int                `json:"candidate_query_count"`
	QueryTrimCount                      int                `json:"query_trim_count"`
	RawExactSurvivalCount               int                `json:"raw_exact_survival_count"`
	PastEventDirectFactCount            int                `json:"past_event_direct_fact_count"`
	HistoricalTransitionCount           int                `json:"historical_transition_count"`
	ProvenanceSourceCount               int                `json:"provenance_source_count"`
	EventBundleCompletionCount          int                `json:"event_bundle_completion_count"`
	SupersedesCompletionCount           int                `json:"supersedes_completion_count"`
	ProvenanceCompletionCount           int                `json:"provenance_completion_count"`
	CounterexampleExpansionCount        int                `json:"counterexample_expansion_count"`
	ReflectionSummaryBoostCount         int                `json:"reflection_summary_boost_count"`
	RerankSkippedCount                  int                `json:"rerank_skipped_count"`
	LLMSavedCallCount                   int                `json:"llm_saved_call_count"`
	AvgQueryAnalysisWaitMS              int64              `json:"avg_query_analysis_wait_ms"`
	StubUsedCount                       int                `json:"stub_used_count"`
	ForbiddenSelectedCount              int                `json:"forbidden_selected_count"`
	P50LatencyMs                        int64              `json:"p50_latency_ms"`
	P95LatencyMs                        int64              `json:"p95_latency_ms"`
	MirrorManifestHash                  string             `json:"mirror_manifest_hash,omitempty"`
	CandidateRecallBySource             map[string]float64 `json:"candidate_recall_by_source,omitempty"`
	SelectedRecallBySource              map[string]float64 `json:"selected_recall_by_source,omitempty"`
}

func NewMatrixRunner(opts MatrixRunnerOptions) *MatrixRunner {
	return &MatrixRunner{opts: opts}
}

func (r *MatrixRunner) Run(ctx context.Context, fixture *Fixture) MatrixReport {
	report := MatrixReport{TestPlanVersion: matrixTestPlanVersion}
	if fixture != nil {
		report.CaseID = fixture.CaseID
	}
	r.ensureQueryAnalysisCache()
	profiles := r.opts.Profiles
	if len(profiles) == 0 {
		profiles = []Profile{ProfileSQLiteGo}
	}
	for _, profile := range profiles {
		report.Profiles = append(report.Profiles, r.runProfile(ctx, fixture, profile))
	}
	report.Deltas = profileDeltas(report.Profiles)
	if strings.TrimSpace(r.opts.ReportDir) != "" {
		_ = os.MkdirAll(r.opts.ReportDir, 0o755)
		_ = writeJSONFile(filepath.Join(r.opts.ReportDir, "report.json"), report)
		_ = writeJSONFile(filepath.Join(r.opts.ReportDir, "query_analysis.json"), BuildQueryAnalysisReport(fixture, report))
		_ = os.WriteFile(filepath.Join(r.opts.ReportDir, "report.md"), []byte(FormatMatrixReport(report)+"\n"), 0o644)
		_ = os.WriteFile(filepath.Join(r.opts.ReportDir, "detail.md"), []byte(FormatMatrixDetailReport(fixture, report)+"\n"), 0o644)
	}
	return report
}

func (r *MatrixRunner) ensureQueryAnalysisCache() {
	if r.opts.QueryAnalysis.Cache != nil {
		return
	}
	switch r.opts.QueryAnalysis.Mode {
	case memorycore.QueryAnalysisModeSemanticAlways, memorycore.QueryAnalysisModeSemanticOnLowConfidence, memorycore.QueryAnalysisModeSemanticRewriteOnly:
		r.opts.QueryAnalysis.Cache = memorycore.NewQueryAnalysisCache()
		return
	}
	for _, profile := range r.opts.Profiles {
		if profile.UsesSemanticQueryAnalysis() {
			r.opts.QueryAnalysis.Cache = memorycore.NewQueryAnalysisCache()
			return
		}
	}
}

func (r *MatrixRunner) runProfile(ctx context.Context, fixture *Fixture, profile Profile) ProfileMatrixReport {
	capability, adapter := r.capability(ctx, profile, fixture)
	out := ProfileMatrixReport{
		Profile:    profile,
		Status:     ProfileStatusPass,
		Capability: capability,
	}
	if capability.Status == CapabilityMissing {
		if r.opts.AllowSkipMissingProvider {
			out.Status = ProfileStatusSkip
			return out
		}
		out.Status = ProfileStatusFail
		out.Error = capability.Reason
		return out
	}
	tempDir := r.opts.TempDir
	if strings.TrimSpace(tempDir) != "" {
		tempDir = filepath.Join(tempDir, sanitizeFileName(string(profile)))
	}
	var artifact *MirrorArtifactManager
	if profile.UsesMirror() && shouldUseMirrorArtifact(r.opts.MirrorArtifactDir, adapter) {
		artifact = &MirrorArtifactManager{
			RootDir:                  r.opts.MirrorArtifactDir,
			ReuseMode:                r.opts.ReuseMirror,
			SearchableTextVersion:    defaultSearchableTextVersion,
			TextNormalizationVersion: defaultTextNormalizationVersion,
			EmbeddingCacheMode:       NormalizeEmbeddingCacheMode(r.opts.EmbeddingCacheMode),
			EmbeddingCacheDBPath:     r.embeddingCacheDBPath(tempDir),
		}
	}
	runReport := NewRunner(RunnerOptions{
		TempDir:            tempDir,
		Profile:            profile,
		MirrorAdapter:      adapter,
		MirrorArtifact:     artifact,
		Strict:             r.opts.Strict,
		EmbeddingCacheMode: r.opts.EmbeddingCacheMode,
		QueryAnalysis:      r.queryAnalysisForProfile(profile),
		SidecarResilience:  r.opts.SidecarResilience,
	}).Run(ctx, fixture)
	out.Report = runReport
	out.MirrorArtifact = runReport.MirrorArtifact
	out.Metrics = ComputeProfileMatrixMetrics(fixture, runReport, profile)
	if artifact != nil {
		if out.MirrorArtifact.ManifestHash != "" {
			out.Metrics.MirrorManifestHash = out.MirrorArtifact.ManifestHash
		} else if hash := latestManifestHash(r.opts.MirrorArtifactDir, fixture, defaultMirrorEmbeddingIdentity()); hash != "" {
			out.Metrics.MirrorManifestHash = hash
		}
	}
	if r.opts.Strict {
		if reason := profileHardMetricFailureReason(profile, out.Metrics); reason != "" {
			out.Status = ProfileStatusFail
			out.Error = appendProfileError(out.Error, reason)
		}
	}
	if runReport.Failed() {
		out.Status = ProfileStatusFail
		out.Error = appendProfileError(out.Error, runReport.Error())
	}
	return out
}

func (r *MatrixRunner) queryAnalysisForProfile(profile Profile) memorycore.QueryAnalysisOptions {
	if !profile.UsesMirror() {
		if localQueryAnalysisMode(r.opts.QueryAnalysis.Mode) {
			return r.opts.QueryAnalysis
		}
		return memorycore.QueryAnalysisOptions{}
	}
	switch profile {
	case ProfileRuleOnlyRaw, ProfileRerankOff, ProfileRerankSelective:
		return memorycore.QueryAnalysisOptions{}
	case ProfileSemanticParseOnly:
		options := r.semanticProfileQueryAnalysisOptions(memorycore.QueryAnalysisModeSemanticAlways)
		options.DisableGeneratedDense = true
		return options
	case ProfileSemanticRewriteOnly:
		return r.semanticProfileQueryAnalysisOptions(memorycore.QueryAnalysisModeSemanticRewriteOnly)
	case ProfileSemanticFullCurrent:
		return r.semanticProfileQueryAnalysisOptions(memorycore.QueryAnalysisModeSemanticAlways)
	case ProfileSemanticFullSoftGated:
		return r.semanticProfileQueryAnalysisOptions(memorycore.QueryAnalysisModeSemanticOnLowConfidence)
	case ProfileSoftRoutingEnabled:
		return r.semanticProfileQueryAnalysisOptions(memorycore.QueryAnalysisModeSemanticOnLowConfidence)
	}
	return r.opts.QueryAnalysis
}

func localQueryAnalysisMode(mode memorycore.QueryAnalysisMode) bool {
	switch mode {
	case memorycore.QueryAnalysisModeLegacyOnly, memorycore.QueryAnalysisModeShadowAdaptive:
		return true
	default:
		return false
	}
}

func (r *MatrixRunner) semanticProfileQueryAnalysisOptions(mode memorycore.QueryAnalysisMode) memorycore.QueryAnalysisOptions {
	options := r.opts.QueryAnalysis
	if options.Provider == "" {
		options.Provider = memorycore.QueryAnalysisProviderSidecar
	}
	if strings.TrimSpace(options.SidecarURL) == "" {
		options.SidecarURL = strings.TrimSpace(r.opts.SidecarURL)
	}
	if options.Timeout <= 0 {
		options.Timeout = queryAnalysisTimeoutFromEnv()
	}
	if options.Timeout <= 0 {
		options.Timeout = 1500 * time.Millisecond
	}
	if options.SoftJoinTimeout <= 0 {
		options.SoftJoinTimeout = queryAnalysisSoftJoinTimeoutFromEnv()
	}
	options.Mode = mode
	if options.Provider != memorycore.QueryAnalysisProviderSidecar || strings.TrimSpace(options.SidecarURL) == "" {
		return memorycore.QueryAnalysisOptions{}
	}
	return options
}

func queryAnalysisTimeoutFromEnv() time.Duration {
	if timeout := positiveDurationEnv("MEMORYCORE_QUERY_ANALYSIS_TIMEOUT_MS", time.Millisecond); timeout > 0 {
		return timeout
	}
	return positiveDurationEnv("MEMORYCORE_QUERY_ANALYSIS_TIMEOUT_SECONDS", time.Second)
}

func queryAnalysisSoftJoinTimeoutFromEnv() time.Duration {
	return positiveDurationEnv("MEMORYCORE_QUERY_ANALYSIS_SOFT_JOIN_TIMEOUT_MS", time.Millisecond)
}

func positiveDurationEnv(name string, unit time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return time.Duration(parsed) * unit
}

func shouldUseMirrorArtifact(root string, adapter memorycore.MirrorAdapter) bool {
	if strings.TrimSpace(root) != "" {
		return true
	}
	switch typed := adapter.(type) {
	case denseOnlyAdapter:
		return typed.configurator != nil
	case graphOnlyAdapter:
		return typed.configurator != nil
	case graphRerankAdapter:
		return typed.configurator != nil
	case rerankNoGraphAdapter:
		return typed.configurator != nil
	}
	_, ok := adapter.(memorycore.MirrorEvalConfigurator)
	return ok
}

func (r *MatrixRunner) embeddingCacheDBPath(tempDir string) string {
	if strings.TrimSpace(r.opts.MirrorArtifactDir) != "" {
		root := filepath.Dir(r.opts.MirrorArtifactDir)
		return filepath.Join(root, "embedding-cache", "embeddings.sqlite3")
	}
	if strings.TrimSpace(tempDir) != "" {
		return filepath.Join(tempDir, "embedding-cache", "embeddings.sqlite3")
	}
	return ""
}

func (r *MatrixRunner) capability(ctx context.Context, profile Profile, fixture *Fixture) (CapabilityReport, memorycore.MirrorAdapter) {
	req := profile.Requirements()
	adapter := r.opts.MirrorAdapter
	if adapter == nil && strings.TrimSpace(r.opts.SidecarURL) != "" {
		adapter = memorycore.NewSidecarMirrorAdapter(r.opts.SidecarURL)
	}
	var configuredEval *memorycore.MirrorEvalConfigResult
	cacheMode := NormalizeEmbeddingCacheMode(r.opts.EmbeddingCacheMode)
	report := CapabilityReport{
		Profile:                    profile,
		QualityMode:                fixture != nil && fixture.QualityMode,
		AllowStub:                  fixture != nil && fixture.AllowStub,
		RequiresSidecar:            req.RequiresSidecar,
		RequiresEmbedding:          req.RequiresEmbedding,
		RequiresMirror:             req.RequiresMirror,
		RequiresGraphActivation:    req.RequiresGraphActivation,
		RequiresRerankProvider:     req.RequiresRerankProvider,
		SidecarAvailable:           !req.RequiresSidecar,
		EmbeddingProviderAvailable: !req.RequiresEmbedding,
		MirrorReady:                !req.RequiresMirror,
		EmbeddingCacheMode:         cacheMode,
		Status:                     CapabilityReady,
		CountsAsPass:               true,
		IncludedInQualityMetrics:   true,
	}
	if err := ValidateEmbeddingCacheMode(cacheMode); err != nil {
		return missingCapability(report, err.Error()), nil
	}
	if req.RequiresSidecar {
		if adapter == nil {
			return missingCapability(report, "sidecar profile requires --sidecar-url or MirrorAdapter"), nil
		}
		if health, ok := adapter.(memorycore.MirrorHealthChecker); ok && health != nil {
			if err := health.Health(ctx); err != nil {
				report.SidecarAvailable = false
				return missingCapability(report, fmt.Sprintf("sidecar health preflight failed: %v", err)), nil
			}
		}
		report.SidecarAvailable = true
	}
	if req.RequiresMirror {
		if adapter == nil {
			return missingCapability(report, "mirror profile requires --sidecar-url or MirrorAdapter"), nil
		}
		if _, ok := adapter.(memorycore.MirrorNamespaceAdapter); !ok {
			return missingCapability(report, "mirror profile requires ClearNamespace support"), nil
		}
		if _, ok := adapter.(memorycore.MirrorCandidateAdapter); !ok {
			return missingCapability(report, "mirror profile requires candidate support"), nil
		}
	}
	if req.RequiresEmbedding || req.RequiresMirror {
		if configurator, ok := adapter.(memorycore.MirrorEvalConfigurator); ok && configurator != nil {
			configured, err := configurator.ConfigureEval(ctx, r.preflightEvalConfig(profile, fixture))
			if err != nil {
				report.EmbeddingProviderAvailable = false
				report.MirrorReady = false
				return missingCapability(report, fmt.Sprintf("sidecar eval configure preflight failed: %v", err)), nil
			}
			configuredEval = configured
			if req.RequiresEmbedding {
				report.EmbeddingProviderAvailable = configured != nil
			}
			if req.RequiresMirror {
				report.MirrorReady = configured != nil
			}
		} else {
			report.EmbeddingProviderAvailable = !req.RequiresEmbedding || adapter != nil
			report.MirrorReady = !req.RequiresMirror || adapter != nil
		}
	}
	if req.RequiresGraphActivation {
		if _, ok := adapter.(memorycore.MirrorActivationAdapter); !ok {
			return missingCapability(report, "graph profile requires activation support"), nil
		}
		report.GraphActivationAvailable = true
	}
	if req.RequiresRerankProvider {
		if _, ok := adapter.(memorycore.MirrorRerankAdapter); !ok {
			return missingCapability(report, "rerank profile requires rerank support"), nil
		}
		if rerankCapabilityReported(configuredEval) {
			report.RerankProviderAvailable = configuredEval.RerankProviderAvailable
			report.RerankProviderMode = configuredEval.RerankProviderMode
			report.RerankCache = configuredEval.RerankCache
			if !configuredEval.RerankProviderAvailable || configuredEval.RerankProviderMode != "live" {
				return missingCapability(report, rerankCapabilityMissingReason(configuredEval)), nil
			}
		} else {
			report.RerankProviderAvailable = true
			report.RerankProviderMode = "live"
			report.RerankCache = false
		}
	}
	return report, profileAdapter(profile, adapter)
}

func rerankCapabilityReported(configured *memorycore.MirrorEvalConfigResult) bool {
	if configured == nil {
		return false
	}
	return configured.RerankProviderAvailable ||
		strings.TrimSpace(configured.RerankProviderMode) != "" ||
		strings.TrimSpace(configured.RerankCapabilityReason) != ""
}

func rerankCapabilityMissingReason(configured *memorycore.MirrorEvalConfigResult) string {
	if configured == nil {
		return "rerank provider requested but eval capability was not reported"
	}
	if reason := strings.TrimSpace(configured.RerankCapabilityReason); reason != "" {
		return "rerank provider requested but " + reason
	}
	switch strings.TrimSpace(configured.RerankProviderMode) {
	case "none":
		return "rerank provider requested but MEMORYCORE_RERANK_PROVIDER is none"
	case "missing_api_key":
		return "rerank provider requested but rerank API key is missing"
	case "":
		return "rerank provider requested but live provider capability was not reported"
	default:
		return fmt.Sprintf("rerank provider requested but provider mode is %s", configured.RerankProviderMode)
	}
}

func (r *MatrixRunner) preflightEvalConfig(profile Profile, fixture *Fixture) memorycore.MirrorEvalConfigRequest {
	tempDir := r.opts.TempDir
	if strings.TrimSpace(tempDir) != "" {
		tempDir = filepath.Join(tempDir, sanitizeFileName(string(profile)))
	}
	triviumDir := ""
	if strings.TrimSpace(r.opts.MirrorArtifactDir) != "" && fixture != nil {
		triviumDir = filepath.Join(r.opts.MirrorArtifactDir, "_preflight", fixture.StableHash(), sanitizeFileName(string(profile)), "trivium")
	} else if strings.TrimSpace(tempDir) != "" {
		triviumDir = filepath.Join(tempDir, "_preflight", "trivium")
	}
	return memorycore.MirrorEvalConfigRequest{
		TriviumDir:               triviumDir,
		EmbeddingCacheMode:       NormalizeEmbeddingCacheMode(r.opts.EmbeddingCacheMode),
		EmbeddingCacheDBPath:     r.embeddingCacheDBPath(tempDir),
		SearchableTextVersion:    defaultSearchableTextVersion,
		TextNormalizationVersion: defaultTextNormalizationVersion,
	}
}

func missingCapability(report CapabilityReport, reason string) CapabilityReport {
	report.Status = CapabilityMissing
	report.Reason = reason
	report.SidecarAvailable = report.SidecarAvailable && !strings.Contains(reason, "sidecar health")
	report.CountsAsPass = false
	report.IncludedInQualityMetrics = false
	return report
}

func appendProfileError(existing string, next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return existing
	}
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return next
	}
	return existing + "\n" + next
}

func (r MatrixReport) Failed() bool {
	for _, profile := range r.Profiles {
		if profile.Status == ProfileStatusFail {
			return true
		}
	}
	return false
}

func (r MatrixReport) Error() string {
	var parts []string
	for _, profile := range r.Profiles {
		if profile.Status == ProfileStatusFail {
			if profile.Error != "" {
				parts = append(parts, fmt.Sprintf("profile=%s error=%s", profile.Profile, profile.Error))
				continue
			}
			parts = append(parts, fmt.Sprintf("profile=%s failed", profile.Profile))
		}
	}
	return strings.Join(parts, "\n")
}

func ComputeMatrixMetrics(fixture *Fixture, report Report) MatrixMetrics {
	return computeMatrixMetrics(fixture, report, "")
}

func ComputeProfileMatrixMetrics(fixture *Fixture, report Report, profile Profile) MatrixMetrics {
	return computeMatrixMetrics(fixture, report, profile)
}

func profileHardMetricFailureReason(profile Profile, metrics MatrixMetrics) string {
	var failures []string
	add := func(name string, value any) {
		failures = append(failures, fmt.Sprintf("%s=%v", name, value))
	}
	if metrics.StubUsedCount > 0 {
		add("stub_used_count", metrics.StubUsedCount)
	}
	if metrics.ForbiddenRecallRate > 0 {
		add("forbidden_recall_rate", metrics.ForbiddenRecallRate)
	}
	if metrics.AuthorityFilterViolationCount > 0 {
		add("authority_filter_violation_count", metrics.AuthorityFilterViolationCount)
	}
	if metrics.ForbiddenSelectedCount > 0 {
		add("forbidden_selected_count", metrics.ForbiddenSelectedCount)
	}
	if metrics.TemporalCorrectnessHardFailures > 0 {
		add("temporal_correctness_hard_failures", metrics.TemporalCorrectnessHardFailures)
	}
	if profile.Requirements().RequiresMirror && metrics.FallbackCount > 0 {
		add("fallback_count", metrics.FallbackCount)
	}
	if metrics.GraphRequiredButNotUsedCount > 0 {
		add("graph_required_but_not_used_count", metrics.GraphRequiredButNotUsedCount)
	}
	if metrics.RerankRequiredButNotUsedCount > 0 {
		add("rerank_required_but_not_used_count", metrics.RerankRequiredButNotUsedCount)
	}
	if len(failures) == 0 {
		return ""
	}
	return "strict quality hard metric failure: " + strings.Join(failures, ", ")
}

func computeMatrixMetrics(fixture *Fixture, report Report, profile Profile) MatrixMetrics {
	metrics := MatrixMetrics{}
	requirements := profile.Requirements()
	var queryAnalysisLatencies []int64
	var queryAnalysisLatencySum int64
	var queryAnalysisLatencyCount int64
	var retrievalCount int
	var correctiveActionCount int
	if fixture != nil && fixture.UsesEvalStub() {
		metrics.StubUsedCount = 1
	}
	metricsFromAssertions(fixture, report, &metrics)
	for _, step := range report.Steps {
		retrieval := step.Retrieval
		if retrieval == nil {
			continue
		}
		retrievalCount++
		if retrieval.Mirror != nil {
			metrics.EmbeddingCacheHits += retrieval.Mirror.EmbeddingCacheHits
			metrics.EmbeddingCacheMisses += retrieval.Mirror.EmbeddingCacheMisses
			metrics.EmbeddingLiveCallCount += retrieval.Mirror.EmbeddingLiveCallCount
			metrics.CandidateQueryCount += retrieval.Mirror.QueryCount
			metrics.QueryTrimCount += retrieval.Mirror.QueryTrimCount
			metrics.RawExactSurvivalCount += countRawExactSurvival(retrieval.Mirror.Candidates)
			if retrieval.Mirror.RewriteQueryCount > 0 {
				metrics.SemanticRewriteDenseCount += retrieval.Mirror.RewriteQueryCount
			} else {
				metrics.SemanticRewriteDenseCount += countSemanticRewriteDenseCandidates(retrieval.Mirror.Candidates)
			}
			if retrieval.Mirror.Status == "used" {
				metrics.MirrorUsedCount++
			}
			if requirements.RequiresMirror && retrieval.Mirror.Degraded {
				metrics.SidecarDegradedCount++
				metrics.FallbackCount++
			}
			if requirements.RequiresMirror && isFallbackStatus(retrieval.Mirror.Status) {
				metrics.FallbackCount++
			}
			collectProviderFallbackMetrics(retrieval.Mirror.FallbackReason, &metrics)
			if retrieval.Mirror.LatencyMs > metrics.P95LatencyMs {
				metrics.P95LatencyMs = retrieval.Mirror.LatencyMs
			}
		}
		if retrieval.QueryAnalysis != nil {
			collectQueryAnalysisMetrics(retrieval.QueryAnalysis, &metrics, &queryAnalysisLatencies)
			if retrieval.QueryAnalysis.Diagnostics != nil && retrieval.QueryAnalysis.Diagnostics.SemanticLatencyMs > 0 {
				queryAnalysisLatencySum += retrieval.QueryAnalysis.Diagnostics.SemanticLatencyMs
				queryAnalysisLatencyCount++
			}
		}
		if retrieval.RetrievalConfidence != nil && retrieval.RetrievalConfidence.HardFailureReason == memorycore.RetrievalHardFailureTemporalInconsistency {
			metrics.TemporalCorrectnessHardFailures++
		}
		if retrieval.RetrievalConfidence != nil && retrieval.RetrievalConfidence.CorrectiveAction != "" {
			correctiveActionCount++
		}
		if retrieval.GraphActivation != nil {
			if retrieval.GraphActivation.Status == "used" {
				metrics.GraphActivationUsedCount++
			} else if requirements.RequiresGraphActivation && isFallbackStatus(retrieval.GraphActivation.Status) {
				metrics.GraphRequiredButNotUsedCount++
				metrics.FallbackCount++
			}
			if requirements.RequiresGraphActivation && retrieval.GraphActivation.Degraded {
				metrics.SidecarDegradedCount++
			}
			collectProviderFallbackMetrics(retrieval.GraphActivation.FallbackReason, &metrics)
		}
		if retrieval.Rerank != nil {
			if retrieval.Rerank.Status == "used" {
				metrics.RerankLiveCallCount++
			} else if retrieval.Rerank.Status == "skipped" {
				metrics.RerankSkippedCount++
			} else if requirements.RequiresRerankProvider && isFallbackStatus(retrieval.Rerank.Status) {
				metrics.RerankRequiredButNotUsedCount++
				metrics.FallbackCount++
			}
			if requirements.RequiresRerankProvider && retrieval.Rerank.Degraded {
				metrics.SidecarDegradedCount++
			}
			collectProviderFallbackMetrics(retrieval.Rerank.FallbackReason, &metrics)
		}
		collectScoreBreakdownMetrics(step.ScoreBreakdowns, &metrics)
	}
	metrics.QueryAnalysisLatencyP50 = percentileInt64(queryAnalysisLatencies, 50)
	metrics.QueryAnalysisLatencyP95 = percentileInt64(queryAnalysisLatencies, 95)
	metrics.SemanticLatencyP95 = metrics.QueryAnalysisLatencyP95
	metrics.RetrievalLatencyP95 = metrics.P95LatencyMs
	if queryAnalysisLatencyCount > 0 {
		metrics.AvgQueryAnalysisWaitMS = queryAnalysisLatencySum / queryAnalysisLatencyCount
	}
	if retrievalCount > 0 {
		semanticCalls := metrics.QueryAnalysisUsedCount + metrics.QueryAnalysisFallbackCount
		metrics.SemanticCallsPer1000Queries = float64(semanticCalls) * 1000 / float64(retrievalCount)
		metrics.PostEvalCorrectiveActionRate = float64(correctiveActionCount) / float64(retrievalCount)
	}
	return metrics
}

func collectQueryAnalysisMetrics(analysis *memorycore.QueryAnalysis, metrics *MatrixMetrics, latencies *[]int64) {
	if analysis == nil || metrics == nil {
		return
	}
	switch analysis.Source {
	case memorycore.QueryAnalysisSourceMerged:
		metrics.QueryAnalysisUsedCount++
	case memorycore.QueryAnalysisSourceSemanticFallback:
		metrics.QueryAnalysisFallbackCount++
	case memorycore.QueryAnalysisSourceRuleOnly:
		metrics.RuleOnlyNoSemanticCallCount++
		metrics.LLMSavedCallCount++
	}
	collectQuerySignalMetrics(analysis.Signals, metrics)
	if analysis.Diagnostics == nil {
		return
	}
	diagnostics := analysis.Diagnostics
	switch diagnostics.FallbackReason {
	case "invalid_json":
		metrics.QueryAnalysisInvalidJSONCount++
	case "validation_failed":
		metrics.QueryAnalysisValidationFailedCount++
		metrics.SemanticValidationRuleFallbackCount++
	case "semantic_timeout", "semantic_soft_timeout":
		metrics.SemanticTimeoutRuleFallbackCount++
	case "provider_timeout":
		metrics.ProviderTimeoutCount++
		metrics.SemanticTimeoutRuleFallbackCount++
	case "sidecar_provider_timeout":
		metrics.SidecarProviderTimeoutCount++
		metrics.SemanticTimeoutRuleFallbackCount++
	case "provider_budget_exhausted":
		metrics.ProviderBudgetExhaustedCount++
	}
	if diagnostics.SemanticLatencyMs > 0 && latencies != nil {
		*latencies = append(*latencies, diagnostics.SemanticLatencyMs)
	}
	metrics.EnglishRewriteCount += diagnostics.EnglishRewriteCount
	metrics.DroppedRewriteCount += diagnostics.DroppedRewriteCount
	metrics.SemanticDriftCount += diagnostics.SemanticDriftCount
}

func collectQuerySignalMetrics(signals []memorycore.QuerySignal, metrics *MatrixMetrics) {
	if metrics == nil {
		return
	}
	for _, signal := range signals {
		switch signal {
		case memorycore.QuerySignalPastEventDirectFact:
			metrics.PastEventDirectFactCount++
		case memorycore.QuerySignalStateTransition:
			metrics.HistoricalTransitionCount++
		case memorycore.QuerySignalProvenanceSource:
			metrics.ProvenanceSourceCount++
		}
	}
}

func collectScoreBreakdownMetrics(values []RetrievalScoreBreakdownReport, metrics *MatrixMetrics) {
	if metrics == nil {
		return
	}
	for _, value := range values {
		switch strings.TrimSpace(value.CompletionSource) {
		case "event_bundle":
			metrics.EventBundleCompletionCount++
		case "historical_transition":
			metrics.SupersedesCompletionCount++
		case "provenance_source":
			metrics.ProvenanceCompletionCount++
		case "premise_counterexample":
			metrics.CounterexampleExpansionCount++
		}
		if value.ReflectionBoost > 0 {
			metrics.ReflectionSummaryBoostCount++
		}
	}
}

func collectProviderFallbackMetrics(reason string, metrics *MatrixMetrics) {
	if metrics == nil {
		return
	}
	switch strings.TrimSpace(reason) {
	case "sidecar_provider_timeout":
		metrics.SidecarProviderTimeoutCount++
	case "provider_budget_exhausted":
		metrics.ProviderBudgetExhaustedCount++
	}
}

func countRawExactSurvival(candidates []memorycore.MirrorCandidateDiagnostics) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.DropReason != "" {
			continue
		}
		switch candidate.Source {
		case "raw_exact", "sqlite_fts", "sqlite_sparse", "raw_dense":
			count++
		}
	}
	return count
}

func countSemanticRewriteDenseCandidates(candidates []memorycore.MirrorCandidateDiagnostics) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.Source == "semantic_rewrite_dense" {
			count++
		}
	}
	return count
}

func percentileInt64(values []int64, percentile int) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 100 {
		return sorted[len(sorted)-1]
	}
	index := int(math.Ceil(float64(percentile)/100*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func metricsFromAssertions(fixture *Fixture, report Report, metrics *MatrixMetrics) {
	if fixture == nil || metrics == nil {
		return
	}
	resultByStep := map[string]*memorycore.MemoryContext{}
	for _, step := range report.Steps {
		resultByStep[step.ID] = step.Retrieval
	}
	var recallSum, candidateRecall20Sum, candidateRecall80Sum, precisionSum, selectedPrecisionSum, mrrSum, ndcgSum, contextPrecisionSum float64
	var recallCount, candidateRecallCount, precisionCount, selectedPrecisionCount, chainCount, chainPass, forbiddenAssertions int
	var timeModeCount, timeModeCorrect, memoryAbilityCount, memoryAbilityCorrect int
	var memoryDomainCount, memoryDomainCorrect, evidenceNeedCount, evidenceNeedCorrect int
	var semanticTP, semanticFP, semanticTN, semanticFN int
	var semanticModeCount, semanticModeCorrect int
	var forgetRouteCount, forgetRouteCorrect int
	candidateRecallBySource := map[string]float64{}
	selectedRecallBySource := map[string]float64{}
	recallCountBySource := map[string]int{}
	for _, assertion := range fixture.Assertions {
		retrieval := resultByStep[assertion.Step]
		switch assertion.Type {
		case "selected_recall_at_k":
			selected := flattenSelectedNodeIDs(retrieval)
			candidates := flattenCandidateNodeIDs(retrieval)
			relevant := cleanAssertionRefs(assertion.RelevantNodeIDs)
			recall := recallAtK(selected, relevant, assertion.At)
			recallSum += recall
			recallCount++
			candidateRecall20Sum += recallAtK(candidates, relevant, 20)
			candidateRecall80Sum += recallAtK(candidates, relevant, 80)
			candidateRecallCount++
			selectedPrecisionSum += precisionAtK(selected, relevant, assertion.At)
			selectedPrecisionCount++
			mrrSum += meanReciprocalRank(selected, relevant, assertion.At)
			ndcgSum += ndcgAtK(selected, relevant, assertion.At)
			addSourceLevelRecall(candidateRecallBySource, selectedRecallBySource, recallCountBySource, retrieval, selected, relevant, assertion.At)
		case "query_analysis":
			analysis := queryAnalysisForMetrics(retrieval)
			if assertion.TimeMode != "" {
				timeModeCount++
				if analysis != nil && string(analysis.TimeMode) == assertion.TimeMode {
					timeModeCorrect++
				}
			}
			if assertion.MemoryAbility != "" {
				memoryAbilityCount++
				if analysis != nil && string(analysis.MemoryAbility) == assertion.MemoryAbility {
					memoryAbilityCorrect++
				}
			}
			if assertion.MemoryDomain != "" {
				memoryDomainCount++
				if analysis != nil && string(analysis.MemoryDomain) == assertion.MemoryDomain {
					memoryDomainCorrect++
				}
			}
			if assertion.EvidenceNeed != "" {
				evidenceNeedCount++
				if analysis != nil && string(analysis.EvidenceNeed) == assertion.EvidenceNeed {
					evidenceNeedCorrect++
				}
			}
			if assertion.ShouldUseSemantic != nil {
				actualUse := effectiveQueryAnalysisDecision(analysis).UseSemantic
				switch {
				case *assertion.ShouldUseSemantic && actualUse:
					semanticTP++
				case *assertion.ShouldUseSemantic && !actualUse:
					semanticFN++
				case !*assertion.ShouldUseSemantic && actualUse:
					semanticFP++
				default:
					semanticTN++
				}
			}
			if assertion.SemanticMode != "" {
				semanticModeCount++
				if effectiveQueryAnalysisDecision(analysis).SemanticMode == assertion.SemanticMode {
					semanticModeCorrect++
				}
			}
			if assertionExpectsForgetRoute(assertion) {
				forgetRouteCount++
				if analysisHasForgetRoute(analysis) {
					forgetRouteCorrect++
				}
			}
		case "context_precision_at_k":
			selected := flattenSelectedNodeIDs(retrieval)
			relevant := cleanAssertionRefs(assertion.RelevantNodeIDs)
			precision := precisionAtK(selected, relevant, assertion.At)
			precisionSum += precision
			contextPrecisionSum += precision
			precisionCount++
		case "selected_chain_correct":
			chainCount++
			if reportAssertionPassed(report, assertion) {
				chainPass++
			}
		case "forbidden_recall_zero":
			forbiddenAssertions++
			present := forbiddenPresent(retrieval, cleanAssertionRefs(assertion.ForbiddenNodeIDs))
			metrics.ForbiddenSelectedCount += present
		}
	}
	if recallCount > 0 {
		metrics.RequiredHitRate = recallSum / float64(recallCount)
		metrics.SelectedRecallAt8 = recallSum / float64(recallCount)
		metrics.MRR = mrrSum / float64(recallCount)
		metrics.NDCGAt8 = ndcgSum / float64(recallCount)
	}
	if candidateRecallCount > 0 {
		metrics.CandidateRecallAt20 = candidateRecall20Sum / float64(candidateRecallCount)
		metrics.CandidateRecallAt80 = candidateRecall80Sum / float64(candidateRecallCount)
	}
	if precisionCount > 0 {
		metrics.PrecisionAt8 = precisionSum / float64(precisionCount)
		metrics.ContextPrecision = contextPrecisionSum / float64(precisionCount)
	} else if selectedPrecisionCount > 0 {
		metrics.PrecisionAt8 = selectedPrecisionSum / float64(selectedPrecisionCount)
	}
	if chainCount > 0 {
		metrics.CausalChainCoverage = float64(chainPass) / float64(chainCount)
	}
	if forbiddenAssertions > 0 && metrics.ForbiddenSelectedCount > 0 {
		metrics.ForbiddenRecallRate = 1
		metrics.AuthorityFilterViolationCount = metrics.ForbiddenSelectedCount
	}
	if timeModeCount > 0 {
		metrics.FieldAccuracyTimeMode = float64(timeModeCorrect) / float64(timeModeCount)
	}
	if memoryAbilityCount > 0 {
		metrics.FieldAccuracyMemoryAbility = float64(memoryAbilityCorrect) / float64(memoryAbilityCount)
	}
	if memoryDomainCount > 0 {
		metrics.FieldAccuracyMemoryDomain = float64(memoryDomainCorrect) / float64(memoryDomainCount)
	}
	if evidenceNeedCount > 0 {
		metrics.FieldAccuracyEvidenceNeed = float64(evidenceNeedCorrect) / float64(evidenceNeedCount)
	}
	metrics.SemanticTriggerPrecision = ratioOrDefault(semanticTP, semanticTP+semanticFP, 1)
	metrics.SemanticTriggerRecall = ratioOrDefault(semanticTP, semanticTP+semanticFN, 1)
	metrics.FalseSkipSemanticRate = ratioOrDefault(semanticFN, semanticTP+semanticFN, 0)
	metrics.UnnecessarySemanticCallRate = ratioOrDefault(semanticFP, semanticFP+semanticTN, 0)
	metrics.SemanticModeAccuracy = ratioOrDefault(semanticModeCorrect, semanticModeCount, 1)
	if forgetRouteCount > 0 {
		metrics.ForgetRouteAccuracy = float64(forgetRouteCorrect) / float64(forgetRouteCount)
	}
	metrics.CandidateRecallBySource = averageRecallMap(candidateRecallBySource, recallCountBySource)
	metrics.SelectedRecallBySource = averageRecallMap(selectedRecallBySource, recallCountBySource)
}

func queryAnalysisForMetrics(retrieval *memorycore.MemoryContext) *memorycore.QueryAnalysis {
	if retrieval == nil {
		return nil
	}
	return retrieval.QueryAnalysis
}

func assertionExpectsForgetRoute(assertion Assertion) bool {
	if assertion.RetrievalMode == "target_resolver" || assertion.SemanticMode == "target_resolver" || assertion.MemoryAbility == string(memorycore.MemoryAbilityBoundary) {
		return true
	}
	for _, signal := range assertion.Signals {
		if signal == string(memorycore.QuerySignalForgetDelete) {
			return true
		}
	}
	return false
}

func analysisHasForgetRoute(analysis *memorycore.QueryAnalysis) bool {
	if analysis == nil {
		return false
	}
	decision := effectiveQueryAnalysisDecision(analysis)
	return analysis.MemoryAbility == memorycore.MemoryAbilityBoundary ||
		decision.RetrievalMode == "target_resolver" ||
		decision.SemanticMode == "target_resolver"
}

func ratioOrDefault(numerator int, denominator int, fallback float64) float64 {
	if denominator <= 0 {
		return fallback
	}
	return float64(numerator) / float64(denominator)
}

func addSourceLevelRecall(candidateSums map[string]float64, selectedSums map[string]float64, counts map[string]int, retrieval *memorycore.MemoryContext, selected []string, relevant []string, at int) {
	candidatesBySource := candidateNodeIDsBySource(retrieval)
	selectedBySource := selectedNodeIDsBySource(selected, candidatesBySource)
	for source, candidateIDs := range candidatesBySource {
		candidateSums[source] += recallAtK(candidateIDs, relevant, 80)
		selectedSums[source] += recallAtK(selectedBySource[source], relevant, at)
		counts[source]++
	}
}

func averageRecallMap(sums map[string]float64, counts map[string]int) map[string]float64 {
	if len(sums) == 0 {
		return nil
	}
	out := make(map[string]float64, len(sums))
	for source, sum := range sums {
		count := counts[source]
		if count <= 0 {
			continue
		}
		out[source] = sum / float64(count)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func reportAssertionPassed(report Report, assertion Assertion) bool {
	for _, result := range report.Results {
		if result.Type == assertion.Type && result.Name == assertion.Name {
			return result.Err == nil
		}
	}
	return false
}

func cleanAssertionRefs(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, qualityCleanRef(value))
	}
	return out
}

func forbiddenPresent(retrieval *memorycore.MemoryContext, forbidden []string) int {
	if retrieval == nil {
		return 0
	}
	set := stringSet(forbidden)
	count := 0
	for _, block := range retrieval.Blocks {
		for _, item := range block.Items {
			if _, ok := set[item.NodeID]; ok {
				count++
			}
			for _, related := range item.RelatedFacts {
				if _, ok := set[related.NodeID]; ok {
					count++
				}
			}
			for _, source := range item.SourceRefs {
				if _, ok := set[source.EpisodeID]; ok {
					count++
				}
			}
		}
	}
	return count
}

func flattenCandidateNodeIDs(retrieval *memorycore.MemoryContext) []string {
	if retrieval == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var ids []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		ids = append(ids, value)
	}
	if retrieval.Mirror != nil {
		for _, candidate := range retrieval.Mirror.Candidates {
			add(candidate.SQLiteFactID)
		}
	}
	if retrieval.GraphActivation != nil {
		for _, candidate := range retrieval.GraphActivation.Candidates {
			add(candidate.SQLiteNodeID)
		}
	}
	if retrieval.AnchorFusion != nil {
		for _, seed := range retrieval.AnchorFusion.Seeds {
			add(seed.NodeID)
		}
	}
	return ids
}

func candidateNodeIDsBySource(retrieval *memorycore.MemoryContext) map[string][]string {
	if retrieval == nil || retrieval.Mirror == nil {
		return nil
	}
	bySource := map[string][]string{}
	seen := map[string]map[string]struct{}{}
	add := func(source string, nodeID string) {
		source = strings.TrimSpace(source)
		nodeID = strings.TrimSpace(nodeID)
		if source == "" || nodeID == "" {
			return
		}
		if seen[source] == nil {
			seen[source] = map[string]struct{}{}
		}
		if _, ok := seen[source][nodeID]; ok {
			return
		}
		seen[source][nodeID] = struct{}{}
		bySource[source] = append(bySource[source], nodeID)
	}
	for _, candidate := range retrieval.Mirror.Candidates {
		if candidate.DropReason != "" {
			continue
		}
		add(candidate.Source, candidate.SQLiteFactID)
	}
	if len(bySource) == 0 {
		return nil
	}
	return bySource
}

func selectedNodeIDsBySource(selected []string, candidatesBySource map[string][]string) map[string][]string {
	if len(selected) == 0 || len(candidatesBySource) == 0 {
		return nil
	}
	selectedSet := stringSet(selected)
	out := map[string][]string{}
	for source, candidates := range candidatesBySource {
		for _, nodeID := range candidates {
			if _, ok := selectedSet[nodeID]; ok {
				out[source] = append(out[source], nodeID)
			}
		}
	}
	return out
}

func meanReciprocalRank(selected []string, relevant []string, at int) float64 {
	relevantSet := stringSet(relevant)
	for index, nodeID := range limitStrings(selected, at) {
		if _, ok := relevantSet[nodeID]; ok {
			return 1 / float64(index+1)
		}
	}
	return 0
}

func ndcgAtK(selected []string, relevant []string, at int) float64 {
	relevantSet := stringSet(relevant)
	limited := limitStrings(selected, at)
	var dcg float64
	for index, nodeID := range limited {
		if _, ok := relevantSet[nodeID]; ok {
			dcg += 1 / log2(float64(index+2))
		}
	}
	idealLimit := len(relevant)
	if at > 0 && at < idealLimit {
		idealLimit = at
	}
	var idcg float64
	for i := 0; i < idealLimit; i++ {
		idcg += 1 / log2(float64(i+2))
	}
	if idcg == 0 {
		return 1
	}
	return dcg / idcg
}

func log2(value float64) float64 {
	return math.Log2(value)
}

func isFallbackStatus(status string) bool {
	switch status {
	case "sidecar_error", "sidecar_timeout", "sidecar_degraded", "breaker_open", "adapter_missing", "persona_not_ready", "candidates_unmapped_or_stale", "skipped_by_budget":
		return true
	default:
		return false
	}
}

func profileDeltas(profiles []ProfileMatrixReport) map[string]float64 {
	byProfile := map[Profile]MatrixMetrics{}
	for _, profile := range profiles {
		if profile.Status == ProfileStatusPass {
			byProfile[profile.Profile] = profile.Metrics
		}
	}
	deltas := map[string]float64{}
	if sqlite, ok := byProfile[ProfileSQLiteGo]; ok {
		if dense, ok := byProfile[ProfileMirrorRealDense]; ok {
			deltas["dense_vs_sqlite.selected_recall_at_8"] = dense.SelectedRecallAt8 - sqlite.SelectedRecallAt8
		}
	}
	if dense, ok := byProfile[ProfileMirrorRealDense]; ok {
		if graph, ok := byProfile[ProfileMirrorRealGraph]; ok {
			deltas["graph_vs_dense.causal_chain_coverage"] = graph.CausalChainCoverage - dense.CausalChainCoverage
		}
	}
	if graph, ok := byProfile[ProfileMirrorRealGraph]; ok {
		if rerank, ok := byProfile[ProfileMirrorRealGraphRerank]; ok {
			deltas["rerank_vs_graph.precision_at_8"] = rerank.PrecisionAt8 - graph.PrecisionAt8
		}
	}
	if len(deltas) == 0 {
		return nil
	}
	return deltas
}

func FormatMatrixReport(report MatrixReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "matrix_report\n")
	fmt.Fprintf(&b, "test_plan_version: %s\n", matrixReportTestPlanVersion(report))
	fmt.Fprintf(&b, "case_id: %s\n", report.CaseID)
	for _, profile := range report.Profiles {
		fmt.Fprintf(&b, "\nprofile: %s\n", profile.Profile)
		fmt.Fprintf(&b, "status: %s\n", profile.Status)
		if profile.Error != "" {
			fmt.Fprintf(&b, "error: %s\n", profile.Error)
		}
		fmt.Fprintf(&b, "capability: %s\n", profile.Capability.Status)
		fmt.Fprintf(&b, "field_accuracy_time_mode: %.3f\n", profile.Metrics.FieldAccuracyTimeMode)
		fmt.Fprintf(&b, "field_accuracy_memory_ability: %.3f\n", profile.Metrics.FieldAccuracyMemoryAbility)
		fmt.Fprintf(&b, "field_accuracy_memory_domain: %.3f\n", profile.Metrics.FieldAccuracyMemoryDomain)
		fmt.Fprintf(&b, "field_accuracy_evidence_need: %.3f\n", profile.Metrics.FieldAccuracyEvidenceNeed)
		fmt.Fprintf(&b, "semantic_trigger_precision: %.3f\n", profile.Metrics.SemanticTriggerPrecision)
		fmt.Fprintf(&b, "semantic_trigger_recall: %.3f\n", profile.Metrics.SemanticTriggerRecall)
		fmt.Fprintf(&b, "false_skip_semantic_rate: %.3f\n", profile.Metrics.FalseSkipSemanticRate)
		fmt.Fprintf(&b, "unnecessary_semantic_call_rate: %.3f\n", profile.Metrics.UnnecessarySemanticCallRate)
		fmt.Fprintf(&b, "semantic_mode_accuracy: %.3f\n", profile.Metrics.SemanticModeAccuracy)
		fmt.Fprintf(&b, "forget_route_accuracy: %.3f\n", profile.Metrics.ForgetRouteAccuracy)
		fmt.Fprintf(&b, "candidate_recall@20: %.3f\n", profile.Metrics.CandidateRecallAt20)
		fmt.Fprintf(&b, "candidate_recall@80: %.3f\n", profile.Metrics.CandidateRecallAt80)
		fmt.Fprintf(&b, "selected_recall@8: %.3f\n", profile.Metrics.SelectedRecallAt8)
		fmt.Fprintf(&b, "selected_recall_at_8: %.3f\n", profile.Metrics.SelectedRecallAt8)
		fmt.Fprintf(&b, "precision@8: %.3f\n", profile.Metrics.PrecisionAt8)
		fmt.Fprintf(&b, "precision_at_8: %.3f\n", profile.Metrics.PrecisionAt8)
		fmt.Fprintf(&b, "required_hit_rate: %.3f\n", profile.Metrics.RequiredHitRate)
		fmt.Fprintf(&b, "causal_chain_coverage: %.3f\n", profile.Metrics.CausalChainCoverage)
		fmt.Fprintf(&b, "forbidden_recall_rate: %.3f\n", profile.Metrics.ForbiddenRecallRate)
		fmt.Fprintf(&b, "temporal_correctness_hard_failures: %d\n", profile.Metrics.TemporalCorrectnessHardFailures)
		fmt.Fprintf(&b, "redundancy_rate: %.3f\n", profile.Metrics.RedundancyRate)
		fmt.Fprintf(&b, "restraint_violation_rate: %.3f\n", profile.Metrics.RestraintViolationRate)
		fmt.Fprintf(&b, "semantic_calls_per_1000_queries: %.3f\n", profile.Metrics.SemanticCallsPer1000Queries)
		fmt.Fprintf(&b, "semantic_cost_per_1000_queries: %.3f\n", profile.Metrics.SemanticCostPer1000Queries)
		fmt.Fprintf(&b, "retrieval_latency_p95: %d\n", profile.Metrics.RetrievalLatencyP95)
		fmt.Fprintf(&b, "post_eval_corrective_action_rate: %.3f\n", profile.Metrics.PostEvalCorrectiveActionRate)
		fmt.Fprintf(&b, "fallback_count: %d\n", profile.Metrics.FallbackCount)
		fmt.Fprintf(&b, "query_analysis_used_count: %d\n", profile.Metrics.QueryAnalysisUsedCount)
		fmt.Fprintf(&b, "query_analysis_fallback_count: %d\n", profile.Metrics.QueryAnalysisFallbackCount)
		fmt.Fprintf(&b, "query_analysis_invalid_json_count: %d\n", profile.Metrics.QueryAnalysisInvalidJSONCount)
		fmt.Fprintf(&b, "query_analysis_validation_failed_count: %d\n", profile.Metrics.QueryAnalysisValidationFailedCount)
		fmt.Fprintf(&b, "rule_only_no_semantic_call_count: %d\n", profile.Metrics.RuleOnlyNoSemanticCallCount)
		fmt.Fprintf(&b, "semantic_timeout_rule_fallback_count: %d\n", profile.Metrics.SemanticTimeoutRuleFallbackCount)
		fmt.Fprintf(&b, "semantic_validation_failed_rule_fallback_count: %d\n", profile.Metrics.SemanticValidationRuleFallbackCount)
		fmt.Fprintf(&b, "semantic_drift_count: %d\n", profile.Metrics.SemanticDriftCount)
		fmt.Fprintf(&b, "provider_timeout_count: %d\n", profile.Metrics.ProviderTimeoutCount)
		fmt.Fprintf(&b, "sidecar_provider_timeout_count: %d\n", profile.Metrics.SidecarProviderTimeoutCount)
		fmt.Fprintf(&b, "provider_budget_exhausted_count: %d\n", profile.Metrics.ProviderBudgetExhaustedCount)
		fmt.Fprintf(&b, "query_analysis_latency_p50: %d\n", profile.Metrics.QueryAnalysisLatencyP50)
		fmt.Fprintf(&b, "query_analysis_latency_p95: %d\n", profile.Metrics.QueryAnalysisLatencyP95)
		fmt.Fprintf(&b, "semantic_latency_p95: %d\n", profile.Metrics.SemanticLatencyP95)
		fmt.Fprintf(&b, "english_rewrite_count: %d\n", profile.Metrics.EnglishRewriteCount)
		fmt.Fprintf(&b, "dropped_rewrite_count: %d\n", profile.Metrics.DroppedRewriteCount)
		fmt.Fprintf(&b, "semantic_rewrite_dense_count: %d\n", profile.Metrics.SemanticRewriteDenseCount)
		fmt.Fprintf(&b, "candidate_query_count: %d\n", profile.Metrics.CandidateQueryCount)
		fmt.Fprintf(&b, "query_trim_count: %d\n", profile.Metrics.QueryTrimCount)
		fmt.Fprintf(&b, "raw_exact_survival_count: %d\n", profile.Metrics.RawExactSurvivalCount)
		fmt.Fprintf(&b, "past_event_direct_fact_count: %d\n", profile.Metrics.PastEventDirectFactCount)
		fmt.Fprintf(&b, "historical_transition_count: %d\n", profile.Metrics.HistoricalTransitionCount)
		fmt.Fprintf(&b, "provenance_source_count: %d\n", profile.Metrics.ProvenanceSourceCount)
		fmt.Fprintf(&b, "event_bundle_completion_count: %d\n", profile.Metrics.EventBundleCompletionCount)
		fmt.Fprintf(&b, "supersedes_completion_count: %d\n", profile.Metrics.SupersedesCompletionCount)
		fmt.Fprintf(&b, "provenance_completion_count: %d\n", profile.Metrics.ProvenanceCompletionCount)
		fmt.Fprintf(&b, "counterexample_expansion_count: %d\n", profile.Metrics.CounterexampleExpansionCount)
		fmt.Fprintf(&b, "reflection_summary_boost_count: %d\n", profile.Metrics.ReflectionSummaryBoostCount)
		fmt.Fprintf(&b, "rerank_skipped_count: %d\n", profile.Metrics.RerankSkippedCount)
		fmt.Fprintf(&b, "llm_saved_call_count: %d\n", profile.Metrics.LLMSavedCallCount)
		fmt.Fprintf(&b, "avg_query_analysis_wait_ms: %d\n", profile.Metrics.AvgQueryAnalysisWaitMS)
		if len(profile.Metrics.CandidateRecallBySource) > 0 {
			fmt.Fprintf(&b, "candidate_recall_by_source: %s\n", formatMetricFloatMap(profile.Metrics.CandidateRecallBySource))
		}
		if len(profile.Metrics.SelectedRecallBySource) > 0 {
			fmt.Fprintf(&b, "selected_recall_by_source: %s\n", formatMetricFloatMap(profile.Metrics.SelectedRecallBySource))
		}
		fmt.Fprintf(&b, "graph_activation_used_count: %d\n", profile.Metrics.GraphActivationUsedCount)
		fmt.Fprintf(&b, "rerank_live_call_count: %d\n", profile.Metrics.RerankLiveCallCount)
		fmt.Fprintf(&b, "embedding_cache_hits: %d\n", profile.Metrics.EmbeddingCacheHits)
		fmt.Fprintf(&b, "embedding_cache_misses: %d\n", profile.Metrics.EmbeddingCacheMisses)
		fmt.Fprintf(&b, "embedding_live_call_count: %d\n", profile.Metrics.EmbeddingLiveCallCount)
		if profile.Capability.RerankProviderMode != "" {
			fmt.Fprintf(&b, "rerank_provider_mode: %s\n", profile.Capability.RerankProviderMode)
			fmt.Fprintf(&b, "rerank_cache: %t\n", profile.Capability.RerankCache)
		}
		if profile.Metrics.MirrorManifestHash != "" {
			fmt.Fprintf(&b, "mirror_manifest_hash: %s\n", profile.Metrics.MirrorManifestHash)
		}
	}
	if len(report.Deltas) > 0 {
		b.WriteString("\ndeltas:\n")
		for key, value := range report.Deltas {
			fmt.Fprintf(&b, "  %s: %.3f\n", key, value)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func formatMetricFloatMap(values map[string]float64) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%.3f", key, values[key]))
	}
	return strings.Join(parts, ",")
}

func matrixReportTestPlanVersion(report MatrixReport) string {
	if strings.TrimSpace(report.TestPlanVersion) != "" {
		return strings.TrimSpace(report.TestPlanVersion)
	}
	return matrixTestPlanVersion
}

func (r MatrixReport) JSONString() string {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func latestManifestHash(root string, fixture *Fixture, identity MirrorArtifactIdentity) string {
	if fixture == nil || strings.TrimSpace(root) == "" {
		return ""
	}
	path := filepath.Join(root, fixture.StableHash(), identity.Fingerprint(), defaultSearchableTextVersion, "manifest.json")
	return hashFile(path)
}

func profileAdapter(profile Profile, adapter memorycore.MirrorAdapter) memorycore.MirrorAdapter {
	if adapter == nil {
		return nil
	}
	namespace, _ := adapter.(memorycore.MirrorNamespaceAdapter)
	candidates, _ := adapter.(memorycore.MirrorCandidateAdapter)
	activation, _ := adapter.(memorycore.MirrorActivationAdapter)
	rerank, _ := adapter.(memorycore.MirrorRerankAdapter)
	configurator, _ := adapter.(memorycore.MirrorEvalConfigurator)
	switch profile {
	case ProfileMirrorRealDense,
		ProfileRuleOnlyRaw,
		ProfileSemanticParseOnly,
		ProfileSemanticRewriteOnly,
		ProfileSemanticFullCurrent,
		ProfileSemanticFullSoftGated,
		ProfileRerankOff,
		ProfileSoftRoutingEnabled:
		return denseOnlyAdapter{base: adapter, namespace: namespace, candidates: candidates, configurator: configurator}
	case ProfileRerankSelective:
		base := denseOnlyAdapter{base: adapter, namespace: namespace, candidates: candidates, configurator: configurator}
		if rerank == nil {
			return base
		}
		return rerankNoGraphAdapter{denseOnlyAdapter: base, rerank: rerank}
	case ProfileMirrorRealGraph:
		return graphOnlyAdapter{denseOnlyAdapter: denseOnlyAdapter{base: adapter, namespace: namespace, candidates: candidates, configurator: configurator}, activation: activation}
	case ProfileMirrorRealGraphRerank:
		return graphRerankAdapter{graphOnlyAdapter: graphOnlyAdapter{denseOnlyAdapter: denseOnlyAdapter{base: adapter, namespace: namespace, candidates: candidates, configurator: configurator}, activation: activation}, rerank: rerank}
	case ProfileMirrorRealRerankNoGraph:
		return rerankNoGraphAdapter{denseOnlyAdapter: denseOnlyAdapter{base: adapter, namespace: namespace, candidates: candidates, configurator: configurator}, rerank: rerank}
	default:
		return adapter
	}
}

type denseOnlyAdapter struct {
	base         memorycore.MirrorAdapter
	namespace    memorycore.MirrorNamespaceAdapter
	candidates   memorycore.MirrorCandidateAdapter
	configurator memorycore.MirrorEvalConfigurator
}

func (a denseOnlyAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	return a.base.UpsertNode(ctx, payload)
}
func (a denseOnlyAdapter) DeleteNode(ctx context.Context, ref memorycore.MirrorNodeRef) error {
	return a.base.DeleteNode(ctx, ref)
}
func (a denseOnlyAdapter) UpsertEdge(ctx context.Context, payload memorycore.MirrorEdgePayload) error {
	return a.base.UpsertEdge(ctx, payload)
}
func (a denseOnlyAdapter) DeleteEdge(ctx context.Context, ref memorycore.MirrorEdgeRef) error {
	return a.base.DeleteEdge(ctx, ref)
}
func (a denseOnlyAdapter) ClearNamespace(ctx context.Context, personaID string) error {
	return a.namespace.ClearNamespace(ctx, personaID)
}
func (a denseOnlyAdapter) FindCandidates(ctx context.Context, req memorycore.MirrorCandidateRequest) (*memorycore.MirrorCandidateResult, error) {
	return a.candidates.FindCandidates(ctx, req)
}
func (a denseOnlyAdapter) ConfigureEval(ctx context.Context, req memorycore.MirrorEvalConfigRequest) (*memorycore.MirrorEvalConfigResult, error) {
	if a.configurator == nil {
		return nil, nil
	}
	return a.configurator.ConfigureEval(ctx, req)
}

type graphOnlyAdapter struct {
	denseOnlyAdapter
	activation memorycore.MirrorActivationAdapter
}

func (a graphOnlyAdapter) ActivateGraph(ctx context.Context, req memorycore.MirrorActivationRequest) (*memorycore.MirrorActivationResult, error) {
	return a.activation.ActivateGraph(ctx, req)
}

type graphRerankAdapter struct {
	graphOnlyAdapter
	rerank memorycore.MirrorRerankAdapter
}

func (a graphRerankAdapter) Rerank(ctx context.Context, req memorycore.MirrorRerankRequest) (*memorycore.MirrorRerankResult, error) {
	return a.rerank.Rerank(ctx, req)
}

type rerankNoGraphAdapter struct {
	denseOnlyAdapter
	rerank memorycore.MirrorRerankAdapter
}

func (a rerankNoGraphAdapter) Rerank(ctx context.Context, req memorycore.MirrorRerankRequest) (*memorycore.MirrorRerankResult, error) {
	return a.rerank.Rerank(ctx, req)
}
