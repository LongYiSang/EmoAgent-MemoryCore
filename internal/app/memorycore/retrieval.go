package memorycore

import (
	"context"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
	"strings"
	"unicode"
)

const maxRerankQueryTextRune = 160

func (s *service) Retrieve(ctx context.Context, req RetrievalRequest) (*MemoryContext, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	policy := req.Policy
	mirrorCandidates, mirrorDiagnostics, err := s.mirrorFactCandidates(ctx, personaID, req.QueryText, policy)
	if err != nil {
		return nil, err
	}
	prepared, err := s.retrieve.Prepare(ctx, memsqlite.RetrievalRequest{
		PersonaID: personaID,
		SessionID: req.SessionID,
		QueryText: req.QueryText,
		Now:       req.Now,
		Policy: memsqlite.RetrievalPolicy{
			SensitivityPermission: policy.SensitivityPermission,
			AllowHistorical:       policy.AllowHistorical,
			AllowDeepArchive:      policy.AllowDeepArchive,
			FinalMemoryCount:      policy.FinalMemoryCount,
			ContextBudgetTokens:   policy.ContextBudgetTokens,
			UseFTS:                policy.UseFTS,
			UseMirror:             policy.UseMirror,
		},
		Context: memsqlite.RetrievalAffectContext{
			UserMoodLabel:         req.Context.UserMoodLabel,
			RelationshipMoodLabel: req.Context.RelationshipMoodLabel,
		},
		Mirror:            mirrorCandidates,
		MirrorDiagnostics: mirrorDiagnostics,
	})
	if err != nil {
		return nil, err
	}
	graphCandidates, graphDiagnostics, err := s.graphActivationCandidates(ctx, personaID, prepared)
	if err != nil {
		return nil, err
	}
	finalCandidates, safeRerankCandidates, err := s.retrieve.BuildRerankCandidates(ctx, prepared, graphCandidates, graphDiagnostics)
	if err != nil {
		return nil, err
	}
	rerankResults, rerankDiagnostics := s.rerankCandidates(ctx, personaID, prepared, safeRerankCandidates)
	result, err := s.retrieve.CompleteFinal(ctx, finalCandidates, rerankResults, rerankDiagnostics)
	if err != nil {
		return nil, err
	}
	return memoryContextFromStore(result), nil
}

func (s *service) mirrorFactCandidates(ctx context.Context, personaID string, queryText string, policy RetrievalPolicy) ([]memsqlite.RetrievalMirrorCandidate, *memsqlite.MirrorDiagnostics, error) {
	diagnostics := &memsqlite.MirrorDiagnostics{Status: "disabled_by_config"}
	if !policy.UseMirror {
		return nil, diagnostics, nil
	}
	diagnostics.Status = "persona_not_ready"
	ready, err := s.mirrorState.IsReady(ctx, personaID)
	if err != nil {
		return nil, nil, err
	}
	if !ready {
		return nil, diagnostics, nil
	}
	diagnostics.Status = "adapter_missing"
	candidateAdapter, ok := s.mirrorAdapter.(MirrorCandidateAdapter)
	if !ok || candidateAdapter == nil {
		return nil, diagnostics, nil
	}
	limit := policy.FinalMemoryCount
	if limit <= 0 {
		limit = 8
	}
	result, err := candidateAdapter.FindCandidates(ctx, MirrorCandidateRequest{
		PersonaID: personaID,
		QueryText: queryText,
		Limit:     limit * 4,
	})
	if err != nil || result == nil {
		diagnostics.Status = "sidecar_error"
		return nil, diagnostics, nil
	}
	if result.Degraded {
		diagnostics.Status = "sidecar_degraded"
		return nil, diagnostics, nil
	}
	diagnostics.SidecarCandidateCount = len(result.Candidates)
	if len(result.Candidates) == 0 {
		diagnostics.Status = "no_candidates"
		return nil, diagnostics, nil
	}
	candidates := make([]memsqlite.MirrorCandidate, 0, len(result.Candidates))
	for idx, candidate := range result.Candidates {
		rank := candidate.Rank
		if rank <= 0 {
			rank = idx + 1
		}
		candidates = append(candidates, memsqlite.MirrorCandidate{
			TriviumNodeID: candidate.TriviumNodeID,
			Score:         candidate.Score,
			Source:        candidate.Source,
			Rank:          rank,
		})
	}
	report, err := s.mirrorMap.MapFactCandidatesWithDiagnostics(ctx, personaID, candidates)
	if err != nil {
		return nil, nil, err
	}
	diagnostics.SidecarCandidateCount = report.SidecarCandidateCount
	diagnostics.MappedCandidateCount = report.MappedCandidateCount
	diagnostics.DroppedCandidateCount = report.DroppedCandidateCount
	diagnostics.Candidates = make([]memsqlite.MirrorCandidateDiagnostic, 0, len(report.Diagnostics))
	for _, item := range report.Diagnostics {
		diagnostics.Candidates = append(diagnostics.Candidates, memsqlite.MirrorCandidateDiagnostic{
			TriviumNodeID: item.TriviumNodeID,
			SQLiteFactID:  item.SQLiteFactID,
			Score:         item.Score,
			Source:        item.Source,
			Rank:          item.Rank,
			DropReason:    item.DropReason,
		})
	}
	if diagnostics.MappedCandidateCount == 0 && diagnostics.SidecarCandidateCount > 0 {
		diagnostics.Status = "candidates_unmapped_or_stale"
	} else {
		diagnostics.Status = "used"
	}
	return report.Mapped, diagnostics, nil
}

func (s *service) graphActivationCandidates(ctx context.Context, personaID string, prepared memsqlite.PreparedRetrieval) ([]memsqlite.RetrievalActivationCandidate, *memsqlite.GraphActivationDiagnostics, error) {
	diagnostics := &memsqlite.GraphActivationDiagnostics{Status: "disabled_by_config"}
	if !prepared.Policy.UseMirror {
		return nil, diagnostics, nil
	}
	diagnostics.Status = "persona_not_ready"
	ready, err := s.mirrorState.IsReady(ctx, personaID)
	if err != nil {
		return nil, nil, err
	}
	if !ready {
		return nil, diagnostics, nil
	}
	diagnostics.Status = "adapter_missing"
	activationAdapter, ok := s.mirrorAdapter.(MirrorActivationAdapter)
	if !ok || activationAdapter == nil {
		return nil, diagnostics, nil
	}
	seeds, err := s.mirrorMap.MapActivationSeeds(ctx, personaID, prepared.FusedAnchors)
	if err != nil {
		return nil, nil, err
	}
	if len(seeds) == 0 {
		diagnostics.Status = "no_seeds"
		return nil, diagnostics, nil
	}
	result, err := activationAdapter.ActivateGraph(ctx, MirrorActivationRequest{
		PersonaID: personaID,
		Seeds:     mirrorActivationSeedsFromStore(seeds),
		Params:    defaultMirrorActivationParams(),
	})
	if err != nil || result == nil {
		diagnostics.Status = "sidecar_error"
		return nil, diagnostics, nil
	}
	diagnostics.SidecarCandidateCount = len(result.Candidates)
	if result.Degraded {
		diagnostics.Status = "sidecar_degraded"
		return nil, diagnostics, nil
	}
	if len(result.Candidates) == 0 {
		diagnostics.Status = "no_candidates"
		return nil, diagnostics, nil
	}
	report, err := s.mirrorMap.MapActivationCandidatesWithDiagnostics(ctx, personaID, activationCandidatesToStore(result.Candidates))
	if err != nil {
		return nil, nil, err
	}
	diagnostics.SidecarCandidateCount = report.SidecarCandidateCount
	diagnostics.MappedCandidateCount = report.MappedCandidateCount
	diagnostics.DroppedCandidateCount = report.DroppedCandidateCount
	diagnostics.Candidates = make([]memsqlite.GraphActivationCandidateDiagnostic, 0, len(report.Diagnostics))
	for _, item := range report.Diagnostics {
		diagnostics.Candidates = append(diagnostics.Candidates, memsqlite.GraphActivationCandidateDiagnostic{
			TriviumNodeID: item.TriviumNodeID,
			SQLiteNodeID:  item.SQLiteNodeID,
			NodeType:      item.NodeType,
			Score:         item.Score,
			Source:        item.Source,
			Rank:          item.Rank,
			DropReason:    item.DropReason,
			Paths:         graphActivationPathsToStore(item.Paths),
		})
	}
	if diagnostics.MappedCandidateCount == 0 && diagnostics.SidecarCandidateCount > 0 {
		diagnostics.Status = "candidates_unmapped_or_stale"
	} else {
		diagnostics.Status = "used"
	}
	return report.Mapped, diagnostics, nil
}

func (s *service) rerankCandidates(ctx context.Context, personaID string, prepared memsqlite.PreparedRetrieval, candidates []memsqlite.RerankCandidate) ([]memsqlite.RerankResultItem, *memsqlite.RerankDiagnostics) {
	diagnostics := &memsqlite.RerankDiagnostics{
		Status:             "disabled_by_config",
		SafeCandidateCount: len(candidates),
	}
	if !prepared.Policy.UseMirror {
		return nil, diagnostics
	}
	if len(candidates) == 0 {
		diagnostics.Status = "no_candidates"
		return nil, diagnostics
	}
	diagnostics.Status = "adapter_missing"
	rerankAdapter, ok := s.mirrorAdapter.(MirrorRerankAdapter)
	if !ok || rerankAdapter == nil {
		return nil, diagnostics
	}
	result, err := rerankAdapter.Rerank(ctx, MirrorRerankRequest{
		PersonaID:  personaID,
		QueryText:  safeRerankQueryText(prepared.Query),
		Candidates: rerankCandidatesFromStore(candidates),
	})
	if err != nil || result == nil {
		diagnostics.Status = "sidecar_error"
		return nil, diagnostics
	}
	diagnostics.Degraded = result.Degraded
	diagnostics.FallbackReason = result.FallbackReason
	if result.Degraded {
		diagnostics.Status = "sidecar_degraded"
		return nil, diagnostics
	}
	diagnostics.Status = "used"
	diagnostics.ResultCount = len(result.Items)
	return rerankItemsToStore(result.Items), diagnostics
}

func safeRerankQueryText(query memsqlite.QueryAnalysis) string {
	parts := make([]string, 0, 6)
	if normalized := boundedRerankQueryText(query.Normalized); normalized != "" {
		parts = append(parts, "query="+normalized)
	}
	parts = append(parts,
		"memory_domain="+string(query.MemoryDomain),
		"memory_ability="+string(query.MemoryAbility),
		"evidence_need="+string(query.EvidenceNeed),
		"time_mode="+string(query.TimeMode),
	)
	if len(query.Signals) > 0 {
		signals := make([]string, 0, len(query.Signals))
		for _, signal := range query.Signals {
			signals = append(signals, string(signal))
		}
		parts = append(parts, "signals="+strings.Join(signals, ","))
	}
	return strings.Join(parts, " ")
}

func boundedRerankQueryText(value string) string {
	var builder strings.Builder
	previousSpace := true
	for _, r := range strings.TrimSpace(value) {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			if !previousSpace {
				builder.WriteByte(' ')
				previousSpace = true
			}
			continue
		}
		builder.WriteRune(r)
		previousSpace = false
	}
	sanitized := strings.TrimSpace(builder.String())
	if sanitized == "" {
		return ""
	}
	runes := []rune(sanitized)
	if len(runes) > maxRerankQueryTextRune {
		return string(runes[:maxRerankQueryTextRune])
	}
	return sanitized
}

func defaultMirrorActivationParams() MirrorActivationParams {
	return MirrorActivationParams{
		MaxHops:             2,
		HopDecay:            0.70,
		MinEnergy:           0.01,
		MaxActiveNodes:      80,
		HubSuppressionPower: 0.50,
		IncludePaths:        true,
	}
}

func mirrorActivationSeedsFromStore(seeds []memsqlite.ActivationSeed) []MirrorActivationSeed {
	result := make([]MirrorActivationSeed, 0, len(seeds))
	for _, seed := range seeds {
		result = append(result, MirrorActivationSeed{
			TriviumNodeID: seed.TriviumNodeID,
			SQLiteNodeID:  seed.NodeID,
			NodeType:      string(seed.NodeType),
			SeedEnergy:    seed.SeedEnergy,
		})
	}
	return result
}

func activationCandidatesToStore(candidates []MirrorActivationCandidate) []memsqlite.ActivationCandidate {
	result := make([]memsqlite.ActivationCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		source := candidate.Source
		if strings.TrimSpace(source) == "" {
			source = "graph_activation"
		}
		result = append(result, memsqlite.ActivationCandidate{
			TriviumNodeID: candidate.TriviumNodeID,
			Score:         candidate.Score,
			Source:        source,
			Rank:          candidate.Rank,
			Paths:         graphActivationPathsToStorePublic(candidate.Paths),
		})
	}
	return result
}

func rerankCandidatesFromStore(candidates []memsqlite.RerankCandidate) []MirrorRerankCandidate {
	result := make([]MirrorRerankCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, MirrorRerankCandidate{
			NodeID:       candidate.NodeID,
			NodeType:     candidate.NodeType,
			SafeSummary:  candidate.SafeSummary,
			CurrentScore: candidate.CurrentScore,
			AnchorEnergy: candidate.AnchorEnergy,
			GraphEnergy:  candidate.GraphEnergy,
		})
	}
	return result
}

func rerankItemsToStore(items []MirrorRerankItem) []memsqlite.RerankResultItem {
	result := make([]memsqlite.RerankResultItem, 0, len(items))
	for _, item := range items {
		result = append(result, memsqlite.RerankResultItem{
			NodeID:      item.NodeID,
			NodeType:    item.NodeType,
			RerankScore: item.RerankScore,
			DebugReason: item.DebugReason,
		})
	}
	return result
}

func graphActivationPathsToStorePublic(paths []MirrorActivationPath) []memsqlite.GraphActivationPath {
	result := make([]memsqlite.GraphActivationPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, memsqlite.GraphActivationPath{
			TriviumNodeIDs: append([]int64(nil), path.TriviumNodeIDs...),
			LinkTypes:      append([]string(nil), path.LinkTypes...),
		})
	}
	return result
}

func graphActivationPathsToStore(paths []memsqlite.GraphActivationPath) []memsqlite.GraphActivationPath {
	result := make([]memsqlite.GraphActivationPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, memsqlite.GraphActivationPath{
			TriviumNodeIDs: append([]int64(nil), path.TriviumNodeIDs...),
			LinkTypes:      append([]string(nil), path.LinkTypes...),
		})
	}
	return result
}

func (s *service) RebuildSearchDocuments(ctx context.Context, req RebuildSearchDocumentsRequest) (*RebuildSearchDocumentsResult, error) {
	personaID := defaultString(req.PersonaID, s.persona)
	result, err := s.search.RebuildSearchDocuments(ctx, personaID)
	if err != nil {
		return nil, err
	}
	return &RebuildSearchDocumentsResult{Upserted: result.Upserted}, nil
}
