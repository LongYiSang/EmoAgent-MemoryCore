package memorycore

import "time"

type Options struct {
	DBPath            string
	PersonaID         string
	AutoMigrate       bool
	EnableFTS         bool
	Now               func() time.Time
	MirrorAdapter     MirrorAdapter
	SidecarResilience SidecarResilienceOptions
}

type SidecarBreakerMode string

const (
	SidecarBreakerModeDefault  SidecarBreakerMode = ""
	SidecarBreakerModeEnabled  SidecarBreakerMode = "enabled"
	SidecarBreakerModeDisabled SidecarBreakerMode = "disabled"
)

type SidecarStageTimeouts struct {
	Total      time.Duration
	Mirror     time.Duration
	Activation time.Duration
	Rerank     time.Duration
}

type SidecarBreakerOptions struct {
	Mode             SidecarBreakerMode
	Window           int
	FailureThreshold int
	OpenFor          time.Duration
}

type SidecarActivationBudgetOptions struct {
	MaxEdgesScannedPerRequest int
	MaxNeighborsPerNode       int
	MaxActivationWall         time.Duration
}

type SidecarResilienceOptions struct {
	Timeouts         SidecarStageTimeouts
	Breaker          SidecarBreakerOptions
	ActivationBudget SidecarActivationBudgetOptions
}
