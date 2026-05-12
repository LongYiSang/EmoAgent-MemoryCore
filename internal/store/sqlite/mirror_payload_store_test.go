package sqlite_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestMirrorPayloadRepositoryBuildsSafeNodePayloads(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedMirrorPayloadFixture(t, ctx, db.SQLDB())

	repo := memsqlite.NewMirrorPayloadRepository(db.SQLDB())

	for _, tc := range []struct {
		name               string
		nodeType           string
		nodeID             string
		wantSearchContains string
	}{
		{name: "entity", nodeType: "entity", nodeID: "ent_user", wantSearchContains: "Long"},
		{name: "fact", nodeType: "fact", nodeID: "fact_visible", wantSearchContains: "用户喜欢咖啡"},
		{name: "narrative", nodeType: "narrative", nodeID: "narrative_week", wantSearchContains: "本周关系"},
		{name: "insight", nodeType: "insight", nodeID: "insight_pref", wantSearchContains: "喜欢安静"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, ok, err := repo.BuildNodePayload(ctx, "default", tc.nodeType, tc.nodeID)
			if err != nil {
				t.Fatalf("build node payload: %v", err)
			}
			if !ok {
				t.Fatalf("payload ok = false, want true")
			}
			if payload.PersonaID != "default" || payload.NodeType != tc.nodeType || payload.SQLiteNodeID != tc.nodeID {
				t.Fatalf("payload identity = %#v", payload)
			}
			if !strings.Contains(payload.SearchableText, tc.wantSearchContains) {
				t.Fatalf("searchable text %q does not contain %q", payload.SearchableText, tc.wantSearchContains)
			}
			forbiddenKeys := []string{"object_literal", "extraction_reasoning", "episode_content", "content"}
			for _, key := range forbiddenKeys {
				if _, exists := payload.Payload[key]; exists {
					t.Fatalf("payload includes forbidden key %q: %#v", key, payload.Payload)
				}
			}
		})
	}
}

func TestMirrorPayloadRepositoryExcludesUnsafeNodes(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedMirrorPayloadFixture(t, ctx, db.SQLDB())

	for _, tc := range []struct {
		name      string
		nodeType  string
		nodeID    string
		updateSQL string
	}{
		{name: "hidden fact", nodeType: "fact", nodeID: "fact_visible", updateSQL: "UPDATE facts SET visibility_status = 'hidden' WHERE id = 'fact_visible'"},
		{name: "forgotten fact", nodeType: "fact", nodeID: "fact_visible", updateSQL: "UPDATE facts SET visibility_status = 'forgotten' WHERE id = 'fact_visible'"},
		{name: "purged fact", nodeType: "fact", nodeID: "fact_visible", updateSQL: "UPDATE facts SET visibility_status = 'purged' WHERE id = 'fact_visible'"},
		{name: "unsearchable fact", nodeType: "fact", nodeID: "fact_visible", updateSQL: "UPDATE facts SET searchable = 0 WHERE id = 'fact_visible'"},
		{name: "hidden entity", nodeType: "entity", nodeID: "ent_user", updateSQL: "UPDATE entities SET visibility_status = 'hidden' WHERE id = 'ent_user'"},
		{name: "purged narrative", nodeType: "narrative", nodeID: "narrative_week", updateSQL: "UPDATE narratives SET visibility_status = 'purged' WHERE id = 'narrative_week'"},
		{name: "unsearchable insight", nodeType: "insight", nodeID: "insight_pref", updateSQL: "UPDATE insights SET searchable = 0 WHERE id = 'insight_pref'"},
		{name: "raw episode skipped", nodeType: "episode", nodeID: "ep_private", updateSQL: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			localDB := openMigratedDB(t, ctx)
			defer localDB.Close()
			seedMirrorPayloadFixture(t, ctx, localDB.SQLDB())
			localRepo := memsqlite.NewMirrorPayloadRepository(localDB.SQLDB())
			if tc.updateSQL != "" {
				if _, err := localDB.SQLDB().ExecContext(ctx, tc.updateSQL); err != nil {
					t.Fatalf("update fixture: %v", err)
				}
			}
			_, ok, err := localRepo.BuildNodePayload(ctx, "default", tc.nodeType, tc.nodeID)
			if err != nil {
				t.Fatalf("build node payload: %v", err)
			}
			if ok {
				t.Fatalf("payload ok = true, want false")
			}
		})
	}
}

func seedMirrorPayloadFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO personas(id, display_name)
VALUES ('default', 'Default')`)
	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO sessions(id, persona_id, channel, started_at)
VALUES ('session_1', 'default', 'cli', '2026-05-13T09:00:00Z')`)
	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO episodes(id, persona_id, session_id, role, content, content_hash, occurred_at, source_type, visibility_status, sensitivity_level, searchable)
VALUES ('ep_private', 'default', 'session_1', 'user', 'raw private episode content must not be mirrored', 'hash_ep_private', '2026-05-13T09:01:00Z', 'chat', 'visible', 'sensitive', 1)`)
	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO entities(id, persona_id, canonical_name, entity_type, description, visibility_status, sensitivity_level, searchable)
VALUES ('ent_user', 'default', 'Long', 'user', '主用户', 'visible', 'normal', 1)`)
	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO entity_aliases(id, persona_id, entity_id, alias, alias_type, confidence)
VALUES ('alias_long', 'default', 'ent_user', 'LongYi', 'nickname', 0.9)`)
	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO facts (
    id, persona_id, subject_entity_id, predicate, object_literal, content_summary,
    fact_type, extraction_confidence, extraction_confidence_score, extraction_reasoning,
    importance, valence, arousal, sensitivity_level, validity_status,
    visibility_status, lifecycle_status, searchable
) VALUES (
    'fact_visible', 'default', 'ent_user', 'likes', '咖啡', '用户喜欢咖啡。',
    'stable_preference', 'explicit', 0.95, 'private extraction reasoning',
    0.7, 0.4, 0.2, 'normal', 'valid',
    'visible', 'active', 1
)`)
	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO narratives (
    id, persona_id, scope, scope_ref, summary, emotional_tone,
    importance, generated_at, visibility_status, lifecycle_status,
    sensitivity_level, searchable
) VALUES (
    'narrative_week', 'default', 'week', '2026-W20', '本周关系整体稳定。',
    'calm', 0.6, '2026-05-13T09:02:00Z', 'visible', 'active',
    'normal', 1
)`)
	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO insights (
    id, persona_id, insight_type, content, confidence, importance,
    valence, arousal, created_at, visibility_status, lifecycle_status,
    sensitivity_level, searchable
) VALUES (
    'insight_pref', 'default', 'preference', '用户喜欢安静的沟通节奏。',
    0.8, 0.65, 0.3, 0.2, '2026-05-13T09:03:00Z', 'visible', 'active',
    'normal', 1
)`)
	mustExecMirrorPayload(t, ctx, db, `
INSERT INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    reasoning, created_by, visibility_status, searchable
) VALUES (
    'link_fact_entity', 'default', 'fact', 'fact_visible', 'ABOUT_ENTITY',
    'entity', 'ent_user', 'forward', 1.0, 1.0,
    'private link reasoning must not be mirrored', 'consolidation', 'visible', 1
)`)
}

func mustExecMirrorPayload(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatalf("exec fixture: %v\nquery: %s", err, query)
	}
}

func TestMirrorPayloadRepositoryBuildsVisibleSearchableEdgePayload(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedMirrorPayloadFixture(t, ctx, db.SQLDB())

	repo := memsqlite.NewMirrorPayloadRepository(db.SQLDB())
	payload, ok, err := repo.BuildEdgePayload(ctx, "default", "link_fact_entity")
	if err != nil {
		t.Fatalf("build edge payload: %v", err)
	}
	if !ok {
		t.Fatalf("payload ok = false, want true")
	}
	if payload.PersonaID != "default" || payload.SQLiteEdgeID != "link_fact_entity" {
		t.Fatalf("payload identity = %#v", payload)
	}
	if payload.FromNodeType != "fact" || payload.FromNodeID != "fact_visible" || payload.ToNodeType != "entity" || payload.ToNodeID != "ent_user" {
		t.Fatalf("payload endpoints = %#v", payload)
	}
	if payload.LinkType != "ABOUT_ENTITY" {
		t.Fatalf("link type = %q, want ABOUT_ENTITY", payload.LinkType)
	}

	if _, err := db.SQLDB().ExecContext(ctx, `UPDATE memory_links SET searchable = 0 WHERE id = 'link_fact_entity'`); err != nil {
		t.Fatalf("hide link: %v", err)
	}
	_, ok, err = repo.BuildEdgePayload(ctx, "default", "link_fact_entity")
	if err != nil {
		t.Fatalf("build hidden edge payload: %v", err)
	}
	if ok {
		t.Fatalf("hidden edge payload ok = true, want false")
	}
}
