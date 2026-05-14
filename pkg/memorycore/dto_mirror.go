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

type RebuildMirrorRequest struct {
	PersonaID string
}

type RebuildMirrorResult struct {
	NodesUpserted int `json:"nodes_upserted"`
	EdgesUpserted int `json:"edges_upserted"`
	Failed        int `json:"failed"`
	Skipped       int `json:"skipped"`
}

type MirrorNodeRef struct {
	PersonaID    string `json:"persona_id"`
	NodeType     string `json:"node_type"`
	SQLiteNodeID string `json:"sqlite_node_id"`
}

type MirrorEdgeRef struct {
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

type MirrorCandidateRequest struct {
	PersonaID string `json:"persona_id"`
	QueryText string `json:"query_text"`
	Limit     int    `json:"limit"`
}

type MirrorCandidate struct {
	TriviumNodeID int64   `json:"trivium_node_id"`
	Score         float64 `json:"score"`
	Source        string  `json:"source"`
	Rank          int     `json:"rank,omitempty"`
}

type MirrorCandidateResult struct {
	Candidates     []MirrorCandidate `json:"candidates"`
	Degraded       bool              `json:"degraded"`
	FallbackReason string            `json:"fallback_reason,omitempty"`
}

type MirrorActivationRequest struct {
	PersonaID string                 `json:"persona_id"`
	Seeds     []MirrorActivationSeed `json:"seeds"`
	Params    MirrorActivationParams `json:"params"`
}

type MirrorActivationSeed struct {
	TriviumNodeID int64   `json:"trivium_node_id"`
	SQLiteNodeID  string  `json:"sqlite_node_id"`
	NodeType      string  `json:"node_type"`
	SeedEnergy    float64 `json:"seed_energy"`
}

type MirrorActivationParams struct {
	MaxHops             int     `json:"max_hops"`
	HopDecay            float64 `json:"hop_decay"`
	MinEnergy           float64 `json:"min_energy"`
	MaxActiveNodes      int     `json:"max_active_nodes"`
	HubSuppressionPower float64 `json:"hub_suppression_power"`
	IncludePaths        bool    `json:"include_paths"`
}

type MirrorActivationCandidate struct {
	TriviumNodeID int64                  `json:"trivium_node_id"`
	Score         float64                `json:"score"`
	Source        string                 `json:"source"`
	Rank          int                    `json:"rank,omitempty"`
	Paths         []MirrorActivationPath `json:"paths,omitempty"`
}

type MirrorActivationPath struct {
	TriviumNodeIDs []int64  `json:"trivium_node_ids"`
	LinkTypes      []string `json:"link_types"`
}

type MirrorActivationResult struct {
	Candidates     []MirrorActivationCandidate `json:"candidates"`
	Degraded       bool                        `json:"degraded"`
	FallbackReason string                      `json:"fallback_reason,omitempty"`
}

type MirrorAdapter interface {
	UpsertNode(ctx context.Context, payload MirrorNodePayload) (MirrorNodeUpsertResult, error)
	DeleteNode(ctx context.Context, ref MirrorNodeRef) error
	UpsertEdge(ctx context.Context, payload MirrorEdgePayload) error
	DeleteEdge(ctx context.Context, ref MirrorEdgeRef) error
}

type MirrorNamespaceAdapter interface {
	ClearNamespace(ctx context.Context, personaID string) error
}

type MirrorCandidateAdapter interface {
	FindCandidates(ctx context.Context, req MirrorCandidateRequest) (*MirrorCandidateResult, error)
}

type MirrorActivationAdapter interface {
	ActivateGraph(ctx context.Context, req MirrorActivationRequest) (*MirrorActivationResult, error)
}
