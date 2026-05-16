package memorycore

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	sidecarStageMirror     = "mirror"
	sidecarStageActivation = "graph"
	sidecarStageRerank     = "rerank"

	sidecarStatusTimeout         = "sidecar_timeout"
	sidecarStatusSkippedByBudget = "skipped_by_budget"
)

func normalizeSidecarResilienceOptions(in SidecarResilienceOptions) SidecarResilienceOptions {
	out := SidecarResilienceOptions{
		Timeouts: SidecarStageTimeouts{
			Total:      400 * time.Millisecond,
			Mirror:     80 * time.Millisecond,
			Activation: 150 * time.Millisecond,
			Rerank:     100 * time.Millisecond,
		},
		Breaker: SidecarBreakerOptions{
			Mode:             SidecarBreakerModeEnabled,
			Window:           20,
			FailureThreshold: 3,
			OpenFor:          60 * time.Second,
		},
		ActivationBudget: SidecarActivationBudgetOptions{
			MaxEdgesScannedPerRequest: 10000,
			MaxNeighborsPerNode:       100,
			MaxActivationWall:         120 * time.Millisecond,
		},
	}
	if in.Timeouts.Total > 0 {
		out.Timeouts.Total = in.Timeouts.Total
	}
	if in.Timeouts.Mirror > 0 {
		out.Timeouts.Mirror = in.Timeouts.Mirror
	}
	if in.Timeouts.Activation > 0 {
		out.Timeouts.Activation = in.Timeouts.Activation
	}
	if in.Timeouts.Rerank > 0 {
		out.Timeouts.Rerank = in.Timeouts.Rerank
	}
	switch in.Breaker.Mode {
	case SidecarBreakerModeEnabled, SidecarBreakerModeDisabled:
		out.Breaker.Mode = in.Breaker.Mode
	case SidecarBreakerModeDefault:
		out.Breaker.Mode = SidecarBreakerModeEnabled
	default:
		out.Breaker.Mode = SidecarBreakerModeEnabled
	}
	if in.Breaker.Window > 0 {
		out.Breaker.Window = in.Breaker.Window
	}
	if in.Breaker.FailureThreshold > 0 {
		out.Breaker.FailureThreshold = in.Breaker.FailureThreshold
	}
	if in.Breaker.OpenFor > 0 {
		out.Breaker.OpenFor = in.Breaker.OpenFor
	}
	if in.ActivationBudget.MaxEdgesScannedPerRequest > 0 {
		out.ActivationBudget.MaxEdgesScannedPerRequest = in.ActivationBudget.MaxEdgesScannedPerRequest
	}
	if in.ActivationBudget.MaxNeighborsPerNode > 0 {
		out.ActivationBudget.MaxNeighborsPerNode = in.ActivationBudget.MaxNeighborsPerNode
	}
	if in.ActivationBudget.MaxActivationWall > 0 {
		out.ActivationBudget.MaxActivationWall = in.ActivationBudget.MaxActivationWall
	}
	return out
}

func sidecarStageTimeout(policy SidecarResilienceOptions, stage string) time.Duration {
	switch stage {
	case sidecarStageMirror:
		return policy.Timeouts.Mirror
	case sidecarStageActivation:
		return policy.Timeouts.Activation
	case sidecarStageRerank:
		return policy.Timeouts.Rerank
	default:
		return 0
	}
}

func sidecarTotalContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, timeout)
}

func sidecarStageContext(parent context.Context, total context.Context, timeout time.Duration) (context.Context, context.CancelFunc, bool) {
	if parent.Err() != nil || total.Err() != nil {
		return total, func() {}, false
	}
	if deadline, ok := total.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return total, func() {}, false
		}
		if timeout <= 0 || remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		return total, func() {}, true
	}
	ctx, cancel := context.WithTimeout(total, timeout)
	return ctx, cancel, true
}

func classifySidecarStageError(parent context.Context, stage context.Context, err error) (string, error) {
	if parent.Err() != nil {
		return "", parent.Err()
	}
	if errors.Is(stage.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return sidecarStatusTimeout, nil
	}
	if errors.Is(stage.Err(), context.Canceled) && parent.Err() == nil {
		return sidecarStatusTimeout, nil
	}
	return "sidecar_error", nil
}

func sanitizeSidecarFallbackReason(reason string) string {
	switch strings.TrimSpace(reason) {
	case "":
		return ""
	case "activation_budget_exceeded",
		"sidecar_breaker_open",
		"sidecar_timeout",
		"sidecar_degraded",
		"adapter_unavailable",
		"service_unavailable",
		"sidecar_unhealthy",
		"persona_not_ready",
		"protocol_error",
		"rerank_not_configured",
		"rerank_provider_error":
		return strings.TrimSpace(reason)
	default:
		return "protocol_error"
	}
}

type sidecarBreakerKey struct {
	PersonaID string
	Stage     string
}

type sidecarCircuitBreaker struct {
	mu      sync.Mutex
	options SidecarBreakerOptions
	now     func() time.Time
	states  map[sidecarBreakerKey]*sidecarBreakerState
}

type sidecarBreakerState struct {
	recent        []bool
	openedUntil   time.Time
	probeInFlight bool
}

func newSidecarCircuitBreaker(options SidecarBreakerOptions, now func() time.Time) *sidecarCircuitBreaker {
	options = normalizeSidecarResilienceOptions(SidecarResilienceOptions{Breaker: options}).Breaker
	if now == nil {
		now = time.Now
	}
	return &sidecarCircuitBreaker{
		options: options,
		now:     now,
		states:  map[sidecarBreakerKey]*sidecarBreakerState{},
	}
}

func (b *sidecarCircuitBreaker) allow(personaID string, stage string) bool {
	if b == nil || b.options.Mode == SidecarBreakerModeDisabled {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	state := b.stateLocked(personaID, stage)
	now := b.now()
	if state.openedUntil.IsZero() {
		return true
	}
	if now.Before(state.openedUntil) {
		return false
	}
	if state.probeInFlight {
		return false
	}
	state.probeInFlight = true
	return true
}

func (b *sidecarCircuitBreaker) record(personaID string, stage string, status string, fallbackReason string) {
	if b == nil || b.options.Mode == SidecarBreakerModeDisabled {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	state := b.stateLocked(personaID, stage)
	failed := sidecarBreakerFailureStatus(status, fallbackReason)
	now := b.now()
	if state.probeInFlight {
		state.probeInFlight = false
		if failed {
			state.recent = nil
			state.openedUntil = now.Add(b.options.OpenFor)
			return
		}
		state.recent = nil
		state.openedUntil = time.Time{}
		return
	}
	if !state.openedUntil.IsZero() && now.Before(state.openedUntil) {
		return
	}
	state.openedUntil = time.Time{}
	state.recent = append(state.recent, failed)
	if len(state.recent) > b.options.Window {
		state.recent = state.recent[len(state.recent)-b.options.Window:]
	}
	failures := 0
	for _, recentFailed := range state.recent {
		if recentFailed {
			failures++
		}
	}
	if failures >= b.options.FailureThreshold {
		state.recent = nil
		state.openedUntil = now.Add(b.options.OpenFor)
	}
}

func (b *sidecarCircuitBreaker) stateLocked(personaID string, stage string) *sidecarBreakerState {
	key := sidecarBreakerKey{PersonaID: personaID, Stage: stage}
	state := b.states[key]
	if state == nil {
		state = &sidecarBreakerState{}
		b.states[key] = state
	}
	return state
}

func sidecarBreakerFailureStatus(status string, fallbackReason string) bool {
	switch strings.TrimSpace(status) {
	case "sidecar_error", "sidecar_timeout":
		return true
	case "sidecar_degraded":
		switch sanitizeSidecarFallbackReason(fallbackReason) {
		case "adapter_unavailable", "service_unavailable", "sidecar_unhealthy", "protocol_error", "rerank_not_configured", "rerank_provider_error":
			return true
		case "activation_budget_exceeded":
			return false
		default:
			return false
		}
	default:
		return false
	}
}

func (s *service) recordSidecarStage(personaID string, stage string, status string, fallbackReason string) {
	if s == nil || s.sidecarBreaker == nil {
		return
	}
	s.sidecarBreaker.record(personaID, stage, status, fallbackReason)
}
