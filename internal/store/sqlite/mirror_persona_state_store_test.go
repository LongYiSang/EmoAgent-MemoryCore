package sqlite_test

import (
	"context"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestMirrorPersonaStateRepositoryDefaultsMissingRowsReadyAndMarksStates(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}

	repo := memsqlite.NewMirrorPersonaStateRepository(db.SQLDB())
	ready, err := repo.IsReady(ctx, "default")
	if err != nil {
		t.Fatalf("is ready for missing row: %v", err)
	}
	if !ready {
		t.Fatalf("missing mirror persona state ready = false, want true")
	}

	if err := repo.MarkRebuilding(ctx, "default"); err != nil {
		t.Fatalf("mark rebuilding: %v", err)
	}
	ready, err = repo.IsReady(ctx, "default")
	if err != nil {
		t.Fatalf("is ready after rebuilding: %v", err)
	}
	if ready {
		t.Fatalf("rebuilding mirror persona state ready = true, want false")
	}

	if err := repo.MarkDegraded(ctx, "default", "clear namespace failed"); err != nil {
		t.Fatalf("mark degraded: %v", err)
	}
	ready, err = repo.IsReady(ctx, "default")
	if err != nil {
		t.Fatalf("is ready after degraded: %v", err)
	}
	if ready {
		t.Fatalf("degraded mirror persona state ready = true, want false")
	}

	if err := repo.MarkReady(ctx, "default"); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	ready, err = repo.IsReady(ctx, "default")
	if err != nil {
		t.Fatalf("is ready after ready: %v", err)
	}
	if !ready {
		t.Fatalf("ready mirror persona state ready = false, want true")
	}
}
