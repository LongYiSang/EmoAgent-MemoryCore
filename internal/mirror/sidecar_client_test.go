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
		if request["schema_version"] != "memory_mirror_candidate_request.v0.2" {
			t.Fatalf("schema_version = %v", request["schema_version"])
		}
		if request["request_id"] == "" {
			t.Fatalf("request_id is empty in request %#v", request)
		}
		if _, ok := request["query_text"]; ok {
			t.Fatalf("request used legacy top-level query_text: %#v", request)
		}
		query, ok := request["query"].(map[string]any)
		if !ok {
			t.Fatalf("query = %#v, want object", request["query"])
		}
		if query["raw_text"] != "咖啡" || query["normalized_text"] != "咖啡" {
			t.Fatalf("query text = raw:%#v normalized:%#v", query["raw_text"], query["normalized_text"])
		}
		if query["time_mode"] != "current" || query["memory_ability"] != "direct_fact" {
			t.Fatalf("query analysis = %#v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_mirror_candidates.v0.2",
			"request_id":     request["request_id"],
			"candidates": []map[string]any{
				{
					"trivium_node_id":  42,
					"fused_score":      0.88,
					"primary_source":   "raw_dense",
					"primary_purpose":  "raw_query",
					"rank":             3,
					"hit_count":        2,
					"source_breakdown": []map[string]any{{"source": "raw_dense", "purpose": "raw_query", "rank": 3, "score": 0.88, "weight": 1.0}},
					"score_breakdown":  map[string]any{"provider_score": 0.12},
				},
			},
			"degraded": false,
			"embedding_cache_stats": map[string]any{
				"hits":            2,
				"misses":          1,
				"live_call_count": 1,
			},
			"diagnostics": map[string]any{
				"query_count":                     1,
				"raw_query_count":                 1,
				"rewrite_query_count":             0,
				"anchor_query_count":              0,
				"merged_candidate_count":          1,
				"query_trims":                     map[string]any{"dropped_similar_count": 2},
				"dense_embedding_wall_latency_ms": 11,
				"dense_search_total_latency_ms":   13,
				"query_count_trimmed_by_budget":   2,
				"per_query_counts":                []map[string]any{{"source": "raw_dense", "purpose": "raw_query", "count": 1, "latency_ms": 7}},
			},
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.FindCandidates(context.Background(), CandidateRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Query: QueryAnalysis{
			Raw:           "咖啡",
			Normalized:    "咖啡",
			TimeMode:      "current",
			MemoryDomain:  "user_profile_memory",
			MemoryAbility: "direct_fact",
			EvidenceNeed:  "exact_observation",
		},
		Limit: 8,
	})
	if err != nil {
		t.Fatalf("find candidates: %v", err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].TriviumNodeID != 42 || result.Candidates[0].Score != 0.88 || result.Candidates[0].Source != "raw_dense" || result.Candidates[0].Rank != 3 {
		t.Fatalf("candidates = %#v", result.Candidates)
	}
	if result.Candidates[0].PrimaryPurpose != "raw_query" || result.Candidates[0].HitCount != 2 {
		t.Fatalf("candidate diagnostics = %#v", result.Candidates[0])
	}
	if len(result.Candidates[0].SourceBreakdown) != 1 || result.Candidates[0].SourceBreakdown[0].Source != "raw_dense" {
		t.Fatalf("source breakdown = %#v", result.Candidates[0].SourceBreakdown)
	}
	if result.Diagnostics.QueryCount != 1 || result.Diagnostics.MergedCandidateCount != 1 || result.Diagnostics.QueryTrimCount != 2 || len(result.Diagnostics.PerQuery) != 1 || result.Diagnostics.PerQuery[0].LatencyMs != 7 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
	if result.Diagnostics.DenseEmbeddingWallLatencyMs != 11 || result.Diagnostics.DenseEmbeddingBatchLatencyMs != 11 || result.Diagnostics.DenseSearchTotalLatencyMs != 13 || result.Diagnostics.QueryCountTrimmedByBudget != 2 {
		t.Fatalf("dense diagnostics = %#v", result.Diagnostics)
	}
	if result.EmbeddingCacheHits != 2 || result.EmbeddingCacheMisses != 1 || result.EmbeddingLiveCallCount != 1 {
		t.Fatalf("embedding stats = hits:%d misses:%d live:%d", result.EmbeddingCacheHits, result.EmbeddingCacheMisses, result.EmbeddingLiveCallCount)
	}
}

func TestSidecarClientConfigureEvalReadsCapabilitiesAndMirrorStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eval/configure" {
			t.Fatalf("path = %s, want /eval/configure", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["schema_version"] != "memory_eval_sidecar_config.v0.1" {
			t.Fatalf("schema_version = %v", request["schema_version"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version":            "memory_eval_sidecar_config_result.v0.1",
			"status":                    "ok",
			"trivium_dir":               "C:/tmp/trivium",
			"embedding_cache_mode":      "read_only",
			"embedding_cache_db_path":   "C:/tmp/cache.sqlite3",
			"embedding":                 map[string]any{"fingerprint": "fp"},
			"trivium_adapter_version":   "adapter-v1",
			"triviumdb_version":         "0.7.1",
			"rerank_provider_available": true,
			"rerank_provider_mode":      "live",
			"rerank_cache":              false,
			"mirror_stats_available":    true,
			"mirror_node_count":         2,
			"mirror_edge_count":         1,
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.ConfigureEval(context.Background(), EvalConfigRequest{
		TriviumDir:               "C:/tmp/trivium",
		EmbeddingCacheMode:       "read_only",
		EmbeddingCacheDBPath:     "C:/tmp/cache.sqlite3",
		SearchableTextVersion:    "search-v1",
		TextNormalizationVersion: "norm-v1",
	})
	if err != nil {
		t.Fatalf("configure eval: %v", err)
	}
	if !result.RerankProviderAvailable || result.RerankProviderMode != "live" || result.RerankCache {
		t.Fatalf("rerank capability = available:%t mode:%q cache:%t", result.RerankProviderAvailable, result.RerankProviderMode, result.RerankCache)
	}
	if !result.MirrorStatsAvailable || result.MirrorNodeCount != 2 || result.MirrorEdgeCount != 1 {
		t.Fatalf("mirror stats = available:%t nodes:%d edges:%d", result.MirrorStatsAvailable, result.MirrorNodeCount, result.MirrorEdgeCount)
	}
	if result.Embedding["fingerprint"] != "fp" {
		t.Fatalf("embedding = %#v", result.Embedding)
	}
}

func TestSidecarClientFindCandidatesCapsAndSanitizesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_mirror_candidates.v0.2",
			"request_id":     request["request_id"],
			"candidates": []map[string]any{
				{"trivium_node_id": -1, "fused_score": 0.99, "primary_source": "bad_id"},
				{"trivium_node_id": 42, "fused_score": 1.25, "primary_source": "high_score"},
				{"trivium_node_id": 43, "fused_score": -0.1, "primary_source": "negative_score"},
				{"trivium_node_id": 44, "fused_score": 0.5, "primary_source": "over_limit"},
			},
			"degraded": false,
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.FindCandidates(context.Background(), CandidateRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Query:     QueryAnalysis{Raw: "咖啡", Normalized: "咖啡"},
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
		params, ok := request["params"].(map[string]any)
		if !ok {
			t.Fatalf("params = %#v, want object", request["params"])
		}
		if params["max_edges_scanned_per_request"] != float64(10000) {
			t.Fatalf("max_edges_scanned_per_request = %#v, want 10000", params["max_edges_scanned_per_request"])
		}
		if params["max_neighbors_per_node"] != float64(100) {
			t.Fatalf("max_neighbors_per_node = %#v, want 100", params["max_neighbors_per_node"])
		}
		if params["max_activation_wall_ms"] != float64(120) {
			t.Fatalf("max_activation_wall_ms = %#v, want 120", params["max_activation_wall_ms"])
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
		Params: ActivationParams{
			MaxHops:                   1,
			IncludePaths:              true,
			MaxEdgesScannedPerRequest: 10000,
			MaxNeighborsPerNode:       100,
			MaxActivationWallMs:       120,
		},
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

func TestSidecarClientRerankPostsSafeCandidatesAndSanitizesResponse(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/rerank" {
			t.Fatalf("path = %s, want /retrieval/rerank", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if captured["schema_version"] != "memory_rerank_request.v0.1" {
			t.Fatalf("schema_version = %v", captured["schema_version"])
		}
		if captured["request_id"] == "" {
			t.Fatalf("request_id is empty in request %#v", captured)
		}
		candidates := captured["candidates"].([]any)
		if len(candidates) != 1 {
			t.Fatalf("candidates = %#v, want one", candidates)
		}
		candidate := candidates[0].(map[string]any)
		if candidate["safe_summary"] != "用户喜欢咖啡。" {
			t.Fatalf("safe_summary = %#v", candidate["safe_summary"])
		}
		if strings.Contains(candidate["safe_summary"].(string), "episode raw") {
			t.Fatalf("safe_summary leaked raw episode content: %#v", candidate)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version": "memory_rerank_result.v0.1",
			"request_id":     captured["request_id"],
			"results": []map[string]any{
				{"node_id": "fact-1", "node_type": "fact", "rerank_score": 1.25, "debug_reason": strings.Repeat("direct\n", 60)},
				{"node_id": "unknown", "node_type": "fact", "rerank_score": 0.99, "debug_reason": "injected"},
				{"node_id": "fact-2", "node_type": "fact", "rerank_score": -0.1, "debug_reason": "bad score"},
			},
			"degraded": false,
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	result, err := client.Rerank(context.Background(), RerankRequest{
		PersonaID: "default",
		QueryText: "咖啡",
		Candidates: []RerankCandidate{
			{
				NodeID:       "fact-1",
				NodeType:     "fact",
				SafeSummary:  "用户喜欢咖啡。",
				CurrentScore: 0.72,
				AnchorEnergy: 1.0,
				GraphEnergy:  0.2,
			},
		},
	})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items = %#v, want one sanitized known result", result.Items)
	}
	item := result.Items[0]
	if item.NodeID != "fact-1" || item.NodeType != "fact" || item.RerankScore != 1 {
		t.Fatalf("item = %#v, want fact-1 clamped score", item)
	}
	if strings.ContainsAny(item.DebugReason, "\r\n\t") {
		t.Fatalf("debug reason was not sanitized: %q", item.DebugReason)
	}
	if len([]rune(item.DebugReason)) > 160 {
		t.Fatalf("debug reason length = %d, want <= 160", len([]rune(item.DebugReason)))
	}
}

func TestSidecarClientRerankRejectsBadResponseIdentity(t *testing.T) {
	tests := []struct {
		name string
		body func(requestID string) map[string]any
		want string
	}{
		{
			name: "wrong schema",
			body: func(requestID string) map[string]any {
				return map[string]any{"schema_version": "wrong", "request_id": requestID}
			},
			want: "schema",
		},
		{
			name: "request mismatch",
			body: func(requestID string) map[string]any {
				return map[string]any{"schema_version": "memory_rerank_result.v0.1", "request_id": requestID + "-other"}
			},
			want: "request_id",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				_ = json.NewEncoder(w).Encode(tc.body(request["request_id"].(string)))
			}))
			defer server.Close()

			client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
			_, err := client.Rerank(context.Background(), RerankRequest{
				PersonaID: "default",
				QueryText: "咖啡",
				Candidates: []RerankCandidate{
					{NodeID: "fact-1", NodeType: "fact", SafeSummary: "用户喜欢咖啡。"},
				},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
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
