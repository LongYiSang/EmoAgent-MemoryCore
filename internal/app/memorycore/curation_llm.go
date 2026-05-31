package memorycore

import (
	"encoding/json"
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
	}, "\n")
}

func parseCurationLLMResponse(text string) (memsqlite.CurationDecision, error) {
	var payload curationLLMResponsePayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return memsqlite.CurationDecision{}, extractionServiceError("invalid_json", "curation provider returned invalid JSON")
	}
	if payload.SchemaVersion != "" && payload.SchemaVersion != CurationResponseSchemaVersion {
		return memsqlite.CurationDecision{}, extractionServiceError("validation_failed", "curation response schema_version is unsupported")
	}
	if !allowedCurationDecision(payload.Decision) || !allowedCurationRelation(payload.SemanticRelation) || !allowedCurationAnswerGain(payload.AnswerGain) {
		return memsqlite.CurationDecision{}, extractionServiceError("validation_failed", "curation response contains unsupported decision fields")
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
		RequiresReview:           payload.RequiresReview,
	}, nil
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
	var hasNoSugar, hasLowSweet, hasSweetenerDislike bool
	for _, fact := range payload.Facts {
		text := fact.ContentSummary + " " + fact.ObjectLiteral
		if strings.Contains(text, "无糖") || strings.Contains(text, "没有糖") {
			hasNoSugar = true
		}
		if strings.Contains(text, "不甜") || strings.Contains(text, "低甜") {
			hasLowSweet = true
		}
		if strings.Contains(text, "代糖") || fact.Predicate == "dislikes" {
			hasSweetenerDislike = true
		}
	}
	sourceIDs := make([]string, 0, len(payload.Facts))
	for _, fact := range payload.Facts {
		sourceIDs = append(sourceIDs, fact.FactID)
	}
	if hasNoSugar && hasLowSweet && !hasSweetenerDislike {
		canonicalID := payload.Facts[0].FactID
		return marshalCurationMock(curationLLMResponsePayload{
			SchemaVersion:          CurationResponseSchemaVersion,
			Decision:               "merge_into_existing",
			SemanticRelation:       "refinement",
			AnswerGain:             "small",
			Confidence:             0.94,
			CanonicalFactID:        canonicalID,
			SourceFactIDs:          sourceIDs,
			MergedContentSummary:   "用户在饮料上偏好无糖、口味不甜。",
			CanonicalPredicate:     "likes",
			CanonicalFactType:      "stable_preference",
			CanonicalObjectLiteral: "无糖、低甜的饮料",
			ReasonCodes:            []string{"same_user_preference", "same_beverage_domain"},
		})
	}
	if hasNoSugar && hasSweetenerDislike {
		return marshalCurationMock(curationLLMResponsePayload{
			SchemaVersion:    CurationResponseSchemaVersion,
			Decision:         "coexist_related",
			SemanticRelation: "complement",
			AnswerGain:       "material",
			Confidence:       0.91,
			SourceFactIDs:    sourceIDs,
			ReasonCodes:      []string{"complement_not_duplicate"},
			RequiresReview:   false,
		})
	}
	return marshalCurationMock(curationLLMResponsePayload{
		SchemaVersion:    CurationResponseSchemaVersion,
		Decision:         "needs_review",
		SemanticRelation: "unclear",
		AnswerGain:       "unknown",
		Confidence:       0.5,
		SourceFactIDs:    sourceIDs,
		ReasonCodes:      []string{"mock_unclear"},
		RequiresReview:   true,
	})
}

func marshalCurationMock(payload curationLLMResponsePayload) string {
	data, _ := json.Marshal(payload)
	return string(data)
}
