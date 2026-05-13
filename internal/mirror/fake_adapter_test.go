package mirror

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

func TestWorkerProcessesAllSupportedOperations(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{rows: []QueueRow{
		{ID: "q1", PersonaID: "p1", NodeType: "fact", NodeID: "fact-1", Operation: OperationUpsertNode},
		{ID: "q2", PersonaID: "p1", NodeType: "fact", NodeID: "fact-1", Operation: OperationDeleteNode},
		{ID: "q3", PersonaID: "p1", NodeType: "edge", NodeID: "edge-1", Operation: OperationUpsertEdge},
		{
			ID:          "q4",
			PersonaID:   "p1",
			NodeType:    "edge",
			NodeID:      "edge-1",
			Operation:   OperationDeleteEdge,
			PayloadJSON: `{"persona_id":"p1","sqlite_edge_id":"edge-1","link_type":"ABOUT_ENTITY","from_node_type":"fact","from_node_id":"fact-1","to_node_type":"entity","to_node_id":"entity-1"}`,
		},
	}}
	payloads := &fakePayloadBuilder{
		nodes: map[string]fakeNodePayloadResult{
			"p1/fact/fact-1": {
				payload: NodePayload{
					PersonaID:      "p1",
					NodeType:       "fact",
					SQLiteNodeID:   "fact-1",
					SearchableText: "safe text",
					Payload:        map[string]any{"node_type": "fact"},
				},
				ok: true,
			},
		},
		edges: map[string]fakeEdgePayloadResult{
			"p1/edge-1": {
				payload: EdgePayload{
					PersonaID:    "p1",
					SQLiteEdgeID: "edge-1",
					LinkType:     "ABOUT_ENTITY",
					FromNodeType: "fact",
					FromNodeID:   "fact-1",
					ToNodeType:   "entity",
					ToNodeID:     "entity-1",
					Direction:    "out",
					Confidence:   0.9,
					Weight:       1.2,
					Payload:      map[string]any{"link_type": "ABOUT_ENTITY"},
				},
				ok: true,
			},
		},
	}
	adapter := &fakeMirrorAdapter{nodeMirrorID: 1001}
	index := &fakeIndexMap{}

	result, err := NewWorker(WorkerOptions{
		Queue:    queue,
		Payloads: payloads,
		Adapter:  adapter,
		IndexMap: index,
	}).RunOnce(ctx, 10)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	assertEqual(t, result.Claimed, 4, "claimed")
	assertEqual(t, result.Completed, 4, "completed")
	assertEqual(t, result.Failed, 0, "failed")
	assertStrings(t, adapter.calls, []string{
		"upsert_node:p1:fact:fact-1",
		"delete_node:p1:fact:fact-1",
		"upsert_edge:p1:edge-1",
		"delete_edge:p1:edge-1",
	})
	assertStrings(t, queue.completed, []string{"q1", "q2", "q3", "q4"})
	assertStrings(t, queue.failed, nil)
	assertStrings(t, index.calls, []string{
		"indexed:p1:fact:fact-1:1001",
		"deleted:p1:fact:fact-1",
	})
	if len(adapter.deleteEdgeRefs) != 1 {
		t.Fatalf("delete edge refs = %d, want 1", len(adapter.deleteEdgeRefs))
	}
	ref := adapter.deleteEdgeRefs[0]
	if ref.LinkType != "ABOUT_ENTITY" || ref.FromNodeType != "fact" || ref.FromNodeID != "fact-1" || ref.ToNodeType != "entity" || ref.ToNodeID != "entity-1" {
		t.Fatalf("delete edge ref = %#v", ref)
	}
}

func TestWorkerFailsRowWhenAdapterFails(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{rows: []QueueRow{
		{ID: "q1", PersonaID: "p1", NodeType: "fact", NodeID: "fact-1", Operation: OperationUpsertNode},
	}}
	payloads := &fakePayloadBuilder{
		nodes: map[string]fakeNodePayloadResult{
			"p1/fact/fact-1": {
				payload: NodePayload{PersonaID: "p1", NodeType: "fact", SQLiteNodeID: "fact-1", SearchableText: "safe"},
				ok:      true,
			},
		},
	}
	adapter := &fakeMirrorAdapter{err: errors.New("sidecar unavailable")}
	index := &fakeIndexMap{}

	result, err := NewWorker(WorkerOptions{
		Queue:    queue,
		Payloads: payloads,
		Adapter:  adapter,
		IndexMap: index,
	}).RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	assertEqual(t, result.Claimed, 1, "claimed")
	assertEqual(t, result.Completed, 0, "completed")
	assertEqual(t, result.Failed, 1, "failed")
	assertStrings(t, queue.completed, nil)
	assertStrings(t, queue.failed, []string{"q1:sidecar unavailable"})
	assertStrings(t, index.calls, nil)
}

func TestWorkerCompletesIneligibleNodeWithoutAdapterCall(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{rows: []QueueRow{
		{ID: "q1", PersonaID: "p1", NodeType: "fact", NodeID: "fact-hidden", Operation: OperationUpsertNode},
	}}
	payloads := &fakePayloadBuilder{
		nodes: map[string]fakeNodePayloadResult{
			"p1/fact/fact-hidden": {ok: false},
		},
	}
	adapter := &fakeMirrorAdapter{}

	result, err := NewWorker(WorkerOptions{
		Queue:    queue,
		Payloads: payloads,
		Adapter:  adapter,
	}).RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}

	assertEqual(t, result.Claimed, 1, "claimed")
	assertEqual(t, result.Completed, 1, "completed")
	assertEqual(t, result.Skipped, 1, "skipped")
	assertEqual(t, result.Failed, 0, "failed")
	assertStrings(t, adapter.calls, nil)
	assertStrings(t, queue.completed, []string{"q1"})
	assertStrings(t, queue.failed, nil)
}

func TestWorkerFailsDeleteEdgeRowWithoutRichPayload(t *testing.T) {
	ctx := context.Background()
	queue := &fakeQueue{rows: []QueueRow{
		{ID: "q1", PersonaID: "p1", NodeType: "memory_link", NodeID: "edge-1", Operation: OperationDeleteEdge},
	}}
	adapter := &fakeMirrorAdapter{}

	result, err := NewWorker(WorkerOptions{
		Queue:    queue,
		Payloads: &fakePayloadBuilder{},
		Adapter:  adapter,
	}).RunOnce(ctx, 1)
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if result.Claimed != 1 || result.Completed != 0 || result.Failed != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(queue.failed) != 1 {
		t.Fatalf("failed rows = %#v, want one row failure", queue.failed)
	}
	if !strings.Contains(queue.failed[0], "payload_json") {
		t.Fatalf("failed row message = %q, want payload_json validation error", queue.failed[0])
	}
	assertStrings(t, queue.completed, nil)
	assertStrings(t, adapter.calls, nil)
}

type fakeQueue struct {
	rows      []QueueRow
	completed []string
	failed    []string
}

func (f *fakeQueue) Claim(ctx context.Context, limit int) ([]QueueRow, error) {
	if limit < len(f.rows) {
		return append([]QueueRow(nil), f.rows[:limit]...), nil
	}
	return append([]QueueRow(nil), f.rows...), nil
}

func (f *fakeQueue) Complete(ctx context.Context, id string) error {
	f.completed = append(f.completed, id)
	return nil
}

func (f *fakeQueue) Fail(ctx context.Context, id string, message string) error {
	f.failed = append(f.failed, id+":"+message)
	return nil
}

type fakePayloadBuilder struct {
	nodes map[string]fakeNodePayloadResult
	edges map[string]fakeEdgePayloadResult
}

type fakeNodePayloadResult struct {
	payload NodePayload
	ok      bool
	err     error
}

type fakeEdgePayloadResult struct {
	payload EdgePayload
	ok      bool
	err     error
}

func (f *fakePayloadBuilder) BuildNodePayload(ctx context.Context, personaID string, nodeType string, nodeID string) (NodePayload, bool, error) {
	result := f.nodes[personaID+"/"+nodeType+"/"+nodeID]
	return result.payload, result.ok, result.err
}

func (f *fakePayloadBuilder) BuildEdgePayload(ctx context.Context, personaID string, edgeID string) (EdgePayload, bool, error) {
	result := f.edges[personaID+"/"+edgeID]
	return result.payload, result.ok, result.err
}

type fakeMirrorAdapter struct {
	nodeMirrorID   int64
	err            error
	calls          []string
	deleteEdgeRefs []EdgeRef
}

func (f *fakeMirrorAdapter) UpsertNode(ctx context.Context, payload NodePayload) (NodeUpsertResult, error) {
	f.calls = append(f.calls, "upsert_node:"+payload.PersonaID+":"+payload.NodeType+":"+payload.SQLiteNodeID)
	if f.err != nil {
		return NodeUpsertResult{}, f.err
	}
	return NodeUpsertResult{MirrorNodeID: f.nodeMirrorID}, nil
}

func (f *fakeMirrorAdapter) DeleteNode(ctx context.Context, ref NodeRef) error {
	f.calls = append(f.calls, "delete_node:"+ref.PersonaID+":"+ref.NodeType+":"+ref.SQLiteNodeID)
	return f.err
}

func (f *fakeMirrorAdapter) UpsertEdge(ctx context.Context, payload EdgePayload) error {
	f.calls = append(f.calls, "upsert_edge:"+payload.PersonaID+":"+payload.SQLiteEdgeID)
	return f.err
}

func (f *fakeMirrorAdapter) DeleteEdge(ctx context.Context, ref EdgeRef) error {
	f.calls = append(f.calls, "delete_edge:"+ref.PersonaID+":"+ref.SQLiteEdgeID)
	f.deleteEdgeRefs = append(f.deleteEdgeRefs, ref)
	return f.err
}

type fakeIndexMap struct {
	calls []string
}

func (f *fakeIndexMap) MarkNodeIndexed(ctx context.Context, payload NodePayload, result NodeUpsertResult) error {
	f.calls = append(f.calls, "indexed:"+payload.PersonaID+":"+payload.NodeType+":"+payload.SQLiteNodeID+":"+strconv.FormatInt(result.MirrorNodeID, 10))
	return nil
}

func (f *fakeIndexMap) MarkNodeDeleted(ctx context.Context, ref NodeRef) error {
	f.calls = append(f.calls, "deleted:"+ref.PersonaID+":"+ref.NodeType+":"+ref.SQLiteNodeID)
	return nil
}

func assertEqual[T comparable](t *testing.T, got T, want T, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func assertStrings(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("strings length = %d, want %d; got %#v want %#v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("strings[%d] = %q, want %q; got %#v want %#v", i, got[i], want[i], got, want)
		}
	}
}
