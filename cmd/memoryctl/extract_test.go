package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunExtractRequestValidateDryRunAndApplyFlow(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "request.json")
	responsePath := filepath.Join(dir, "response.json")

	requestJSON := requireRunText(t,
		"extract-request",
		"--db", dbPath,
		"--session", "session_seed",
		"--trigger", "session_end",
		"--format", "json",
	)
	if err := os.WriteFile(requestPath, []byte(requestJSON), 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}
	var req map[string]any
	if err := json.Unmarshal([]byte(requestJSON), &req); err != nil {
		t.Fatalf("request json did not decode: %v\n%s", err, requestJSON)
	}
	if req["schema_version"] != "memory_extraction_protocol.v0.1.request" {
		t.Fatalf("request schema_version = %v", req["schema_version"])
	}

	responseJSON := responseForRequest(t, requestJSON)
	if err := os.WriteFile(responsePath, []byte(responseJSON), 0o644); err != nil {
		t.Fatalf("write response: %v", err)
	}

	validate := requireRunText(t, "extract-validate", "--db", dbPath, "--request", requestPath, "--response", responsePath, "--format", "json")
	requireContains(t, validate, `"accepted_fact_count":1`)

	validateWithoutDB := requireRunText(t, "extract-validate", "--request", requestPath, "--response", responsePath, "--format", "json")
	requireContains(t, validateWithoutDB, `"accepted_fact_count":1`)

	dryRun := requireRunText(t, "extract-dry-run", "--db", dbPath, "--request", requestPath, "--response", responsePath, "--format", "text")
	requireContains(t, dryRun, "accepted facts")
	requireContains(t, dryRun, "f1")

	dryRunWithoutDB := requireRunText(t, "extract-dry-run", "--request", requestPath, "--response", responsePath, "--format", "text")
	requireContains(t, dryRunWithoutDB, "accepted facts")

	apply := requireRunText(t, "extract-apply", "--db", dbPath, "--request", requestPath, "--response", responsePath, "--format", "json")
	requireContains(t, apply, `"status":"applied"`)
	retrieved := requireRunText(t, "retrieve", "--db", dbPath, "--query", "早上八点", "--format", "text")
	requireContains(t, retrieved, "用户不喜欢早上八点开会。")
}

func TestRunExtractParserErrorsExitTwoAndNothingAppliedExitOne(t *testing.T) {
	dbPath := seedCLIConsolidationDB(t)
	dir := t.TempDir()
	requestPath := filepath.Join(dir, "request.json")
	responsePath := filepath.Join(dir, "response.json")
	requestJSON := requireRunText(t, "extract-request", "--db", dbPath, "--session", "session_seed")
	if err := os.WriteFile(requestPath, []byte("```json\n"+requestJSON+"\n```"), 0o644); err != nil {
		t.Fatalf("write fenced request: %v", err)
	}
	if err := os.WriteFile(responsePath, []byte(responseForRequest(t, requestJSON)), 0o644); err != nil {
		t.Fatalf("write response: %v", err)
	}
	_, stderr, code := runCLI("extract-validate", "--db", dbPath, "--request", requestPath, "--response", responsePath)
	if code != 2 {
		t.Fatalf("extract-validate code = %d, want 2; stderr=%q", code, stderr)
	}
	requireContains(t, stderr, "request json")

	if err := os.WriteFile(requestPath, []byte(requestJSON), 0o644); err != nil {
		t.Fatalf("write request: %v", err)
	}
	rejectResponse := strings.Replace(responseForRequest(t, requestJSON), `"quality_decision":"accept_for_consolidation"`, `"quality_decision":"needs_review"`, 1)
	if err := os.WriteFile(responsePath, []byte(rejectResponse), 0o644); err != nil {
		t.Fatalf("write review response: %v", err)
	}
	applyOut, stderr, code := runCLI("extract-apply", "--db", dbPath, "--request", requestPath, "--response", responsePath, "--format", "json")
	if code != 1 {
		t.Fatalf("extract-apply review-only code = %d, want 1; stdout=%q stderr=%q", code, applyOut, stderr)
	}
	requireContains(t, applyOut, `"status":"nothing_applied"`)
}

func responseForRequest(t *testing.T, requestJSON string) string {
	t.Helper()
	var request struct {
		RequestID string  `json:"request_id"`
		PersonaID string  `json:"persona_id"`
		SessionID *string `json:"session_id"`
		Trigger   string  `json:"trigger"`
	}
	if err := json.Unmarshal([]byte(requestJSON), &request); err != nil {
		t.Fatalf("decode request for response: %v", err)
	}
	sessionID := "null"
	if request.SessionID != nil {
		sessionID = `"` + *request.SessionID + `"`
	}
	return `{
  "schema_version":"memory_extraction_protocol.v0.1",
  "request_id":"` + request.RequestID + `",
  "persona_id":"` + request.PersonaID + `",
  "session_id":` + sessionID + `,
  "trigger":"` + request.Trigger + `",
  "source_window":{"episode_ids":["ep_seed"],"started_at":null,"ended_at":null},
  "entities":[],
  "facts":[{
    "candidate_id":"f1",
    "subject_entity_candidate_id":"user",
    "predicate":"dislikes",
    "object_entity_candidate_id":null,
    "object_literal":"早上八点开会",
    "content_summary":"用户不喜欢早上八点开会。",
    "fact_type":"stable_preference",
    "valid_from":null,
    "valid_to":null,
    "temporal_precision":"unknown",
    "extraction_confidence":"explicit",
    "extraction_confidence_score":0.95,
    "importance":0.7,
    "valence":-0.55,
    "arousal":0.35,
    "sensitivity_level":"normal",
    "source_episode_ids":["ep_seed"],
    "evidence_notes":"用户直接表达。",
    "reasoning":null,
    "operation_hint":"insert_candidate",
    "pinned":false,
    "user_requested":false,
    "searchable_hint":true,
    "quality_decision":"accept_for_consolidation",
    "quality_reasons":["explicit_user_statement"]
  }],
  "links":[],
  "affect_events":[],
  "deletion_intents":[],
  "pin_intents":[],
  "correction_hints":[],
  "rejected_candidates":[],
  "quality_flags":[],
  "gate_summary":{"accepted_fact_count":1,"needs_review_count":0,"rejected_count":0,"has_deletion_intent":false,"has_pin_intent":false,"requires_human_review":false,"notes":"ok"}
}`
}
