package extraction_test

import (
	"strings"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/memory/extraction"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func TestStrictParsersRejectUnknownFieldsCodeFenceTrailingGarbageAndSchemaMismatch(t *testing.T) {
	t.Run("response unknown field", func(t *testing.T) {
		body := strings.Replace(validResponseJSON(), `"gate_summary"`, `"unexpected":true,"gate_summary"`, 1)
		if _, err := extraction.ParseResponse(strings.NewReader(body)); err == nil {
			t.Fatalf("ParseResponse accepted an unknown field")
		}
	})

	t.Run("request code fence", func(t *testing.T) {
		if _, err := extraction.ParseRequest(strings.NewReader("```json\n" + validRequestJSON() + "\n```")); err == nil {
			t.Fatalf("ParseRequest accepted a code fence wrapper")
		}
	})

	t.Run("response trailing garbage", func(t *testing.T) {
		if _, err := extraction.ParseResponse(strings.NewReader(validResponseJSON() + "\n{}")); err == nil {
			t.Fatalf("ParseResponse accepted trailing JSON")
		}
	})

	t.Run("request schema mismatch", func(t *testing.T) {
		body := strings.Replace(validRequestJSON(), "memory_extraction_protocol.v0.1.request", "wrong", 1)
		if _, err := extraction.ParseRequest(strings.NewReader(body)); err == nil {
			t.Fatalf("ParseRequest accepted mismatched schema_version")
		}
	})

	t.Run("response schema mismatch", func(t *testing.T) {
		body := strings.Replace(validResponseJSON(), "memory_extraction_protocol.v0.1", "wrong", 1)
		if _, err := extraction.ParseResponse(strings.NewReader(body)); err == nil {
			t.Fatalf("ParseResponse accepted mismatched schema_version")
		}
	})
}

func TestParsePreFilterResponseAcceptsProtocolRoutingHints(t *testing.T) {
	for _, hint := range []string{"extract", "forget_manager", "pin_manager", "skip", "review", "route"} {
		t.Run(hint, func(t *testing.T) {
			body := validPreFilterJSON(hint)
			resp, err := extraction.ParsePreFilterResponse(strings.NewReader(body))
			if err != nil {
				t.Fatalf("ParsePreFilterResponse(%s): %v", hint, err)
			}
			if resp.SchemaVersion != memorycore.ExtractionPreFilterSchemaVersion {
				t.Fatalf("schema_version = %q", resp.SchemaVersion)
			}
			if resp.Episodes[0].RoutingHint != hint {
				t.Fatalf("routing_hint = %q, want %q", resp.Episodes[0].RoutingHint, hint)
			}
		})
	}
}

func validRequestJSON() string {
	return `{
  "schema_version": "memory_extraction_protocol.v0.1.request",
  "request_id": "req_test",
  "persona_id": "default",
  "session_id": "session_seed",
  "trigger": "session_end",
  "now": "2026-05-11T10:00:00+08:00",
  "timezone": "Asia/Singapore",
  "episodes": [
    {
      "episode_id": "ep_seed",
      "role": "user",
      "content": "我不喜欢早上八点开会。",
      "occurred_at": "2026-05-11T09:00:00+08:00",
      "source_type": "chat",
      "prev_episode_id": null,
      "next_episode_id": null,
      "visibility_status": "visible",
      "sensitivity_level": "normal"
    }
  ],
  "approved_work_candidates": [],
  "known_entities": [
    {
      "entity_id": "ent_user",
      "canonical_name": "User",
      "entity_type": "user",
      "aliases": [],
      "description": null,
      "visibility_status": "visible",
      "sensitivity_level": "normal"
    }
  ],
  "predicate_schemas": [
    {
      "predicate": "dislikes",
      "canonical_label": "不喜欢",
      "default_fact_type": "stable_preference",
      "cardinality": "multi",
      "conflict_policy": "coexist",
      "temporal_behavior": "preference",
      "object_kind": "either",
      "default_tau_days": 365,
      "default_importance": 0.6,
      "allow_inference": true,
      "sensitive_by_default": false
    }
  ],
  "policy": {
    "allow_sensitive_extraction": false,
    "allow_inference": true,
    "manual_pin": false,
    "manual_forget": false,
    "max_facts": 12,
    "max_links": 20
  }
}`
}

func validResponseJSON() string {
	return `{
  "schema_version": "memory_extraction_protocol.v0.1",
  "request_id": "req_test",
  "persona_id": "default",
  "session_id": "session_seed",
  "trigger": "session_end",
  "source_window": {
    "episode_ids": ["ep_seed"],
    "started_at": null,
    "ended_at": null
  },
  "entities": [],
  "facts": [
    {
      "candidate_id": "f1",
      "subject_entity_candidate_id": "user",
      "predicate": "dislikes",
      "object_entity_candidate_id": null,
      "object_literal": "早上八点开会",
      "content_summary": "用户不喜欢早上八点开会。",
      "fact_type": "stable_preference",
      "valid_from": null,
      "valid_to": null,
      "temporal_precision": "unknown",
      "extraction_confidence": "explicit",
      "extraction_confidence_score": 0.95,
      "importance": 0.7,
      "valence": -0.55,
      "arousal": 0.35,
      "sensitivity_level": "normal",
      "source_episode_ids": ["ep_seed"],
      "evidence_notes": "用户直接表达。",
      "reasoning": null,
      "operation_hint": "insert_candidate",
      "pinned": false,
      "user_requested": false,
      "searchable_hint": true,
      "quality_decision": "accept_for_consolidation",
      "quality_reasons": ["explicit_user_statement"]
    }
  ],
  "links": [],
  "affect_events": [],
  "deletion_intents": [],
  "pin_intents": [],
  "correction_hints": [],
  "rejected_candidates": [],
  "quality_flags": [],
  "gate_summary": {
    "accepted_fact_count": 1,
    "needs_review_count": 0,
    "rejected_count": 0,
    "has_deletion_intent": false,
    "has_pin_intent": false,
    "requires_human_review": false,
    "notes": "One explicit preference candidate."
  }
}`
}

func validPreFilterJSON(routingHint string) string {
	return `{
  "schema_version": "memory_extraction_protocol.v0.1.prefilter",
  "request_id": "req_test",
  "persona_id": "default",
  "session_id": "session_seed",
  "trigger": "session_end",
  "episodes": [
    {
      "episode_id": "ep_seed",
      "keep": false,
      "routing_hint": "` + routingHint + `",
      "reason_codes": ["test"]
    }
  ],
  "quality_flags": []
}`
}
