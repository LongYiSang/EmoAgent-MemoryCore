package memorycore

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const (
	CurationRequestSchemaVersion  = "memory_delta_curation.v0.1.request"
	CurationResponseSchemaVersion = "memory_delta_curation.v0.1.response"
)

type curationLLMRequestPayload struct {
	SchemaVersion string               `json:"schema_version"`
	PersonaID     string               `json:"persona_id"`
	GroupID       string               `json:"group_id"`
	Facts         []curationPromptFact `json:"facts"`
	Policy        map[string]any       `json:"policy"`
}

type curationPromptFact struct {
	FactID               string                     `json:"fact_id"`
	Role                 string                     `json:"role"`
	ContentSummary       string                     `json:"content_summary"`
	FactType             string                     `json:"fact_type"`
	Predicate            string                     `json:"predicate"`
	SubjectEntityID      string                     `json:"subject_entity_id,omitempty"`
	ObjectLiteral        string                     `json:"object_literal,omitempty"`
	ExtractionConfidence string                     `json:"extraction_confidence"`
	Importance           float64                    `json:"importance"`
	SensitivityLevel     string                     `json:"sensitivity_level"`
	Pinned               bool                       `json:"pinned"`
	SourceEpisodeRefs    []curationSourceEpisodeRef `json:"source_episode_refs,omitempty"`
}

type curationSourceEpisodeRef struct {
	EpisodeID  string `json:"episode_id"`
	OccurredAt string `json:"occurred_at"`
}

type curationLLMResponsePayload struct {
	SchemaVersion            string   `json:"schema_version"`
	Decision                 string   `json:"decision"`
	SemanticRelation         string   `json:"semantic_relation"`
	AnswerGain               string   `json:"answer_gain"`
	Confidence               float64  `json:"confidence"`
	CanonicalFactID          string   `json:"canonical_fact_id"`
	SourceFactIDs            []string `json:"source_fact_ids"`
	MergedContentSummary     string   `json:"merged_content_summary"`
	CanonicalSubjectEntityID string   `json:"canonical_subject_entity_id"`
	CanonicalPredicate       string   `json:"canonical_predicate"`
	CanonicalFactType        string   `json:"canonical_fact_type"`
	CanonicalObjectLiteral   string   `json:"canonical_object_literal"`
	CanonicalObjectEntityID  string   `json:"canonical_object_entity_id"`
	ReasonCodes              []string `json:"reason_codes"`
	RequiresReview           bool     `json:"requires_review"`
}

func curationSystemPrompt() string {
	return "You analyze candidate memory facts for semantic curation. Return only strict JSON."
}

func curationDeveloperPrompt() string {
	return strings.Join([]string{
		"Decide whether the facts express the same memory, a refinement, a complement, a conflict, or distinct facts.",
		"Only same/refinement with no or small answer gain should be eligible for automatic merge.",
		"Do not merge complements or conflicts as the same fact.",
		"Return exactly one top-level JSON object using this schema: schema_version, decision, semantic_relation, answer_gain, confidence, canonical_fact_id, source_fact_ids, merged_content_summary, canonical_subject_entity_id, canonical_predicate, canonical_fact_type, canonical_object_literal, canonical_object_entity_id, reason_codes, requires_review.",
		"schema_version must be memory_delta_curation.v0.1.response.",
		"For merge_into_existing and reinforce_existing, source_fact_ids must include canonical_fact_id plus every fact being merged. Do not use source_fact_ids to mean only non-canonical inputs.",
		"confidence must be a JSON number from 0.0 to 1.0, not a string label such as high, medium, or low.",
		"Do not return actions, arrays, multiple objects, or multiple clusters.",
		"If the facts cannot be represented by one decision, return decision=needs_review, semantic_relation=unclear, answer_gain=unknown, requires_review=true.",
		"Return no markdown, comments, or extra text.",
		"Allowed enum values:",
		"decision: no_op, reinforce_existing, merge_into_existing, create_canonical_fact, coexist_related, conflict_needs_review, needs_review",
		"semantic_relation: same, refinement, overlap, complement, distinct, conflict, unclear",
		"answer_gain: none, small, material, unknown",
		"If unsure, use decision=needs_review, semantic_relation=unclear, answer_gain=unknown, requires_review=true.",
	}, "\n")
}

func parseCurationLLMResponse(text string) (memsqlite.CurationDecision, error) {
	responseHash := curationResponseHash(text)
	shape, err := unsupportedCurationResponseShape(text)
	if err != nil {
		return memsqlite.CurationDecision{}, extractionServiceError("invalid_json", "curation provider returned invalid JSON")
	}
	if shape != "" {
		return unsupportedCurationShapeDecision(shape, responseHash), nil
	}
	var payload curationLLMResponsePayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		if decision, ok := unsupportedCurationSchemaDecision(text, responseHash); ok {
			return decision, nil
		}
		return memsqlite.CurationDecision{}, extractionServiceError("invalid_json", "curation provider returned invalid JSON")
	}
	if payload.SchemaVersion != "" && payload.SchemaVersion != CurationResponseSchemaVersion {
		return unsupportedCurationSchemaVersionDecision(payload, responseHash), nil
	}
	if !allowedCurationDecision(payload.Decision) || !allowedCurationRelation(payload.SemanticRelation) || !allowedCurationAnswerGain(payload.AnswerGain) {
		return unsupportedCurationEnumDecision(payload, responseHash), nil
	}
	if payload.Confidence < 0 || payload.Confidence > 1 {
		return memsqlite.CurationDecision{}, extractionServiceError("validation_failed", "curation response confidence must be within [0, 1]")
	}
	return memsqlite.CurationDecision{
		Decision:                 payload.Decision,
		SemanticRelation:         payload.SemanticRelation,
		AnswerGain:               payload.AnswerGain,
		Confidence:               payload.Confidence,
		CanonicalFactID:          payload.CanonicalFactID,
		SourceFactIDs:            payload.SourceFactIDs,
		MergedContentSummary:     payload.MergedContentSummary,
		CanonicalSubjectEntityID: payload.CanonicalSubjectEntityID,
		CanonicalPredicate:       payload.CanonicalPredicate,
		CanonicalFactType:        payload.CanonicalFactType,
		CanonicalObjectLiteral:   payload.CanonicalObjectLiteral,
		CanonicalObjectEntityID:  payload.CanonicalObjectEntityID,
		ReasonCodes:              payload.ReasonCodes,
		LLMResponseHash:          responseHash,
		RequiresReview:           payload.RequiresReview,
	}, nil
}

func normalizeCurationDecisionForGroup(decision memsqlite.CurationDecision, group memsqlite.CurationCandidateGroup) memsqlite.CurationDecision {
	switch decision.Decision {
	case "merge_into_existing", "reinforce_existing":
	default:
		return decision
	}
	canonicalID := strings.TrimSpace(decision.CanonicalFactID)
	if canonicalID == "" || !curationGroupContainsFact(group, canonicalID) {
		return decision
	}
	sourceIDs := uniqueStrings(decision.SourceFactIDs)
	if containsCurationSourceID(sourceIDs, canonicalID) {
		decision.SourceFactIDs = sourceIDs
		return decision
	}
	decision.SourceFactIDs = append([]string{canonicalID}, sourceIDs...)
	decision.ReasonCodes = uniqueStrings(append(decision.ReasonCodes, "canonical_source_fact_id_added"))
	return decision
}

func curationGroupContainsFact(group memsqlite.CurationCandidateGroup, factID string) bool {
	for _, fact := range group.Facts {
		if strings.TrimSpace(fact.FactID) == factID {
			return true
		}
	}
	return false
}

func containsCurationSourceID(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func unsupportedCurationResponseShape(text string) (string, error) {
	var raw any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return "", err
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return "non_object", nil
	}
	if _, ok := object["actions"]; ok {
		return "actions", nil
	}
	return "", nil
}

func unsupportedCurationShapeDecision(shape string, responseHash string) memsqlite.CurationDecision {
	return memsqlite.CurationDecision{
		Decision:         "needs_review",
		SemanticRelation: "unclear",
		AnswerGain:       "unknown",
		Confidence:       0,
		ReasonCodes: []string{
			"unsupported_llm_enum",
			"unsupported_response_shape=" + curationEnumReasonValue(shape),
		},
		LLMResponseHash: responseHash,
		RequiresReview:  true,
	}
}

func unsupportedCurationSchemaDecision(text string, responseHash string) (memsqlite.CurationDecision, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return memsqlite.CurationDecision{}, false
	}
	confidence, ok := raw["confidence"]
	if !ok || json.Unmarshal(confidence, new(float64)) == nil {
		return memsqlite.CurationDecision{}, false
	}
	var payload struct {
		CanonicalFactID string   `json:"canonical_fact_id"`
		SourceFactIDs   []string `json:"source_fact_ids"`
		ReasonCodes     []string `json:"reason_codes"`
	}
	_ = json.Unmarshal([]byte(text), &payload)
	reasonCodes := append([]string(nil), payload.ReasonCodes...)
	reasonCodes = append(reasonCodes, "unsupported_llm_schema", "unsupported_confidence_type="+curationJSONValueKind(confidence))
	if value, ok := curationJSONStringValue(confidence); ok {
		reasonCodes = append(reasonCodes, "unsupported_confidence="+curationEnumReasonValue(value))
	}
	return memsqlite.CurationDecision{
		Decision:         "needs_review",
		SemanticRelation: "unclear",
		AnswerGain:       "unknown",
		Confidence:       0,
		CanonicalFactID:  payload.CanonicalFactID,
		SourceFactIDs:    payload.SourceFactIDs,
		ReasonCodes:      uniqueStrings(reasonCodes),
		LLMResponseHash:  responseHash,
		RequiresReview:   true,
	}, true
}

func unsupportedCurationSchemaVersionDecision(payload curationLLMResponsePayload, responseHash string) memsqlite.CurationDecision {
	reasonCodes := append([]string(nil), payload.ReasonCodes...)
	reasonCodes = append(reasonCodes, "unsupported_llm_schema", "unsupported_schema_version="+curationEnumReasonValue(payload.SchemaVersion))
	return memsqlite.CurationDecision{
		Decision:                 "needs_review",
		SemanticRelation:         "unclear",
		AnswerGain:               "unknown",
		Confidence:               0,
		CanonicalFactID:          payload.CanonicalFactID,
		SourceFactIDs:            payload.SourceFactIDs,
		MergedContentSummary:     payload.MergedContentSummary,
		CanonicalSubjectEntityID: payload.CanonicalSubjectEntityID,
		CanonicalPredicate:       payload.CanonicalPredicate,
		CanonicalFactType:        payload.CanonicalFactType,
		CanonicalObjectLiteral:   payload.CanonicalObjectLiteral,
		CanonicalObjectEntityID:  payload.CanonicalObjectEntityID,
		ReasonCodes:              uniqueStrings(reasonCodes),
		LLMResponseHash:          responseHash,
		RequiresReview:           true,
	}
}

func curationJSONValueKind(data json.RawMessage) string {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "empty"
	}
	switch trimmed[0] {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "bool"
	case 'n':
		return "null"
	default:
		return "unknown"
	}
}

func curationJSONStringValue(data json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", false
	}
	return value, true
}

func unsupportedCurationEnumDecision(payload curationLLMResponsePayload, responseHash string) memsqlite.CurationDecision {
	confidence := payload.Confidence
	if confidence < 0 || confidence > 1 {
		confidence = 0
	}
	if confidence > 0.5 {
		confidence = 0.5
	}
	reasonCodes := append([]string(nil), payload.ReasonCodes...)
	found := false
	for _, code := range reasonCodes {
		if code == "unsupported_llm_enum" {
			found = true
			break
		}
	}
	if !found {
		reasonCodes = append(reasonCodes, "unsupported_llm_enum")
	}
	if !allowedCurationDecision(payload.Decision) {
		reasonCodes = append(reasonCodes, "unsupported_decision="+curationEnumReasonValue(payload.Decision))
	}
	if !allowedCurationRelation(payload.SemanticRelation) {
		reasonCodes = append(reasonCodes, "unsupported_semantic_relation="+curationEnumReasonValue(payload.SemanticRelation))
	}
	if !allowedCurationAnswerGain(payload.AnswerGain) {
		reasonCodes = append(reasonCodes, "unsupported_answer_gain="+curationEnumReasonValue(payload.AnswerGain))
	}
	return memsqlite.CurationDecision{
		Decision:         "needs_review",
		SemanticRelation: "unclear",
		AnswerGain:       "unknown",
		Confidence:       confidence,
		CanonicalFactID:  payload.CanonicalFactID,
		SourceFactIDs:    payload.SourceFactIDs,
		ReasonCodes:      reasonCodes,
		LLMResponseHash:  responseHash,
		RequiresReview:   true,
	}
}

func curationEnumReasonValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), "_")
	if value == "" {
		return "<empty>"
	}
	if len(value) > 80 {
		return value[:80]
	}
	return value
}

func curationResponseHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func allowedCurationDecision(value string) bool {
	switch value {
	case "no_op", "reinforce_existing", "merge_into_existing", "create_canonical_fact", "coexist_related", "conflict_needs_review", "needs_review":
		return true
	default:
		return false
	}
}

func allowedCurationRelation(value string) bool {
	switch value {
	case "same", "refinement", "overlap", "complement", "distinct", "conflict", "unclear":
		return true
	default:
		return false
	}
}

func allowedCurationAnswerGain(value string) bool {
	switch value {
	case "none", "small", "material", "unknown":
		return true
	default:
		return false
	}
}

func deterministicCurationResponse(req ExtractionLLMRequest) string {
	var payload curationLLMRequestPayload
	_ = json.Unmarshal([]byte(req.UserPrompt), &payload)
	sourceIDs := make([]string, 0, len(payload.Facts))
	for _, fact := range payload.Facts {
		sourceIDs = append(sourceIDs, fact.FactID)
	}
	if len(payload.Facts) < 2 {
		return marshalCurationMock(curationLLMResponsePayload{
			SchemaVersion:    CurationResponseSchemaVersion,
			Decision:         "no_op",
			SemanticRelation: "distinct",
			AnswerGain:       "unknown",
			Confidence:       0.4,
			ReasonCodes:      []string{"insufficient_group"},
		})
	}
	canonical := payload.Facts[0]
	return marshalCurationMock(curationLLMResponsePayload{
		SchemaVersion:            CurationResponseSchemaVersion,
		Decision:                 "merge_into_existing",
		SemanticRelation:         "refinement",
		AnswerGain:               "small",
		Confidence:               0.94,
		CanonicalFactID:          canonical.FactID,
		SourceFactIDs:            sourceIDs,
		MergedContentSummary:     curationMockMergedSummary(payload.Facts),
		CanonicalSubjectEntityID: canonical.SubjectEntityID,
		CanonicalPredicate:       canonical.Predicate,
		CanonicalFactType:        canonical.FactType,
		CanonicalObjectLiteral:   canonical.ObjectLiteral,
		ReasonCodes:              []string{"mock_group_merge"},
	})
}

func curationMockMergedSummary(facts []curationPromptFact) string {
	seen := map[string]struct{}{}
	var summaries []string
	for _, fact := range facts {
		summary := strings.TrimSpace(fact.ContentSummary)
		if summary == "" {
			continue
		}
		if _, ok := seen[summary]; ok {
			continue
		}
		seen[summary] = struct{}{}
		summaries = append(summaries, strings.TrimSuffix(summary, "。"))
	}
	if len(summaries) == 0 {
		return ""
	}
	sort.Strings(summaries)
	return strings.Join(summaries, "；") + "。"
}

func marshalCurationMock(payload curationLLMResponsePayload) string {
	data, _ := json.Marshal(payload)
	return string(data)
}
