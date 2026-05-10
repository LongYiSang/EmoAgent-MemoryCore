package memorycore_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	_ "modernc.org/sqlite"
)

func TestServiceConsolidateSupersedesSinglePredicates(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID, shanghaiID, hangzhouID := seedConsolidationSubjectAndPlaces(t, ctx, svc)
	firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我住在上海。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我现在住在杭州。", time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC))

	first, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "lives_in",
			ObjectEntityID:   &shanghaiID,
			ContentSummary:   "用户住在上海。",
			SourceEpisodeIDs: []string{firstEpisode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.8,
		},
	})
	if err != nil {
		t.Fatalf("consolidate first lives_in: %v", err)
	}
	if first.Action != memorycore.ConsolidationActionInsert || first.Fact == nil {
		t.Fatalf("first result = %#v, want inserted fact", first)
	}

	second, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "lives_in",
			ObjectEntityID:   &hangzhouID,
			ContentSummary:   "用户住在杭州。",
			SourceEpisodeIDs: []string{secondEpisode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.9,
		},
	})
	if err != nil {
		t.Fatalf("consolidate second lives_in: %v", err)
	}
	if second.Action != memorycore.ConsolidationActionSupersede || second.Fact == nil {
		t.Fatalf("second result = %#v, want supersede", second)
	}
	if len(second.SupersededFactIDs) != 1 || second.SupersededFactIDs[0] != first.Fact.ID {
		t.Fatalf("superseded facts = %#v, want %s", second.SupersededFactIDs, first.Fact.ID)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireFactValidity(t, db, first.Fact.ID, memorycore.ValidityInvalidated)
	requireLink(t, db, second.Fact.ID, "SUPERSEDES", first.Fact.ID)
	requireLink(t, db, second.Fact.ID, "EVIDENCED_BY", secondEpisode.ID)
	requireLink(t, db, second.Fact.ID, "ABOUT_ENTITY", userID)
	requireLink(t, db, second.Fact.ID, "ABOUT_ENTITY", hangzhouID)
}

func TestServiceConsolidatePrefersNameSupersedesLiteral(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "请叫我 Long。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "以后叫我 Yi。", time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC))
	longName := "Long"
	yiName := "Yi"

	first, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "prefers_name",
			ObjectLiteral:    &longName,
			ContentSummary:   "用户偏好被称呼为 Long。",
			SourceEpisodeIDs: []string{firstEpisode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.8,
		},
	})
	if err != nil {
		t.Fatalf("consolidate first name: %v", err)
	}
	second, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "prefers_name",
			ObjectLiteral:    &yiName,
			ContentSummary:   "用户偏好被称呼为 Yi。",
			SourceEpisodeIDs: []string{secondEpisode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.85,
		},
	})
	if err != nil {
		t.Fatalf("consolidate second name: %v", err)
	}
	if second.Action != memorycore.ConsolidationActionSupersede {
		t.Fatalf("second action = %q, want supersede", second.Action)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireFactValidity(t, db, first.Fact.ID, memorycore.ValidityInvalidated)
}

func TestServiceConsolidateCoexistAndReinforceLikes(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我也喜欢茶。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	thirdEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我还是喜欢咖啡。", time.Date(2026, 5, 10, 11, 0, 0, 0, time.UTC))
	coffee := "咖啡"
	tea := "茶"

	first := consolidateLiteral(t, ctx, svc, userID, "likes", coffee, "用户喜欢咖啡。", firstEpisode.ID)
	second := consolidateLiteral(t, ctx, svc, userID, "likes", tea, "用户喜欢茶。", secondEpisode.ID)
	third := consolidateLiteral(t, ctx, svc, userID, "likes", coffee, "用户喜欢咖啡。", thirdEpisode.ID)

	if first.Action != memorycore.ConsolidationActionInsert {
		t.Fatalf("first action = %q, want insert", first.Action)
	}
	if second.Action != memorycore.ConsolidationActionCoexist {
		t.Fatalf("second action = %q, want coexist", second.Action)
	}
	if third.Action != memorycore.ConsolidationActionReinforce {
		t.Fatalf("third action = %q, want reinforce", third.Action)
	}
	if third.Fact.ID != first.Fact.ID {
		t.Fatalf("reinforced fact id = %q, want %q", third.Fact.ID, first.Fact.ID)
	}
	if third.Fact.ReinforcementCount != 1 {
		t.Fatalf("reinforcement count = %d, want 1", third.Fact.ReinforcementCount)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireFactCount(t, db, "likes", 2)
	requireLink(t, db, first.Fact.ID, "EVIDENCED_BY", thirdEpisode.ID)
}

func TestServiceConsolidateMergeBoundaryReinforcesConservatively(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "不要在晚上十点后提醒我工作。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "晚上十点后不要提醒我工作。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	boundary := "晚上十点后不要提醒我工作"

	first := consolidateLiteral(t, ctx, svc, userID, "has_boundary", boundary, "用户不希望晚上十点后被提醒工作。", firstEpisode.ID)
	second := consolidateLiteral(t, ctx, svc, userID, "has_boundary", boundary, "用户不希望晚上十点后被提醒工作和任务。", secondEpisode.ID)

	if second.Action != memorycore.ConsolidationActionReinforce {
		t.Fatalf("second action = %q, want reinforce", second.Action)
	}
	if second.Fact.ID != first.Fact.ID {
		t.Fatalf("reinforced fact id = %q, want %q", second.Fact.ID, first.Fact.ID)
	}
	if second.Fact.ContentSummary != first.Fact.ContentSummary {
		t.Fatalf("summary changed to %q, want conservative %q", second.Fact.ContentSummary, first.Fact.ContentSummary)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireFactCount(t, db, "has_boundary", 1)
}

func TestServiceConsolidateLLMCheckNeedsReviewWithoutFact(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我现在感觉可以信任 Agent。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	feeling := "信任"
	result, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "feels_about_agent",
			ObjectLiteral:    &feeling,
			ContentSummary:   "用户信任 Agent。",
			SourceEpisodeIDs: []string{episode.ID},
			Confidence:       memorycore.ConfidenceAmbiguous,
			Importance:       0.7,
		},
	})
	if err != nil {
		t.Fatalf("consolidate llm_check: %v", err)
	}
	if result.Action != memorycore.ConsolidationActionNeedsReview {
		t.Fatalf("action = %q, want needs_review", result.Action)
	}
	if result.Status != memorycore.ConsolidationStatusNeedsReview {
		t.Fatalf("status = %q, want needs_review", result.Status)
	}
	if result.Fact != nil {
		t.Fatalf("fact = %#v, want nil", result.Fact)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireFactCount(t, db, "feels_about_agent", 0)
}

func TestServiceConsolidateExpireByTimeUsesCandidateValidFromForDefaultTTL(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我最近忙上线准备。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	busyWith := "上线准备"
	validFrom := time.Date(2026, 5, 9, 8, 0, 0, 0, time.UTC)
	result, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "is_busy_with",
			ObjectLiteral:    &busyWith,
			ContentSummary:   "用户近期忙于上线准备。",
			ValidFrom:        &validFrom,
			SourceEpisodeIDs: []string{episode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.6,
		},
	})
	if err != nil {
		t.Fatalf("consolidate expire_by_time: %v", err)
	}
	if result.Action != memorycore.ConsolidationActionInsert || result.Fact == nil {
		t.Fatalf("result = %#v, want inserted fact", result)
	}
	if result.Fact.ValidTo == nil {
		t.Fatal("valid_to is nil, want default ttl")
	}
	wantValidTo := validFrom.Add(21 * 24 * time.Hour)
	if !result.Fact.ValidTo.Equal(wantValidTo) {
		t.Fatalf("valid_to = %s, want %s", result.Fact.ValidTo.Format(time.RFC3339Nano), wantValidTo.Format(time.RFC3339Nano))
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireFactValidTo(t, db, result.Fact.ID, wantValidTo)
	requireFactLifecycleVisibility(t, db, result.Fact.ID, "active", "visible")
}

func TestServiceConsolidateMergeNonExactNeedsReviewWithoutInsert(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "不要在晚上十点后提醒我工作。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "周末不要讨论工作。", time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC))
	firstBoundary := "晚上十点后不要提醒我工作"
	secondBoundary := "不要在周末讨论工作"

	first := consolidateLiteral(t, ctx, svc, userID, "has_boundary", firstBoundary, "用户不希望晚上十点后被提醒工作。", firstEpisode.ID)
	second, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "has_boundary",
			ObjectLiteral:    &secondBoundary,
			ContentSummary:   "用户不希望周末讨论工作。",
			SourceEpisodeIDs: []string{secondEpisode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.8,
		},
	})
	if err != nil {
		t.Fatalf("consolidate non-exact boundary: %v", err)
	}
	if second.Action != memorycore.ConsolidationActionNeedsReview {
		t.Fatalf("second action = %q, want needs_review", second.Action)
	}
	if second.Status != memorycore.ConsolidationStatusNeedsReview {
		t.Fatalf("second status = %q, want needs_review", second.Status)
	}
	if second.Fact != nil {
		t.Fatalf("second fact = %#v, want nil", second.Fact)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireFactCount(t, db, "has_boundary", 1)
	requireFactReinforcementCount(t, db, first.Fact.ID, 0)
}

func TestServiceConsolidateRejectsUnsafeCandidates(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	visibleEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	hidden := false
	hiddenEpisode, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		SessionID:        sessionID,
		Content:          "隐藏来源。",
		VisibilityStatus: memorycore.VisibilityHidden,
		Searchable:       &hidden,
	})
	if err != nil {
		t.Fatalf("append hidden episode: %v", err)
	}
	coffee := "咖啡"

	cases := []struct {
		name    string
		request memorycore.ConsolidateCandidateRequest
	}{
		{
			name: "hidden source",
			request: memorycore.ConsolidateCandidateRequest{Candidate: memorycore.ManualFactCandidate{
				SubjectEntityID: userID, Predicate: "likes", ObjectLiteral: &coffee, ContentSummary: "用户喜欢咖啡。", SourceEpisodeIDs: []string{hiddenEpisode.ID},
			}},
		},
		{
			name: "no source",
			request: memorycore.ConsolidateCandidateRequest{Candidate: memorycore.ManualFactCandidate{
				SubjectEntityID: userID, Predicate: "likes", ObjectLiteral: &coffee, ContentSummary: "用户喜欢咖啡。",
			}},
		},
		{
			name:    "agent affect",
			request: memorycore.ConsolidateCandidateRequest{Trigger: memorycore.ConsolidationTriggerAgentAffect, Candidate: memorycore.ManualFactCandidate{SubjectEntityID: userID, Predicate: "likes", ObjectLiteral: &coffee, ContentSummary: "Agent 喜欢用户。", SourceEpisodeIDs: []string{visibleEpisode.ID}}},
		},
		{
			name:    "work candidate without approval",
			request: memorycore.ConsolidateCandidateRequest{Trigger: memorycore.ConsolidationTriggerWorkCandidate, Candidate: memorycore.ManualFactCandidate{SubjectEntityID: userID, Predicate: "likes", ObjectLiteral: &coffee, ContentSummary: "用户喜欢咖啡。", SourceEpisodeIDs: []string{visibleEpisode.ID}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := svc.ConsolidateCandidate(ctx, tc.request)
			if err != nil {
				t.Fatalf("consolidate rejected candidate: %v", err)
			}
			if result.Status != memorycore.ConsolidationStatusRejected {
				t.Fatalf("status = %q, want rejected", result.Status)
			}
		})
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireFactCount(t, db, "likes", 0)
}

func openConsolidationService(t *testing.T, ctx context.Context) (memorycore.Service, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		AutoMigrate: true,
		Now: func() time.Time {
			return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	return svc, dbPath
}

func seedConsolidationSubjectAndPlaces(t *testing.T, ctx context.Context, svc memorycore.Service) (string, string, string, string) {
	t.Helper()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	shanghai, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{CanonicalName: "上海", EntityType: memorycore.EntityTypePlace})
	if err != nil {
		t.Fatalf("ensure shanghai: %v", err)
	}
	hangzhou, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{CanonicalName: "杭州", EntityType: memorycore.EntityTypePlace})
	if err != nil {
		t.Fatalf("ensure hangzhou: %v", err)
	}
	return sessionID, userID, shanghai.ID, hangzhou.ID
}

func seedConsolidationSubject(t *testing.T, ctx context.Context, svc memorycore.Service) (string, string) {
	t.Helper()

	session, err := svc.StartSession(ctx, memorycore.StartSessionRequest{})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	user, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{CanonicalName: "Long", EntityType: memorycore.EntityTypeUser})
	if err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	return session.ID, user.ID
}

func appendConsolidationEpisode(t *testing.T, ctx context.Context, svc memorycore.Service, sessionID string, content string, occurredAt time.Time) *memorycore.Episode {
	t.Helper()

	episode, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		SessionID:  sessionID,
		Content:    content,
		OccurredAt: occurredAt,
	})
	if err != nil {
		t.Fatalf("append episode: %v", err)
	}
	return episode
}

func consolidateLiteral(t *testing.T, ctx context.Context, svc memorycore.Service, subjectID string, predicate string, object string, summary string, sourceEpisodeID string) *memorycore.ConsolidationResult {
	t.Helper()

	result, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  subjectID,
			Predicate:        predicate,
			ObjectLiteral:    &object,
			ContentSummary:   summary,
			SourceEpisodeIDs: []string{sourceEpisodeID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.7,
		},
	})
	if err != nil {
		t.Fatalf("consolidate literal: %v", err)
	}
	if result.Fact == nil {
		t.Fatalf("result fact is nil: %#v", result)
	}
	return result
}

func openSQLDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func requireFactValidity(t *testing.T, db *sql.DB, factID string, want string) {
	t.Helper()

	var got string
	var validTo sql.NullString
	if err := db.QueryRow(`SELECT validity_status, valid_to FROM facts WHERE id = ?`, factID).Scan(&got, &validTo); err != nil {
		t.Fatalf("query fact %s: %v", factID, err)
	}
	if got != want {
		t.Fatalf("fact %s validity = %q, want %q", factID, got, want)
	}
	if want == memorycore.ValidityInvalidated && !validTo.Valid {
		t.Fatalf("fact %s valid_to is null, want invalidation time", factID)
	}
}

func requireFactValidTo(t *testing.T, db *sql.DB, factID string, want time.Time) {
	t.Helper()

	var got string
	if err := db.QueryRow(`SELECT valid_to FROM facts WHERE id = ?`, factID).Scan(&got); err != nil {
		t.Fatalf("query fact valid_to: %v", err)
	}
	if got != want.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("fact %s valid_to = %q, want %q", factID, got, want.UTC().Format(time.RFC3339Nano))
	}
}

func requireFactLifecycleVisibility(t *testing.T, db *sql.DB, factID string, wantLifecycle string, wantVisibility string) {
	t.Helper()

	var lifecycle, visibility string
	if err := db.QueryRow(`SELECT lifecycle_status, visibility_status FROM facts WHERE id = ?`, factID).Scan(&lifecycle, &visibility); err != nil {
		t.Fatalf("query fact lifecycle visibility: %v", err)
	}
	if lifecycle != wantLifecycle {
		t.Fatalf("fact %s lifecycle = %q, want %q", factID, lifecycle, wantLifecycle)
	}
	if visibility != wantVisibility {
		t.Fatalf("fact %s visibility = %q, want %q", factID, visibility, wantVisibility)
	}
}

func requireFactCount(t *testing.T, db *sql.DB, predicate string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM facts WHERE predicate = ?`, predicate).Scan(&got); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if got != want {
		t.Fatalf("fact count for %s = %d, want %d", predicate, got, want)
	}
}

func requireFactReinforcementCount(t *testing.T, db *sql.DB, factID string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow(`SELECT reinforcement_count FROM facts WHERE id = ?`, factID).Scan(&got); err != nil {
		t.Fatalf("query fact reinforcement count: %v", err)
	}
	if got != want {
		t.Fatalf("fact %s reinforcement_count = %d, want %d", factID, got, want)
	}
}

func requireLink(t *testing.T, db *sql.DB, fromID string, linkType string, toID string) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_links
WHERE from_node_id = ? AND link_type = ? AND to_node_id = ?`, fromID, linkType, toID).Scan(&count); err != nil {
		t.Fatalf("count link: %v", err)
	}
	if count != 1 {
		t.Fatalf("link %s -%s-> %s count = %d, want 1", fromID, linkType, toID, count)
	}
}
