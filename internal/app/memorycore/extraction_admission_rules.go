package memorycore

import "strings"

func evaluateFactAdmission(ctx gateContext, fact ExtractedFactCandidate) AdmissionDecision {
	decision := AdmissionDecision{
		CandidateID: fact.CandidateID,
		Kind:        "fact",
		Action:      AdmissionAccept,
	}
	addReject := func(reason string) {
		decision.Action = AdmissionReject
		decision.ReasonCodes = append(decision.ReasonCodes, reason)
	}
	addReview := func(reason string) {
		if decision.Action != AdmissionReject {
			decision.Action = AdmissionNeedsReview
		}
		decision.ReasonCodes = append(decision.ReasonCodes, reason)
	}

	if ctx.req.Trigger == ExtractionTriggerManualForget {
		addReject(ReasonManualForgetFactRejected)
	}
	if fact.OperationHint == ReasonNoWriteHint {
		addReject(ReasonNoWriteHint)
	}
	if fact.QualityDecision == "reject" {
		addReject(ReasonModelRejected)
	}

	sources := factSourceEpisodes(ctx, fact.SourceEpisodeIDs)
	sourceText := sourceEpisodeText(sources)
	factText := factAdmissionText(fact)
	combinedText := normalizeAdmissionText(sourceText + " " + factText)

	if allFactSourcesHaveRole(ctx, fact.SourceEpisodeIDs, RoleAssistant) {
		if looksAssistantSpeculation(combinedText) {
			addReject(ReasonAssistantSpeculation)
		} else {
			addReject(ReasonAssistantSuggestion)
		}
	} else if noise, reason := factSourcesAreToolSystemOrWorkNoise(ctx, fact.SourceEpisodeIDs); noise {
		addReject(reason)
	} else if len(fact.SourceEpisodeIDs) > 0 && !factHasUserGroundedSource(sources) {
		addReject(ReasonNoUserOwnedClaim)
		addReject(ReasonSourceEpisodeNotUserGrounded)
	}

	if containsDoNotRemember(sourceText) && !fact.Pinned {
		addReject(ReasonDoNotRemember)
	}
	if containsDoNotMention(sourceText) {
		addReject(ReasonDoNotMention)
	}
	if containsDeletionCommand(sourceText) {
		addReject(ReasonDeletionIntentOnly)
	}
	if containsCorrectionAgainstFact(sourceText, fact) {
		addReject(ReasonCorrectionHintOnly)
	}
	if looksHypothetical(combinedText) && !fact.UserRequested && !fact.Pinned {
		addReject(ReasonHypotheticalScenario)
	}
	if fact.ExtractionConfidence == ConfidenceInferred && !ctx.req.Policy.AllowInference {
		addReview(ReasonWeakInference)
	}
	if isSensitiveInference(combinedText, fact) {
		addReview(ReasonSensitiveInference)
	}
	if hasNoDurableValue(sourceText, fact) && !fact.UserRequested && !fact.Pinned {
		if looksEphemeralChitchat(sourceText) {
			addReject(ReasonEphemeralChitchat)
		}
		addReject(ReasonNoDurableValue)
	}

	decision.ReasonCodes = uniqueStrings(decision.ReasonCodes)
	if decision.Action == AdmissionReject {
		decision.Notes = "fact rejected by memory admission policy"
	} else if decision.Action == AdmissionNeedsReview {
		decision.Notes = "fact requires admission review"
	}
	return decision
}

func factAdmissionText(fact ExtractedFactCandidate) string {
	parts := []string{
		fact.Predicate,
		fact.ContentSummary,
		deref(fact.ObjectLiteral),
		deref(fact.EvidenceNotes),
		deref(fact.Reasoning),
		strings.Join(fact.QualityReasons, " "),
	}
	return strings.Join(parts, " ")
}

func factSourceEpisodes(ctx gateContext, ids []string) []ExtractionEpisode {
	episodes := make([]ExtractionEpisode, 0, len(ids))
	for _, id := range ids {
		if episode, ok := ctx.episodes[id]; ok {
			episodes = append(episodes, episode)
		}
	}
	return episodes
}

func sourceEpisodeText(episodes []ExtractionEpisode) string {
	parts := make([]string, 0, len(episodes))
	for _, episode := range episodes {
		parts = append(parts, episode.Content)
	}
	return strings.Join(parts, " ")
}

func factHasUserGroundedSource(episodes []ExtractionEpisode) bool {
	for _, episode := range episodes {
		if episode.Role == RoleUser {
			return true
		}
	}
	return false
}

func factSourcesAreToolSystemOrWorkNoise(ctx gateContext, ids []string) (bool, string) {
	if len(ids) == 0 {
		return false, ""
	}
	allNoise := true
	hasWork := false
	for _, id := range ids {
		episode, ok := ctx.episodes[id]
		if !ok {
			return false, ""
		}
		if episode.Role == RoleWorkReport || episode.SourceType == SourceTypeWorkCandidate {
			hasWork = true
			continue
		}
		if episode.Role == RoleSystem || episode.Role == RoleToolSummary || episode.SourceType == SourceTypeSystem || episode.SourceType == SourceTypePlugin {
			continue
		}
		allNoise = false
		break
	}
	if !allNoise {
		return false, ""
	}
	if hasWork {
		return true, ReasonWorkLogNoise
	}
	return true, ReasonToolNoise
}

func containsDoNotRemember(text string) bool {
	return containsAnyAdmissionText(text, []string{
		"别记", "不要记", "不准记", "别保存", "不要保存", "不要写进记忆", "别写进记忆", "别把这", "不要把这",
		"do not remember", "don't remember", "do not save", "don't save", "not for memory", "off the record",
	})
}

func containsDoNotMention(text string) bool {
	return containsAnyAdmissionText(text, []string{
		"不要再提", "别再提", "不要提", "别提", "不要说起", "别说起",
		"do not mention", "don't mention", "never mention", "never bring up", "stop mentioning",
	})
}

func containsDeletionCommand(text string) bool {
	return containsAnyAdmissionText(text, []string{
		"忘掉", "删掉", "删除", "清除", "抹掉", "从记忆里移除",
		"forget this", "forget about this", "delete this", "remove this", "erase this",
	})
}

func containsCorrectionAgainstFact(sourceText string, fact ExtractedFactCandidate) bool {
	normalized := normalizeAdmissionText(sourceText)
	if !containsAnyAdmissionText(normalized, []string{"不是", "并不是", "更正", "纠正", "修正", "改成", "而是", "correction", "actually", "not "}) {
		return false
	}
	object := normalizeAdmissionText(deref(fact.ObjectLiteral))
	if object == "" {
		return fact.OperationHint == ReasonNoWriteHint
	}
	return strings.Contains(normalized, "不是"+object) ||
		strings.Contains(normalized, "并不是"+object) ||
		strings.Contains(normalized, "不在"+object) ||
		strings.Contains(normalized, "not "+object)
}

func looksHypothetical(text string) bool {
	return containsAnyAdmissionText(text, []string{
		"如果", "假设", "假如", "要是", "万一", "以后如果", "将来如果", "可能会",
		"if i ", "if we ", "if someday", "suppose", "hypothetical", "imaginary", "would be", "might move",
	})
}

func looksAssistantSpeculation(text string) bool {
	return containsAnyAdmissionText(text, []string{
		"可能", "也许", "看起来", "我猜", "推测", "大概",
		"probably", "maybe", "seems", "looks like", "i guess", "might be",
	})
}

func isSensitiveInference(text string, fact ExtractedFactCandidate) bool {
	if fact.ExtractionConfidence != ConfidenceInferred {
		return false
	}
	if fact.SensitivityLevel == SensitivitySensitive || fact.SensitivityLevel == SensitivityHighlySensitive {
		return true
	}
	return containsAnyAdmissionText(text, []string{
		"焦虑", "抑郁", "创伤", "自残", "心理疾病", "诊断", "人格障碍", "成瘾", "躁郁",
		"anxiety", "depression", "trauma", "diagnosis", "addiction", "bipolar", "ptsd",
	})
}

func hasNoDurableValue(sourceText string, fact ExtractedFactCandidate) bool {
	if looksEphemeralChitchat(sourceText) {
		return true
	}
	return fact.Importance > 0 && fact.Importance < 0.2
}

func looksEphemeralChitchat(text string) bool {
	return containsAnyAdmissionText(text, []string{
		"随便说说", "开玩笑", "哈哈", "闲聊", "不用当真",
		"just chatting", "small talk", "joking", "joke",
	})
}

func containsAnyAdmissionText(text string, needles []string) bool {
	text = normalizeAdmissionText(text)
	for _, needle := range needles {
		if strings.Contains(text, normalizeAdmissionText(needle)) {
			return true
		}
	}
	return false
}

func normalizeAdmissionText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
}
