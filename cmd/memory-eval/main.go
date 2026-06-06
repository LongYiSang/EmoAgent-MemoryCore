package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
	memoryeval "github.com/longyisang/emoagent-memorycore/internal/memory/eval"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type options struct {
	configPath               string
	mode                     string
	reportMode               memoryeval.QualityBenchmarkMode
	suite                    string
	root                     string
	fixture                  string
	tempDir                  string
	profiles                 []memoryeval.Profile
	qualityNoStub            bool
	strictCapabilities       bool
	allowSkipMissingProvider bool
	sidecarURL               string
	mirrorAdapter            appcore.MirrorAdapter
	sidecarResilience        memorycore.SidecarResilienceOptions
	mirrorArtifactDir        string
	embeddingCacheMode       string
	reuseMirror              string
	reportDir                string
	liveProvider             string
	liveBaseURL              string
	liveModel                string
	liveAPIKeyEnv            string
	liveRawLogDir            string
	liveTimeout              time.Duration
	liveMaxTokens            int
	liveThinking             bool
	queryAnalysis            memorycore.QueryAnalysisOptions
	retrievalPolicy          memorycore.RetrievalPolicy
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	opts, ok := parseOptions(args, stderr)
	if !ok {
		return 2
	}

	paths, err := fixturePaths(opts)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 2
	}
	if len(paths) == 0 {
		fmt.Fprintf(stderr, "no fixture files found\n")
		return 2
	}

	ctx := context.Background()
	if opts.mode == "matrix" {
		return runMatrix(ctx, opts, paths, stdout, stderr)
	}
	if opts.mode == "live" {
		return runLiveExtraction(ctx, opts, paths, stdout, stderr)
	}
	cases := make([]memoryeval.QualityBenchmarkCase, 0, len(paths))
	for _, path := range paths {
		fixture, err := memoryeval.LoadFixtureFile(path)
		if err != nil {
			cases = append(cases, memoryeval.QualityBenchmarkCase{
				Path: path,
				Report: memoryeval.Report{
					CaseID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
					Err:    err,
				},
			})
			continue
		}
		if shouldForbidEvalStubs(opts, fixture) {
			if err := fixture.ValidateStubPolicy(memoryeval.FixtureStubPolicyForbid); err != nil {
				cases = append(cases, memoryeval.QualityBenchmarkCase{
					Path:    path,
					Fixture: fixture,
					Report: memoryeval.Report{
						CaseID: fixture.CaseID,
						Err:    err,
					},
				})
				continue
			}
		}
		report := memoryeval.NewRunner(memoryeval.RunnerOptions{
			TempDir:         opts.tempDir,
			RetrievalPolicy: opts.retrievalPolicy,
		}).Run(ctx, fixture)
		cases = append(cases, memoryeval.QualityBenchmarkCase{
			Path:    path,
			Fixture: fixture,
			Report:  report,
		})
	}

	output := memoryeval.FormatQualityBenchmarkReport(cases, memoryeval.QualityBenchmarkReportOptions{Mode: opts.reportMode})
	fmt.Fprintln(stdout, output)
	if qualityFailed(cases) {
		return 1
	}
	return 0
}

func runLiveExtraction(ctx context.Context, opts options, paths []string, stdout io.Writer, stderr io.Writer) int {
	if opts.suite != "extract" {
		fmt.Fprintln(stderr, "--mode live is only supported with --suite extract")
		return 2
	}
	report := memoryeval.RunLiveExtractionFiles(ctx, paths, memoryeval.LiveExtractionRunnerOptions{
		TempDir:   opts.tempDir,
		ReportDir: opts.reportDir,
		Provider:  opts.liveProvider,
		BaseURL:   opts.liveBaseURL,
		Model:     opts.liveModel,
		APIKeyEnv: opts.liveAPIKeyEnv,
		RawLogDir: opts.liveRawLogDir,
		Timeout:   opts.liveTimeout,
		MaxTokens: opts.liveMaxTokens,
		Thinking:  opts.liveThinking,
	})
	fmt.Fprintln(stdout, memoryeval.FormatLiveExtractionReport(report))
	if opts.reportDir != "" {
		if err := memoryeval.WriteLiveExtractionReports(opts.reportDir, report); err != nil {
			fmt.Fprintf(stderr, "write live extraction reports: %v\n", err)
			return 1
		}
	}
	if report.Failed > 0 {
		return 1
	}
	return 0
}

type matrixRunOutput struct {
	Fixture *memoryeval.Fixture
	Report  memoryeval.MatrixReport
}

func runMatrix(ctx context.Context, opts options, paths []string, stdout io.Writer, stderr io.Writer) int {
	failed := false
	outputs := make([]matrixRunOutput, 0, len(paths))
	for index, path := range paths {
		fixture, err := memoryeval.LoadFixtureFile(path)
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", path, err)
			failed = true
			continue
		}
		if shouldForbidEvalStubs(opts, fixture) {
			if err := fixture.ValidateStubPolicy(memoryeval.FixtureStubPolicyForbid); err != nil {
				fmt.Fprintf(stderr, "%s: %v\n", path, err)
				failed = true
				continue
			}
		}
		reportDir := opts.reportDir
		if reportDir != "" && len(paths) > 1 {
			reportDir = filepath.Join(reportDir, sanitizePathName(fixture.CaseID))
		}
		report := memoryeval.NewMatrixRunner(memoryeval.MatrixRunnerOptions{
			TempDir:                  opts.tempDir,
			Profiles:                 opts.profiles,
			MirrorAdapter:            opts.mirrorAdapter,
			SidecarURL:               opts.sidecarURL,
			Strict:                   opts.strictCapabilities,
			AllowSkipMissingProvider: opts.allowSkipMissingProvider,
			MirrorArtifactDir:        opts.mirrorArtifactDir,
			EmbeddingCacheMode:       opts.embeddingCacheMode,
			ReuseMirror:              opts.reuseMirror,
			ReportDir:                reportDir,
			QueryAnalysis:            opts.queryAnalysis,
			RetrievalPolicy:          opts.retrievalPolicy,
			SidecarResilience:        opts.sidecarResilience,
		}).Run(ctx, fixture)
		outputs = append(outputs, matrixRunOutput{Fixture: fixture, Report: report})
		if index > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintln(stdout, memoryeval.FormatMatrixReport(report))
		if report.Failed() {
			failed = true
		}
	}
	if opts.reportDir != "" && len(outputs) > 1 {
		if err := writeCombinedMatrixReports(opts.reportDir, outputs); err != nil {
			fmt.Fprintf(stderr, "write combined matrix reports: %v\n", err)
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

func shouldForbidEvalStubs(opts options, fixture *memoryeval.Fixture) bool {
	if !opts.qualityNoStub || fixture == nil {
		return false
	}
	return fixture.QualityMode || !fixture.AllowStub
}

func writeCombinedMatrixReports(reportDir string, outputs []matrixRunOutput) error {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return err
	}
	reports := make([]memoryeval.MatrixReport, 0, len(outputs))
	queryAnalysisReports := make([]memoryeval.QueryAnalysisReport, 0, len(outputs))
	var summary strings.Builder
	var detail strings.Builder
	for index, output := range outputs {
		if index > 0 {
			summary.WriteString("\n\n")
			detail.WriteString("\n\n")
		}
		summary.WriteString(memoryeval.FormatMatrixReport(output.Report))
		detail.WriteString(memoryeval.FormatMatrixDetailReport(output.Fixture, output.Report))
		reports = append(reports, output.Report)
		queryAnalysisReports = append(queryAnalysisReports, memoryeval.BuildQueryAnalysisReport(output.Fixture, output.Report))
	}
	if err := os.WriteFile(filepath.Join(reportDir, "report.md"), []byte(summary.String()+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(reportDir, "detail.md"), []byte(detail.String()+"\n"), 0o644); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(reportDir, "report.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	queryAnalysisData, err := json.MarshalIndent(queryAnalysisReports, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(reportDir, "query_analysis.json"), append(queryAnalysisData, '\n'), 0o644)
}

func parseOptions(args []string, stderr io.Writer) (options, bool) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		repoRoot = "."
	}
	var rawMode string
	var rawProfiles string
	var queryAnalysisMode string
	var queryAnalysisTimeoutMS int
	var queryAnalysisSoftJoinTimeoutMS int
	var queryAnalysisMaxSemanticLatencyMS int
	rawLiveThinking := "false"
	opts := options{suite: "retrieval", qualityNoStub: true, strictCapabilities: true, embeddingCacheMode: "off", reuseMirror: "auto"}
	fs := flag.NewFlagSet("memory-eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.configPath, "config", "", "MemoryCore config path")
	fs.StringVar(&rawMode, "mode", string(memoryeval.QualityBenchmarkModeBrief), "output mode: brief, full, or matrix")
	fs.StringVar(&opts.suite, "suite", opts.suite, "quality suite under testdata/memory_eval/quality")
	fs.StringVar(&opts.root, "root", "", "directory containing quality benchmark fixtures")
	fs.StringVar(&opts.fixture, "fixture", "", "single fixture file to run instead of --root")
	fs.StringVar(&opts.tempDir, "temp-dir", "", "optional temp directory for per-fixture SQLite databases")
	fs.StringVar(&rawProfiles, "profiles", "sqlite_go", "comma-separated eval profiles")
	fs.BoolVar(&opts.qualityNoStub, "quality-no-stub", opts.qualityNoStub, "forbid mirror_stub, graph_activation_stub, and rerank_stub")
	fs.BoolVar(&opts.strictCapabilities, "strict-capabilities", opts.strictCapabilities, "fail requested profiles when required capabilities are missing")
	fs.BoolVar(&opts.allowSkipMissingProvider, "allow-skip-missing-provider", false, "skip missing sidecar/provider profiles without counting as pass")
	fs.StringVar(&opts.sidecarURL, "sidecar-url", "", "loopback HTTP URL for real mirror profiles")
	fs.StringVar(&opts.mirrorArtifactDir, "mirror-artifact-dir", "", "directory for mirror artifacts")
	fs.StringVar(&opts.embeddingCacheMode, "embedding-cache-mode", opts.embeddingCacheMode, "embedding cache mode: off, read_write, read_only, or refresh")
	fs.StringVar(&opts.reuseMirror, "reuse-mirror", opts.reuseMirror, "mirror reuse mode: auto or never")
	fs.StringVar(&opts.reportDir, "report-dir", "", "optional directory for matrix report.json and report.md")
	fs.StringVar(&opts.liveProvider, "provider", "", "live extraction provider: mock or openai-compatible")
	fs.StringVar(&opts.liveBaseURL, "base-url", "", "OpenAI-compatible base URL for --suite extract --mode live")
	fs.StringVar(&opts.liveModel, "model", "", "extraction model for --suite extract --mode live")
	fs.StringVar(&opts.liveAPIKeyEnv, "api-key-env", "", "environment variable containing live extraction provider API key")
	fs.StringVar(&opts.liveRawLogDir, "raw-log-dir", "", "directory for live extraction raw logs")
	fs.DurationVar(&opts.liveTimeout, "timeout", 0, "live extraction provider timeout")
	fs.IntVar(&opts.liveMaxTokens, "max-tokens", 0, "live extraction maximum output tokens")
	fs.StringVar(&rawLiveThinking, "thinking", rawLiveThinking, "live extraction thinking mode: true or false")
	fs.StringVar(&queryAnalysisMode, "query-analysis-mode", "rule_only", "query analysis mode: rule_only, semantic_always, semantic_on_low_confidence, semantic_rewrite_only, legacy_only, shadow_adaptive, adaptive, adaptive_safe, or adaptive_full")
	fs.IntVar(&queryAnalysisTimeoutMS, "query-analysis-timeout-ms", 1500, "query analysis sidecar timeout in milliseconds")
	fs.IntVar(&queryAnalysisSoftJoinTimeoutMS, "query-analysis-soft-join-timeout-ms", 0, "semantic query-analysis wait budget before raw-only completion in milliseconds; 0 uses query-analysis-timeout-ms")
	fs.IntVar(&queryAnalysisMaxSemanticLatencyMS, "query-analysis-max-semantic-latency-ms", 0, "maximum semantic provider latency budget in milliseconds; 0 uses query-analysis default")
	if err := fs.Parse(args); err != nil {
		return options{}, false
	}
	explicit := explicitFlagNames(fs)
	if strings.TrimSpace(opts.configPath) != "" {
		cfg, err := loadMemoryEvalConfig(opts.configPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return options{}, false
		}
		if err := applyMemoryEvalConfig(&opts, &cfg, explicit, &queryAnalysisMode, &queryAnalysisTimeoutMS, &queryAnalysisMaxSemanticLatencyMS); err != nil {
			fmt.Fprintln(stderr, err)
			return options{}, false
		}
	}
	mode, reportMode, ok := parseMode(rawMode)
	if !ok {
		fmt.Fprintln(stderr, "mode must be brief, full, matrix, or live")
		return options{}, false
	}
	profiles, err := memoryeval.ParseProfiles(rawProfiles)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return options{}, false
	}
	if err := memoryeval.ValidateEmbeddingCacheMode(opts.embeddingCacheMode); err != nil {
		fmt.Fprintln(stderr, err)
		return options{}, false
	}
	liveThinking, err := strconv.ParseBool(strings.TrimSpace(rawLiveThinking))
	if err != nil {
		fmt.Fprintln(stderr, "thinking must be true or false")
		return options{}, false
	}
	if queryAnalysisRequiresSidecar(queryAnalysisMode) && !hasMirrorProfile(profiles) {
		fmt.Fprintln(stderr, "query-analysis-mode requires at least one mirror/semantic profile")
		return options{}, false
	}
	queryAnalysis, err := parseQueryAnalysisOptions(queryAnalysisMode, opts.sidecarURL, queryAnalysisTimeoutMS, queryAnalysisSoftJoinTimeoutMS, queryAnalysisMaxSemanticLatencyMS)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return options{}, false
	}
	opts.mode = mode
	opts.reportMode = reportMode
	opts.profiles = profiles
	opts.embeddingCacheMode = memoryeval.NormalizeEmbeddingCacheMode(opts.embeddingCacheMode)
	opts.queryAnalysis = queryAnalysis
	opts.liveThinking = liveThinking
	if strings.TrimSpace(opts.root) == "" {
		opts.root = defaultSuiteRoot(repoRoot, opts.suite)
	}
	return opts, true
}

func explicitFlagNames(fs *flag.FlagSet) map[string]bool {
	names := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		names[f.Name] = true
	})
	return names
}

func loadMemoryEvalConfig(path string) (memconfig.Config, error) {
	opts := memconfig.LoadOptions{SkipValidate: true}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		return memconfig.LoadJSONWithOptions(path, opts)
	}
	return memconfig.LoadYAMLWithOptions(path, opts)
}

func applyMemoryEvalConfig(opts *options, cfg *memconfig.Config, explicit map[string]bool, queryAnalysisMode *string, queryAnalysisTimeoutMS *int, queryAnalysisMaxSemanticLatencyMS *int) error {
	if explicit["sidecar-url"] {
		cfg.Sidecar.Enabled = true
		cfg.Sidecar.URL = opts.sidecarURL
	}
	if explicit["query-analysis-mode"] {
		cfg.Pipelines.QueryAnalysis.RuntimeMode = *queryAnalysisMode
	}
	if explicit["query-analysis-timeout-ms"] && *queryAnalysisTimeoutMS > 0 {
		cfg.Pipelines.QueryAnalysis.TimeoutMS = *queryAnalysisTimeoutMS
	}
	if explicit["query-analysis-max-semantic-latency-ms"] && *queryAnalysisMaxSemanticLatencyMS > 0 {
		cfg.Pipelines.QueryAnalysis.Budget.MaxSemanticLatencyMS = *queryAnalysisMaxSemanticLatencyMS
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	runtimeOptions, err := cfg.ToOptions()
	if err != nil {
		return err
	}
	if !explicit["sidecar-url"] && cfg.Sidecar.Enabled {
		opts.sidecarURL = strings.TrimSpace(runtimeOptions.QueryAnalysis.SidecarURL)
	}
	opts.mirrorAdapter = nil
	if cfg.Sidecar.Enabled {
		switch cfg.Sidecar.Adapter {
		case "fake":
			opts.mirrorAdapter = appcore.NewFakeMirrorAdapter()
		case "trivium":
			opts.mirrorAdapter = appcore.NewSidecarMirrorAdapter(cfg.Sidecar.URL)
		}
	}
	opts.sidecarResilience = runtimeOptions.SidecarResilience
	opts.retrievalPolicy = cfg.RetrievalPolicy()
	if !explicit["query-analysis-mode"] {
		if mode := strings.TrimSpace(string(runtimeOptions.QueryAnalysis.Mode)); mode != "" {
			*queryAnalysisMode = mode
		}
	}
	if !explicit["query-analysis-timeout-ms"] && runtimeOptions.QueryAnalysis.Timeout > 0 {
		*queryAnalysisTimeoutMS = durationMilliseconds(runtimeOptions.QueryAnalysis.Timeout)
	}
	if !explicit["query-analysis-max-semantic-latency-ms"] && runtimeOptions.QueryAnalysis.MaxSemanticLatency > 0 {
		*queryAnalysisMaxSemanticLatencyMS = durationMilliseconds(runtimeOptions.QueryAnalysis.MaxSemanticLatency)
	}
	return nil
}

func durationMilliseconds(value time.Duration) int {
	return int(value / time.Millisecond)
}

func defaultSuiteRoot(repoRoot string, suite string) string {
	switch strings.TrimSpace(suite) {
	case "query_analysis":
		return filepath.Join(repoRoot, "testdata", "memory_eval", "query_analysis")
	case "natural_memory":
		return filepath.Join(repoRoot, "testdata", "memory_eval", "natural_memory")
	}
	return filepath.Join(repoRoot, "testdata", "memory_eval", "quality", suite)
}

func queryAnalysisRequested(mode string) bool {
	mode = strings.TrimSpace(mode)
	return mode != "" && mode != "rule_only"
}

func queryAnalysisRequiresSidecar(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "semantic_always", "semantic_on_low_confidence", "semantic_rewrite_only", "adaptive", "adaptive_safe", "adaptive_full":
		return true
	default:
		return false
	}
}

func hasMirrorProfile(profiles []memoryeval.Profile) bool {
	for _, profile := range profiles {
		if profile.UsesMirror() {
			return true
		}
	}
	return false
}

func parseQueryAnalysisOptions(mode string, sidecarURL string, timeoutMS int, softJoinTimeoutMS int, maxSemanticLatencyMS int) (memorycore.QueryAnalysisOptions, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "rule_only"
	}
	switch mode {
	case "rule_only":
		return memorycore.QueryAnalysisOptions{}, nil
	case "legacy_only", "shadow_adaptive":
		return memorycore.QueryAnalysisOptions{Mode: memorycore.QueryAnalysisMode(mode)}, nil
	case "semantic_always", "semantic_on_low_confidence", "semantic_rewrite_only", "adaptive", "adaptive_safe", "adaptive_full":
	default:
		return memorycore.QueryAnalysisOptions{}, fmt.Errorf("query-analysis-mode must be one of rule_only, semantic_always, semantic_on_low_confidence, semantic_rewrite_only, legacy_only, shadow_adaptive, adaptive, adaptive_safe, adaptive_full")
	}
	if strings.TrimSpace(sidecarURL) == "" {
		return memorycore.QueryAnalysisOptions{}, fmt.Errorf("--sidecar-url is required when --query-analysis-mode is not rule_only")
	}
	if timeoutMS <= 0 {
		return memorycore.QueryAnalysisOptions{}, fmt.Errorf("query-analysis-timeout-ms must be > 0")
	}
	if softJoinTimeoutMS < 0 {
		return memorycore.QueryAnalysisOptions{}, fmt.Errorf("query-analysis-soft-join-timeout-ms must be >= 0")
	}
	if maxSemanticLatencyMS < 0 {
		return memorycore.QueryAnalysisOptions{}, fmt.Errorf("query-analysis-max-semantic-latency-ms must be >= 0")
	}
	options := memorycore.QueryAnalysisOptions{
		Provider:   memorycore.QueryAnalysisProviderSidecar,
		Mode:       memorycore.QueryAnalysisMode(mode),
		SidecarURL: strings.TrimSpace(sidecarURL),
		Timeout:    time.Duration(timeoutMS) * time.Millisecond,
	}
	if softJoinTimeoutMS > 0 {
		options.SoftJoinTimeout = time.Duration(softJoinTimeoutMS) * time.Millisecond
	}
	if maxSemanticLatencyMS > 0 {
		options.MaxSemanticLatency = time.Duration(maxSemanticLatencyMS) * time.Millisecond
	}
	return options, nil
}

func parseMode(value string) (string, memoryeval.QualityBenchmarkMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "brief", "short":
		return "brief", memoryeval.QualityBenchmarkModeBrief, true
	case "full", "all":
		return "full", memoryeval.QualityBenchmarkModeFull, true
	case "matrix":
		return "matrix", memoryeval.QualityBenchmarkModeFull, true
	case "live":
		return "live", memoryeval.QualityBenchmarkModeFull, true
	default:
		return "", "", false
	}
}

func sanitizePathName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "case"
	}
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func fixturePaths(opts options) ([]string, error) {
	if strings.TrimSpace(opts.fixture) != "" {
		path, err := filepath.Abs(opts.fixture)
		if err != nil {
			return nil, fmt.Errorf("resolve fixture path: %w", err)
		}
		return []string{path}, nil
	}
	root, err := filepath.Abs(opts.root)
	if err != nil {
		return nil, fmt.Errorf("resolve root path: %w", err)
	}
	return memoryeval.DiscoverFixtureFiles(root)
}

func qualityFailed(cases []memoryeval.QualityBenchmarkCase) bool {
	for _, item := range cases {
		if item.Report.Failed() {
			return true
		}
	}
	return false
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
