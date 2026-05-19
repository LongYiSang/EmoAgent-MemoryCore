package mirror

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSidecarClientQueryAnalysisPostsRequestAndReadsWrappedResult(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retrieval/query-analysis" {
			t.Fatalf("path = %s, want /retrieval/query-analysis", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if captured["schema_version"] != "memory_query_analysis_request.v0.1" {
			t.Fatalf("schema_version = %v", captured["schema_version"])
		}
		if captured["request_id"] == "" {
			t.Fatalf("request_id is empty: %#v", captured)
		}
		if captured["persona_id"] != "default" || captured["query_text"] != "Long 证据" {
			t.Fatalf("request identity fields = %#v", captured)
		}
		if captured["session_id"] != "session-1" || captured["message_id"] != "message-1" {
			t.Fatalf("session/message fields = %#v", captured)
		}
		if captured["timezone"] != "Asia/Shanghai" {
			t.Fatalf("timezone = %#v", captured["timezone"])
		}
		if captured["rule_analysis"] == nil || captured["visible_entity_hints"] == nil || captured["allowed_enums"] == nil || captured["retrieval_policy"] == nil {
			t.Fatalf("request missing analysis context: %#v", captured)
		}
		debug := captured["debug"].(map[string]any)
		if debug["include_rationale_summary"] != true {
			t.Fatalf("debug = %#v, want include_rationale_summary true", debug)
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema_version":  "memory_query_analysis_result.v0.1",
			"request_id":      captured["request_id"],
			"status":          "ok",
			"degraded":        false,
			"provider":        "sidecar",
			"model":           "semantic-model",
			"prompt_version":  "semantic_query_analyzer.v0.1",
			"fallback_reason": "",
			"analysis": map[string]any{
				"time_mode":      "historical",
				"memory_domain":  "user_profile_memory",
				"memory_ability": "provenance",
				"evidence_need":  "provenance_source",
				"confidence":     0.88,
				"entity_mentions": []map[string]any{{
					"entity_id":      "ent_user",
					"canonical_name": "Long",
					"match_text":     "Long",
					"match_kind":     "canonical",
					"confidence":     0.91,
				}},
				"query_rewrites":   []map[string]any{{"text": "Long provenance", "purpose": "dense", "weight": 0.7}},
				"semantic_anchors": []map[string]any{{"text": "Long", "anchor_type": "entity", "entity_id": "ent_user", "weight": 0.4, "confidence": 0.91}},
				"policy_hints":     map[string]any{"prefer_evidenced_by_links": true, "max_hops_hint": 2},
			},
		})
	}))
	defer server.Close()

	client := NewSidecarClient(SidecarClientOptions{BaseURL: server.URL, Timeout: time.Second})
	sessionID := "session-1"
	messageID := "message-1"
	result, err := client.QueryAnalysis(context.Background(), QueryAnalysisRequest{
		PersonaID: "default",
		SessionID: &sessionID,
		MessageID: &messageID,
		QueryText: "Long 证据",
		Now:       time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC),
		Timezone:  "Asia/Shanghai",
		RuleAnalysis: QueryAnalysis{
			TimeMode:      "current",
			MemoryDomain:  "user_profile_memory",
			MemoryAbility: "direct_fact",
			EvidenceNeed:  "exact_observation",
			Confidence:    0.42,
		},
		VisibleEntityHints: []VisibleEntityHint{{EntityID: "ent_user", CanonicalName: "Long"}},
		AllowedEnums: QueryAnalysisAllowedEnums{
			TimeModes: []string{"current", "historical"},
		},
		RetrievalPolicy: RetrievalPolicy{
			SensitivityPermission: "normal",
			FinalMemoryCount:      8,
			ContextBudgetTokens:   1200,
			UseFTS:                true,
		},
		Debug: QueryAnalysisDebug{IncludeRationaleSummary: true},
	})
	if err != nil {
		t.Fatalf("query analysis: %v", err)
	}
	if result.Status != "ok" || result.Provider != "sidecar" || result.Model != "semantic-model" {
		t.Fatalf("result wrapper = %#v", result)
	}
	if result.Analysis.MemoryAbility != "provenance" || len(result.Analysis.QueryRewrites) != 1 {
		t.Fatalf("analysis = %#v", result.Analysis)
	}
	if len(result.Analysis.EntityMentions) != 1 || result.Analysis.EntityMentions[0].EntityID != "ent_user" || result.Analysis.EntityMentions[0].Confidence != 0.91 {
		t.Fatalf("entity mentions = %#v", result.Analysis.EntityMentions)
	}
	if len(result.Analysis.SemanticAnchors) != 1 || result.Analysis.SemanticAnchors[0].AnchorType != "entity" {
		t.Fatalf("semantic anchors = %#v", result.Analysis.SemanticAnchors)
	}
	if !result.Analysis.PolicyHints.PreferEvidencedByLinks || result.Analysis.PolicyHints.MaxHopsHint != 2 {
		t.Fatalf("policy hints = %#v", result.Analysis.PolicyHints)
	}
}

func TestSidecarClientQueryAnalysisRejectsBadResponseIdentityAndStatus(t *testing.T) {
	tests := []struct {
		name string
		body func(requestID string) map[string]any
		want string
	}{
		{
			name: "missing status",
			body: func(requestID string) map[string]any {
				return map[string]any{"schema_version": "memory_query_analysis_result.v0.1", "request_id": requestID}
			},
			want: "status",
		},
		{
			name: "unknown status",
			body: func(requestID string) map[string]any {
				return map[string]any{"schema_version": "memory_query_analysis_result.v0.1", "request_id": requestID, "status": "surprise"}
			},
			want: "status",
		},
		{
			name: "wrong schema",
			body: func(requestID string) map[string]any {
				return map[string]any{"schema_version": "wrong", "request_id": requestID, "status": "ok"}
			},
			want: "schema",
		},
		{
			name: "request mismatch",
			body: func(requestID string) map[string]any {
				return map[string]any{"schema_version": "memory_query_analysis_result.v0.1", "request_id": requestID + "-other", "status": "ok"}
			},
			want: "request_id",
		},
		{
			name: "flattened analysis",
			body: func(requestID string) map[string]any {
				return map[string]any{
					"schema_version": "memory_query_analysis_result.v0.1",
					"request_id":     requestID,
					"status":         "ok",
					"degraded":       false,
					"query_rewrites": []map[string]any{{"text": "coffee preference", "weight": 0.7}},
				}
			},
			want: "analysis",
		},
		{
			name: "empty analysis",
			body: func(requestID string) map[string]any {
				return map[string]any{
					"schema_version": "memory_query_analysis_result.v0.1",
					"request_id":     requestID,
					"status":         "ok",
					"degraded":       false,
					"analysis":       map[string]any{},
				}
			},
			want: "analysis",
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
			_, err := client.QueryAnalysis(context.Background(), QueryAnalysisRequest{
				PersonaID: "default",
				QueryText: "咖啡",
				Now:       time.Date(2026, 5, 19, 8, 0, 0, 0, time.UTC),
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
		})
	}
}
