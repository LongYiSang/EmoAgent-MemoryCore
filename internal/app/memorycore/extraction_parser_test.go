package memorycore

import (
	"strings"
	"testing"
)

func TestParseResponseRepairsRejectedCandidateModelShape(t *testing.T) {
	body := strings.Replace(validExtractionResponseJSONForParserTest(), `"rejected_candidates":[]`, `"rejected_candidates":[{
		"candidate_id":"r1",
		"reason_code":"hypothetical_scenario",
		"reason":"用户表达的是假设性计划，并非当前事实。",
		"source_episode_ids":["ep_1"]
	}]`, 1)

	resp, report, err := ParseResponseWithRepairReport(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseResponseWithRepairReport: %v", err)
	}
	if len(resp.RejectedCandidates) != 1 {
		t.Fatalf("rejected candidates = %#v, want one", resp.RejectedCandidates)
	}
	got := resp.RejectedCandidates[0]
	if got.CandidateID != "r1" || got.Kind != "candidate" {
		t.Fatalf("rejected candidate = %#v, want normalized candidate r1", got)
	}
	if len(got.Reasons) != 1 || got.Reasons[0] != "hypothetical_scenario" {
		t.Fatalf("rejected reasons = %#v, want hypothetical_scenario", got.Reasons)
	}
	if !report.Applied {
		t.Fatalf("repair report was not marked applied")
	}
}

func TestParseResponseRepairsCorrectionHintModelShape(t *testing.T) {
	body := strings.Replace(validExtractionResponseJSONForParserTest(), `"correction_hints":[]`, `"correction_hints":[{
		"candidate_id":"ch1",
		"kind":"correction",
		"target_candidate_id":null,
		"target_predicate":"likes",
		"target_object_literal":"杭州美食",
		"corrected_topic":"杭州美食",
		"corrected_value":"用户并不喜欢杭州美食，只是觉得杭州美食选择多。",
		"source_episode_ids":["ep_1"],
		"reasoning":"用户明确纠正了助理关于喜欢杭州美食的假设。"
	}]`, 1)

	resp, report, err := ParseResponseWithRepairReport(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseResponseWithRepairReport: %v", err)
	}
	if len(resp.CorrectionHints) != 1 {
		t.Fatalf("correction hints = %#v, want one", resp.CorrectionHints)
	}
	got := resp.CorrectionHints[0]
	if got.CandidateID != "ch1" || got.CorrectedTopic != "杭州美食" {
		t.Fatalf("correction hint = %#v, want normalized ch1 topic", got)
	}
	if got.Reasoning == nil || *got.Reasoning == "" {
		t.Fatalf("correction hint reasoning was not preserved: %#v", got)
	}
	if !report.Applied {
		t.Fatalf("repair report was not marked applied")
	}
}

func TestParseResponseRepairsDeletionIntentModelShape(t *testing.T) {
	body := strings.Replace(validExtractionResponseJSONForParserTest(), `"deletion_intents":[]`, `"deletion_intents":[{
		"candidate_id":"del_1",
		"target_candidate_id":null,
		"target_predicate":"likes",
		"target_object_literal":"杭州的美食",
		"target_episode_ids":[],
		"scope":"soft_forget",
		"reasoning":"用户明确要求以后别再提。",
		"source_episode_ids":["ep_1"],
		"operation_hint":"delete_candidate",
		"quality_decision":"accept_for_consolidation",
		"quality_reasons":["explicit_user_request"]
	}]`, 1)

	resp, report, err := ParseResponseWithRepairReport(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ParseResponseWithRepairReport: %v", err)
	}
	if len(resp.DeletionIntents) != 1 {
		t.Fatalf("deletion intents = %#v, want one", resp.DeletionIntents)
	}
	got := resp.DeletionIntents[0]
	if got.CandidateID != "del_1" || got.ForgetLevel != ForgetLevelSoft || got.TargetDescription != "杭州的美食" || got.SourceEpisodeID != "ep_1" {
		t.Fatalf("deletion intent = %#v, want normalized soft forget target", got)
	}
	if got.Reasoning == nil || *got.Reasoning == "" {
		t.Fatalf("deletion intent reasoning was not preserved: %#v", got)
	}
	if !report.Applied {
		t.Fatalf("repair report was not marked applied")
	}
}

func TestRepairDeveloperPromptIncludesDeletionIntentContract(t *testing.T) {
	prompt := repairDeveloperPrompt(nil)
	for _, want := range []string{
		"response.deletion_intents fields only",
		"forget_level",
		"target_description",
		"source_episode_id",
		"Do not output target_candidate_id",
		"scope",
		"operation_hint",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("repair prompt missing %q: %s", want, prompt)
		}
	}
}

func validExtractionResponseJSONForParserTest() string {
	return `{"schema_version":"memory_extraction_protocol.v0.1","request_id":"req_test","persona_id":"default","session_id":"session_seed","trigger":"session_end","source_window":{"episode_ids":["ep_1"],"started_at":null,"ended_at":null},"entities":[],"facts":[],"links":[],"affect_events":[],"deletion_intents":[],"pin_intents":[],"correction_hints":[],"rejected_candidates":[],"quality_flags":[],"gate_summary":{"accepted_fact_count":0,"needs_review_count":0,"rejected_count":1,"has_deletion_intent":false,"has_pin_intent":false,"requires_human_review":false,"notes":"通过"}}`
}
