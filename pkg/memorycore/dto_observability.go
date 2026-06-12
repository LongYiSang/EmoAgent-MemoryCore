package memorycore

import "time"

type ObservabilitySnapshotRequest struct {
	PersonaID    string    `json:"persona_id,omitempty"`
	Since        time.Time `json:"since,omitempty"`
	IncludeDebug bool      `json:"include_debug,omitempty"`
}

type ObservabilitySnapshot struct {
	PersonaID     string                     `json:"persona_id"`
	GeneratedAt   time.Time                  `json:"generated_at"`
	Status        string                     `json:"status"`
	Warnings      []string                   `json:"warnings,omitempty"`
	Store         StoreObservability         `json:"store"`
	Retrieval     RetrievalObservability     `json:"retrieval"`
	Extraction    ExtractionObservability    `json:"extraction"`
	Forgetting    ForgettingObservability    `json:"forgetting"`
	Retention     RetentionObservability     `json:"retention"`
	Mirror        MirrorObservability        `json:"mirror"`
	NaturalMemory NaturalMemoryObservability `json:"natural_memory"`
}

type StoreObservability struct {
	PersonaCount         int64            `json:"persona_count"`
	SessionCount         int64            `json:"session_count"`
	EpisodeByVisibility  map[string]int64 `json:"episode_by_visibility"`
	FactByVisibility     map[string]int64 `json:"fact_by_visibility"`
	FactByValidity       map[string]int64 `json:"fact_by_validity"`
	FactByLifecycle      map[string]int64 `json:"fact_by_lifecycle"`
	FactBySensitivity    map[string]int64 `json:"fact_by_sensitivity"`
	NarrativeCount       int64            `json:"narrative_count"`
	InsightCount         int64            `json:"insight_count"`
	SearchDocumentCount  int64            `json:"search_document_count"`
	SearchDocumentByTier map[string]int64 `json:"search_document_by_tier"`
	FTSAvailable         bool             `json:"fts_available"`
}

type RetrievalObservability struct {
	AccessEventsByType    map[string]int64 `json:"access_events_by_type"`
	RecentRetrievedCount  int64            `json:"recent_retrieved_count"`
	RecentInjectedCount   int64            `json:"recent_prompt_injected_count"`
	RecentSuppressedCount int64            `json:"recent_suppressed_count"`
}

type ExtractionObservability struct {
	RunsByStatus      map[string]int64 `json:"runs_by_status"`
	RecentRunCount    int64            `json:"recent_run_count"`
	RecentFailedCount int64            `json:"recent_failed_count"`
}

type ForgettingObservability struct {
	DeletionEventsByLevel map[string]int64 `json:"deletion_events_by_level"`
	RecentDeletionCount   int64            `json:"recent_deletion_count"`
	PendingManualCount    int64            `json:"pending_manual_count"`
}

type RetentionObservability struct {
	JobsByStatus      map[string]int64 `json:"jobs_by_status"`
	RecentRunCount    int64            `json:"recent_run_count"`
	RecentFailedCount int64            `json:"recent_failed_count"`
}

type MirrorObservability struct {
	EnabledOrConfigured bool             `json:"enabled_or_configured"`
	QueueByOperation    map[string]int64 `json:"queue_by_operation"`
	QueueByStatus       map[string]int64 `json:"queue_by_status"`
	PendingCount        int64            `json:"pending_count"`
	FailedCount         int64            `json:"failed_count"`
	PersonaReady        bool             `json:"persona_ready"`
	PersonaDegraded     bool             `json:"persona_degraded"`
	LastSyncAt          *time.Time       `json:"last_sync_at,omitempty"`
}

type NaturalMemoryObservability struct {
	RunsByStatus         map[string]int64 `json:"runs_by_status"`
	RecentRunCount       int64            `json:"recent_run_count"`
	RecentDecayedNodes   int64            `json:"recent_decayed_nodes"`
	RecentProtectedNodes int64            `json:"recent_protected_nodes"`
}
