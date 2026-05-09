package sqlite_test

import (
	"context"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestEntityRepositoryEnsureByCanonicalIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}

	entities := memsqlite.NewEntityRepository(db.SQLDB())
	first, err := entities.EnsureByCanonical(ctx, core.Entity{
		ID:            "entity_01",
		PersonaID:     "default",
		CanonicalName: "Long",
		EntityType:    core.EntityTypeUser,
	})
	if err != nil {
		t.Fatalf("ensure first entity: %v", err)
	}
	second, err := entities.EnsureByCanonical(ctx, core.Entity{
		ID:            "entity_02",
		PersonaID:     "default",
		CanonicalName: "Long",
		EntityType:    core.EntityTypeUser,
	})
	if err != nil {
		t.Fatalf("ensure second entity: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second entity id = %q, want %q", second.ID, first.ID)
	}

	var count int
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT COUNT(*)
FROM entities
WHERE persona_id = 'default' AND canonical_name = 'Long' AND entity_type = 'user'`).Scan(&count); err != nil {
		t.Fatalf("count entities: %v", err)
	}
	if count != 1 {
		t.Fatalf("entity count = %d, want 1", count)
	}
}

func TestEntityRepositoryEnsureAliasIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}

	entities := memsqlite.NewEntityRepository(db.SQLDB())
	entity, err := entities.EnsureByCanonical(ctx, core.Entity{
		ID:            "entity_01",
		PersonaID:     "default",
		CanonicalName: "Long",
		EntityType:    core.EntityTypeUser,
	})
	if err != nil {
		t.Fatalf("ensure entity: %v", err)
	}

	first, err := entities.EnsureAlias(ctx, core.EntityAlias{
		ID:        "alias_01",
		PersonaID: "default",
		EntityID:  entity.ID,
		Alias:     "Longy",
		AliasType: core.AliasTypeNickname,
	})
	if err != nil {
		t.Fatalf("ensure first alias: %v", err)
	}
	second, err := entities.EnsureAlias(ctx, core.EntityAlias{
		ID:        "alias_02",
		PersonaID: "default",
		EntityID:  entity.ID,
		Alias:     "Longy",
		AliasType: core.AliasTypeNickname,
	})
	if err != nil {
		t.Fatalf("ensure second alias: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second alias id = %q, want %q", second.ID, first.ID)
	}

	var count int
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT COUNT(*)
FROM entity_aliases
WHERE persona_id = 'default' AND entity_id = ? AND alias = 'Longy' AND alias_type = 'nickname'`, entity.ID).Scan(&count); err != nil {
		t.Fatalf("count aliases: %v", err)
	}
	if count != 1 {
		t.Fatalf("alias count = %d, want 1", count)
	}
}
