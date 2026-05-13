package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Worker struct {
	queue    Queue
	payloads PayloadBuilder
	adapter  MirrorAdapter
	indexMap IndexMap
}

func NewWorker(options WorkerOptions) *Worker {
	return &Worker{
		queue:    options.Queue,
		payloads: options.Payloads,
		adapter:  options.Adapter,
		indexMap: options.IndexMap,
	}
}

func (w *Worker) RunOnce(ctx context.Context, limit int) (Result, error) {
	if err := w.validate(); err != nil {
		return Result{}, err
	}

	rows, err := w.queue.Claim(ctx, limit)
	if err != nil {
		return Result{}, err
	}

	result := Result{Claimed: len(rows)}
	for _, row := range rows {
		completed, skipped, err := w.processRow(ctx, row)
		if err != nil {
			if failErr := w.queue.Fail(ctx, row.ID, err.Error()); failErr != nil {
				return result, failErr
			}
			result.Failed++
			continue
		}
		if completed {
			result.Completed++
		}
		if skipped {
			result.Skipped++
		}
	}
	return result, nil
}

func (w *Worker) validate() error {
	if w.queue == nil {
		return errors.New("mirror worker requires queue")
	}
	if w.payloads == nil {
		return errors.New("mirror worker requires payload builder")
	}
	if w.adapter == nil {
		return errors.New("mirror worker requires adapter")
	}
	return nil
}

func (w *Worker) processRow(ctx context.Context, row QueueRow) (completed bool, skipped bool, err error) {
	switch row.Operation {
	case OperationUpsertNode:
		return w.processUpsertNode(ctx, row)
	case OperationDeleteNode:
		return w.processDeleteNode(ctx, row)
	case OperationUpsertEdge:
		return w.processUpsertEdge(ctx, row)
	case OperationDeleteEdge:
		return w.processDeleteEdge(ctx, row)
	default:
		return false, false, fmt.Errorf("unsupported mirror operation %q", row.Operation)
	}
}

func (w *Worker) processUpsertNode(ctx context.Context, row QueueRow) (bool, bool, error) {
	payload, ok, err := w.payloads.BuildNodePayload(ctx, row.PersonaID, row.NodeType, row.NodeID)
	if err != nil {
		return false, false, err
	}
	if !ok {
		if err := w.queue.Complete(ctx, row.ID); err != nil {
			return false, false, err
		}
		return true, true, nil
	}

	adapterResult, err := w.adapter.UpsertNode(ctx, payload)
	if err != nil {
		return false, false, err
	}
	if w.indexMap != nil {
		if err := w.indexMap.MarkNodeIndexed(ctx, payload, adapterResult); err != nil {
			return false, false, err
		}
	}
	if err := w.queue.Complete(ctx, row.ID); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (w *Worker) processDeleteNode(ctx context.Context, row QueueRow) (bool, bool, error) {
	ref := NodeRef{
		PersonaID:    row.PersonaID,
		NodeType:     row.NodeType,
		SQLiteNodeID: row.NodeID,
	}
	if err := w.adapter.DeleteNode(ctx, ref); err != nil {
		return false, false, err
	}
	if w.indexMap != nil {
		if err := w.indexMap.MarkNodeDeleted(ctx, ref); err != nil {
			return false, false, err
		}
	}
	if err := w.queue.Complete(ctx, row.ID); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (w *Worker) processUpsertEdge(ctx context.Context, row QueueRow) (bool, bool, error) {
	payload, ok, err := w.payloads.BuildEdgePayload(ctx, row.PersonaID, row.NodeID)
	if err != nil {
		return false, false, err
	}
	if !ok {
		if err := w.queue.Complete(ctx, row.ID); err != nil {
			return false, false, err
		}
		return true, true, nil
	}
	if err := w.adapter.UpsertEdge(ctx, payload); err != nil {
		return false, false, err
	}
	if err := w.queue.Complete(ctx, row.ID); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (w *Worker) processDeleteEdge(ctx context.Context, row QueueRow) (bool, bool, error) {
	ref, err := deleteEdgeRefFromQueueRow(row)
	if err != nil {
		return false, false, err
	}
	if err := w.adapter.DeleteEdge(ctx, ref); err != nil {
		return false, false, err
	}
	if err := w.queue.Complete(ctx, row.ID); err != nil {
		return false, false, err
	}
	return true, false, nil
}

type deleteEdgePayload struct {
	PersonaID        string `json:"persona_id"`
	SQLiteEdgeID     string `json:"sqlite_edge_id"`
	LinkType         string `json:"link_type"`
	FromNodeType     string `json:"from_node_type"`
	FromNodeID       string `json:"from_node_id"`
	ToNodeType       string `json:"to_node_type"`
	ToNodeID         string `json:"to_node_id"`
	FromMirrorNodeID *int64 `json:"from_mirror_node_id,omitempty"`
	ToMirrorNodeID   *int64 `json:"to_mirror_node_id,omitempty"`
}

func deleteEdgeRefFromQueueRow(row QueueRow) (EdgeRef, error) {
	payloadJSON := strings.TrimSpace(row.PayloadJSON)
	if payloadJSON == "" {
		return EdgeRef{}, fmt.Errorf("delete_edge row %s missing payload_json", row.ID)
	}
	var payload deleteEdgePayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return EdgeRef{}, fmt.Errorf("delete_edge row %s invalid payload_json: %w", row.ID, err)
	}
	if strings.TrimSpace(payload.PersonaID) == "" ||
		strings.TrimSpace(payload.SQLiteEdgeID) == "" ||
		strings.TrimSpace(payload.LinkType) == "" ||
		strings.TrimSpace(payload.FromNodeType) == "" ||
		strings.TrimSpace(payload.FromNodeID) == "" ||
		strings.TrimSpace(payload.ToNodeType) == "" ||
		strings.TrimSpace(payload.ToNodeID) == "" {
		return EdgeRef{}, fmt.Errorf("delete_edge row %s payload_json missing required fields", row.ID)
	}
	if payload.PersonaID != row.PersonaID {
		return EdgeRef{}, fmt.Errorf("delete_edge row %s payload_json persona_id mismatch", row.ID)
	}
	if payload.SQLiteEdgeID != row.NodeID {
		return EdgeRef{}, fmt.Errorf("delete_edge row %s payload_json sqlite_edge_id mismatch", row.ID)
	}
	return EdgeRef{
		PersonaID:        payload.PersonaID,
		SQLiteEdgeID:     payload.SQLiteEdgeID,
		LinkType:         payload.LinkType,
		FromNodeType:     payload.FromNodeType,
		FromNodeID:       payload.FromNodeID,
		ToNodeType:       payload.ToNodeType,
		ToNodeID:         payload.ToNodeID,
		FromMirrorNodeID: payload.FromMirrorNodeID,
		ToMirrorNodeID:   payload.ToMirrorNodeID,
	}, nil
}
