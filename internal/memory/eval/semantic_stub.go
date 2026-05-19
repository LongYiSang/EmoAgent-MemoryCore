package eval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

const (
	evalSemanticRequestSchemaVersion  = "memory_query_analysis_request.v0.1"
	evalSemanticResponseSchemaVersion = "memory_query_analysis_result.v0.1"
)

type evalSemanticSidecar struct {
	server *httptest.Server
	mu     sync.Mutex
	stub   *SemanticStubSettings
}

type evalSemanticRequest struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
}

type evalSemanticResponse struct {
	SchemaVersion  string               `json:"schema_version"`
	RequestID      string               `json:"request_id,omitempty"`
	Status         string               `json:"status"`
	Degraded       bool                 `json:"degraded,omitempty"`
	FallbackReason string               `json:"fallback_reason,omitempty"`
	Provider       string               `json:"provider,omitempty"`
	Model          string               `json:"model,omitempty"`
	PromptVersion  string               `json:"prompt_version,omitempty"`
	Analysis       evalSemanticAnalysis `json:"analysis,omitempty"`
}

type evalSemanticAnalysis struct {
	TimeMode          string                 `json:"time_mode,omitempty"`
	Signals           []string               `json:"signals,omitempty"`
	MemoryDomain      string                 `json:"memory_domain,omitempty"`
	MemoryAbility     string                 `json:"memory_ability,omitempty"`
	EvidenceNeed      string                 `json:"evidence_need,omitempty"`
	Confidence        float64                `json:"confidence,omitempty"`
	FieldConfidence   evalSemanticConfidence `json:"field_confidence,omitempty"`
	EntityMentions    []evalSemanticEntity   `json:"entity_mentions,omitempty"`
	QueryRewrites     []evalSemanticRewrite  `json:"query_rewrites,omitempty"`
	SemanticAnchors   []evalSemanticAnchor   `json:"semantic_anchors,omitempty"`
	ContextBlockHints []string               `json:"context_block_hints,omitempty"`
}

type evalSemanticConfidence struct {
	Overall          float64 `json:"overall,omitempty"`
	TimeMode         float64 `json:"time_mode,omitempty"`
	MemoryAbility    float64 `json:"memory_ability,omitempty"`
	MemoryDomain     float64 `json:"memory_domain,omitempty"`
	EvidenceNeed     float64 `json:"evidence_need,omitempty"`
	EntityResolution float64 `json:"entity_resolution,omitempty"`
}

type evalSemanticEntity struct {
	EntityID      string  `json:"entity_id"`
	CanonicalName string  `json:"canonical_name,omitempty"`
	Alias         string  `json:"alias,omitempty"`
	MatchText     string  `json:"match_text,omitempty"`
	MatchKind     string  `json:"match_kind,omitempty"`
	Confidence    float64 `json:"confidence,omitempty"`
}

type evalSemanticRewrite struct {
	Text    string  `json:"text"`
	Purpose string  `json:"purpose,omitempty"`
	Weight  float64 `json:"weight,omitempty"`
}

type evalSemanticAnchor struct {
	Text       string  `json:"text"`
	AnchorType string  `json:"anchor_type,omitempty"`
	EntityID   string  `json:"entity_id,omitempty"`
	Weight     float64 `json:"weight,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

func newEvalSemanticSidecar() *evalSemanticSidecar {
	sidecar := &evalSemanticSidecar{}
	mux := http.NewServeMux()
	mux.HandleFunc("/retrieval/query-analysis", sidecar.handleQueryAnalysis)
	sidecar.server = httptest.NewServer(mux)
	return sidecar
}

func (s *evalSemanticSidecar) URL() string {
	if s == nil || s.server == nil {
		return ""
	}
	return s.server.URL
}

func (s *evalSemanticSidecar) Close() {
	if s != nil && s.server != nil {
		s.server.Close()
	}
}

func (s *evalSemanticSidecar) setStub(stub *SemanticStubSettings) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stub = stub
}

func (s *evalSemanticSidecar) currentStub() *SemanticStubSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stub
}

func (s *evalSemanticSidecar) handleQueryAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req evalSemanticRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	stub := s.currentStub()
	response := evalSemanticResponse{
		SchemaVersion:  evalSemanticResponseSchemaVersion,
		RequestID:      req.RequestID,
		Status:         "error",
		FallbackReason: "semantic_unavailable",
		Provider:       "eval_semantic_stub",
	}
	if req.SchemaVersion != evalSemanticRequestSchemaVersion {
		response.FallbackReason = "semantic_protocol_error"
		writeEvalSemanticResponse(w, response)
		return
	}
	if stub != nil {
		response = semanticStubResponse(req.RequestID, *stub)
	}
	writeEvalSemanticResponse(w, response)
}

func writeEvalSemanticResponse(w http.ResponseWriter, response evalSemanticResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func semanticStubResponse(requestID string, stub SemanticStubSettings) evalSemanticResponse {
	status := strings.TrimSpace(stub.Status)
	if status == "" {
		status = "ok"
	}
	return evalSemanticResponse{
		SchemaVersion:  evalSemanticResponseSchemaVersion,
		RequestID:      requestID,
		Status:         status,
		Degraded:       stub.Degraded,
		FallbackReason: stub.FallbackReason,
		Provider:       defaultString(stub.Provider, "eval_semantic_stub"),
		Model:          stub.Model,
		PromptVersion:  stub.PromptVersion,
		Analysis:       evalSemanticAnalysisFromStub(stub.Analysis),
	}
}

func evalSemanticAnalysisFromStub(value SemanticStubAnalysis) evalSemanticAnalysis {
	out := evalSemanticAnalysis{
		TimeMode:          value.TimeMode,
		Signals:           append([]string(nil), value.Signals...),
		MemoryDomain:      value.MemoryDomain,
		MemoryAbility:     value.MemoryAbility,
		EvidenceNeed:      value.EvidenceNeed,
		Confidence:        value.Confidence,
		ContextBlockHints: append([]string(nil), value.ContextBlockHints...),
		FieldConfidence: evalSemanticConfidence{
			Overall:          value.FieldConfidence.Overall,
			TimeMode:         value.FieldConfidence.TimeMode,
			MemoryAbility:    value.FieldConfidence.MemoryAbility,
			MemoryDomain:     value.FieldConfidence.MemoryDomain,
			EvidenceNeed:     value.FieldConfidence.EvidenceNeed,
			EntityResolution: value.FieldConfidence.EntityResolution,
		},
	}
	for _, mention := range value.EntityMentions {
		out.EntityMentions = append(out.EntityMentions, evalSemanticEntity{
			EntityID:      mention.EntityID,
			CanonicalName: mention.CanonicalName,
			Alias:         mention.Alias,
			MatchText:     mention.MatchText,
			MatchKind:     mention.MatchKind,
			Confidence:    mention.Confidence,
		})
	}
	for _, rewrite := range value.QueryRewrites {
		out.QueryRewrites = append(out.QueryRewrites, evalSemanticRewrite{
			Text:    rewrite.Text,
			Purpose: rewrite.Purpose,
			Weight:  rewrite.Weight,
		})
	}
	for _, anchor := range value.SemanticAnchors {
		out.SemanticAnchors = append(out.SemanticAnchors, evalSemanticAnchor{
			Text:       anchor.Text,
			AnchorType: anchor.AnchorType,
			EntityID:   anchor.EntityID,
			Weight:     anchor.Weight,
			Confidence: anchor.Confidence,
		})
	}
	return out
}
