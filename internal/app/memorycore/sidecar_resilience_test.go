package memorycore

import (
	"context"
	"testing"
	"time"
)

func TestSidecarResilienceDefaultsEnableBreaker(t *testing.T) {
	opts := normalizeSidecarResilienceOptions(SidecarResilienceOptions{})
	if opts.Breaker.Mode != SidecarBreakerModeEnabled {
		t.Fatalf("breaker mode = %q, want enabled", opts.Breaker.Mode)
	}
	if opts.Timeouts.Total != 400*time.Millisecond {
		t.Fatalf("total timeout = %s, want 400ms", opts.Timeouts.Total)
	}
	if opts.Timeouts.Activation != 150*time.Millisecond {
		t.Fatalf("activation timeout = %s, want 150ms", opts.Timeouts.Activation)
	}
	if opts.ActivationBudget.MaxActivationWall != 120*time.Millisecond {
		t.Fatalf("activation wall = %s, want 120ms", opts.ActivationBudget.MaxActivationWall)
	}
}

func TestSidecarResilienceExplicitDisableBreaker(t *testing.T) {
	opts := normalizeSidecarResilienceOptions(SidecarResilienceOptions{
		Breaker: SidecarBreakerOptions{Mode: SidecarBreakerModeDisabled},
	})
	if opts.Breaker.Mode != SidecarBreakerModeDisabled {
		t.Fatalf("breaker mode = %q, want disabled", opts.Breaker.Mode)
	}
}

func TestSidecarStageContextUsesTotalRemaining(t *testing.T) {
	parent := context.Background()
	total, cancel := context.WithTimeout(parent, 30*time.Millisecond)
	defer cancel()

	stage, stageCancel, ok := sidecarStageContext(parent, total, 200*time.Millisecond)
	defer stageCancel()
	if !ok {
		t.Fatal("stage context not allowed")
	}
	deadline, ok := stage.Deadline()
	if !ok {
		t.Fatal("stage context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > 40*time.Millisecond {
		t.Fatalf("stage remaining = %s, want bounded by total", remaining)
	}
}

func TestSidecarStageContextSkipsWhenTotalExpired(t *testing.T) {
	parent := context.Background()
	total, cancel := context.WithTimeout(parent, time.Nanosecond)
	cancel()

	_, stageCancel, ok := sidecarStageContext(parent, total, 10*time.Millisecond)
	defer stageCancel()
	if ok {
		t.Fatal("stage context allowed after total budget expired")
	}
}

func TestSidecarBreakerHalfOpenSuccessResets(t *testing.T) {
	now := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	breaker := newSidecarCircuitBreaker(SidecarBreakerOptions{
		Mode:             SidecarBreakerModeEnabled,
		Window:           2,
		FailureThreshold: 1,
		OpenFor:          time.Second,
	}, func() time.Time { return now })

	breaker.record("p1", sidecarStageMirror, "sidecar_timeout", "sidecar_timeout")
	if breaker.allow("p1", sidecarStageMirror) {
		t.Fatal("breaker allowed while open")
	}
	now = now.Add(time.Second + time.Millisecond)
	if !breaker.allow("p1", sidecarStageMirror) {
		t.Fatal("breaker did not allow half-open probe")
	}
	breaker.record("p1", sidecarStageMirror, "used", "")
	if !breaker.allow("p1", sidecarStageMirror) {
		t.Fatal("breaker did not close after successful probe")
	}
}

func TestSidecarBreakerPersonaAndStageIsolation(t *testing.T) {
	now := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	breaker := newSidecarCircuitBreaker(SidecarBreakerOptions{
		Mode:             SidecarBreakerModeEnabled,
		Window:           2,
		FailureThreshold: 1,
		OpenFor:          time.Minute,
	}, func() time.Time { return now })
	breaker.record("p1", sidecarStageMirror, "sidecar_timeout", "sidecar_timeout")
	if breaker.allow("p1", sidecarStageMirror) {
		t.Fatal("p1 mirror should be open")
	}
	if !breaker.allow("p1", sidecarStageActivation) {
		t.Fatal("p1 graph should remain closed")
	}
	if !breaker.allow("p2", sidecarStageMirror) {
		t.Fatal("p2 mirror should remain closed")
	}
}

func TestSidecarBreakerIgnoresActivationBudgetExceeded(t *testing.T) {
	now := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	breaker := newSidecarCircuitBreaker(SidecarBreakerOptions{
		Mode:             SidecarBreakerModeEnabled,
		Window:           1,
		FailureThreshold: 1,
		OpenFor:          time.Minute,
	}, func() time.Time { return now })
	breaker.record("p1", sidecarStageActivation, "sidecar_degraded", "activation_budget_exceeded")
	if !breaker.allow("p1", sidecarStageActivation) {
		t.Fatal("activation budget degradation should not open breaker")
	}
}
