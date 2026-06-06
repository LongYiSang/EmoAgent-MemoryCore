package memorycore

func AllowedExtractionEntityTypes() []string {
	return []string{
		EntityTypeUser,
		EntityTypeAgent,
		EntityTypePerson,
		EntityTypePlace,
		EntityTypeOrg,
		EntityTypeConcept,
		EntityTypeObject,
		EntityTypeEventTopic,
	}
}

func AllowedExtractionMergeHints() []string {
	return []string{
		"known_entity",
		"maybe_existing",
		"new_entity",
		"ambiguous",
	}
}

func AllowedExtractionConfidenceLabels() []string {
	return []string{
		ConfidenceExplicit,
		ConfidenceInferred,
		ConfidenceAmbiguous,
	}
}

func IsAllowedExtractionEntityType(value string) bool {
	return containsExtractionString(AllowedExtractionEntityTypes(), value)
}

func IsAllowedExtractionMergeHint(value string) bool {
	return containsExtractionString(AllowedExtractionMergeHints(), value)
}

func IsAllowedExtractionConfidenceLabel(value string) bool {
	return containsExtractionString(AllowedExtractionConfidenceLabels(), value)
}

func containsExtractionString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
