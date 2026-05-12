package sqlite_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestMirrorQueueClaimOrdersByPriorityThenCreatedAt(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	base := time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC)
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_later_high_priority",
		nodeType:  "fact",
		nodeID:    "fact_later_high_priority",
		operation: "upsert_node",
		priority:  1,
		status:    "pending",
		createdAt: base.Add(2 * time.Minute),
	})
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_earlier_high_priority",
		nodeType:  "fact",
		nodeID:    "fact_earlier_high_priority",
		operation: "delete_node",
		priority:  1,
		status:    "pending",
		createdAt: base.Add(time.Minute),
	})
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_low_priority",
		nodeType:  "memory_link",
		nodeID:    "link_low_priority",
		operation: "upsert_edge",
		priority:  5,
		status:    "pending",
		createdAt: base,
	})

	repo := memsqlite.NewMirrorQueueRepository(db.SQLDB())
	claimed, err := repo.Claim(ctx, 2)
	if err != nil {
		t.Fatalf("claim queue rows: %v", err)
	}

	if got, want := mirrorQueueIDs(claimed), []string{"q_earlier_high_priority", "q_later_high_priority"}; !equalStrings(got, want) {
		t.Fatalf("claimed ids = %#v, want %#v", got, want)
	}
	for _, row := range claimed {
		if row.Status != memsqlite.MirrorQueueStatusProcessing {
			t.Fatalf("claimed row %s status = %q, want %q", row.ID, row.Status, memsqlite.MirrorQueueStatusProcessing)
		}
		if row.ErrorMessage != "" {
			t.Fatalf("claimed row %s error_message = %q, want cleared", row.ID, row.ErrorMessage)
		}
		requireMirrorQueueStatus(t, ctx, db, row.ID, "processing")
	}
	requireMirrorQueueStatus(t, ctx, db, "q_low_priority", "pending")
}

func TestMirrorQueueFailedRowsAreRetriedAndFailureIncrementsAttempts(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:           "q_failed",
		nodeType:     "fact",
		nodeID:       "fact_failed",
		operation:    "upsert_node",
		priority:     3,
		status:       "failed",
		attempts:     2,
		createdAt:    time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC),
		errorMessage: "previous error",
	})

	repo := memsqlite.NewMirrorQueueRepository(db.SQLDB())
	claimed, err := repo.Claim(ctx, 1)
	if err != nil {
		t.Fatalf("claim failed queue row: %v", err)
	}
	if got, want := mirrorQueueIDs(claimed), []string{"q_failed"}; !equalStrings(got, want) {
		t.Fatalf("claimed ids = %#v, want %#v", got, want)
	}
	if claimed[0].Attempts != 2 {
		t.Fatalf("claimed attempts = %d, want previous attempts 2", claimed[0].Attempts)
	}
	if claimed[0].ErrorMessage != "" {
		t.Fatalf("claimed error_message = %q, want cleared", claimed[0].ErrorMessage)
	}

	longMessage := "sidecar failed\nwith private-ish stack\ttrace " + strings.Repeat("x", 600)
	if err := repo.Fail(ctx, "q_failed", longMessage); err != nil {
		t.Fatalf("fail queue row: %v", err)
	}

	var status, errorMessage string
	var attempts int
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT status, attempts, COALESCE(error_message, '')
FROM index_sync_queue
WHERE id = ?`, "q_failed").Scan(&status, &attempts, &errorMessage); err != nil {
		t.Fatalf("read failed row: %v", err)
	}
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if strings.ContainsAny(errorMessage, "\r\n\t") {
		t.Fatalf("error_message contains raw control whitespace: %q", errorMessage)
	}
	if len(errorMessage) > 512 {
		t.Fatalf("error_message length = %d, want <= 512", len(errorMessage))
	}

	reclaimed, err := repo.Claim(ctx, 1)
	if err != nil {
		t.Fatalf("reclaim failed queue row: %v", err)
	}
	if got, want := mirrorQueueIDs(reclaimed), []string{"q_failed"}; !equalStrings(got, want) {
		t.Fatalf("reclaimed ids = %#v, want %#v", got, want)
	}
}

func TestMirrorQueueFailureSanitizesPayloadLikeErrors(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_payload_error",
		nodeType:  "fact",
		nodeID:    "fact_payload_error",
		operation: "upsert_node",
		priority:  1,
		status:    "processing",
		createdAt: time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC),
	})

	repo := memsqlite.NewMirrorQueueRepository(db.SQLDB())
	raw := `adapter failed: status 500 body={"searchable_text":"用户喜欢咖啡。","api_key":"sk-secret","path":"C:\\Users\\secret\\file.json"}`
	if err := repo.Fail(ctx, "q_payload_error", raw); err != nil {
		t.Fatalf("fail queue row: %v", err)
	}

	var errorMessage string
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT COALESCE(error_message, '')
FROM index_sync_queue
WHERE id = 'q_payload_error'`).Scan(&errorMessage); err != nil {
		t.Fatalf("query sanitized error: %v", err)
	}
	for _, forbidden := range []string{"用户喜欢咖啡", "sk-secret", "searchable_text", "C:\\\\Users", "file.json", "body="} {
		if strings.Contains(errorMessage, forbidden) {
			t.Fatalf("sanitized error %q still contains forbidden %q", errorMessage, forbidden)
		}
	}
	if !strings.Contains(errorMessage, "adapter error") || !strings.Contains(errorMessage, "status 500") {
		t.Fatalf("sanitized error = %q, want adapter category and status code", errorMessage)
	}
}

func TestMirrorQueueClaimSkipsDoneAndProcessingRows(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	base := time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC)
	freshUpdatedAt := time.Now().UTC()
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_done",
		nodeType:  "fact",
		nodeID:    "fact_done",
		operation: "upsert_node",
		priority:  0,
		status:    "done",
		createdAt: base,
	})
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_processing",
		nodeType:  "fact",
		nodeID:    "fact_processing",
		operation: "upsert_node",
		priority:  0,
		status:    "processing",
		createdAt: base.Add(time.Second),
		updatedAt: &freshUpdatedAt,
	})
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_pending",
		nodeType:  "persona",
		nodeID:    "default",
		operation: "rebuild_persona",
		priority:  9,
		status:    "pending",
		createdAt: base.Add(2 * time.Second),
	})

	repo := memsqlite.NewMirrorQueueRepository(db.SQLDB())
	claimed, err := repo.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("claim queue rows: %v", err)
	}
	if got, want := mirrorQueueIDs(claimed), []string{"q_pending"}; !equalStrings(got, want) {
		t.Fatalf("claimed ids = %#v, want %#v", got, want)
	}

	if err := repo.Complete(ctx, "q_pending"); err != nil {
		t.Fatalf("complete pending row: %v", err)
	}
	requireMirrorQueueStatus(t, ctx, db, "q_pending", "done")
	requireMirrorQueueStatus(t, ctx, db, "q_done", "done")
	requireMirrorQueueStatus(t, ctx, db, "q_processing", "processing")

	claimedAgain, err := repo.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("claim queue rows again: %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("claimed rows after only done/processing remain = %#v, want none", claimedAgain)
	}
}

func TestMirrorQueueClaimExpiresStaleProcessingRows(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	now := time.Now().UTC()
	staleUpdatedAt := now.Add(-16 * time.Minute)
	freshUpdatedAt := now.Add(-5 * time.Minute)
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_pending_before_expired",
		nodeType:  "fact",
		nodeID:    "fact_pending_before_expired",
		operation: "upsert_node",
		priority:  0,
		status:    "pending",
		createdAt: now.Add(-time.Minute),
	})
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:           "q_stale_processing_updated",
		nodeType:     "fact",
		nodeID:       "fact_stale_processing_updated",
		operation:    "upsert_node",
		priority:     5,
		status:       "processing",
		attempts:     2,
		createdAt:    now.Add(-time.Hour),
		updatedAt:    &staleUpdatedAt,
		errorMessage: "worker interrupted",
	})
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:            "q_stale_processing_created",
		nodeType:      "fact",
		nodeID:        "fact_stale_processing_created",
		operation:     "delete_node",
		priority:      6,
		status:        "processing",
		attempts:      4,
		createdAt:     now.Add(-20 * time.Minute),
		updatedAtNull: true,
		errorMessage:  "worker interrupted",
	})
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:           "q_fresh_processing",
		nodeType:     "fact",
		nodeID:       "fact_fresh_processing",
		operation:    "upsert_node",
		priority:     1,
		status:       "processing",
		attempts:     7,
		createdAt:    now.Add(-time.Hour),
		updatedAt:    &freshUpdatedAt,
		errorMessage: "still leased",
	})

	repo := memsqlite.NewMirrorQueueRepository(db.SQLDB())
	claimed, err := repo.Claim(ctx, 1)
	if err != nil {
		t.Fatalf("claim pending queue row: %v", err)
	}
	if got, want := mirrorQueueIDs(claimed), []string{"q_pending_before_expired"}; !equalStrings(got, want) {
		t.Fatalf("claimed ids = %#v, want %#v", got, want)
	}

	requireMirrorQueueState(t, ctx, db, "q_stale_processing_updated", "failed", 3, "mirror queue lease expired")
	requireMirrorQueueState(t, ctx, db, "q_stale_processing_created", "failed", 5, "mirror queue lease expired")
	requireMirrorQueueState(t, ctx, db, "q_fresh_processing", "processing", 7, "still leased")

	reclaimed, err := repo.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("claim expired queue rows: %v", err)
	}
	if got, want := mirrorQueueIDs(reclaimed), []string{"q_stale_processing_updated", "q_stale_processing_created"}; !equalStrings(got, want) {
		t.Fatalf("reclaimed ids = %#v, want %#v", got, want)
	}
	requireMirrorQueueState(t, ctx, db, "q_stale_processing_updated", "processing", 3, "")
	requireMirrorQueueState(t, ctx, db, "q_stale_processing_created", "processing", 5, "")
	requireMirrorQueueState(t, ctx, db, "q_fresh_processing", "processing", 7, "still leased")
}

func TestMirrorQueueClaimDoesNotExpireFreshSQLiteTimestampWithNullUpdatedAt(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)

	if _, err := db.SQLDB().ExecContext(ctx, `
INSERT INTO index_sync_queue (
    id, persona_id, node_type, node_id, operation, priority, status, attempts, created_at, updated_at, error_message
) VALUES (
    'q_fresh_sqlite_timestamp', 'default', 'fact', 'fact_fresh_sqlite_timestamp',
    'delete_node', 0, 'processing', 1, CURRENT_TIMESTAMP, NULL, 'still leased'
)`); err != nil {
		t.Fatalf("insert fresh sqlite timestamp row: %v", err)
	}
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_pending",
		nodeType:  "fact",
		nodeID:    "fact_pending",
		operation: "upsert_node",
		priority:  1,
		status:    "pending",
		createdAt: time.Now().UTC(),
	})

	repo := memsqlite.NewMirrorQueueRepository(db.SQLDB())
	claimed, err := repo.Claim(ctx, 10)
	if err != nil {
		t.Fatalf("claim queue rows: %v", err)
	}
	if got, want := mirrorQueueIDs(claimed), []string{"q_pending"}; !equalStrings(got, want) {
		t.Fatalf("claimed ids = %#v, want %#v", got, want)
	}
	requireMirrorQueueState(t, ctx, db, "q_fresh_sqlite_timestamp", "processing", 1, "still leased")
}

func TestMirrorQueueClaimForPersonaOnlyClaimsThatPersona(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)
	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "other", DisplayName: "Other"}); err != nil {
		t.Fatalf("ensure other persona: %v", err)
	}

	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:        "q_default",
		nodeType:  "fact",
		nodeID:    "fact_default",
		operation: "upsert_node",
		priority:  1,
		status:    "pending",
		createdAt: time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC),
	})
	insertMirrorQueueRowForPersona(t, ctx, db, "other", mirrorQueueSeed{
		id:        "q_other",
		nodeType:  "fact",
		nodeID:    "fact_other",
		operation: "upsert_node",
		priority:  0,
		status:    "pending",
		createdAt: time.Date(2026, 5, 13, 9, 0, 0, 0, time.UTC),
	})

	repo := memsqlite.NewMirrorQueueRepository(db.SQLDB())
	claimed, err := repo.ClaimForPersona(ctx, "default", 10)
	if err != nil {
		t.Fatalf("claim persona queue rows: %v", err)
	}
	if got, want := mirrorQueueIDs(claimed), []string{"q_default"}; !equalStrings(got, want) {
		t.Fatalf("claimed ids = %#v, want %#v", got, want)
	}
	requireMirrorQueueStatus(t, ctx, db, "q_default", "processing")
	requireMirrorQueueStatus(t, ctx, db, "q_other", "pending")
}

func TestMirrorQueueClaimForPersonaCanClaimExpiredProcessingRows(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	ensureMirrorQueuePersona(t, ctx, db)
	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "other", DisplayName: "Other"}); err != nil {
		t.Fatalf("ensure other persona: %v", err)
	}

	now := time.Now().UTC()
	staleUpdatedAt := now.Add(-16 * time.Minute)
	freshUpdatedAt := now.Add(-5 * time.Minute)
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:           "q_default_expired_processing",
		nodeType:     "fact",
		nodeID:       "fact_default_expired_processing",
		operation:    "upsert_node",
		priority:     1,
		status:       "processing",
		attempts:     1,
		createdAt:    now.Add(-time.Hour),
		updatedAt:    &staleUpdatedAt,
		errorMessage: "worker interrupted",
	})
	insertMirrorQueueRow(t, ctx, db, mirrorQueueSeed{
		id:           "q_default_fresh_processing",
		nodeType:     "fact",
		nodeID:       "fact_default_fresh_processing",
		operation:    "upsert_node",
		priority:     0,
		status:       "processing",
		attempts:     3,
		createdAt:    now.Add(-time.Hour),
		updatedAt:    &freshUpdatedAt,
		errorMessage: "still leased",
	})
	insertMirrorQueueRowForPersona(t, ctx, db, "other", mirrorQueueSeed{
		id:           "q_other_expired_processing",
		nodeType:     "fact",
		nodeID:       "fact_other_expired_processing",
		operation:    "upsert_node",
		priority:     0,
		status:       "processing",
		attempts:     2,
		createdAt:    now.Add(-time.Hour),
		updatedAt:    &staleUpdatedAt,
		errorMessage: "worker interrupted",
	})

	repo := memsqlite.NewMirrorQueueRepository(db.SQLDB())
	claimedDefault, err := repo.ClaimForPersona(ctx, "default", 10)
	if err != nil {
		t.Fatalf("claim default expired queue rows: %v", err)
	}
	if got, want := mirrorQueueIDs(claimedDefault), []string{"q_default_expired_processing"}; !equalStrings(got, want) {
		t.Fatalf("claimed default ids = %#v, want %#v", got, want)
	}
	requireMirrorQueueState(t, ctx, db, "q_default_expired_processing", "processing", 2, "")
	requireMirrorQueueState(t, ctx, db, "q_default_fresh_processing", "processing", 3, "still leased")
	requireMirrorQueueState(t, ctx, db, "q_other_expired_processing", "processing", 2, "worker interrupted")

	claimedOther, err := repo.ClaimForPersona(ctx, "other", 10)
	if err != nil {
		t.Fatalf("claim other expired queue rows: %v", err)
	}
	if got, want := mirrorQueueIDs(claimedOther), []string{"q_other_expired_processing"}; !equalStrings(got, want) {
		t.Fatalf("claimed other ids = %#v, want %#v", got, want)
	}
	requireMirrorQueueState(t, ctx, db, "q_other_expired_processing", "processing", 3, "")
}

type mirrorQueueSeed struct {
	id            string
	nodeType      string
	nodeID        string
	operation     string
	priority      int
	status        string
	attempts      int
	createdAt     time.Time
	updatedAt     *time.Time
	updatedAtNull bool
	errorMessage  string
}

func ensureMirrorQueuePersona(t *testing.T, ctx context.Context, db *memsqlite.DB) {
	t.Helper()

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}
}

func insertMirrorQueueRow(t *testing.T, ctx context.Context, db *memsqlite.DB, seed mirrorQueueSeed) {
	t.Helper()

	insertMirrorQueueRowForPersona(t, ctx, db, "default", seed)
}

func insertMirrorQueueRowForPersona(t *testing.T, ctx context.Context, db *memsqlite.DB, personaID string, seed mirrorQueueSeed) {
	t.Helper()

	_, err := db.SQLDB().ExecContext(ctx, `
INSERT INTO index_sync_queue (
    id, persona_id, node_type, node_id, operation, priority, status, attempts, created_at, updated_at, error_message
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		seed.id,
		personaID,
		seed.nodeType,
		seed.nodeID,
		seed.operation,
		seed.priority,
		seed.status,
		seed.attempts,
		seed.createdAt.UTC().Format(time.RFC3339Nano),
		mirrorQueueUpdatedAt(seed),
		nullIfEmpty(seed.errorMessage),
	)
	if err != nil {
		t.Fatalf("insert queue row %s: %v", seed.id, err)
	}
}

func requireMirrorQueueStatus(t *testing.T, ctx context.Context, db *memsqlite.DB, id string, want string) {
	t.Helper()

	var status string
	if err := db.SQLDB().QueryRowContext(ctx, `SELECT status FROM index_sync_queue WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatalf("read queue status for %s: %v", id, err)
	}
	if status != want {
		t.Fatalf("queue row %s status = %q, want %q", id, status, want)
	}
}

func requireMirrorQueueState(t *testing.T, ctx context.Context, db *memsqlite.DB, id string, wantStatus string, wantAttempts int, wantErrorMessage string) {
	t.Helper()

	var status, errorMessage string
	var attempts int
	if err := db.SQLDB().QueryRowContext(ctx, `
SELECT status, attempts, COALESCE(error_message, '')
FROM index_sync_queue
WHERE id = ?`, id).Scan(&status, &attempts, &errorMessage); err != nil {
		t.Fatalf("read queue state for %s: %v", id, err)
	}
	if status != wantStatus {
		t.Fatalf("queue row %s status = %q, want %q", id, status, wantStatus)
	}
	if attempts != wantAttempts {
		t.Fatalf("queue row %s attempts = %d, want %d", id, attempts, wantAttempts)
	}
	if errorMessage != wantErrorMessage {
		t.Fatalf("queue row %s error_message = %q, want %q", id, errorMessage, wantErrorMessage)
	}
}

func mirrorQueueIDs(rows []memsqlite.MirrorQueueRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func equalStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func mirrorQueueUpdatedAt(seed mirrorQueueSeed) any {
	if seed.updatedAtNull {
		return nil
	}
	if seed.updatedAt != nil {
		return seed.updatedAt.UTC().Format(time.RFC3339Nano)
	}
	return seed.createdAt.UTC().Format(time.RFC3339Nano)
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
