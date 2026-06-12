package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestObservabilityRepositoryCountsByPersona(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	now := fixedObservabilityNow()
	since := now.Add(-24 * time.Hour)
	seedObservabilityPersona(t, ctx, db.SQLDB(), "default", "visible", "active")
	seedObservabilityPersona(t, ctx, db.SQLDB(), "other", "hidden", "archived")

	repo := memsqlite.NewObservabilityRepositoryWithOptions(db.SQLDB(), memsqlite.StoreOptions{Now: func() time.Time { return now }})
	snapshot, err := repo.Snapshot(ctx, memsqlite.ObservabilitySnapshotRequest{
		PersonaID:        "default",
		Since:            since,
		MirrorConfigured: true,
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if snapshot.PersonaID != "default" || snapshot.Status != "ok" {
		t.Fatalf("snapshot identity/status = %q/%q", snapshot.PersonaID, snapshot.Status)
	}
	if snapshot.Store.PersonaCount != 1 || snapshot.Store.SessionCount != 1 {
		t.Fatalf("store counts = %#v", snapshot.Store)
	}
	if snapshot.Store.EpisodeByVisibility["visible"] != 1 || snapshot.Store.EpisodeByVisibility["hidden"] != 0 {
		t.Fatalf("episode visibility = %#v", snapshot.Store.EpisodeByVisibility)
	}
	if snapshot.Store.FactByLifecycle["active"] != 1 || snapshot.Store.FactByLifecycle["archived"] != 0 {
		t.Fatalf("fact lifecycle = %#v", snapshot.Store.FactByLifecycle)
	}
	if snapshot.Store.SearchDocumentByTier["hot"] != 1 {
		t.Fatalf("search tiers = %#v", snapshot.Store.SearchDocumentByTier)
	}
	if snapshot.Retrieval.AccessEventsByType["retrieved"] != 1 ||
		snapshot.Retrieval.RecentRetrievedCount != 1 ||
		snapshot.Retrieval.RecentInjectedCount != 1 ||
		snapshot.Retrieval.RecentSuppressedCount != 1 {
		t.Fatalf("retrieval = %#v", snapshot.Retrieval)
	}
	if snapshot.Extraction.RunsByStatus["failed"] != 1 ||
		snapshot.Extraction.RecentRunCount != 1 ||
		snapshot.Extraction.RecentFailedCount != 1 {
		t.Fatalf("extraction = %#v", snapshot.Extraction)
	}
	if snapshot.Forgetting.DeletionEventsByLevel["soft_forget"] != 1 ||
		snapshot.Forgetting.RecentDeletionCount != 1 ||
		snapshot.Forgetting.PendingManualCount != 1 {
		t.Fatalf("forgetting = %#v", snapshot.Forgetting)
	}
	if snapshot.NaturalMemory.RunsByStatus["completed"] != 1 ||
		snapshot.NaturalMemory.RecentRunCount != 1 ||
		snapshot.NaturalMemory.RecentDecayedNodes != 2 ||
		snapshot.NaturalMemory.RecentProtectedNodes != 3 {
		t.Fatalf("natural memory = %#v", snapshot.NaturalMemory)
	}
	if snapshot.Retention.RecentRunCount != 0 || len(snapshot.Retention.JobsByStatus) != 0 {
		t.Fatalf("retention = %#v", snapshot.Retention)
	}
}

func TestObservabilityRepositoryReportsMirrorQueueAndPersonaState(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	now := fixedObservabilityNow()
	seedObservabilityPersona(t, ctx, db.SQLDB(), "default", "visible", "active")
	mustExec(t, db.SQLDB(), `
UPDATE mirror_persona_state SET state = 'degraded', updated_at = ? WHERE persona_id = 'default'`, formatSQLiteTestTime(now))

	repo := memsqlite.NewObservabilityRepositoryWithOptions(db.SQLDB(), memsqlite.StoreOptions{Now: func() time.Time { return now }})
	snapshot, err := repo.Snapshot(ctx, memsqlite.ObservabilitySnapshotRequest{PersonaID: "default", Since: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if !snapshot.Mirror.EnabledOrConfigured {
		t.Fatalf("mirror enabled/configured = false, want true")
	}
	if snapshot.Mirror.QueueByOperation["upsert_node"] != 1 || snapshot.Mirror.QueueByStatus["pending"] != 1 {
		t.Fatalf("mirror queue = %#v", snapshot.Mirror)
	}
	if snapshot.Mirror.PendingCount != 1 || snapshot.Mirror.FailedCount != 1 {
		t.Fatalf("mirror pending/failed = %d/%d", snapshot.Mirror.PendingCount, snapshot.Mirror.FailedCount)
	}
	if snapshot.Mirror.PersonaReady || !snapshot.Mirror.PersonaDegraded {
		t.Fatalf("mirror persona ready/degraded = %t/%t", snapshot.Mirror.PersonaReady, snapshot.Mirror.PersonaDegraded)
	}
	if snapshot.Mirror.LastSyncAt == nil || !snapshot.Mirror.LastSyncAt.Equal(now.Add(-time.Hour)) {
		t.Fatalf("mirror last sync = %v", snapshot.Mirror.LastSyncAt)
	}
}

func TestObservabilityRepositoryReportsMirrorConfiguredFromStateOnly(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	now := fixedObservabilityNow()
	mustExec(t, db.SQLDB(), `INSERT INTO personas (id, display_name) VALUES ('default', 'default')`)
	mustExec(t, db.SQLDB(), `
INSERT INTO mirror_persona_state (persona_id, state, updated_at)
VALUES ('default', 'ready', ?)`, formatSQLiteTestTime(now))

	repo := memsqlite.NewObservabilityRepositoryWithOptions(db.SQLDB(), memsqlite.StoreOptions{Now: func() time.Time { return now }})
	snapshot, err := repo.Snapshot(ctx, memsqlite.ObservabilitySnapshotRequest{PersonaID: "default", Since: now.Add(-24 * time.Hour)})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if !snapshot.Mirror.EnabledOrConfigured {
		t.Fatalf("mirror enabled/configured = false, want true")
	}
	if !snapshot.Mirror.PersonaReady || snapshot.Mirror.PersonaDegraded {
		t.Fatalf("mirror persona ready/degraded = %t/%t", snapshot.Mirror.PersonaReady, snapshot.Mirror.PersonaDegraded)
	}
	if snapshot.Mirror.PendingCount != 0 || snapshot.Mirror.FailedCount != 0 || snapshot.Mirror.LastSyncAt != nil {
		t.Fatalf("mirror should only report state: %#v", snapshot.Mirror)
	}
}

func TestObservabilityRepositoryHandlesFTSDisabled(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.MigrateWithOptions(ctx, memsqlite.MigrateOptions{EnableFTS: false}); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	repo := memsqlite.NewObservabilityRepository(db.SQLDB())
	snapshot, err := repo.Snapshot(ctx, memsqlite.ObservabilitySnapshotRequest{PersonaID: "default"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Store.FTSAvailable {
		t.Fatalf("fts available = true, want false")
	}
}

func TestObservabilityRepositoryDoesNotSelectSensitiveColumns(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	seedObservabilityPersona(t, ctx, db.SQLDB(), "default", "visible", "active")
	mustExec(t, db.SQLDB(), `
INSERT INTO narratives (id, persona_id, scope, summary)
VALUES ('nar_secret', 'default', 'topic', 'private narrative summary')`)
	mustExec(t, db.SQLDB(), `
INSERT INTO insights (id, persona_id, insight_type, content)
VALUES ('ins_secret', 'default', 'pattern', 'private insight content')`)

	repo := memsqlite.NewObservabilityRepository(db.SQLDB())
	snapshot, err := repo.Snapshot(ctx, memsqlite.ObservabilitySnapshotRequest{PersonaID: "default"})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	text := string(data)
	for _, secret := range []string{
		"episode secret content",
		"fact secret summary",
		"fact secret literal",
		"private narrative summary",
		"private insight content",
	} {
		if strings.Contains(text, secret) {
			t.Fatalf("snapshot leaked %q: %s", secret, text)
		}
	}
}

func seedObservabilityPersona(t *testing.T, ctx context.Context, db *sql.DB, personaID string, episodeVisibility string, factLifecycle string) {
	t.Helper()

	now := fixedObservabilityNow()
	createdAt := formatSQLiteTestTime(now.Add(-time.Hour))
	mustExec(t, db, `INSERT INTO personas (id, display_name) VALUES (?, ?)`, personaID, personaID)
	mustExec(t, db, `
INSERT INTO sessions (id, persona_id, channel, started_at)
VALUES (?, ?, 'api', ?)`, personaID+"_session", personaID, createdAt)
	mustExec(t, db, `
INSERT INTO episodes (id, persona_id, session_id, role, content, content_hash, occurred_at, source_type, visibility_status)
VALUES (?, ?, ?, 'user', 'episode secret content', ?, ?, 'chat', ?)`,
		personaID+"_episode", personaID, personaID+"_session", personaID+"_episode_hash", createdAt, episodeVisibility)
	mustExec(t, db, `
INSERT INTO facts (
	id, persona_id, predicate, object_literal, content_summary, fact_type,
	extraction_confidence, visibility_status, lifecycle_status, sensitivity_level
) VALUES (?, ?, 'likes', 'fact secret literal', 'fact secret summary', 'stable_preference',
	'explicit', 'visible', ?, 'normal')`,
		personaID+"_fact", personaID, factLifecycle)
	mustExec(t, db, `
INSERT INTO memory_search_documents (id, persona_id, node_type, node_id, search_text, search_tier)
VALUES (?, ?, 'fact', ?, 'search text', 'hot')`, personaID+"_doc", personaID, personaID+"_fact")
	for _, accessType := range []string{"retrieved", "prompt_injected", "suppressed"} {
		mustExec(t, db, `
INSERT INTO memory_access_events (id, persona_id, node_type, node_id, access_type, created_at)
VALUES (?, ?, 'fact', ?, ?, ?)`, personaID+"_access_"+accessType, personaID, personaID+"_fact", accessType, createdAt)
	}
	mustExec(t, db, `
INSERT INTO extraction_runs (id, request_id, persona_id, trigger, mode, status, fingerprint, created_at, updated_at)
VALUES (?, ?, ?, 'session_end', 'apply', 'failed', ?, ?, ?)`,
		personaID+"_extract", personaID+"_request", personaID, personaID+"_fingerprint", createdAt, createdAt)
	mustExec(t, db, `
INSERT INTO deletion_events (id, persona_id, deletion_level, actor, reason_code, status, created_at)
VALUES (?, ?, 'soft_forget', 'user', 'user_requested', 'completed', ?)`,
		personaID+"_deletion", personaID, createdAt)
	mustExec(t, db, `
INSERT INTO pending_manual_forget_operations (
	id, persona_id, status, requested_level, requires_confirmation,
	candidates_json, created_at, updated_at, expires_at
) VALUES (?, ?, 'pending_confirmation', 'soft_forget', 1, '[]', ?, ?, ?)`,
		personaID+"_pending", personaID, createdAt, createdAt, formatSQLiteTestTime(now.Add(time.Hour)))
	mustExec(t, db, `
INSERT INTO memory_natural_runs (
	id, persona_id, run_kind, started_at, completed_at, status, decayed_nodes, protected_nodes
) VALUES (?, ?, 'manual', ?, ?, 'completed', 2, 3)`,
		personaID+"_natural", personaID, createdAt, createdAt)
	mustExec(t, db, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation, status, updated_at)
VALUES (?, ?, 'fact', ?, 'upsert_node', 'pending', ?)`,
		personaID+"_queue_pending", personaID, personaID+"_fact", createdAt)
	mustExec(t, db, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation, status, updated_at)
VALUES (?, ?, 'fact', ?, 'delete_node', 'failed', ?)`,
		personaID+"_queue_failed", personaID, personaID+"_fact", createdAt)
	mustExec(t, db, `
INSERT INTO memory_index_map (persona_id, node_type, node_id, trivium_node_id, index_status, indexed_at, updated_at)
VALUES (?, 'fact', ?, ?, 'indexed', ?, ?)`,
		personaID, personaID+"_fact", observabilityTriviumID(personaID), formatSQLiteTestTime(now.Add(-time.Hour)), createdAt)
	mustExec(t, db, `
INSERT INTO mirror_persona_state (persona_id, state, updated_at)
VALUES (?, 'ready', ?)`, personaID, createdAt)
}

func observabilityTriviumID(personaID string) int {
	if personaID == "default" {
		return 1001
	}
	return 1002
}

func fixedObservabilityNow() time.Time {
	return time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
}
