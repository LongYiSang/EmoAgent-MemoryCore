package mirror

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

func TestRebuilderClearsNamespaceAndIndexesEligiblePayloads(t *testing.T) {
	ctx := context.Background()
	source := &fakeRebuildSource{
		nodeRefs: []NodeRef{
			{PersonaID: "p1", NodeType: "fact", SQLiteNodeID: "fact-1"},
			{PersonaID: "p1", NodeType: "fact", SQLiteNodeID: "fact-hidden"},
		},
		edgeRefs: []EdgeRef{
			{PersonaID: "p1", SQLiteEdgeID: "edge-1"},
		},
		nodes: map[string]fakeNodePayloadResult{
			"p1/fact/fact-1": {
				payload: NodePayload{PersonaID: "p1", NodeType: "fact", SQLiteNodeID: "fact-1", SearchableText: "safe"},
				ok:      true,
			},
			"p1/fact/fact-hidden": {ok: false},
		},
		edges: map[string]fakeEdgePayloadResult{
			"p1/edge-1": {
				payload: EdgePayload{PersonaID: "p1", SQLiteEdgeID: "edge-1", LinkType: "ABOUT_ENTITY"},
				ok:      true,
			},
		},
	}
	adapter := &fakeMirrorAdapter{nodeMirrorID: 1001}
	namespace := &fakeNamespaceClearer{}
	index := &fakeRebuildIndexMap{}

	result, err := NewRebuilder(RebuilderOptions{
		Source:    source,
		Adapter:   adapter,
		Namespace: namespace,
		IndexMap:  index,
	}).Rebuild(ctx, "p1")
	if err != nil {
		t.Fatalf("Rebuild returned error: %v", err)
	}

	assertEqual(t, result.NodesUpserted, 1, "nodes upserted")
	assertEqual(t, result.EdgesUpserted, 1, "edges upserted")
	assertEqual(t, result.Skipped, 1, "skipped")
	assertEqual(t, result.Failed, 0, "failed")
	assertStrings(t, namespace.cleared, []string{"p1"})
	assertStrings(t, adapter.calls, []string{"upsert_node:p1:fact:fact-1", "upsert_edge:p1:edge-1"})
	assertStrings(t, index.calls, []string{"persona_deleted:p1", "indexed:p1:fact:fact-1:1001"})
}

func TestRebuilderMarksExistingNodeFailedAndContinues(t *testing.T) {
	ctx := context.Background()
	source := &fakeRebuildSource{
		nodeRefs: []NodeRef{
			{PersonaID: "p1", NodeType: "fact", SQLiteNodeID: "fact-1"},
			{PersonaID: "p1", NodeType: "fact", SQLiteNodeID: "fact-2"},
		},
		edgeRefs: []EdgeRef{
			{PersonaID: "p1", SQLiteEdgeID: "edge-1"},
		},
		nodes: map[string]fakeNodePayloadResult{
			"p1/fact/fact-1": {
				payload: NodePayload{PersonaID: "p1", NodeType: "fact", SQLiteNodeID: "fact-1", SearchableText: "safe 1"},
				ok:      true,
			},
			"p1/fact/fact-2": {
				payload: NodePayload{PersonaID: "p1", NodeType: "fact", SQLiteNodeID: "fact-2", SearchableText: "safe 2"},
				ok:      true,
			},
		},
		edges: map[string]fakeEdgePayloadResult{
			"p1/edge-1": {
				payload: EdgePayload{PersonaID: "p1", SQLiteEdgeID: "edge-1", LinkType: "ABOUT_ENTITY"},
				ok:      true,
			},
		},
	}
	adapter := &selectiveFailMirrorAdapter{failNodeID: "fact-1", nodeMirrorID: 2002}
	index := &fakeRebuildIndexMap{}

	result, err := NewRebuilder(RebuilderOptions{
		Source:    source,
		Adapter:   adapter,
		Namespace: &fakeNamespaceClearer{},
		IndexMap:  index,
	}).Rebuild(ctx, "p1")
	if err != nil {
		t.Fatalf("Rebuild returned error: %v", err)
	}

	assertEqual(t, result.NodesUpserted, 1, "nodes upserted")
	assertEqual(t, result.Failed, 1, "failed")
	assertStrings(t, adapter.calls, []string{"upsert_node:fact-1", "upsert_node:fact-2"})
	assertStrings(t, index.calls, []string{
		"persona_deleted:p1",
		"failed:p1:fact:fact-1:adapter unavailable",
		"indexed:p1:fact:fact-2:2002",
	})
}

func TestRebuilderFailsBeforeMutatingIndexWhenClearFails(t *testing.T) {
	ctx := context.Background()
	index := &fakeRebuildIndexMap{}

	_, err := NewRebuilder(RebuilderOptions{
		Source:    &fakeRebuildSource{},
		Adapter:   &fakeMirrorAdapter{},
		Namespace: &fakeNamespaceClearer{err: errors.New("sidecar unavailable")},
		IndexMap:  index,
	}).Rebuild(ctx, "p1")
	if err == nil {
		t.Fatalf("err = nil, want clear namespace failure")
	}
	assertStrings(t, index.calls, nil)
}

type fakeRebuildSource struct {
	nodeRefs []NodeRef
	edgeRefs []EdgeRef
	nodes    map[string]fakeNodePayloadResult
	edges    map[string]fakeEdgePayloadResult
}

func (f *fakeRebuildSource) ListRebuildNodeRefs(ctx context.Context, personaID string) ([]NodeRef, error) {
	return append([]NodeRef(nil), f.nodeRefs...), nil
}

func (f *fakeRebuildSource) ListRebuildEdgeRefs(ctx context.Context, personaID string) ([]EdgeRef, error) {
	return append([]EdgeRef(nil), f.edgeRefs...), nil
}

func (f *fakeRebuildSource) BuildNodePayload(ctx context.Context, personaID string, nodeType string, nodeID string) (NodePayload, bool, error) {
	result := f.nodes[personaID+"/"+nodeType+"/"+nodeID]
	return result.payload, result.ok, result.err
}

func (f *fakeRebuildSource) BuildEdgePayload(ctx context.Context, personaID string, edgeID string) (EdgePayload, bool, error) {
	result := f.edges[personaID+"/"+edgeID]
	return result.payload, result.ok, result.err
}

type fakeNamespaceClearer struct {
	cleared []string
	err     error
}

func (f *fakeNamespaceClearer) ClearNamespace(ctx context.Context, personaID string) error {
	f.cleared = append(f.cleared, personaID)
	return f.err
}

type fakeRebuildIndexMap struct {
	calls []string
}

func (f *fakeRebuildIndexMap) MarkPersonaDeleted(ctx context.Context, personaID string) error {
	f.calls = append(f.calls, "persona_deleted:"+personaID)
	return nil
}

func (f *fakeRebuildIndexMap) MarkNodeIndexed(ctx context.Context, payload NodePayload, result NodeUpsertResult) error {
	f.calls = append(f.calls, "indexed:"+payload.PersonaID+":"+payload.NodeType+":"+payload.SQLiteNodeID+":"+strconv.FormatInt(result.MirrorNodeID, 10))
	return nil
}

func (f *fakeRebuildIndexMap) MarkNodeFailed(ctx context.Context, ref NodeRef, message string) error {
	f.calls = append(f.calls, "failed:"+ref.PersonaID+":"+ref.NodeType+":"+ref.SQLiteNodeID+":"+message)
	return nil
}

type selectiveFailMirrorAdapter struct {
	failNodeID   string
	nodeMirrorID int64
	calls        []string
}

func (f *selectiveFailMirrorAdapter) UpsertNode(ctx context.Context, payload NodePayload) (NodeUpsertResult, error) {
	f.calls = append(f.calls, "upsert_node:"+payload.SQLiteNodeID)
	if payload.SQLiteNodeID == f.failNodeID {
		return NodeUpsertResult{}, errors.New("adapter unavailable")
	}
	return NodeUpsertResult{MirrorNodeID: f.nodeMirrorID}, nil
}

func (f *selectiveFailMirrorAdapter) DeleteNode(ctx context.Context, ref NodeRef) error {
	return nil
}

func (f *selectiveFailMirrorAdapter) UpsertEdge(ctx context.Context, payload EdgePayload) error {
	f.calls = append(f.calls, "upsert_edge:"+payload.SQLiteEdgeID)
	return nil
}

func (f *selectiveFailMirrorAdapter) DeleteEdge(ctx context.Context, ref EdgeRef) error {
	return nil
}
