package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestStoreEndsSessionAndReturnsPersistedFields(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}

	startedAt := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	title := "morning chat"
	if err := store.EnsureSession(ctx, core.Session{
		ID:        "session_01",
		PersonaID: "default",
		Channel:   core.ChannelAPI,
		Title:     &title,
		StartedAt: startedAt,
	}); err != nil {
		t.Fatalf("ensure session: %v", err)
	}

	got, err := store.GetSession(ctx, "default", "session_01")
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.Title == nil || *got.Title != title {
		t.Fatalf("session title = %v, want %q", got.Title, title)
	}
	if !got.StartedAt.Equal(startedAt) {
		t.Fatalf("session started_at = %s, want %s", got.StartedAt, startedAt)
	}

	endedAt := startedAt.Add(30 * time.Minute)
	summary := "用户补充了会议偏好。"
	ended, err := store.EndSession(ctx, core.Session{
		ID:        "session_01",
		PersonaID: "default",
		EndedAt:   &endedAt,
		Summary:   &summary,
	})
	if err != nil {
		t.Fatalf("end session: %v", err)
	}
	if ended.EndedAt == nil || !ended.EndedAt.Equal(endedAt) {
		t.Fatalf("ended_at = %v, want %s", ended.EndedAt, endedAt)
	}
	if ended.Summary == nil || *ended.Summary != summary {
		t.Fatalf("summary = %v, want %q", ended.Summary, summary)
	}
}
