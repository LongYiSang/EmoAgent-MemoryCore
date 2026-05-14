package eval

import (
	"context"
	"errors"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type evalMirrorAdapter struct {
	unavailable              bool
	candidates               []memorycore.MirrorCandidate
	activationUnavailable    bool
	activationDegraded       bool
	activationFallbackReason string
	activationCandidates     []memorycore.MirrorActivationCandidate
	activationCalls          int
	rerankUnavailable        bool
	rerankDegraded           bool
	rerankFallbackReason     string
	rerankItems              []memorycore.MirrorRerankItem
	rerankCalls              int
	lastRerankRequest        memorycore.MirrorRerankRequest
	nextID                   int64
}

func (a *evalMirrorAdapter) resetForStep() {
	a.unavailable = false
	a.candidates = nil
	a.activationUnavailable = false
	a.activationDegraded = false
	a.activationFallbackReason = ""
	a.activationCandidates = nil
	a.rerankUnavailable = false
	a.rerankDegraded = false
	a.rerankFallbackReason = ""
	a.rerankItems = nil
	a.lastRerankRequest = memorycore.MirrorRerankRequest{}
}

func (a *evalMirrorAdapter) UpsertNode(ctx context.Context, payload memorycore.MirrorNodePayload) (memorycore.MirrorNodeUpsertResult, error) {
	if a.unavailable {
		return memorycore.MirrorNodeUpsertResult{}, errors.New("sidecar unavailable")
	}
	a.nextID++
	return memorycore.MirrorNodeUpsertResult{MirrorNodeID: a.nextID}, nil
}

func (a *evalMirrorAdapter) DeleteNode(ctx context.Context, ref memorycore.MirrorNodeRef) error {
	if a.unavailable {
		return errors.New("sidecar unavailable")
	}
	return nil
}

func (a *evalMirrorAdapter) UpsertEdge(ctx context.Context, payload memorycore.MirrorEdgePayload) error {
	if a.unavailable {
		return errors.New("sidecar unavailable")
	}
	return nil
}

func (a *evalMirrorAdapter) DeleteEdge(ctx context.Context, ref memorycore.MirrorEdgeRef) error {
	if a.unavailable {
		return errors.New("sidecar unavailable")
	}
	return nil
}

func (a *evalMirrorAdapter) ClearNamespace(ctx context.Context, personaID string) error {
	if a.unavailable {
		return errors.New("sidecar unavailable")
	}
	return nil
}

func (a *evalMirrorAdapter) FindCandidates(ctx context.Context, req memorycore.MirrorCandidateRequest) (*memorycore.MirrorCandidateResult, error) {
	if a.unavailable {
		return nil, errors.New("sidecar unavailable")
	}
	return &memorycore.MirrorCandidateResult{
		Candidates: append([]memorycore.MirrorCandidate(nil), a.candidates...),
	}, nil
}

func (a *evalMirrorAdapter) ActivateGraph(ctx context.Context, req memorycore.MirrorActivationRequest) (*memorycore.MirrorActivationResult, error) {
	a.activationCalls++
	if a.activationUnavailable {
		return nil, errors.New("activation sidecar unavailable")
	}
	return &memorycore.MirrorActivationResult{
		Candidates:     append([]memorycore.MirrorActivationCandidate(nil), a.activationCandidates...),
		Degraded:       a.activationDegraded,
		FallbackReason: a.activationFallbackReason,
	}, nil
}

func (a *evalMirrorAdapter) Rerank(ctx context.Context, req memorycore.MirrorRerankRequest) (*memorycore.MirrorRerankResult, error) {
	a.rerankCalls++
	a.lastRerankRequest = req
	if a.rerankUnavailable {
		return nil, errors.New("rerank sidecar unavailable")
	}
	return &memorycore.MirrorRerankResult{
		Items:          append([]memorycore.MirrorRerankItem(nil), a.rerankItems...),
		Degraded:       a.rerankDegraded,
		FallbackReason: a.rerankFallbackReason,
	}, nil
}
