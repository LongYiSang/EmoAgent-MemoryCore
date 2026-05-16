package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEvalFixtures(t *testing.T) {
	paths, err := DiscoverFixtureFiles(filepath.Join(repoRoot(t), "testdata", "memory_eval"))
	if err != nil {
		t.Fatalf("discover fixtures: %v", err)
	}
	if len(paths) < 10 {
		t.Fatalf("fixture count = %d, want at least 10", len(paths))
	}

	ctx := context.Background()
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).RunFile(ctx, path)
			if report.Failed() {
				t.Fatal(report.Error())
			}
		})
	}
}

func TestR012BatchAuthorityEquivalence(t *testing.T) {
	runFixtureFile(t, filepath.Join("retrieval", "R012_batch_authority_equivalence.yaml"))
}

func TestR013BatchReconstructionEquivalence(t *testing.T) {
	runFixtureFile(t, filepath.Join("retrieval", "R013_batch_reconstruction_equivalence.yaml"))
}

func runFixtureFile(t *testing.T, relativePath string) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(repoRoot(t), "testdata", "memory_eval", relativePath)
	report := NewRunner(RunnerOptions{TempDir: t.TempDir()}).RunFile(ctx, path)
	if report.Failed() {
		t.Fatal(report.Error())
	}
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
