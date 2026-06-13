package entityresolver

import (
	"context"
	"database/sql"
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

func TestResolverUserCandidateUsesPersonaScopedFallback(t *testing.T) {
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

	if _, err := svc.Writes().EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		ID:            "ent_user",
		PersonaID:     "default",
		CanonicalName: "User",
		EntityType:    memorycore.EntityTypeUser,
	}); err != nil {
		t.Fatalf("ensure default user entity: %v", err)
	}
	if _, err := svc.Writes().EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		ID:            "custom_user_persona_custom",
		PersonaID:     "persona_custom",
		CanonicalName: "Human",
		EntityType:    memorycore.EntityTypeUser,
	}); err != nil {
		t.Fatalf("ensure custom user entity: %v", err)
	}

	resolver := Resolver{Service: svc, DB: db.SQLDB()}
	cases := []struct {
		name      string
		personaID string
		wantID    string
	}{
		{name: "legacy default user is reused", personaID: "default", wantID: "ent_user"},
		{name: "first non-default user is persona scoped", personaID: "persona_alpha", wantID: "ent_user_persona_alpha"},
		{name: "second non-default user gets a different persona scoped id", personaID: "persona_beta", wantID: "ent_user_persona_beta"},
		{name: "custom user entity is reused", personaID: "persona_custom", wantID: "custom_user_persona_custom"},
		{name: "case underscore and dash are preserved", personaID: "Persona-B_02", wantID: "ent_user_Persona-B_02"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := resolver.Resolve(ctx, Input{
				PersonaID:   tc.personaID,
				CandidateID: "user",
			})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if result.EntityID != tc.wantID || result.Status != "resolved" || result.CanonicalKey != "special:user" {
				t.Fatalf("result = %#v, want resolved %q", result, tc.wantID)
			}
			assertEntityOwner(t, db.SQLDB(), tc.wantID, tc.personaID)
		})
	}

	assertNoEntity(t, db.SQLDB(), "ent_user_default")
	assertEntityOwner(t, db.SQLDB(), "ent_user", "default")
}

func assertEntityOwner(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, entityID string, personaID string) {
	t.Helper()
	var got string
	if err := db.QueryRowContext(context.Background(), `
SELECT persona_id
FROM entities
WHERE id = ?`, entityID).Scan(&got); err != nil {
		t.Fatalf("query entity %s: %v", entityID, err)
	}
	if got != personaID {
		t.Fatalf("entity %s persona_id = %q, want %q", entityID, got, personaID)
	}
}

func assertNoEntity(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, entityID string) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM entities
WHERE id = ?`, entityID).Scan(&count); err != nil {
		t.Fatalf("count entity %s: %v", entityID, err)
	}
	if count != 0 {
		t.Fatalf("entity %s count = %d, want 0", entityID, count)
	}
}
