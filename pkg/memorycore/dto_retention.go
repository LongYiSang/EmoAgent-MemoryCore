package memorycore

import "time"

type RunRetentionRequest struct {
	PersonaID string
	Now       time.Time
	DryRun    bool
}

type RunRetentionResult struct {
	EvaluatedFacts        int
	ExpiredFacts          int
	ArchivedFacts         int
	SearchDocumentsSynced int
	MirrorUpdatesEnqueued int
}
