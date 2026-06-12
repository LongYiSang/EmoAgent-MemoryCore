package memorycore_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestObservabilitySnapshotEmptyMigratedDB(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		AutoMigrate: true,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()

	snapshot, err := svc.Ops().GetObservabilitySnapshot(ctx, memorycore.ObservabilitySnapshotRequest{})
	if err != nil {
		t.Fatalf("observability snapshot: %v", err)
	}
	if snapshot.PersonaID != "default" {
		t.Fatalf("persona id = %q, want default", snapshot.PersonaID)
	}
	if snapshot.Status != "ok" {
		t.Fatalf("status = %q warnings=%v", snapshot.Status, snapshot.Warnings)
	}
	if !snapshot.GeneratedAt.Equal(now) {
		t.Fatalf("generated_at = %s, want %s", snapshot.GeneratedAt, now)
	}
	if snapshot.Store.PersonaCount != 0 || snapshot.Store.SessionCount != 0 {
		t.Fatalf("empty store counts = %#v", snapshot.Store)
	}
	if snapshot.Store.FTSAvailable {
		t.Fatalf("fts_available = true, want false unless EnableFTS is explicit")
	}
}

func TestObservabilitySnapshotDoesNotReturnRawContent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      filepath.Join(t.TempDir(), "memory.db"),
		PersonaID:   "default",
		AutoMigrate: true,
		Now:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "episode private raw text", now)
	consolidateLiteral(t, ctx, svc, userID, "likes", "fact private literal", "fact private summary", episode.ID)

	snapshot, err := svc.Ops().GetObservabilitySnapshot(ctx, memorycore.ObservabilitySnapshotRequest{
		PersonaID: "default",
		Since:     now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("observability snapshot: %v", err)
	}
	if snapshot.Store.SessionCount != 1 || snapshot.Store.FactByVisibility["visible"] != 1 {
		t.Fatalf("snapshot counts = %#v", snapshot.Store)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	text := string(data)
	for _, secret := range []string{"episode private raw text", "fact private literal", "fact private summary"} {
		if strings.Contains(text, secret) {
			t.Fatalf("snapshot leaked %q: %s", secret, text)
		}
	}
}
