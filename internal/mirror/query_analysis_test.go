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
		ruleAnalysis := captured["rule_analysis"].(map[string]any)
		ruleScores := ruleAnalysis["scores"].(map[string]any)
		if ruleScores["rule_fit"] != 0.61 || ruleScores["expected_retrieval_confidence"] != 0.55 {
			t.Fatalf("rule scores = %#v", ruleScores)
		}
		ruleProbes := ruleAnalysis["probes"].(map[string]any)
		if ruleProbes["sparse_probe_conf"] != 0.62 || ruleProbes["fallback_search_hit_count"] != float64(4) {
			t.Fatalf("rule probes = %#v", ruleProbes)
		}
		ruleDecision := ruleAnalysis["decision"].(map[string]any)
		if ruleDecision["use_semantic"] != true || ruleDecision["semantic_mode"] != "decompose" {
			t.Fatalf("rule decision = %#v", ruleDecision)
		}
		if gotReasons := ruleDecision["reason_codes"].([]any); gotReasons[0] != "causal_intent" {
			t.Fatalf("rule decision reasons = %#v", gotReasons)
		}
		if gotEvidence := ruleAnalysis["evidence"].([]any); gotEvidence[0].(map[string]any)["detector"] != "rule_regex_v1" {
			t.Fatalf("rule evidence = %#v", gotEvidence)
		}
		if gotAlternatives := ruleAnalysis["alternatives"].([]any); gotAlternatives[0].(map[string]any)["value"] != "historical" {
			t.Fatalf("rule alternatives = %#v", gotAlternatives)
		}
		if captured["deadline_ms"] != float64(900) || captured["provider_timeout_ms"] != float64(800) {
			t.Fatalf("budget fields = deadline:%#v provider:%#v", captured["deadline_ms"], captured["provider_timeout_ms"])
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
				"scores":           map[string]any{"rule_fit": 0.72, "anchor_readiness": 0.43, "expected_retrieval_confidence": 0.61, "semantic_need": 0.69},
				"probes":           map[string]any{"entity_exact_conf": 0.84, "sparse_probe_conf": 0.52, "fallback_search_hit_count": 3, "top1_margin": 0.17},
				"decision":         map[string]any{"use_semantic": true, "semantic_mode": "light", "retrieval_mode": "provenance", "reason_codes": []string{"provenance_intent"}, "threshold_version": "semantic_router_v1", "scorer_version": "query_analysis_scorer_v1"},
				"evidence":         []map[string]any{{"field": "memory_ability", "signal": "provenance_word", "match_text": "证据", "span_start": 5, "span_end": 11, "weight": 0.45, "detector": "provider_v1"}},
				"alternatives":     []map[string]any{{"field": "time_mode", "value": "current", "confidence": 0.33, "reason_codes": []string{"weak_time"}, "detector": "provider_v1"}},
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
			Scores: QueryAnalysisScores{
				RuleFit:                     0.61,
				ExpectedRetrievalConfidence: 0.55,
			},
			Probes: QueryAnchorProbe{
				SparseProbeConf:        0.62,
				FallbackSearchHitCount: 4,
			},
			Decision: QueryAnalysisDecision{
				UseSemantic:   true,
				SemanticMode:  "decompose",
				RetrievalMode: "graph_contextual",
				ReasonCodes:   []string{"causal_intent"},
			},
			Evidence: []QueryAnalysisEvidence{{
				Field:     "memory_ability",
				Signal:    "causal_word",
				MatchText: "为什么",
				Weight:    0.38,
				Detector:  "rule_regex_v1",
			}},
			Alternatives: []QueryAnalysisAlternative{{
				Field:       "time_mode",
				Value:       "historical",
				Confidence:  0.41,
				ReasonCodes: []string{"historical_phrase"},
				Detector:    "rule_regex_v1",
			}},
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
		DeadlineMS:        900,
		ProviderTimeoutMS: 800,
		Debug:             QueryAnalysisDebug{IncludeRationaleSummary: true},
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
	if result.Analysis.Scores.RuleFit != 0.72 || result.Analysis.Scores.SemanticNeed != 0.69 {
		t.Fatalf("scores = %#v", result.Analysis.Scores)
	}
	if result.Analysis.Probes.EntityExactConf != 0.84 || result.Analysis.Probes.Top1Margin != 0.17 {
		t.Fatalf("probes = %#v", result.Analysis.Probes)
	}
	if !result.Analysis.Decision.UseSemantic || result.Analysis.Decision.ReasonCodes[0] != "provenance_intent" {
		t.Fatalf("decision = %#v", result.Analysis.Decision)
	}
	if len(result.Analysis.Evidence) != 1 || result.Analysis.Evidence[0].Detector != "provider_v1" {
		t.Fatalf("evidence = %#v", result.Analysis.Evidence)
	}
	if len(result.Analysis.Alternatives) != 1 || result.Analysis.Alternatives[0].ReasonCodes[0] != "weak_time" {
		t.Fatalf("alternatives = %#v", result.Analysis.Alternatives)
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
