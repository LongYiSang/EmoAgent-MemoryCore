package extraction

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type ContractRepairReport struct {
	Applied bool                   `json:"applied"`
	Changes []ContractRepairChange `json:"changes"`
	Flags   []string               `json:"flags"`
}

type ContractRepairChange struct {
	CandidateID string `json:"candidate_id"`
	Kind        string `json:"kind"`
	Field       string `json:"field"`
	From        string `json:"from"`
	To          string `json:"to"`
	Reason      string `json:"reason"`
}

func NormalizeExtractionResponseContract(raw []byte) ([]byte, ContractRepairReport, error) {
	var root map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return raw, ContractRepairReport{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != nil && err != io.EOF {
		return raw, ContractRepairReport{}, err
	}
	if extra != nil {
		return raw, ContractRepairReport{}, fmt.Errorf("trailing JSON value after top-level object")
	}

	report := ContractRepairReport{}
	entities, _ := root["entities"].([]any)
	for _, item := range entities {
		entity, ok := item.(map[string]any)
		if !ok {
			continue
		}
		candidateID := stringValue(entity["candidate_id"])
		normalizeEntityType(entity, candidateID, &report)
		normalizeEntityConfidence(entity, candidateID, &report)
		normalizeMergeHint(entity, candidateID, &report)
	}
	normalizeDeletionIntents(root, &report)
	normalizeCorrectionHints(root, &report)
	normalizeRejectedCandidates(root, &report)
	appendQualityFlags(root, report.Flags)
	out, err := json.Marshal(root)
	if err != nil {
		return raw, report, err
	}
	return out, report, nil
}

func normalizeEntityType(entity map[string]any, candidateID string, report *ContractRepairReport) {
	value, ok := entity["entity_type"].(string)
	if !ok {
		return
	}
	repaired, ok := repairEntityTypeAlias(value)
	if !ok {
		return
	}
	entity["entity_type"] = repaired
	report.add(candidateID, "entity", "entity_type", value, repaired, "entity_type_alias")
}

func normalizeEntityConfidence(entity map[string]any, candidateID string, report *ContractRepairReport) {
	value, ok := entity["confidence"].(string)
	if !ok {
		return
	}
	repaired, ok := repairEntityConfidenceString(value)
	if !ok {
		entity["confidence"] = -1
		report.add(candidateID, "entity", "confidence", value, "-1", "unrepairable_entity_confidence_string")
		return
	}
	entity["confidence"] = repaired
	report.add(candidateID, "entity", "confidence", value, fmt.Sprintf("%.2f", repaired), "entity_confidence_string")
}

func normalizeMergeHint(entity map[string]any, candidateID string, report *ContractRepairReport) {
	value, ok := entity["merge_hint"].(string)
	if !ok {
		return
	}
	repaired, ok := repairMergeHintAlias(value, hasKnownEntityID(entity["known_entity_id"]))
	if !ok {
		return
	}
	entity["merge_hint"] = repaired
	reason := "merge_hint_alias"
	if canonicalContractToken(value) == "known" && repaired == "maybe_existing" {
		reason = "known_without_known_entity_id"
	}
	report.add(candidateID, "entity", "merge_hint", value, repaired, reason)
}

func normalizeDeletionIntents(root map[string]any, report *ContractRepairReport) {
	intents, _ := root["deletion_intents"].([]any)
	for _, item := range intents {
		intent, ok := item.(map[string]any)
		if !ok {
			continue
		}
		candidateID := stringValue(intent["candidate_id"])
		if stringValue(intent["forget_level"]) == "" {
			if scope := stringValue(intent["scope"]); scope != "" {
				intent["forget_level"] = scope
				report.add(candidateID, "deletion_intent", "scope", scope, "forget_level", "deletion_intent_scope_alias")
			}
		}
		if stringValue(intent["target_description"]) == "" {
			if target := stringValue(intent["target_object_literal"]); target != "" {
				intent["target_description"] = target
				report.add(candidateID, "deletion_intent", "target_object_literal", target, "target_description", "deletion_intent_target_literal")
			} else if target := stringValue(intent["target_candidate_id"]); target != "" {
				intent["target_description"] = target
				report.add(candidateID, "deletion_intent", "target_candidate_id", target, "target_description", "deletion_intent_target_candidate")
			}
		}
		if stringValue(intent["source_episode_id"]) == "" {
			if episodes := stringSliceValue(intent["source_episode_ids"]); len(episodes) > 0 {
				intent["source_episode_id"] = episodes[0]
				report.add(candidateID, "deletion_intent", "source_episode_ids", "present", "source_episode_id", "deletion_intent_source_episode_ids")
			}
		}
		if _, ok := intent["requires_confirmation"]; !ok && hasAnyField(intent, "scope", "target_candidate_id", "target_predicate", "target_object_literal", "target_episode_ids", "source_episode_ids") {
			intent["requires_confirmation"] = true
			report.add(candidateID, "deletion_intent", "requires_confirmation", "", "true", "deletion_intent_default_requires_confirmation")
		}
		for _, field := range []string{
			"target_candidate_id",
			"target_predicate",
			"target_object_literal",
			"target_episode_ids",
			"source_episode_ids",
			"scope",
			"operation_hint",
			"quality_decision",
			"quality_reasons",
		} {
			if _, ok := intent[field]; ok {
				delete(intent, field)
				report.add(candidateID, "deletion_intent", field, "present", "", "deletion_intent_extra_field")
			}
		}
	}
}

func normalizeCorrectionHints(root map[string]any, report *ContractRepairReport) {
	hints, _ := root["correction_hints"].([]any)
	for _, item := range hints {
		hint, ok := item.(map[string]any)
		if !ok {
			continue
		}
		candidateID := stringValue(hint["candidate_id"])
		for _, field := range []string{
			"kind",
			"target_candidate_id",
			"target_predicate",
			"target_object_literal",
			"corrected_value",
			"source_episode_ids",
		} {
			if _, ok := hint[field]; ok {
				delete(hint, field)
				report.add(candidateID, "correction_hint", field, "present", "", "correction_hint_extra_field")
			}
		}
	}
}

func normalizeRejectedCandidates(root map[string]any, report *ContractRepairReport) {
	rejected, _ := root["rejected_candidates"].([]any)
	for _, item := range rejected {
		candidate, ok := item.(map[string]any)
		if !ok {
			continue
		}
		candidateID := stringValue(candidate["candidate_id"])
		if strings.TrimSpace(stringValue(candidate["kind"])) == "" {
			candidate["kind"] = "candidate"
			report.add(candidateID, "rejected_candidate", "kind", "", "candidate", "rejected_candidate_default_kind")
		}
		reasons := stringSliceValue(candidate["reasons"])
		if reasonCode := stringValue(candidate["reason_code"]); reasonCode != "" {
			reasons = appendUnique(reasons, reasonCode)
			delete(candidate, "reason_code")
			report.add(candidateID, "rejected_candidate", "reason_code", reasonCode, "reasons", "rejected_candidate_reason_code")
		}
		for _, reasonCode := range stringSliceValue(candidate["reason_codes"]) {
			reasons = appendUnique(reasons, reasonCode)
		}
		if _, ok := candidate["reason_codes"]; ok {
			delete(candidate, "reason_codes")
			report.add(candidateID, "rejected_candidate", "reason_codes", "present", "reasons", "rejected_candidate_reason_codes")
		}
		if len(reasons) == 0 && strings.TrimSpace(stringValue(candidate["reason"])) != "" {
			reasons = appendUnique(reasons, "model_rejected")
		}
		if len(reasons) > 0 {
			candidate["reasons"] = reasons
		}
		if _, ok := candidate["reason"]; ok {
			delete(candidate, "reason")
			report.add(candidateID, "rejected_candidate", "reason", "present", "", "rejected_candidate_reason_text")
		}
		if _, ok := candidate["source_episode_ids"]; ok {
			delete(candidate, "source_episode_ids")
			report.add(candidateID, "rejected_candidate", "source_episode_ids", "present", "", "rejected_candidate_source_episode_ids")
		}
	}
}

func repairEntityTypeAlias(value string) (string, bool) {
	switch canonicalContractToken(value) {
	case "pet", "animal", "cat", "dog", "kitten", "puppy", "宠物", "猫", "狗", "小猫", "小狗":
		return memorycore.EntityTypeObject, true
	default:
		return "", false
	}
}

func repairEntityConfidenceString(value string) (float64, bool) {
	switch canonicalContractToken(value) {
	case "explicit", "directly_stated":
		return 0.95, true
	case "inferred":
		return 0.65, true
	case "implicit":
		return 0.60, true
	case "ambiguous", "uncertain":
		return 0.35, true
	default:
		return 0, false
	}
}

func repairMergeHintAlias(value string, hasKnownID bool) (string, bool) {
	switch canonicalContractToken(value) {
	case "new", "create":
		return "new_entity", true
	case "known":
		if hasKnownID {
			return "known_entity", true
		}
		return "maybe_existing", true
	case "existing", "maybe", "maybe_existing_entity":
		return "maybe_existing", true
	case "uncertain", "unsure":
		return "ambiguous", true
	default:
		return "", false
	}
}

func (r *ContractRepairReport) add(candidateID string, kind string, field string, from string, to string, reason string) {
	r.Applied = true
	change := ContractRepairChange{
		CandidateID: candidateID,
		Kind:        kind,
		Field:       field,
		From:        from,
		To:          to,
		Reason:      reason,
	}
	r.Changes = append(r.Changes, change)
	r.Flags = appendUnique(r.Flags, repairFlag(change))
}

func repairFlag(change ContractRepairChange) string {
	prefix := "repaired"
	if strings.HasPrefix(change.Reason, "unrepairable_") {
		prefix = "unrepairable"
	}
	if change.Reason == "known_without_known_entity_id" {
		return fmt.Sprintf("%s:%s:%s->%s", change.Reason, change.CandidateID, change.From, change.To)
	}
	reason := strings.TrimPrefix(change.Reason, "unrepairable_")
	return fmt.Sprintf("%s_%s:%s:%s->%s", prefix, reason, change.CandidateID, change.From, change.To)
}

func appendQualityFlags(root map[string]any, flags []string) {
	if len(flags) == 0 {
		return
	}
	existing := []string{}
	if values, ok := root["quality_flags"].([]any); ok {
		for _, value := range values {
			if s, ok := value.(string); ok {
				existing = append(existing, s)
			}
		}
	}
	root["quality_flags"] = appendUnique(existing, flags...)
}

func appendUnique(values []string, extra ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values)+len(extra))
	for _, value := range append(values, extra...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func hasKnownEntityID(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return false
	}
}

func hasAnyField(values map[string]any, fields ...string) bool {
	for _, field := range fields {
		if _, ok := values[field]; ok {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func stringSliceValue(value any) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func canonicalContractToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
