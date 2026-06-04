package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestCurationSelectsDeltaFactsFromCheckpoint(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedCurationGraph(t, ctx, db.SQLDB())

	before := insertCurationFact(t, ctx, db.SQLDB(), "fact_before", "likes", "用户喜欢热茶。", curationTime(0))
	_ = before
	delta := insertCurationFact(t, ctx, db.SQLDB(), "fact_delta", "likes", "用户喜欢科幻小说。", curationTime(1))
	other := insertCurationFact(t, ctx, db.SQLDB(), "fact_same_second", "likes", "用户喜欢历史小说。", curationTime(1))
	excluded := insertCurationFact(t, ctx, db.SQLDB(), "fact_identity", "prefers_name", "用户偏好被叫 Long。", curationTime(2))
	updateCurationFact(t, db.SQLDB(), excluded.ID, "fact_type = 'core_identity'")

	repo := memsqlite.NewCurationRepository(db.SQLDB(), fixedCurationIDs(), fixedCurationNow)
	facts, err := repo.LoadDeltaFacts(ctx, memsqlite.CurationDeltaQuery{
		PersonaID:        "default",
		SinceCreatedAt:   ptrTime(curationTime(0)),
		SinceFactID:      "fact_before",
		MaxNewFacts:      10,
		IncludeFactTypes: []string{string(core.FactTypeStablePreference), string(core.FactTypeRelationalState), string(core.FactTypeTransientContext), string(core.FactTypeTaskRelevantContext)},
		ExcludeFactTypes: []string{string(core.FactTypeCoreIdentity), string(core.FactTypeCommitment)},
	})
	if err != nil {
		t.Fatalf("load delta facts: %v", err)
	}
	gotIDs := curationFactIDs(facts)
	wantIDs := []string{delta.ID, other.ID}
	if strings.Join(gotIDs, ",") != strings.Join(wantIDs, ",") {
		t.Fatalf("delta fact ids = %#v, want %#v", gotIDs, wantIDs)
	}
}

func TestCurationLoadDeltaFactsNormalizesMixedCreatedAtWindow(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedCurationGraph(t, ctx, db.SQLDB())

	fact := insertCurationFact(t, ctx, db.SQLDB(), "fact_sqlite_created_at", "likes", "用户喜欢绿茶。", curationTime(1))
	if _, err := db.SQLDB().ExecContext(ctx, `UPDATE facts SET created_at = ?, updated_at = NULL WHERE id = ?`, "2026-06-04 08:00:00", fact.ID); err != nil {
		t.Fatalf("seed sqlite created_at: %v", err)
	}
	boundary := time.Date(2026, 6, 4, 7, 59, 59, 0, time.UTC)
	repo := memsqlite.NewCurationRepository(db.SQLDB(), fixedCurationIDs(), fixedCurationNow)

	t.Run("since", func(t *testing.T) {
		facts, err := repo.LoadDeltaFacts(ctx, memsqlite.CurationDeltaQuery{
			PersonaID:      "default",
			SinceCreatedAt: ptrTime(boundary),
			MaxNewFacts:    10,
		})
		if err != nil {
			t.Fatalf("load delta facts: %v", err)
		}
		if !containsString(curationFactIDs(facts), fact.ID) {
			t.Fatalf("delta fact ids = %#v, want %s", curationFactIDs(facts), fact.ID)
		}
	})

	t.Run("until", func(t *testing.T) {
		facts, err := repo.LoadDeltaFacts(ctx, memsqlite.CurationDeltaQuery{
			PersonaID:        "default",
			UntilCreatedAt:   ptrTime(boundary),
			MaxNewFacts:      10,
			IncludeFactTypes: []string{string(core.FactTypeStablePreference)},
		})
		if err != nil {
			t.Fatalf("load delta facts: %v", err)
		}
		if containsString(curationFactIDs(facts), fact.ID) {
			t.Fatalf("delta fact ids = %#v, want %s excluded before until boundary", curationFactIDs(facts), fact.ID)
		}
	})
}

func TestCurationRetrievesComparableFactsAndBuildsGroups(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedCurationGraph(t, ctx, db.SQLDB())

	delta := insertCurationFact(t, ctx, db.SQLDB(), "fact_delta_novel", "likes", "用户喜欢科幻小说。", curationTime(3))
	existing := insertCurationFact(t, ctx, db.SQLDB(), "fact_existing_novel", "likes", "用户周末喜欢读科幻小说。", curationTime(1))
	inBatch := insertCurationFact(t, ctx, db.SQLDB(), "fact_batch_novel", "likes", "用户喜欢科幻小说和太空歌剧。", curationTime(4))
	dislikes := insertCurationFact(t, ctx, db.SQLDB(), "fact_dislikes_novel", "dislikes", "用户不喜欢科幻小说剧透。", curationTime(2))
	hidden := insertCurationFact(t, ctx, db.SQLDB(), "fact_hidden_novel", "likes", "隐藏候选不应参与。", curationTime(2))
	updateCurationFact(t, db.SQLDB(), hidden.ID, "visibility_status = 'hidden'")

	repo := memsqlite.NewCurationRepository(db.SQLDB(), fixedCurationIDs(), fixedCurationNow)
	candidates, err := repo.RetrieveComparableFacts(ctx, memsqlite.CurationComparableQuery{
		PersonaID:             "default",
		DeltaFactID:           delta.ID,
		CandidateLimitPerFact: 10,
	})
	if err != nil {
		t.Fatalf("retrieve comparable facts: %v", err)
	}
	gotCandidateIDs := curationFactIDs(candidates)
	if !containsString(gotCandidateIDs, existing.ID) || containsString(gotCandidateIDs, dislikes.ID) || containsString(gotCandidateIDs, hidden.ID) {
		t.Fatalf("candidate ids = %#v, want existing only among seeded exclusions", gotCandidateIDs)
	}

	groups := repo.BuildGroups(
		[]core.Fact{delta, inBatch},
		map[string][]core.Fact{
			delta.ID:   {existing, inBatch},
			inBatch.ID: {delta, existing},
		},
		8,
	)
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want one connected group", groups)
	}
	groupIDs := curationGroupFactIDs(groups[0].Facts)
	for _, want := range []string{delta.ID, existing.ID, inBatch.ID} {
		if !containsString(groupIDs, want) {
			t.Fatalf("group ids = %#v, missing %s", groupIDs, want)
		}
	}
}

func TestCurationBuildGroupsDoesNotUnionUnrelatedSamePredicateFacts(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedCurationGraph(t, ctx, db.SQLDB())

	canonical := insertCurationFact(t, ctx, db.SQLDB(), "fact_novel_canonical", "likes", "用户喜欢科幻小说。", curationTime(1))
	updateCurationFact(t, db.SQLDB(), canonical.ID, "object_literal = '科幻小说'")
	weekend := insertCurationFact(t, ctx, db.SQLDB(), "fact_novel_weekend", "likes", "用户周末喜欢读科幻小说。", curationTime(2))
	spaceOpera := insertCurationFact(t, ctx, db.SQLDB(), "fact_novel_space_opera", "likes", "用户喜欢科幻小说和太空歌剧。", curationTime(3))
	jazz := insertCurationFact(t, ctx, db.SQLDB(), "fact_jazz_music", "likes", "用户喜欢听爵士乐。", curationTime(4))
	hiking := insertCurationFact(t, ctx, db.SQLDB(), "fact_hiking", "likes", "用户喜欢周末徒步旅行。", curationTime(5))

	repo := memsqlite.NewCurationRepository(db.SQLDB(), fixedCurationIDs(), fixedCurationNow)
	deltaFacts := []core.Fact{canonical, weekend, spaceOpera, jazz, hiking}
	candidates := map[string][]core.Fact{}
	for _, fact := range deltaFacts {
		found, err := repo.RetrieveComparableFacts(ctx, memsqlite.CurationComparableQuery{
			PersonaID:             "default",
			DeltaFactID:           fact.ID,
			CandidateLimitPerFact: 10,
		})
		if err != nil {
			t.Fatalf("retrieve comparable facts for %s: %v", fact.ID, err)
		}
		candidates[fact.ID] = found
	}

	groups := repo.BuildGroups(deltaFacts, candidates, 8)
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want only one overlapping preference group", groups)
	}
	groupIDs := curationGroupFactIDs(groups[0].Facts)
	for _, want := range []string{canonical.ID, weekend.ID, spaceOpera.ID} {
		if !containsString(groupIDs, want) {
			t.Fatalf("group ids = %#v, missing %s", groupIDs, want)
		}
	}
	for _, unwanted := range []string{jazz.ID, hiking.ID} {
		if containsString(groupIDs, unwanted) {
			t.Fatalf("group ids = %#v, should not include unrelated same-predicate fact %s", groupIDs, unwanted)
		}
	}
}

func TestCurationBuildGroupsRetainsDeltaWhenGroupIsTruncated(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedCurationGraph(t, ctx, db.SQLDB())

	oldA := insertCurationFact(t, ctx, db.SQLDB(), "fact_old_a", "likes", "用户喜欢科幻小说。", curationTime(1))
	oldB := insertCurationFact(t, ctx, db.SQLDB(), "fact_old_b", "likes", "用户周末喜欢读科幻小说。", curationTime(2))
	delta := insertCurationFact(t, ctx, db.SQLDB(), "fact_new_delta", "likes", "用户喜欢科幻小说和太空歌剧。", curationTime(3))

	repo := memsqlite.NewCurationRepository(db.SQLDB(), fixedCurationIDs(), fixedCurationNow)
	groups := repo.BuildGroups(
		[]core.Fact{delta},
		map[string][]core.Fact{delta.ID: {oldA, oldB}},
		2,
	)
	if len(groups) != 1 {
		t.Fatalf("groups = %#v, want one truncated group", groups)
	}
	groupIDs := curationGroupFactIDs(groups[0].Facts)
	if !containsString(groupIDs, delta.ID) {
		t.Fatalf("group ids = %#v, want delta retained after truncation", groupIDs)
	}
	if len(groupIDs) != 2 {
		t.Fatalf("group ids = %#v, want max group size preserved", groupIDs)
	}
}

func TestCurationDryRunWritesAuditWithoutMutationOrCheckpoint(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedCurationGraph(t, ctx, db.SQLDB())

	canonical := insertCurationFact(t, ctx, db.SQLDB(), "fact_dry_canonical", "likes", "用户喜欢科幻小说。", curationTime(1))
	source := insertCurationFact(t, ctx, db.SQLDB(), "fact_dry_source", "likes", "用户周末喜欢读科幻小说。", curationTime(2))
	repo := memsqlite.NewCurationRepository(db.SQLDB(), fixedCurationIDs(), fixedCurationNow)

	result, err := repo.ApplyDecisions(ctx, memsqlite.CurationApplyRequest{
		PersonaID:         "default",
		Mode:              memsqlite.CurationModeDryRun,
		Trigger:           "test",
		NewFactCount:      1,
		CursorToCreatedAt: ptrTime(source.CreatedAt),
		CursorToFactID:    source.ID,
		UpdateCheckpoint:  true,
		Groups: []memsqlite.CurationPreparedGroup{{
			ID:    "group_dry",
			Facts: curationGroupFacts(canonical.ID, source.ID),
			Decision: memsqlite.CurationDecision{
				Decision:               "merge_into_existing",
				SemanticRelation:       "refinement",
				AnswerGain:             "small",
				Confidence:             0.95,
				CanonicalFactID:        canonical.ID,
				SourceFactIDs:          []string{canonical.ID, source.ID},
				MergedContentSummary:   "用户喜欢科幻小说，尤其周末会读。",
				CanonicalPredicate:     "likes",
				CanonicalObjectLiteral: "科幻小说",
				CanonicalFactType:      string(core.FactTypeStablePreference),
			},
		}},
	})
	if err != nil {
		t.Fatalf("dry-run apply decisions: %v", err)
	}
	if result.AppliedGroupCount != 0 || result.GroupCount != 1 || result.ReviewGroupCount != 0 || result.NoopGroupCount != 1 {
		t.Fatalf("dry-run result = %#v", result)
	}
	requireCurationFactSearchable(t, db.SQLDB(), source.ID, string(core.LifecycleActive), true)
	requireCurationSearchDocument(t, db.SQLDB(), source.ID, true)
	requireTableRowCount(t, db.SQLDB(), "memory_curation_runs", 1)
	requireTableRowCount(t, db.SQLDB(), "memory_curation_groups", 1)
	requireTableRowCount(t, db.SQLDB(), "memory_curation_checkpoints", 0)
}

func TestCurationMergeIntoExistingMarksSourcesUnsearchableAndCopiesEvidence(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedCurationGraph(t, ctx, db.SQLDB())

	canonical := insertCurationFactWithEvidence(t, ctx, db.SQLDB(), "fact_canonical_novel", "likes", "用户喜欢科幻小说。", "ep_old", curationTime(1))
	source := insertCurationFactWithEvidence(t, ctx, db.SQLDB(), "fact_source_novel", "likes", "用户周末喜欢读科幻小说。", "ep_new", curationTime(2))
	insertIndexMapWithTriviumID(t, db.SQLDB(), core.NodeTypeFact, canonical.ID, 2001)
	insertIndexMapWithTriviumID(t, db.SQLDB(), core.NodeTypeFact, source.ID, 2002)

	repo := memsqlite.NewCurationRepository(db.SQLDB(), fixedCurationIDs(), fixedCurationNow)
	result, err := repo.ApplyDecisions(ctx, memsqlite.CurationApplyRequest{
		PersonaID:              "default",
		Mode:                   memsqlite.CurationModeApply,
		Trigger:                "test",
		NewFactCount:           1,
		MinAutoApplyConfidence: 0.88,
		CursorToCreatedAt:      ptrTime(source.CreatedAt),
		CursorToFactID:         source.ID,
		UpdateCheckpoint:       true,
		ProviderID:             "mock",
		ProviderKind:           "mock",
		Model:                  "memory-curator",
		Groups: []memsqlite.CurationPreparedGroup{{
			ID:    "group_apply",
			Facts: curationGroupFacts(canonical.ID, source.ID),
			Decision: memsqlite.CurationDecision{
				Decision:               "merge_into_existing",
				SemanticRelation:       "refinement",
				AnswerGain:             "small",
				Confidence:             0.95,
				CanonicalFactID:        canonical.ID,
				SourceFactIDs:          []string{canonical.ID, source.ID},
				MergedContentSummary:   "用户喜欢科幻小说，尤其周末会读。",
				CanonicalPredicate:     "likes",
				CanonicalObjectLiteral: "科幻小说",
				CanonicalFactType:      string(core.FactTypeStablePreference),
				ReasonCodes:            []string{"same_user_preference", "adds_context"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("apply curation decisions: %v", err)
	}
	if result.AppliedGroupCount != 1 || result.ReviewGroupCount != 0 || result.NoopGroupCount != 0 {
		t.Fatalf("apply result = %#v", result)
	}

	requireCurationFactSummary(t, db.SQLDB(), canonical.ID, "用户喜欢科幻小说，尤其周末会读。", "科幻小说")
	requireCurationFactSearchable(t, db.SQLDB(), canonical.ID, string(core.LifecycleActive), true)
	requireCurationFactSearchable(t, db.SQLDB(), source.ID, string(core.LifecycleConsolidated), false)
	requireCurationSearchDocument(t, db.SQLDB(), canonical.ID, true)
	requireCurationSearchDocument(t, db.SQLDB(), source.ID, false)
	requireFTSRowCount(t, db.SQLDB(), core.NodeTypeFact, source.ID, 0)
	requireSQLiteLinkCount(t, db.SQLDB(), canonical.ID, string(core.LinkTypeEvidencedBy), "ep_old", 1)
	requireSQLiteLinkCount(t, db.SQLDB(), canonical.ID, string(core.LinkTypeEvidencedBy), "ep_new", 1)
	requireSQLiteLinkCount(t, db.SQLDB(), canonical.ID, string(core.LinkTypeDerivedFrom), source.ID, 1)
	requireQueueCount(t, db.SQLDB(), "fact", canonical.ID, "upsert_node", 1)
	requireQueueCount(t, db.SQLDB(), "fact", source.ID, "delete_node", 1)
	requireCurationQueueOperationCount(t, db.SQLDB(), "memory_link", "upsert_edge", 2)
	requireCurationCheckpoint(t, db.SQLDB(), "default", result.RunID, source.CreatedAt, source.ID)
}

func TestCurationCheckpointOnlyUpdatesAfterSuccessfulApply(t *testing.T) {
	ctx := context.Background()
	db := openMigratedDB(t, ctx)
	defer db.Close()
	seedCurationGraph(t, ctx, db.SQLDB())

	canonical := insertCurationFact(t, ctx, db.SQLDB(), "fact_checkpoint_canonical", "likes", "用户喜欢科幻小说。", curationTime(1))
	source := insertCurationFact(t, ctx, db.SQLDB(), "fact_checkpoint_source", "likes", "用户周末喜欢读科幻小说。", curationTime(2))
	repo := memsqlite.NewCurationRepository(db.SQLDB(), fixedCurationIDs(), fixedCurationNow)
	_, err := repo.ApplyDecisions(ctx, memsqlite.CurationApplyRequest{
		PersonaID:              "default",
		Mode:                   memsqlite.CurationModeApply,
		Trigger:                "test",
		CursorToCreatedAt:      ptrTime(source.CreatedAt),
		CursorToFactID:         source.ID,
		UpdateCheckpoint:       false,
		MinAutoApplyConfidence: 0.88,
		Groups: []memsqlite.CurationPreparedGroup{{
			ID:    "group_no_checkpoint",
			Facts: curationGroupFacts(canonical.ID, source.ID),
			Decision: memsqlite.CurationDecision{
				Decision:             "needs_review",
				SemanticRelation:     "unclear",
				AnswerGain:           "unknown",
				Confidence:           0.3,
				CanonicalFactID:      canonical.ID,
				SourceFactIDs:        []string{canonical.ID, source.ID},
				MergedContentSummary: "不应自动整理。",
			},
		}},
	})
	if err != nil {
		t.Fatalf("apply decisions without checkpoint: %v", err)
	}
	requireTableRowCount(t, db.SQLDB(), "memory_curation_checkpoints", 0)
	requireCurationFactSearchable(t, db.SQLDB(), source.ID, string(core.LifecycleActive), true)
}

func seedCurationGraph(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedConsolidationStoreGraph(t, ctx, db)
	episodes := memsqlite.NewEpisodeRepository(db)
	for _, episode := range []core.Episode{
		{ID: "ep_old", PersonaID: "default", SessionID: "s1", Role: core.RoleUser, Content: "我喜欢科幻小说。", OccurredAt: curationTime(1), SourceType: core.SourceTypeChat},
		{ID: "ep_new", PersonaID: "default", SessionID: "s1", Role: core.RoleUser, Content: "我周末喜欢读科幻小说。", OccurredAt: curationTime(2), SourceType: core.SourceTypeChat},
	} {
		if err := episodes.Append(ctx, episode); err != nil {
			t.Fatalf("append curation episode %s: %v", episode.ID, err)
		}
	}
}

func insertCurationFact(t *testing.T, ctx context.Context, db *sql.DB, factID string, predicate string, summary string, createdAt time.Time) core.Fact {
	t.Helper()
	object := strings.TrimSuffix(strings.TrimPrefix(summary, "用户喜欢"), "。")
	fact := core.Fact{
		ID:                   factID,
		PersonaID:            "default",
		SubjectEntityID:      ptr("ent_user"),
		Predicate:            predicate,
		ObjectLiteral:        &object,
		ContentSummary:       summary,
		FactType:             core.FactTypeStablePreference,
		ExtractionConfidence: core.ExtractionConfidenceExplicit,
		Importance:           0.7,
		LifecycleStatus:      core.LifecycleActive,
		Searchable:           true,
	}
	if predicate == "prefers_name" {
		fact.FactType = core.FactTypeCoreIdentity
	}
	if err := memsqlite.NewFactRepository(db).Insert(ctx, fact); err != nil {
		t.Fatalf("insert curation fact %s: %v", factID, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE facts SET created_at = ?, updated_at = NULL WHERE id = ?`, formatTestTime(createdAt), factID); err != nil {
		t.Fatalf("update curation fact created_at: %v", err)
	}
	if err := memsqlite.NewSearchRepository(db).UpsertFactDocument(ctx, "default", factID); err != nil {
		t.Fatalf("upsert curation search document: %v", err)
	}
	fact.CreatedAt = createdAt
	return fact
}

func insertCurationFactWithEvidence(t *testing.T, ctx context.Context, db *sql.DB, factID string, predicate string, summary string, episodeID string, createdAt time.Time) core.Fact {
	t.Helper()
	fact := insertCurationFact(t, ctx, db, factID, predicate, summary, createdAt)
	insertCurationLink(t, ctx, db, "link_"+factID+"_"+episodeID, factID, core.LinkTypeEvidencedBy, core.NodeTypeEpisode, episodeID)
	return fact
}

func insertCurationLink(t *testing.T, ctx context.Context, db *sql.DB, linkID string, fromFactID string, linkType core.LinkType, toType core.NodeType, toID string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    created_by, visibility_status, searchable
) VALUES (?, 'default', 'fact', ?, ?, ?, ?, 'forward', 1.0, 1.0, 'consolidation', 'visible', 1)`,
		linkID, fromFactID, string(linkType), string(toType), toID)
	if err != nil {
		t.Fatalf("insert curation link: %v", err)
	}
}

func updateCurationFact(t *testing.T, db *sql.DB, factID string, setClause string) {
	t.Helper()
	if _, err := db.Exec("UPDATE facts SET "+setClause+" WHERE id = ?", factID); err != nil {
		t.Fatalf("update curation fact %s: %v", factID, err)
	}
}

func curationGroupFacts(canonicalID string, sourceID string) []memsqlite.CurationGroupFact {
	return []memsqlite.CurationGroupFact{
		{FactID: canonicalID, Role: "existing_candidate"},
		{FactID: sourceID, Role: "new_delta"},
	}
}

func curationFactIDs(facts []core.Fact) []string {
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.ID)
	}
	return ids
}

func curationGroupFactIDs(facts []memsqlite.CurationGroupFact) []string {
	ids := make([]string, 0, len(facts))
	for _, fact := range facts {
		ids = append(ids, fact.FactID)
	}
	return ids
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func requireCurationFactSearchable(t *testing.T, db *sql.DB, factID string, wantLifecycle string, wantSearchable bool) {
	t.Helper()
	var lifecycle string
	var searchable int
	if err := db.QueryRow(`SELECT lifecycle_status, searchable FROM facts WHERE id = ?`, factID).Scan(&lifecycle, &searchable); err != nil {
		t.Fatalf("query curation fact state: %v", err)
	}
	if lifecycle != wantLifecycle || (searchable == 1) != wantSearchable {
		t.Fatalf("fact %s state = %s/%d, want %s/%t", factID, lifecycle, searchable, wantLifecycle, wantSearchable)
	}
}

func requireCurationFactSummary(t *testing.T, db *sql.DB, factID string, wantSummary string, wantObject string) {
	t.Helper()
	var summary string
	var object sql.NullString
	if err := db.QueryRow(`SELECT content_summary, object_literal FROM facts WHERE id = ?`, factID).Scan(&summary, &object); err != nil {
		t.Fatalf("query curation fact summary: %v", err)
	}
	if summary != wantSummary || !object.Valid || object.String != wantObject {
		t.Fatalf("fact %s summary/object = %q/%v, want %q/%q", factID, summary, object, wantSummary, wantObject)
	}
}

func requireCurationSearchDocument(t *testing.T, db *sql.DB, factID string, wantPresent bool) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM memory_search_documents WHERE node_type = 'fact' AND node_id = ?`, factID).Scan(&count); err != nil {
		t.Fatalf("count curation search document: %v", err)
	}
	if (count > 0) != wantPresent {
		t.Fatalf("search document for %s present = %t, want %t", factID, count > 0, wantPresent)
	}
}

func requireCurationCheckpoint(t *testing.T, db *sql.DB, personaID string, wantRunID string, wantCreatedAt time.Time, wantFactID string) {
	t.Helper()
	var runID, createdAt, factID string
	if err := db.QueryRow(`SELECT last_successful_run_id, cursor_created_at, cursor_fact_id FROM memory_curation_checkpoints WHERE persona_id = ?`, personaID).Scan(&runID, &createdAt, &factID); err != nil {
		t.Fatalf("query curation checkpoint: %v", err)
	}
	if runID != wantRunID || createdAt != formatTestTime(wantCreatedAt) || factID != wantFactID {
		t.Fatalf("checkpoint = %s/%s/%s, want %s/%s/%s", runID, createdAt, factID, wantRunID, formatTestTime(wantCreatedAt), wantFactID)
	}
}

func requireCurationQueueOperationCount(t *testing.T, db *sql.DB, nodeType string, operation string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT COUNT(*) FROM index_sync_queue WHERE node_type = ? AND operation = ?`, nodeType, operation).Scan(&got); err != nil {
		t.Fatalf("count curation queue operation: %v", err)
	}
	if got != want {
		t.Fatalf("queue %s/%s count = %d, want %d", nodeType, operation, got, want)
	}
}

func fixedCurationIDs() func() string {
	index := 0
	return func() string {
		index++
		return fmt.Sprintf("curation_id_%02d", index)
	}
}

func fixedCurationNow() time.Time {
	return time.Date(2026, 5, 31, 13, 0, 0, 0, time.UTC)
}

func curationTime(minutes int) time.Time {
	return time.Date(2026, 5, 31, 12, minutes, 0, 0, time.UTC)
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func formatTestTime(value time.Time) string {
	return formatSQLiteTestTime(value)
}
