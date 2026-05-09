package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestConsolidationRepositoryWritesFactLinksAndQueueAtomically(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	repo := memsqlite.NewConsolidationRepository(db.SQLDB(), fixedConsolidationIDs(), fixedConsolidationNow)
	result, err := repo.ConsolidateCandidate(ctx, memsqlite.ConsolidateCandidateRequest{
		PersonaID: "default",
		Trigger:   "manual",
		Candidate: memsqlite.ManualFactCandidate{
			SubjectEntityID:  "ent_user",
			Predicate:        "likes",
			ObjectLiteral:    ptr("咖啡"),
			ContentSummary:   "用户喜欢咖啡。",
			FactType:         string(core.FactTypeStablePreference),
			SourceEpisodeIDs: []string{"ep_visible"},
			Confidence:       string(core.ExtractionConfidenceExplicit),
			Importance:       0.7,
		},
	})
	if err != nil {
		t.Fatalf("consolidate candidate: %v", err)
	}
	if result.Action != memsqlite.ConsolidationActionInsert {
		t.Fatalf("action = %q, want insert", result.Action)
	}
	if result.Fact == nil {
		t.Fatal("result fact is nil")
	}

	requireSQLiteFactCount(t, db.SQLDB(), "likes", 1)
	requireSQLiteLinkCount(t, db.SQLDB(), result.Fact.ID, string(core.LinkTypeEvidencedBy), "ep_visible", 1)
	requireSQLiteLinkCount(t, db.SQLDB(), result.Fact.ID, string(core.LinkTypeAboutEntity), "ent_user", 1)
	requireQueueCount(t, db.SQLDB(), "fact", result.Fact.ID, "upsert_node", 1)
	requireQueueCount(t, db.SQLDB(), "memory_link", result.LinkIDs[0], "upsert_edge", 1)
}

func TestConsolidationRepositoryReinforcesAndKeepsLinksIdempotent(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	repo := memsqlite.NewConsolidationRepository(db.SQLDB(), fixedConsolidationIDs(), fixedConsolidationNow)
	first, err := repo.ConsolidateCandidate(ctx, memsqlite.ConsolidateCandidateRequest{
		PersonaID: "default",
		Trigger:   "manual",
		Candidate: memsqlite.ManualFactCandidate{
			SubjectEntityID:  "ent_user",
			Predicate:        "likes",
			ObjectLiteral:    ptr("咖啡"),
			ContentSummary:   "用户喜欢咖啡。",
			SourceEpisodeIDs: []string{"ep_visible"},
			Confidence:       string(core.ExtractionConfidenceExplicit),
			Importance:       0.4,
		},
	})
	if err != nil {
		t.Fatalf("first consolidate: %v", err)
	}
	second, err := repo.ConsolidateCandidate(ctx, memsqlite.ConsolidateCandidateRequest{
		PersonaID: "default",
		Trigger:   "manual",
		Candidate: memsqlite.ManualFactCandidate{
			SubjectEntityID:  "ent_user",
			Predicate:        "likes",
			ObjectLiteral:    ptr("咖啡"),
			ContentSummary:   "用户喜欢咖啡。",
			SourceEpisodeIDs: []string{"ep_visible"},
			Confidence:       string(core.ExtractionConfidenceExplicit),
			Importance:       0.8,
		},
	})
	if err != nil {
		t.Fatalf("second consolidate: %v", err)
	}

	if second.Action != memsqlite.ConsolidationActionReinforce {
		t.Fatalf("second action = %q, want reinforce", second.Action)
	}
	if second.Fact.ID != first.Fact.ID {
		t.Fatalf("reinforced fact id = %q, want %q", second.Fact.ID, first.Fact.ID)
	}
	if second.Fact.ReinforcementCount != 1 {
		t.Fatalf("reinforcement count = %d, want 1", second.Fact.ReinforcementCount)
	}
	if second.Fact.Importance != 0.8 {
		t.Fatalf("importance = %.2f, want 0.8", second.Fact.Importance)
	}
	requireSQLiteFactCount(t, db.SQLDB(), "likes", 1)
	requireSQLiteLinkCount(t, db.SQLDB(), first.Fact.ID, string(core.LinkTypeEvidencedBy), "ep_visible", 1)
	requireSQLiteLinkCount(t, db.SQLDB(), first.Fact.ID, string(core.LinkTypeAboutEntity), "ent_user", 1)
}

func TestConsolidationRepositorySupersedesAndPersistsInvalidation(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	repo := memsqlite.NewConsolidationRepository(db.SQLDB(), fixedConsolidationIDs(), fixedConsolidationNow)
	first, err := repo.ConsolidateCandidate(ctx, memsqlite.ConsolidateCandidateRequest{
		PersonaID: "default",
		Trigger:   "manual",
		Candidate: memsqlite.ManualFactCandidate{
			SubjectEntityID:  "ent_user",
			Predicate:        "lives_in",
			ObjectEntityID:   ptr("ent_shanghai"),
			ContentSummary:   "用户住在上海。",
			SourceEpisodeIDs: []string{"ep_visible"},
			Confidence:       string(core.ExtractionConfidenceExplicit),
		},
	})
	if err != nil {
		t.Fatalf("first consolidate: %v", err)
	}
	second, err := repo.ConsolidateCandidate(ctx, memsqlite.ConsolidateCandidateRequest{
		PersonaID: "default",
		Trigger:   "manual",
		Candidate: memsqlite.ManualFactCandidate{
			SubjectEntityID:  "ent_user",
			Predicate:        "lives_in",
			ObjectEntityID:   ptr("ent_hangzhou"),
			ContentSummary:   "用户住在杭州。",
			SourceEpisodeIDs: []string{"ep_later"},
			Confidence:       string(core.ExtractionConfidenceExplicit),
		},
	})
	if err != nil {
		t.Fatalf("second consolidate: %v", err)
	}
	if second.Action != memsqlite.ConsolidationActionSupersede {
		t.Fatalf("second action = %q, want supersede", second.Action)
	}
	requireSQLiteFactValidity(t, db.SQLDB(), first.Fact.ID, string(core.ValidityInvalidated))
	requireSQLiteLinkCount(t, db.SQLDB(), second.Fact.ID, string(core.LinkTypeSupersedes), first.Fact.ID, 1)
	requireQueueCount(t, db.SQLDB(), "fact", first.Fact.ID, "upsert_node", 2)
}

func seedConsolidationStoreGraph(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	store := memsqlite.NewStore(db)
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}
	if err := store.EnsureSession(ctx, core.Session{ID: "s1", PersonaID: "default", Channel: core.ChannelAPI}); err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	entities := memsqlite.NewEntityRepository(db)
	for _, entity := range []core.Entity{
		{ID: "ent_user", PersonaID: "default", CanonicalName: "Long", EntityType: core.EntityTypeUser},
		{ID: "ent_shanghai", PersonaID: "default", CanonicalName: "上海", EntityType: core.EntityTypePlace},
		{ID: "ent_hangzhou", PersonaID: "default", CanonicalName: "杭州", EntityType: core.EntityTypePlace},
	} {
		if err := entities.Upsert(ctx, entity); err != nil {
			t.Fatalf("upsert entity %s: %v", entity.ID, err)
		}
	}
	episodes := memsqlite.NewEpisodeRepository(db)
	for _, episode := range []core.Episode{
		{ID: "ep_visible", PersonaID: "default", SessionID: "s1", Role: core.RoleUser, Content: "我喜欢咖啡。", OccurredAt: fixedConsolidationNow(), SourceType: core.SourceTypeChat},
		{ID: "ep_later", PersonaID: "default", SessionID: "s1", Role: core.RoleUser, Content: "我住在杭州。", OccurredAt: fixedConsolidationNow().Add(time.Hour), SourceType: core.SourceTypeChat},
	} {
		if err := episodes.Append(ctx, episode); err != nil {
			t.Fatalf("append episode %s: %v", episode.ID, err)
		}
	}
}

func fixedConsolidationIDs() func() string {
	index := 0
	return func() string {
		index++
		return fmt.Sprintf("id_%02d", index)
	}
}

func fixedConsolidationNow() time.Time {
	return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
}

func requireSQLiteFactCount(t *testing.T, db *sql.DB, predicate string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM facts WHERE predicate = ?`, predicate).Scan(&got); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if got != want {
		t.Fatalf("fact count = %d, want %d", got, want)
	}
}

func requireSQLiteFactValidity(t *testing.T, db *sql.DB, factID string, want string) {
	t.Helper()

	var got string
	var validTo sql.NullString
	if err := db.QueryRow(`SELECT validity_status, valid_to FROM facts WHERE id = ?`, factID).Scan(&got, &validTo); err != nil {
		t.Fatalf("query fact validity: %v", err)
	}
	if got != want {
		t.Fatalf("validity = %q, want %q", got, want)
	}
	if !validTo.Valid {
		t.Fatalf("valid_to is null, want invalidation time")
	}
}

func requireSQLiteLinkCount(t *testing.T, db *sql.DB, fromID string, linkType string, toID string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_links
WHERE from_node_id = ? AND link_type = ? AND to_node_id = ?`, fromID, linkType, toID).Scan(&got); err != nil {
		t.Fatalf("count link: %v", err)
	}
	if got != want {
		t.Fatalf("link count = %d, want %d", got, want)
	}
}

func requireQueueCount(t *testing.T, db *sql.DB, nodeType string, nodeID string, operation string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM index_sync_queue
WHERE node_type = ? AND node_id = ? AND operation = ?`, nodeType, nodeID, operation).Scan(&got); err != nil {
		t.Fatalf("count queue rows: %v", err)
	}
	if got != want {
		t.Fatalf("queue rows for %s/%s/%s = %d, want %d", nodeType, nodeID, operation, got, want)
	}
}
