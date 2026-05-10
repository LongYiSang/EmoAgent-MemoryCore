package extraction

import (
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

const (
	decisionAccept      = "accept"
	decisionReject      = "reject"
	decisionNeedsReview = "needs_review"
	decisionRouteOnly   = "route_only"
	decisionNotApplied  = "not_applied"
)

func ValidateExtraction(req memorycore.ExtractionRequest, resp memorycore.ExtractionResponse) memorycore.ExtractionGateResult {
	ctx := newGateContext(req, resp)
	result := memorycore.ExtractionGateResult{
		RequestID: req.RequestID,
		PersonaID: req.PersonaID,
		Status:    "ok",
	}

	result.ResponseDecisions = validateResponseEnvelope(ctx)
	result.EntityDecisions = validateEntities(ctx)
	result.FactDecisions = validateFacts(ctx, result.EntityDecisions)
	result.LinkDecisions = validateLinks(ctx)
	result.AffectEventDecisions = validateAffectEvents(ctx)
	result.DeletionIntentDecisions = validateDeletionIntents(ctx)
	result.PinIntentDecisions = validatePinIntents(ctx)
	result.CorrectionHintDecisions = validateCorrectionHints(ctx)
	result.Summary = summarizeGate(result)
	if len(result.ResponseDecisions) > 0 {
		result.Status = "blocked"
	} else if result.Summary.RejectedCount > 0 {
		result.Status = "has_rejected"
	} else if result.Summary.NeedsReviewCount > 0 {
		result.Status = "has_review"
	}
	return result
}

type gateContext struct {
	req              memorycore.ExtractionRequest
	resp             memorycore.ExtractionResponse
	episodes         map[string]memorycore.ExtractionEpisode
	predicates       map[string]memorycore.ExtractionPredicateSchema
	knownEntities    map[string]memorycore.ExtractionKnownEntity
	entityCandidates map[string]memorycore.ExtractedEntityCandidate
}

func newGateContext(req memorycore.ExtractionRequest, resp memorycore.ExtractionResponse) gateContext {
	ctx := gateContext{
		req:              req,
		resp:             resp,
		episodes:         map[string]memorycore.ExtractionEpisode{},
		predicates:       map[string]memorycore.ExtractionPredicateSchema{},
		knownEntities:    map[string]memorycore.ExtractionKnownEntity{},
		entityCandidates: map[string]memorycore.ExtractedEntityCandidate{},
	}
	for _, episode := range req.Episodes {
		ctx.episodes[episode.EpisodeID] = episode
	}
	for _, schema := range req.PredicateSchemas {
		ctx.predicates[schema.Predicate] = schema
	}
	for _, entity := range req.KnownEntities {
		ctx.knownEntities[entity.EntityID] = entity
	}
	for _, entity := range resp.Entities {
		ctx.entityCandidates[entity.CandidateID] = entity
	}
	return ctx
}

func validateResponseEnvelope(ctx gateContext) []memorycore.CandidateGateDecision {
	var decisions []memorycore.CandidateGateDecision
	if ctx.resp.RequestID != ctx.req.RequestID {
		decisions = append(decisions, decision("response", "response", decisionReject, "request_id_mismatch", "response.request_id must match request.request_id"))
	}
	if ctx.resp.PersonaID != ctx.req.PersonaID {
		decisions = append(decisions, decision("response", "response", decisionReject, "persona_id_mismatch", "response.persona_id must match request.persona_id"))
	}
	if (ctx.req.SessionID == nil) != (ctx.resp.SessionID == nil) {
		decisions = append(decisions, decision("response", "response", decisionReject, "session_id_mismatch", "response.session_id must match request.session_id"))
	} else if ctx.req.SessionID != nil && ctx.resp.SessionID != nil && *ctx.resp.SessionID != *ctx.req.SessionID {
		decisions = append(decisions, decision("response", "response", decisionReject, "session_id_mismatch", "response.session_id must match request.session_id when present"))
	}
	if ctx.resp.Trigger != ctx.req.Trigger {
		decisions = append(decisions, decision("response", "response", decisionReject, "trigger_mismatch", "response.trigger must match request.trigger"))
	}
	for _, episodeID := range ctx.resp.SourceWindow.EpisodeIDs {
		if _, ok := ctx.episodes[episodeID]; !ok {
			decisions = append(decisions, decision("response", "response", decisionReject, "source_window_episode_not_in_request", episodeID))
		}
	}
	return decisions
}

func validateEntities(ctx gateContext) []memorycore.CandidateGateDecision {
	var decisions []memorycore.CandidateGateDecision
	for _, entity := range ctx.resp.Entities {
		reasons := validateSourceIDs(ctx, entity.SourceEpisodeIDs, true)
		if strings.TrimSpace(entity.CandidateID) == "" {
			reasons = append(reasons, "candidate_id_required")
		}
		if !validMergeHint(entity.MergeHint) {
			reasons = append(reasons, "invalid_merge_hint")
		}
		if entity.MergeHint == "ambiguous" {
			reasons = append(reasons, "ambiguous_entity")
		}
		if entity.EntityType == memorycore.EntityTypeAgent {
			reasons = append(reasons, "agent_affect_boundary")
		}
		if entity.Confidence < 0 || entity.Confidence > 1 {
			reasons = append(reasons, "confidence_out_of_range")
		}
		if entity.KnownEntityID != nil {
			if _, ok := ctx.knownEntities[*entity.KnownEntityID]; !ok {
				reasons = append(reasons, "known_entity_not_visible_searchable")
			}
		}
		if entity.MergeHint == "new_entity" {
			if strings.TrimSpace(entity.CanonicalName) == "" {
				reasons = append(reasons, "entity_name_required")
			}
			if !validEntityType(entity.EntityType) {
				reasons = append(reasons, "invalid_entity_type")
			}
			if !validSensitivity(entity.SensitivityLevel) {
				reasons = append(reasons, "invalid_sensitivity_level")
			}
		}
		if entity.MergeHint == "maybe_existing" {
			matches := countKnownEntityMatches(ctx.req.KnownEntities, entity.CanonicalName)
			if matches > 1 {
				reasons = append(reasons, "ambiguous_entity")
			}
		}
		if hasReason(reasons, "agent_affect_boundary") {
			decisions = append(decisions, decisionMany(entity.CandidateID, "entity", decisionReject, reasons, "agent entities cannot become user memory facts"))
			continue
		}
		if hasReason(reasons, "ambiguous_entity") || hasReason(reasons, "known_entity_not_visible_searchable") {
			decisions = append(decisions, decisionMany(entity.CandidateID, "entity", decisionNeedsReview, reasons, "entity requires review"))
			continue
		}
		if len(reasons) > 0 {
			decisions = append(decisions, decisionMany(entity.CandidateID, "entity", decisionReject, reasons, "invalid entity candidate"))
			continue
		}
		decisions = append(decisions, decision(entity.CandidateID, "entity", decisionAccept, "entity_candidate_valid", ""))
	}
	return decisions
}

func validateFacts(ctx gateContext, entityDecisions []memorycore.CandidateGateDecision) []memorycore.CandidateGateDecision {
	entityDecisionByID := map[string]memorycore.CandidateGateDecision{}
	for _, d := range entityDecisions {
		entityDecisionByID[d.CandidateID] = d
	}
	var decisions []memorycore.CandidateGateDecision
	for _, fact := range ctx.resp.Facts {
		rejectReasons := []string{}
		reviewReasons := []string{}
		if strings.TrimSpace(fact.CandidateID) == "" {
			rejectReasons = append(rejectReasons, "candidate_id_required")
		}
		if fact.QualityDecision == "reject" {
			rejectReasons = append(rejectReasons, "model_rejected")
		}
		if fact.QualityDecision == "needs_review" {
			reviewReasons = append(reviewReasons, "model_needs_review")
		}
		for _, r := range validateFactShape(fact) {
			rejectReasons = append(rejectReasons, r)
		}
		if fact.ExtractionConfidence == memorycore.ConfidenceInferred && !ctx.req.Policy.AllowInference {
			reviewReasons = append(reviewReasons, "inference_not_allowed")
		}
		if ctx.req.Trigger == memorycore.ExtractionTriggerManualForget {
			rejectReasons = append(rejectReasons, "manual_forget_fact_rejected")
		}
		sourceReasons := validateSourceIDs(ctx, fact.SourceEpisodeIDs, false)
		for _, r := range sourceReasons {
			rejectReasons = append(rejectReasons, r)
		}
		if _, ok := ctx.predicates[fact.Predicate]; !ok {
			reviewReasons = append(reviewReasons, "unknown_predicate")
		}
		if hasAgentAffectLeak(fact) {
			rejectReasons = append(rejectReasons, "agent_affect_boundary")
		}
		if fact.SensitivityLevel == memorycore.SensitivityHighlySensitive && !ctx.req.Policy.AllowSensitiveExtraction {
			reviewReasons = append(reviewReasons, "highly_sensitive_requires_review")
		}
		if fact.SubjectEntityCandidateID == "" {
			rejectReasons = append(rejectReasons, "subject_entity_candidate_id_required")
		} else if known, ok := ctx.knownEntities[fact.SubjectEntityCandidateID]; ok && known.EntityType == memorycore.EntityTypeAgent {
			rejectReasons = append(rejectReasons, "agent_affect_boundary")
		} else if entityDecision, ok := entityDecisionByID[fact.SubjectEntityCandidateID]; ok {
			if entityDecision.Decision == decisionNeedsReview {
				reviewReasons = append(reviewReasons, "entity_needs_review")
			}
			if entityDecision.Decision == decisionReject {
				rejectReasons = append(rejectReasons, "entity_rejected")
			}
		} else if !specialEntityCandidate(fact.SubjectEntityCandidateID) {
			reviewReasons = append(reviewReasons, "entity_reference_unresolved")
		}
		if fact.ObjectEntityCandidateID != nil && strings.TrimSpace(*fact.ObjectEntityCandidateID) != "" {
			id := strings.TrimSpace(*fact.ObjectEntityCandidateID)
			if entityDecision, ok := entityDecisionByID[id]; ok {
				if entityDecision.Decision == decisionNeedsReview {
					reviewReasons = append(reviewReasons, "entity_needs_review")
				}
				if entityDecision.Decision == decisionReject {
					rejectReasons = append(rejectReasons, "entity_rejected")
				}
			} else if !specialEntityCandidate(id) {
				reviewReasons = append(reviewReasons, "entity_reference_unresolved")
			}
		}
		hasObjectEntity := fact.ObjectEntityCandidateID != nil && strings.TrimSpace(*fact.ObjectEntityCandidateID) != ""
		hasObjectLiteral := fact.ObjectLiteral != nil && strings.TrimSpace(*fact.ObjectLiteral) != ""
		if hasObjectEntity == hasObjectLiteral {
			rejectReasons = append(rejectReasons, "object_entity_or_literal_required")
		}
		if len(rejectReasons) > 0 {
			decisions = append(decisions, decisionMany(fact.CandidateID, "fact", decisionReject, rejectReasons, "fact rejected by Go gate"))
			continue
		}
		if len(reviewReasons) > 0 {
			decisions = append(decisions, decisionMany(fact.CandidateID, "fact", decisionNeedsReview, reviewReasons, "fact requires review"))
			continue
		}
		decisions = append(decisions, decision(fact.CandidateID, "fact", decisionAccept, "accepted_for_consolidation", ""))
	}
	return decisions
}

func validateLinks(ctx gateContext) []memorycore.CandidateGateDecision {
	var decisions []memorycore.CandidateGateDecision
	for _, link := range ctx.resp.Links {
		reasons := validateSourceIDs(ctx, link.SourceEpisodeIDs, true)
		if strings.TrimSpace(link.CandidateID) == "" {
			reasons = append(reasons, "candidate_id_required")
		}
		if !validLinkType(link.LinkType) {
			reasons = append(reasons, "invalid_link_type")
		}
		if len(reasons) > 0 {
			decisions = append(decisions, decisionMany(link.CandidateID, "link", decisionReject, reasons, "invalid link source"))
			continue
		}
		decisions = append(decisions, decision(link.CandidateID, "link", decisionNotApplied, "unsupported_apply", "links are not written in Phase2B"))
	}
	return decisions
}

func validateFactShape(fact memorycore.ExtractedFactCandidate) []string {
	var reasons []string
	if !validFactQualityDecision(fact.QualityDecision) {
		reasons = append(reasons, "invalid_quality_decision")
	}
	if fact.OperationHint != "" && !validOperationHint(fact.OperationHint) {
		reasons = append(reasons, "invalid_operation_hint")
	}
	if fact.TemporalPrecision != "" && !validTemporalPrecision(fact.TemporalPrecision) {
		reasons = append(reasons, "invalid_temporal_precision")
	}
	if fact.ExtractionConfidence != "" && fact.ExtractionConfidence != memorycore.ConfidenceExplicit && fact.ExtractionConfidence != memorycore.ConfidenceInferred && fact.ExtractionConfidence != memorycore.ConfidenceAmbiguous {
		reasons = append(reasons, "invalid_extraction_confidence")
	}
	if fact.FactType != "" && !validFactType(fact.FactType) {
		reasons = append(reasons, "invalid_fact_type")
	}
	if !validSensitivity(fact.SensitivityLevel) {
		reasons = append(reasons, "invalid_sensitivity_level")
	}
	if fact.ExtractionConfidenceScore < 0 || fact.ExtractionConfidenceScore > 1 {
		reasons = append(reasons, "score_out_of_range")
	}
	if fact.Importance < 0 || fact.Importance > 1 {
		reasons = append(reasons, "importance_out_of_range")
	}
	if fact.Valence < -1 || fact.Valence > 1 {
		reasons = append(reasons, "valence_out_of_range")
	}
	if fact.Arousal < 0 || fact.Arousal > 1 {
		reasons = append(reasons, "arousal_out_of_range")
	}
	if fact.ValidFrom != nil && fact.ValidTo != nil && fact.ValidTo.Before(*fact.ValidFrom) {
		reasons = append(reasons, "invalid_temporal_range")
	}
	return reasons
}

func validateAffectEvents(ctx gateContext) []memorycore.CandidateGateDecision {
	var decisions []memorycore.CandidateGateDecision
	for _, event := range ctx.resp.AffectEvents {
		if strings.TrimSpace(event.CandidateID) == "" {
			decisions = append(decisions, decision(event.CandidateID, "affect_event", decisionReject, "candidate_id_required", ""))
			continue
		}
		if !validAffectScope(event.Scope) {
			decisions = append(decisions, decision(event.CandidateID, "affect_event", decisionReject, "invalid_affect_scope", ""))
			continue
		}
		if event.Scope == "agent" {
			decisions = append(decisions, decision(event.CandidateID, "affect_event", decisionReject, "agent_affect_boundary", "agent affect is not user memory"))
			continue
		}
		reasons := validateSourceIDs(ctx, event.SourceEpisodeIDs, true)
		if len(reasons) > 0 {
			decisions = append(decisions, decisionMany(event.CandidateID, "affect_event", decisionReject, reasons, "invalid affect event source"))
			continue
		}
		decisions = append(decisions, decision(event.CandidateID, "affect_event", decisionNotApplied, "unsupported_apply", "affect events are not written in Phase2B"))
	}
	return decisions
}

func validateDeletionIntents(ctx gateContext) []memorycore.CandidateGateDecision {
	var decisions []memorycore.CandidateGateDecision
	for _, intent := range ctx.resp.DeletionIntents {
		if strings.TrimSpace(intent.CandidateID) == "" {
			decisions = append(decisions, decision(intent.CandidateID, "deletion_intent", decisionReject, "candidate_id_required", ""))
			continue
		}
		if !validForgetLevel(intent.ForgetLevel) {
			decisions = append(decisions, decision(intent.CandidateID, "deletion_intent", decisionReject, "invalid_forget_level", ""))
			continue
		}
		if _, ok := ctx.episodes[intent.SourceEpisodeID]; !ok {
			decisions = append(decisions, decision(intent.CandidateID, "deletion_intent", decisionReject, "source_episode_not_in_request", ""))
			continue
		}
		decisions = append(decisions, decision(intent.CandidateID, "deletion_intent", decisionRouteOnly, "route_to_forget_manager", "deletion intents are not executed in Phase2B"))
	}
	return decisions
}

func validatePinIntents(ctx gateContext) []memorycore.CandidateGateDecision {
	var decisions []memorycore.CandidateGateDecision
	for _, intent := range ctx.resp.PinIntents {
		reasons := validateSourceIDs(ctx, intent.SourceEpisodeIDs, true)
		if strings.TrimSpace(intent.CandidateID) == "" {
			reasons = append(reasons, "candidate_id_required")
		}
		if intent.TargetCandidateID == nil || strings.TrimSpace(*intent.TargetCandidateID) == "" {
			reasons = append(reasons, "pin_target_required")
		}
		if len(reasons) > 0 {
			decisions = append(decisions, decisionMany(intent.CandidateID, "pin_intent", decisionNeedsReview, reasons, "pin intent requires review"))
			continue
		}
		decisions = append(decisions, decision(intent.CandidateID, "pin_intent", decisionRouteOnly, "route_to_fact_candidate", ""))
	}
	return decisions
}

func validateCorrectionHints(ctx gateContext) []memorycore.CandidateGateDecision {
	var decisions []memorycore.CandidateGateDecision
	for _, hint := range ctx.resp.CorrectionHints {
		if strings.TrimSpace(hint.CandidateID) == "" {
			decisions = append(decisions, decision(hint.CandidateID, "correction_hint", decisionReject, "candidate_id_required", ""))
			continue
		}
		decisions = append(decisions, decision(hint.CandidateID, "correction_hint", decisionNotApplied, "handoff_only", "correction hints are not written in Phase2B"))
	}
	return decisions
}

func validateSourceIDs(ctx gateContext, ids []string, allowEmpty bool) []string {
	if len(ids) == 0 {
		if allowEmpty {
			return nil
		}
		return []string{"source_episode_ids_required"}
	}
	var reasons []string
	for _, id := range ids {
		episode, ok := ctx.episodes[id]
		if !ok {
			reasons = append(reasons, "source_episode_not_in_request")
			continue
		}
		if episode.VisibilityStatus != memorycore.VisibilityVisible {
			reasons = append(reasons, "source_episode_not_visible")
		}
	}
	return uniqueStrings(reasons)
}

func hasAgentAffectLeak(fact memorycore.ExtractedFactCandidate) bool {
	text := strings.ToLower(fact.Predicate + " " + fact.ContentSummary + " " + deref(fact.ObjectLiteral) + " " + deref(fact.Reasoning))
	if fact.SubjectEntityCandidateID == "agent" {
		return true
	}
	if strings.Contains(text, "agent") && (strings.Contains(text, "喜欢用户") || strings.Contains(text, "担心用户") || strings.Contains(text, "依恋") || strings.Contains(text, "委屈") || strings.Contains(text, "占有")) {
		return true
	}
	if strings.Contains(text, "依恋") || strings.Contains(text, "委屈") || strings.Contains(text, "占有") {
		return true
	}
	return false
}

func summarizeGate(result memorycore.ExtractionGateResult) memorycore.ExtractionGateSummary {
	summary := memorycore.ExtractionGateSummary{}
	all := [][]memorycore.CandidateGateDecision{
		result.ResponseDecisions,
		result.FactDecisions,
		result.EntityDecisions,
		result.LinkDecisions,
		result.AffectEventDecisions,
		result.DeletionIntentDecisions,
		result.PinIntentDecisions,
		result.CorrectionHintDecisions,
	}
	for _, group := range all {
		for _, d := range group {
			switch d.Decision {
			case decisionAccept:
				if d.Kind == "fact" {
					summary.AcceptedFactCount++
				}
			case decisionNeedsReview:
				summary.NeedsReviewCount++
			case decisionReject:
				summary.RejectedCount++
			case decisionRouteOnly:
				summary.RoutedCount++
			case decisionNotApplied:
				summary.NotAppliedCount++
			}
		}
	}
	summary.HasDeletionIntent = len(result.DeletionIntentDecisions) > 0
	summary.HasPinIntent = len(result.PinIntentDecisions) > 0
	summary.RequiresHumanReview = summary.NeedsReviewCount > 0
	return summary
}

func decision(candidateID string, kind string, value string, reason string, notes string) memorycore.CandidateGateDecision {
	return decisionMany(candidateID, kind, value, []string{reason}, notes)
}

func decisionMany(candidateID string, kind string, value string, reasons []string, notes string) memorycore.CandidateGateDecision {
	return memorycore.CandidateGateDecision{
		CandidateID: candidateID,
		Kind:        kind,
		Decision:    value,
		ReasonCodes: uniqueStrings(reasons),
		Notes:       notes,
	}
}

func decisionByID(decisions []memorycore.CandidateGateDecision, candidateID string) (memorycore.CandidateGateDecision, bool) {
	for _, d := range decisions {
		if d.CandidateID == candidateID {
			return d, true
		}
	}
	return memorycore.CandidateGateDecision{}, false
}

func specialEntityCandidate(id string) bool {
	return id == "user" || id == "agent"
}

func validEntityType(value string) bool {
	switch value {
	case memorycore.EntityTypeUser,
		memorycore.EntityTypeAgent,
		memorycore.EntityTypePerson,
		memorycore.EntityTypePlace,
		memorycore.EntityTypeOrg,
		memorycore.EntityTypeConcept,
		memorycore.EntityTypeObject,
		memorycore.EntityTypeEventTopic:
		return true
	default:
		return false
	}
}

func validSensitivity(value string) bool {
	switch value {
	case "", memorycore.SensitivityNormal, memorycore.SensitivitySensitive, memorycore.SensitivityHighlySensitive:
		return true
	default:
		return false
	}
}

func validFactType(value string) bool {
	switch value {
	case memorycore.FactTypeCoreIdentity,
		memorycore.FactTypeSignificantEvent,
		memorycore.FactTypeStablePreference,
		memorycore.FactTypeRelationalState,
		memorycore.FactTypeCommitment,
		memorycore.FactTypeTransientContext,
		memorycore.FactTypeTaskRelevantContext:
		return true
	default:
		return false
	}
}

func validFactQualityDecision(value string) bool {
	switch value {
	case "accept_for_consolidation", "needs_review", "reject":
		return true
	default:
		return false
	}
}

func validOperationHint(value string) bool {
	switch value {
	case "insert_candidate", "correction_candidate", "duplicate_candidate", "supersede_candidate", "no_write_hint":
		return true
	default:
		return false
	}
}

func validTemporalPrecision(value string) bool {
	switch value {
	case "unknown", "date", "time", "datetime", "range", "relative":
		return true
	default:
		return false
	}
}

func validMergeHint(value string) bool {
	switch value {
	case "known_entity", "maybe_existing", "new_entity", "ambiguous":
		return true
	default:
		return false
	}
}

func validForgetLevel(value string) bool {
	switch value {
	case "soft_forget", "hard_forget", "source_redact", "purge":
		return true
	default:
		return false
	}
}

func validAffectScope(value string) bool {
	switch value {
	case "user", "relationship", "conversation", "agent":
		return true
	default:
		return false
	}
}

func validLinkType(value string) bool {
	switch value {
	case "EVIDENCED_BY", "ABOUT_ENTITY", "CAUSED_BY", "EXPLAINS", "CONTRADICTS", "SUPPORTS", "INHIBITS":
		return true
	default:
		return false
	}
}

func countKnownEntityMatches(entities []memorycore.ExtractionKnownEntity, name string) int {
	name = normalizeText(name)
	if name == "" {
		return 0
	}
	var count int
	seen := map[string]bool{}
	for _, entity := range entities {
		matched := normalizeText(entity.CanonicalName) == name
		for _, alias := range entity.Aliases {
			if normalizeText(alias.Alias) == name {
				matched = true
			}
		}
		if matched && !seen[entity.EntityID] {
			count++
			seen[entity.EntityID] = true
		}
	}
	return count
}

func hasReason(reasons []string, reason string) bool {
	for _, r := range reasons {
		if r == reason {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizeText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
