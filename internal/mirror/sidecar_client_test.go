package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSidecarClientPostsAllOperationsAndReturnsMirrorID(t *testing.T) {
	var operations []string
	var deleteEdge map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mirror/operation" {
			t.Fatalf("path = %s, want /mirror/operation", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["schema_version"] != "memory_mirror_operation.v0.1" {
			t.Fatalf("schema_version = %v", request["schema_version"])
		}
		if request["operation_id"] == "" {
			t.Fatalf("operation_id is empty in request %#v", request)
		}
		if request["persona_id"] != "default" {
			t.Fatalf("persona_id = %v, want default", request["persona_id"])
		}
		operation := request["operation"].(string)
		operations = append(operations, operation)
		if operation == string(OperationDeleteEdge) {
			edge, ok := request["edge"].(map[string]any)
			if !ok {
				t.Fatalf("delete edge payload type = %T, want map", request["edge"])
			}
			deleteEdge = edge
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version":  "memory_mirror_operation_result.v0.1",
			"operation_id":    request["operation_id"],
			"status":          "ok",
			"trivium_node_id": 321,
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	ctx := context.Background()
	result, err := client.UpsertNode(ctx, NodePayload{
		PersonaID:      "default",
		NodeType:       "fact",
		SQLiteNodeID:   "fact-1",
		SearchableText: "safe text",
		Payload:        map[string]any{"node_type": "fact"},
	})
	if err != nil {
		t.Fatalf("upsert node: %v", err)
	}
	if result.MirrorNodeID != 321 {
		t.Fatalf("MirrorNodeID = %d, want 321", result.MirrorNodeID)
	}
	if err := client.DeleteNode(ctx, NodeRef{PersonaID: "default", NodeType: "fact", SQLiteNodeID: "fact-1"}); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	if err := client.UpsertEdge(ctx, EdgePayload{PersonaID: "default", SQLiteEdgeID: "edge-1", LinkType: "ABOUT_ENTITY"}); err != nil {
		t.Fatalf("upsert edge: %v", err)
	}
	if err := client.DeleteEdge(ctx, EdgeRef{
		PersonaID:        "default",
		SQLiteEdgeID:     "edge-1",
		LinkType:         "ABOUT_ENTITY",
		FromNodeType:     "fact",
		FromNodeID:       "fact-1",
		ToNodeType:       "entity",
		ToNodeID:         "entity-1",
		FromMirrorNodeID: ptrInt64(101),
		ToMirrorNodeID:   ptrInt64(202),
	}); err != nil {
		t.Fatalf("delete edge: %v", err)
	}

	assertStrings(t, operations, []string{"upsert_node", "delete_node", "upsert_edge", "delete_edge"})
	if deleteEdge == nil {
		t.Fatal("delete_edge payload not captured")
	}
	assertMapStringField(t, deleteEdge, "persona_id", "default")
	assertMapStringField(t, deleteEdge, "sqlite_edge_id", "edge-1")
	assertMapStringField(t, deleteEdge, "link_type", "ABOUT_ENTITY")
	assertMapStringField(t, deleteEdge, "from_node_type", "fact")
	assertMapStringField(t, deleteEdge, "from_node_id", "fact-1")
	assertMapStringField(t, deleteEdge, "to_node_type", "entity")
	assertMapStringField(t, deleteEdge, "to_node_id", "entity-1")
	assertMapFloatField(t, deleteEdge, "from_mirror_node_id", 101)
	assertMapFloatField(t, deleteEdge, "to_mirror_node_id", 202)
}

func TestSidecarClientReturnsErrorForHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	_, err := client.UpsertNode(context.Background(), NodePayload{PersonaID: "default", NodeType: "fact", SQLiteNodeID: "fact-1"})
	if err == nil || !strings.Contains(err.Error(), "status 503") {
		t.Fatalf("err = %v, want status 503", err)
	}
}

func TestSidecarClientRejectsNonLoopbackBaseURL(t *testing.T) {
	client := NewSidecarClient(SidecarClientOptions{BaseURL: "https://example.com", Timeout: time.Second})
	err := client.DeleteNode(context.Background(), NodeRef{PersonaID: "default", NodeType: "fact", SQLiteNodeID: "fact-1"})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v, want loopback validation error", err)
	}
}

func TestSidecarClientRejectsBaseURLWithQueryOrFragment(t *testing.T) {
	for _, baseURL := range []string{"http://127.0.0.1:8765?x=1", "http://127.0.0.1:8765#frag"} {
		t.Run(baseURL, func(t *testing.T) {
			err := ValidateLoopbackURL(baseURL)
			if err == nil || !strings.Contains(err.Error(), "query or fragment") {
				t.Fatalf("err = %v, want query or fragment validation error", err)
			}
		})
	}
}

func TestSidecarClientReturnsErrorForTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: 10 * time.Millisecond})
	err := client.DeleteNode(context.Background(), NodeRef{PersonaID: "default", NodeType: "fact", SQLiteNodeID: "fact-1"})
	if err == nil {
		t.Fatalf("err = nil, want timeout error")
	}
}

func TestSidecarClientRejectsMalformedOrWrongSchemaResponse(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed", body: `{`, want: "decode"},
		{name: "wrong schema", body: `{"schema_version":"wrong","status":"ok"}`, want: "schema"},
		{name: "error status", body: `{"schema_version":"memory_mirror_operation_result.v0.1","operation_id":"upsert_node:default:fact:fact-1","status":"error","error":"adapter failed"}`, want: "adapter failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
			_, err := client.UpsertNode(context.Background(), NodePayload{PersonaID: "default", NodeType: "fact", SQLiteNodeID: "fact-1"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestSidecarClientHonorsContextCancellation(t *testing.T) {
	client := NewSidecarClient(SidecarClientOptions{BaseURL: "http://127.0.0.1:1", Timeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.DeleteEdge(ctx, EdgeRef{PersonaID: "default", SQLiteEdgeID: "edge-1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSidecarClientFindCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/candidates" {
			t.Fatalf("path = %s, want /retrieval/candidates", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["schema_version"] != "memory_mirror_candidate_request.v0.1" {
			t.Fatalf("schema_version = %v", request["schema_version"])
		}
		if request["request_id"] == "" {
			t.Fatalf("request_id is empty in request %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_mirror_candidates.v0.1",
			"request_id":     request["request_id"],
			"candidates": []map[string]any{
				{"trivium_node_id": 42, "score": 0.88, "source": "fake_sparse"},
			},
			"degraded": false,
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.FindCandidates(context.Background(), CandidateRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Limit:     8,
	})
	if err != nil {
		t.Fatalf("find candidates: %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].TriviumNodeID != 42 || result.Candidates[0].Score != 0.88 {
		t.Fatalf("candidates = %#v", result.Candidates)
	}
}

func TestSidecarClientFindCandidatesCapsAndSanitizesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_mirror_candidates.v0.1",
			"request_id":     request["request_id"],
			"candidates": []map[string]any{
				{"trivium_node_id": -1, "score": 0.99, "source": "bad_id"},
				{"trivium_node_id": 42, "score": 1.25, "source": "high_score"},
				{"trivium_node_id": 43, "score": -0.1, "source": "negative_score"},
				{"trivium_node_id": 44, "score": 0.5, "source": "over_limit"},
			},
			"degraded": false,
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.FindCandidates(context.Background(), CandidateRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Limit:     1,
	})
	if err != nil {
		t.Fatalf("find candidates: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(result.Candidates), result.Candidates)
	}
	if result.Candidates[0].TriviumNodeID != 42 || result.Candidates[0].Score != 1 {
		t.Fatalf("candidate = %#v, want id 42 score clamped to 1", result.Candidates[0])
	}
}

func TestSidecarClientActivateGraph(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/activate" {
			t.Fatalf("path = %s, want /retrieval/activate", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["schema_version"] != "memory_graph_activation_request.v0.1" {
			t.Fatalf("schema_version = %v", request["schema_version"])
		}
		if request["request_id"] == "" {
			t.Fatalf("request_id is empty in request %#v", request)
		}
		seeds, ok := request["seeds"].([]any)
		if !ok || len(seeds) != 1 {
			t.Fatalf("seeds = %#v, want one seed", request["seeds"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_graph_activation_result.v0.1",
			"request_id":     request["request_id"],
			"candidates": []map[string]any{
				{
					"trivium_node_id": 42,
					"score":           0.75,
					"source":          "graph_activation",
					"rank":            1,
					"paths": []map[string]any{
						{
							"trivium_node_ids": []int{7, 42},
							"link_types":       []string{"CAUSED_BY"},
						},
					},
				},
			},
			"degraded": false,
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.ActivateGraph(context.Background(), ActivationRequest{
		PersonaID: "default",
		Seeds: []ActivationSeed{
			{TriviumNodeID: 7, SQLiteNodeID: "fact-seed", NodeType: "fact", SeedEnergy: 1.0},
		},
		Params: ActivationParams{MaxHops: 1, IncludePaths: true},
	})
	if err != nil {
		t.Fatalf("activate graph: %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].TriviumNodeID != 42 || result.Candidates[0].Score != 0.75 || result.Candidates[0].Rank != 1 {
		t.Fatalf("activation candidates = %#v", result.Candidates)
	}
	if len(result.Candidates[0].Paths) != 1 || len(result.Candidates[0].Paths[0].TriviumNodeIDs) != 2 {
		t.Fatalf("activation paths = %#v", result.Candidates[0].Paths)
	}
}

func TestSidecarClientActivateGraphCapsAndSanitizesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_graph_activation_result.v0.1",
			"request_id":     request["request_id"],
			"candidates": []map[string]any{
				{"trivium_node_id": -1, "score": 0.99, "source": "bad_id", "rank": 1},
				{"trivium_node_id": 42, "score": 1.25, "source": "high_score", "rank": 2},
				{"trivium_node_id": 43, "score": -0.1, "source": "negative_score", "rank": 3},
				{"trivium_node_id": 44, "score": 0.5, "source": "over_limit", "rank": 4},
			},
			"degraded": false,
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.ActivateGraph(context.Background(), ActivationRequest{
		PersonaID: "default",
		Seeds: []ActivationSeed{
			{TriviumNodeID: 7, SQLiteNodeID: "fact-seed", NodeType: "fact", SeedEnergy: 1.0},
		},
		Params: ActivationParams{MaxActiveNodes: 1},
	})
	if err != nil {
		t.Fatalf("activate graph: %v", err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(result.Candidates), result.Candidates)
	}
	if result.Candidates[0].TriviumNodeID != 42 || result.Candidates[0].Score != 1 {
		t.Fatalf("candidate = %#v, want id 42 score clamped to 1", result.Candidates[0])
	}
}

func ptrInt64(v int64) *int64 {
	return &v
}

func assertMapStringField(t *testing.T, value map[string]any, field string, want string) {
	t.Helper()
	got, ok := value[field].(string)
	if !ok || got != want {
		t.Fatalf("%s = %#v, want %q", field, value[field], want)
	}
}

func assertMapFloatField(t *testing.T, value map[string]any, field string, want float64) {
	t.Helper()
	got, ok := value[field].(float64)
	if !ok || got != want {
		t.Fatalf("%s = %#v, want %v", field, value[field], want)
	}
}
