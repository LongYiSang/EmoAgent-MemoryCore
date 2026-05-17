package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	memoryeval "github.com/longyisang/emoagent-memorycore/internal/memory/eval"
)

type options struct {
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
	mirrorArtifactDir        string
	embeddingCacheMode       string
	reuseMirror              string
	reportDir                string
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
		if opts.qualityNoStub {
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
		report := memoryeval.NewRunner(memoryeval.RunnerOptions{TempDir: opts.tempDir}).Run(ctx, fixture)
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
		if opts.qualityNoStub {
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
			SidecarURL:               opts.sidecarURL,
			Strict:                   opts.strictCapabilities,
			AllowSkipMissingProvider: opts.allowSkipMissingProvider,
			MirrorArtifactDir:        opts.mirrorArtifactDir,
			EmbeddingCacheMode:       opts.embeddingCacheMode,
			ReuseMirror:              opts.reuseMirror,
			ReportDir:                reportDir,
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

func writeCombinedMatrixReports(reportDir string, outputs []matrixRunOutput) error {
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		return err
	}
	reports := make([]memoryeval.MatrixReport, 0, len(outputs))
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
	return os.WriteFile(filepath.Join(reportDir, "report.json"), append(data, '\n'), 0o644)
}

func parseOptions(args []string, stderr io.Writer) (options, bool) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		repoRoot = "."
	}
	var rawMode string
	var rawProfiles string
	opts := options{suite: "retrieval", qualityNoStub: true, strictCapabilities: true, embeddingCacheMode: "off", reuseMirror: "auto"}
	fs := flag.NewFlagSet("memory-eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
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
	if err := fs.Parse(args); err != nil {
		return options{}, false
	}
	mode, reportMode, ok := parseMode(rawMode)
	if !ok {
		fmt.Fprintln(stderr, "mode must be brief, full, or matrix")
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
	opts.mode = mode
	opts.reportMode = reportMode
	opts.profiles = profiles
	opts.embeddingCacheMode = memoryeval.NormalizeEmbeddingCacheMode(opts.embeddingCacheMode)
	if strings.TrimSpace(opts.root) == "" {
		opts.root = filepath.Join(repoRoot, "testdata", "memory_eval", "quality", opts.suite)
	}
	return opts, true
}

func parseMode(value string) (string, memoryeval.QualityBenchmarkMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "brief", "short":
		return "brief", memoryeval.QualityBenchmarkModeBrief, true
	case "full", "all":
		return "full", memoryeval.QualityBenchmarkModeFull, true
	case "matrix":
		return "matrix", memoryeval.QualityBenchmarkModeFull, true
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
