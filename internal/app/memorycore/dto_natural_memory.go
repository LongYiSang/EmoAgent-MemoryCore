package memorycore

import "time"

const NaturalMemoryAlgorithmPowerSleepV1 = "natural_power_sleep_v1"

type NaturalMemoryRunKind string

const (
	NaturalMemoryRunSleepCycle NaturalMemoryRunKind = "sleep_cycle"
	NaturalMemoryRunManual     NaturalMemoryRunKind = "manual"
	NaturalMemoryRunAPI        NaturalMemoryRunKind = "api"
	NaturalMemoryRunTest       NaturalMemoryRunKind = "test"
)

type NaturalMemoryRunStatus string

const (
	NaturalMemoryRunStatusRunning   NaturalMemoryRunStatus = "running"
	NaturalMemoryRunStatusCompleted NaturalMemoryRunStatus = "completed"
	NaturalMemoryRunStatusFailed    NaturalMemoryRunStatus = "failed"
	NaturalMemoryRunStatusSkipped   NaturalMemoryRunStatus = "skipped"
)

type NaturalMemoryState string

const (
	NaturalMemoryStateSalient           NaturalMemoryState = "salient"
	NaturalMemoryStateAvailable         NaturalMemoryState = "available"
	NaturalMemoryStateLatent            NaturalMemoryState = "latent"
	NaturalMemoryStateFaded             NaturalMemoryState = "faded"
	NaturalMemoryStateSleepConsolidated NaturalMemoryState = "sleep_consolidated"
)

type RunNaturalMemoryCycleRequest struct {
	PersonaID      string
	Now            time.Time
	DryRun         bool
	Force          bool
	Explain        bool
	RunKind        NaturalMemoryRunKind
	LocalDate      string
	LocalTime      string
	Timezone       string
	MarkSleepCycle bool
	Options        NaturalMemoryOptions
}

type RunNaturalMemoryTickRequest struct {
	PersonaID string
	Now       time.Time
	Force     bool
	Explain   bool
	Options   NaturalMemoryOptions
}

type RunNaturalMemoryCycleResult struct {
	RunID                       string                     `json:"run_id"`
	PersonaID                   string                     `json:"persona_id"`
	RunKind                     NaturalMemoryRunKind       `json:"run_kind"`
	AlgorithmVersion            string                     `json:"algorithm_version"`
	DryRun                      bool                       `json:"dry_run"`
	Status                      NaturalMemoryRunStatus     `json:"status"`
	EvaluatedNodes              int                        `json:"evaluated_nodes"`
	ScoredNodes                 int                        `json:"scored_nodes"`
	ProtectedNodes              int                        `json:"protected_nodes"`
	DecayedNodes                int                        `json:"decayed_nodes"`
	ReactivatedNodes            int                        `json:"reactivated_nodes"`
	FirstSleepConsolidatedNodes int                        `json:"first_sleep_consolidated_nodes"`
	SearchTierUpdates           int                        `json:"search_tier_updates"`
	SearchDocumentsCreated      int                        `json:"search_documents_created"`
	MirrorUpdatesEnqueued       int                        `json:"mirror_updates_enqueued"`
	CompressionCandidates       int                        `json:"compression_candidates"`
	NarrativesCreated           int                        `json:"narratives_created"`
	InsightsCreated             int                        `json:"insights_created"`
	SkippedNodes                int                        `json:"skipped_nodes"`
	Explain                     []NaturalMemoryExplainItem `json:"explain,omitempty"`
}

type NaturalMemoryExplainItem struct {
	NodeType          string   `json:"node_type"`
	NodeID            string   `json:"node_id"`
	FromNaturalState  string   `json:"from_natural_state,omitempty"`
	ToNaturalState    string   `json:"to_natural_state,omitempty"`
	FromSearchTier    string   `json:"from_search_tier,omitempty"`
	ToSearchTier      string   `json:"to_search_tier,omitempty"`
	NaturalStrength   float64  `json:"natural_strength"`
	Retrievability    float64  `json:"retrievability"`
	ReactivationScore float64  `json:"reactivation_score"`
	ReasonCodes       []string `json:"reason_codes,omitempty"`
	SafeReasonSummary string   `json:"safe_reason_summary,omitempty"`
}
