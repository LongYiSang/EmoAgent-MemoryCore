package eval

import (
	"context"
	"errors"

	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

type evalMirrorAdapter struct {
	unavailable              bool
	candidates               []appcore.MirrorCandidate
	activationUnavailable    bool
	activationDegraded       bool
	activationFallbackReason string
	activationCandidates     []appcore.MirrorActivationCandidate
	activationCalls          int
	rerankUnavailable        bool
	rerankDegraded           bool
	rerankFallbackReason     string
	rerankItems              []appcore.MirrorRerankItem
	rerankCalls              int
	lastRerankRequest        appcore.MirrorRerankRequest
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
	a.lastRerankRequest = appcore.MirrorRerankRequest{}
}

func (a *evalMirrorAdapter) UpsertNode(ctx context.Context, payload appcore.MirrorNodePayload) (appcore.MirrorNodeUpsertResult, error) {
	if a.unavailable {
		return appcore.MirrorNodeUpsertResult{}, errors.New("sidecar unavailable")
	}
	a.nextID++
	return appcore.MirrorNodeUpsertResult{MirrorNodeID: a.nextID}, nil
}

func (a *evalMirrorAdapter) DeleteNode(ctx context.Context, ref appcore.MirrorNodeRef) error {
	if a.unavailable {
		return errors.New("sidecar unavailable")
	}
	return nil
}

func (a *evalMirrorAdapter) UpsertEdge(ctx context.Context, payload appcore.MirrorEdgePayload) error {
	if a.unavailable {
		return errors.New("sidecar unavailable")
	}
	return nil
}

func (a *evalMirrorAdapter) DeleteEdge(ctx context.Context, ref appcore.MirrorEdgeRef) error {
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

func (a *evalMirrorAdapter) FindCandidates(ctx context.Context, req appcore.MirrorCandidateRequest) (*appcore.MirrorCandidateResult, error) {
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
	candidates := append([]appcore.MirrorCandidate(nil), a.candidates...)
	if a.fusionMode == "max_only" && (rewriteCount > 0 || anchorCount > 0) {
		candidates = maxOnlyMirrorCandidates(candidates)
	}
	return &appcore.MirrorCandidateResult{
		Candidates: candidates,
		Diagnostics: appcore.MirrorCandidateSidecarDiagnostics{
			QueryCount:           queryCount,
			RawQueryCount:        rawQueryCount,
			RewriteQueryCount:    rewriteCount,
			AnchorQueryCount:     anchorCount,
			MergedCandidateCount: len(candidates),
			PerQuery:             evalMirrorPerQueryDiagnostics(req),
		},
	}, nil
}

func maxOnlyMirrorCandidates(candidates []appcore.MirrorCandidate) []appcore.MirrorCandidate {
	filtered := make([]appcore.MirrorCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		switch candidate.Source {
		case "raw", "eval_raw":
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func evalMirrorPerQueryDiagnostics(req appcore.MirrorCandidateRequest) []appcore.MirrorCandidatePerQueryDiagnostic {
	var out []appcore.MirrorCandidatePerQueryDiagnostic
	if req.QueryText != "" || req.Query.Raw != "" {
		out = append(out, appcore.MirrorCandidatePerQueryDiagnostic{Source: "raw", Count: 1})
	}
	for _, rewrite := range req.Query.QueryRewrites {
		out = append(out, appcore.MirrorCandidatePerQueryDiagnostic{
			Source:  "rewrite",
			Purpose: rewrite.Purpose,
			Count:   1,
		})
	}
	for _, anchor := range req.Query.SemanticAnchors {
		out = append(out, appcore.MirrorCandidatePerQueryDiagnostic{
			Source:  "semantic_anchor",
			Purpose: anchor.AnchorType,
			Count:   1,
		})
	}
	return out
}

func (a *evalMirrorAdapter) ActivateGraph(ctx context.Context, req appcore.MirrorActivationRequest) (*appcore.MirrorActivationResult, error) {
	a.activationCalls++
	if a.activationUnavailable {
		return nil, errors.New("activation sidecar unavailable")
	}
	return &appcore.MirrorActivationResult{
		Candidates:     append([]appcore.MirrorActivationCandidate(nil), a.activationCandidates...),
		Degraded:       a.activationDegraded,
		FallbackReason: a.activationFallbackReason,
	}, nil
}

func (a *evalMirrorAdapter) Rerank(ctx context.Context, req appcore.MirrorRerankRequest) (*appcore.MirrorRerankResult, error) {
	a.rerankCalls++
	a.lastRerankRequest = req
	if a.rerankUnavailable {
		return nil, errors.New("rerank sidecar unavailable")
	}
	return &appcore.MirrorRerankResult{
		Items:          append([]appcore.MirrorRerankItem(nil), a.rerankItems...),
		Degraded:       a.rerankDegraded,
		FallbackReason: a.rerankFallbackReason,
	}, nil
}
