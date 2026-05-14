package mirror

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	sidecarRequestSchemaVersion            = "memory_mirror_operation.v0.1"
	sidecarResponseSchemaVersion           = "memory_mirror_operation_result.v0.1"
	sidecarClearRequestSchemaVersion       = "memory_mirror_clear_namespace.v0.1"
	sidecarClearResponseSchemaVersion      = "memory_mirror_clear_namespace_result.v0.1"
	sidecarCandidateRequestSchemaVersion   = "memory_mirror_candidate_request.v0.1"
	sidecarCandidateResponseSchemaVersion  = "memory_mirror_candidates.v0.1"
	sidecarActivationRequestSchemaVersion  = "memory_graph_activation_request.v0.1"
	sidecarActivationResponseSchemaVersion = "memory_graph_activation_result.v0.1"
	defaultSidecarTimeout                  = 10 * time.Second
)

type SidecarClientOptions struct {
	BaseURL    string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type SidecarClient struct {
	baseURL    string
	httpClient *http.Client
}

type CandidateRequest struct {
	PersonaID string
	QueryText string
	Limit     int
}

type Candidate struct {
	TriviumNodeID int64
	Score         float64
	Source        string
}

type CandidateResult struct {
	Candidates     []Candidate
	Degraded       bool
	FallbackReason string
}

type ActivationRequest struct {
	PersonaID string
	Seeds     []ActivationSeed
	Params    ActivationParams
}

type ActivationSeed struct {
	TriviumNodeID int64
	SQLiteNodeID  string
	NodeType      string
	SeedEnergy    float64
}

type ActivationParams struct {
	MaxHops             int
	HopDecay            float64
	MinEnergy           float64
	MaxActiveNodes      int
	HubSuppressionPower float64
	IncludePaths        bool
}

type ActivationCandidate struct {
	TriviumNodeID int64
	Score         float64
	Source        string
	Rank          int
	Paths         []ActivationPath
}

type ActivationPath struct {
	TriviumNodeIDs []int64
	LinkTypes      []string
}

type ActivationResult struct {
	Candidates     []ActivationCandidate
	Degraded       bool
	FallbackReason string
}

func NewSidecarClient(options SidecarClientOptions) *SidecarClient {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultSidecarTimeout
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &SidecarClient{
		baseURL:    strings.TrimRight(strings.TrimSpace(options.BaseURL), "/"),
		httpClient: httpClient,
	}
}

func ValidateLoopbackURL(baseURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return fmt.Errorf("sidecar URL is invalid: %w", err)
	}
	if parsed.Scheme != "http" {
		return fmt.Errorf("sidecar URL must use http loopback")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("sidecar URL must not include query or fragment")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("sidecar URL must include a loopback host")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("sidecar URL host must be loopback")
	}
	if !addr.IsLoopback() {
		return fmt.Errorf("sidecar URL host must be loopback")
	}
	return nil
}

func (c *SidecarClient) endpoint(path string) (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("sidecar base URL is required")
	}
	if err := ValidateLoopbackURL(c.baseURL); err != nil {
		return "", err
	}
	return c.baseURL + path, nil
}

func (c *SidecarClient) Health(ctx context.Context) error {
	endpoint, err := c.endpoint("/health")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sidecar health request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("sidecar health status %d", resp.StatusCode)
	}
	return nil
}

func (c *SidecarClient) UpsertNode(ctx context.Context, payload NodePayload) (NodeUpsertResult, error) {
	response, err := c.doOperation(ctx, OperationUpsertNode, nodePayloadJSON(payload), nil)
	if err != nil {
		return NodeUpsertResult{}, err
	}
	if response.TriviumNodeID <= 0 {
		return NodeUpsertResult{}, fmt.Errorf("sidecar response missing positive trivium_node_id")
	}
	return NodeUpsertResult{MirrorNodeID: response.TriviumNodeID}, nil
}

func (c *SidecarClient) DeleteNode(ctx context.Context, ref NodeRef) error {
	_, err := c.doOperation(ctx, OperationDeleteNode, nodeRefJSON(ref), nil)
	return err
}

func (c *SidecarClient) UpsertEdge(ctx context.Context, payload EdgePayload) error {
	_, err := c.doOperation(ctx, OperationUpsertEdge, nil, edgePayloadJSON(payload))
	return err
}

func (c *SidecarClient) DeleteEdge(ctx context.Context, ref EdgeRef) error {
	_, err := c.doOperation(ctx, OperationDeleteEdge, nil, edgeRefJSON(ref))
	return err
}

func (c *SidecarClient) ClearNamespace(ctx context.Context, personaID string) error {
	endpoint, err := c.endpoint("/mirror/clear-namespace")
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"schema_version": sidecarClearRequestSchemaVersion,
		"persona_id":     strings.TrimSpace(personaID),
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sidecar clear namespace request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(data))
		if message == "" {
			return fmt.Errorf("sidecar clear namespace status %d", resp.StatusCode)
		}
		return fmt.Errorf("sidecar clear namespace status %d: %s", resp.StatusCode, message)
	}
	var response sidecarOperationResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("sidecar clear namespace response decode: %w", err)
	}
	if response.SchemaVersion != sidecarClearResponseSchemaVersion {
		return fmt.Errorf("sidecar clear namespace response schema mismatch: %q", response.SchemaVersion)
	}
	if response.Status != "ok" {
		if strings.TrimSpace(response.Error) != "" {
			return fmt.Errorf("sidecar clear namespace error: %s", response.Error)
		}
		return fmt.Errorf("sidecar clear namespace error status %q", response.Status)
	}
	return nil
}

func (c *SidecarClient) FindCandidates(ctx context.Context, request CandidateRequest) (CandidateResult, error) {
	endpoint, err := c.endpoint("/retrieval/candidates")
	if err != nil {
		return CandidateResult{}, err
	}
	requestID := candidateRequestID(request)
	body, err := json.Marshal(map[string]any{
		"schema_version": sidecarCandidateRequestSchemaVersion,
		"request_id":     requestID,
		"persona_id":     strings.TrimSpace(request.PersonaID),
		"query_text":     request.QueryText,
		"limit":          request.Limit,
	})
	if err != nil {
		return CandidateResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return CandidateResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return CandidateResult{}, fmt.Errorf("sidecar candidates request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(data))
		if message == "" {
			return CandidateResult{}, fmt.Errorf("sidecar candidates status %d", resp.StatusCode)
		}
		return CandidateResult{}, fmt.Errorf("sidecar candidates status %d: %s", resp.StatusCode, message)
	}
	var response sidecarCandidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return CandidateResult{}, fmt.Errorf("sidecar candidates response decode: %w", err)
	}
	if response.SchemaVersion != sidecarCandidateResponseSchemaVersion {
		return CandidateResult{}, fmt.Errorf("sidecar candidates response schema mismatch: %q", response.SchemaVersion)
	}
	if response.RequestID != requestID {
		return CandidateResult{}, fmt.Errorf("sidecar candidates response request_id mismatch: %q", response.RequestID)
	}
	result := CandidateResult{
		Candidates:     make([]Candidate, 0, len(response.Candidates)),
		Degraded:       response.Degraded,
		FallbackReason: response.FallbackReason,
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 8
	}
	for _, candidate := range response.Candidates {
		if len(result.Candidates) >= limit {
			break
		}
		score, ok := normalizedCandidateScore(candidate.Score)
		if candidate.TriviumNodeID <= 0 || !ok {
			continue
		}
		result.Candidates = append(result.Candidates, Candidate{
			TriviumNodeID: candidate.TriviumNodeID,
			Score:         score,
			Source:        candidate.Source,
		})
	}
	return result, nil
}

func (c *SidecarClient) ActivateGraph(ctx context.Context, request ActivationRequest) (ActivationResult, error) {
	endpoint, err := c.endpoint("/retrieval/activate")
	if err != nil {
		return ActivationResult{}, err
	}
	requestID := activationRequestID(request)
	body, err := json.Marshal(map[string]any{
		"schema_version": sidecarActivationRequestSchemaVersion,
		"request_id":     requestID,
		"persona_id":     strings.TrimSpace(request.PersonaID),
		"seeds":          activationSeedsJSON(request.Seeds),
		"params":         activationParamsJSON(request.Params),
	})
	if err != nil {
		return ActivationResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ActivationResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return ActivationResult{}, fmt.Errorf("sidecar activation request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(data))
		if message == "" {
			return ActivationResult{}, fmt.Errorf("sidecar activation status %d", resp.StatusCode)
		}
		return ActivationResult{}, fmt.Errorf("sidecar activation status %d: %s", resp.StatusCode, message)
	}
	var response sidecarActivationResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return ActivationResult{}, fmt.Errorf("sidecar activation response decode: %w", err)
	}
	if response.SchemaVersion != sidecarActivationResponseSchemaVersion {
		return ActivationResult{}, fmt.Errorf("sidecar activation response schema mismatch: %q", response.SchemaVersion)
	}
	if response.RequestID != requestID {
		return ActivationResult{}, fmt.Errorf("sidecar activation response request_id mismatch: %q", response.RequestID)
	}
	result := ActivationResult{
		Candidates:     make([]ActivationCandidate, 0, len(response.Candidates)),
		Degraded:       response.Degraded,
		FallbackReason: response.FallbackReason,
	}
	limit := request.Params.MaxActiveNodes
	if limit <= 0 {
		limit = 80
	}
	for idx, candidate := range response.Candidates {
		if len(result.Candidates) >= limit {
			break
		}
		score, ok := normalizedCandidateScore(candidate.Score)
		if candidate.TriviumNodeID <= 0 || !ok {
			continue
		}
		rank := candidate.Rank
		if rank <= 0 {
			rank = idx + 1
		}
		source := strings.TrimSpace(candidate.Source)
		if source == "" {
			source = "graph_activation"
		}
		result.Candidates = append(result.Candidates, ActivationCandidate{
			TriviumNodeID: candidate.TriviumNodeID,
			Score:         score,
			Source:        source,
			Rank:          rank,
			Paths:         activationPathsFromResponse(candidate.Paths),
		})
	}
	return result, nil
}

type sidecarOperationRequest struct {
	SchemaVersion string    `json:"schema_version"`
	OperationID   string    `json:"operation_id"`
	PersonaID     string    `json:"persona_id"`
	Operation     Operation `json:"operation"`
	Node          any       `json:"node,omitempty"`
	Edge          any       `json:"edge,omitempty"`
}

type sidecarOperationResponse struct {
	SchemaVersion string `json:"schema_version"`
	OperationID   string `json:"operation_id,omitempty"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	TriviumNodeID int64  `json:"trivium_node_id,omitempty"`
}

type sidecarCandidateResponse struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id,omitempty"`
	Candidates    []struct {
		TriviumNodeID int64   `json:"trivium_node_id"`
		Score         float64 `json:"score"`
		Source        string  `json:"source"`
	} `json:"candidates"`
	Degraded       bool   `json:"degraded"`
	FallbackReason string `json:"fallback_reason,omitempty"`
}

type sidecarActivationResponse struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id,omitempty"`
	Candidates    []struct {
		TriviumNodeID int64   `json:"trivium_node_id"`
		Score         float64 `json:"score"`
		Source        string  `json:"source"`
		Rank          int     `json:"rank,omitempty"`
		Paths         []struct {
			TriviumNodeIDs []int64  `json:"trivium_node_ids"`
			LinkTypes      []string `json:"link_types"`
		} `json:"paths,omitempty"`
	} `json:"candidates"`
	Degraded       bool   `json:"degraded"`
	FallbackReason string `json:"fallback_reason,omitempty"`
}

func (c *SidecarClient) doOperation(ctx context.Context, operation Operation, node any, edge any) (sidecarOperationResponse, error) {
	endpoint, err := c.endpoint("/mirror/operation")
	if err != nil {
		return sidecarOperationResponse{}, err
	}
	personaID := operationPersonaID(node, edge)
	operationID := operationRequestID(operation, node, edge)
	body, err := json.Marshal(sidecarOperationRequest{
		SchemaVersion: sidecarRequestSchemaVersion,
		OperationID:   operationID,
		PersonaID:     personaID,
		Operation:     operation,
		Node:          node,
		Edge:          edge,
	})
	if err != nil {
		return sidecarOperationResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return sidecarOperationResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return sidecarOperationResponse{}, fmt.Errorf("sidecar request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(data))
		if message == "" {
			return sidecarOperationResponse{}, fmt.Errorf("sidecar status %d", resp.StatusCode)
		}
		return sidecarOperationResponse{}, fmt.Errorf("sidecar status %d: %s", resp.StatusCode, message)
	}

	var response sidecarOperationResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return sidecarOperationResponse{}, fmt.Errorf("sidecar response decode: %w", err)
	}
	if response.SchemaVersion != sidecarResponseSchemaVersion {
		return sidecarOperationResponse{}, fmt.Errorf("sidecar response schema mismatch: %q", response.SchemaVersion)
	}
	if response.OperationID != operationID {
		return sidecarOperationResponse{}, fmt.Errorf("sidecar response operation_id mismatch: %q", response.OperationID)
	}
	if response.Status != "ok" {
		if strings.TrimSpace(response.Error) != "" {
			return sidecarOperationResponse{}, fmt.Errorf("sidecar error: %s", response.Error)
		}
		return sidecarOperationResponse{}, fmt.Errorf("sidecar error status %q", response.Status)
	}
	return response, nil
}

func operationPersonaID(node any, edge any) string {
	if value := mapStringField(node, "persona_id"); value != "" {
		return value
	}
	return mapStringField(edge, "persona_id")
}

func operationRequestID(operation Operation, node any, edge any) string {
	switch operation {
	case OperationUpsertNode, OperationDeleteNode:
		return strings.Join([]string{
			string(operation),
			mapStringField(node, "persona_id"),
			mapStringField(node, "node_type"),
			mapStringField(node, "sqlite_node_id"),
		}, ":")
	case OperationUpsertEdge, OperationDeleteEdge:
		return strings.Join([]string{
			string(operation),
			mapStringField(edge, "persona_id"),
			mapStringField(edge, "sqlite_edge_id"),
		}, ":")
	default:
		return string(operation)
	}
}

func candidateRequestID(request CandidateRequest) string {
	return strings.Join([]string{
		"candidates",
		strings.TrimSpace(request.PersonaID),
		strings.TrimSpace(request.QueryText),
		fmt.Sprintf("%d", request.Limit),
	}, ":")
}

func activationRequestID(request ActivationRequest) string {
	parts := []string{
		"activate",
		strings.TrimSpace(request.PersonaID),
		fmt.Sprintf("h%d", request.Params.MaxHops),
		fmt.Sprintf("n%d", request.Params.MaxActiveNodes),
	}
	for _, seed := range request.Seeds {
		parts = append(parts, fmt.Sprintf("%d", seed.TriviumNodeID))
	}
	return strings.Join(parts, ":")
}

func normalizedCandidateScore(score float64) (float64, bool) {
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
		return 0, false
	}
	if score > 1 {
		return 1, true
	}
	return score, true
}

func activationSeedsJSON(seeds []ActivationSeed) []map[string]any {
	result := make([]map[string]any, 0, len(seeds))
	for _, seed := range seeds {
		result = append(result, map[string]any{
			"trivium_node_id": seed.TriviumNodeID,
			"sqlite_node_id":  seed.SQLiteNodeID,
			"node_type":       seed.NodeType,
			"seed_energy":     seed.SeedEnergy,
		})
	}
	return result
}

func activationParamsJSON(params ActivationParams) map[string]any {
	return map[string]any{
		"max_hops":              params.MaxHops,
		"hop_decay":             params.HopDecay,
		"min_energy":            params.MinEnergy,
		"max_active_nodes":      params.MaxActiveNodes,
		"hub_suppression_power": params.HubSuppressionPower,
		"include_paths":         params.IncludePaths,
	}
}

func activationPathsFromResponse(paths []struct {
	TriviumNodeIDs []int64  `json:"trivium_node_ids"`
	LinkTypes      []string `json:"link_types"`
}) []ActivationPath {
	result := make([]ActivationPath, 0, len(paths))
	for _, path := range paths {
		if len(path.TriviumNodeIDs) == 0 {
			continue
		}
		result = append(result, ActivationPath{
			TriviumNodeIDs: append([]int64(nil), path.TriviumNodeIDs...),
			LinkTypes:      append([]string(nil), path.LinkTypes...),
		})
	}
	return result
}

func mapStringField(value any, field string) string {
	item, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	stringValue, _ := item[field].(string)
	return stringValue
}

func nodePayloadJSON(payload NodePayload) map[string]any {
	return map[string]any{
		"persona_id":      payload.PersonaID,
		"node_type":       payload.NodeType,
		"sqlite_node_id":  payload.SQLiteNodeID,
		"searchable_text": payload.SearchableText,
		"payload":         payload.Payload,
	}
}

func nodeRefJSON(ref NodeRef) map[string]any {
	return map[string]any{
		"persona_id":     ref.PersonaID,
		"node_type":      ref.NodeType,
		"sqlite_node_id": ref.SQLiteNodeID,
	}
}

func edgePayloadJSON(payload EdgePayload) map[string]any {
	return map[string]any{
		"persona_id":     payload.PersonaID,
		"sqlite_edge_id": payload.SQLiteEdgeID,
		"link_type":      payload.LinkType,
		"from_node_type": payload.FromNodeType,
		"from_node_id":   payload.FromNodeID,
		"to_node_type":   payload.ToNodeType,
		"to_node_id":     payload.ToNodeID,
		"direction":      payload.Direction,
		"confidence":     payload.Confidence,
		"weight":         payload.Weight,
		"payload":        payload.Payload,
	}
}

func edgeRefJSON(ref EdgeRef) map[string]any {
	item := map[string]any{
		"persona_id":     ref.PersonaID,
		"sqlite_edge_id": ref.SQLiteEdgeID,
		"link_type":      ref.LinkType,
		"from_node_type": ref.FromNodeType,
		"from_node_id":   ref.FromNodeID,
		"to_node_type":   ref.ToNodeType,
		"to_node_id":     ref.ToNodeID,
	}
	if ref.FromMirrorNodeID != nil {
		item["from_mirror_node_id"] = *ref.FromMirrorNodeID
	}
	if ref.ToMirrorNodeID != nil {
		item["to_mirror_node_id"] = *ref.ToMirrorNodeID
	}
	return item
}
