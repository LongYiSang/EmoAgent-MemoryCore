package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

func TestEvalFixtures(t *testing.T) {
	suites := []fixtureRegressionSuite{
		{Dir: "consolidation", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "extraction_consolidation", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "forgetting", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "phase5", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "retrieval", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "retention", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "natural_memory", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "controlled", StubPolicy: FixtureStubPolicyRequire},
	}

	ctx := context.Background()
	var count int
	for _, suite := range suites {
		paths := discoverSuiteFixtures(t, suite.Dir)
		count += len(paths)
		for _, path := range paths {
			path := path
			suite := suite
			t.Run(path, func(t *testing.T) {
				fixture, err := LoadFixtureFile(path)
				if err != nil {
					t.Fatalf("load fixture: %v", err)
				}
				if err := fixture.ValidateStubPolicy(suite.StubPolicy); err != nil {
					t.Fatal(err)
				}
				report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(ctx, fixture)
				logEvalDebug(t, report)
				if report.Failed() {
					t.Fatal(report.Error())
				}
			})
		}
	}
	if count < 10 {
		t.Fatalf("fixture count = %d, want at least 10", count)
	}
}

func TestExtractionConsolidationFixturesWriteLatestReport(t *testing.T) {
	paths := discoverSuiteFixtures(t, "extraction_consolidation")
	if len(paths) < 12 {
		t.Fatalf("extraction_consolidation fixture count = %d, want at least 12", len(paths))
	}
	ctx := context.Background()
	cases := make([]QualityBenchmarkCase, 0, len(paths))
	for _, path := range paths {
		fixture, err := LoadFixtureFile(path)
		if err != nil {
			cases = append(cases, QualityBenchmarkCase{Path: path, Report: Report{Err: err}})
			continue
		}
		report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).Run(ctx, fixture)
		cases = append(cases, QualityBenchmarkCase{Path: path, Fixture: fixture, Report: report})
	}
	reportDir := filepath.Join(repoRoot(t), "reports", "memory_eval")
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatalf("create report dir: %v", err)
	}
	if err := writeExtractionLatestReports(filepath.Join(reportDir, "latest.json"), filepath.Join(reportDir, "latest.md"), cases); err != nil {
		t.Fatalf("write latest reports: %v", err)
	}
	for _, item := range cases {
		logEvalDebug(t, item.Report)
		if item.Report.Failed() {
			t.Fatal(item.Report.Error())
		}
	}
}

func TestQualityFixturesDoNotUseEvalStubs(t *testing.T) {
	paths := discoverSuiteFixtures(t, filepath.Join("quality", "retrieval"))
	if len(paths) == 0 {
		t.Fatal("quality retrieval fixture count = 0, want at least 1")
	}
	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			fixture, err := LoadFixtureFile(path)
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			if err := fixture.ValidateStubPolicy(FixtureStubPolicyForbid); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestQualityRetrievalFixturesDeclareQualityMetadata(t *testing.T) {
	paths := discoverSuiteFixtures(t, filepath.Join("quality", "retrieval"))
	if len(paths) == 0 {
		t.Fatal("quality retrieval fixture count = 0, want at least 1")
	}
	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			fixture, err := LoadFixtureFile(path)
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			if fixture.SchemaVersion != "memory_eval.v0.2" {
				t.Fatalf("schema_version = %q, want memory_eval.v0.2", fixture.SchemaVersion)
			}
			if fixture.Suite != "quality_retrieval" {
				t.Fatalf("suite = %q, want quality_retrieval", fixture.Suite)
			}
			if !fixture.QualityMode {
				t.Fatalf("quality_mode = false, want true")
			}
			if fixture.AllowStub {
				t.Fatalf("allow_stub = true, want false")
			}
		})
	}
}

func TestQualityExtractDirectoryDocumentsLiveLLMBoundary(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "testdata", "memory_eval", "quality", "extract")
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("quality extract directory missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("quality extract path is not a directory: %s", dir)
	}
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("quality extract README missing: %v", err)
	}
	text := string(readme)
	for _, want := range []string{
		"suite: quality_extract",
		"quality_mode: true",
		"allow_stub: false",
		"openai-compatible",
		"raw-log",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("quality extract README missing %q", want)
		}
	}
}

func TestQualityExtractLiveFixturesDeclareLiveMetadata(t *testing.T) {
	paths := discoverSuiteFixtures(t, filepath.Join("quality", "extract"))
	if len(paths) < 2 {
		t.Fatalf("quality extract fixture count = %d, want at least 2", len(paths))
	}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture, err := LoadFixtureFile(path)
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			if fixture.Suite != "quality_extract" {
				t.Fatalf("suite = %q, want quality_extract", fixture.Suite)
			}
			if !fixture.QualityMode || fixture.AllowStub {
				t.Fatalf("quality metadata = quality_mode:%v allow_stub:%v", fixture.QualityMode, fixture.AllowStub)
			}
			if fixture.LiveExtraction == nil {
				t.Fatal("live_extraction is nil")
			}
			if strings.TrimSpace(fixture.LiveExtraction.SessionID) == "" {
				t.Fatal("live_extraction.session_id is required")
			}
			if err := fixture.ValidateStubPolicy(FixtureStubPolicyForbid); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPhase8QueryAnalysisFixturesRunWithShadowAdaptive(t *testing.T) {
	paths := discoverSuiteFixtures(t, "query_analysis")
	wantCaseIDs := map[string]struct{}{
		"direct_fact_entity_exact":             {},
		"direct_fact_generic_weak_anchor":      {},
		"causal_recent_weak_anchor":            {},
		"historical_current_ambiguous":         {},
		"provenance_question":                  {},
		"premise_check":                        {},
		"relationship_arc":                     {},
		"forget_target_ambiguous":              {},
		"sensitive_recall_should_not_semantic": {},
		"no_memory_chat":                       {},
		"smalltalk_no_memory_no_semantic":      {},
	}
	if len(paths) != len(wantCaseIDs) {
		t.Fatalf("query_analysis fixture count = %d, want %d", len(paths), len(wantCaseIDs))
	}
	ctx := context.Background()
	seen := map[string]struct{}{}
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			fixture, err := LoadFixtureFile(path)
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			if _, ok := wantCaseIDs[fixture.CaseID]; !ok {
				t.Fatalf("unexpected case_id %q from %s", fixture.CaseID, path)
			}
			seen[fixture.CaseID] = struct{}{}
			if err := fixture.ValidateStubPolicy(FixtureStubPolicyForbid); err != nil {
				t.Fatal(err)
			}
			report := NewRunner(RunnerOptions{
				TempDir: t.TempDir(),
				QueryAnalysis: memorycore.QueryAnalysisOptions{
					Mode: memorycore.QueryAnalysisModeShadowAdaptive,
				},
			}).Run(ctx, fixture)
			logEvalDebug(t, report)
			if report.Failed() {
				t.Fatal(report.Error())
			}
		})
	}
	for caseID := range wantCaseIDs {
		if _, ok := seen[caseID]; !ok {
			t.Fatalf("missing query_analysis fixture case_id %q", caseID)
		}
	}
}

type fixtureRegressionSuite struct {
	Dir        string
	StubPolicy FixtureStubPolicy
}

func discoverSuiteFixtures(t *testing.T, relativeDir string) []string {
	t.Helper()
	paths, err := DiscoverFixtureFiles(filepath.Join(repoRoot(t), "testdata", "memory_eval", relativeDir))
	if err != nil {
		t.Fatalf("discover fixtures in %s: %v", relativeDir, err)
	}
	return paths
}

func TestR012BatchAuthorityEquivalence(t *testing.T) {
	runFixtureFile(t, filepath.Join("controlled", "retrieval", "R012_batch_authority_equivalence.yaml"))
}

func TestR013BatchReconstructionEquivalence(t *testing.T) {
	runFixtureFile(t, filepath.Join("retrieval", "R013_batch_reconstruction_equivalence.yaml"))
}

func runFixtureFile(t *testing.T, relativePath string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(repoRoot(t), "testdata", "memory_eval", relativePath)
	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).RunFile(ctx, path)
	logEvalDebug(t, report)
	if report.Failed() {
		t.Fatal(report.Error())
	}
}

func logEvalDebug(t *testing.T, report Report) {
	t.Helper()
	if !evalDebugEnabled() {
		return
	}
	t.Log("\n" + report.DebugString())
}

func evalDebugEnabled() bool {
	for _, name := range []string{"MEMORY_EVAL_DEBUG", "MEMORY_EVAL_VERBOSE"} {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
		case "1", "true", "yes", "on":
			return true
		}
	}
	return false
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from %s", dir)
		}
		dir = parent
	}
}
