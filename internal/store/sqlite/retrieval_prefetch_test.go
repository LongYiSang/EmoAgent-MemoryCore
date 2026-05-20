package sqlite

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func TestRetrievalPrefetchHelpers(t *testing.T) {
	if got := uniqueSortedStrings([]string{"b", "a", "b", ""}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("uniqueSortedStrings = %#v, want [a b]", got)
	}
	if got := chunkedIDs([]string{"a", "b", "c"}, 2); !reflect.DeepEqual(got, [][]string{{"a", "b"}, {"c"}}) {
		t.Fatalf("chunkedIDs = %#v, want [[a b] [c]]", got)
	}
	if got := placeholders(3); got != "?,?,?" {
		t.Fatalf("placeholders(3) = %q, want ?,?,?", got)
	}
}

func TestScoringPrefetchAuthorityEvidenceAndFatigue(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	addPrefetchEpisode(t, ctx, db, core.Episode{
		ID:               "ep_hidden",
		PersonaID:        "default",
		SessionID:        "s1",
		Content:          "hidden source",
		OccurredAt:       prefetchNow().Add(time.Minute),
		SourceType:       core.SourceTypeChat,
		VisibilityStatus: core.VisibilityHidden,
	})

	facts := []core.Fact{
		prefetchFact("fact_visible", "visible fact", ptrForPrefetch("ent_user")),
		prefetchFact("fact_hidden_entity", "hidden entity fact", ptrForPrefetch("ent_hidden")),
		prefetchFact("fact_sensitive_entity", "sensitive entity fact", ptrForPrefetch("ent_sensitive")),
		prefetchFact("fact_no_evidence", "no evidence fact", ptrForPrefetch("ent_user")),
		prefetchFact("fact_pinned_no_evidence", "pinned fact", ptrForPrefetch("ent_user")),
		prefetchFact("fact_hidden_episode", "hidden episode fact", ptrForPrefetch("ent_user")),
		prefetchFact("fact_pinned_hidden_episode", "pinned hidden episode fact", ptrForPrefetch("ent_user")),
		prefetchFact("fact_invalidated", "invalidated fact", ptrForPrefetch("ent_user")),
		prefetchFact("fact_archived", "archived fact", ptrForPrefetch("ent_user")),
		prefetchFact("fact_deep_archived", "deep archived fact", ptrForPrefetch("ent_user")),
	}
	for _, fact := range facts {
		insertPrefetchFact(t, ctx, db, fact)
	}
	mustExecPrefetch(t, db, `UPDATE facts SET pinned = 1 WHERE id IN ('fact_pinned_no_evidence', 'fact_pinned_hidden_episode')`)
	mustExecPrefetch(t, db, `UPDATE facts SET reinforcement_count = 3 WHERE id = 'fact_visible'`)
	mustExecPrefetch(t, db, `UPDATE facts SET validity_status = 'invalidated' WHERE id = 'fact_invalidated'`)
	mustExecPrefetch(t, db, `UPDATE facts SET lifecycle_status = 'archived' WHERE id = 'fact_archived'`)
	mustExecPrefetch(t, db, `UPDATE facts SET lifecycle_status = 'deep_archived' WHERE id = 'fact_deep_archived'`)

	for _, factID := range []string{
		"fact_visible",
		"fact_hidden_entity",
		"fact_sensitive_entity",
		"fact_invalidated",
		"fact_archived",
		"fact_deep_archived",
	} {
		addPrefetchEvidence(t, ctx, db, "link_"+factID+"_visible", factID, "ep_visible")
	}
	addPrefetchEvidence(t, ctx, db, "link_hidden_episode", "fact_hidden_episode", "ep_hidden")
	addPrefetchEvidence(t, ctx, db, "link_pinned_hidden_episode", "fact_pinned_hidden_episode", "ep_hidden")
	addPrefetchAccessEvent(t, db, "event_same_session", "s1", "fact_visible", "retrieved")
	addPrefetchAccessEvent(t, db, "event_other_session", "s2", "fact_visible", "retrieved")
	addPrefetchAccessEvent(t, db, "event_suppressed", "s1", "fact_visible", "suppressed")
	addPrefetchAccessEvent(t, db, "event_other_node", "s1", "fact_hidden_entity", "retrieved")

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	candidates := map[string]retrievalCandidate{}
	for _, fact := range facts {
		candidates[fact.ID] = retrievalCandidate{FactID: fact.ID, AnchorEnergy: 1}
	}
	candidates["fact_absent"] = retrievalCandidate{FactID: "fact_absent", AnchorEnergy: 1}

	policy := RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal)}
	pf, err := repo.buildScoringPrefetch(ctx, RetrievalRequest{
		PersonaID: "default",
		SessionID: ptrForPrefetch("s1"),
	}, policy, candidates)
	if err != nil {
		t.Fatalf("buildScoringPrefetch: %v", err)
	}

	if _, ok := pf.facts["fact_absent"]; ok {
		t.Fatalf("missing fact was loaded into prefetch")
	}
	requirePrefetchAuthority(t, pf, policy, "fact_visible", true)
	requirePrefetchAuthority(t, pf, policy, "fact_hidden_entity", false)
	requirePrefetchAuthority(t, pf, policy, "fact_sensitive_entity", false)
	requirePrefetchAuthority(t, pf, policy, "fact_no_evidence", false)
	requirePrefetchAuthority(t, pf, policy, "fact_pinned_no_evidence", true)
	requirePrefetchAuthority(t, pf, policy, "fact_hidden_episode", false)
	requirePrefetchAuthority(t, pf, policy, "fact_pinned_hidden_episode", false)
	requirePrefetchAuthority(t, pf, policy, "fact_invalidated", false)
	requirePrefetchAuthority(t, pf, policy, "fact_archived", false)
	requirePrefetchAuthority(t, pf, policy, "fact_deep_archived", false)

	missingEntityFact := prefetchFact("fact_missing_entity", "missing entity", ptrForPrefetch("ent_missing"))
	missingEntityFact.Pinned = true
	if authorityAllowsFromPrefetch(missingEntityFact, policy, pf) {
		t.Fatalf("missing linked entity allowed authority")
	}

	strength, sourceEpisodeIDs := evidenceStrengthFromPrefetch(pf.facts["fact_visible"], pf)
	if !reflect.DeepEqual(sourceEpisodeIDs, []string{"ep_visible"}) {
		t.Fatalf("sourceEpisodeIDs = %#v, want [ep_visible]", sourceEpisodeIDs)
	}
	if math.Abs(strength-0.785) > 0.000001 {
		t.Fatalf("evidence strength = %.6f, want 0.785", strength)
	}
	if got := pf.fatigue["fact_visible"]; got != 1 {
		t.Fatalf("fatigue count = %d, want 1 same-session retrieved event", got)
	}
	if got := pf.fatigue["fact_hidden_entity"]; got != 1 {
		t.Fatalf("fatigue count for other fact = %d, want 1", got)
	}
}

func TestRetrievalBatchPrefetchGoldenBehavior(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	addPrefetchEpisode(t, ctx, db, core.Episode{ID: "ep_batch_1", PersonaID: "default", SessionID: "s1", Content: "source 1", OccurredAt: prefetchNow().Add(-3 * time.Hour)})
	addPrefetchEpisode(t, ctx, db, core.Episode{ID: "ep_batch_2", PersonaID: "default", SessionID: "s1", Content: "source 2", OccurredAt: prefetchNow().Add(-2 * time.Hour)})
	addPrefetchEpisode(t, ctx, db, core.Episode{ID: "ep_batch_3", PersonaID: "default", SessionID: "s1", Content: "source 3", OccurredAt: prefetchNow().Add(-1 * time.Hour)})
	addPrefetchEpisode(t, ctx, db, core.Episode{
		ID:               "ep_hidden_batch",
		PersonaID:        "default",
		SessionID:        "s1",
		Content:          "hidden source",
		OccurredAt:       prefetchNow(),
		VisibilityStatus: core.VisibilityHidden,
	})

	for _, fact := range []core.Fact{
		prefetchFact("fact_visible_batch", "用户因为早会安排而焦虑。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_pinned_batch", "用户手动固定了无来源偏好。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_related_batch", "用户不喜欢早会安排。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_related_new_batch", "用户已经调整早会安排。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_fatigue_batch", "用户近期反复提到咖啡。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_hidden_batch", "隐藏事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_unsearchable_batch", "不可搜索事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_sensitive_batch", "敏感事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_hidden_entity_batch", "隐藏实体事实。", ptrForPrefetch("ent_hidden")),
		prefetchFact("fact_no_evidence_batch", "无证据事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_hidden_source_batch", "隐藏来源事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_invalidated_batch", "失效事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_archived_batch", "归档事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_deep_archived_batch", "深归档事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_mirror_denied_batch", "镜像拒绝事实。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_graph_denied_batch", "图激活拒绝事实。", ptrForPrefetch("ent_user")),
	} {
		insertPrefetchFact(t, ctx, db, fact)
	}
	mustExecPrefetch(t, db, `UPDATE facts SET pinned = 1 WHERE id = 'fact_pinned_batch'`)
	mustExecPrefetch(t, db, `UPDATE facts SET visibility_status = 'hidden', searchable = 0 WHERE id IN ('fact_hidden_batch', 'fact_mirror_denied_batch', 'fact_graph_denied_batch')`)
	mustExecPrefetch(t, db, `UPDATE facts SET searchable = 0 WHERE id = 'fact_unsearchable_batch'`)
	mustExecPrefetch(t, db, `UPDATE facts SET sensitivity_level = 'sensitive' WHERE id = 'fact_sensitive_batch'`)
	mustExecPrefetch(t, db, `UPDATE facts SET validity_status = 'invalidated' WHERE id = 'fact_invalidated_batch'`)
	mustExecPrefetch(t, db, `UPDATE facts SET lifecycle_status = 'archived' WHERE id = 'fact_archived_batch'`)
	mustExecPrefetch(t, db, `UPDATE facts SET lifecycle_status = 'deep_archived' WHERE id = 'fact_deep_archived_batch'`)

	for _, episodeID := range []string{"ep_batch_1", "ep_batch_2", "ep_batch_3"} {
		addPrefetchEvidence(t, ctx, db, "link_visible_batch_"+episodeID, "fact_visible_batch", episodeID)
	}
	for _, factID := range []string{
		"fact_related_batch",
		"fact_related_new_batch",
		"fact_fatigue_batch",
		"fact_hidden_batch",
		"fact_unsearchable_batch",
		"fact_sensitive_batch",
		"fact_hidden_entity_batch",
		"fact_invalidated_batch",
		"fact_archived_batch",
		"fact_deep_archived_batch",
		"fact_mirror_denied_batch",
		"fact_graph_denied_batch",
	} {
		addPrefetchEvidence(t, ctx, db, "link_"+factID+"_evidence", factID, "ep_visible")
	}
	addPrefetchEvidence(t, ctx, db, "link_hidden_source_batch", "fact_hidden_source_batch", "ep_hidden_batch")
	addPrefetchFactLink(t, ctx, db, "link_visible_related_batch", "fact_visible_batch", "CAUSED_BY", "fact_related_batch", 1.0)
	addPrefetchFactLink(t, ctx, db, "link_related_superseded_batch", "fact_related_new_batch", "SUPERSEDES", "fact_related_batch", 1.0)
	addPrefetchAccessEvent(t, db, "event_fatigue_batch", "s1", "fact_fatigue_batch", "retrieved")

	mirrorDiagnostics := &MirrorDiagnostics{Candidates: []MirrorCandidateDiagnostic{{
		TriviumNodeID: 101,
		SQLiteFactID:  "fact_mirror_denied_batch",
		Score:         0.99,
		Source:        AnchorSourceTriviumDense,
		Rank:          1,
	}}}
	graphDiagnostics := &GraphActivationDiagnostics{Candidates: []GraphActivationCandidateDiagnostic{{
		TriviumNodeID: 202,
		SQLiteNodeID:  "fact_graph_denied_batch",
		NodeType:      string(core.NodeTypeFact),
		Score:         0.98,
		Source:        "activation",
		Rank:          1,
	}}}
	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	prepared := PreparedRetrieval{
		Request: RetrievalRequest{
			PersonaID: "default",
			SessionID: ptrForPrefetch("s1"),
			Mirror: []RetrievalMirrorCandidate{{
				FactID:        "fact_mirror_denied_batch",
				TriviumNodeID: 101,
				Score:         0.99,
				Source:        AnchorSourceTriviumDense,
				Rank:          1,
			}},
			MirrorDiagnostics: mirrorDiagnostics,
		},
		Query: QueryAnalysis{
			Raw:           "为什么早会让我焦虑",
			Normalized:    "为什么早会让我焦虑",
			Terms:         []string{"为什么早会让我焦虑"},
			MemoryAbility: MemoryAbilityCausalExplain,
			Signals:       []QuerySignal{QuerySignalCausal},
		},
		Policy: RetrievalPolicy{
			SensitivityPermission: string(core.SensitivityNormal),
			FinalMemoryCount:      2,
			ContextBudgetTokens:   10000,
		},
		Now: prefetchNow(),
		FusedAnchors: []FusedAnchor{
			prefetchAnchor("fact_visible_batch", 1.0),
			prefetchAnchor("fact_pinned_batch", 0.75),
			prefetchAnchor("fact_fatigue_batch", 0.95),
			prefetchAnchor("fact_hidden_batch", 0.9),
			prefetchAnchor("fact_unsearchable_batch", 0.9),
			prefetchAnchor("fact_sensitive_batch", 0.9),
			prefetchAnchor("fact_hidden_entity_batch", 0.9),
			prefetchAnchor("fact_no_evidence_batch", 0.9),
			prefetchAnchor("fact_hidden_source_batch", 0.9),
			prefetchAnchor("fact_invalidated_batch", 0.9),
			prefetchAnchor("fact_archived_batch", 0.9),
			prefetchAnchor("fact_deep_archived_batch", 0.9),
			prefetchAnchor("fact_mirror_denied_batch", 0.9),
		},
	}

	finalCandidates, safeCandidates, err := repo.BuildRerankCandidates(ctx, prepared, []RetrievalActivationCandidate{{
		FactID:        "fact_graph_denied_batch",
		TriviumNodeID: 202,
		Score:         0.98,
		Source:        "activation",
		Rank:          1,
	}}, graphDiagnostics)
	if err != nil {
		t.Fatalf("BuildRerankCandidates: %v", err)
	}
	if got := prefetchRerankIDs(safeCandidates); !reflect.DeepEqual(got, []string{"fact_visible_batch", "fact_pinned_batch", "fact_related_batch"}) {
		t.Fatalf("safe rerank candidates = %#v, want visible, pinned, then completed related", got)
	}
	if mirrorDiagnostics.DroppedCandidateCount != 1 || mirrorDiagnostics.Candidates[0].DropReason != "dropped_by_authority_filter" {
		t.Fatalf("mirror diagnostics = %#v, want one authority drop", mirrorDiagnostics)
	}
	if graphDiagnostics.DroppedCandidateCount != 1 || graphDiagnostics.Candidates[0].DropReason != "dropped_by_authority_filter" {
		t.Fatalf("graph diagnostics = %#v, want one authority drop", graphDiagnostics)
	}

	result, err := repo.CompleteFinal(ctx, finalCandidates, nil, nil)
	if err != nil {
		t.Fatalf("CompleteFinal: %v", err)
	}
	if len(result.Blocks) != 1 || result.Blocks[0].BlockType != MemoryBlockTypeRelevantCausalMemory {
		t.Fatalf("blocks = %#v, want one causal block", result.Blocks)
	}
	if got := prefetchContextItemIDs(result.Blocks[0].Items); !reflect.DeepEqual(got, []string{"fact_visible_batch", "fact_pinned_batch"}) {
		t.Fatalf("selected item order = %#v, want visible then pinned", got)
	}
	visibleItem := result.Blocks[0].Items[0]
	if visibleItem.HistoricalStatus != MemoryHistoricalStatusCurrent {
		t.Fatalf("visible historical status = %q, want current", visibleItem.HistoricalStatus)
	}
	if got := prefetchSourceIDs(visibleItem.SourceRefs); !reflect.DeepEqual(got, []string{"ep_batch_1", "ep_batch_2"}) {
		t.Fatalf("source refs = %#v, want first two chronological refs", visibleItem.SourceRefs)
	}
	for _, ref := range visibleItem.SourceRefs {
		if ref.EvidenceCount != 3 || ref.QuoteAllowed {
			t.Fatalf("source ref = %#v, want EvidenceCount=3 QuoteAllowed=false", ref)
		}
	}
	if len(visibleItem.RelatedFacts) != 1 {
		t.Fatalf("related facts = %#v, want one related fact", visibleItem.RelatedFacts)
	}
	related := visibleItem.RelatedFacts[0]
	if related.NodeID != "fact_related_batch" || related.LinkType != "CAUSED_BY" || related.Direction != relatedFactDirectionOutbound || related.HistoricalStatus != MemoryHistoricalStatusSuperseded {
		t.Fatalf("related fact = %#v, want outbound CAUSED_BY superseded related fact", related)
	}
	requirePrefetchSuppression(t, result.DoNotMention, "fact_fatigue_batch", MemorySuppressionReasonFatigue)
	requirePrefetchAccessEvent(t, db, "fact_visible_batch", "retrieved", 1)
	requirePrefetchAccessEvent(t, db, "fact_pinned_batch", "retrieved", 2)
	requirePrefetchAccessEvent(t, db, "fact_fatigue_batch", "suppressed", -1)

	breakdown := requirePrefetchScoreBreakdown(t, db, "fact_visible_batch", "retrieved")
	requirePrefetchBreakdownNumber(t, breakdown, "evidence_strength", 0.81)
	requirePrefetchBreakdownNumber(t, breakdown, "fatigue_penalty", 0)
	requirePrefetchBreakdownNumber(t, breakdown, "lifecycle_multiplier", 1)
	if finalScore, ok := breakdown["final_score"].(float64); !ok || finalScore <= 0 {
		t.Fatalf("final_score = %#v, want positive number", breakdown["final_score"])
	}
}

func TestRetrievalCompletionPrefetchFiltersUnauthorizedLinkedFacts(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	for _, fact := range []core.Fact{
		prefetchFact("fact_completion_source", "用户因为早会安排而焦虑。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_completion_visible", "早会安排触发了用户焦虑。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_completion_hidden", "隐藏的触发原因。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_completion_unsearchable", "不可搜索的触发原因。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_completion_sensitive", "敏感的触发原因。", ptrForPrefetch("ent_user")),
	} {
		insertPrefetchFact(t, ctx, db, fact)
		addPrefetchEvidence(t, ctx, db, "link_"+fact.ID+"_evidence", fact.ID, "ep_visible")
	}
	mustExecPrefetch(t, db, `UPDATE facts SET visibility_status = 'hidden' WHERE id = 'fact_completion_hidden'`)
	mustExecPrefetch(t, db, `UPDATE facts SET searchable = 0 WHERE id = 'fact_completion_unsearchable'`)
	mustExecPrefetch(t, db, `UPDATE facts SET sensitivity_level = 'sensitive' WHERE id = 'fact_completion_sensitive'`)
	addPrefetchFactLink(t, ctx, db, "link_completion_visible", "fact_completion_source", "CAUSED_BY", "fact_completion_visible", 1.0)
	addPrefetchFactLink(t, ctx, db, "link_completion_hidden", "fact_completion_source", "CAUSED_BY", "fact_completion_hidden", 1.0)
	addPrefetchFactLink(t, ctx, db, "link_completion_unsearchable", "fact_completion_source", "CAUSED_BY", "fact_completion_unsearchable", 1.0)
	addPrefetchFactLink(t, ctx, db, "link_completion_sensitive", "fact_completion_source", "CAUSED_BY", "fact_completion_sensitive", 1.0)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	sourceFact, err := repo.getFact(ctx, "default", "fact_completion_source")
	if err != nil {
		t.Fatalf("get source fact: %v", err)
	}
	completed, err := repo.completeLinkedCandidates(ctx, RetrievalRequest{
		PersonaID: "default",
	}, QueryAnalysis{
		Raw:           "为什么早会焦虑",
		Normalized:    "为什么早会焦虑",
		Terms:         []string{"早会", "焦虑"},
		MemoryAbility: MemoryAbilityCausalExplain,
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
	}, []scoredFact{{
		Fact: sourceFact,
	}})
	if err != nil {
		t.Fatalf("completeLinkedCandidates: %v", err)
	}
	if _, ok := completed["fact_completion_visible"]; !ok {
		t.Fatalf("visible linked fact missing from completion candidates: %#v", completed)
	}
	for _, factID := range []string{"fact_completion_hidden", "fact_completion_unsearchable", "fact_completion_sensitive"} {
		if _, ok := completed[factID]; ok {
			t.Fatalf("unauthorized fact %s entered completion candidates: %#v", factID, completed)
		}
	}
}

func TestNarrativeInsightCompletionDiscoveryIsBounded(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	mustExecPrefetch(t, db, `
INSERT INTO narratives (
    id, persona_id, scope, scope_ref, summary, importance,
    visibility_status, sensitivity_level, lifecycle_status, searchable
) VALUES ('narrative_bounded_completion', 'default', 'topic', 'relationship', '关系变化：陪伴感增强，用户提到不孤独。', 0.8, 'visible', 'normal', 'active', 1)`)
	if err := NewSearchRepository(db.SQLDB()).UpsertNarrativeDocument(ctx, "default", "narrative_bounded_completion"); err != nil {
		t.Fatalf("upsert bounded narrative search document: %v", err)
	}

	for i := 0; i < maxCompletionCandidates*4; i++ {
		suffix := formatInt(i)
		if i < 10 {
			suffix = "0" + suffix
		}
		factID := "fact_bounded_completion_" + suffix
		fact := prefetchFact(factID, "普通关系补全候选。", ptrForPrefetch("ent_user"))
		insertPrefetchFact(t, ctx, db, fact)
		addPrefetchEvidence(t, ctx, db, "link_"+factID+"_evidence", factID, "ep_visible")
		if err := NewLinkRepository(db.SQLDB()).Insert(ctx, core.MemoryLink{
			ID:           "link_bounded_completion_" + suffix,
			PersonaID:    "default",
			FromNodeType: core.NodeTypeNarrative,
			FromNodeID:   "narrative_bounded_completion",
			LinkType:     core.LinkType("DERIVED_FROM"),
			ToNodeType:   core.NodeTypeFact,
			ToNodeID:     factID,
			Weight:       0.1,
		}); err != nil {
			t.Fatalf("insert bounded completion link %s: %v", factID, err)
		}
	}
	priorityFact := prefetchFact("fact_zz_bounded_priority_outcome", "用户和 Agent 聊完以后感觉没那么孤独，有陪伴感。", ptrForPrefetch("ent_user"))
	priorityFact.FactType = core.FactTypeRelationalState
	insertPrefetchFact(t, ctx, db, priorityFact)
	addPrefetchEvidence(t, ctx, db, "link_fact_zz_bounded_priority_outcome_evidence", "fact_zz_bounded_priority_outcome", "ep_visible")
	if err := NewLinkRepository(db.SQLDB()).Insert(ctx, core.MemoryLink{
		ID:           "link_bounded_priority_outcome",
		PersonaID:    "default",
		FromNodeType: core.NodeTypeNarrative,
		FromNodeID:   "narrative_bounded_completion",
		LinkType:     core.LinkType("DERIVED_FROM"),
		ToNodeType:   core.NodeTypeFact,
		ToNodeID:     "fact_zz_bounded_priority_outcome",
		Weight:       1,
	}); err != nil {
		t.Fatalf("insert bounded priority outcome link: %v", err)
	}

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	acc := map[string]retrievalCompletionCandidate{}
	err := repo.addNarrativeInsightCompletions(ctx, "default", QueryAnalysis{
		Raw:           "我们的关系最近有什么变化，有没有陪伴感",
		Normalized:    "我们的关系最近有什么变化，有没有陪伴感",
		Terms:         []string{"关系", "变化", "陪伴"},
		MemoryAbility: MemoryAbilityRelationshipArc,
		EvidenceNeed:  EvidenceNeedRelationshipTimeline,
	}, RetrievalPolicy{SensitivityPermission: string(core.SensitivityNormal)}, acc)
	if err != nil {
		t.Fatalf("addNarrativeInsightCompletions: %v", err)
	}
	if len(acc) > maxCompletionCandidates*2 {
		t.Fatalf("completion discovery accumulated %d candidates, want bounded to <= %d", len(acc), maxCompletionCandidates*2)
	}
	if _, ok := acc["fact_zz_bounded_priority_outcome"]; !ok {
		t.Fatalf("high-priority outcome was not retained in bounded completion candidates")
	}
}

func TestBatchPrefetchFatigueQueryPlanUsesSessionNodeAccessIndex(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)
	addPrefetchAccessEvent(t, db, "event_plan", "s1", "fact_plan", "retrieved")

	rows, err := db.SQLDB().QueryContext(ctx, `
EXPLAIN QUERY PLAN
SELECT node_id, COUNT(*)
FROM memory_access_events
WHERE session_id = ?
  AND node_type = 'fact'
  AND access_type = 'retrieved'
  AND node_id IN (?)
GROUP BY node_id
ORDER BY node_id ASC`, "s1", "fact_plan")
	if err != nil {
		t.Fatalf("explain fatigue query: %v", err)
	}
	defer rows.Close()

	var details []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	plan := strings.Join(details, "\n")
	if !strings.Contains(plan, "idx_memory_access_events_session_node_access") {
		t.Fatalf("fatigue query plan = %q, want idx_memory_access_events_session_node_access", plan)
	}
}

func requirePrefetchAuthority(t *testing.T, pf scoringPrefetch, policy RetrievalPolicy, factID string, want bool) {
	t.Helper()

	fact, ok := pf.facts[factID]
	if !ok {
		t.Fatalf("fact %s missing from prefetch", factID)
	}
	if got := authorityAllowsFromPrefetch(fact, policy, pf); got != want {
		t.Fatalf("authorityAllowsFromPrefetch(%s) = %v, want %v", factID, got, want)
	}
}

func openPrefetchTestDB(t *testing.T, ctx context.Context) *DB {
	t.Helper()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		db.Close()
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

func seedPrefetchGraph(t *testing.T, ctx context.Context, db *DB) {
	t.Helper()

	store := NewStore(db.SQLDB())
	if err := store.EnsurePersona(ctx, core.Persona{ID: "default", DisplayName: "Default"}); err != nil {
		t.Fatalf("ensure persona: %v", err)
	}
	if err := store.EnsureSession(ctx, core.Session{ID: "s1", PersonaID: "default", Channel: core.ChannelAPI}); err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	if err := store.EnsureSession(ctx, core.Session{ID: "s2", PersonaID: "default", Channel: core.ChannelAPI}); err != nil {
		t.Fatalf("ensure session s2: %v", err)
	}
	entities := NewEntityRepository(db.SQLDB())
	for _, entity := range []core.Entity{
		{ID: "ent_user", PersonaID: "default", CanonicalName: "Long", EntityType: core.EntityTypeUser},
		{ID: "ent_hidden", PersonaID: "default", CanonicalName: "Hidden", EntityType: core.EntityTypeConcept, VisibilityStatus: core.VisibilityHidden},
		{ID: "ent_sensitive", PersonaID: "default", CanonicalName: "Sensitive", EntityType: core.EntityTypeConcept, SensitivityLevel: core.SensitivitySensitive},
	} {
		if err := entities.Upsert(ctx, entity); err != nil {
			t.Fatalf("upsert entity %s: %v", entity.ID, err)
		}
	}
	addPrefetchEpisode(t, ctx, db, core.Episode{
		ID:         "ep_visible",
		PersonaID:  "default",
		SessionID:  "s1",
		Content:    "visible source",
		OccurredAt: prefetchNow(),
		SourceType: core.SourceTypeChat,
	})
}

func prefetchFact(id string, summary string, subjectEntityID *string) core.Fact {
	object := summary
	return core.Fact{
		ID:                        id,
		PersonaID:                 "default",
		SubjectEntityID:           subjectEntityID,
		Predicate:                 "likes",
		ObjectLiteral:             &object,
		ContentSummary:            summary,
		FactType:                  core.FactTypeStablePreference,
		ExtractionConfidence:      core.ExtractionConfidenceExplicit,
		ExtractionConfidenceScore: 0.8,
		Importance:                0.7,
		LifecycleStatus:           core.LifecycleActive,
	}
}

func insertPrefetchFact(t *testing.T, ctx context.Context, db *DB, fact core.Fact) {
	t.Helper()

	if err := NewFactRepository(db.SQLDB()).Insert(ctx, fact); err != nil {
		t.Fatalf("insert fact %s: %v", fact.ID, err)
	}
}

func addPrefetchEpisode(t *testing.T, ctx context.Context, db *DB, episode core.Episode) {
	t.Helper()

	if episode.OccurredAt.IsZero() {
		episode.OccurredAt = prefetchNow()
	}
	if err := NewEpisodeRepository(db.SQLDB()).Append(ctx, episode); err != nil {
		t.Fatalf("append episode %s: %v", episode.ID, err)
	}
}

func addPrefetchEvidence(t *testing.T, ctx context.Context, db *DB, linkID string, factID string, episodeID string) {
	t.Helper()

	if err := NewLinkRepository(db.SQLDB()).Insert(ctx, core.MemoryLink{
		ID:           linkID,
		PersonaID:    "default",
		FromNodeType: core.NodeTypeFact,
		FromNodeID:   factID,
		LinkType:     core.LinkTypeEvidencedBy,
		ToNodeType:   core.NodeTypeEpisode,
		ToNodeID:     episodeID,
	}); err != nil {
		t.Fatalf("insert evidence link %s: %v", linkID, err)
	}
}

func addPrefetchAccessEvent(t *testing.T, db *DB, id string, sessionID string, factID string, accessType string) {
	t.Helper()

	mustExecPrefetch(t, db, `
INSERT INTO memory_access_events (id, persona_id, session_id, node_type, node_id, access_type)
VALUES (?, 'default', ?, 'fact', ?, ?)`, id, sessionID, factID, accessType)
}

func mustExecPrefetch(t *testing.T, db *DB, query string, args ...any) {
	t.Helper()

	if _, err := db.SQLDB().Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func ptrForPrefetch(value string) *string {
	return &value
}

func prefetchNow() time.Time {
	return time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
}

func prefetchAnchor(factID string, energy float64) FusedAnchor {
	return FusedAnchor{
		NodeID:           factID,
		NodeType:         core.NodeTypeFact,
		FusedAnchorScore: energy,
		SeedEnergy:       energy,
	}
}

func prefetchRerankIDs(candidates []RerankCandidate) []string {
	ids := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.NodeID)
	}
	return ids
}

func prefetchContextItemIDs(items []MemoryContextItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.NodeID)
	}
	return ids
}

func prefetchSourceIDs(refs []MemorySourceRef) []string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.EpisodeID)
	}
	return ids
}

func requirePrefetchSuppression(t *testing.T, suppressions []MemorySuppression, factID string, reason string) {
	t.Helper()

	for _, suppression := range suppressions {
		if suppression.NodeType == string(core.NodeTypeFact) && suppression.NodeID == factID && suppression.Reason == reason {
			return
		}
	}
	t.Fatalf("suppression %s/%s not found in %#v", factID, reason, suppressions)
}

func requirePrefetchAccessEvent(t *testing.T, db *DB, factID string, accessType string, rank int) {
	t.Helper()

	query := `
SELECT COUNT(*)
FROM memory_access_events
WHERE node_type = 'fact' AND node_id = ? AND access_type = ?`
	args := []any{factID, accessType}
	if rank >= 0 {
		query += ` AND rank_position = ?`
		args = append(args, rank)
	} else {
		query += ` AND rank_position IS NULL`
	}
	var count int
	if err := db.SQLDB().QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count access event %s/%s: %v", factID, accessType, err)
	}
	if count != 1 {
		t.Fatalf("access event count for %s/%s rank %d = %d, want 1", factID, accessType, rank, count)
	}
}

func requirePrefetchScoreBreakdown(t *testing.T, db *DB, factID string, accessType string) map[string]any {
	t.Helper()

	var raw string
	if err := db.SQLDB().QueryRow(`
SELECT score_breakdown_json
FROM memory_access_events
WHERE node_type = 'fact' AND node_id = ? AND access_type = ?
ORDER BY created_at DESC, id DESC
LIMIT 1`, factID, accessType).Scan(&raw); err != nil {
		t.Fatalf("query score breakdown %s/%s: %v", factID, accessType, err)
	}
	var breakdown map[string]any
	if err := json.Unmarshal([]byte(raw), &breakdown); err != nil {
		t.Fatalf("decode score breakdown %q: %v", raw, err)
	}
	return breakdown
}

func requirePrefetchBreakdownNumber(t *testing.T, breakdown map[string]any, key string, want float64) {
	t.Helper()

	got, ok := breakdown[key].(float64)
	if !ok {
		t.Fatalf("breakdown[%s] = %#v, want number", key, breakdown[key])
	}
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("breakdown[%s] = %.6f, want %.6f", key, got, want)
	}
}
