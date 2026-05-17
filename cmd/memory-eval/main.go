package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	memoryeval "github.com/longyisang/emoagent-memorycore/internal/memory/eval"
)

type options struct {
	mode    memoryeval.QualityBenchmarkMode
	suite   string
	root    string
	fixture string
	tempDir string
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
		report := memoryeval.NewRunner(memoryeval.RunnerOptions{TempDir: opts.tempDir}).Run(ctx, fixture)
		cases = append(cases, memoryeval.QualityBenchmarkCase{
			Path:    path,
			Fixture: fixture,
			Report:  report,
		})
	}

	output := memoryeval.FormatQualityBenchmarkReport(cases, memoryeval.QualityBenchmarkReportOptions{Mode: opts.mode})
	fmt.Fprintln(stdout, output)
	if qualityFailed(cases) {
		return 1
	}
	return 0
}

func parseOptions(args []string, stderr io.Writer) (options, bool) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		repoRoot = "."
	}
	var rawMode string
	opts := options{suite: "retrieval"}
	fs := flag.NewFlagSet("memory-eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&rawMode, "mode", string(memoryeval.QualityBenchmarkModeBrief), "output mode: brief or full")
	fs.StringVar(&opts.suite, "suite", opts.suite, "quality suite under testdata/memory_eval/quality")
	fs.StringVar(&opts.root, "root", "", "directory containing quality benchmark fixtures")
	fs.StringVar(&opts.fixture, "fixture", "", "single fixture file to run instead of --root")
	fs.StringVar(&opts.tempDir, "temp-dir", "", "optional temp directory for per-fixture SQLite databases")
	if err := fs.Parse(args); err != nil {
		return options{}, false
	}
	mode, ok := parseMode(rawMode)
	if !ok {
		fmt.Fprintln(stderr, "mode must be brief or full")
		return options{}, false
	}
	opts.mode = mode
	if strings.TrimSpace(opts.root) == "" {
		opts.root = filepath.Join(repoRoot, "testdata", "memory_eval", "quality", opts.suite)
	}
	return opts, true
}

func parseMode(value string) (memoryeval.QualityBenchmarkMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "brief", "short":
		return memoryeval.QualityBenchmarkModeBrief, true
	case "full", "all":
		return memoryeval.QualityBenchmarkModeFull, true
	default:
		return "", false
	}
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
