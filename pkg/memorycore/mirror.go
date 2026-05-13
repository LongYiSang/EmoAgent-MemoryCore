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

func (a sidecarMirrorAdapter) FindCandidates(ctx context.Context, req MirrorCandidateRequest) (*MirrorCandidateResult, error) {
	result, err := a.client.FindCandidates(ctx, internalmirror.CandidateRequest{
		PersonaID: req.PersonaID,
		QueryText: req.QueryText,
		Limit:     req.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := &MirrorCandidateResult{
		Candidates:     make([]MirrorCandidate, 0, len(result.Candidates)),
		Degraded:       result.Degraded,
		FallbackReason: result.FallbackReason,
	}
	for _, candidate := range result.Candidates {
		out.Candidates = append(out.Candidates, MirrorCandidate{
			TriviumNodeID: candidate.TriviumNodeID,
			Score:         candidate.Score,
			Source:        candidate.Source,
		})
	}
	return out, nil
}
