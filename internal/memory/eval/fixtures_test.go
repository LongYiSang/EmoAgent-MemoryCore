package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvalFixtures(t *testing.T) {
	suites := []fixtureRegressionSuite{
		{Dir: "consolidation", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "forgetting", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "phase5", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "retrieval", StubPolicy: FixtureStubPolicyForbid},
		{Dir: "retention", StubPolicy: FixtureStubPolicyForbid},
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
