package memorycore

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	internalmirror "github.com/longyisang/emoagent-memorycore/internal/mirror"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

const defaultMirrorSyncLimit = 100

func (s *service) RunMirrorSync(ctx context.Context, req RunMirrorSyncRequest) (*RunMirrorSyncResult, error) {
	if s.mirrorAdapter == nil {
		return nil, fmt.Errorf("%w: MirrorAdapter is required", ErrInvalidOptions)
	}
	personaID := defaultString(req.PersonaID, s.persona)
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
			ID:        row.ID,
			PersonaID: row.PersonaID,
			NodeType:  row.NodeType,
			NodeID:    row.NodeID,
			Operation: internalmirror.Operation(row.Operation),
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
		PersonaID:    ref.PersonaID,
		SQLiteEdgeID: ref.SQLiteEdgeID,
	})
}

type mirrorIndexBridge struct {
	repo *memsqlite.MirrorIndexRepository
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
