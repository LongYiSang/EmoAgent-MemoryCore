package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func TestReconstructionPrefetchPreservesSourceRefsRelatedFactsAndHistory(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	addPrefetchEpisode(t, ctx, db, core.Episode{ID: "ep_src_1", PersonaID: "default", SessionID: "s1", Content: "source 1", OccurredAt: prefetchNow().Add(-2 * time.Hour)})
	addPrefetchEpisode(t, ctx, db, core.Episode{ID: "ep_src_2", PersonaID: "default", SessionID: "s1", Content: "source 2", OccurredAt: prefetchNow().Add(-1 * time.Hour)})
	addPrefetchEpisode(t, ctx, db, core.Episode{ID: "ep_src_3", PersonaID: "default", SessionID: "s1", Content: "source 3", OccurredAt: prefetchNow()})
	addPrefetchEpisode(t, ctx, db, core.Episode{
		ID:               "ep_sensitive",
		PersonaID:        "default",
		SessionID:        "s1",
		Content:          "sensitive source",
		OccurredAt:       prefetchNow().Add(-3 * time.Hour),
		SensitivityLevel: core.SensitivitySensitive,
	})

	for _, fact := range []core.Fact{
		prefetchFact("fact_effect", "用户因为早会安排而焦虑。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_a", "用户不喜欢早会安排。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_b", "用户上班前会查看日程。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_hidden", "用户有隐藏原因。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_a_new", "用户已经调整早会安排。", ptrForPrefetch("ent_user")),
	} {
		insertPrefetchFact(t, ctx, db, fact)
	}
	mustExecPrefetch(t, db, `UPDATE facts SET visibility_status = 'hidden', searchable = 0 WHERE id = 'fact_hidden'`)

	for _, episodeID := range []string{"ep_src_1", "ep_src_2", "ep_src_3", "ep_sensitive"} {
		addPrefetchEvidence(t, ctx, db, "link_effect_"+episodeID, "fact_effect", episodeID)
	}
	for _, factID := range []string{"fact_a", "fact_b", "fact_hidden", "fact_a_new"} {
		addPrefetchEvidence(t, ctx, db, "link_"+factID+"_evidence", factID, "ep_visible")
	}
	addPrefetchFactLink(t, ctx, db, "link_effect_hidden", "fact_effect", "CAUSED_BY", "fact_hidden", 1.0)
	addPrefetchFactLink(t, ctx, db, "link_effect_b", "fact_effect", "CAUSED_BY", "fact_b", 0.9)
	addPrefetchFactLink(t, ctx, db, "link_effect_a", "fact_effect", "CAUSED_BY", "fact_a", 0.8)
	addPrefetchFactLink(t, ctx, db, "link_a_superseded", "fact_a_new", "SUPERSEDES", "fact_a", 0.7)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	effect := mustPrefetchFact(t, ctx, repo, "fact_effect")
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{MemoryAbility: MemoryAbilityCausalExplain}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: effect, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if blockTypeByFactID["fact_effect"] != MemoryBlockTypeRelevantCausalMemory {
		t.Fatalf("blockTypeByFactID = %#v, want causal context for fact_effect", blockTypeByFactID)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeRelevantCausalMemory || len(blocks[0].Items) != 1 {
		t.Fatalf("blocks = %#v, want one causal block with one item", blocks)
	}
	item := blocks[0].Items[0]
	if len(item.SourceRefs) != 2 {
		t.Fatalf("source refs = %#v, want two capped refs", item.SourceRefs)
	}
	if item.SourceRefs[0].EpisodeID != "ep_src_1" || item.SourceRefs[1].EpisodeID != "ep_src_2" {
		t.Fatalf("source refs order = %#v, want ep_src_1 then ep_src_2", item.SourceRefs)
	}
	for _, ref := range item.SourceRefs {
		if ref.EvidenceCount != 3 {
			t.Fatalf("source ref %#v EvidenceCount = %d, want full eligible count 3", ref, ref.EvidenceCount)
		}
		if ref.QuoteAllowed {
			t.Fatalf("source ref %#v QuoteAllowed = true, want false", ref)
		}
		if ref.EpisodeID == "ep_sensitive" {
			t.Fatalf("sensitive source ref leaked: %#v", item.SourceRefs)
		}
	}
	if len(item.RelatedFacts) != 2 {
		t.Fatalf("related facts = %#v, want two capped refs", item.RelatedFacts)
	}
	if item.RelatedFacts[0].NodeID != "fact_a" || item.RelatedFacts[0].HistoricalStatus != MemoryHistoricalStatusSuperseded {
		t.Fatalf("first related fact = %#v, want fact_a superseded after final sort", item.RelatedFacts[0])
	}
	if item.RelatedFacts[1].NodeID != "fact_b" || item.RelatedFacts[1].HistoricalStatus != MemoryHistoricalStatusCurrent {
		t.Fatalf("second related fact = %#v, want fact_b current", item.RelatedFacts[1])
	}
	for _, related := range item.RelatedFacts {
		if related.NodeID == "fact_hidden" {
			t.Fatalf("hidden related fact leaked: %#v", item.RelatedFacts)
		}
	}
}

func TestContextBlockTypeUsesSemanticBlockNames(t *testing.T) {
	fact := core.Fact{FactType: core.FactTypeStablePreference}
	tests := []struct {
		name  string
		query QueryAnalysis
		want  string
	}{
		{name: "provenance", query: QueryAnalysis{MemoryAbility: MemoryAbilityProvenance}, want: MemoryBlockTypeProvenanceMemory},
		{name: "causal", query: QueryAnalysis{MemoryAbility: MemoryAbilityCausalExplain}, want: MemoryBlockTypeRelevantCausalMemory},
		{name: "historical", query: QueryAnalysis{MemoryAbility: MemoryAbilityHistorical}, want: MemoryBlockTypeHistoricalTransitionMemory},
		{name: "premise", query: QueryAnalysis{MemoryAbility: MemoryAbilityPremiseCheck}, want: MemoryBlockTypePremiseCheckMemory},
		{name: "relationship arc", query: QueryAnalysis{MemoryAbility: MemoryAbilityRelationshipArc}, want: MemoryBlockTypeRelationshipArcMemory},
		{name: "supportive", query: QueryAnalysis{MemoryAbility: MemoryAbilitySupportive}, want: MemoryBlockTypeSupportiveMemory},
		{name: "forget delete signal", query: QueryAnalysis{Signals: []QuerySignal{QuerySignalForgetDelete}}, want: MemoryBlockTypeSupportiveMemory},
		{name: "relationship arc hint", query: QueryAnalysis{ContextBlockHints: []string{MemoryBlockTypeRelationshipArcMemory}}, want: MemoryBlockTypeRelationshipArcMemory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contextBlockType(tt.query, fact); got != tt.want {
				t.Fatalf("contextBlockType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReconstructionPrefetchSelectedSupersedesRequiresAuthorizedSuperseder(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	for _, fact := range []core.Fact{
		prefetchFact("fact_old_hidden_superseder", "用户以前使用旧手机。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_hidden_superseder", "用户现在使用隐藏手机。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_old_visible_superseder", "用户以前住在北京。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_visible_superseder", "用户现在住在上海。", ptrForPrefetch("ent_user")),
	} {
		insertPrefetchFact(t, ctx, db, fact)
		addPrefetchEvidence(t, ctx, db, "link_"+fact.ID+"_evidence", fact.ID, "ep_visible")
	}
	validTo := prefetchNow().Add(-24 * time.Hour).Format(time.RFC3339)
	mustExecPrefetch(t, db, `UPDATE facts SET validity_status = 'invalidated', valid_to = ? WHERE id IN ('fact_old_hidden_superseder', 'fact_old_visible_superseder')`, validTo)
	mustExecPrefetch(t, db, `UPDATE facts SET visibility_status = 'hidden', searchable = 0 WHERE id = 'fact_hidden_superseder'`)
	addPrefetchFactLink(t, ctx, db, "link_hidden_supersedes_old", "fact_hidden_superseder", "SUPERSEDES", "fact_old_hidden_superseder", 1.0)
	addPrefetchFactLink(t, ctx, db, "link_visible_supersedes_old", "fact_visible_superseder", "SUPERSEDES", "fact_old_visible_superseder", 1.0)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	oldHidden := mustPrefetchFact(t, ctx, repo, "fact_old_hidden_superseder")
	oldVisible := mustPrefetchFact(t, ctx, repo, "fact_old_visible_superseder")
	blocks, _, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{MemoryAbility: MemoryAbilityHistorical}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{
		{Fact: oldHidden, Score: 1, TokenCost: 1},
		{Fact: oldVisible, Score: 1, TokenCost: 1},
	})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if len(blocks) != 1 || len(blocks[0].Items) != 2 {
		t.Fatalf("blocks = %#v, want two historical items", blocks)
	}
	got := map[string]string{}
	for _, item := range blocks[0].Items {
		got[item.NodeID] = item.HistoricalStatus
	}
	if got["fact_old_hidden_superseder"] != MemoryHistoricalStatusHistorical {
		t.Fatalf("hidden superseder status = %q, want historical", got["fact_old_hidden_superseder"])
	}
	if got["fact_old_visible_superseder"] != MemoryHistoricalStatusSuperseded {
		t.Fatalf("visible superseder status = %q, want superseded", got["fact_old_visible_superseder"])
	}
}

func addPrefetchFactLink(t *testing.T, ctx context.Context, db *DB, linkID string, fromFactID string, linkType string, toFactID string, weight float64) {
	t.Helper()

	if err := NewLinkRepository(db.SQLDB()).Insert(ctx, core.MemoryLink{
		ID:           linkID,
		PersonaID:    "default",
		FromNodeType: core.NodeTypeFact,
		FromNodeID:   fromFactID,
		LinkType:     core.LinkType(linkType),
		ToNodeType:   core.NodeTypeFact,
		ToNodeID:     toFactID,
		Weight:       weight,
	}); err != nil {
		t.Fatalf("insert fact link %s: %v", linkID, err)
	}
}

func mustPrefetchFact(t *testing.T, ctx context.Context, repo *RetrievalRepository, factID string) core.Fact {
	t.Helper()

	fact, err := repo.getFact(ctx, "default", factID)
	if err != nil {
		t.Fatalf("get fact %s: %v", factID, err)
	}
	return fact
}
