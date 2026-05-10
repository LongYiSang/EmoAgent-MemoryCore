package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExamplesSmokeRunSh(t *testing.T) {
	bashPath, ok := findSmokeBash()
	if !ok {
		t.Skip("bash with Go on PATH not available")
	}

	repoRoot := filepath.Join("..", "..")
	dbPath := filepath.Join(t.TempDir(), "memory-smoke.db")
	cmd := exec.Command(bashPath, "examples/smoke/run.sh")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "MEMORY_DB="+dbPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("smoke failed: %v\n%s", err, out)
	}
	requireContains(t, string(out), "SMOKE PASS")
}

func findSmokeBash() (string, bool) {
	candidates := []string{"bash"}
	if runtime.GOOS == "windows" {
		candidates = []string{
			`C:\MySoftware\Git\bin\bash.exe`,
			`C:\Program Files\Git\bin\bash.exe`,
			"bash",
		}
	}
	for _, candidate := range candidates {
		path := candidate
		if resolved, err := exec.LookPath(candidate); err == nil {
			path = resolved
		}
		if _, err := os.Stat(path); err != nil && filepath.IsAbs(path) {
			continue
		}
		cmd := exec.Command(path, "-lc", "command -v go >/dev/null 2>&1")
		if err := cmd.Run(); err == nil {
			return path, true
		}
	}
	return "", false
}
