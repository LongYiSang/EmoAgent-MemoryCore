package sqlite_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestRetentionRepositoryExpiresFactsAndSyncsSearchTier(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	expired := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_expired", "上线准备", "用户近期忙于上线准备。", false)
	future := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_future", "咖啡", "用户喜欢咖啡。", false)
	now := fixedRetentionNow()
	setFactValidTo(t, db.SQLDB(), expired.ID, now.Add(-time.Hour))
	setFactValidTo(t, db.SQLDB(), future.ID, now.Add(time.Hour))
	requireSearchDocumentLifecycle(t, db.SQLDB(), expired.ID, string(core.LifecycleActive), string(core.SearchTierHot))

	repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
	result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default"})
	if err != nil {
		t.Fatalf("run retention: %v", err)
	}
	if result.EvaluatedFacts != 1 || result.ExpiredFacts != 1 || result.ArchivedFacts != 1 || result.SearchDocumentsSynced != 1 || result.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("retention result = %#v", result)
	}
	requireFactRetentionState(t, db.SQLDB(), expired.ID, string(core.ValidityInvalidated), string(core.LifecycleArchived), now)
	requireFactRetentionState(t, db.SQLDB(), future.ID, string(core.ValidityValid), string(core.LifecycleActive), time.Time{})
	requireSearchDocumentLifecycle(t, db.SQLDB(), expired.ID, string(core.LifecycleArchived), string(core.SearchTierCold))

	second, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default", Now: now})
	if err != nil {
		t.Fatalf("run retention again: %v", err)
	}
	if second.EvaluatedFacts != 0 || second.ExpiredFacts != 0 || second.ArchivedFacts != 0 || second.SearchDocumentsSynced != 0 || second.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("second retention result = %#v, want idempotent zero counts", second)
	}
}

func TestRetentionRepositoryDryRunDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	fact := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_dry_run", "茶", "用户喜欢茶。", false)
	now := fixedRetentionNow()
	setFactValidTo(t, db.SQLDB(), fact.ID, now.Add(-time.Hour))

	repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
	result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default", DryRun: true})
	if err != nil {
		t.Fatalf("dry-run retention: %v", err)
	}
	if result.EvaluatedFacts != 1 || result.ExpiredFacts != 1 || result.ArchivedFacts != 1 || result.SearchDocumentsSynced != 0 || result.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("dry-run result = %#v", result)
	}
	requireFactRetentionState(t, db.SQLDB(), fact.ID, string(core.ValidityValid), string(core.LifecycleActive), time.Time{})
	requireSearchDocumentLifecycle(t, db.SQLDB(), fact.ID, string(core.LifecycleActive), string(core.SearchTierHot))
	requireQueueCount(t, db.SQLDB(), "fact", fact.ID, "upsert_node", 0)
}

func TestRetentionRepositoryPinnedFactInvalidatesWithoutArchive(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	fact := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_pinned", "杭州", "用户喜欢杭州。", true)
	now := fixedRetentionNow()
	setFactValidTo(t, db.SQLDB(), fact.ID, now.Add(-time.Hour))
	setFactLifecycle(t, db.SQLDB(), fact.ID, string(core.LifecycleConsolidated))

	repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
	result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default"})
	if err != nil {
		t.Fatalf("run retention: %v", err)
	}
	if result.EvaluatedFacts != 1 || result.ExpiredFacts != 1 || result.ArchivedFacts != 0 || result.SearchDocumentsSynced != 1 {
		t.Fatalf("retention result = %#v", result)
	}
	requireFactRetentionState(t, db.SQLDB(), fact.ID, string(core.ValidityInvalidated), string(core.LifecycleConsolidated), now)
	requireSearchDocumentLifecycle(t, db.SQLDB(), fact.ID, string(core.LifecycleConsolidated), string(core.SearchTierWarm))
}

func TestRetentionRepositoryInvalidatesProtectedFactTypesWithoutArchive(t *testing.T) {
	for _, tt := range []struct {
		name     string
		factType core.FactType
	}{
		{name: "core_identity", factType: core.FactTypeCoreIdentity},
		{name: "commitment", factType: core.FactTypeCommitment},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openMigratedDB(t, ctx)
			defer db.Close()
			seedConsolidationStoreGraph(t, ctx, db.SQLDB())

			fact := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_"+tt.name, "Long", "用户偏好被称呼为 Long。", false)
			now := fixedRetentionNow()
			setFactValidTo(t, db.SQLDB(), fact.ID, now.Add(-time.Hour))
			setFactType(t, db.SQLDB(), fact.ID, string(tt.factType))

			repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
			result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default"})
			if err != nil {
				t.Fatalf("run retention: %v", err)
			}
			if result.EvaluatedFacts != 1 || result.ExpiredFacts != 1 || result.ArchivedFacts != 0 || result.SearchDocumentsSynced != 1 {
				t.Fatalf("retention result = %#v", result)
			}
			requireFactRetentionState(t, db.SQLDB(), fact.ID, string(core.ValidityInvalidated), string(core.LifecycleActive), now)
			requireSearchDocumentLifecycle(t, db.SQLDB(), fact.ID, string(core.LifecycleActive), string(core.SearchTierHot))
		})
	}
}

func TestRetentionRepositoryEnqueuesMirrorUpsertForMappedFact(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	fact := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_mapped", "咖啡", "用户喜欢咖啡。", false)
	now := fixedRetentionNow()
	setFactValidTo(t, db.SQLDB(), fact.ID, now.Add(-time.Hour))
	insertIndexMap(t, db.SQLDB(), core.NodeTypeFact, fact.ID)

	repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
	result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default"})
	if err != nil {
		t.Fatalf("run retention: %v", err)
	}
	if result.MirrorUpdatesEnqueued != 1 {
		t.Fatalf("mirror updates enqueued = %d, want 1", result.MirrorUpdatesEnqueued)
	}
	requireQueueCount(t, db.SQLDB(), "fact", fact.ID, "upsert_node", 1)

	second, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default", Now: now})
	if err != nil {
		t.Fatalf("run retention again: %v", err)
	}
	if second.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("second mirror updates enqueued = %d, want 0", second.MirrorUpdatesEnqueued)
	}
	requireQueueCount(t, db.SQLDB(), "fact", fact.ID, "upsert_node", 1)
}

func TestRetentionRepositoryDeepArchivesOldArchivedFacts(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	fact := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_deep_archive", "旧项目", "用户以前参与过旧项目。", false)
	now := fixedRetentionNow()
	archivedAt := now.AddDate(0, 0, -181)
	setFactArchivedAt(t, db.SQLDB(), fact.ID, archivedAt)

	repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
	result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default", DeepArchiveAfterDays: 180})
	if err != nil {
		t.Fatalf("run deep archive retention: %v", err)
	}
	if result.EvaluatedFacts != 1 || result.ExpiredFacts != 0 || result.ArchivedFacts != 0 || result.DeepArchivedFacts != 1 || result.SearchDocumentsSynced != 1 || result.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("deep archive result = %#v", result)
	}
	requireFactRetentionState(t, db.SQLDB(), fact.ID, string(core.ValidityValid), string(core.LifecycleDeepArchived), now)
	requireSearchDocumentLifecycle(t, db.SQLDB(), fact.ID, string(core.LifecycleDeepArchived), string(core.SearchTierDeepCold))
}

func TestRetentionRepositoryDeepArchiveDryRunDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	fact := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_deep_archive_dry_run", "旧项目", "用户以前参与过旧项目。", false)
	now := fixedRetentionNow()
	setFactArchivedAt(t, db.SQLDB(), fact.ID, now.AddDate(0, 0, -181))

	repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
	result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default", DeepArchiveAfterDays: 180, DryRun: true})
	if err != nil {
		t.Fatalf("dry-run deep archive retention: %v", err)
	}
	if result.EvaluatedFacts != 1 || result.ExpiredFacts != 0 || result.ArchivedFacts != 0 || result.DeepArchivedFacts != 1 || result.SearchDocumentsSynced != 0 || result.MirrorUpdatesEnqueued != 0 {
		t.Fatalf("dry-run deep archive result = %#v", result)
	}
	requireFactRetentionState(t, db.SQLDB(), fact.ID, string(core.ValidityValid), string(core.LifecycleArchived), now.AddDate(0, 0, -181))
	requireSearchDocumentLifecycle(t, db.SQLDB(), fact.ID, string(core.LifecycleArchived), string(core.SearchTierCold))
	requireQueueCount(t, db.SQLDB(), "fact", fact.ID, "upsert_node", 0)
}

func TestRetentionRepositoryDeepArchiveSkipsPinnedCoreAndCommitmentFacts(t *testing.T) {
	tests := []struct {
		name     string
		pinned   bool
		factType core.FactType
	}{
		{name: "pinned", pinned: true, factType: core.FactTypeStablePreference},
		{name: "core_identity", factType: core.FactTypeCoreIdentity},
		{name: "commitment", factType: core.FactTypeCommitment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db := openMigratedDB(t, ctx)
			defer db.Close()
			seedConsolidationStoreGraph(t, ctx, db.SQLDB())

			fact := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_deep_archive_"+tt.name, "Long", "用户偏好被称呼为 Long。", tt.pinned)
			now := fixedRetentionNow()
			setFactType(t, db.SQLDB(), fact.ID, string(tt.factType))
			setFactArchivedAt(t, db.SQLDB(), fact.ID, now.AddDate(0, 0, -181))

			repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
			result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default", DeepArchiveAfterDays: 180})
			if err != nil {
				t.Fatalf("run deep archive retention: %v", err)
			}
			if result.DeepArchivedFacts != 0 || result.SearchDocumentsSynced != 0 || result.MirrorUpdatesEnqueued != 0 {
				t.Fatalf("deep archive protected result = %#v", result)
			}
			requireFactRetentionState(t, db.SQLDB(), fact.ID, string(core.ValidityValid), string(core.LifecycleArchived), now.AddDate(0, 0, -181))
			requireSearchDocumentLifecycle(t, db.SQLDB(), fact.ID, string(core.LifecycleArchived), string(core.SearchTierCold))
		})
	}
}

func TestRetentionRepositoryDeepArchiveEnqueuesMirrorUpsertForMappedFact(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedConsolidationStoreGraph(t, ctx, db.SQLDB())

	fact := insertRetentionFact(t, ctx, db.SQLDB(), "fact_retention_deep_archive_mapped", "旧项目", "用户以前参与过旧项目。", false)
	now := fixedRetentionNow()
	setFactArchivedAt(t, db.SQLDB(), fact.ID, now.AddDate(0, 0, -181))
	insertIndexMap(t, db.SQLDB(), core.NodeTypeFact, fact.ID)

	repo := memsqlite.NewRetentionRepository(db.SQLDB(), fixedRetentionIDs(), func() time.Time { return now })
	result, err := repo.Run(ctx, memsqlite.RetentionRequest{PersonaID: "default", DeepArchiveAfterDays: 180})
	if err != nil {
		t.Fatalf("run deep archive retention: %v", err)
	}
	if result.EvaluatedFacts != 1 || result.ExpiredFacts != 0 || result.ArchivedFacts != 0 || result.DeepArchivedFacts != 1 || result.MirrorUpdatesEnqueued != 1 {
		t.Fatalf("deep archive mapped result = %#v", result)
	}
	requireQueueCount(t, db.SQLDB(), "fact", fact.ID, "upsert_node", 1)
}

func setFactValidTo(t *testing.T, db *sql.DB, factID string, validTo time.Time) {
	t.Helper()

	if _, err := db.Exec(`
UPDATE facts
SET valid_to = ?
WHERE id = ?`, validTo.UTC().Format(time.RFC3339Nano), factID); err != nil {
		t.Fatalf("set fact valid_to: %v", err)
	}
}

func setFactArchivedAt(t *testing.T, db *sql.DB, factID string, archivedAt time.Time) {
	t.Helper()

	if _, err := db.Exec(`
UPDATE facts
SET lifecycle_status = 'archived',
    updated_at = ?
WHERE id = ?`, archivedAt.UTC().Format(time.RFC3339Nano), factID); err != nil {
		t.Fatalf("set fact archived_at proxy: %v", err)
	}
	if err := memsqlite.NewSearchRepository(db).UpsertFactDocument(context.Background(), "default", factID); err != nil {
		t.Fatalf("refresh fact search document: %v", err)
	}
}

func insertRetentionFact(t *testing.T, ctx context.Context, db *sql.DB, factID string, object string, summary string, pinned bool) core.Fact {
	t.Helper()

	fact := core.Fact{
		ID:                   factID,
		PersonaID:            "default",
		SubjectEntityID:      ptr("ent_user"),
		Predicate:            "likes",
		ObjectLiteral:        &object,
		ContentSummary:       summary,
		FactType:             core.FactTypeStablePreference,
		ExtractionConfidence: core.ExtractionConfidenceExplicit,
		Importance:           0.7,
		LifecycleStatus:      core.LifecycleActive,
		Pinned:               pinned,
		Searchable:           true,
	}
	if err := memsqlite.NewFactRepository(db).Insert(ctx, fact); err != nil {
		t.Fatalf("insert retention fact: %v", err)
	}
	if err := memsqlite.NewSearchRepository(db).UpsertFactDocument(ctx, "default", fact.ID); err != nil {
		t.Fatalf("upsert retention fact search document: %v", err)
	}
	return fact
}

func setFactLifecycle(t *testing.T, db *sql.DB, factID string, lifecycle string) {
	t.Helper()

	if _, err := db.Exec(`
UPDATE facts
SET lifecycle_status = ?
WHERE id = ?`, lifecycle, factID); err != nil {
		t.Fatalf("set fact lifecycle: %v", err)
	}
	if err := memsqlite.NewSearchRepository(db).UpsertFactDocument(context.Background(), "default", factID); err != nil {
		t.Fatalf("refresh fact search document: %v", err)
	}
}

func setFactType(t *testing.T, db *sql.DB, factID string, factType string) {
	t.Helper()

	if _, err := db.Exec(`
UPDATE facts
SET fact_type = ?
WHERE id = ?`, factType, factID); err != nil {
		t.Fatalf("set fact type: %v", err)
	}
	if err := memsqlite.NewSearchRepository(db).UpsertFactDocument(context.Background(), "default", factID); err != nil {
		t.Fatalf("refresh fact search document: %v", err)
	}
}

func requireFactRetentionState(t *testing.T, db *sql.DB, factID string, wantValidity string, wantLifecycle string, wantUpdatedAt time.Time) {
	t.Helper()

	var validity, lifecycle string
	var updatedAt sql.NullString
	if err := db.QueryRow(`
SELECT validity_status, lifecycle_status, updated_at
FROM facts
WHERE id = ?`, factID).Scan(&validity, &lifecycle, &updatedAt); err != nil {
		t.Fatalf("query fact retention state: %v", err)
	}
	if validity != wantValidity || lifecycle != wantLifecycle {
		t.Fatalf("fact %s state = %s/%s, want %s/%s", factID, validity, lifecycle, wantValidity, wantLifecycle)
	}
	if wantUpdatedAt.IsZero() {
		if updatedAt.Valid {
			t.Fatalf("fact %s updated_at = %q, want null", factID, updatedAt.String)
		}
		return
	}
	if !updatedAt.Valid || updatedAt.String != wantUpdatedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("fact %s updated_at = %v, want %s", factID, updatedAt, wantUpdatedAt.UTC().Format(time.RFC3339Nano))
	}
}

func requireSearchDocumentLifecycle(t *testing.T, db *sql.DB, factID string, wantLifecycle string, wantTier string) {
	t.Helper()

	var lifecycle, tier string
	if err := db.QueryRow(`
SELECT lifecycle_status, search_tier
FROM memory_search_documents
WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&lifecycle, &tier); err != nil {
		t.Fatalf("query search document lifecycle: %v", err)
	}
	if lifecycle != wantLifecycle || tier != wantTier {
		t.Fatalf("search document %s lifecycle/tier = %s/%s, want %s/%s", factID, lifecycle, tier, wantLifecycle, wantTier)
	}
}

func fixedRetentionIDs() func() string {
	index := 0
	return func() string {
		index++
		return "retention_id_" + string(rune('0'+index))
	}
}

func fixedRetentionNow() time.Time {
	return time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
}
