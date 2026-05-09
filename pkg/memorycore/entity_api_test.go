package memorycore_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceEnsureEntityAndAliasAreIdempotent(t *testing.T) {
	ctx := context.Background()
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()

	description := "主用户"
	entity, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		CanonicalName: "Long",
		EntityType:    memorycore.EntityTypeUser,
		Description:   &description,
		Aliases: []memorycore.EntityAliasInput{
			{Alias: "Longy", AliasType: memorycore.AliasTypeNickname},
		},
	})
	if err != nil {
		t.Fatalf("ensure entity: %v", err)
	}
	if entity.ID == "" {
		t.Fatal("entity id is empty")
	}
	if entity.PersonaID != "default" {
		t.Fatalf("entity persona = %q, want default", entity.PersonaID)
	}
	if entity.EntityType != memorycore.EntityTypeUser {
		t.Fatalf("entity type = %q, want %q", entity.EntityType, memorycore.EntityTypeUser)
	}
	if len(entity.Aliases) != 1 || entity.Aliases[0].Alias != "Longy" {
		t.Fatalf("entity aliases = %#v, want Longy", entity.Aliases)
	}

	same, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		CanonicalName: "Long",
		EntityType:    memorycore.EntityTypeUser,
	})
	if err != nil {
		t.Fatalf("ensure same entity: %v", err)
	}
	if same.ID != entity.ID {
		t.Fatalf("same entity id = %q, want %q", same.ID, entity.ID)
	}

	firstAlias, err := svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{
		EntityID:  entity.ID,
		Alias:     "Long",
		AliasType: memorycore.AliasTypeSurface,
	})
	if err != nil {
		t.Fatalf("add alias: %v", err)
	}
	secondAlias, err := svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{
		EntityID:  entity.ID,
		Alias:     "Long",
		AliasType: memorycore.AliasTypeSurface,
	})
	if err != nil {
		t.Fatalf("add duplicate alias: %v", err)
	}
	if secondAlias.ID != firstAlias.ID {
		t.Fatalf("duplicate alias id = %q, want %q", secondAlias.ID, firstAlias.ID)
	}
}

func TestServiceEntityValidation(t *testing.T) {
	ctx := context.Background()
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()

	if _, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("ensure entity without canonical name err = %v, want ErrInvalidRequest", err)
	}
	if _, err := svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{Alias: "Long"}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("add alias without entity err = %v, want ErrInvalidRequest", err)
	}
	if _, err := svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{EntityID: "missing", Alias: "Long"}); !errors.Is(err, memorycore.ErrNotFound) {
		t.Fatalf("add alias missing entity err = %v, want ErrNotFound", err)
	}
	if _, err := svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{EntityID: "entity", Alias: ""}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("add alias without alias err = %v, want ErrInvalidRequest", err)
	}
}
