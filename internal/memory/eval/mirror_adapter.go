package eval

import (
	"context"
	"errors"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
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
	fusionMode               string
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
	rewriteCount := len(req.Query.QueryRewrites)
	anchorCount := len(req.Query.SemanticAnchors)
	queryCount := 0
	rawQueryCount := 0
	if req.QueryText != "" || req.Query.Raw != "" {
		queryCount++
		rawQueryCount = 1
	}
	queryCount += rewriteCount + anchorCount
	candidates := append([]memorycore.MirrorCandidate(nil), a.candidates...)
	if a.fusionMode == "max_only" && (rewriteCount > 0 || anchorCount > 0) {
		candidates = maxOnlyMirrorCandidates(candidates)
	}
	return &memorycore.MirrorCandidateResult{
		Candidates: candidates,
		Diagnostics: memorycore.MirrorCandidateSidecarDiagnostics{
			QueryCount:           queryCount,
			RawQueryCount:        rawQueryCount,
			RewriteQueryCount:    rewriteCount,
			AnchorQueryCount:     anchorCount,
			MergedCandidateCount: len(candidates),
			PerQuery:             evalMirrorPerQueryDiagnostics(req),
		},
	}, nil
}

func maxOnlyMirrorCandidates(candidates []memorycore.MirrorCandidate) []memorycore.MirrorCandidate {
	filtered := make([]memorycore.MirrorCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		switch candidate.Source {
		case "raw", "eval_raw":
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func evalMirrorPerQueryDiagnostics(req memorycore.MirrorCandidateRequest) []memorycore.MirrorCandidatePerQueryDiagnostic {
	var out []memorycore.MirrorCandidatePerQueryDiagnostic
	if req.QueryText != "" || req.Query.Raw != "" {
		out = append(out, memorycore.MirrorCandidatePerQueryDiagnostic{Source: "raw", Count: 1})
	}
	for _, rewrite := range req.Query.QueryRewrites {
		out = append(out, memorycore.MirrorCandidatePerQueryDiagnostic{
			Source:  "rewrite",
			Purpose: rewrite.Purpose,
			Count:   1,
		})
	}
	for _, anchor := range req.Query.SemanticAnchors {
		out = append(out, memorycore.MirrorCandidatePerQueryDiagnostic{
			Source:  "semantic_anchor",
			Purpose: anchor.AnchorType,
			Count:   1,
		})
	}
	return out
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
