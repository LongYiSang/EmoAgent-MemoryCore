package sqlite_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestEpisodeRepositoryMaintainsThreeEpisodeChainAndStableHash(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}
	if err := store.EnsureSession(ctx, core.Session{ID: "s1", PersonaID: "default", Channel: core.ChannelAPI}); err != nil {
		t.Fatalf("ensure session: %v", err)
	}

	episodes := memsqlite.NewEpisodeRepository(db.SQLDB())
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	for _, episode := range []core.Episode{
		{ID: "ep_01", PersonaID: "default", SessionID: "s1", Content: "same content", OccurredAt: now},
		{ID: "ep_02", PersonaID: "default", SessionID: "s1", Content: "same content", OccurredAt: now.Add(time.Minute)},
		{ID: "ep_03", PersonaID: "default", SessionID: "s1", Content: "different content", OccurredAt: now.Add(2 * time.Minute)},
	} {
		if err := episodes.Append(ctx, episode); err != nil {
			t.Fatalf("append %s: %v", episode.ID, err)
		}
	}

	first, err := episodes.Get(ctx, "default", "ep_01")
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	second, err := episodes.Get(ctx, "default", "ep_02")
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	third, err := episodes.Get(ctx, "default", "ep_03")
	if err != nil {
		t.Fatalf("get third: %v", err)
	}

	if first.NextEpisodeID == nil || *first.NextEpisodeID != "ep_02" {
		t.Fatalf("first next = %v, want ep_02", first.NextEpisodeID)
	}
	if second.PrevEpisodeID == nil || *second.PrevEpisodeID != "ep_01" {
		t.Fatalf("second prev = %v, want ep_01", second.PrevEpisodeID)
	}
	if second.NextEpisodeID == nil || *second.NextEpisodeID != "ep_03" {
		t.Fatalf("second next = %v, want ep_03", second.NextEpisodeID)
	}
	if third.PrevEpisodeID == nil || *third.PrevEpisodeID != "ep_02" {
		t.Fatalf("third prev = %v, want ep_02", third.PrevEpisodeID)
	}
	if first.ContentHash != second.ContentHash || first.ContentHash != sha256Hex("same content") {
		t.Fatalf("stable hash mismatch: first=%q second=%q", first.ContentHash, second.ContentHash)
	}
}

func TestEpisodeRepositoryExtractionCandidatesOnlyVisibleSearchable(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()

	store := memsqlite.NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}
	if err := store.EnsureSession(ctx, core.Session{ID: "s1", PersonaID: "default", Channel: core.ChannelAPI}); err != nil {
		t.Fatalf("ensure session: %v", err)
	}

	episodes := memsqlite.NewEpisodeRepository(db.SQLDB())
	now := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	for _, episode := range []core.Episode{
		{ID: "visible", PersonaID: "default", SessionID: "s1", Content: "visible", OccurredAt: now},
		{ID: "hidden", PersonaID: "default", SessionID: "s1", Content: "hidden", OccurredAt: now.Add(time.Minute), VisibilityStatus: core.VisibilityHidden},
		{ID: "redacted", PersonaID: "default", SessionID: "s1", Content: "redacted", OccurredAt: now.Add(2 * time.Minute), VisibilityStatus: core.VisibilityRedacted},
	} {
		if err := episodes.Append(ctx, episode); err != nil {
			t.Fatalf("append %s: %v", episode.ID, err)
		}
	}

	candidates, err := episodes.ListExtractionCandidates(ctx, "default", "s1")
	if err != nil {
		t.Fatalf("list extraction candidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].ID != "visible" {
		t.Fatalf("candidates = %#v, want only visible", candidates)
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
