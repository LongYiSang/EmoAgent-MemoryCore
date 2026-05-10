package extraction_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/memory/extraction"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
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
