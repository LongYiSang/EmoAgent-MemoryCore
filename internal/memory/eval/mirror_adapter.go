package eval

import (
	"context"
	"errors"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type evalMirrorAdapter struct {
	unavailable bool
	candidates  []memorycore.MirrorCandidate
	nextID      int64
}

func (a *evalMirrorAdapter) resetForStep() {
	a.unavailable = false
	a.candidates = nil
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
