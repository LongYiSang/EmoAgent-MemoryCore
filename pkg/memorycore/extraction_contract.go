package memorycore

import appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"

func AllowedExtractionEntityTypes() []string {
	return appcore.AllowedExtractionEntityTypes()
}

func AllowedExtractionMergeHints() []string {
	return appcore.AllowedExtractionMergeHints()
}

func AllowedExtractionConfidenceLabels() []string {
	return appcore.AllowedExtractionConfidenceLabels()
}
