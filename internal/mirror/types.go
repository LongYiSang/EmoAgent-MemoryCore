package mirror

import "context"

type Operation string

const (
	OperationUpsertNode Operation = "upsert_node"
	OperationDeleteNode Operation = "delete_node"
	OperationUpsertEdge Operation = "upsert_edge"
	OperationDeleteEdge Operation = "delete_edge"
)

type QueueRow struct {
	ID          string
	PersonaID   string
	NodeType    string
	NodeID      string
	Operation   Operation
	PayloadJSON string
}

type NodeRef struct {
	PersonaID    string
	NodeType     string
	SQLiteNodeID string
}

type EdgeRef struct {
	PersonaID        string
	SQLiteEdgeID     string
	LinkType         string
	FromNodeType     string
	FromNodeID       string
	ToNodeType       string
	ToNodeID         string
	FromMirrorNodeID *int64
	ToMirrorNodeID   *int64
}

type NodePayload struct {
	PersonaID      string
	NodeType       string
	SQLiteNodeID   string
	SearchableText string
	Payload        map[string]any
}

type EdgePayload struct {
	PersonaID    string
	SQLiteEdgeID string
	LinkType     string
	FromNodeType string
	FromNodeID   string
	ToNodeType   string
	ToNodeID     string
	Direction    string
	Confidence   float64
	Weight       float64
	Payload      map[string]any
}

type NodeUpsertResult struct {
	MirrorNodeID int64
}

type Queue interface {
	Claim(ctx context.Context, limit int) ([]QueueRow, error)
	Complete(ctx context.Context, id string) error
	Fail(ctx context.Context, id string, message string) error
}

type PayloadBuilder interface {
	BuildNodePayload(ctx context.Context, personaID string, nodeType string, nodeID string) (NodePayload, bool, error)
	BuildEdgePayload(ctx context.Context, personaID string, edgeID string) (EdgePayload, bool, error)
}

type MirrorAdapter interface {
	UpsertNode(ctx context.Context, payload NodePayload) (NodeUpsertResult, error)
	DeleteNode(ctx context.Context, ref NodeRef) error
	UpsertEdge(ctx context.Context, payload EdgePayload) error
	DeleteEdge(ctx context.Context, ref EdgeRef) error
}

type IndexMap interface {
	MarkNodeIndexed(ctx context.Context, payload NodePayload, result NodeUpsertResult) error
	MarkNodeDeleted(ctx context.Context, ref NodeRef) error
}

type RebuildSource interface {
	ListRebuildNodeRefs(ctx context.Context, personaID string) ([]NodeRef, error)
	ListRebuildEdgeRefs(ctx context.Context, personaID string) ([]EdgeRef, error)
	BuildNodePayload(ctx context.Context, personaID string, nodeType string, nodeID string) (NodePayload, bool, error)
	BuildEdgePayload(ctx context.Context, personaID string, edgeID string) (EdgePayload, bool, error)
}

type NamespaceClearer interface {
	ClearNamespace(ctx context.Context, personaID string) error
}

type RebuildIndexMap interface {
	MarkPersonaDeleted(ctx context.Context, personaID string) error
	MarkNodeIndexed(ctx context.Context, payload NodePayload, result NodeUpsertResult) error
	MarkNodeFailed(ctx context.Context, ref NodeRef, message string) error
}

type WorkerOptions struct {
	Queue    Queue
	Payloads PayloadBuilder
	Adapter  MirrorAdapter
	IndexMap IndexMap
}

type Result struct {
	Claimed   int
	Completed int
	Failed    int
	Skipped   int
}

type RebuildResult struct {
	NodesUpserted int
	EdgesUpserted int
	Failed        int
	Skipped       int
}
