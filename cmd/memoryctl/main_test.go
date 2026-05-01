package main

import (
	"bytes"
	"context"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestRunInitDBMigratesAndSeedsDefaultPersona(t *testing.T) {
	dbPath := t.TempDir() + "/memory.db"

	var stdout, stderr bytes.Buffer
	code := run([]string{"init-db", "--db", dbPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}

	db, err := memsqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer db.Close()

	var displayName string
	err = db.SQLDB().QueryRow(`SELECT display_name FROM personas WHERE id = 'default'`).Scan(&displayName)
	if err != nil {
		t.Fatalf("default persona not seeded: %v", err)
	}
	if displayName != "Default" {
		t.Fatalf("default persona display name = %q, want Default", displayName)
	}
}

func TestRunRejectsMissingDBFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"init-db"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("exit code = 0, want failure")
	}
	if stderr.Len() == 0 {
		t.Fatalf("stderr is empty, want usage error")
	}
}
