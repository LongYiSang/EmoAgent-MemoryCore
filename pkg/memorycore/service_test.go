package memorycore_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	_ "modernc.org/sqlite"
)

func TestServiceStartSessionAndAppendEpisode(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		PersonaID:   "default",
		AutoMigrate: true,
		Now: func() time.Time {
			return now
		},
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()

	title := "API session"
	session, err := svc.StartSession(ctx, memorycore.StartSessionRequest{
		Title: &title,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if session.ID == "" {
		t.Fatal("session id is empty")
	}
	if session.PersonaID != "default" {
		t.Fatalf("session persona = %q, want default", session.PersonaID)
	}
	if session.Channel != memorycore.ChannelAPI {
		t.Fatalf("session channel = %q, want %q", session.Channel, memorycore.ChannelAPI)
	}
	if session.StartedAt != now {
		t.Fatalf("session started_at = %s, want %s", session.StartedAt, now)
	}

	first, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		SessionID: session.ID,
		Content:   "请记住，我不喜欢早上 8 点的会议。",
	})
	if err != nil {
		t.Fatalf("append first episode: %v", err)
	}
	if first.ID == "" {
		t.Fatal("first episode id is empty")
	}
	if first.PersonaID != "default" {
		t.Fatalf("first persona = %q, want default", first.PersonaID)
	}
	if first.SessionID != session.ID {
		t.Fatalf("first session = %q, want %q", first.SessionID, session.ID)
	}
	if first.Role != memorycore.RoleUser {
		t.Fatalf("first role = %q, want %q", first.Role, memorycore.RoleUser)
	}
	if first.SourceType != memorycore.SourceTypeChat {
		t.Fatalf("first source type = %q, want %q", first.SourceType, memorycore.SourceTypeChat)
	}
	if first.ContentHash != sha256Hex(first.Content) {
		t.Fatalf("first content hash = %q", first.ContentHash)
	}
	if first.PrevEpisodeID != nil || first.NextEpisodeID != nil {
		t.Fatalf("first prev/next = %v/%v, want nil/nil", first.PrevEpisodeID, first.NextEpisodeID)
	}

	second, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		SessionID:  session.ID,
		Role:       memorycore.RoleAssistant,
		Content:    "我记住了。",
		OccurredAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("append second episode: %v", err)
	}
	if second.PrevEpisodeID == nil || *second.PrevEpisodeID != first.ID {
		t.Fatalf("second prev = %v, want %q", second.PrevEpisodeID, first.ID)
	}
	requireEpisodeNextID(t, dbPath, first.ID, second.ID)

	third, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		SessionID: session.ID,
		Content:   "第三条消息。",
	})
	if err != nil {
		t.Fatalf("append third episode: %v", err)
	}
	if third.PrevEpisodeID == nil || *third.PrevEpisodeID != second.ID {
		t.Fatalf("third prev = %v, want %q", third.PrevEpisodeID, second.ID)
	}
	requireEpisodeNextID(t, dbPath, second.ID, third.ID)

	endedAt := now.Add(time.Hour)
	summary := "用户表达了会议偏好。"
	ended, err := svc.EndSession(ctx, memorycore.EndSessionRequest{
		SessionID: session.ID,
		EndedAt:   endedAt,
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

func TestServiceValidation(t *testing.T) {
	ctx := context.Background()
	if _, err := memorycore.Open(ctx, memorycore.Options{}); !errors.Is(err, memorycore.ErrInvalidOptions) {
		t.Fatalf("open without db path err = %v, want ErrInvalidOptions", err)
	}

	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		AutoMigrate: true,
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()

	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{Content: "missing session"}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("append without session err = %v, want ErrInvalidRequest", err)
	}
	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{SessionID: "s1"}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("append without content err = %v, want ErrInvalidRequest", err)
	}
	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{SessionID: "missing", Content: "hello"}); !errors.Is(err, memorycore.ErrNotFound) {
		t.Fatalf("append missing session err = %v, want ErrNotFound", err)
	}
	if _, err := svc.EndSession(ctx, memorycore.EndSessionRequest{}); !errors.Is(err, memorycore.ErrInvalidRequest) {
		t.Fatalf("end without session err = %v, want ErrInvalidRequest", err)
	}
	if _, err := svc.EndSession(ctx, memorycore.EndSessionRequest{SessionID: "missing"}); !errors.Is(err, memorycore.ErrNotFound) {
		t.Fatalf("end missing session err = %v, want ErrNotFound", err)
	}
}

func TestServiceOpenCanDisableFTS(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		EnableFTS:   false,
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close service: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	requireDBObject(t, db, "memory_search_documents")
	requireNoDBObject(t, db, "memory_search_fts")
}

func requireDBObject(t *testing.T, db *sql.DB, name string) {
	t.Helper()

	var got string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE name = ?`, name).Scan(&got)
	if err != nil {
		t.Fatalf("db object %s does not exist: %v", name, err)
	}
}

func requireNoDBObject(t *testing.T, db *sql.DB, name string) {
	t.Helper()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, name).Scan(&count); err != nil {
		t.Fatalf("count db object %s: %v", name, err)
	}
	if count != 0 {
		t.Fatalf("db object %s exists, want absent", name)
	}
}

func requireEpisodeNextID(t *testing.T, dbPath string, episodeID string, want string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var got string
	if err := db.QueryRow(`SELECT next_episode_id FROM episodes WHERE id = ?`, episodeID).Scan(&got); err != nil {
		t.Fatalf("query next episode id: %v", err)
	}
	if got != want {
		t.Fatalf("episode %s next = %q, want %q", episodeID, got, want)
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
