package memorycore

import (
	"strings"
	"testing"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestParseCurationLLMResponseUnsupportedEnumsBecomeNeedsReview(t *testing.T) {
	decision, err := parseCurationLLMResponse(`{
		"schema_version":"memory_delta_curation.v0.1.response",
		"decision":"merge",
		"semantic_relation":"duplicate",
		"answer_gain":"low",
		"confidence":0.97,
		"canonical_fact_id":"fact_a",
		"source_fact_ids":["fact_a","fact_b"],
		"reason_codes":["provider_enum_drift"]
	}`)
	if err != nil {
		t.Fatalf("parse curation response: %v", err)
	}
	if decision.Decision != "needs_review" ||
		decision.SemanticRelation != "unclear" ||
		decision.AnswerGain != "unknown" ||
		!decision.RequiresReview {
		t.Fatalf("decision = %#v, want conservative needs_review", decision)
	}
	if decision.Confidence >= 0.88 {
		t.Fatalf("confidence = %v, want below auto-apply threshold", decision.Confidence)
	}
	if !containsCurationString(decision.SourceFactIDs, "fact_b") {
		t.Fatalf("source ids = %#v, want original ids preserved", decision.SourceFactIDs)
	}
	if !containsCurationString(decision.ReasonCodes, "unsupported_llm_enum") {
		t.Fatalf("reason codes = %#v, want unsupported_llm_enum", decision.ReasonCodes)
	}
	for _, want := range []string{
		"unsupported_decision=merge",
		"unsupported_semantic_relation=duplicate",
		"unsupported_answer_gain=low",
	} {
		if !containsCurationString(decision.ReasonCodes, want) {
			t.Fatalf("reason codes = %#v, want %s", decision.ReasonCodes, want)
		}
	}
	if decision.LLMResponseHash == "" {
		t.Fatal("llm response hash is empty")
	}
}

func TestParseCurationLLMResponseActionsShapeBecomesNeedsReview(t *testing.T) {
	decision, err := parseCurationLLMResponse(`{
		"actions":[
			{"type":"create_canonical_fact","fact_ids":["fact_a","fact_b"]}
		]
	}`)
	if err != nil {
		t.Fatalf("parse curation actions response: %v", err)
	}
	if decision.Decision != "needs_review" ||
		decision.SemanticRelation != "unclear" ||
		decision.AnswerGain != "unknown" ||
		!decision.RequiresReview {
		t.Fatalf("decision = %#v, want conservative needs_review", decision)
	}
	if !containsCurationString(decision.ReasonCodes, "unsupported_response_shape=actions") {
		t.Fatalf("reason codes = %#v, want unsupported_response_shape=actions", decision.ReasonCodes)
	}
}

func TestParseCurationLLMResponseConfidenceStringBecomesNeedsReview(t *testing.T) {
	decision, err := parseCurationLLMResponse(`{
		"schema_version":"memory_delta_curation.v0.1.response",
		"decision":"merge_into_existing",
		"semantic_relation":"refinement",
		"answer_gain":"small",
		"confidence":"high",
		"canonical_fact_id":"fact_a",
		"source_fact_ids":["fact_a","fact_b"],
		"reason_codes":["model_reason"],
		"requires_review":false
	}`)
	if err != nil {
		t.Fatalf("parse curation confidence string response: %v", err)
	}
	if decision.Decision != "needs_review" ||
		decision.SemanticRelation != "unclear" ||
		decision.AnswerGain != "unknown" ||
		!decision.RequiresReview {
		t.Fatalf("decision = %#v, want conservative needs_review", decision)
	}
	for _, want := range []string{
		"unsupported_llm_schema",
		"unsupported_confidence_type=string",
		"unsupported_confidence=high",
	} {
		if !containsCurationString(decision.ReasonCodes, want) {
			t.Fatalf("reason codes = %#v, want %s", decision.ReasonCodes, want)
		}
	}
	if !containsCurationString(decision.SourceFactIDs, "fact_b") {
		t.Fatalf("source ids = %#v, want original ids preserved", decision.SourceFactIDs)
	}
}

func TestCurationDeveloperPromptListsAllowedEnums(t *testing.T) {
	prompt := curationDeveloperPrompt()
	for _, want := range []string{
		`Return exactly one top-level JSON object using this schema`,
		`source_fact_ids must include canonical_fact_id plus every fact being merged`,
		`confidence must be a JSON number from 0.0 to 1.0, not a string label`,
		`Do not return actions, arrays, multiple objects, or multiple clusters`,
		`decision: no_op, reinforce_existing, merge_into_existing, create_canonical_fact, coexist_related, conflict_needs_review, needs_review`,
		`semantic_relation: same, refinement, overlap, complement, distinct, conflict, unclear`,
		`answer_gain: none, small, material, unknown`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("curation prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestNormalizeCurationDecisionAddsCanonicalSourceFactID(t *testing.T) {
	group := memsqlite.CurationCandidateGroup{
		Facts: []memsqlite.CurationGroupFact{
			{FactID: "canonical_fact", Role: "new_delta"},
			{FactID: "source_fact", Role: "new_delta"},
		},
	}
	decision := memsqlite.CurationDecision{
		Decision:        "merge_into_existing",
		CanonicalFactID: "canonical_fact",
		SourceFactIDs:   []string{"source_fact"},
		ReasonCodes:     []string{"model_reason"},
	}

	got := normalizeCurationDecisionForGroup(decision, group)

	for _, want := range []string{"canonical_fact", "source_fact"} {
		if !containsCurationString(got.SourceFactIDs, want) {
			t.Fatalf("source ids = %#v, missing %s", got.SourceFactIDs, want)
		}
	}
	if !containsCurationString(got.ReasonCodes, "canonical_source_fact_id_added") {
		t.Fatalf("reason codes = %#v, want canonical_source_fact_id_added", got.ReasonCodes)
	}
}

func containsCurationString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
