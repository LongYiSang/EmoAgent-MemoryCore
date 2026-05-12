package mirror

import (
	"context"
	"errors"
	"fmt"
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
	ref := EdgeRef{
		PersonaID:    row.PersonaID,
		SQLiteEdgeID: row.NodeID,
	}
	if err := w.adapter.DeleteEdge(ctx, ref); err != nil {
		return false, false, err
	}
	if err := w.queue.Complete(ctx, row.ID); err != nil {
		return false, false, err
	}
	return true, false, nil
}
