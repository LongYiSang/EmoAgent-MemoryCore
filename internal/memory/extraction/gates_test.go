package extraction_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
	"github.com/longyisang/emoagent-memorycore/internal/memory/extraction"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestValidateExtractionHardRulesAndRouting(t *testing.T) {
	req := validRequest(t)
	resp := validResponse(t)

	resp.RequestID = "other"
	gate := extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.ResponseDecisions, "response", "reject", "request_id_mismatch")

	resp = validResponse(t)
	resp.SessionID = nil
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.ResponseDecisions, "response", "reject", "session_id_mismatch")

	resp = validResponse(t)
	resp.Facts[0].SourceEpisodeIDs = []string{"ep_other"}
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.FactDecisions, "f1", "reject", "source_episode_not_in_request")

	resp = validResponse(t)
	resp.Facts[0].Predicate = "unknown_predicate"
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.FactDecisions, "f1", "needs_review", "unknown_predicate")

	resp = validResponse(t)
	resp.Links = []memorycore.ExtractedLinkCandidate{{
		CandidateID:     "link_evidence",
		FromCandidateID: "f1",
		ToCandidateID:   "f1",
		LinkType:        "EVIDENCED_BY",
		Confidence:      0.9,
	}}
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.LinkDecisions, "link_evidence", "reject", "invalid_link_type")

	req = validRequest(t)
	label := "居住地"
	factType := memorycore.FactTypeCoreIdentity
	req.PredicateSchemas = append(req.PredicateSchemas, memorycore.ExtractionPredicateSchema{
		Predicate:         "lives_in",
		CanonicalLabel:    &label,
		DefaultFactType:   &factType,
		Cardinality:       "single",
		ConflictPolicy:    "supersede",
		TemporalBehavior:  "state",
		ObjectKind:        "entity",
		DefaultImportance: 0.8,
		AllowInference:    true,
	})
	resp = validResponse(t)
	resp.Facts[0].Predicate = "lives_in"
	resp.Facts[0].ObjectLiteral = stringPtr("新加坡")
	resp.Facts[0].ObjectEntityCandidateID = nil
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.FactDecisions, "f1", "needs_review", "object_entity_required")

	resp = validResponse(t)
	resp.Facts[0].SensitivityLevel = memorycore.SensitivityHighlySensitive
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.FactDecisions, "f1", "needs_review", "highly_sensitive_requires_review")

	resp = validResponse(t)
	resp.Facts[0].QualityDecision = "garbage"
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.FactDecisions, "f1", "reject", "invalid_quality_decision")

	req.Policy.AllowInference = false
	resp = validResponse(t)
	resp.Facts[0].ExtractionConfidence = memorycore.ConfidenceInferred
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.FactDecisions, "f1", "needs_review", "inference_not_allowed")

	req.Trigger = memorycore.ExtractionTriggerManualForget
	resp = validResponse(t)
	resp.Trigger = memorycore.ExtractionTriggerManualForget
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.FactDecisions, "f1", "reject", "manual_forget_fact_rejected")

	req = validRequest(t)
	resp = validResponse(t)
	resp.DeletionIntents = []memorycore.ExtractedDeletionIntent{{
		CandidateID:       "d1",
		ForgetLevel:       "hard_forget",
		TargetDescription: "早上八点开会",
		SourceEpisodeID:   "ep_seed",
		Confidence:        0.9,
	}}
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.DeletionIntentDecisions, "d1", "route_only", "route_to_forget_manager")

	resp = validResponse(t)
	resp.AffectEvents = []memorycore.ExtractedAffectEventCandidate{{
		CandidateID:      "a1",
		Scope:            "agent",
		SourceEpisodeIDs: []string{"ep_seed"},
		Confidence:       0.9,
	}}
	gate = extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.AffectEventDecisions, "a1", "reject", "agent_affect_boundary")
}

func TestValidateExtractionAdmissionRules(t *testing.T) {
	cases := []struct {
		name         string
		content      string
		role         string
		sourceType   string
		modify       func(*memorycore.ExtractionRequest, *memorycore.ExtractedFactCandidate)
		wantDecision string
		wantReason   string
	}{
		{
			name:         "hypothetical scenario is not a fact",
			content:      "如果我以后搬去东京，你要提醒我整理资料。",
			wantDecision: "reject",
			wantReason:   "hypothetical_scenario",
			modify: func(req *memorycore.ExtractionRequest, fact *memorycore.ExtractedFactCandidate) {
				fact.Predicate = "dislikes"
				fact.ObjectLiteral = stringPtr("东京")
				fact.ContentSummary = "用户住在东京。"
			},
		},
		{
			name:         "assistant guess is rejected",
			content:      "你可能是不喜欢早会。",
			role:         memorycore.RoleAssistant,
			wantDecision: "reject",
			wantReason:   "assistant_speculation_not_user_fact",
		},
		{
			name:         "assistant suggestion is rejected",
			content:      "你可以试试周末运动。",
			role:         memorycore.RoleAssistant,
			wantDecision: "reject",
			wantReason:   "assistant_suggestion_not_user_fact",
			modify: func(req *memorycore.ExtractionRequest, fact *memorycore.ExtractedFactCandidate) {
				fact.ObjectLiteral = stringPtr("周末运动")
				fact.ContentSummary = "用户适合周末运动。"
			},
		},
		{
			name:         "tool noise is rejected",
			content:      "search result: npm install failed with stack trace",
			role:         memorycore.RoleToolSummary,
			sourceType:   memorycore.SourceTypePlugin,
			wantDecision: "reject",
			wantReason:   "tool_noise",
		},
		{
			name:         "work log noise is rejected",
			content:      "npm install 失败，正在重试。",
			role:         memorycore.RoleWorkReport,
			sourceType:   memorycore.SourceTypeWorkCandidate,
			wantDecision: "reject",
			wantReason:   "work_log_noise",
		},
		{
			name:         "do not remember blocks current fact",
			content:      "我其实很讨厌早会，但这句别记。",
			wantDecision: "reject",
			wantReason:   "do_not_remember",
		},
		{
			name:         "do not mention is deletion intent only",
			content:      "以后不要再提我讨厌早会这件事。",
			wantDecision: "reject",
			wantReason:   "do_not_mention",
		},
		{
			name:         "forget command is deletion intent only",
			content:      "忘掉我不吃香菜。",
			wantDecision: "reject",
			wantReason:   "deletion_intent_only",
			modify: func(req *memorycore.ExtractionRequest, fact *memorycore.ExtractedFactCandidate) {
				fact.ObjectLiteral = stringPtr("香菜")
				fact.ContentSummary = "用户不吃香菜。"
			},
		},
		{
			name:         "correction does not re-emit stale fact",
			content:      "不是北京，是上海。",
			wantDecision: "reject",
			wantReason:   "correction_hint_only",
			modify: func(req *memorycore.ExtractionRequest, fact *memorycore.ExtractedFactCandidate) {
				fact.ObjectLiteral = stringPtr("北京")
				fact.ContentSummary = "用户住在北京。"
			},
		},
		{
			name:         "weak inference needs review",
			content:      "我最近状态不太好。",
			wantDecision: "needs_review",
			wantReason:   "weak_inference",
			modify: func(req *memorycore.ExtractionRequest, fact *memorycore.ExtractedFactCandidate) {
				req.Policy.AllowInference = false
				fact.ExtractionConfidence = memorycore.ConfidenceInferred
			},
		},
		{
			name:         "sensitive inference needs review",
			content:      "我最近状态不太好。",
			wantDecision: "needs_review",
			wantReason:   "sensitive_inference",
			modify: func(req *memorycore.ExtractionRequest, fact *memorycore.ExtractedFactCandidate) {
				fact.ExtractionConfidence = memorycore.ConfidenceInferred
				fact.ObjectLiteral = stringPtr("长期焦虑")
				fact.ContentSummary = "用户长期焦虑。"
			},
		},
		{
			name:         "user confirmation is accepted",
			content:      "对，我就是不喜欢早会。",
			wantDecision: "accept",
			wantReason:   "accepted_for_consolidation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest(t)
			resp := validResponse(t)
			req.Episodes[0].Content = tc.content
			if tc.role != "" {
				req.Episodes[0].Role = tc.role
			}
			if tc.sourceType != "" {
				req.Episodes[0].SourceType = tc.sourceType
			}
			if tc.modify != nil {
				tc.modify(&req, &resp.Facts[0])
			}
			gate := extraction.ValidateExtraction(req, resp)
			requireDecision(t, gate.FactDecisions, "f1", tc.wantDecision, tc.wantReason)
		})
	}
}

func TestApplyAcceptedFactsStopsBlockedEnvelope(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := seedExtractionDB(t)
	defer cleanup()

	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	svc, err := memorycore.Open(ctx, memorycore.Options{DBPath: dbPath})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()

	req := validRequest(t)
	resp := validResponse(t)
	resp.RequestID = "stale_response"
	gate := extraction.ValidateExtraction(req, resp)
	result := extraction.ApplyAcceptedFacts(ctx, svc, db.SQLDB(), req, resp, gate)
	if result.Status != "failed" || len(result.Failures) == 0 || result.Failures[0].CandidateID != "response" {
		t.Fatalf("apply result = %#v, want response-level gate failure", result)
	}
	var factCount int
	if err := db.SQLDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM facts`).Scan(&factCount); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if factCount != 0 {
		t.Fatalf("fact count = %d, want blocked response to write nothing", factCount)
	}
}

func TestValidateExtractionAmbiguousEntityNeedsReview(t *testing.T) {
	req := validRequest(t)
	resp := validResponse(t)
	resp.Entities = []memorycore.ExtractedEntityCandidate{{
		CandidateID:      "amb",
		CanonicalName:    "Someone",
		EntityType:       memorycore.EntityTypePerson,
		Confidence:       0.8,
		SourceEpisodeIDs: []string{"ep_seed"},
		MergeHint:        "ambiguous",
		SensitivityLevel: memorycore.SensitivityNormal,
	}}
	resp.Facts[0].SubjectEntityCandidateID = "amb"

	gate := extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.EntityDecisions, "amb", "needs_review", "ambiguous_entity")
	requireDecision(t, gate.FactDecisions, "f1", "needs_review", "entity_needs_review")
}

func TestValidateExtractionRejectsAgentEntityCandidateBypass(t *testing.T) {
	req := validRequest(t)
	resp := validResponse(t)
	resp.Entities = []memorycore.ExtractedEntityCandidate{{
		CandidateID:      "e_agent",
		CanonicalName:    "Agent",
		EntityType:       memorycore.EntityTypeAgent,
		Confidence:       0.8,
		SourceEpisodeIDs: []string{"ep_seed"},
		MergeHint:        "new_entity",
		SensitivityLevel: memorycore.SensitivityNormal,
	}}
	resp.Facts[0].SubjectEntityCandidateID = "e_agent"
	resp.Facts[0].ContentSummary = "Agent 记录自己的状态。"

	gate := extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.EntityDecisions, "e_agent", "reject", "agent_affect_boundary")
	requireDecision(t, gate.FactDecisions, "f1", "reject", "entity_rejected")
}

func TestBuildRequestFiltersIneligibleEpisodesAndIncludesCatalogs(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := seedExtractionDB(t)
	defer cleanup()

	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	req, err := extraction.BuildRequest(ctx, db.SQLDB(), extraction.BuildRequestOptions{
		PersonaID: "default",
		SessionID: stringPtr("session_seed"),
		Trigger:   memorycore.ExtractionTriggerSessionEnd,
		Now:       time.Date(2026, 5, 11, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60)),
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if len(req.Episodes) != 1 || req.Episodes[0].EpisodeID != "ep_seed" {
		t.Fatalf("episodes = %#v, want only visible/searchable ep_seed", req.Episodes)
	}
	if req.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q, want Asia/Shanghai", req.Timezone)
	}
	if len(req.KnownEntities) == 0 || req.KnownEntities[0].EntityID != "ent_user" {
		t.Fatalf("known entities missing ent_user: %#v", req.KnownEntities)
	}
	if len(req.PredicateSchemas) == 0 {
		t.Fatalf("predicate schemas missing")
	}

	_, err = extraction.BuildRequest(ctx, db.SQLDB(), extraction.BuildRequestOptions{
		PersonaID:  "default",
		EpisodeIDs: []string{"ep_hidden"},
		Trigger:    memorycore.ExtractionTriggerSessionEnd,
	})
	if err == nil {
		t.Fatalf("BuildRequest accepted explicitly requested hidden episode")
	}
}

func TestBuildRequestUsesNormalizedWindowForMixedTimestampFormats(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := seedExtractionDB(t)
	defer cleanup()

	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.SQLDB().ExecContext(ctx, `UPDATE episodes SET occurred_at = ? WHERE id = ?`, "2026-06-04 08:00:00", "ep_seed"); err != nil {
		t.Fatalf("set episode occurred_at: %v", err)
	}

	until := time.Date(2026, 6, 4, 7, 59, 59, 0, time.UTC)
	_, err = extraction.BuildRequest(ctx, db.SQLDB(), extraction.BuildRequestOptions{
		PersonaID: "default",
		SessionID: stringPtr("session_seed"),
		Trigger:   memorycore.ExtractionTriggerSessionEnd,
		Until:     &until,
	})
	if err == nil {
		t.Fatalf("BuildRequest accepted episode after mixed-format until boundary")
	}
}

func TestBuildRequestKnownEntitiesKeepsAliasesGroupedByEntity(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := seedExtractionDB(t)
	defer cleanup()

	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	for _, entity := range []struct {
		id   string
		name string
	}{
		{"ent_alpha", "Alpha"},
		{"ent_beta", "Beta"},
	} {
		if _, err := db.SQLDB().ExecContext(ctx, `
INSERT INTO entities(id, persona_id, canonical_name, entity_type)
VALUES (?, 'default', ?, 'concept')`, entity.id, entity.name); err != nil {
			t.Fatalf("insert entity %s: %v", entity.id, err)
		}
	}
	if _, err := db.SQLDB().ExecContext(ctx, `
INSERT INTO entities(id, persona_id, canonical_name, entity_type, visibility_status, searchable)
VALUES ('ent_hidden_alias', 'default', 'Hidden', 'concept', 'hidden', 1)`); err != nil {
		t.Fatalf("insert hidden entity: %v", err)
	}
	for _, alias := range []struct {
		id        string
		entityID  string
		value     string
		createdAt string
	}{
		{"alias_beta_old", "ent_beta", "B-old", "2026-05-31T01:00:00Z"},
		{"alias_alpha_old", "ent_alpha", "A-old", "2026-05-31T02:00:00Z"},
		{"alias_beta_new", "ent_beta", "B-new", "2026-05-31T03:00:00Z"},
		{"alias_alpha_new", "ent_alpha", "A-new", "2026-05-31T04:00:00Z"},
		{"alias_hidden", "ent_hidden_alias", "hidden-alias", "2026-05-31T05:00:00Z"},
	} {
		if _, err := db.SQLDB().ExecContext(ctx, `
INSERT INTO entity_aliases(id, persona_id, entity_id, alias, alias_type, confidence, created_at)
VALUES (?, 'default', ?, ?, 'surface', 1.0, ?)`, alias.id, alias.entityID, alias.value, alias.createdAt); err != nil {
			t.Fatalf("insert alias %s: %v", alias.id, err)
		}
	}

	req, err := extraction.BuildRequest(ctx, db.SQLDB(), extraction.BuildRequestOptions{
		PersonaID: "default",
		SessionID: stringPtr("session_seed"),
		Trigger:   memorycore.ExtractionTriggerSessionEnd,
	})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	gotAliases := map[string][]string{}
	for _, entity := range req.KnownEntities {
		for _, alias := range entity.Aliases {
			gotAliases[entity.EntityID] = append(gotAliases[entity.EntityID], alias.Alias)
		}
	}
	if want := []string{"A-old", "A-new"}; !slices.Equal(gotAliases["ent_alpha"], want) {
		t.Fatalf("alpha aliases = %#v, want %#v", gotAliases["ent_alpha"], want)
	}
	if want := []string{"B-old", "B-new"}; !slices.Equal(gotAliases["ent_beta"], want) {
		t.Fatalf("beta aliases = %#v, want %#v", gotAliases["ent_beta"], want)
	}
	for _, entity := range req.KnownEntities {
		if entity.EntityID == "ent_hidden_alias" {
			t.Fatalf("hidden entity included in known entities: %#v", entity)
		}
	}
}

func TestApplyAcceptedFactsMarksPinAndDoesNotApplyRouteOnlyItems(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := seedExtractionDB(t)
	defer cleanup()

	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	svc, err := memorycore.Open(ctx, memorycore.Options{DBPath: dbPath})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()

	req := validRequest(t)
	resp := validResponse(t)
	resp.PinIntents = []memorycore.ExtractedPinIntent{{
		CandidateID:       "p1",
		TargetCandidateID: stringPtr("f1"),
		ContentSummary:    "记住用户不喜欢早上八点开会",
		SourceEpisodeIDs:  []string{"ep_seed"},
		PinReason:         "user requested memory",
		Confidence:        0.95,
	}}
	resp.DeletionIntents = []memorycore.ExtractedDeletionIntent{{
		CandidateID:       "d1",
		ForgetLevel:       "hard_forget",
		TargetDescription: "不要提早会",
		SourceEpisodeID:   "ep_seed",
		Confidence:        0.9,
	}}

	gate := extraction.ValidateExtraction(req, resp)
	result := extraction.ApplyAcceptedFacts(ctx, svc, db.SQLDB(), req, resp, gate)
	if result.Status != "applied" || result.AppliedCount != 1 {
		t.Fatalf("apply result = %#v, want one applied fact", result)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("apply failures = %#v", result.Failures)
	}

	var pinned, factCount int
	if err := db.SQLDB().QueryRowContext(ctx, `SELECT pinned FROM facts WHERE predicate = 'dislikes'`).Scan(&pinned); err != nil {
		t.Fatalf("query fact pinned: %v", err)
	}
	if pinned != 1 {
		t.Fatalf("pinned = %d, want 1", pinned)
	}
	if err := db.SQLDB().QueryRowContext(ctx, `SELECT COUNT(*) FROM facts`).Scan(&factCount); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if factCount != 1 {
		t.Fatalf("fact count = %d, want deletion intent not applied as fact", factCount)
	}
}

func TestApplyAcceptedFactsObjectEntityNeedsReviewDoesNotBecomeApplyFailure(t *testing.T) {
	ctx := context.Background()
	dbPath, cleanup := seedExtractionDB(t)
	defer cleanup()

	db, err := memsqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	svc, err := memorycore.Open(ctx, memorycore.Options{DBPath: dbPath})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	defer svc.Close()
	if _, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		ID:            "ent_hidden_place",
		CanonicalName: "Hidden Place",
		EntityType:    memorycore.EntityTypePlace,
	}); err != nil {
		t.Fatalf("ensure place: %v", err)
	}
	if _, err := db.SQLDB().ExecContext(ctx, `UPDATE entities SET visibility_status = 'hidden' WHERE id = 'ent_hidden_place'`); err != nil {
		t.Fatalf("hide place: %v", err)
	}

	req := validRequest(t)
	label := "居住地"
	factType := memorycore.FactTypeCoreIdentity
	req.KnownEntities = append(req.KnownEntities, memorycore.ExtractionKnownEntity{
		EntityID:         "ent_hidden_place",
		CanonicalName:    "Hidden Place",
		EntityType:       memorycore.EntityTypePlace,
		VisibilityStatus: memorycore.VisibilityVisible,
		SensitivityLevel: memorycore.SensitivityNormal,
	})
	req.PredicateSchemas = append(req.PredicateSchemas, memorycore.ExtractionPredicateSchema{
		Predicate:         "lives_in",
		CanonicalLabel:    &label,
		DefaultFactType:   &factType,
		Cardinality:       "single",
		ConflictPolicy:    "supersede",
		TemporalBehavior:  "state",
		ObjectKind:        "entity",
		DefaultImportance: 0.8,
		AllowInference:    true,
	})
	resp := validResponse(t)
	resp.Entities = []memorycore.ExtractedEntityCandidate{{
		CandidateID:      "place_candidate",
		KnownEntityID:    stringPtr("ent_hidden_place"),
		CanonicalName:    "Hidden Place",
		EntityType:       memorycore.EntityTypePlace,
		Confidence:       0.9,
		SourceEpisodeIDs: []string{"ep_seed"},
		MergeHint:        "known_entity",
		SensitivityLevel: memorycore.SensitivityNormal,
	}}
	resp.Facts[0].Predicate = "lives_in"
	resp.Facts[0].ObjectLiteral = nil
	resp.Facts[0].ObjectEntityCandidateID = stringPtr("place_candidate")
	resp.Facts[0].ContentSummary = "用户住在 Hidden Place。"
	gate := extraction.ValidateExtraction(req, resp)
	requireDecision(t, gate.FactDecisions, "f1", "accept", "accepted_for_consolidation")

	result := extraction.ApplyAcceptedFacts(ctx, svc, db.SQLDB(), req, resp, gate)
	if len(result.Failures) != 0 {
		t.Fatalf("apply failures = %#v, want object resolution needs_review result", result.Failures)
	}
	if len(result.Results) != 1 || result.Results[0].Status != memorycore.ConsolidationStatusNeedsReview {
		t.Fatalf("apply results = %#v, want one needs_review result", result.Results)
	}
	if result.Results[0].Result == nil || result.Results[0].Result.Action != memorycore.ConsolidationActionNeedsReview {
		t.Fatalf("apply result = %#v, want needs_review action", result.Results[0].Result)
	}
}

func validRequest(t *testing.T) memorycore.ExtractionRequest {
	t.Helper()
	req, err := extraction.ParseRequest(stringsReader(validRequestJSON()))
	if err != nil {
		t.Fatalf("valid request fixture: %v", err)
	}
	return req
}

func validResponse(t *testing.T) memorycore.ExtractionResponse {
	t.Helper()
	resp, err := extraction.ParseResponse(stringsReader(validResponseJSON()))
	if err != nil {
		t.Fatalf("valid response fixture: %v", err)
	}
	return resp
}

func requireDecision(t *testing.T, decisions []memorycore.CandidateGateDecision, candidateID string, decision string, reason string) {
	t.Helper()
	for _, got := range decisions {
		if got.CandidateID != candidateID {
			continue
		}
		if got.Decision != decision {
			t.Fatalf("%s decision = %s, want %s; %#v", candidateID, got.Decision, decision, got)
		}
		for _, code := range got.ReasonCodes {
			if code == reason {
				return
			}
		}
		t.Fatalf("%s reasons = %#v, want %s", candidateID, got.ReasonCodes, reason)
	}
	t.Fatalf("decision for %s not found in %#v", candidateID, decisions)
}

func seedExtractionDB(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")
	svc, err := memorycore.Open(ctx, memorycore.Options{DBPath: dbPath, AutoMigrate: true, EnableFTS: false})
	if err != nil {
		t.Fatalf("open service: %v", err)
	}
	session, err := svc.StartSession(ctx, memorycore.StartSessionRequest{ID: "session_seed", Channel: "cli"})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{ID: "ep_seed", SessionID: session.ID, Content: "我不喜欢早上八点开会。"}); err != nil {
		t.Fatalf("append visible: %v", err)
	}
	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{ID: "ep_hidden", SessionID: session.ID, Content: "hidden", VisibilityStatus: memorycore.VisibilityHidden}); err != nil {
		t.Fatalf("append hidden: %v", err)
	}
	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{ID: "ep_redacted", SessionID: session.ID, Content: "redacted", VisibilityStatus: memorycore.VisibilityRedacted}); err != nil {
		t.Fatalf("append redacted: %v", err)
	}
	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{ID: "ep_unsearchable", SessionID: session.ID, Content: "unsearchable", Searchable: boolPtr(false)}); err != nil {
		t.Fatalf("append unsearchable: %v", err)
	}
	if _, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{ID: "ent_user", CanonicalName: "User", EntityType: memorycore.EntityTypeUser}); err != nil {
		t.Fatalf("ensure user entity: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close seed service: %v", err)
	}
	return dbPath, func() {}
}

func stringPtr(value string) *string {
	return &value
}

func boolPtr(value bool) *bool {
	return &value
}

func stringsReader(value string) *strings.Reader {
	return strings.NewReader(value)
}
