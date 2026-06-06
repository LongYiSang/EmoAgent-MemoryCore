package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestMigrateAppliesSchemaAndSeedsPredicates(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	requireTable(t, db.SQLDB(), "personas")
	requireTable(t, db.SQLDB(), "episodes")
	requireTable(t, db.SQLDB(), "facts")
	requireTable(t, db.SQLDB(), "memory_links")
	requireTable(t, db.SQLDB(), "deletion_events")
	requireTable(t, db.SQLDB(), "memory_search_documents")
	requireTable(t, db.SQLDB(), "extraction_runs")
	requireTable(t, db.SQLDB(), "mirror_persona_state")
	requireTable(t, db.SQLDB(), "consolidation_apply_fingerprints")
	requireTable(t, db.SQLDB(), "consolidation_session_fact_writes")
	requireTable(t, db.SQLDB(), "pending_manual_forget_operations")
	requireTable(t, db.SQLDB(), "memory_natural_states")
	requireTable(t, db.SQLDB(), "memory_natural_events")
	requireTable(t, db.SQLDB(), "memory_natural_runs")
	requireTable(t, db.SQLDB(), "memory_natural_compression_candidates")
	requireTable(t, db.SQLDB(), "semantic_mirror_meta")
	requireTable(t, db.SQLDB(), "semantic_decision_audit")
	requireTable(t, db.SQLDB(), "memory_curation_checkpoints")
	requireTable(t, db.SQLDB(), "memory_curation_runs")
	requireTable(t, db.SQLDB(), "memory_curation_groups")
	requireTable(t, db.SQLDB(), "memory_curation_group_facts")

	var migrationCount int
	var migrationChecksum string
	var migrationDirty int
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT COUNT(*), MAX(checksum), MAX(dirty)
FROM schema_migrations`).Scan(&migrationCount, &migrationChecksum, &migrationDirty); err != nil {
		t.Fatalf("read migration ledger: %v", err)
	}
	if migrationCount != 2 {
		t.Fatalf("migration count = %d, want 2", migrationCount)
	}
	if migrationChecksum == "" {
		t.Fatalf("migration checksum is empty")
	}
	if migrationDirty != 0 {
		t.Fatalf("migration dirty = %d, want 0", migrationDirty)
	}
	requireColumn(t, db.SQLDB(), "pending_manual_forget_operations", "preview_hash")
	mustExec(t, db.SQLDB(), `INSERT INTO personas (id, display_name) VALUES ('default', 'Default')`)
	mustExec(t, db.SQLDB(), `INSERT INTO pending_manual_forget_operations (
		id, persona_id, status, requested_level, requires_confirmation,
		candidates_json, preview_hash, created_at, updated_at, expires_at
	) VALUES (
		'op_expired', 'default', 'expired', 'soft_forget', 1,
		'[]', 'hash-expired', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z'
	)`)

	predicates := memsqlite.NewPredicateRepository(db.SQLDB())
	predicate, err := predicates.Get(ctx, "lives_in")
	if err != nil {
		t.Fatalf("get seeded predicate: %v", err)
	}
	if predicate.ConflictPolicy != core.ConflictPolicySupersede {
		t.Fatalf("lives_in conflict policy = %q, want %q", predicate.ConflictPolicy, core.ConflictPolicySupersede)
	}
}

func TestAgentAffectPlaceholderTablesDefaultToUnsearchable(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}

	mustExec(t, db.SQLDB(), `INSERT INTO agent_affect_profiles (id, persona_id, profile_name) VALUES ('profile_1', 'default', 'default')`)
	mustExec(t, db.SQLDB(), `INSERT INTO agent_affect_states (id, persona_id, profile_id) VALUES ('state_1', 'default', 'profile_1')`)
	mustExec(t, db.SQLDB(), `INSERT INTO agent_appraisals (id, persona_id, trigger_type) VALUES ('appraisal_1', 'default', 'user_message')`)
	mustExec(t, db.SQLDB(), `INSERT INTO agent_affect_events (id, persona_id, trigger_type, appraisal_id) VALUES ('event_1', 'default', 'user_message', 'appraisal_1')`)
	mustExec(t, db.SQLDB(), `INSERT INTO agent_expression_decisions (id, persona_id, guidance_json) VALUES ('decision_1', 'default', '{}')`)

	for _, tc := range []struct {
		table string
		id    string
	}{
		{"agent_affect_profiles", "profile_1"},
		{"agent_affect_states", "state_1"},
		{"agent_appraisals", "appraisal_1"},
		{"agent_affect_events", "event_1"},
		{"agent_expression_decisions", "decision_1"},
	} {
		var searchable int
		if err := db.SQLDB().QueryRowContext(ctx, "SELECT searchable FROM "+tc.table+" WHERE id = ?", tc.id).Scan(&searchable); err != nil {
			t.Fatalf("query %s searchable: %v", tc.table, err)
		}
		if searchable != 0 {
			t.Fatalf("%s searchable = %d, want 0", tc.table, searchable)
		}
	}
}

func TestRepositoriesRoundTripCoreRows(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}
	if err := store.EnsureSession(ctx, core.Session{ID: "s1", PersonaID: "default", Channel: core.ChannelCLI}); err != nil {
		t.Fatalf("ensure session: %v", err)
	}

	episodes := memsqlite.NewEpisodeRepository(db.SQLDB())
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	first := core.Episode{
		ID:          "ep_01",
		PersonaID:   "default",
		SessionID:   "s1",
		Role:        core.RoleUser,
		Content:     "我希望你叫我 Long。",
		ContentHash: "hash_ep_01",
		OccurredAt:  now,
		SourceType:  core.SourceTypeChat,
	}
	second := core.Episode{
		ID:          "ep_02",
		PersonaID:   "default",
		SessionID:   "s1",
		Role:        core.RoleAssistant,
		Content:     "我记住了。",
		ContentHash: "hash_ep_02",
		OccurredAt:  now.Add(time.Minute),
		SourceType:  core.SourceTypeChat,
	}
	if err := episodes.Append(ctx, first); err != nil {
		t.Fatalf("append first episode: %v", err)
	}
	if err := episodes.Append(ctx, second); err != nil {
		t.Fatalf("append second episode: %v", err)
	}

	gotFirst, err := episodes.Get(ctx, "default", "ep_01")
	if err != nil {
		t.Fatalf("get first episode: %v", err)
	}
	if gotFirst.NextEpisodeID == nil || *gotFirst.NextEpisodeID != "ep_02" {
		t.Fatalf("first next episode = %v, want ep_02", gotFirst.NextEpisodeID)
	}
	gotSecond, err := episodes.Get(ctx, "default", "ep_02")
	if err != nil {
		t.Fatalf("get second episode: %v", err)
	}
	if gotSecond.PrevEpisodeID == nil || *gotSecond.PrevEpisodeID != "ep_01" {
		t.Fatalf("second previous episode = %v, want ep_01", gotSecond.PrevEpisodeID)
	}

	entities := memsqlite.NewEntityRepository(db.SQLDB())
	user := core.Entity{
		ID:            "ent_user",
		PersonaID:     "default",
		CanonicalName: "Long",
		EntityType:    core.EntityTypeUser,
	}
	if err := entities.Upsert(ctx, user); err != nil {
		t.Fatalf("upsert entity: %v", err)
	}
	if err := entities.AddAlias(ctx, core.EntityAlias{
		ID:        "alias_user_long",
		PersonaID: "default",
		EntityID:  "ent_user",
		Alias:     "Long",
		AliasType: core.AliasTypeSurface,
	}); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	resolved, err := entities.ResolveByAlias(ctx, "default", "Long")
	if err != nil {
		t.Fatalf("resolve alias: %v", err)
	}
	if resolved.ID != "ent_user" {
		t.Fatalf("resolved entity = %q, want ent_user", resolved.ID)
	}

	facts := memsqlite.NewFactRepository(db.SQLDB())
	object := "Long"
	fact := core.Fact{
		ID:                   "fact_01",
		PersonaID:            "default",
		SubjectEntityID:      ptr("ent_user"),
		Predicate:            "prefers_name",
		ObjectLiteral:        &object,
		ContentSummary:       "用户偏好被称呼为 Long。",
		FactType:             core.FactTypeCoreIdentity,
		ExtractionConfidence: core.ExtractionConfidenceExplicit,
		Importance:           0.85,
	}
	if err := facts.Insert(ctx, fact); err != nil {
		t.Fatalf("insert fact: %v", err)
	}
	gotFact, err := facts.Get(ctx, "default", "fact_01")
	if err != nil {
		t.Fatalf("get fact: %v", err)
	}
	if gotFact.ContentSummary != fact.ContentSummary {
		t.Fatalf("fact summary = %q, want %q", gotFact.ContentSummary, fact.ContentSummary)
	}
	assertTextHasOffset(t, db.SQLDB(), "SELECT created_at FROM facts WHERE id = 'fact_01'", "+08:00")

	links := memsqlite.NewLinkRepository(db.SQLDB())
	if err := links.Insert(ctx, core.MemoryLink{
		ID:           "link_fact_entity",
		PersonaID:    "default",
		FromNodeType: core.NodeTypeFact,
		FromNodeID:   "fact_01",
		LinkType:     core.LinkTypeAboutEntity,
		ToNodeType:   core.NodeTypeEntity,
		ToNodeID:     "ent_user",
		CreatedBy:    core.LinkCreatedBySystem,
	}); err != nil {
		t.Fatalf("insert link: %v", err)
	}
	fromLinks, err := links.ListFrom(ctx, "default", core.NodeTypeFact, "fact_01")
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(fromLinks) != 1 || fromLinks[0].ToNodeID != "ent_user" {
		t.Fatalf("links from fact = %#v, want one ABOUT_ENTITY link to ent_user", fromLinks)
	}

	search := memsqlite.NewSearchRepository(db.SQLDB())
	if err := search.UpsertDocument(ctx, core.SearchDocument{
		ID:         "search_fact_01",
		PersonaID:  "default",
		NodeType:   core.NodeTypeFact,
		NodeID:     "fact_01",
		SearchText: "用户偏好被称呼为 Long。",
		SearchTier: core.SearchTierHot,
	}); err != nil {
		t.Fatalf("upsert search document: %v", err)
	}
	docs, err := search.KeywordSearch(ctx, "default", "Long", 10)
	if err != nil {
		t.Fatalf("keyword search: %v", err)
	}
	if len(docs) != 1 || docs[0].NodeID != "fact_01" {
		t.Fatalf("keyword docs = %#v, want fact_01", docs)
	}
}

func openMigratedDB(t *testing.T, ctx context.Context) *memsqlite.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func formatSQLiteTestTime(value time.Time) string {
	return value.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.RFC3339Nano)
}

func requireTable(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type IN ('table', 'virtual table') AND name = ?`, table).Scan(&name)
	if err != nil {
		t.Fatalf("table %s does not exist: %v", table, err)
	}
}

func requireColumn(t *testing.T, db *sql.DB, table string, column string) {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("inspect columns for %s: %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan column for %s: %v", table, err)
		}
		if name == column {
			return
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read columns for %s: %v", table, err)
	}
	t.Fatalf("column %s.%s does not exist", table, column)
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()

	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func ptr(value string) *string {
	return &value
}
