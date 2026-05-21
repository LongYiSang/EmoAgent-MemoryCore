package memorycore_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestServiceRetrieveFindsConsolidatedFactByKeywordAndLogsAccess(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	coffee := "咖啡"
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", coffee, "用户喜欢咖啡。", episode.ID).Fact

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")

	db := openSQLDB(t, dbPath)
	defer db.Close()
	requireAccessEvent(t, db, fact.ID, "retrieved")
	breakdown := requireAccessEventBreakdown(t, db, fact.ID, "retrieved")
	if _, ok := breakdown["activation_score"].(float64); !ok {
		t.Fatalf("activation_score = %#v, want number", breakdown["activation_score"])
	}
	if _, ok := breakdown["graph_energy"].(float64); !ok {
		t.Fatalf("graph_energy = %#v, want number", breakdown["graph_energy"])
	}
	if _, ok := breakdown["final_score"].(float64); !ok {
		t.Fatalf("final_score = %#v, want number", breakdown["final_score"])
	}
	requireBreakdownObject(t, breakdown, "query_analysis")
	observed := requireBreakdownObject(t, breakdown, "observed_confidence")
	if _, ok := observed["overall"].(float64); !ok {
		t.Fatalf("observed_confidence.overall = %#v, want number", observed["overall"])
	}
	queryAnalysis := requireBreakdownObject(t, breakdown, "query_analysis")
	scores := requireBreakdownObject(t, queryAnalysis, "scores")
	if _, ok := scores["expected_retrieval_confidence"].(float64); !ok {
		t.Fatalf("query_analysis.scores.expected_retrieval_confidence = %#v, want number", scores["expected_retrieval_confidence"])
	}
}

func TestServiceRetrieveChineseCounterexampleExpansionWorksWithQueryAnalysisDisabled(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openRetrievalMirrorServiceWithQueryAnalysisOptions(t, ctx, nil, memorycore.QueryAnalysisOptions{
		Provider: memorycore.QueryAnalysisProviderNone,
		Mode:     memorycore.QueryAnalysisModeRuleOnlyExplicit,
	})
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "后来我自己做了糖醋排骨，味道不错。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "糖醋排骨", "用户后来自己做了糖醋排骨，味道不错。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, fact.ID, "importance", 0.1)

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "我是不是完全不会做饭，从来没下厨房？",
		Policy: memorycore.RetrievalPolicy{
			FinalMemoryCount: 1,
			UseMirror:        false,
			UseFTS:           false,
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if contextResult.QueryAnalysis == nil {
		t.Fatalf("query analysis is nil")
	}
	if contextResult.QueryAnalysis.Source != memorycore.QueryAnalysisSourceRuleOnly {
		t.Fatalf("query analysis source = %q, want rule_only", contextResult.QueryAnalysis.Source)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户后来自己做了糖醋排骨，味道不错。", "")
}

func TestServiceRetrieveReturnsQueryAnalysisAndAppliesHistoricalEffectivePolicy(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我以前参与过旧项目。", time.Date(2025, 1, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "旧项目", "用户以前参与过旧项目。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, fact.ID, "validity_status", memorycore.ValidityInvalidated)
	updateFactColumn(t, db, fact.ID, "lifecycle_status", "archived")

	historical, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "Long 以前 旧项目",
	})
	if err != nil {
		t.Fatalf("retrieve historical query: %v", err)
	}
	requireMemoryItem(t, historical, fact.ID, "用户以前参与过旧项目。", "historical")
	if historical.QueryAnalysis == nil {
		t.Fatalf("query analysis is nil")
	}
	if historical.QueryAnalysis.TimeMode != memorycore.QueryTimeModeHistorical {
		t.Fatalf("time_mode = %q, want historical", historical.QueryAnalysis.TimeMode)
	}
	if !hasQuerySignal(historical.QueryAnalysis.Signals, memorycore.QuerySignalHistorical) {
		t.Fatalf("signals = %#v, want historical", historical.QueryAnalysis.Signals)
	}
	if historical.QueryAnalysis.MemoryAbility != memorycore.MemoryAbilityDirectFact {
		t.Fatalf("memory_ability = %q, want direct_fact", historical.QueryAnalysis.MemoryAbility)
	}
	if historical.QueryAnalysis.EvidenceNeed != memorycore.EvidenceNeedExactObservation {
		t.Fatalf("evidence_need = %q, want exact_observation", historical.QueryAnalysis.EvidenceNeed)
	}
	if hasQuerySignal(historical.QueryAnalysis.Signals, memorycore.QuerySignalStateTransition) {
		t.Fatalf("signals = %#v, should not include state_transition for bare historical lookup", historical.QueryAnalysis.Signals)
	}

	updateFactColumn(t, db, fact.ID, "lifecycle_status", "deep_archived")
	deepDefault, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "Long 以前 旧项目",
	})
	if err != nil {
		t.Fatalf("retrieve historical deep default: %v", err)
	}
	requireNoMemoryItem(t, deepDefault, fact.ID)

	deepAllowed, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "Long 以前 旧项目",
		Policy: memorycore.RetrievalPolicy{
			AllowDeepArchive: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve historical deep allowed: %v", err)
	}
	requireMemoryItem(t, deepAllowed, fact.ID, "用户以前参与过旧项目。", "historical")
}

func TestServiceRetrievePreservesPhase5EContextFields(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "早会让我焦虑，所以我最近抗拒上班。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	effect := consolidateLiteral(t, ctx, svc, userID, "dislikes", "上班焦虑", "用户因为早会安排而对上班感到焦虑。", episode.ID).Fact
	cause := consolidateLiteral(t, ctx, svc, userID, "dislikes", "早会", "用户不喜欢早会安排。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertPublicFactLink(t, db, "link_public_effect_cause", effect.ID, "CAUSED_BY", cause.ID)

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "为什么 焦虑 上班",
		Policy: memorycore.RetrievalPolicy{
			FinalMemoryCount: 1,
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	block := requirePublicBlock(t, contextResult, memorycore.MemoryBlockTypeRelevantCausalMemory)
	item := requirePublicBlockItem(t, block, effect.ID)
	if item.HistoricalStatus != memorycore.MemoryHistoricalStatusCurrent {
		t.Fatalf("historical_status = %q, want current", item.HistoricalStatus)
	}
	if len(item.SourceRefs) != 1 {
		t.Fatalf("source_refs = %#v, want one source", item.SourceRefs)
	}
	if item.SourceRefs[0].EpisodeID != episode.ID || item.SourceRefs[0].QuoteAllowed {
		t.Fatalf("source ref = %#v, want safe episode id and quote_allowed=false", item.SourceRefs[0])
	}
	if len(item.RelatedFacts) != 1 {
		t.Fatalf("related_facts = %#v, want one related fact", item.RelatedFacts)
	}
	related := item.RelatedFacts[0]
	if related.NodeID != cause.ID || related.LinkType != "CAUSED_BY" || related.Direction != "outbound" || related.HistoricalStatus != memorycore.MemoryHistoricalStatusCurrent {
		t.Fatalf("related fact = %#v, want public causal related fact", related)
	}
	if !item.DoNotOverstate {
		t.Fatalf("do_not_overstate = false, want true for causal context")
	}
}

func TestMemoryContextItemJSONOmitsPhase5EZeroValueFields(t *testing.T) {
	item := memorycore.MemoryContextItem{
		NodeType:         "fact",
		NodeID:           "fact_direct",
		Summary:          "用户喜欢咖啡。",
		Confidence:       0.8,
		HistoricalStatus: memorycore.MemoryHistoricalStatusCurrent,
		SourceRefs:       []memorycore.MemorySourceRef{},
		RelatedFacts:     []memorycore.MemoryRelatedFactRef{},
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal memory context item: %v", err)
	}
	jsonText := string(raw)
	for _, field := range []string{`"ValidFrom"`, `"ValidTo"`, `"SourceRefs"`, `"RelatedFacts"`, `"DoNotOverstate"`} {
		if strings.Contains(jsonText, field) {
			t.Fatalf("json contains zero-value Phase 5E field %s: %s", field, jsonText)
		}
	}
	if !strings.Contains(jsonText, `"HistoricalStatus":"current"`) {
		t.Fatalf("json = %s, want current historical status preserved", jsonText)
	}
}

func TestServiceRetrieveQueryAnalysisKeepsSensitivityGateAndReportsEntityMentions(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	if _, err := svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{
		EntityID:  userID,
		Alias:     "LongYi",
		AliasType: memorycore.AliasTypeNickname,
	}); err != nil {
		t.Fatalf("add alias: %v", err)
	}
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我以前的银行卡卡号是4111。", time.Date(2025, 1, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "银行卡卡号4111", "用户以前提到银行卡卡号4111。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, fact.ID, "validity_status", memorycore.ValidityInvalidated)
	updateFactColumn(t, db, fact.ID, "lifecycle_status", "archived")
	updateFactColumn(t, db, fact.ID, "sensitivity_level", memorycore.SensitivitySensitive)

	normalPermission, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "debug: LongYi 上次部署为什么失败的证据和银行卡4111",
	})
	if err != nil {
		t.Fatalf("retrieve normal permission: %v", err)
	}
	requireNoMemoryItem(t, normalPermission, fact.ID)
	if normalPermission.QueryAnalysis == nil {
		t.Fatalf("query analysis is nil")
	}
	analysis := normalPermission.QueryAnalysis
	if analysis.MemoryDomain != memorycore.MemoryDomainWorkExperience {
		t.Fatalf("memory_domain = %q, want work_experience_memory", analysis.MemoryDomain)
	}
	if analysis.MemoryAbility != memorycore.MemoryAbilityProvenance {
		t.Fatalf("memory_ability = %q, want provenance", analysis.MemoryAbility)
	}
	if analysis.EvidenceNeed != memorycore.EvidenceNeedProvenanceSource {
		t.Fatalf("evidence_need = %q, want provenance_source", analysis.EvidenceNeed)
	}
	for _, signal := range []memorycore.QuerySignal{
		memorycore.QuerySignalHistorical,
		memorycore.QuerySignalCausal,
		memorycore.QuerySignalProvenance,
		memorycore.QuerySignalDebug,
	} {
		if !hasQuerySignal(analysis.Signals, signal) {
			t.Fatalf("signals = %#v, want %q", analysis.Signals, signal)
		}
	}
	if len(analysis.EntityMentions) != 1 || analysis.EntityMentions[0].EntityID != userID || analysis.EntityMentions[0].MatchText != "LongYi" {
		t.Fatalf("entity_mentions = %#v, want alias mention for %s", analysis.EntityMentions, userID)
	}

	sensitivePermission, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "LongYi 以前的银行卡4111",
		Policy: memorycore.RetrievalPolicy{
			SensitivityPermission: memorycore.SensitivitySensitive,
		},
	})
	if err != nil {
		t.Fatalf("retrieve sensitive permission: %v", err)
	}
	requireMemoryItem(t, sensitivePermission, fact.ID, "用户以前提到银行卡卡号4111。", "historical")
}

func TestServiceRetrieveQueryAnalysisDoesNotLeakSensitiveEntityMentions(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	_, userID := seedConsolidationSubject(t, ctx, svc)
	if _, err := svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{
		EntityID:  userID,
		Alias:     "LongYi",
		AliasType: memorycore.AliasTypeNickname,
	}); err != nil {
		t.Fatalf("add alias: %v", err)
	}

	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateEntityColumn(t, db, userID, "sensitivity_level", memorycore.SensitivitySensitive)

	normalPermission, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "LongYi",
	})
	if err != nil {
		t.Fatalf("retrieve normal permission: %v", err)
	}
	if normalPermission.QueryAnalysis == nil {
		t.Fatalf("query analysis is nil")
	}
	if len(normalPermission.QueryAnalysis.EntityMentions) != 0 {
		t.Fatalf("entity_mentions = %#v, want no sensitive entity mention at normal permission", normalPermission.QueryAnalysis.EntityMentions)
	}

	sensitivePermission, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "LongYi",
		Policy: memorycore.RetrievalPolicy{
			SensitivityPermission: memorycore.SensitivitySensitive,
		},
	})
	if err != nil {
		t.Fatalf("retrieve sensitive permission: %v", err)
	}
	if sensitivePermission.QueryAnalysis == nil {
		t.Fatalf("query analysis is nil")
	}
	if len(sensitivePermission.QueryAnalysis.EntityMentions) != 1 || sensitivePermission.QueryAnalysis.EntityMentions[0].EntityID != userID || sensitivePermission.QueryAnalysis.EntityMentions[0].MatchText != "LongYi" {
		t.Fatalf("entity_mentions = %#v, want alias mention for %s with sensitive permission", sensitivePermission.QueryAnalysis.EntityMentions, userID)
	}
}

func TestServiceRetrieveAuthorityFiltersLinkedSensitiveEntities(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	secretEntity, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		CanonicalName:    "SecretProject",
		EntityType:       memorycore.EntityTypeConcept,
		SensitivityLevel: memorycore.SensitivitySensitive,
	})
	if err != nil {
		t.Fatalf("ensure secret entity: %v", err)
	}
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我参与过 SecretProject。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	result, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "likes",
			ObjectEntityID:   &secretEntity.ID,
			ContentSummary:   "用户参与过一个项目。",
			SourceEpisodeIDs: []string{episode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.7,
			Sensitivity:      memorycore.SensitivityNormal,
		},
	})
	if err != nil {
		t.Fatalf("consolidate linked entity fact: %v", err)
	}
	if result.Fact == nil {
		t.Fatalf("result fact is nil: %#v", result)
	}

	normalPermission, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "SecretProject",
	})
	if err != nil {
		t.Fatalf("retrieve normal permission: %v", err)
	}
	requireNoMemoryItem(t, normalPermission, result.Fact.ID)

	sensitivePermission, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "SecretProject",
		Policy: memorycore.RetrievalPolicy{
			SensitivityPermission: memorycore.SensitivitySensitive,
		},
	})
	if err != nil {
		t.Fatalf("retrieve sensitive permission: %v", err)
	}
	requireMemoryItem(t, sensitivePermission, result.Fact.ID, "用户参与过一个项目。", "")
}

func TestServiceRetrieveAppliesAuthorityFilters(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡、茶和果汁。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	visible := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	hidden := consolidateLiteral(t, ctx, svc, userID, "likes", "茶", "用户喜欢茶。", episode.ID).Fact
	forgotten := consolidateLiteral(t, ctx, svc, userID, "likes", "果汁", "用户喜欢果汁。", episode.ID).Fact
	unsearchable := consolidateLiteral(t, ctx, svc, userID, "likes", "汽水", "用户喜欢汽水。", episode.ID).Fact

	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, hidden.ID, "visibility_status", memorycore.VisibilityHidden)
	updateFactColumn(t, db, forgotten.ID, "visibility_status", memorycore.VisibilityForgotten)
	updateFactColumn(t, db, unsearchable.ID, "searchable", 0)

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "用户喜欢",
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}

	requireMemoryItem(t, contextResult, visible.ID, "用户喜欢咖啡。", "")
	requireNoMemoryItem(t, contextResult, hidden.ID)
	requireNoMemoryItem(t, contextResult, forgotten.ID)
	requireNoMemoryItem(t, contextResult, unsearchable.ID)
}

func TestServiceRetrieveHistoricalAndDeepArchivePolicy(t *testing.T) {
	ctx := context.Background()
	svc, dbPath := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	firstEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "请叫我 Long。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	secondEpisode := appendConsolidationEpisode(t, ctx, svc, sessionID, "以后叫我 Yi。", time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC))
	longName := "Long"
	yiName := "Yi"
	oldName, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "prefers_name",
			ObjectLiteral:    &longName,
			ContentSummary:   "用户偏好被称呼为 Long。",
			SourceEpisodeIDs: []string{firstEpisode.ID},
		},
	})
	if err != nil {
		t.Fatalf("consolidate old name: %v", err)
	}
	newName, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "prefers_name",
			ObjectLiteral:    &yiName,
			ContentSummary:   "用户偏好被称呼为 Yi。",
			SourceEpisodeIDs: []string{secondEpisode.ID},
		},
	})
	if err != nil {
		t.Fatalf("consolidate new name: %v", err)
	}

	currentOnly, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "Long"})
	if err != nil {
		t.Fatalf("retrieve current only: %v", err)
	}
	requireNoMemoryItem(t, currentOnly, oldName.Fact.ID)

	historical, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "Long",
		Policy: memorycore.RetrievalPolicy{
			AllowHistorical: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve historical: %v", err)
	}
	requireMemoryItem(t, historical, oldName.Fact.ID, "用户偏好被称呼为 Long。", "historical")

	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, newName.Fact.ID, "lifecycle_status", "archived")

	archivedDefault, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{QueryText: "Yi"})
	if err != nil {
		t.Fatalf("retrieve archived default: %v", err)
	}
	requireNoMemoryItem(t, archivedDefault, newName.Fact.ID)

	archivedHistorical, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "Yi",
		Policy: memorycore.RetrievalPolicy{
			AllowHistorical: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve archived historical: %v", err)
	}
	requireMemoryItem(t, archivedHistorical, newName.Fact.ID, "用户偏好被称呼为 Yi。", "")

	updateFactColumn(t, db, newName.Fact.ID, "lifecycle_status", "deep_archived")

	deepDefault, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "Yi"})
	if err != nil {
		t.Fatalf("retrieve deep default: %v", err)
	}
	requireNoMemoryItem(t, deepDefault, newName.Fact.ID)

	deepAllowed, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "Yi",
		Policy: memorycore.RetrievalPolicy{
			AllowDeepArchive: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve deep allowed: %v", err)
	}
	requireMemoryItem(t, deepAllowed, newName.Fact.ID, "用户偏好被称呼为 Yi。", "")
}

func TestServiceRetrieveSensitivityPermission(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "不要在晚上十点后提醒我工作。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	boundary := consolidateLiteral(t, ctx, svc, userID, "has_boundary", "晚上十点后不要提醒我工作", "用户不希望晚上十点后被提醒工作。", episode.ID).Fact

	normal, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "晚上十点",
	})
	if err != nil {
		t.Fatalf("retrieve normal: %v", err)
	}
	requireNoMemoryItem(t, normal, boundary.ID)

	sensitive, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "晚上十点",
		Policy: memorycore.RetrievalPolicy{
			SensitivityPermission: memorycore.SensitivitySensitive,
		},
	})
	if err != nil {
		t.Fatalf("retrieve sensitive: %v", err)
	}
	requireMemoryItem(t, sensitive, boundary.ID, "用户不希望晚上十点后被提醒工作。", "")
}

func TestServiceRetrieveFatigueSuppression(t *testing.T) {
	ctx := context.Background()
	svc, _ := openConsolidationService(t, ctx)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	first, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
	if err != nil {
		t.Fatalf("first retrieve: %v", err)
	}
	requireMemoryItem(t, first, fact.ID, "用户喜欢咖啡。", "")

	second, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{SessionID: &sessionID, QueryText: "咖啡"})
	if err != nil {
		t.Fatalf("second retrieve: %v", err)
	}
	requireNoMemoryItem(t, second, fact.ID)
	if len(second.DoNotMention) != 1 || second.DoNotMention[0].NodeID != fact.ID {
		t.Fatalf("suppression = %#v, want fact %s", second.DoNotMention, fact.ID)
	}
}

func TestPublicSuppressionReasonConstants(t *testing.T) {
	if memorycore.MemorySuppressionReasonFatigue != "fatigue" {
		t.Fatalf("fatigue suppression reason = %q, want fatigue", memorycore.MemorySuppressionReasonFatigue)
	}
	if memorycore.MemorySuppressionReasonMMRDuplicate != "mmr_duplicate" {
		t.Fatalf("mmr suppression reason = %q, want mmr_duplicate", memorycore.MemorySuppressionReasonMMRDuplicate)
	}
	if memorycore.MemorySuppressionReasonContextBudget != "context_budget" {
		t.Fatalf("context budget suppression reason = %q, want context_budget", memorycore.MemorySuppressionReasonContextBudget)
	}
}

func TestServiceRetrieveUseMirrorAddsValidatedCandidate(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7001, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7001, Score: 0.88, Source: "fake_sparse"}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "espresso-only",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with mirror: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")
	if contextResult.Mirror == nil || contextResult.Mirror.Status != "used" {
		t.Fatalf("mirror diagnostics = %#v, want used", contextResult.Mirror)
	}
	if contextResult.Mirror.SidecarCandidateCount != 1 || contextResult.Mirror.MappedCandidateCount != 1 || contextResult.Mirror.DroppedCandidateCount != 0 {
		t.Fatalf("mirror counts = %#v", contextResult.Mirror)
	}
}

func TestServiceRetrieveLowAuthorityConfidenceFallsBackToSQLiteOnce(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡，也提到过隐藏茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	coffee := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	hidden := consolidateLiteral(t, ctx, svc, userID, "likes", "隐藏茶", "用户喜欢隐藏茶。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, hidden.ID, "visibility_status", memorycore.VisibilityHidden)
	insertMirrorMapForFact(t, db, hidden.ID, 7011, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7011, Score: 0.99, Source: "trivium_dense", Rank: 1}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy: memorycore.RetrievalPolicy{
			UseMirror:        true,
			UseFTS:           true,
			FinalMemoryCount: 1,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with corrective fallback: %v", err)
	}
	requireMemoryItem(t, contextResult, coffee.ID, "用户喜欢咖啡。", "")
	requireNoMemoryItem(t, contextResult, hidden.ID)
	if len(adapter.candidateRequests) != 1 {
		t.Fatalf("mirror candidate requests = %d, want one initial request before sqlite fallback", len(adapter.candidateRequests))
	}
	if contextResult.RetrievalConfidence == nil {
		t.Fatalf("retrieval confidence is nil")
	}
	if contextResult.RetrievalConfidence.CorrectiveAction != memorycore.RetrievalCorrectiveActionSQLiteFallback {
		t.Fatalf("corrective action = %q, want sqlite_fallback", contextResult.RetrievalConfidence.CorrectiveAction)
	}
	if contextResult.RetrievalConfidence.AuthorityPassRatio != 1 {
		t.Fatalf("authority_pass_ratio = %f, want final sqlite fallback ratio 1", contextResult.RetrievalConfidence.AuthorityPassRatio)
	}
}

func TestServiceRetrieveCorrectiveSemanticFailureFallsBackToSQLite(t *testing.T) {
	ctx := context.Background()
	semanticCalls := 0
	semanticSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		semanticCalls++
		http.Error(w, "semantic unavailable", http.StatusServiceUnavailable)
	}))
	defer semanticSidecar.Close()

	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorServiceWithQueryAnalysisOptions(t, ctx, adapter, memorycore.QueryAnalysisOptions{
		Provider:   memorycore.QueryAnalysisProviderSidecar,
		Mode:       memorycore.QueryAnalysisModeRuleOnlyExplicit,
		SidecarURL: semanticSidecar.URL,
		Timeout:    time.Second,
	})
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "早会让我抗拒上班。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "dislikes", "早会", "用户因为早会安排而抗拒上班。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, fact.ID, "importance", 1.0)

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "为什么最近抗拒早会",
		Policy: memorycore.RetrievalPolicy{
			UseMirror:        true,
			UseFTS:           false,
			FinalMemoryCount: 1,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with semantic corrective failure: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户因为早会安排而抗拒上班。", "")
	if semanticCalls != 1 {
		t.Fatalf("semantic corrective calls = %d, want 1", semanticCalls)
	}
	if len(adapter.candidateRequests) != 1 {
		t.Fatalf("mirror candidate requests = %d, want one initial request before sqlite fallback", len(adapter.candidateRequests))
	}
	if contextResult.Mirror == nil || contextResult.Mirror.Status != "disabled_by_corrective_sqlite_fallback" {
		t.Fatalf("mirror diagnostics = %#v, want corrective sqlite fallback", contextResult.Mirror)
	}
	if contextResult.RetrievalConfidence == nil || contextResult.RetrievalConfidence.CorrectiveAction != memorycore.RetrievalCorrectiveActionSemanticLight {
		t.Fatalf("retrieval confidence = %#v, want semantic_light corrective action", contextResult.RetrievalConfidence)
	}
}

func TestServiceRetrieveRunsSemanticAnalysisBeforeMirrorAndPassesMergedAnalysis(t *testing.T) {
	ctx := context.Background()
	semanticSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/query-analysis" {
			t.Fatalf("path = %s, want /retrieval/query-analysis", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode query analysis request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version":  "memory_query_analysis_result.v0.1",
			"request_id":      request["request_id"],
			"status":          "ok",
			"provider":        "sidecar",
			"model":           "semantic-test",
			"prompt_version":  "qa-test",
			"fallback_reason": "",
			"analysis": map[string]any{
				"time_mode":      "historical",
				"memory_domain":  "work_experience_memory",
				"memory_ability": "provenance",
				"evidence_need":  "provenance_source",
				"confidence":     0.93,
				"field_confidence": map[string]any{
					"overall":        0.93,
					"time_mode":      0.93,
					"memory_ability": 0.93,
					"memory_domain":  0.93,
					"evidence_need":  0.93,
				},
				"signals":          []string{"provenance"},
				"query_rewrites":   []map[string]any{{"text": "semantic rewrite target", "purpose": "semantic_recall", "weight": 0.7}},
				"semantic_anchors": []map[string]any{{"text": "semantic anchor target", "weight": 0.5}},
			},
		})
	}))
	defer semanticSidecar.Close()

	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorServiceWithQueryAnalysisOptions(t, ctx, adapter, memorycore.QueryAnalysisOptions{
		Provider:   memorycore.QueryAnalysisProviderSidecar,
		Mode:       memorycore.QueryAnalysisModeSemanticAlways,
		SidecarURL: semanticSidecar.URL,
		Timeout:    time.Second,
	})
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我之前在 repo 里记录过来源。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "repo", "用户记录过 repo 来源。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7251, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7251, Score: 0.88, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "where did repo source come from",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户记录过 repo 来源。", "")
	if adapter.calls != 2 {
		t.Fatalf("mirror calls = %d, want 2 dual-lane calls", adapter.calls)
	}
	if adapter.candidateRequests[0].Query.Source != memorycore.QueryAnalysisSourceRuleOnly {
		t.Fatalf("raw lane query source = %q, want rule_only", adapter.candidateRequests[0].Query.Source)
	}
	semanticRequest := adapter.candidateRequests[1]
	if semanticRequest.Query.Source != memorycore.QueryAnalysisSourceMerged {
		t.Fatalf("semantic lane query source = %q, want merged: %#v", semanticRequest.Query.Source, semanticRequest.Query)
	}
	if len(semanticRequest.Query.QueryRewrites) != 1 || semanticRequest.Query.QueryRewrites[0].Text != "semantic rewrite target" {
		t.Fatalf("semantic lane query rewrites = %#v", semanticRequest.Query.QueryRewrites)
	}
	if contextResult.QueryAnalysis == nil || contextResult.QueryAnalysis.Source != memorycore.QueryAnalysisSourceMerged {
		t.Fatalf("result query analysis = %#v, want merged", contextResult.QueryAnalysis)
	}
}

func TestRetrievalAPIDoesNotSendDroppedEnglishRewriteToMirrorCandidates(t *testing.T) {
	ctx := context.Background()
	semanticSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode query analysis request: %v", err)
		}
		time.Sleep(80 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_query_analysis_result.v0.1",
			"request_id":     request["request_id"],
			"status":         "ok",
			"analysis": map[string]any{
				"time_mode":      "current",
				"memory_domain":  "user_profile_memory",
				"memory_ability": "provenance",
				"evidence_need":  "provenance_source",
				"confidence":     0.95,
				"field_confidence": map[string]any{
					"overall":        0.95,
					"time_mode":      0.95,
					"memory_ability": 0.95,
					"memory_domain":  0.95,
					"evidence_need":  0.95,
				},
				"query_rewrites": []map[string]any{
					{"text": "when did the user say they like Laufey", "purpose": "semantic_recall", "weight": 0.7},
					{"text": "用户喜欢Laufey的来源", "purpose": "semantic_recall", "weight": 0.6},
					{"text": "Laufey", "purpose": "entity_anchor", "weight": 0.5},
				},
			},
		})
	}))
	defer semanticSidecar.Close()

	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorServiceWithQueryAnalysisOptions(t, ctx, adapter, memorycore.QueryAnalysisOptions{
		Provider:        memorycore.QueryAnalysisProviderSidecar,
		Mode:            memorycore.QueryAnalysisModeSemanticAlways,
		SidecarURL:      semanticSidecar.URL,
		Timeout:         time.Second,
		SoftJoinTimeout: 500 * time.Millisecond,
	})
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢Laufey。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "Laufey", "用户喜欢Laufey。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7254, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7254, Score: 0.88, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "我喜欢Laufey这件事是从哪里知道的？",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢Laufey。", "")
	if adapter.calls != 2 {
		t.Fatalf("mirror calls = %d, want 2 dual-lane calls", adapter.calls)
	}
	if len(adapter.lastCandidateRequest.Query.QueryRewrites) != 2 {
		t.Fatalf("mirror query rewrites = %#v, want dropped English rewrite excluded", adapter.lastCandidateRequest.Query.QueryRewrites)
	}
	for _, rewrite := range adapter.lastCandidateRequest.Query.QueryRewrites {
		if rewrite.Text == "when did the user say they like Laufey" {
			t.Fatalf("mirror query rewrites leaked dropped English rewrite: %#v", adapter.lastCandidateRequest.Query.QueryRewrites)
		}
	}
	if contextResult.QueryAnalysis == nil || contextResult.QueryAnalysis.Diagnostics == nil {
		t.Fatalf("query analysis diagnostics = %#v, want rewrite drop diagnostics", contextResult.QueryAnalysis)
	}
	if contextResult.QueryAnalysis.Diagnostics.DroppedRewriteCount != 1 {
		t.Fatalf("dropped rewrite count = %d, want 1", contextResult.QueryAnalysis.Diagnostics.DroppedRewriteCount)
	}
}

func TestServiceRetrieveSemanticFailureStillCallsMirrorWithRuleFallbackAnalysis(t *testing.T) {
	ctx := context.Background()
	semanticSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "semantic down with private body", http.StatusServiceUnavailable)
	}))
	defer semanticSidecar.Close()

	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorServiceWithQueryAnalysisOptions(t, ctx, adapter, memorycore.QueryAnalysisOptions{
		Provider:   memorycore.QueryAnalysisProviderSidecar,
		Mode:       memorycore.QueryAnalysisModeSemanticAlways,
		SidecarURL: semanticSidecar.URL,
		Timeout:    time.Second,
	})
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7252, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7252, Score: 0.88, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")
	if adapter.calls != 1 {
		t.Fatalf("mirror calls = %d, want 1", adapter.calls)
	}
	if adapter.lastCandidateRequest.Query.Source != memorycore.QueryAnalysisSourceRuleOnly {
		t.Fatalf("raw lane query source = %q, want rule_only", adapter.lastCandidateRequest.Query.Source)
	}
	if adapter.lastCandidateRequest.Query.Raw != "咖啡" || len(adapter.lastCandidateRequest.Query.QueryRewrites) != 0 || len(adapter.lastCandidateRequest.Query.SemanticAnchors) != 0 {
		t.Fatalf("mirror query = %#v, want raw rule fallback without generated semantic queries", adapter.lastCandidateRequest.Query)
	}
	if contextResult.QueryAnalysis == nil || contextResult.QueryAnalysis.Source != memorycore.QueryAnalysisSourceSemanticFallback {
		t.Fatalf("result query analysis = %#v, want semantic fallback diagnostics", contextResult.QueryAnalysis)
	}
}

func TestServiceRetrieveDoesNotWaitFullSemanticTimeoutAfterRawMirror(t *testing.T) {
	ctx := context.Background()
	semanticStarted := make(chan struct{})
	semanticSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(semanticStarted)
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_query_analysis_result.v0.1",
			"request_id":     r.Header.Get("x-request-id"),
			"status":         "ok",
			"analysis": map[string]any{
				"time_mode":      "current",
				"memory_domain":  "user_profile_memory",
				"memory_ability": "direct_fact",
				"evidence_need":  "exact_observation",
				"confidence":     0.9,
			},
		})
	}))
	defer semanticSidecar.Close()

	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorServiceWithQueryAnalysisOptions(t, ctx, adapter, memorycore.QueryAnalysisOptions{
		Provider:        memorycore.QueryAnalysisProviderSidecar,
		Mode:            memorycore.QueryAnalysisModeSemanticAlways,
		SidecarURL:      semanticSidecar.URL,
		Timeout:         500 * time.Millisecond,
		SoftJoinTimeout: 50 * time.Millisecond,
	})
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7255, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7255, Score: 0.88, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}

	started := time.Now()
	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	select {
	case <-semanticStarted:
	default:
		t.Fatal("semantic sidecar was not called")
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")
	if elapsed >= 250*time.Millisecond {
		t.Fatalf("retrieve elapsed %s, want raw path to finish before full semantic timeout", elapsed)
	}
	if adapter.calls != 1 {
		t.Fatalf("mirror calls = %d, want only raw lane when semantic is not soft-join ready", adapter.calls)
	}
	if contextResult.QueryAnalysis == nil || contextResult.QueryAnalysis.Diagnostics == nil || contextResult.QueryAnalysis.Diagnostics.FallbackReason != "semantic_soft_timeout" {
		t.Fatalf("query analysis = %#v, want semantic soft-timeout fallback diagnostic", contextResult.QueryAnalysis)
	}
}

func TestServiceRetrieveRerankInputExcludesSemanticRewritesAndAnchors(t *testing.T) {
	ctx := context.Background()
	semanticSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode query analysis request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_query_analysis_result.v0.1",
			"request_id":     request["request_id"],
			"status":         "ok",
			"analysis": map[string]any{
				"time_mode":      "current",
				"memory_domain":  "user_profile_memory",
				"memory_ability": "provenance",
				"evidence_need":  "provenance_source",
				"signals":        []string{"provenance_source"},
				"confidence":     0.95,
				"field_confidence": map[string]any{
					"overall":        0.95,
					"time_mode":      0.95,
					"memory_ability": 0.95,
					"memory_domain":  0.95,
					"evidence_need":  0.95,
				},
				"query_rewrites":   []map[string]any{{"text": "SECRET_REWRITE_SHOULD_NOT_RERANK", "purpose": "semantic_recall", "weight": 0.7}},
				"semantic_anchors": []map[string]any{{"text": "SECRET_ANCHOR_SHOULD_NOT_RERANK", "weight": 0.5}},
			},
		})
	}))
	defer semanticSidecar.Close()

	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorServiceWithQueryAnalysisOptions(t, ctx, adapter, memorycore.QueryAnalysisOptions{
		Provider:   memorycore.QueryAnalysisProviderSidecar,
		Mode:       memorycore.QueryAnalysisModeSemanticAlways,
		SidecarURL: semanticSidecar.URL,
		Timeout:    time.Second,
	})
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7253, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7253, Score: 0.88, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}
	adapter.rerankItems = []memorycore.MirrorRerankItem{{NodeID: fact.ID, NodeType: "fact", RerankScore: 0.8}}

	_, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if adapter.rerankCalls != 1 {
		t.Fatalf("rerank calls = %d, want 1", adapter.rerankCalls)
	}
	if strings.Contains(adapter.lastRerankRequest.QueryText, "SECRET_REWRITE_SHOULD_NOT_RERANK") || strings.Contains(adapter.lastRerankRequest.QueryText, "SECRET_ANCHOR_SHOULD_NOT_RERANK") {
		t.Fatalf("rerank query leaked generated semantic text: %q", adapter.lastRerankRequest.QueryText)
	}
	if !strings.Contains(adapter.lastRerankRequest.QueryText, "query=咖啡") {
		t.Fatalf("rerank query = %q, want bounded raw/normalized query", adapter.lastRerankRequest.QueryText)
	}
}

func TestServiceRetrieveDirectFactSkipsRerankWhenRawExactMarginHigh(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 9251, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 9251, Score: 0.99, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true, UseFTS: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")
	if adapter.rerankCalls != 0 {
		t.Fatalf("rerank calls = %d, want 0 for high-confidence direct fact; request=%#v", adapter.rerankCalls, adapter.lastRerankRequest)
	}
	if contextResult.Rerank == nil || contextResult.Rerank.Status != "skipped" || contextResult.Rerank.SkippedReason == "" {
		t.Fatalf("rerank diagnostics = %#v, want skipped with reason", contextResult.Rerank)
	}
	if contextResult.Rerank.InputCount == 0 {
		t.Fatalf("rerank diagnostics = %#v, want input count", contextResult.Rerank)
	}
}

func TestServiceRetrievePremiseAndProvenanceQueriesStillCallRerank(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		object   string
		summary  string
		source   string
		mirrorID int64
	}{
		{
			name:     "premise counterexample",
			query:    "我是不是完全不会做饭，从来没下厨房？",
			object:   "糖醋排骨",
			summary:  "用户后来自己做了糖醋排骨，味道不错。",
			source:   "后来我自己做了糖醋排骨，味道不错。",
			mirrorID: 9252,
		},
		{
			name:     "provenance source",
			query:    "小陈建议我睡前听白噪音这件事，是什么时候告诉我的？",
			object:   "白噪音",
			summary:  "小陈建议用户睡前听白噪音。",
			source:   "小陈建议我睡前听白噪音。",
			mirrorID: 9253,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			adapter := &retrievalMirrorAdapter{}
			svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
			defer svc.Close()

			sessionID, userID := seedConsolidationSubject(t, ctx, svc)
			episode := appendConsolidationEpisode(t, ctx, svc, sessionID, tt.source, time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
			fact := consolidateLiteral(t, ctx, svc, userID, "likes", tt.object, tt.summary, episode.ID).Fact
			db := openSQLDB(t, dbPath)
			defer db.Close()
			insertMirrorMapForFact(t, db, fact.ID, tt.mirrorID, "indexed")
			adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: tt.mirrorID, Score: 0.99, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}
			adapter.rerankItems = []memorycore.MirrorRerankItem{{NodeID: fact.ID, NodeType: "fact", RerankScore: 0.9, DebugReason: "intent requires rerank"}}

			contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
				SessionID: &sessionID,
				QueryText: tt.query,
				Policy:    memorycore.RetrievalPolicy{UseMirror: true, UseFTS: true},
			})
			if err != nil {
				t.Fatalf("retrieve: %v", err)
			}
			requireMemoryItem(t, contextResult, fact.ID, tt.summary, "")
			if adapter.rerankCalls != 1 {
				t.Fatalf("rerank calls = %d, want 1", adapter.rerankCalls)
			}
			if contextResult.Rerank == nil || contextResult.Rerank.Status != "used" {
				t.Fatalf("rerank diagnostics = %#v, want used", contextResult.Rerank)
			}
		})
	}
}

func TestServiceRetrieveForgetDeleteDoesNotInjectOperationTargetCandidateAsContext(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢薄荷茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "薄荷茶", "用户喜欢薄荷茶。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7254, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{
		TriviumNodeID:  7254,
		Score:          0.96,
		Source:         "semantic_rewrite_dense",
		PrimaryPurpose: "operation_target",
		Rank:           1,
		HitCount:       1,
	}}
	adapter.activationCandidates = []memorycore.MirrorActivationCandidate{{TriviumNodeID: 7254, Score: 0.9, Source: "graph_activation", Rank: 1}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "删除薄荷茶这条记忆",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireNoMemoryItem(t, contextResult, fact.ID)
	if adapter.activationCalls != 0 {
		t.Fatalf("activation calls = %d, want no broad graph expansion for forget/delete operation target", adapter.activationCalls)
	}
	if contextResult.Mirror == nil || len(contextResult.Mirror.Candidates) != 1 {
		t.Fatalf("mirror diagnostics = %#v, want operation target diagnostic", contextResult.Mirror)
	}
	candidate := contextResult.Mirror.Candidates[0]
	if candidate.Source != "semantic_rewrite_dense" || candidate.PrimaryPurpose != "operation_target" || candidate.DropReason != "operation_target_not_context" {
		t.Fatalf("operation target diagnostic = %#v", candidate)
	}
}

func TestServiceRetrieveForgetDeleteDoesNotInjectRawDenseMirrorCandidateAsContext(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢茉莉花茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "茉莉花茶", "用户喜欢茉莉花茶。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7255, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{
		TriviumNodeID:  7255,
		Score:          0.97,
		Source:         "raw_dense",
		PrimaryPurpose: "raw_query",
		Rank:           1,
		HitCount:       1,
	}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "删除茉莉花茶这条记忆",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireNoMemoryItem(t, contextResult, fact.ID)
	if adapter.activationCalls != 0 {
		t.Fatalf("activation calls = %d, want no graph expansion for forget/delete mirror candidates", adapter.activationCalls)
	}
	if contextResult.Mirror == nil || len(contextResult.Mirror.Candidates) != 1 {
		t.Fatalf("mirror diagnostics = %#v, want raw dense diagnostic", contextResult.Mirror)
	}
	candidate := contextResult.Mirror.Candidates[0]
	if candidate.Source != "raw_dense" || candidate.PrimaryPurpose != "raw_query" || candidate.DropReason != "forget_delete_not_context" {
		t.Fatalf("raw dense forget/delete diagnostic = %#v", candidate)
	}
	if contextResult.Mirror.Status != "forget_delete_not_context" {
		t.Fatalf("mirror status = %q, want forget_delete_not_context", contextResult.Mirror.Status)
	}
}

func TestServiceRetrieveExposesAnchorFusionDiagnosticsWithStableMirrorRank(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡和乌龙茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	first := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	second := consolidateLiteral(t, ctx, svc, userID, "likes", "乌龙茶", "用户喜欢乌龙茶。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, first.ID, 7201, "indexed")
	insertMirrorMapForFact(t, db, second.ID, 7202, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{
		{TriviumNodeID: 7201, Score: 0.10, Source: "trivium_dense"},
		{TriviumNodeID: 7202, Score: 0.99, Source: "trivium_dense"},
	}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "mirror-only",
		Policy: memorycore.RetrievalPolicy{
			UseMirror:        true,
			FinalMemoryCount: 2,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with mirror: %v", err)
	}
	if contextResult.AnchorFusion == nil {
		t.Fatalf("anchor fusion diagnostics is nil")
	}
	requirePublicAnchorSeed(t, contextResult.AnchorFusion, "fact", first.ID, "trivium_dense", 1)
	requirePublicAnchorSeed(t, contextResult.AnchorFusion, "fact", second.ID, "trivium_dense", 2)
	if contextResult.Mirror == nil || len(contextResult.Mirror.Candidates) != 2 {
		t.Fatalf("mirror diagnostics = %#v, want two candidates", contextResult.Mirror)
	}
	if contextResult.Mirror.Candidates[0].Rank != 1 || contextResult.Mirror.Candidates[1].Rank != 2 {
		t.Fatalf("mirror candidate ranks = %#v, want source order ranks 1 and 2", contextResult.Mirror.Candidates)
	}
}

func TestServiceRetrieveUseMirrorFiltersPurgedCandidate(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7001, "indexed")
	updateFactColumn(t, db, fact.ID, "visibility_status", memorycore.VisibilityPurged)
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7001, Score: 0.88, Source: "stale"}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "espresso-only",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with stale mirror: %v", err)
	}
	requireNoMemoryItem(t, contextResult, fact.ID)
	if contextResult.Mirror == nil || contextResult.Mirror.Status != "used" {
		t.Fatalf("mirror diagnostics = %#v, want used", contextResult.Mirror)
	}
	if contextResult.Mirror.DroppedCandidateCount == 0 {
		t.Fatalf("mirror dropped count = %#v, want > 0", contextResult.Mirror)
	}
	var sawAuthorityDrop bool
	for _, candidate := range contextResult.Mirror.Candidates {
		if candidate.DropReason == "dropped_by_authority_filter" && candidate.SQLiteFactID == "" {
			sawAuthorityDrop = true
		}
		if candidate.DropReason == "dropped_by_authority_filter" && candidate.SQLiteFactID == fact.ID {
			t.Fatalf("authority-dropped mirror candidate leaked sqlite fact id: %#v", candidate)
		}
	}
	if !sawAuthorityDrop {
		t.Fatalf("mirror candidates = %#v, want redacted dropped_by_authority_filter candidate", contextResult.Mirror.Candidates)
	}
}

func TestServiceRetrieveUseMirrorRedactsAuthorityDroppedSensitiveLinkedEntityCandidate(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	secretEntity, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		CanonicalName:    "SecretProject",
		EntityType:       memorycore.EntityTypeConcept,
		SensitivityLevel: memorycore.SensitivitySensitive,
	})
	if err != nil {
		t.Fatalf("ensure secret entity: %v", err)
	}
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我参与过 SecretProject。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	result, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  userID,
			Predicate:        "likes",
			ObjectEntityID:   &secretEntity.ID,
			ContentSummary:   "用户参与过一个项目。",
			SourceEpisodeIDs: []string{episode.ID},
			Confidence:       memorycore.ConfidenceExplicit,
			Importance:       0.7,
			Sensitivity:      memorycore.SensitivityNormal,
		},
	})
	if err != nil {
		t.Fatalf("consolidate linked entity fact: %v", err)
	}
	if result.Fact == nil {
		t.Fatalf("result fact is nil: %#v", result)
	}
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, result.Fact.ID, 7101, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7101, Score: 0.91, Source: "sensitive_link"}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "mirror-only",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with sensitive linked mirror candidate: %v", err)
	}
	requireNoMemoryItem(t, contextResult, result.Fact.ID)
	if contextResult.Mirror == nil || contextResult.Mirror.DroppedCandidateCount == 0 {
		t.Fatalf("mirror diagnostics = %#v, want authority drop", contextResult.Mirror)
	}
	for _, candidate := range contextResult.Mirror.Candidates {
		if candidate.DropReason == "dropped_by_authority_filter" && candidate.SQLiteFactID != "" {
			t.Fatalf("authority-dropped sensitive linked candidate leaked sqlite fact id: %#v", candidate)
		}
	}
}

func TestServiceRetrieveUseMirrorFallsBackWhenAdapterFails(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{err: sql.ErrConnDone}
	svc, _ := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with failing mirror: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")
}

func TestServiceRetrieveUseMirrorFallsBackWhenMirrorDegraded(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{degraded: true, fallbackReason: "provider_budget_exhausted"}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, fact.ID, 7001, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7001, Score: 0.88, Source: "degraded"}}

	for _, reason := range []string{"provider_budget_exhausted", "sidecar_provider_timeout"} {
		adapter.fallbackReason = reason
		contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
			SessionID: &sessionID,
			QueryText: "espresso-only",
			Policy: memorycore.RetrievalPolicy{
				UseMirror: true,
			},
		})
		if err != nil {
			t.Fatalf("retrieve with degraded mirror: %v", err)
		}
		requireNoMemoryItem(t, contextResult, fact.ID)
		if contextResult.Mirror == nil || contextResult.Mirror.FallbackReason != reason {
			t.Fatalf("mirror diagnostics = %#v, want %s fallback", contextResult.Mirror, reason)
		}
	}
}

func TestServiceRetrieveGraphActivationAddsAuthorityValidatedCandidate(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡，但早会让我焦虑。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	seed := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	related := consolidateLiteral(t, ctx, svc, userID, "dislikes", "早会", "用户不喜欢早会。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, related.ID, "importance", 1.0)
	insertMirrorMapForFact(t, db, seed.ID, 8101, "indexed")
	insertMirrorMapForFact(t, db, related.ID, 8102, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 8101, Score: 0.9, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}
	adapter.activationCandidates = []memorycore.MirrorActivationCandidate{
		{
			TriviumNodeID: 8102,
			Score:         0.77,
			Source:        "graph_activation",
			Rank:          1,
			Paths: []memorycore.MirrorActivationPath{
				{TriviumNodeIDs: []int64{8101, 8102}, LinkTypes: []string{"CAUSED_BY"}},
			},
		},
	}
	adapter.rerankItems = []memorycore.MirrorRerankItem{{NodeID: related.ID, NodeType: "fact", RerankScore: 1.0, DebugReason: "graph candidate relevant"}}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy: memorycore.RetrievalPolicy{
			UseMirror:        true,
			FinalMemoryCount: 2,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with graph activation: %v", err)
	}
	requireMemoryItem(t, contextResult, seed.ID, "用户喜欢咖啡。", "")
	requireMemoryItem(t, contextResult, related.ID, "用户不喜欢早会。", "")
	if adapter.activationCalls != 1 {
		t.Fatalf("activation calls = %d, want 1", adapter.activationCalls)
	}
	if len(adapter.lastActivationRequest.Seeds) == 0 || adapter.lastActivationRequest.Seeds[0].TriviumNodeID != 8101 {
		t.Fatalf("activation seeds = %#v, want mapped seed 8101", adapter.lastActivationRequest.Seeds)
	}
	if contextResult.GraphActivation == nil || contextResult.GraphActivation.Status != "used" {
		t.Fatalf("graph activation diagnostics = %#v, want used", contextResult.GraphActivation)
	}
	requirePublicGraphActivationCandidate(t, contextResult.GraphActivation, related.ID, "graph_activation", 1)
}

func TestServiceRetrieveGraphActivationUsesConfiguredBudgetParams(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorServiceWithOptions(t, ctx, adapter, memorycore.SidecarResilienceOptions{
		ActivationBudget: memorycore.SidecarActivationBudgetOptions{
			MaxEdgesScannedPerRequest: 321,
			MaxNeighborsPerNode:       17,
			MaxActivationWall:         99 * time.Millisecond,
		},
	})
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	seed := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, seed.ID, 8401, "indexed")

	_, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	params := adapter.lastActivationRequest.Params
	if params.MaxEdgesScannedPerRequest != 321 || params.MaxNeighborsPerNode != 17 || params.MaxActivationWallMs != 99 {
		t.Fatalf("activation params = %#v, want configured budget values", params)
	}
}

func TestServiceRetrieveGraphActivationFallsBackWhenAdapterFails(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{activationErr: sql.ErrConnDone}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	seed := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, seed.ID, 8201, "indexed")

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with failing graph activation: %v", err)
	}
	requireMemoryItem(t, contextResult, seed.ID, "用户喜欢咖啡。", "")
	if contextResult.GraphActivation == nil || contextResult.GraphActivation.Status != "sidecar_error" {
		t.Fatalf("graph activation diagnostics = %#v, want sidecar_error", contextResult.GraphActivation)
	}
}

func TestServiceRetrieveGraphActivationRedactsAuthorityDroppedCandidate(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡，但早会让我焦虑。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	seed := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	hidden := consolidateLiteral(t, ctx, svc, userID, "dislikes", "早会", "用户不喜欢早会。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	insertMirrorMapForFact(t, db, seed.ID, 8301, "indexed")
	insertMirrorMapForFact(t, db, hidden.ID, 8302, "indexed")
	updateFactColumn(t, db, hidden.ID, "visibility_status", memorycore.VisibilityPurged)
	adapter.activationCandidates = []memorycore.MirrorActivationCandidate{
		{TriviumNodeID: 8302, Score: 0.91, Source: "graph_activation", Rank: 1},
	}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy: memorycore.RetrievalPolicy{
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with graph activation authority drop: %v", err)
	}
	requireNoMemoryItem(t, contextResult, hidden.ID)
	if contextResult.GraphActivation == nil || contextResult.GraphActivation.DroppedCandidateCount == 0 {
		t.Fatalf("graph activation diagnostics = %#v, want dropped candidate", contextResult.GraphActivation)
	}
	for _, candidate := range contextResult.GraphActivation.Candidates {
		if candidate.DropReason == "dropped_by_authority_filter" && candidate.SQLiteNodeID != "" {
			t.Fatalf("authority-dropped graph activation leaked sqlite node id: %#v", candidate)
		}
	}
}

func TestServiceRetrieveRerankReceivesOnlySafeSummaries(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "RAW_EPISODE_SECRET coffee detail", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	visible := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	hidden := consolidateLiteral(t, ctx, svc, userID, "likes", "隐藏咖啡", "用户隐藏喜欢咖啡。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, hidden.ID, "visibility_status", memorycore.VisibilityHidden)
	adapter.rerankItems = []memorycore.MirrorRerankItem{
		{NodeID: visible.ID, NodeType: "fact", RerankScore: 1.0, DebugReason: "safe summary match"},
	}

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡是什么时候告诉我的\n" + strings.Repeat("x", 220) + " RAW_PROMPT_SUFFIX",
		Policy: memorycore.RetrievalPolicy{
			UseFTS:    true,
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with rerank: %v", err)
	}
	if adapter.rerankCalls != 1 {
		t.Fatalf("rerank calls = %d, want 1", adapter.rerankCalls)
	}
	if !strings.Contains(adapter.lastRerankRequest.QueryText, "query=咖啡") {
		t.Fatalf("rerank query = %q, want capped normalized query surface", adapter.lastRerankRequest.QueryText)
	}
	if !strings.Contains(adapter.lastRerankRequest.QueryText, "memory_ability=provenance") ||
		!strings.Contains(adapter.lastRerankRequest.QueryText, "evidence_need=provenance_source") {
		t.Fatalf("rerank query = %q, want retrieval intent labels", adapter.lastRerankRequest.QueryText)
	}
	if strings.Contains(adapter.lastRerankRequest.QueryText, "RAW_PROMPT_SUFFIX") || strings.Contains(adapter.lastRerankRequest.QueryText, "\n") {
		t.Fatalf("rerank query exceeded sanitized bounded surface: %q", adapter.lastRerankRequest.QueryText)
	}
	if len(adapter.lastRerankRequest.Candidates) != 1 {
		t.Fatalf("rerank candidates = %#v, want one safe candidate", adapter.lastRerankRequest.Candidates)
	}
	candidate := adapter.lastRerankRequest.Candidates[0]
	if candidate.NodeID != visible.ID || candidate.SafeSummary != "用户喜欢咖啡。" {
		t.Fatalf("rerank candidate = %#v, want visible safe summary only", candidate)
	}
	if strings.Contains(candidate.SafeSummary, "RAW_EPISODE_SECRET") || candidate.NodeID == hidden.ID {
		t.Fatalf("rerank input leaked unsafe content/candidate: %#v", candidate)
	}
	requireMemoryItem(t, contextResult, visible.ID, "用户喜欢咖啡。", "")
	requireNoMemoryItem(t, contextResult, hidden.ID)
	if contextResult.Rerank == nil || contextResult.Rerank.Status != "used" || contextResult.Rerank.SafeCandidateCount != 1 {
		t.Fatalf("rerank diagnostics = %#v, want used with one safe candidate", contextResult.Rerank)
	}
}

func TestServiceRetrieveRerankFallsBackWhenAdapterFails(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{rerankErr: sql.ErrConnDone}
	svc, _ := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	fact := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact

	contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡是什么时候告诉我的",
		Policy: memorycore.RetrievalPolicy{
			UseFTS:    true,
			UseMirror: true,
		},
	})
	if err != nil {
		t.Fatalf("retrieve with failing rerank: %v", err)
	}
	requireMemoryItem(t, contextResult, fact.ID, "用户喜欢咖啡。", "")
	if contextResult.Rerank == nil || contextResult.Rerank.Status != "sidecar_error" {
		t.Fatalf("rerank diagnostics = %#v, want sidecar_error", contextResult.Rerank)
	}
}

func TestServiceRetrieveUseMirrorFallsBackWhenPersonaMirrorStateNotReady(t *testing.T) {
	for _, state := range []string{"rebuilding", "degraded"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			adapter := &retrievalMirrorAdapter{}
			svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
			defer svc.Close()

			sessionID, userID := seedConsolidationSubject(t, ctx, svc)
			episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡和乌龙茶。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
			coffee := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
			tea := consolidateLiteral(t, ctx, svc, userID, "likes", "乌龙茶", "用户喜欢乌龙茶。", episode.ID).Fact
			db := openSQLDB(t, dbPath)
			defer db.Close()
			insertMirrorMapForFact(t, db, tea.ID, 7002, "indexed")
			setMirrorPersonaState(t, db, "default", state)
			adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 7002, Score: 0.99, Source: "mirror_only"}}

			contextResult, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
				SessionID: &sessionID,
				QueryText: "咖啡",
				Policy: memorycore.RetrievalPolicy{
					UseMirror: true,
				},
			})
			if err != nil {
				t.Fatalf("retrieve with %s persona mirror state: %v", state, err)
			}
			if adapter.calls != 0 {
				t.Fatalf("mirror adapter calls = %d, want 0 for state %s", adapter.calls, state)
			}
			if contextResult.Mirror == nil || contextResult.Mirror.Status != "persona_not_ready" {
				t.Fatalf("mirror diagnostics = %#v, want persona_not_ready", contextResult.Mirror)
			}
			requireMemoryItem(t, contextResult, coffee.ID, "用户喜欢咖啡。", "")
			requireNoMemoryItem(t, contextResult, tea.ID)
		})
	}
}

func TestServiceRetrieveMirrorDiagnosticsDisabledByConfig(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{}
	svc, _ := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{QueryText: "coffee"})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if result.Mirror == nil || result.Mirror.Status != "disabled_by_config" {
		t.Fatalf("mirror diagnostics = %#v, want disabled_by_config", result.Mirror)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter calls = %d, want 0", adapter.calls)
	}
}

func TestServiceRetrieveMirrorDiagnosticsAdapterMissing(t *testing.T) {
	ctx := context.Background()
	svc, _ := openRetrievalMirrorService(t, ctx, nil)
	defer svc.Close()

	result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "coffee",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if result.Mirror == nil || result.Mirror.Status != "adapter_missing" {
		t.Fatalf("mirror diagnostics = %#v, want adapter_missing", result.Mirror)
	}
}

func TestServiceRetrieveMirrorDiagnosticsSidecarErrorAndDegradedAndNoCandidates(t *testing.T) {
	t.Run("sidecar_error", func(t *testing.T) {
		ctx := context.Background()
		adapter := &retrievalMirrorAdapter{err: sql.ErrConnDone}
		svc, _ := openRetrievalMirrorService(t, ctx, adapter)
		defer svc.Close()

		result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
			QueryText: "coffee",
			Policy:    memorycore.RetrievalPolicy{UseMirror: true},
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if result.Mirror == nil || result.Mirror.Status != "sidecar_error" {
			t.Fatalf("mirror diagnostics = %#v, want sidecar_error", result.Mirror)
		}
	})

	t.Run("sidecar_degraded", func(t *testing.T) {
		ctx := context.Background()
		adapter := &retrievalMirrorAdapter{degraded: true}
		svc, _ := openRetrievalMirrorService(t, ctx, adapter)
		defer svc.Close()

		result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
			QueryText: "coffee",
			Policy:    memorycore.RetrievalPolicy{UseMirror: true},
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if result.Mirror == nil || result.Mirror.Status != "sidecar_degraded" {
			t.Fatalf("mirror diagnostics = %#v, want sidecar_degraded", result.Mirror)
		}
	})

	t.Run("no_candidates", func(t *testing.T) {
		ctx := context.Background()
		adapter := &retrievalMirrorAdapter{}
		svc, _ := openRetrievalMirrorService(t, ctx, adapter)
		defer svc.Close()

		result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
			QueryText: "coffee",
			Policy:    memorycore.RetrievalPolicy{UseMirror: true},
		})
		if err != nil {
			t.Fatalf("retrieve: %v", err)
		}
		if result.Mirror == nil || result.Mirror.Status != "no_candidates" {
			t.Fatalf("mirror diagnostics = %#v, want no_candidates", result.Mirror)
		}
	})
}

func TestServiceRetrieveMarksStageTimeoutWithoutTopLevelError(t *testing.T) {
	ctx := context.Background()
	adapter := &blockingRetrievalMirrorAdapter{}
	svc, _ := openRetrievalMirrorServiceWithOptions(t, ctx, adapter, memorycore.SidecarResilienceOptions{
		Timeouts: memorycore.SidecarStageTimeouts{
			Total:  50 * time.Millisecond,
			Mirror: 5 * time.Millisecond,
		},
		Breaker: memorycore.SidecarBreakerOptions{Mode: memorycore.SidecarBreakerModeDisabled},
	})
	defer svc.Close()

	result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		QueryText: "coffee",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if result.Mirror == nil || result.Mirror.Status != "sidecar_timeout" {
		t.Fatalf("mirror diagnostics = %#v, want sidecar_timeout", result.Mirror)
	}
	if !result.Mirror.Degraded || result.Mirror.FallbackReason != "sidecar_timeout" {
		t.Fatalf("mirror degradation = %#v, want timeout fallback", result.Mirror)
	}
	if result.Mirror.LatencyMs <= 0 {
		t.Fatalf("latency_ms = %d, want positive", result.Mirror.LatencyMs)
	}
}

func TestServiceRetrieveParentCancellationIsNotSidecarTimeout(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	adapter := &blockingRetrievalMirrorAdapter{}
	svc, _ := openRetrievalMirrorServiceWithOptions(t, context.Background(), adapter, memorycore.SidecarResilienceOptions{
		Breaker: memorycore.SidecarBreakerOptions{Mode: memorycore.SidecarBreakerModeEnabled, FailureThreshold: 1},
	})
	defer svc.Close()

	_, err := svc.Retrieve(parent, memorycore.RetrievalRequest{
		QueryText: "coffee",
		Policy:    memorycore.RetrievalPolicy{UseMirror: true},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retrieve error = %v, want context.Canceled", err)
	}
}

func TestServiceRetrieveBreakerDisabledDoesNotOpen(t *testing.T) {
	ctx := context.Background()
	adapter := &blockingRetrievalMirrorAdapter{}
	svc, _ := openRetrievalMirrorServiceWithOptions(t, ctx, adapter, memorycore.SidecarResilienceOptions{
		Timeouts: memorycore.SidecarStageTimeouts{Total: 30 * time.Millisecond, Mirror: 5 * time.Millisecond},
		Breaker:  memorycore.SidecarBreakerOptions{Mode: memorycore.SidecarBreakerModeDisabled},
	})
	defer svc.Close()

	for i := 0; i < 2; i++ {
		result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
			QueryText: "coffee",
			Policy:    memorycore.RetrievalPolicy{UseMirror: true},
		})
		if err != nil {
			t.Fatalf("retrieve %d: %v", i, err)
		}
		if result.Mirror == nil || result.Mirror.Status != "sidecar_timeout" {
			t.Fatalf("retrieve %d mirror = %#v, want sidecar_timeout", i, result.Mirror)
		}
	}
	if adapter.mirrorCalls != 2 {
		t.Fatalf("mirror calls = %d, want 2 with disabled breaker", adapter.mirrorCalls)
	}
}

func TestServiceRetrieveGraphActivationBudgetDegradedKeepsPartialCandidates(t *testing.T) {
	ctx := context.Background()
	adapter := &retrievalMirrorAdapter{activationDegraded: true}
	svc, dbPath := openRetrievalMirrorService(t, ctx, adapter)
	defer svc.Close()

	sessionID, userID := seedConsolidationSubject(t, ctx, svc)
	episode := appendConsolidationEpisode(t, ctx, svc, sessionID, "我喜欢咖啡，也不喜欢早会。", time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC))
	seed := consolidateLiteral(t, ctx, svc, userID, "likes", "咖啡", "用户喜欢咖啡。", episode.ID).Fact
	partial := consolidateLiteral(t, ctx, svc, userID, "dislikes", "早会", "用户不喜欢早会。", episode.ID).Fact
	db := openSQLDB(t, dbPath)
	defer db.Close()
	updateFactColumn(t, db, partial.ID, "importance", 1.0)
	insertMirrorMapForFact(t, db, seed.ID, 9101, "indexed")
	insertMirrorMapForFact(t, db, partial.ID, 9102, "indexed")
	adapter.candidates = []memorycore.MirrorCandidate{{TriviumNodeID: 9101, Score: 0.9, Source: "raw_dense", PrimaryPurpose: "raw_query", Rank: 1}}
	adapter.activationFallbackReason = "activation_budget_exceeded"
	adapter.activationCandidates = []memorycore.MirrorActivationCandidate{
		{TriviumNodeID: 9102, Score: 0.77, Source: "graph_activation", Rank: 1},
	}
	adapter.rerankItems = []memorycore.MirrorRerankItem{{NodeID: partial.ID, NodeType: "fact", RerankScore: 1.0, DebugReason: "partial graph candidate relevant"}}

	result, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: "咖啡",
		Policy: memorycore.RetrievalPolicy{
			UseMirror:        true,
			FinalMemoryCount: 2,
		},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	requireMemoryItem(t, result, partial.ID, "用户不喜欢早会。", "")
	if result.GraphActivation == nil || !result.GraphActivation.Degraded || result.GraphActivation.FallbackReason != "activation_budget_exceeded" {
		t.Fatalf("graph diagnostics = %#v, want budget degraded", result.GraphActivation)
	}
}

type retrievalMirrorAdapter struct {
	candidates               []memorycore.MirrorCandidate
	activationCandidates     []memorycore.MirrorActivationCandidate
	rerankItems              []memorycore.MirrorRerankItem
	err                      error
	fallbackReason           string
	activationErr            error
	activationFallbackReason string
	rerankErr                error
	rerankFallbackReason     string
	degraded                 bool
	activationDegraded       bool
	rerankDegraded           bool
	calls                    int
	activationCalls          int
	rerankCalls              int
	lastCandidateRequest     memorycore.MirrorCandidateRequest
	candidateRequests        []memorycore.MirrorCandidateRequest
	lastActivationRequest    memorycore.MirrorActivationRequest
	lastRerankRequest        memorycore.MirrorRerankRequest
}

func (a *retrievalMirrorAdapter) FindCandidates(ctx context.Context, req memorycore.MirrorCandidateRequest) (*memorycore.MirrorCandidateResult, error) {
	a.calls++
	a.lastCandidateRequest = req
	a.candidateRequests = append(a.candidateRequests, req)
	if a.err != nil {
		return nil, a.err
	}
	return &memorycore.MirrorCandidateResult{Candidates: append([]memorycore.MirrorCandidate(nil), a.candidates...), Degraded: a.degraded, FallbackReason: a.fallbackReason}, nil
}

func (a *retrievalMirrorAdapter) ActivateGraph(ctx context.Context, req memorycore.MirrorActivationRequest) (*memorycore.MirrorActivationResult, error) {
	a.activationCalls++
	a.lastActivationRequest = req
	if a.activationErr != nil {
		return nil, a.activationErr
	}
	return &memorycore.MirrorActivationResult{
		Candidates:     append([]memorycore.MirrorActivationCandidate(nil), a.activationCandidates...),
		Degraded:       a.activationDegraded,
		FallbackReason: a.activationFallbackReason,
	}, nil
}

func (a *retrievalMirrorAdapter) Rerank(ctx context.Context, req memorycore.MirrorRerankRequest) (*memorycore.MirrorRerankResult, error) {
	a.rerankCalls++
	a.lastRerankRequest = req
	if a.rerankErr != nil {
		return nil, a.rerankErr
	}
	return &memorycore.MirrorRerankResult{
		Items:          append([]memorycore.MirrorRerankItem(nil), a.rerankItems...),
		Degraded:       a.rerankDegraded,
		FallbackReason: a.rerankFallbackReason,
	}, nil
}

func (a *retrievalMirrorAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	return memorycore.MirrorNodeUpsertResult{}, nil
}

func (a *retrievalMirrorAdapter) DeleteNode(ctx context.Context, ref memorycore.MirrorNodeRef) error {
	return nil
}

func (a *retrievalMirrorAdapter) UpsertEdge(ctx context.Context, payload memorycore.MirrorEdgePayload) error {
	return nil
}

func (a *retrievalMirrorAdapter) DeleteEdge(ctx context.Context, ref memorycore.MirrorEdgeRef) error {
	return nil
}

func openRetrievalMirrorService(t *testing.T, ctx context.Context, adapter memorycore.MirrorAdapter) (memorycore.Service, string) {
	t.Helper()
	return openRetrievalMirrorServiceWithOptions(t, ctx, adapter, memorycore.SidecarResilienceOptions{})
}

func openRetrievalMirrorServiceWithOptions(t *testing.T, ctx context.Context, adapter memorycore.MirrorAdapter, resilience memorycore.SidecarResilienceOptions) (memorycore.Service, string) {
	return openRetrievalMirrorServiceWithAllOptions(t, ctx, adapter, resilience, memorycore.QueryAnalysisOptions{})
}

func openRetrievalMirrorServiceWithQueryAnalysisOptions(t *testing.T, ctx context.Context, adapter memorycore.MirrorAdapter, queryAnalysis memorycore.QueryAnalysisOptions) (memorycore.Service, string) {
	return openRetrievalMirrorServiceWithAllOptions(t, ctx, adapter, memorycore.SidecarResilienceOptions{}, queryAnalysis)
}

func openRetrievalMirrorServiceWithAllOptions(t *testing.T, ctx context.Context, adapter memorycore.MirrorAdapter, resilience memorycore.SidecarResilienceOptions, queryAnalysis memorycore.QueryAnalysisOptions) (memorycore.Service, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:            dbPath,
		AutoMigrate:       true,
		MirrorAdapter:     adapter,
		QueryAnalysis:     queryAnalysis,
		SidecarResilience: resilience,
		Now: func() time.Time {
			return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	return svc, dbPath
}

type blockingRetrievalMirrorAdapter struct {
	retrievalMirrorAdapter
	mirrorCalls     int
	activationCalls int
	rerankCalls     int
}

func (a *blockingRetrievalMirrorAdapter) FindCandidates(ctx context.Context, req memorycore.MirrorCandidateRequest) (*memorycore.MirrorCandidateResult, error) {
	a.mirrorCalls++
	<-ctx.Done()
	return nil, ctx.Err()
}

func (a *blockingRetrievalMirrorAdapter) ActivateGraph(ctx context.Context, req memorycore.MirrorActivationRequest) (*memorycore.MirrorActivationResult, error) {
	a.activationCalls++
	<-ctx.Done()
	return nil, ctx.Err()
}

func (a *blockingRetrievalMirrorAdapter) Rerank(ctx context.Context, req memorycore.MirrorRerankRequest) (*memorycore.MirrorRerankResult, error) {
	a.rerankCalls++
	<-ctx.Done()
	return nil, ctx.Err()
}

func insertMirrorMapForFact(t *testing.T, db *sql.DB, factID string, triviumNodeID int64, status string) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES (?, 'default', 'fact', ?, ?, ?)`, "map_"+factID, factID, triviumNodeID, status); err != nil {
		t.Fatalf("insert mirror map: %v", err)
	}
}

func setMirrorPersonaState(t *testing.T, db *sql.DB, personaID string, state string) {
	t.Helper()
	if _, err := db.Exec(`
INSERT INTO mirror_persona_state (persona_id, state, reason, updated_at)
VALUES (?, ?, 'test state', CURRENT_TIMESTAMP)
ON CONFLICT(persona_id) DO UPDATE SET
    state = excluded.state,
    reason = excluded.reason,
    updated_at = excluded.updated_at`, personaID, state); err != nil {
		t.Fatalf("set mirror persona state: %v", err)
	}
}

func requireMemoryItem(t *testing.T, contextResult *memorycore.MemoryContext, nodeID string, summary string, guidanceContains string) {
	t.Helper()

	for _, block := range contextResult.Blocks {
		for _, item := range block.Items {
			if item.NodeID != nodeID {
				continue
			}
			if item.Summary != summary {
				t.Fatalf("item %s summary = %q, want %q", nodeID, item.Summary, summary)
			}
			if guidanceContains != "" && !strings.Contains(item.UsageGuidance, guidanceContains) {
				t.Fatalf("item %s guidance = %q, want contains %q", nodeID, item.UsageGuidance, guidanceContains)
			}
			return
		}
	}
	t.Fatalf("memory item %s not found in %#v", nodeID, contextResult)
}

func requirePublicBlock(t *testing.T, contextResult *memorycore.MemoryContext, blockType string) memorycore.MemoryBlock {
	t.Helper()

	for _, block := range contextResult.Blocks {
		if block.BlockType == blockType {
			return block
		}
	}
	t.Fatalf("block %q not found in %#v", blockType, contextResult.Blocks)
	return memorycore.MemoryBlock{}
}

func requirePublicBlockItem(t *testing.T, block memorycore.MemoryBlock, nodeID string) memorycore.MemoryContextItem {
	t.Helper()

	for _, item := range block.Items {
		if item.NodeID == nodeID {
			return item
		}
	}
	t.Fatalf("item %q not found in %#v", nodeID, block.Items)
	return memorycore.MemoryContextItem{}
}

func requireNoMemoryItem(t *testing.T, contextResult *memorycore.MemoryContext, nodeID string) {
	t.Helper()

	for _, block := range contextResult.Blocks {
		for _, item := range block.Items {
			if item.NodeID == nodeID {
				t.Fatalf("memory item %s found in %#v", nodeID, contextResult)
			}
		}
	}
}

func insertPublicFactLink(t *testing.T, db *sql.DB, linkID string, fromFactID string, linkType string, toFactID string) {
	t.Helper()

	if _, err := db.Exec(`
INSERT INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    created_by, visibility_status, searchable
) VALUES (?, 'default', 'fact', ?, ?, 'fact', ?, 'forward', 1, 1, 'system', 'visible', 1)`,
		linkID,
		fromFactID,
		linkType,
		toFactID,
	); err != nil {
		t.Fatalf("insert public fact link %s: %v", linkID, err)
	}
}

func requirePublicAnchorSeed(t *testing.T, diagnostics *memorycore.AnchorFusionDiagnostics, nodeType string, nodeID string, source string, rank int) {
	t.Helper()

	for _, seed := range diagnostics.Seeds {
		if seed.NodeType != nodeType || seed.NodeID != nodeID {
			continue
		}
		if seed.FusedAnchorScore <= 0 || seed.SeedEnergy <= 0 {
			t.Fatalf("seed %#v has non-positive fused score or energy", seed)
		}
		for _, breakdown := range seed.SourceBreakdown {
			if breakdown.Source == source && breakdown.Rank == rank {
				return
			}
		}
		t.Fatalf("seed %#v missing source=%s rank=%d", seed, source, rank)
	}
	t.Fatalf("anchor seed %s/%s not found in %#v", nodeType, nodeID, diagnostics)
}

func requirePublicGraphActivationCandidate(t *testing.T, diagnostics *memorycore.GraphActivationDiagnostics, nodeID string, source string, rank int) {
	t.Helper()

	for _, candidate := range diagnostics.Candidates {
		if candidate.SQLiteNodeID == nodeID && candidate.Source == source && candidate.Rank == rank {
			if candidate.Score <= 0 {
				t.Fatalf("graph activation candidate %#v has non-positive score", candidate)
			}
			return
		}
	}
	t.Fatalf("graph activation candidate %s source=%s rank=%d not found in %#v", nodeID, source, rank, diagnostics)
}

func TestRetrievalQuerySignalPreciseConstantsArePublic(t *testing.T) {
	for _, signal := range []memorycore.QuerySignal{
		memorycore.QuerySignalPastEventDirectFact,
		memorycore.QuerySignalStateTransition,
		memorycore.QuerySignalProvenanceSource,
		memorycore.QuerySignalCausalChain,
		memorycore.QuerySignalPremiseCounterexample,
		memorycore.QuerySignalEventBundle,
		memorycore.QuerySignalReflectionSummary,
		memorycore.QuerySignalExactFact,
	} {
		if signal == "" {
			t.Fatalf("query signal constant is empty")
		}
	}
}

func hasQuerySignal(signals []memorycore.QuerySignal, want memorycore.QuerySignal) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}

func requireAccessEvent(t *testing.T, db *sql.DB, factID string, accessType string) {
	t.Helper()

	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM memory_access_events
WHERE node_type = 'fact' AND node_id = ? AND access_type = ?`, factID, accessType).Scan(&count); err != nil {
		t.Fatalf("count access events: %v", err)
	}
	if count == 0 {
		t.Fatalf("access event for %s/%s not found", factID, accessType)
	}
}

func requireAccessEventBreakdown(t *testing.T, db *sql.DB, factID string, accessType string) map[string]any {
	t.Helper()

	var raw string
	if err := db.QueryRow(`
SELECT score_breakdown_json
FROM memory_access_events
WHERE node_type = 'fact' AND node_id = ? AND access_type = ?
ORDER BY created_at DESC, id DESC
LIMIT 1`, factID, accessType).Scan(&raw); err != nil {
		t.Fatalf("load access event breakdown: %v", err)
	}
	var breakdown map[string]any
	if err := json.Unmarshal([]byte(raw), &breakdown); err != nil {
		t.Fatalf("decode score_breakdown_json %q: %v", raw, err)
	}
	return breakdown
}

func requireBreakdownObject(t *testing.T, breakdown map[string]any, key string) map[string]any {
	t.Helper()

	value, ok := breakdown[key].(map[string]any)
	if !ok {
		t.Fatalf("breakdown[%s] = %#v, want object", key, breakdown[key])
	}
	return value
}

func updateFactColumn(t *testing.T, db *sql.DB, factID string, column string, value any) {
	t.Helper()

	switch column {
	case "visibility_status", "searchable", "lifecycle_status", "validity_status", "sensitivity_level", "importance":
	default:
		t.Fatalf("unsupported fact column %q", column)
	}
	if _, err := db.Exec("UPDATE facts SET "+column+" = ? WHERE id = ?", value, factID); err != nil {
		t.Fatalf("update fact %s.%s: %v", factID, column, err)
	}
}

func updateEntityColumn(t *testing.T, db *sql.DB, entityID string, column string, value any) {
	t.Helper()

	switch column {
	case "visibility_status", "searchable", "sensitivity_level":
	default:
		t.Fatalf("unsupported entity column %q", column)
	}
	if _, err := db.Exec("UPDATE entities SET "+column+" = ? WHERE id = ?", value, entityID); err != nil {
		t.Fatalf("update entity %s.%s: %v", entityID, column, err)
	}
}
