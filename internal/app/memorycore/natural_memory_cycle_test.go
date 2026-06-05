package memorycore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func TestNaturalDryRunNoWrites(t *testing.T) {
	ctx := context.Background()
	svc, db := openNaturalTestService(t, ctx, fixedNaturalNow())
	defer svc.Close()
	seedNaturalFact(t, db, "fact_dry_run", core.FactTypeTransientContext, "用户临时关注发布清单。", naturalFactOptions{})

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		DryRun:    true,
		Now:       fixedNaturalNow(),
	})
	if err != nil {
		t.Fatalf("natural dry run: %v", err)
	}
	if result.EvaluatedNodes != 1 || result.SearchTierUpdates == 0 {
		t.Fatalf("dry-run result = %#v, want evaluated node and predicted tier update", result)
	}
	requireTableCount(t, db, "memory_natural_runs", 0)
	requireTableCount(t, db, "memory_natural_states", 0)
	requireTableCount(t, db, "memory_natural_events", 0)
	requireSearchTier(t, db, "fact_dry_run", string(core.SearchTierHot))
}

func TestNaturalSleepCycleOncePerLocalDay(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 5, 3, 31, 0, 0, time.FixedZone("CST", 8*60*60))
	svc, db := openNaturalTestService(t, ctx, now)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_sleep", core.FactTypeTransientContext, "用户这周关注上线事项。", naturalFactOptions{})

	first, err := svc.RunNaturalMemoryTick(ctx, RunNaturalMemoryTickRequest{
		PersonaID: "default",
		Now:       now,
	})
	if err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if first.Status != NaturalMemoryRunStatusCompleted {
		t.Fatalf("first tick status = %s, want completed", first.Status)
	}
	second, err := svc.RunNaturalMemoryTick(ctx, RunNaturalMemoryTickRequest{
		PersonaID: "default",
		Now:       now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if second.Status != NaturalMemoryRunStatusSkipped {
		t.Fatalf("second tick status = %s, want skipped", second.Status)
	}
	requireTableCount(t, db, "memory_natural_runs", 1)
}

func TestNaturalSleepCycleForceBypassesMinInterval(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 5, 3, 31, 0, 0, time.FixedZone("CST", 8*60*60))
	svc, db := openNaturalTestService(t, ctx, now)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_force_sleep", core.FactTypeTransientContext, "用户这周关注强制睡眠周期。", naturalFactOptions{})

	first, err := svc.RunNaturalMemoryTick(ctx, RunNaturalMemoryTickRequest{
		PersonaID: "default",
		Now:       now,
	})
	if err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if first.Status != NaturalMemoryRunStatusCompleted {
		t.Fatalf("first tick status = %s, want completed", first.Status)
	}
	forced, err := svc.RunNaturalMemoryTick(ctx, RunNaturalMemoryTickRequest{
		PersonaID: "default",
		Now:       now.Add(time.Hour),
		Force:     true,
	})
	if err != nil {
		t.Fatalf("forced tick: %v", err)
	}
	if forced.Status != NaturalMemoryRunStatusCompleted {
		t.Fatalf("forced tick status = %s, want completed", forced.Status)
	}
	requireTableCount(t, db, "memory_natural_runs", 2)
}

func TestNaturalSleepCycleUsesOpenOptionsTimezoneWhenNaturalTimezoneEmpty(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 5, 23, 31, 0, 0, time.UTC)
	opts := defaultNaturalMemoryOptions()
	opts.SleepCycle.LocalTime = "23:30"
	opts.SleepCycle.Timezone = ""
	svc, db := openNaturalTestServiceWithOptions(t, ctx, now, "UTC", opts)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_utc_sleep", core.FactTypeTransientContext, "用户临时关注 UTC 调度。", naturalFactOptions{})

	result, err := svc.RunNaturalMemoryTick(ctx, RunNaturalMemoryTickRequest{
		PersonaID: "default",
		Now:       now,
	})
	if err != nil {
		t.Fatalf("natural tick: %v", err)
	}
	if result.Status != NaturalMemoryRunStatusCompleted {
		t.Fatalf("tick status = %s, want completed with Options timezone", result.Status)
	}
	requireNaturalRunTimezone(t, db, "UTC")
}

func TestNaturalManualDoesNotConsumeSleepCycleQuotaUnlessMarked(t *testing.T) {
	ctx := context.Background()
	now := fixedNaturalNow()
	svc, db := openNaturalTestService(t, ctx, now)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_manual", core.FactTypeTransientContext, "用户临时关注演示。", naturalFactOptions{})

	manual, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("manual run: %v", err)
	}
	if manual.Status != NaturalMemoryRunStatusCompleted {
		t.Fatalf("manual status = %s, want completed", manual.Status)
	}
	sleep, err := svc.RunNaturalMemoryTick(ctx, RunNaturalMemoryTickRequest{
		PersonaID: "default",
		Now:       now,
	})
	if err != nil {
		t.Fatalf("sleep after manual: %v", err)
	}
	if sleep.Status != NaturalMemoryRunStatusCompleted {
		t.Fatalf("sleep status after unmarked manual = %s, want completed", sleep.Status)
	}

	marked, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID:      "default",
		RunKind:        NaturalMemoryRunManual,
		Now:            now.AddDate(0, 0, 1),
		MarkSleepCycle: true,
	})
	if err != nil {
		t.Fatalf("marked manual: %v", err)
	}
	if marked.Status != NaturalMemoryRunStatusCompleted {
		t.Fatalf("marked manual status = %s, want completed", marked.Status)
	}
	skipped, err := svc.RunNaturalMemoryTick(ctx, RunNaturalMemoryTickRequest{
		PersonaID: "default",
		Now:       now.AddDate(0, 0, 1).Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("sleep after marked manual: %v", err)
	}
	if skipped.Status != NaturalMemoryRunStatusSkipped {
		t.Fatalf("sleep after marked manual status = %s, want skipped", skipped.Status)
	}
}

func TestNaturalSkipsHiddenForgottenPurgedNodes(t *testing.T) {
	ctx := context.Background()
	svc, db := openNaturalTestService(t, ctx, fixedNaturalNow())
	defer svc.Close()
	seedNaturalFact(t, db, "fact_visible", core.FactTypeTransientContext, "用户临时关注可见事项。", naturalFactOptions{})
	seedNaturalFact(t, db, "fact_hidden", core.FactTypeTransientContext, "隐藏。", naturalFactOptions{Visibility: core.VisibilityHidden})
	seedNaturalFact(t, db, "fact_forgotten", core.FactTypeTransientContext, "遗忘。", naturalFactOptions{Visibility: core.VisibilityForgotten})
	seedNaturalFact(t, db, "fact_purged", core.FactTypeTransientContext, "清除。", naturalFactOptions{Visibility: core.VisibilityPurged})

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       fixedNaturalNow(),
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.EvaluatedNodes != 1 {
		t.Fatalf("result = %#v, want one evaluated node", result)
	}
	requireNaturalStateCount(t, db, "fact_visible", 1)
	requireNaturalStateCount(t, db, "fact_hidden", 0)
	requireNaturalStateCount(t, db, "fact_forgotten", 0)
	requireNaturalStateCount(t, db, "fact_purged", 0)
}

func TestNaturalCandidateLimitAppliesAfterEligibilityFilter(t *testing.T) {
	ctx := context.Background()
	now := fixedNaturalNow()
	opts := defaultNaturalMemoryOptions()
	opts.Limits.MaxCandidatesPerRun = 1
	svc, db := openNaturalTestServiceWithOptions(t, ctx, now, "Asia/Shanghai", opts)
	defer svc.Close()
	seedNaturalFact(t, db, "a_hidden", core.FactTypeTransientContext, "隐藏。", naturalFactOptions{Visibility: core.VisibilityHidden})
	seedNaturalFact(t, db, "z_visible", core.FactTypeTransientContext, "可见。", naturalFactOptions{})

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.EvaluatedNodes != 1 {
		t.Fatalf("evaluated nodes = %d, want one eligible node after SQL filtering", result.EvaluatedNodes)
	}
	requireNaturalStateCount(t, db, "z_visible", 1)
}

func TestNaturalSkipsDeepArchivedByDefault(t *testing.T) {
	ctx := context.Background()
	now := fixedNaturalNow()
	svc, db := openNaturalTestService(t, ctx, now)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_deep_archived_skip", core.FactTypeSignificantEvent, "深归档默认不参与 Natural。", naturalFactOptions{
		Lifecycle: core.LifecycleDeepArchived,
	})

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.EvaluatedNodes != 0 {
		t.Fatalf("evaluated nodes = %d, want deep_archived skipped by default", result.EvaluatedNodes)
	}
	requireNaturalStateCount(t, db, "fact_deep_archived_skip", 0)
	requireNaturalEventCount(t, db, "fact_deep_archived_skip", "scored", 0)
}

func TestNaturalWritesOnlySearchTierAndUpdatedAt(t *testing.T) {
	ctx := context.Background()
	now := fixedNaturalNow()
	svc, db := openNaturalTestService(t, ctx, now)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_tier_only", core.FactTypeTransientContext, "用户临时关注旧事项。", naturalFactOptions{})
	before := readSearchDocumentSnapshot(t, db, "fact_tier_only")

	_, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	after := readSearchDocumentSnapshot(t, db, "fact_tier_only")
	if after.Tier == before.Tier || after.UpdatedAt == before.UpdatedAt {
		t.Fatalf("search tier/updated_at did not change: before=%#v after=%#v", before, after)
	}
	before.Tier = after.Tier
	before.UpdatedAt = after.UpdatedAt
	if before != after {
		t.Fatalf("natural changed non-tier search document fields: before=%#v after=%#v", before, after)
	}
	requireFactGovernance(t, db, "fact_tier_only", string(core.VisibilityVisible), string(core.ValidityValid), string(core.LifecycleActive))
}

func TestNaturalEnqueuesMirrorUpsertOnTierChange(t *testing.T) {
	ctx := context.Background()
	svc, db := openNaturalTestService(t, ctx, fixedNaturalNow())
	defer svc.Close()
	seedNaturalFact(t, db, "fact_mirror", core.FactTypeTransientContext, "用户临时关注镜像事项。", naturalFactOptions{})
	insertNaturalIndexMap(t, db, core.NodeTypeFact, "fact_mirror")

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       fixedNaturalNow(),
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.MirrorUpdatesEnqueued != 1 {
		t.Fatalf("mirror updates = %d, want 1", result.MirrorUpdatesEnqueued)
	}
	requireQueueCount(t, db, "fact", "fact_mirror", "upsert_node", 1)
}

func TestNaturalDoesNotEnqueueMirrorUpsertWhenDisabled(t *testing.T) {
	ctx := context.Background()
	now := fixedNaturalNow()
	opts := defaultNaturalMemoryOptions()
	opts.SearchTier.EnqueueMirrorUpsertOnTierChange = false
	svc, db := openNaturalTestServiceWithOptions(t, ctx, now, "Asia/Shanghai", opts)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_mirror_disabled", core.FactTypeTransientContext, "用户临时关注镜像关闭事项。", naturalFactOptions{})
	insertNaturalIndexMap(t, db, core.NodeTypeFact, "fact_mirror_disabled")

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("mirror updates = %d, want 0 when enqueue disabled", result.MirrorUpdatesEnqueued)
	}
	requireQueueCount(t, db, "fact", "fact_mirror_disabled", "upsert_node", 0)
}

func TestNaturalMaxWritesPerRunCapsNodeWrites(t *testing.T) {
	ctx := context.Background()
	now := fixedNaturalNow()
	opts := defaultNaturalMemoryOptions()
	opts.Limits.MaxWritesPerRun = 1
	svc, db := openNaturalTestServiceWithOptions(t, ctx, now, "Asia/Shanghai", opts)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_write_1", core.FactTypeTransientContext, "用户临时关注写入一。", naturalFactOptions{})
	seedNaturalFact(t, db, "fact_write_2", core.FactTypeTransientContext, "用户临时关注写入二。", naturalFactOptions{})

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.EvaluatedNodes != 2 || result.SearchTierUpdates != 1 {
		t.Fatalf("result = %#v, want two scored nodes and one written tier update", result)
	}
	requireTableCount(t, db, "memory_natural_states", 1)
	requireSearchTier(t, db, "fact_write_1", string(core.SearchTierDeepCold))
	requireSearchTier(t, db, "fact_write_2", string(core.SearchTierHot))
}

func TestNaturalCreatesMissingSearchDocumentWhenEligible(t *testing.T) {
	ctx := context.Background()
	svc, db := openNaturalTestService(t, ctx, fixedNaturalNow())
	defer svc.Close()
	seedNaturalFact(t, db, "fact_missing_doc", core.FactTypeTransientContext, "用户临时关注缺失索引。", naturalFactOptions{})
	if _, err := db.Exec(`DELETE FROM memory_search_documents WHERE node_id = 'fact_missing_doc'`); err != nil {
		t.Fatalf("delete search doc: %v", err)
	}

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       fixedNaturalNow(),
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.SearchDocumentsCreated != 1 {
		t.Fatalf("search documents created = %d, want 1", result.SearchDocumentsCreated)
	}
	requireSearchTier(t, db, "fact_missing_doc", string(core.SearchTierDeepCold))
}

func TestNaturalProtectedPinnedAndCoreIdentityMinTier(t *testing.T) {
	ctx := context.Background()
	svc, db := openNaturalTestService(t, ctx, fixedNaturalNow())
	defer svc.Close()
	seedNaturalFact(t, db, "fact_core", core.FactTypeCoreIdentity, "用户偏好被称呼为 Long。", naturalFactOptions{Pinned: true})
	if _, err := db.Exec(`UPDATE memory_search_documents SET search_tier = 'deep_cold' WHERE node_id = 'fact_core'`); err != nil {
		t.Fatalf("set search tier: %v", err)
	}

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       fixedNaturalNow(),
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.ProtectedNodes != 1 {
		t.Fatalf("protected nodes = %d, want 1", result.ProtectedNodes)
	}
	requireSearchTier(t, db, "fact_core", string(core.SearchTierHot))
	requireFactGovernance(t, db, "fact_core", string(core.VisibilityVisible), string(core.ValidityValid), string(core.LifecycleActive))
}

func TestNaturalCompressionCandidateDoesNotModifySourceLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, db := openNaturalTestService(t, ctx, fixedNaturalNow())
	defer svc.Close()
	seedNaturalFact(t, db, "fact_c1", core.FactTypeTransientContext, "用户临时关注 Alpha。", naturalFactOptions{Predicate: "topic_alpha"})
	seedNaturalFact(t, db, "fact_c2", core.FactTypeTransientContext, "用户临时关注 Beta。", naturalFactOptions{Predicate: "topic_alpha"})
	seedNaturalFact(t, db, "fact_c3", core.FactTypeTransientContext, "用户临时关注 Gamma。", naturalFactOptions{Predicate: "topic_alpha"})

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       fixedNaturalNow(),
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.CompressionCandidates != 1 {
		t.Fatalf("compression candidates = %d, want 1", result.CompressionCandidates)
	}
	requireTableCount(t, db, "memory_natural_compression_candidates", 1)
	requireNaturalEventTypeCount(t, db, "compression_candidate_emitted", 1)
	for _, id := range []string{"fact_c1", "fact_c2", "fact_c3"} {
		requireFactGovernance(t, db, id, string(core.VisibilityVisible), string(core.ValidityValid), string(core.LifecycleActive))
	}
}

func TestNaturalLifecycleCapEmitsStorageRewarmCandidate(t *testing.T) {
	ctx := context.Background()
	now := fixedNaturalNow()
	svc, db := openNaturalTestService(t, ctx, now)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_archived", core.FactTypeSignificantEvent, "用户重新强烈提到已归档事项。", naturalFactOptions{
		Predicate:  "topic_rewarm",
		Lifecycle:  core.LifecycleArchived,
		SearchTier: core.SearchTierHot,
	})
	seedNaturalFact(t, db, "fact_deep_archived", core.FactTypeSignificantEvent, "用户重新强烈提到深归档事项。", naturalFactOptions{
		Predicate:  "topic_rewarm",
		Lifecycle:  core.LifecycleDeepArchived,
		SearchTier: core.SearchTierHot,
	})
	seedNaturalFact(t, db, "fact_rewarm_seed", core.FactTypeTransientContext, "用户今天使用了同主题活跃事项。", naturalFactOptions{Predicate: "topic_rewarm"})
	insertNaturalAccessEvent(t, db, "access_archived_prompt", "fact_archived", "prompt_injected", now.Add(-2*time.Hour))
	insertNaturalAccessEvent(t, db, "access_deep_prompt", "fact_deep_archived", "prompt_injected", now.Add(-2*time.Hour))
	insertNaturalAccessEvent(t, db, "access_rewarm_seed", "fact_rewarm_seed", "retrieved", now.Add(-1*time.Hour))

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.ReactivatedNodes < 1 {
		t.Fatalf("reactivated nodes = %d, want archived node reactivated", result.ReactivatedNodes)
	}
	requireNaturalEventCount(t, db, "fact_archived", "storage_rewarm_candidate", 1)
	requireNaturalEventCount(t, db, "fact_deep_archived", "storage_rewarm_candidate", 0)
	requireSearchTier(t, db, "fact_archived", string(core.SearchTierCold))
	requireSearchTier(t, db, "fact_deep_archived", string(core.SearchTierHot))
	requireFactGovernance(t, db, "fact_archived", string(core.VisibilityVisible), string(core.ValidityValid), string(core.LifecycleArchived))
	requireFactGovernance(t, db, "fact_deep_archived", string(core.VisibilityVisible), string(core.ValidityValid), string(core.LifecycleDeepArchived))
}

func TestNaturalRunUsesRecentAccessEventsAndStructuralSignals(t *testing.T) {
	ctx := context.Background()
	now := fixedNaturalNow()
	svc, db := openNaturalTestService(t, ctx, now)
	defer svc.Close()
	seedNaturalFact(t, db, "fact_reactivated", core.FactTypeTransientContext, "用户临时关注结构重激活事项。", naturalFactOptions{Predicate: "topic_reactivate"})
	seedNaturalFact(t, db, "fact_seed", core.FactTypeTransientContext, "用户今天再次使用了同主题事项。", naturalFactOptions{Predicate: "topic_reactivate"})
	insertNaturalAccessEvent(t, db, "access_prompt", "fact_reactivated", "prompt_injected", now.Add(-2*time.Hour))
	insertNaturalAccessEvent(t, db, "access_seed", "fact_seed", "retrieved", now.Add(-1*time.Hour))

	result, err := svc.RunNaturalMemoryCycle(ctx, RunNaturalMemoryCycleRequest{
		PersonaID: "default",
		RunKind:   NaturalMemoryRunManual,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("natural run: %v", err)
	}
	if result.ReactivatedNodes < 1 {
		t.Fatalf("reactivated nodes = %d, want at least one", result.ReactivatedNodes)
	}
	requireNaturalEventCount(t, db, "fact_reactivated", "reactivated", 1)
}

type naturalFactOptions struct {
	Predicate  string
	Visibility core.VisibilityStatus
	Pinned     bool
	Lifecycle  core.LifecycleStatus
	SearchTier core.SearchTier
}

type searchDocumentSnapshot struct {
	Text        string
	Tier        string
	Visibility  string
	Sensitivity string
	Lifecycle   string
	Searchable  int
	UpdatedAt   string
}

func openNaturalTestService(t *testing.T, ctx context.Context, now time.Time) (*service, *sql.DB) {
	t.Helper()
	return openNaturalTestServiceWithOptions(t, ctx, now, "Asia/Shanghai", defaultNaturalMemoryOptions())
}

func openNaturalTestServiceWithOptions(t *testing.T, ctx context.Context, now time.Time, timezone string, opts NaturalMemoryOptions) (*service, *sql.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := Open(ctx, Options{
		DBPath:        dbPath,
		PersonaID:     "default",
		AutoMigrate:   true,
		EnableFTS:     true,
		Timezone:      timezone,
		Now:           func() time.Time { return now },
		NaturalMemory: opts,
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	concrete, ok := svc.(*service)
	if !ok {
		t.Fatalf("service type = %T, want *service", svc)
	}
	return concrete, concrete.sqlDB
}

func seedNaturalFact(t *testing.T, db *sql.DB, id string, factType core.FactType, summary string, opts naturalFactOptions) {
	t.Helper()
	predicate := opts.Predicate
	if predicate == "" {
		predicate = "likes"
	}
	visibility := opts.Visibility
	if visibility == "" {
		visibility = core.VisibilityVisible
	}
	lifecycle := opts.Lifecycle
	if lifecycle == "" {
		lifecycle = core.LifecycleActive
	}
	searchTier := opts.SearchTier
	if searchTier == "" {
		searchTier = core.SearchTierHot
	}
	created := fixedNaturalNow().AddDate(0, 0, -45).Format(time.RFC3339Nano)
	if _, err := db.Exec(`
INSERT OR IGNORE INTO personas(id, display_name, created_at, updated_at)
VALUES ('default', 'Default', ?, ?)`, created, created); err != nil {
		t.Fatalf("insert persona: %v", err)
	}
	if _, err := db.Exec(`
INSERT INTO facts (
    id, persona_id, predicate, object_literal, content_summary, fact_type,
    ingested_at, extraction_confidence, extraction_confidence_score, importance,
		sensitivity_level, validity_status, visibility_status, lifecycle_status,
	    pinned, access_count, reinforcement_count, searchable, created_at
	) VALUES (?, 'default', ?, ?, ?, ?, ?, 'explicit', 0.8, 0.7,
	          'normal', 'valid', ?, ?, ?, 0, 0, 1, ?)`,
		id, predicate, id, summary, string(factType), created, string(visibility), string(lifecycle), naturalBoolInt(opts.Pinned), created); err != nil {
		t.Fatalf("insert fact %s: %v", id, err)
	}
	if _, err := db.Exec(`
INSERT INTO memory_search_documents (
    id, persona_id, node_type, node_id, search_text, search_tier,
    visibility_status, sensitivity_level, lifecycle_status, searchable, updated_at
	) VALUES (?, 'default', 'fact', ?, ?, ?, ?, 'normal', ?, 1, ?)`,
		"search_"+id, id, summary, string(searchTier), string(visibility), string(lifecycle), created); err != nil {
		t.Fatalf("insert search document %s: %v", id, err)
	}
}

func fixedNaturalNow() time.Time {
	return time.Date(2026, 6, 5, 3, 31, 0, 0, time.FixedZone("CST", 8*60*60))
}

func naturalBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func requireTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}

func requireSearchTier(t *testing.T, db *sql.DB, factID string, want string) {
	t.Helper()
	var tier string
	if err := db.QueryRow(`
SELECT search_tier FROM memory_search_documents
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = ?`, factID).Scan(&tier); err != nil {
		t.Fatalf("query search tier: %v", err)
	}
	if tier != want {
		t.Fatalf("search tier = %s, want %s", tier, want)
	}
}

func readSearchDocumentSnapshot(t *testing.T, db *sql.DB, factID string) searchDocumentSnapshot {
	t.Helper()
	var snap searchDocumentSnapshot
	if err := db.QueryRow(`
SELECT search_text, search_tier, visibility_status, sensitivity_level, lifecycle_status, searchable, updated_at
FROM memory_search_documents
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = ?`, factID).Scan(
		&snap.Text, &snap.Tier, &snap.Visibility, &snap.Sensitivity, &snap.Lifecycle, &snap.Searchable, &snap.UpdatedAt,
	); err != nil {
		t.Fatalf("query search document: %v", err)
	}
	return snap
}

func requireFactGovernance(t *testing.T, db *sql.DB, factID string, wantVisibility string, wantValidity string, wantLifecycle string) {
	t.Helper()
	var visibility, validity, lifecycle string
	if err := db.QueryRow(`
SELECT visibility_status, validity_status, lifecycle_status
FROM facts
WHERE persona_id = 'default' AND id = ?`, factID).Scan(&visibility, &validity, &lifecycle); err != nil {
		t.Fatalf("query fact governance: %v", err)
	}
	if visibility != wantVisibility || validity != wantValidity || lifecycle != wantLifecycle {
		t.Fatalf("fact governance = %s/%s/%s, want %s/%s/%s", visibility, validity, lifecycle, wantVisibility, wantValidity, wantLifecycle)
	}
}

func insertNaturalIndexMap(t *testing.T, db *sql.DB, nodeType core.NodeType, nodeID string) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO memory_index_map(id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES (?, 'default', ?, ?, 1001, 'indexed')`,
		"map_"+nodeID, string(nodeType), nodeID); err != nil {
		t.Fatalf("insert index map: %v", err)
	}
}

func requireQueueCount(t *testing.T, db *sql.DB, nodeType string, nodeID string, operation string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM index_sync_queue
WHERE persona_id = 'default' AND node_type = ? AND node_id = ? AND operation = ?`, nodeType, nodeID, operation).Scan(&count); err != nil {
		t.Fatalf("count queue: %v", err)
	}
	if count != want {
		t.Fatalf("queue count = %d, want %d", count, want)
	}
}

func requireNaturalRunTimezone(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	var timezone string
	if err := db.QueryRow(`
SELECT timezone
FROM memory_natural_runs
WHERE persona_id = 'default'
ORDER BY completed_at DESC
LIMIT 1`).Scan(&timezone); err != nil {
		t.Fatalf("query natural run timezone: %v", err)
	}
	if timezone != want {
		t.Fatalf("natural run timezone = %s, want %s", timezone, want)
	}
}

func requireNaturalStateCount(t *testing.T, db *sql.DB, nodeID string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_natural_states
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = ?`, nodeID).Scan(&count); err != nil {
		t.Fatalf("count natural state: %v", err)
	}
	if count != want {
		t.Fatalf("natural state count = %d, want %d", count, want)
	}
}

func insertNaturalAccessEvent(t *testing.T, db *sql.DB, id string, factID string, accessType string, at time.Time) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO memory_access_events(id, persona_id, node_type, node_id, access_type, created_at)
VALUES (?, 'default', 'fact', ?, ?, ?)`,
		id, factID, accessType, at.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("insert access event %s: %v", id, err)
	}
}

func requireNaturalEventCount(t *testing.T, db *sql.DB, nodeID string, eventType string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_natural_events
WHERE persona_id = 'default' AND node_type = 'fact' AND node_id = ? AND event_type = ?`, nodeID, eventType).Scan(&count); err != nil {
		t.Fatalf("count natural events: %v", err)
	}
	if count != want {
		t.Fatalf("natural event %s/%s count = %d, want %d", nodeID, eventType, count, want)
	}
}

func requireNaturalEventTypeCount(t *testing.T, db *sql.DB, eventType string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_natural_events
WHERE persona_id = 'default' AND event_type = ?`, eventType).Scan(&count); err != nil {
		t.Fatalf("count natural event type: %v", err)
	}
	if count != want {
		t.Fatalf("natural event type %s count = %d, want %d", eventType, count, want)
	}
}
