package memorycore

import "context"

type RunMirrorSyncRequest struct {
	PersonaID string
	Limit     int
}

type RunMirrorSyncResult struct {
	Claimed   int `json:"claimed"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

type MirrorNodeRef struct {
	PersonaID    string `json:"persona_id"`
	NodeType     string `json:"node_type"`
	SQLiteNodeID string `json:"sqlite_node_id"`
}

type MirrorEdgeRef struct {
	PersonaID    string `json:"persona_id"`
	SQLiteEdgeID string `json:"sqlite_edge_id"`
}

type MirrorNodePayload struct {
	PersonaID      string         `json:"persona_id"`
	NodeType       string         `json:"node_type"`
	SQLiteNodeID   string         `json:"sqlite_node_id"`
	SearchableText string         `json:"searchable_text"`
	Payload        map[string]any `json:"payload"`
}

type MirrorEdgePayload struct {
	PersonaID    string         `json:"persona_id"`
	SQLiteEdgeID string         `json:"sqlite_edge_id"`
	LinkType     string         `json:"link_type"`
	FromNodeType string         `json:"from_node_type"`
	FromNodeID   string         `json:"from_node_id"`
	ToNodeType   string         `json:"to_node_type"`
	ToNodeID     string         `json:"to_node_id"`
	Direction    string         `json:"direction"`
	Confidence   float64        `json:"confidence"`
	Weight       float64        `json:"weight"`
	Payload      map[string]any `json:"payload"`
}

type MirrorNodeUpsertResult struct {
	MirrorNodeID int64 `json:"mirror_node_id"`
}

type MirrorAdapter interface {
	UpsertNode(ctx context.Context, payload MirrorNodePayload) (MirrorNodeUpsertResult, error)
	DeleteNode(ctx context.Context, ref MirrorNodeRef) error
	UpsertEdge(ctx context.Context, payload MirrorEdgePayload) error
	DeleteEdge(ctx context.Context, ref MirrorEdgeRef) error
}
