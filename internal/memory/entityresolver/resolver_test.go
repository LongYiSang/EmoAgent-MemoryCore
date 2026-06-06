package entityresolver

import (
	"context"
	"path/filepath"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestResolverReusesNamedPetAliasOnlyInHasPetContext(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{DBPath: dbPath, AutoMigrate: true})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()
	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	existing, err := svc.Writes().EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		ID:            "ent_xiaoju",
		PersonaID:     "default",
		CanonicalName: "小橘",
		EntityType:    memorycore.EntityTypeObject,
	})
	if err != nil {
		t.Fatalf("ensure existing pet entity: %v", err)
	}

	resolver := Resolver{Service: svc, DB: db.SQLDB()}
	result, err := resolver.Resolve(ctx, Input{
		PersonaID: "default",
		ResponseEntities: []memorycore.ExtractedEntityCandidate{{
			CandidateID:      "e_pet",
			CanonicalName:    "小橘猫",
			EntityType:       memorycore.EntityTypeObject,
			Confidence:       0.95,
			MergeHint:        "new_entity",
			SensitivityLevel: memorycore.SensitivityNormal,
		}},
		CandidateID:    "e_pet",
		AllowSensitive: true,
		Predicate:      "has_pet",
		ObjectKind:     "entity",
	})
	if err != nil {
		t.Fatalf("resolve named pet alias: %v", err)
	}
	if result.EntityID != existing.ID || result.Status != "resolved" {
		t.Fatalf("resolve result = %#v, want existing pet entity", result)
	}

	result, err = resolver.Resolve(ctx, Input{
		PersonaID: "default",
		ResponseEntities: []memorycore.ExtractedEntityCandidate{{
			CandidateID:      "e_place_like",
			CanonicalName:    "小橘猫",
			EntityType:       memorycore.EntityTypeObject,
			Confidence:       0.95,
			MergeHint:        "new_entity",
			SensitivityLevel: memorycore.SensitivityNormal,
		}},
		CandidateID:    "e_place_like",
		AllowSensitive: true,
		Predicate:      "related_to",
		ObjectKind:     "entity",
	})
	if err != nil {
		t.Fatalf("resolve non-pet context: %v", err)
	}
	if result.EntityID == existing.ID {
		t.Fatalf("non-pet context resolved to existing pet entity: %#v", result)
	}
}
