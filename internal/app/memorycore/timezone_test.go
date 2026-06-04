package memorycore

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServiceUsesConfiguredTimezoneForStoreWrites(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := Open(ctx, Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		Timezone:    "Asia/Shanghai",
		Now: func() time.Time {
			return time.Date(2026, 6, 4, 21, 30, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer svc.Close()

	if _, err := svc.StartSession(ctx, StartSessionRequest{ID: "session_tz"}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := svc.AppendEpisode(ctx, AppendEpisodeRequest{ID: "ep_tz", SessionID: "session_tz", Content: "我喜欢咖啡。"}); err != nil {
		t.Fatalf("AppendEpisode: %v", err)
	}
	if _, err := svc.EnsureEntity(ctx, EnsureEntityRequest{ID: "ent_user_tz", CanonicalName: "Long", EntityType: EntityTypeUser}); err != nil {
		t.Fatalf("EnsureEntity: %v", err)
	}
	fact, err := svc.ConsolidateCandidate(ctx, ConsolidateCandidateRequest{
		Candidate: ManualFactCandidate{
			SubjectEntityID:  "ent_user_tz",
			Predicate:        "likes",
			ObjectLiteral:    stringPtrValue("咖啡"),
			ContentSummary:   "用户喜欢咖啡。",
			SourceEpisodeIDs: []string{"ep_tz"},
			Confidence:       ConfidenceExplicit,
			Importance:       0.7,
		},
		Policy: ConsolidationPolicy{Approved: true},
	})
	if err != nil {
		t.Fatalf("ConsolidateCandidate: %v", err)
	}
	if fact.Fact == nil {
		t.Fatalf("ConsolidateCandidate fact = nil")
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	assertDBTextHasOffset(t, db, "SELECT created_at FROM personas WHERE id = 'default'", "+08:00")
	assertDBTextHasOffset(t, db, "SELECT started_at FROM sessions WHERE id = 'session_tz'", "+08:00")
	assertDBTextHasOffset(t, db, "SELECT created_at FROM facts WHERE id = '"+fact.Fact.ID+"'", "+08:00")
}

func TestExtractionRawLogUsesTraceTimezone(t *testing.T) {
	start := time.Date(2026, 6, 4, 21, 30, 0, 123, time.FixedZone("CST", 8*60*60))
	trace := newRawLogTrace(start, ExtractionRunRequest{
		Request: ExtractionRequest{RequestID: "req_tz"},
		RawLog:  ExtractionRawLogOptions{Enabled: true},
	})
	if trace == nil {
		t.Fatal("trace is nil")
	}
	if !strings.Contains(trace.StartedAt.Format(time.RFC3339Nano), "+08:00") {
		t.Fatalf("trace started_at = %s, want +08:00", trace.StartedAt.Format(time.RFC3339Nano))
	}
	name := rawLogFilename(trace.StartedAt, ExtractionRunResult{
		RequestID:   "req_tz",
		Status:      ExtractionRunStatusDryRun,
		Fingerprint: "abcdef123456",
	})
	if !strings.HasPrefix(name, "20260604T213000.000000123+0800_") {
		t.Fatalf("raw log filename = %q, want local +0800 prefix", name)
	}
}

func TestCurationRawLogUsesTraceTimezone(t *testing.T) {
	start := time.Date(2026, 6, 4, 21, 30, 0, 0, time.FixedZone("CST", 8*60*60))
	trace := newCurationRawLogTrace(start, RunCurationRequest{Trigger: "test"}, CurationRawLogOptions{Enabled: true})
	if trace == nil {
		t.Fatal("trace is nil")
	}
	if !strings.Contains(trace.StartedAt.Format(time.RFC3339Nano), "+08:00") {
		t.Fatalf("trace started_at = %s, want +08:00", trace.StartedAt.Format(time.RFC3339Nano))
	}
	name := curationRawLogFilename(trace.StartedAt, &RunCurationResult{RunID: "cur_tz", Status: "completed"})
	if !strings.HasPrefix(name, "20260604T213000.000000000+0800_") {
		t.Fatalf("curation raw log filename = %q, want local +0800 prefix", name)
	}
}

func assertDBTextHasOffset(t *testing.T, db *sql.DB, query string, offset string) {
	t.Helper()
	var value string
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if !strings.Contains(value, offset) {
		t.Fatalf("%q = %q, want offset %s", query, value, offset)
	}
}
