package memorycore

import "time"

type RunRetentionRequest struct {
	PersonaID            string
	Now                  time.Time
	DryRun               bool
	DeepArchiveAfterDays int
}

type RunRetentionResult struct {
	EvaluatedFacts        int
	ExpiredFacts          int
	ArchivedFacts         int
	DeepArchivedFacts     int
	SearchDocumentsSynced int
	MirrorUpdatesEnqueued int
}
