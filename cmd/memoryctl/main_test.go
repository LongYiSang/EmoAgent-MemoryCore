package main

import (
	"bytes"
	"context"
	"strings"
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

func TestRunInitDBIsRepeatable(t *testing.T) {
	dbPath := t.TempDir() + "/memory.db"

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init-db", "--db", dbPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("first exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"init-db", "--db", dbPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("second exit code = %d, want 0; stderr=%s", code, stderr.String())
	}

	db, err := memsqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	defer db.Close()

	var migrationCount int
	if err := db.SQLDB().QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != 2 {
		t.Fatalf("migration count = %d, want 2", migrationCount)
	}
}

func TestRunInitDBRejectsDirtyMigration(t *testing.T) {
	dbPath := t.TempDir() + "/memory.db"

	var stdout, stderr bytes.Buffer
	if code := run([]string{"init-db", "--db", dbPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("initial exit code = %d, want 0; stderr=%s", code, stderr.String())
	}

	db, err := memsqlite.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	if _, err := db.SQLDB().Exec(`UPDATE schema_migrations SET dirty = 1 WHERE version = '0001'`); err != nil {
		db.Close()
		t.Fatalf("mark migration dirty: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code := run([]string{"init-db", "--db", dbPath}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("dirty init-db exit code = 0, want failure")
	}
	if !strings.Contains(stderr.String(), "dirty") {
		t.Fatalf("stderr = %q, want dirty migration error", stderr.String())
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
