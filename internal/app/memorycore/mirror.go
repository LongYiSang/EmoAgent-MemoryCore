package memorycore

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	internalmirror "github.com/longyisang/emoagent-memorycore/internal/mirror"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const defaultMirrorSyncLimit = 100
const mirrorRebuildSupersedeReason = "superseded by mirror rebuild"

func (s *service) RunMirrorSync(ctx context.Context, req RunMirrorSyncRequest) (*RunMirrorSyncResult, error) {
	if s.mirrorAdapter == nil {
		return nil, fmt.Errorf("%w: MirrorAdapter is required", ErrInvalidOptions)
	}
	personaID := defaultString(req.PersonaID, s.persona)
	ready, err := s.mirrorState.IsReady(ctx, personaID)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, fmt.Errorf("%w: mirror sync blocked for persona %q because mirror state is not ready", ErrInvalidRequest, personaID)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultMirrorSyncLimit
	}

	worker := internalmirror.NewWorker(internalmirror.WorkerOptions{
		Queue:    mirrorQueueBridge{repo: s.mirrorQueue, personaID: personaID},
		Payloads: mirrorPayloadBridge{repo: s.mirrorPayload},
		Adapter:  mirrorAdapterBridge{adapter: s.mirrorAdapter},
		IndexMap: mirrorIndexBridge{repo: s.mirrorIndex},
	})
	result, err := worker.RunOnce(ctx, limit)
	if err != nil {
		return nil, err
	}
	return &RunMirrorSyncResult{
		Claimed:   result.Claimed,
		Completed: result.Completed,
		Failed:    result.Failed,
		Skipped:   result.Skipped,
	}, nil
}

func (s *service) RebuildMirror(ctx context.Context, req RebuildMirrorRequest) (*RebuildMirrorResult, error) {
	if s.mirrorAdapter == nil {
		return nil, fmt.Errorf("%w: MirrorAdapter is required", ErrInvalidOptions)
	}
	namespace, ok := s.mirrorAdapter.(MirrorNamespaceAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: MirrorAdapter must support ClearNamespace", ErrInvalidOptions)
	}
	personaID := defaultString(req.PersonaID, s.persona)
	if err := s.ensurePersona(ctx, personaID); err != nil {
		return nil, err
	}
	if _, err := s.mirrorQueue.PrepareForPersonaRebuild(ctx, personaID, mirrorRebuildSupersedeReason); err != nil {
		if errors.Is(err, memsqlite.ErrMirrorQueuePersonaProcessingActive) {
			return nil, fmt.Errorf("%w: mirror rebuild blocked for persona %q because processing queue rows remain", ErrInvalidRequest, personaID)
		}
		return nil, err
	}
	if err := s.mirrorState.MarkRebuilding(ctx, personaID); err != nil {
		return nil, err
	}
	rebuilder := internalmirror.NewRebuilder(internalmirror.RebuilderOptions{
		Source:    mirrorPayloadBridge{repo: s.mirrorPayload},
		Adapter:   mirrorAdapterBridge{adapter: s.mirrorAdapter},
		Namespace: mirrorNamespaceBridge{namespace: namespace},
		IndexMap:  mirrorIndexBridge{repo: s.mirrorIndex},
	})
	result, err := rebuilder.Rebuild(ctx, personaID)
	if err != nil {
		if markErr := s.mirrorState.MarkDegraded(ctx, personaID, "rebuild failed"); markErr != nil {
			return nil, markErr
		}
		return nil, err
	}
	if result.Failed > 0 {
		if err := s.mirrorState.MarkDegraded(ctx, personaID, "rebuild completed with failed nodes"); err != nil {
			return nil, err
		}
	} else if err := s.mirrorState.MarkReady(ctx, personaID); err != nil {
		return nil, err
	}
	return &RebuildMirrorResult{
		NodesUpserted: result.NodesUpserted,
		EdgesUpserted: result.EdgesUpserted,
		Failed:        result.Failed,
		Skipped:       result.Skipped,
	}, nil
}

type mirrorQueueBridge struct {
	repo      *memsqlite.MirrorQueueRepository
	personaID string
}

func (b mirrorQueueBridge) Claim(ctx context.Context, limit int) ([]internalmirror.QueueRow, error) {
	rows, err := b.repo.ClaimForPersona(ctx, b.personaID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]internalmirror.QueueRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, internalmirror.QueueRow{
			ID:          row.ID,
			PersonaID:   row.PersonaID,
			NodeType:    row.NodeType,
			NodeID:      row.NodeID,
			Operation:   internalmirror.Operation(row.Operation),
			PayloadJSON: row.PayloadJSON,
		})
	}
	return result, nil
}

func (b mirrorQueueBridge) Complete(ctx context.Context, id string) error {
	return b.repo.Complete(ctx, id)
}

func (b mirrorQueueBridge) Fail(ctx context.Context, id string, message string) error {
	return b.repo.Fail(ctx, id, message)
}

type mirrorPayloadBridge struct {
	repo *memsqlite.MirrorPayloadRepository
}

func (b mirrorPayloadBridge) ListRebuildNodeRefs(ctx context.Context, personaID string) ([]internalmirror.NodeRef, error) {
	refs, err := b.repo.ListRebuildNodeRefs(ctx, personaID)
	if err != nil {
		return nil, err
	}
	out := make([]internalmirror.NodeRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, internalmirror.NodeRef{
			PersonaID:    ref.PersonaID,
			NodeType:     ref.NodeType,
			SQLiteNodeID: ref.SQLiteNodeID,
		})
	}
	return out, nil
}

func (b mirrorPayloadBridge) ListRebuildEdgeRefs(ctx context.Context, personaID string) ([]internalmirror.EdgeRef, error) {
	refs, err := b.repo.ListRebuildEdgeRefs(ctx, personaID)
	if err != nil {
		return nil, err
	}
	out := make([]internalmirror.EdgeRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, internalmirror.EdgeRef{
			PersonaID:    ref.PersonaID,
			SQLiteEdgeID: ref.SQLiteEdgeID,
		})
	}
	return out, nil
}

func (b mirrorPayloadBridge) BuildNodePayload(ctx context.Context, personaID string, nodeType string, nodeID string) (internalmirror.NodePayload, bool, error) {
	payload, ok, err := b.repo.BuildNodePayload(ctx, personaID, nodeType, nodeID)
	if err != nil || !ok {
		return internalmirror.NodePayload{}, ok, err
	}
	return internalmirror.NodePayload{
		PersonaID:      payload.PersonaID,
		NodeType:       payload.NodeType,
		SQLiteNodeID:   payload.SQLiteNodeID,
		SearchableText: payload.SearchableText,
		Payload:        payload.Payload,
	}, true, nil
}

func (b mirrorPayloadBridge) BuildEdgePayload(ctx context.Context, personaID string, edgeID string) (internalmirror.EdgePayload, bool, error) {
	payload, ok, err := b.repo.BuildEdgePayload(ctx, personaID, edgeID)
	if err != nil || !ok {
		return internalmirror.EdgePayload{}, ok, err
	}
	return internalmirror.EdgePayload{
		PersonaID:    payload.PersonaID,
		SQLiteEdgeID: payload.SQLiteEdgeID,
		LinkType:     payload.LinkType,
		FromNodeType: payload.FromNodeType,
		FromNodeID:   payload.FromNodeID,
		ToNodeType:   payload.ToNodeType,
		ToNodeID:     payload.ToNodeID,
		Direction:    payload.Direction,
		Confidence:   payload.Confidence,
		Weight:       payload.Weight,
		Payload:      payload.Payload,
	}, true, nil
}

type mirrorAdapterBridge struct {
	adapter MirrorAdapter
}

func (b mirrorAdapterBridge) UpsertNode(ctx context.Context, payload internalmirror.NodePayload) (internalmirror.NodeUpsertResult, error) {
	result, err := b.adapter.UpsertNode(ctx, MirrorNodePayload{
		PersonaID:      payload.PersonaID,
		NodeType:       payload.NodeType,
		SQLiteNodeID:   payload.SQLiteNodeID,
		SearchableText: payload.SearchableText,
		Payload:        payload.Payload,
	})
	return internalmirror.NodeUpsertResult{MirrorNodeID: result.MirrorNodeID}, err
}

func (b mirrorAdapterBridge) DeleteNode(ctx context.Context, ref internalmirror.NodeRef) error {
	return b.adapter.DeleteNode(ctx, MirrorNodeRef{
		PersonaID:    ref.PersonaID,
		NodeType:     ref.NodeType,
		SQLiteNodeID: ref.SQLiteNodeID,
	})
}

func (b mirrorAdapterBridge) UpsertEdge(ctx context.Context, payload internalmirror.EdgePayload) error {
	return b.adapter.UpsertEdge(ctx, MirrorEdgePayload{
		PersonaID:    payload.PersonaID,
		SQLiteEdgeID: payload.SQLiteEdgeID,
		LinkType:     payload.LinkType,
		FromNodeType: payload.FromNodeType,
		FromNodeID:   payload.FromNodeID,
		ToNodeType:   payload.ToNodeType,
		ToNodeID:     payload.ToNodeID,
		Direction:    payload.Direction,
		Confidence:   payload.Confidence,
		Weight:       payload.Weight,
		Payload:      payload.Payload,
	})
}

func (b mirrorAdapterBridge) DeleteEdge(ctx context.Context, ref internalmirror.EdgeRef) error {
	return b.adapter.DeleteEdge(ctx, MirrorEdgeRef{
		PersonaID:        ref.PersonaID,
		SQLiteEdgeID:     ref.SQLiteEdgeID,
		LinkType:         ref.LinkType,
		FromNodeType:     ref.FromNodeType,
		FromNodeID:       ref.FromNodeID,
		ToNodeType:       ref.ToNodeType,
		ToNodeID:         ref.ToNodeID,
		FromMirrorNodeID: ref.FromMirrorNodeID,
		ToMirrorNodeID:   ref.ToMirrorNodeID,
	})
}

type mirrorIndexBridge struct {
	repo *memsqlite.MirrorIndexRepository
}

func (b mirrorIndexBridge) MarkPersonaDeleted(ctx context.Context, personaID string) error {
	return b.repo.MarkPersonaDeleted(ctx, personaID)
}

func (b mirrorIndexBridge) MarkNodeIndexed(ctx context.Context, payload internalmirror.NodePayload, result internalmirror.NodeUpsertResult) error {
	return b.repo.RecordNodeIndexed(ctx, memsqlite.MirrorIndexedNode{
		PersonaID:     payload.PersonaID,
		NodeType:      payload.NodeType,
		NodeID:        payload.SQLiteNodeID,
		TriviumNodeID: result.MirrorNodeID,
	})
}

func (b mirrorIndexBridge) MarkNodeDeleted(ctx context.Context, ref internalmirror.NodeRef) error {
	return b.repo.MarkNodeDeleted(ctx, ref.PersonaID, ref.NodeType, ref.SQLiteNodeID)
}

func (b mirrorIndexBridge) MarkNodeFailed(ctx context.Context, ref internalmirror.NodeRef, message string) error {
	return b.repo.MarkNodeFailed(ctx, ref.PersonaID, ref.NodeType, ref.SQLiteNodeID, message)
}

type mirrorNamespaceBridge struct {
	namespace MirrorNamespaceAdapter
}

func (b mirrorNamespaceBridge) ClearNamespace(ctx context.Context, personaID string) error {
	return b.namespace.ClearNamespace(ctx, personaID)
}

type fakeMirrorAdapter struct{}

func NewFakeMirrorAdapter() MirrorAdapter {
	return fakeMirrorAdapter{}
}

func (fakeMirrorAdapter) UpsertNode(ctx context.Context, payload MirrorNodePayload) (MirrorNodeUpsertResult, error) {
	return MirrorNodeUpsertResult{MirrorNodeID: stableFakeMirrorID(payload.PersonaID, payload.NodeType, payload.SQLiteNodeID)}, nil
}

func (fakeMirrorAdapter) DeleteNode(ctx context.Context, ref MirrorNodeRef) error {
	return nil
}

func (fakeMirrorAdapter) UpsertEdge(ctx context.Context, payload MirrorEdgePayload) error {
	return nil
}

func (fakeMirrorAdapter) DeleteEdge(ctx context.Context, ref MirrorEdgeRef) error {
	return nil
}

func (fakeMirrorAdapter) ClearNamespace(ctx context.Context, personaID string) error {
	return nil
}

func (fakeMirrorAdapter) Rerank(ctx context.Context, req MirrorRerankRequest) (*MirrorRerankResult, error) {
	items := make([]MirrorRerankItem, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		items = append(items, MirrorRerankItem{
			NodeID:      candidate.NodeID,
			NodeType:    candidate.NodeType,
			RerankScore: 0,
			DebugReason: "fake reranker neutral score",
		})
	}
	return &MirrorRerankResult{Items: items}, nil
}

func stableFakeMirrorID(parts ...string) int64 {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	sum := hash.Sum(nil)
	const maxInt64 = uint64(1<<63 - 1)
	id := int64(binary.BigEndian.Uint64(sum[:8]) & maxInt64)
	if id == 0 {
		return 1
	}
	return id
}

type sidecarMirrorAdapter struct {
	client *internalmirror.SidecarClient
}

func NewSidecarMirrorAdapter(baseURL string) MirrorAdapter {
	return sidecarMirrorAdapter{
		client: internalmirror.NewSidecarClient(internalmirror.SidecarClientOptions{
			BaseURL: baseURL,
		}),
	}
}

func (a sidecarMirrorAdapter) UpsertNode(ctx context.Context, payload MirrorNodePayload) (MirrorNodeUpsertResult, error) {
	result, err := a.client.UpsertNode(ctx, internalmirror.NodePayload{
		PersonaID:      payload.PersonaID,
		NodeType:       payload.NodeType,
		SQLiteNodeID:   payload.SQLiteNodeID,
		SearchableText: payload.SearchableText,
		Payload:        payload.Payload,
	})
	return MirrorNodeUpsertResult{MirrorNodeID: result.MirrorNodeID}, err
}

func (a sidecarMirrorAdapter) DeleteNode(ctx context.Context, ref MirrorNodeRef) error {
	return a.client.DeleteNode(ctx, internalmirror.NodeRef{
		PersonaID:    ref.PersonaID,
		NodeType:     ref.NodeType,
		SQLiteNodeID: ref.SQLiteNodeID,
	})
}

func (a sidecarMirrorAdapter) UpsertEdge(ctx context.Context, payload MirrorEdgePayload) error {
	return a.client.UpsertEdge(ctx, internalmirror.EdgePayload{
		PersonaID:    payload.PersonaID,
		SQLiteEdgeID: payload.SQLiteEdgeID,
		LinkType:     payload.LinkType,
		FromNodeType: payload.FromNodeType,
		FromNodeID:   payload.FromNodeID,
		ToNodeType:   payload.ToNodeType,
		ToNodeID:     payload.ToNodeID,
		Direction:    payload.Direction,
		Confidence:   payload.Confidence,
		Weight:       payload.Weight,
		Payload:      payload.Payload,
	})
}

func (a sidecarMirrorAdapter) DeleteEdge(ctx context.Context, ref MirrorEdgeRef) error {
	return a.client.DeleteEdge(ctx, internalmirror.EdgeRef{
		PersonaID:        ref.PersonaID,
		SQLiteEdgeID:     ref.SQLiteEdgeID,
		LinkType:         ref.LinkType,
		FromNodeType:     ref.FromNodeType,
		FromNodeID:       ref.FromNodeID,
		ToNodeType:       ref.ToNodeType,
		ToNodeID:         ref.ToNodeID,
		FromMirrorNodeID: ref.FromMirrorNodeID,
		ToMirrorNodeID:   ref.ToMirrorNodeID,
	})
}

func (a sidecarMirrorAdapter) ClearNamespace(ctx context.Context, personaID string) error {
	return a.client.ClearNamespace(ctx, personaID)
}

func (a sidecarMirrorAdapter) Health(ctx context.Context) error {
	return a.client.Health(ctx)
}

func (a sidecarMirrorAdapter) FindCandidates(ctx context.Context, req MirrorCandidateRequest) (*MirrorCandidateResult, error) {
	result, err := a.client.FindCandidates(ctx, internalmirror.CandidateRequest{
		PersonaID: req.PersonaID,
		QueryText: req.QueryText,
		Query:     queryAnalysisPublicToMirror(req.Query),
		Limit:     req.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := &MirrorCandidateResult{
		Candidates:             make([]MirrorCandidate, 0, len(result.Candidates)),
		Degraded:               result.Degraded,
		FallbackReason:         result.FallbackReason,
		EmbeddingCacheHits:     result.EmbeddingCacheHits,
		EmbeddingCacheMisses:   result.EmbeddingCacheMisses,
		EmbeddingLiveCallCount: result.EmbeddingLiveCallCount,
		Diagnostics:            mirrorCandidateDiagnosticsFromInternal(result.Diagnostics),
	}
	for _, candidate := range result.Candidates {
		out.Candidates = append(out.Candidates, MirrorCandidate{
			TriviumNodeID:   candidate.TriviumNodeID,
			Score:           candidate.Score,
			Source:          candidate.Source,
			PrimaryPurpose:  candidate.PrimaryPurpose,
			Rank:            candidate.Rank,
			HitCount:        candidate.HitCount,
			SourceBreakdown: mirrorCandidateSourceBreakdownFromInternal(candidate.SourceBreakdown),
		})
	}
	return out, nil
}

func mirrorCandidateSourceBreakdownFromInternal(values []internalmirror.CandidateSourceBreakdown) []MirrorCandidateSourceBreakdown {
	result := make([]MirrorCandidateSourceBreakdown, 0, len(values))
	for _, value := range values {
		result = append(result, MirrorCandidateSourceBreakdown{
			Source:  value.Source,
			Purpose: value.Purpose,
			Rank:    value.Rank,
			Score:   value.Score,
			Weight:  value.Weight,
		})
	}
	return result
}

func (a sidecarMirrorAdapter) ActivateGraph(ctx context.Context, req MirrorActivationRequest) (*MirrorActivationResult, error) {
	result, err := a.client.ActivateGraph(ctx, internalmirror.ActivationRequest{
		PersonaID: req.PersonaID,
		Seeds:     activationSeedsToInternal(req.Seeds),
		Params: internalmirror.ActivationParams{
			MaxHops:                   req.Params.MaxHops,
			HopDecay:                  req.Params.HopDecay,
			MinEnergy:                 req.Params.MinEnergy,
			MaxActiveNodes:            req.Params.MaxActiveNodes,
			HubSuppressionPower:       req.Params.HubSuppressionPower,
			IncludePaths:              req.Params.IncludePaths,
			MaxEdgesScannedPerRequest: req.Params.MaxEdgesScannedPerRequest,
			MaxNeighborsPerNode:       req.Params.MaxNeighborsPerNode,
			MaxActivationWallMs:       req.Params.MaxActivationWallMs,
		},
	})
	if err != nil {
		return nil, err
	}
	out := &MirrorActivationResult{
		Candidates:     make([]MirrorActivationCandidate, 0, len(result.Candidates)),
		Degraded:       result.Degraded,
		FallbackReason: result.FallbackReason,
	}
	for _, candidate := range result.Candidates {
		out.Candidates = append(out.Candidates, MirrorActivationCandidate{
			TriviumNodeID: candidate.TriviumNodeID,
			Score:         candidate.Score,
			Source:        candidate.Source,
			Rank:          candidate.Rank,
			Paths:         activationPathsFromInternal(candidate.Paths),
		})
	}
	return out, nil
}

func (a sidecarMirrorAdapter) Rerank(ctx context.Context, req MirrorRerankRequest) (*MirrorRerankResult, error) {
	result, err := a.client.Rerank(ctx, internalmirror.RerankRequest{
		PersonaID:  req.PersonaID,
		QueryText:  req.QueryText,
		Candidates: rerankCandidatesToInternal(req.Candidates),
	})
	if err != nil {
		return nil, err
	}
	out := &MirrorRerankResult{
		Items:          make([]MirrorRerankItem, 0, len(result.Items)),
		Degraded:       result.Degraded,
		FallbackReason: result.FallbackReason,
	}
	for _, item := range result.Items {
		out.Items = append(out.Items, MirrorRerankItem{
			NodeID:      item.NodeID,
			NodeType:    item.NodeType,
			RerankScore: item.RerankScore,
			DebugReason: item.DebugReason,
		})
	}
	return out, nil
}

func (a sidecarMirrorAdapter) ConfigureEval(ctx context.Context, req MirrorEvalConfigRequest) (*MirrorEvalConfigResult, error) {
	result, err := a.client.ConfigureEval(ctx, internalmirror.EvalConfigRequest{
		TriviumDir:               req.TriviumDir,
		EmbeddingCacheMode:       req.EmbeddingCacheMode,
		EmbeddingCacheDBPath:     req.EmbeddingCacheDBPath,
		SearchableTextVersion:    req.SearchableTextVersion,
		TextNormalizationVersion: req.TextNormalizationVersion,
	})
	if err != nil {
		return nil, err
	}
	return &MirrorEvalConfigResult{
		TriviumDir:              result.TriviumDir,
		EmbeddingCacheMode:      result.EmbeddingCacheMode,
		EmbeddingCacheDBPath:    result.EmbeddingCacheDBPath,
		Embedding:               cloneStringMap(result.Embedding),
		TriviumAdapterVersion:   result.TriviumAdapterVersion,
		TriviumDBVersion:        result.TriviumDBVersion,
		RerankProviderAvailable: result.RerankProviderAvailable,
		RerankProviderMode:      result.RerankProviderMode,
		RerankCapabilityReason:  result.RerankCapabilityReason,
		RerankCache:             result.RerankCache,
		MirrorStatsAvailable:    result.MirrorStatsAvailable,
		MirrorStatsError:        result.MirrorStatsError,
		MirrorNodeCount:         result.MirrorNodeCount,
		MirrorEdgeCount:         result.MirrorEdgeCount,
	}, nil
}

func activationSeedsToInternal(seeds []MirrorActivationSeed) []internalmirror.ActivationSeed {
	result := make([]internalmirror.ActivationSeed, 0, len(seeds))
	for _, seed := range seeds {
		result = append(result, internalmirror.ActivationSeed{
			TriviumNodeID: seed.TriviumNodeID,
			SQLiteNodeID:  seed.SQLiteNodeID,
			NodeType:      seed.NodeType,
			SeedEnergy:    seed.SeedEnergy,
		})
	}
	return result
}

func rerankCandidatesToInternal(candidates []MirrorRerankCandidate) []internalmirror.RerankCandidate {
	result := make([]internalmirror.RerankCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, internalmirror.RerankCandidate{
			NodeID:       candidate.NodeID,
			NodeType:     candidate.NodeType,
			SafeSummary:  candidate.SafeSummary,
			CurrentScore: candidate.CurrentScore,
			AnchorEnergy: candidate.AnchorEnergy,
			GraphEnergy:  candidate.GraphEnergy,
			SourceScores: cloneFloatMap(candidate.SourceScores),
		})
	}
	return result
}

func activationPathsFromInternal(paths []internalmirror.ActivationPath) []MirrorActivationPath {
	result := make([]MirrorActivationPath, 0, len(paths))
	for _, path := range paths {
		result = append(result, MirrorActivationPath{
			TriviumNodeIDs: append([]int64(nil), path.TriviumNodeIDs...),
			LinkTypes:      append([]string(nil), path.LinkTypes...),
		})
	}
	return result
}

func queryAnalysisPublicToMirror(value QueryAnalysis) internalmirror.QueryAnalysis {
	out := internalmirror.QueryAnalysis{
		Raw:               value.Raw,
		Normalized:        value.Normalized,
		Terms:             append([]string(nil), value.Terms...),
		TimeMode:          string(value.TimeMode),
		MemoryDomain:      string(value.MemoryDomain),
		MemoryAbility:     string(value.MemoryAbility),
		EvidenceNeed:      string(value.EvidenceNeed),
		Source:            string(value.Source),
		Confidence:        value.Confidence,
		FieldConfidence:   queryAnalysisConfidencePublicToMirror(value.FieldConfidence),
		Scores:            queryAnalysisScoresPublicToMirror(value.Scores),
		Probes:            queryAnchorProbePublicToMirror(value.Probes),
		Decision:          queryAnalysisDecisionPublicToMirror(value.Decision),
		Evidence:          queryAnalysisEvidencePublicToMirror(value.Evidence),
		Alternatives:      queryAnalysisAlternativesPublicToMirror(value.Alternatives),
		ContextBlockHints: append([]string(nil), value.ContextBlockHints...),
		PolicyHints:       queryPolicyHintsPublicToMirror(value.PolicyHints),
	}
	for _, signal := range value.Signals {
		out.Signals = append(out.Signals, string(signal))
	}
	for _, mention := range value.EntityMentions {
		out.EntityMentions = append(out.EntityMentions, internalmirror.QueryEntityMention{
			EntityID:      mention.EntityID,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
			MatchKind:     string(mention.MatchKind),
		})
	}
	for _, rewrite := range value.QueryRewrites {
		out.QueryRewrites = append(out.QueryRewrites, internalmirror.QueryRewrite{
			Text:    rewrite.Text,
			Purpose: rewrite.Purpose,
			Weight:  rewrite.Weight,
		})
	}
	for _, anchor := range value.SemanticAnchors {
		out.SemanticAnchors = append(out.SemanticAnchors, internalmirror.SemanticAnchor{
			Text:       anchor.Text,
			AnchorType: anchor.AnchorType,
			EntityID:   anchor.EntityID,
			Weight:     anchor.Weight,
			Confidence: anchor.Confidence,
		})
	}
	return out
}

func queryAnalysisConfidencePublicToMirror(value QueryAnalysisConfidence) internalmirror.QueryAnalysisConfidence {
	return internalmirror.QueryAnalysisConfidence{
		Overall:          value.Overall,
		TimeMode:         value.TimeMode,
		MemoryAbility:    value.MemoryAbility,
		MemoryDomain:     value.MemoryDomain,
		EvidenceNeed:     value.EvidenceNeed,
		EntityResolution: value.EntityResolution,
	}
}

func queryAnalysisScoresPublicToMirror(value QueryAnalysisScores) internalmirror.QueryAnalysisScores {
	return internalmirror.QueryAnalysisScores{
		RuleFit:                     value.RuleFit,
		AnchorReadiness:             value.AnchorReadiness,
		ExpectedRetrievalConfidence: value.ExpectedRetrievalConfidence,
		SemanticNeed:                value.SemanticNeed,
		Complexity:                  value.Complexity,
		Ambiguity:                   value.Ambiguity,
		Specificity:                 value.Specificity,
		SafetyRisk:                  value.SafetyRisk,
		IntentEvidence:              value.IntentEvidence,
		TimeEvidence:                value.TimeEvidence,
		DomainEvidence:              value.DomainEvidence,
		EvidenceNeedEvidence:        value.EvidenceNeedEvidence,
		EntityResolution:            value.EntityResolution,
		FieldConsistency:            value.FieldConsistency,
		DefaultFallbackPenalty:      value.DefaultFallbackPenalty,
		MultiIntentConflictPenalty:  value.MultiIntentConflictPenalty,
		SensitivityPenalty:          value.SensitivityPenalty,
	}
}

func queryAnchorProbePublicToMirror(value QueryAnchorProbe) internalmirror.QueryAnchorProbe {
	return internalmirror.QueryAnchorProbe{
		EntityExactConf:        value.EntityExactConf,
		EntityAmbiguity:        value.EntityAmbiguity,
		SparseProbeConf:        value.SparseProbeConf,
		PredicateProbeConf:     value.PredicateProbeConf,
		RecentProbeConf:        value.RecentProbeConf,
		PinnedCoreProbeConf:    value.PinnedCoreProbeConf,
		NarrativeProbeConf:     value.NarrativeProbeConf,
		FallbackSearchHitCount: value.FallbackSearchHitCount,
		Top1Score:              value.Top1Score,
		Top2Score:              value.Top2Score,
		Top1Margin:             value.Top1Margin,
		Breakdown:              queryAnchorProbeBreakdownPublicToMirror(value.Breakdown),
	}
}

func queryAnchorProbeBreakdownPublicToMirror(values []QueryAnchorProbeBreakdown) []internalmirror.QueryAnchorProbeBreakdown {
	if len(values) == 0 {
		return nil
	}
	out := make([]internalmirror.QueryAnchorProbeBreakdown, 0, len(values))
	for _, value := range values {
		out = append(out, internalmirror.QueryAnchorProbeBreakdown{
			Source:      value.Source,
			Confidence:  value.Confidence,
			HitCount:    value.HitCount,
			TopScore:    value.TopScore,
			SecondScore: value.SecondScore,
			Reason:      value.Reason,
			Error:       value.Error,
		})
	}
	return out
}

func queryAnalysisDecisionPublicToMirror(value QueryAnalysisDecision) internalmirror.QueryAnalysisDecision {
	return internalmirror.QueryAnalysisDecision{
		UseSemantic:      value.UseSemantic,
		SemanticMode:     value.SemanticMode,
		RetrievalMode:    value.RetrievalMode,
		ReasonCodes:      append([]string(nil), value.ReasonCodes...),
		ThresholdVersion: value.ThresholdVersion,
		ScorerVersion:    value.ScorerVersion,
	}
}

func queryAnalysisEvidencePublicToMirror(values []QueryAnalysisEvidence) []internalmirror.QueryAnalysisEvidence {
	if len(values) == 0 {
		return nil
	}
	out := make([]internalmirror.QueryAnalysisEvidence, 0, len(values))
	for _, value := range values {
		out = append(out, internalmirror.QueryAnalysisEvidence{
			Field:     value.Field,
			Signal:    value.Signal,
			MatchText: value.MatchText,
			SpanStart: value.SpanStart,
			SpanEnd:   value.SpanEnd,
			Weight:    value.Weight,
			Detector:  value.Detector,
		})
	}
	return out
}

func queryAnalysisAlternativesPublicToMirror(values []QueryAnalysisAlternative) []internalmirror.QueryAnalysisAlternative {
	if len(values) == 0 {
		return nil
	}
	out := make([]internalmirror.QueryAnalysisAlternative, 0, len(values))
	for _, value := range values {
		out = append(out, internalmirror.QueryAnalysisAlternative{
			Field:       value.Field,
			Value:       value.Value,
			Confidence:  value.Confidence,
			ReasonCodes: append([]string(nil), value.ReasonCodes...),
			Detector:    value.Detector,
		})
	}
	return out
}

func queryPolicyHintsPublicToMirror(value QueryPolicyHints) internalmirror.QueryPolicyHints {
	return internalmirror.QueryPolicyHints{
		PreferEvidencedByLinks: value.PreferEvidencedByLinks,
		PreferSupersedesLinks:  value.PreferSupersedesLinks,
		PreferCausalLinks:      value.PreferCausalLinks,
		PreferCounterexamples:  value.PreferCounterexamples,
		PreferNarratives:       value.PreferNarratives,
		MaxHopsHint:            value.MaxHopsHint,
	}
}

func mirrorCandidateDiagnosticsFromInternal(value internalmirror.CandidateDiagnostics) MirrorCandidateSidecarDiagnostics {
	out := MirrorCandidateSidecarDiagnostics{
		QueryCount:                   value.QueryCount,
		RawQueryCount:                value.RawQueryCount,
		RewriteQueryCount:            value.RewriteQueryCount,
		AnchorQueryCount:             value.AnchorQueryCount,
		MergedCandidateCount:         value.MergedCandidateCount,
		QueryTrimCount:               value.QueryTrimCount,
		DenseEmbeddingWallLatencyMs:  value.DenseEmbeddingWallLatencyMs,
		DenseEmbeddingBatchLatencyMs: value.DenseEmbeddingBatchLatencyMs,
		DenseSearchTotalLatencyMs:    value.DenseSearchTotalLatencyMs,
		QueryCountTrimmedByBudget:    value.QueryCountTrimmedByBudget,
		PerQuery:                     make([]MirrorCandidatePerQueryDiagnostic, 0, len(value.PerQuery)),
	}
	for _, item := range value.PerQuery {
		out.PerQuery = append(out.PerQuery, MirrorCandidatePerQueryDiagnostic{
			Source:    item.Source,
			Purpose:   item.Purpose,
			Count:     item.Count,
			LatencyMs: item.LatencyMs,
		})
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
