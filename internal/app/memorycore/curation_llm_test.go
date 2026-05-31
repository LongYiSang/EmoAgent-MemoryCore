package memorycore

import (
	"strings"
	"testing"
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
}

func TestCurationDeveloperPromptListsAllowedEnums(t *testing.T) {
	prompt := curationDeveloperPrompt()
	for _, want := range []string{
		`decision: no_op, reinforce_existing, merge_into_existing, create_canonical_fact, coexist_related, conflict_needs_review, needs_review`,
		`semantic_relation: same, refinement, overlap, complement, distinct, conflict, unclear`,
		`answer_gain: none, small, material, unknown`,
		`Return a single JSON object`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("curation prompt missing %q:\n%s", want, prompt)
		}
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
