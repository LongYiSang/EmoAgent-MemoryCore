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
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{Signals: []QuerySignal{QuerySignalCausal}}, RetrievalPolicy{
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
		fact  core.Fact
		want  string
	}{
		{name: "provenance", query: QueryAnalysis{EvidenceNeed: EvidenceNeedProvenanceSource, Signals: []QuerySignal{QuerySignalProvenanceSource}}, want: MemoryBlockTypeProvenanceMemory},
		{name: "causal", query: QueryAnalysis{Signals: []QuerySignal{QuerySignalCausalChain}}, want: MemoryBlockTypeRelevantCausalMemory},
		{name: "historical", query: QueryAnalysis{EvidenceNeed: EvidenceNeedStateTransition}, want: MemoryBlockTypeHistoricalTransitionMemory, fact: core.Fact{FactType: core.FactTypeStablePreference, ValidTo: ptrForPrefetchTime(prefetchNow())}},
		{name: "premise", query: QueryAnalysis{EvidenceNeed: EvidenceNeedPremiseCounterexample, Signals: []QuerySignal{QuerySignalPremiseCounterexample}}, want: MemoryBlockTypePremiseCheckMemory},
		{name: "relationship arc", query: QueryAnalysis{EvidenceNeed: EvidenceNeedRelationshipTimeline, Signals: []QuerySignal{QuerySignalRelationshipArc}}, want: MemoryBlockTypeRelationshipArcMemory},
		{name: "supportive", query: QueryAnalysis{MemoryAbility: MemoryAbilitySupportive}, want: MemoryBlockTypeSupportiveMemory},
		{name: "forget delete signal", query: QueryAnalysis{Signals: []QuerySignal{QuerySignalForgetDelete}}, want: MemoryBlockTypeSupportiveMemory},
		{name: "relationship arc hint without gate", query: QueryAnalysis{ContextBlockHints: []string{MemoryBlockTypeRelationshipArcMemory}}, want: MemoryBlockTypeFacts, fact: core.Fact{FactType: core.FactTypeRelationalState}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFact := fact
			if tt.fact.FactType != "" || tt.fact.ValidTo != nil {
				testFact = tt.fact
			}
			if got := contextBlockType(tt.query, testFact); got != tt.want {
				t.Fatalf("contextBlockType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSupportiveMemoryHintRequiresSupportiveSignal(t *testing.T) {
	fact := core.Fact{ID: "fact_supportive_hint", FactType: core.FactTypeStablePreference}
	pf := reconstructionPrefetch{}
	policy := RetrievalPolicy{}

	nonSupportive := QueryAnalysis{
		MemoryAbility:     MemoryAbilityPlanning,
		ContextBlockHints: []string{MemoryBlockTypeSupportiveMemory},
	}
	if got := secondaryContextBlockHint(nonSupportive, fact, policy, pf); got != "" {
		t.Fatalf("secondaryContextBlockHint(non-supportive) = %q, want no block", got)
	}
	if got := primaryContextBlockForSelectedFact(nonSupportive, fact, policy, pf); got != MemoryBlockTypeFacts {
		t.Fatalf("primaryContextBlockForSelectedFact(non-supportive) = %q, want %q", got, MemoryBlockTypeFacts)
	}

	supportive := QueryAnalysis{
		MemoryAbility:     MemoryAbilitySupportive,
		ContextBlockHints: []string{MemoryBlockTypeSupportiveMemory},
	}
	if got := secondaryContextBlockHint(supportive, fact, policy, pf); got != MemoryBlockTypeSupportiveMemory {
		t.Fatalf("secondaryContextBlockHint(supportive) = %q, want %q", got, MemoryBlockTypeSupportiveMemory)
	}
	if got := primaryContextBlockForSelectedFact(supportive, fact, policy, pf); got != MemoryBlockTypeSupportiveMemory {
		t.Fatalf("primaryContextBlockForSelectedFact(supportive) = %q, want %q", got, MemoryBlockTypeSupportiveMemory)
	}
}

func TestReconstructionPastEventDirectFactDoesNotRouteToHistoricalBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_past_trip", "用户去年去过杭州。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)
	addPrefetchEvidence(t, ctx, db, "link_past_trip_evidence", fact.ID, "ep_visible")

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		TimeMode:      QueryTimeModeHistorical,
		MemoryAbility: MemoryAbilityDirectFact,
		EvidenceNeed:  EvidenceNeedExactObservation,
		Signals:       []QuerySignal{QuerySignalPastEventDirectFact},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got == MemoryBlockTypeHistoricalTransitionMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want facts or experience_context", fact.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want direct past event in facts block", blocks)
	}
}

func TestReconstructionBareHistoricalLookupDoesNotRouteToHistoricalBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_bare_historical_city", "用户以前住在北京。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)
	addPrefetchEvidence(t, ctx, db, "link_bare_historical_city_evidence", fact.ID, "ep_visible")

	queryText := "以前住在哪里"
	query := QueryAnalysis{
		Raw:           queryText,
		Normalized:    queryText,
		TimeMode:      queryTimeMode(queryText),
		MemoryAbility: queryMemoryAbility(queryText),
		EvidenceNeed:  queryEvidenceNeed(queryText),
		Signals:       querySignals(queryText, queryTimeMode(queryText)),
	}
	if query.EvidenceNeed == EvidenceNeedStateTransition || hasQuerySignal(query, QuerySignalStateTransition) {
		t.Fatalf("test setup query = %#v, want bare historical lookup without state_transition", query)
	}

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, query, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got == MemoryBlockTypeHistoricalTransitionMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want non-historical without state_transition or SUPERSEDES", fact.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want facts for bare historical direct lookup", blocks)
	}
}

func TestReconstructionConflictingPastEventAndStateTransitionSignalsStaysDirect(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_conflicting_past_transition", "用户去年去过杭州。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		TimeMode:      QueryTimeModeHistorical,
		MemoryAbility: MemoryAbilityDirectFact,
		EvidenceNeed:  EvidenceNeedStateTransition,
		Signals:       []QuerySignal{QuerySignalPastEventDirectFact, QuerySignalStateTransition},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got == MemoryBlockTypeHistoricalTransitionMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want direct block without authorized SUPERSEDES", fact.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want facts for conflicting direct/state-transition signals", blocks)
	}
}

func TestReconstructionStateTransitionRequiresSupersedesEvidence(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	for _, fact := range []core.Fact{
		prefetchFact("fact_old_city_transition", "用户以前住在北京。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_new_city_transition", "用户现在住在上海。", ptrForPrefetch("ent_user")),
	} {
		insertPrefetchFact(t, ctx, db, fact)
		addPrefetchEvidence(t, ctx, db, "link_"+fact.ID+"_evidence", fact.ID, "ep_visible")
	}
	addPrefetchFactLink(t, ctx, db, "link_city_transition_supersedes", "fact_new_city_transition", "SUPERSEDES", "fact_old_city_transition", 1.0)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	oldFact := mustPrefetchFact(t, ctx, repo, "fact_old_city_transition")
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryAbility: MemoryAbilityDynamicState,
		EvidenceNeed:  EvidenceNeedStateTransition,
		Signals:       []QuerySignal{QuerySignalStateTransition},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: oldFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[oldFact.ID]; got != MemoryBlockTypeHistoricalTransitionMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want historical_transition_memory", oldFact.ID, got)
	}
	item := requirePrefetchBlockItem(t, blocks, MemoryBlockTypeHistoricalTransitionMemory, oldFact.ID)
	if item.HistoricalStatus != MemoryHistoricalStatusSuperseded {
		t.Fatalf("historical_status = %q, want superseded", item.HistoricalStatus)
	}
}

func TestReconstructionStateTransitionSignalRoutesCurrentFactToHistoricalBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_current_transition_question", "用户现在住在上海。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)
	addPrefetchEvidence(t, ctx, db, "link_current_transition_question_evidence", fact.ID, "ep_visible")

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		EvidenceNeed: EvidenceNeedStateTransition,
		Signals:      []QuerySignal{QuerySignalStateTransition},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got != MemoryBlockTypeHistoricalTransitionMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want historical_transition_memory from state_transition signal", fact.ID, got)
	}
	requirePrefetchBlockItem(t, blocks, MemoryBlockTypeHistoricalTransitionMemory, fact.ID)
}

func TestReconstructionProvenanceSignalRoutesToProvenanceMemory(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_source_question", "用户提到自己喜欢手冲咖啡。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)
	addPrefetchEvidence(t, ctx, db, "link_source_question_evidence", fact.ID, "ep_visible")

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryAbility: MemoryAbilityDirectFact,
		EvidenceNeed:  EvidenceNeedProvenanceSource,
		Signals:       []QuerySignal{QuerySignalProvenanceSource},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got != MemoryBlockTypeProvenanceMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want provenance_memory", fact.ID, got)
	}
	item := requirePrefetchBlockItem(t, blocks, MemoryBlockTypeProvenanceMemory, fact.ID)
	if len(item.SourceRefs) != 1 {
		t.Fatalf("source_refs = %#v, want one source ref", item.SourceRefs)
	}
}

func TestReconstructionSourceEvidenceRoutesToProvenanceMemoryWithoutSignal(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_source_only", "用户喜欢浅烘咖啡。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)
	addPrefetchEvidence(t, ctx, db, "link_source_only_evidence", fact.ID, "ep_visible")

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got != MemoryBlockTypeProvenanceMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want provenance_memory from source evidence path", fact.ID, got)
	}
	requirePrefetchBlockItem(t, blocks, MemoryBlockTypeProvenanceMemory, fact.ID)
}

func TestReconstructionExperienceContextWinsOverSourceEvidenceFallback(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_workflow_source_only", "部署前需要先检查环境变量。", ptrForPrefetch("ent_user"))
	fact.FactType = core.FactTypeTaskRelevantContext
	insertPrefetchFact(t, ctx, db, fact)
	addPrefetchEvidence(t, ctx, db, "link_workflow_source_only_evidence", fact.ID, "ep_visible")

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryDomain:  MemoryDomainWorkExperience,
		MemoryAbility: MemoryAbilityWorkflow,
		EvidenceNeed:  EvidenceNeedProcedureNote,
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got != MemoryBlockTypeExperienceContext {
		t.Fatalf("blockTypeByFactID[%s] = %q, want experience_context", fact.ID, got)
	}
	item := requirePrefetchBlockItem(t, blocks, MemoryBlockTypeExperienceContext, fact.ID)
	if len(item.SourceRefs) != 1 {
		t.Fatalf("source_refs = %#v, want source ref preserved in experience_context", item.SourceRefs)
	}
}

func TestReconstructionCausalAbilityWithoutSignalDoesNotRouteToCausalBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	for _, fact := range []core.Fact{
		prefetchFact("fact_ability_only_effect", "用户因为早会安排而焦虑。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_ability_only_cause", "早会安排触发了用户焦虑。", ptrForPrefetch("ent_user")),
	} {
		insertPrefetchFact(t, ctx, db, fact)
		if fact.ID == "fact_ability_only_cause" {
			addPrefetchEvidence(t, ctx, db, "link_"+fact.ID+"_evidence", fact.ID, "ep_visible")
		}
	}
	addPrefetchFactLink(t, ctx, db, "link_ability_only_cause", "fact_ability_only_effect", "CAUSED_BY", "fact_ability_only_cause", 1.0)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	effect := mustPrefetchFact(t, ctx, repo, "fact_ability_only_effect")
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryAbility: MemoryAbilityCausalExplain,
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: effect, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[effect.ID]; got == MemoryBlockTypeRelevantCausalMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want non-causal without causal signal", effect.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want facts without causal signal", blocks)
	}
}

func TestReconstructionCausalHintWithoutSignalDoesNotRouteToCausalBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	for _, fact := range []core.Fact{
		prefetchFact("fact_causal_hint_effect", "用户因为早会安排而焦虑。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_causal_hint_cause", "早会安排触发了用户焦虑。", ptrForPrefetch("ent_user")),
	} {
		insertPrefetchFact(t, ctx, db, fact)
		if fact.ID == "fact_causal_hint_cause" {
			addPrefetchEvidence(t, ctx, db, "link_"+fact.ID+"_evidence", fact.ID, "ep_visible")
		}
	}
	addPrefetchFactLink(t, ctx, db, "link_causal_hint_cause", "fact_causal_hint_effect", "CAUSED_BY", "fact_causal_hint_cause", 1.0)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	effect := mustPrefetchFact(t, ctx, repo, "fact_causal_hint_effect")
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryAbility:     MemoryAbilityCausalExplain,
		ContextBlockHints: []string{MemoryBlockTypeRelevantCausalMemory},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: effect, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[effect.ID]; got == MemoryBlockTypeRelevantCausalMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want non-causal without causal signal", effect.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want facts without causal signal", blocks)
	}
}

func TestReconstructionProvenanceHintAndAbilityWithoutEvidenceDoesNotRouteToProvenanceBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_provenance_hint_ability_only", "用户喜欢浅烘咖啡。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryAbility:     MemoryAbilityProvenance,
		ContextBlockHints: []string{MemoryBlockTypeProvenanceMemory},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got == MemoryBlockTypeProvenanceMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want non-provenance without provenance signal/evidence or source refs", fact.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want facts without provenance signal/evidence or source refs", blocks)
	}
}

func TestReconstructionHistoricalHintAndAbilityWithoutTransitionDoesNotRouteToHistoricalBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_historical_hint_current", "用户现在住在上海。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		TimeMode:          QueryTimeModeHistorical,
		MemoryAbility:     MemoryAbilityHistorical,
		ContextBlockHints: []string{MemoryBlockTypeHistoricalTransitionMemory},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got == MemoryBlockTypeHistoricalTransitionMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want non-historical without state_transition signal/evidence or SUPERSEDES", fact.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want facts without state_transition signal/evidence or SUPERSEDES", blocks)
	}
}

func TestReconstructionRelationshipAbilityAndFactTypeWithoutSignalDoesNotRouteToRelationshipBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_relationship_ability_only", "用户和 Agent 的信任感增强。", ptrForPrefetch("ent_user"))
	fact.FactType = core.FactTypeRelationalState
	insertPrefetchFact(t, ctx, db, fact)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryAbility: MemoryAbilityRelationshipArc,
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got == MemoryBlockTypeRelationshipArcMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want non-relationship without relationship signal/evidence", fact.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want facts without relationship signal/evidence", blocks)
	}
}

func TestReconstructionNarrativeInsightCompletionRoutesToRelationshipBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_relationship_completion", "用户和 Agent 聊完以后感觉没那么孤独。", ptrForPrefetch("ent_user"))
	fact.FactType = core.FactTypeRelationalState
	insertPrefetchFact(t, ctx, db, fact)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{
		Fact:      storedFact,
		Score:     1,
		TokenCost: 1,
		Breakdown: retrievalScoreBreakdown{CompletionSource: completionSourceNarrative},
	}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got != MemoryBlockTypeRelationshipArcMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want relationship_arc_memory from narrative/insight completion", fact.ID, got)
	}
	requirePrefetchBlockItem(t, blocks, MemoryBlockTypeRelationshipArcMemory, fact.ID)
}

func TestReconstructionPremiseHintAndAbilityWithoutCounterexampleDoesNotRouteToPremiseBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	fact := prefetchFact("fact_premise_ability_only", "用户喜欢川菜。", ptrForPrefetch("ent_user"))
	insertPrefetchFact(t, ctx, db, fact)

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	storedFact := mustPrefetchFact(t, ctx, repo, fact.ID)
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryAbility:     MemoryAbilityPremiseCheck,
		ContextBlockHints: []string{MemoryBlockTypePremiseCheckMemory},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{{Fact: storedFact, Score: 1, TokenCost: 1}})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if got := blockTypeByFactID[fact.ID]; got == MemoryBlockTypePremiseCheckMemory {
		t.Fatalf("blockTypeByFactID[%s] = %q, want non-premise without premise_counterexample signal/evidence", fact.ID, got)
	}
	if len(blocks) != 1 || blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("blocks = %#v, want facts without premise_counterexample signal/evidence", blocks)
	}
}

func TestReconstructionMultipleHintsSelectsOneSecondaryBlock(t *testing.T) {
	ctx := context.Background()
	db := openPrefetchTestDB(t, ctx)
	defer db.Close()
	seedPrefetchGraph(t, ctx, db)

	for _, fact := range []core.Fact{
		prefetchFact("fact_multi_hint_one", "用户喜欢拿铁。", ptrForPrefetch("ent_user")),
		prefetchFact("fact_multi_hint_two", "用户喜欢手冲。", ptrForPrefetch("ent_user")),
	} {
		insertPrefetchFact(t, ctx, db, fact)
		addPrefetchEvidence(t, ctx, db, "link_"+fact.ID+"_evidence", fact.ID, "ep_visible")
	}

	repo := NewRetrievalRepository(db.SQLDB(), nil, prefetchNow)
	first := mustPrefetchFact(t, ctx, repo, "fact_multi_hint_one")
	second := mustPrefetchFact(t, ctx, repo, "fact_multi_hint_two")
	blocks, blockTypeByFactID, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{
		MemoryAbility: MemoryAbilityDirectFact,
		EvidenceNeed:  EvidenceNeedExactObservation,
		Signals:       []QuerySignal{QuerySignalExactFact},
		ContextBlockHints: []string{
			MemoryBlockTypeProvenanceMemory,
			MemoryBlockTypeRelevantCausalMemory,
			MemoryBlockTypePremiseCheckMemory,
		},
	}, RetrievalPolicy{
		SensitivityPermission: string(core.SensitivityNormal),
		ContextBudgetTokens:   10000,
	}, []scoredFact{
		{Fact: first, Score: 1, TokenCost: 1},
		{Fact: second, Score: 0.9, TokenCost: 1},
	})
	if err != nil {
		t.Fatalf("reconstructMemoryBlocks: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("blocks = %#v, want one primary block from multiple hints", blocks)
	}
	if blocks[0].BlockType != MemoryBlockTypeFacts {
		t.Fatalf("block type = %q, want deterministic direct fact fallback facts", blocks[0].BlockType)
	}
	for factID, blockType := range blockTypeByFactID {
		if blockType != MemoryBlockTypeFacts {
			t.Fatalf("blockTypeByFactID[%s] = %q, want facts", factID, blockType)
		}
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
	blocks, _, _, err := repo.reconstructMemoryBlocks(ctx, RetrievalRequest{PersonaID: "default"}, QueryAnalysis{EvidenceNeed: EvidenceNeedStateTransition}, RetrievalPolicy{
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

func requirePrefetchBlockItem(t *testing.T, blocks []MemoryBlock, blockType string, nodeID string) MemoryContextItem {
	t.Helper()

	for _, block := range blocks {
		if block.BlockType != blockType {
			continue
		}
		for _, item := range block.Items {
			if item.NodeID == nodeID {
				return item
			}
		}
		t.Fatalf("item %q not found in block %q: %#v", nodeID, blockType, block.Items)
	}
	t.Fatalf("block %q not found in %#v", blockType, blocks)
	return MemoryContextItem{}
}

func ptrForPrefetchTime(value time.Time) *time.Time {
	return &value
}
