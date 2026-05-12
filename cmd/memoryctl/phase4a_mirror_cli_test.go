package main

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunMirrorSyncWithFakeAdapter(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	factID := requireRunID(t,
		"consolidate-fact",
		"--db", dbPath,
		"--subject", "ent_user",
		"--predicate", "likes",
		"--object-literal", "咖啡",
		"--summary", "用户喜欢咖啡。",
		"--source-episode", "ep_seed",
		"--format", "id",
	)

	out := requireRunOK(t, "mirror-sync-run", "--db", dbPath, "--limit", "10", "--fake-adapter", "--format", "text")
	want := "claimed=3\ncompleted=3\nfailed=0\nskipped=1\n"
	if out != want {
		t.Fatalf("mirror sync output = %q, want %q", out, want)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var status string
	if err := db.QueryRow(`
SELECT index_status
FROM memory_index_map
WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&status); err != nil {
		t.Fatalf("query mirror index map: %v", err)
	}
	if status != "indexed" {
		t.Fatalf("mirror index status = %q, want indexed", status)
	}
}

func TestRunMirrorSyncRejectsWithoutAdapterChoice(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	_, stderr, code := runCLI("mirror-sync-run", "--db", dbPath)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "--fake-adapter or --sidecar-url is required")
}
