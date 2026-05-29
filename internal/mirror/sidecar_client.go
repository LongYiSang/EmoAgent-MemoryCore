package mirror

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	sidecarCandidateRequestSchemaVersion   = "memory_mirror_candidate_request.v0.2"
	sidecarCandidateResponseSchemaVersion  = "memory_mirror_candidates.v0.2"
	sidecarDedupSearchSchemaVersion        = "memory_dedup_search.v0.1"
	sidecarDeleteCandidatesSchemaVersion   = "memory_delete_candidates.v0.1"
	sidecarActivationRequestSchemaVersion  = "memory_graph_activation_request.v0.1"
	sidecarActivationResponseSchemaVersion = "memory_graph_activation_result.v0.1"
	sidecarRerankRequestSchemaVersion      = "memory_rerank_request.v0.1"
	sidecarRerankResponseSchemaVersion     = "memory_rerank_result.v0.1"
	sidecarEvalConfigRequestSchemaVersion  = "memory_eval_sidecar_config.v0.1"
	sidecarEvalConfigResponseSchemaVersion = "memory_eval_sidecar_config_result.v0.1"
	defaultSidecarTimeout                  = 10 * time.Second
	maxRerankDebugReasonRunes              = 160
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
	Query     QueryAnalysis
	Limit     int
}

type Candidate struct {
	TriviumNodeID   int64
	Score           float64
	Source          string
	PrimaryPurpose  string
	Rank            int
	HitCount        int
	SourceBreakdown []CandidateSourceBreakdown
}

type CandidateSourceBreakdown struct {
	Source  string
	Purpose string
	Rank    int
	Score   float64
	Weight  float64
}

type CandidateDiagnostics struct {
	QueryCount                   int
	RawQueryCount                int
	RewriteQueryCount            int
	AnchorQueryCount             int
	MergedCandidateCount         int
	QueryTrimCount               int
	DenseEmbeddingWallLatencyMs  int64
	DenseEmbeddingBatchLatencyMs int64
	DenseSearchTotalLatencyMs    int64
	QueryCountTrimmedByBudget    int
	PerQuery                     []CandidatePerQueryDiagnostics
}

type CandidatePerQueryDiagnostics struct {
	Source    string
	Purpose   string
	Count     int
	LatencyMs int64
}

type CandidateResult struct {
	Candidates             []Candidate
	Degraded               bool
	FallbackReason         string
	EmbeddingCacheHits     int
	EmbeddingCacheMisses   int
	EmbeddingLiveCallCount int
	Diagnostics            CandidateDiagnostics
}

type DedupSearchRequest struct {
	RequestID string
	PersonaID string
	Candidate DedupSearchCandidate
	Policy    DedupSearchPolicy
}

type DedupSearchCandidate struct {
	CandidateID      string
	SafeSummary      string
	FactType         string
	Predicate        string
	SubjectEntityID  string
	ObjectEntityID   *string
	ObjectLiteral    string
	SourceEpisodeIDs []string
}

type DedupSearchPolicy struct {
	Limit             int
	SameSubjectBoost  bool
	SameFactTypeBoost bool
	ThresholdProfile  string
	Shadow            bool
}

type DedupSearchCandidateResult struct {
	NodeType    string
	NodeID      string
	Similarity  float64
	MatchClass  string
	MatchReason string
	MergeHint   string
}

type DedupSearchResult struct {
	RequestID      string
	Status         string
	Degraded       bool
	FallbackReason string
	Candidates     []DedupSearchCandidateResult
	Diagnostics    map[string]any
}

type DeleteCandidatesRequest struct {
	RequestID string
	PersonaID string
	Intent    DeleteCandidateIntent
	Scope     DeleteCandidateScope
	Policy    DeleteCandidatePolicy
}

type DeleteCandidateIntent struct {
	RawText             string
	OperationPurpose    string
	OperationTargetOnly bool
}

type DeleteCandidateScope struct {
	SessionID           string
	RecentPromptItemIDs []string
	EntityIDs           []string
	TimeWindow          map[string]string
}

type DeleteCandidatePolicy struct {
	Limit                  int
	AllowEpisodeCandidates bool
	AllowFactCandidates    bool
	IncludeSafeSummary     bool
}

type DeleteCandidate struct {
	NodeType    string
	NodeID      string
	SafeSummary string
	Score       float64
	WhyMatched  []string
	RiskFlags   []string
}

type DeleteCandidatesResult struct {
	RequestID       string
	Status          string
	Degraded        bool
	FallbackReason  string
	PreviewHashSeed string
	Candidates      []DeleteCandidate
	Diagnostics     map[string]any
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
	MaxHops                   int
	HopDecay                  float64
	MinEnergy                 float64
	MaxActiveNodes            int
	HubSuppressionPower       float64
	IncludePaths              bool
	MaxEdgesScannedPerRequest int
	MaxNeighborsPerNode       int
	MaxActivationWallMs       float64
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

type RerankRequest struct {
	PersonaID  string
	QueryText  string
	Candidates []RerankCandidate
}

type RerankCandidate struct {
	NodeID       string
	NodeType     string
	SafeSummary  string
	CurrentScore float64
	AnchorEnergy float64
	GraphEnergy  float64
	SourceScores map[string]float64
}

type RerankResult struct {
	Items          []RerankItem
	Degraded       bool
	FallbackReason string
}

type RerankItem struct {
	NodeID      string
	NodeType    string
	RerankScore float64
	DebugReason string
}

type EvalConfigRequest struct {
	TriviumDir               string
	EmbeddingCacheMode       string
	EmbeddingCacheDBPath     string
	SearchableTextVersion    string
	TextNormalizationVersion string
}

type EvalConfigResult struct {
	TriviumDir              string
	EmbeddingCacheMode      string
	EmbeddingCacheDBPath    string
	Embedding               map[string]string
	TriviumAdapterVersion   string
	TriviumDBVersion        string
	RerankProviderAvailable bool
	RerankProviderMode      string
	RerankCapabilityReason  string
	RerankCache             bool
	MirrorStatsAvailable    bool
	MirrorStatsError        string
	MirrorNodeCount         int
	MirrorEdgeCount         int
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

func (c *SidecarClient) ConfigureEval(ctx context.Context, request EvalConfigRequest) (EvalConfigResult, error) {
	endpoint, err := c.endpoint("/eval/configure")
	if err != nil {
		return EvalConfigResult{}, err
	}
	body, err := json.Marshal(map[string]any{
		"schema_version":             sidecarEvalConfigRequestSchemaVersion,
		"trivium_dir":                strings.TrimSpace(request.TriviumDir),
		"embedding_cache_mode":       strings.TrimSpace(request.EmbeddingCacheMode),
		"embedding_cache_db_path":    strings.TrimSpace(request.EmbeddingCacheDBPath),
		"searchable_text_version":    strings.TrimSpace(request.SearchableTextVersion),
		"text_normalization_version": strings.TrimSpace(request.TextNormalizationVersion),
	})
	if err != nil {
		return EvalConfigResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return EvalConfigResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return EvalConfigResult{}, fmt.Errorf("sidecar eval configure request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(data))
		if message == "" {
			return EvalConfigResult{}, fmt.Errorf("sidecar eval configure status %d", resp.StatusCode)
		}
		return EvalConfigResult{}, fmt.Errorf("sidecar eval configure status %d: %s", resp.StatusCode, message)
	}
	var response sidecarEvalConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return EvalConfigResult{}, fmt.Errorf("sidecar eval configure response decode: %w", err)
	}
	if response.SchemaVersion != sidecarEvalConfigResponseSchemaVersion {
		return EvalConfigResult{}, fmt.Errorf("sidecar eval configure response schema mismatch: %q", response.SchemaVersion)
	}
	if response.Status != "ok" {
		if strings.TrimSpace(response.Error) != "" {
			return EvalConfigResult{}, fmt.Errorf("sidecar eval configure error: %s", response.Error)
		}
		return EvalConfigResult{}, fmt.Errorf("sidecar eval configure error status %q", response.Status)
	}
	return EvalConfigResult{
		TriviumDir:              response.TriviumDir,
		EmbeddingCacheMode:      response.EmbeddingCacheMode,
		EmbeddingCacheDBPath:    response.EmbeddingCacheDBPath,
		Embedding:               cloneStringMap(response.Embedding),
		TriviumAdapterVersion:   response.TriviumAdapterVersion,
		TriviumDBVersion:        response.TriviumDBVersion,
		RerankProviderAvailable: response.RerankProviderAvailable,
		RerankProviderMode:      response.RerankProviderMode,
		RerankCapabilityReason:  response.RerankCapabilityReason,
		RerankCache:             response.RerankCache,
		MirrorStatsAvailable:    response.MirrorStatsAvailable,
		MirrorStatsError:        response.MirrorStatsError,
		MirrorNodeCount:         response.MirrorNodeCount,
		MirrorEdgeCount:         response.MirrorEdgeCount,
	}, nil
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
		"query":          candidateQueryJSON(request.Query, request.QueryText),
		"limit":          request.Limit,
		"debug_scores":   true,
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
		Candidates:             make([]Candidate, 0, len(response.Candidates)),
		Degraded:               response.Degraded,
		FallbackReason:         response.FallbackReason,
		EmbeddingCacheHits:     response.EmbeddingCacheStats.Hits,
		EmbeddingCacheMisses:   response.EmbeddingCacheStats.Misses,
		EmbeddingLiveCallCount: response.EmbeddingCacheStats.LiveCallCount,
		Diagnostics:            candidateDiagnosticsFromResponse(response.Diagnostics),
	}
	limit := request.Limit
	if limit <= 0 {
		limit = 8
	}
	for _, candidate := range response.Candidates {
		if len(result.Candidates) >= limit {
			break
		}
		score, ok := normalizedCandidateScore(candidate.FusedScore)
		if candidate.TriviumNodeID <= 0 || !ok {
			continue
		}
		result.Candidates = append(result.Candidates, Candidate{
			TriviumNodeID:   candidate.TriviumNodeID,
			Score:           score,
			Source:          strings.TrimSpace(candidate.PrimarySource),
			PrimaryPurpose:  strings.TrimSpace(candidate.PrimaryPurpose),
			Rank:            candidate.Rank,
			HitCount:        candidate.HitCount,
			SourceBreakdown: candidateSourceBreakdownFromResponse(candidate.SourceBreakdown),
		})
	}
	return result, nil
}

func (c *SidecarClient) DedupSearch(ctx context.Context, request DedupSearchRequest) (DedupSearchResult, error) {
	endpoint, err := c.endpoint("/memory/dedup-search")
	if err != nil {
		return DedupSearchResult{}, err
	}
	requestID := strings.TrimSpace(request.RequestID)
	if requestID == "" {
		requestID = dedupSearchRequestID(request)
	}
	body, err := json.Marshal(map[string]any{
		"schema_version": sidecarDedupSearchSchemaVersion,
		"request_id":     requestID,
		"persona_id":     strings.TrimSpace(request.PersonaID),
		"candidate":      dedupSearchCandidateJSON(request.Candidate),
		"policy":         dedupSearchPolicyJSON(request.Policy),
	})
	if err != nil {
		return DedupSearchResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return DedupSearchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DedupSearchResult{}, fmt.Errorf("sidecar dedup search request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(data))
		if message == "" {
			return DedupSearchResult{}, fmt.Errorf("sidecar dedup search status %d", resp.StatusCode)
		}
		return DedupSearchResult{}, fmt.Errorf("sidecar dedup search status %d: %s", resp.StatusCode, message)
	}
	var response sidecarDedupSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return DedupSearchResult{}, fmt.Errorf("sidecar dedup search response decode: %w", err)
	}
	if response.SchemaVersion != sidecarDedupSearchSchemaVersion {
		return DedupSearchResult{}, fmt.Errorf("sidecar dedup search response schema mismatch: %q", response.SchemaVersion)
	}
	if response.RequestID != requestID {
		return DedupSearchResult{}, fmt.Errorf("sidecar dedup search response request_id mismatch: %q", response.RequestID)
	}
	result := DedupSearchResult{
		RequestID:      response.RequestID,
		Status:         response.Status,
		Degraded:       response.Degraded,
		FallbackReason: response.FallbackReason,
		Candidates:     make([]DedupSearchCandidateResult, 0, len(response.Candidates)),
		Diagnostics:    response.Diagnostics,
	}
	limit := request.Policy.Limit
	if limit <= 0 {
		limit = 12
	}
	for _, candidate := range response.Candidates {
		if len(result.Candidates) >= limit {
			break
		}
		nodeType := strings.TrimSpace(candidate.NodeType)
		nodeID := strings.TrimSpace(candidate.NodeID)
		score, ok := normalizedCandidateScore(candidate.Similarity)
		if nodeType == "" || nodeID == "" || !ok {
			continue
		}
		result.Candidates = append(result.Candidates, DedupSearchCandidateResult{
			NodeType:    nodeType,
			NodeID:      nodeID,
			Similarity:  score,
			MatchClass:  strings.TrimSpace(candidate.MatchClass),
			MatchReason: strings.TrimSpace(candidate.MatchReason),
			MergeHint:   strings.TrimSpace(candidate.MergeHint),
		})
	}
	return result, nil
}

func (c *SidecarClient) DeleteCandidates(ctx context.Context, request DeleteCandidatesRequest) (DeleteCandidatesResult, error) {
	endpoint, err := c.endpoint("/memory/delete-candidates")
	if err != nil {
		return DeleteCandidatesResult{}, err
	}
	requestID := strings.TrimSpace(request.RequestID)
	if requestID == "" {
		requestID = deleteCandidatesRequestID(request)
	}
	body, err := json.Marshal(map[string]any{
		"schema_version": sidecarDeleteCandidatesSchemaVersion,
		"request_id":     requestID,
		"persona_id":     strings.TrimSpace(request.PersonaID),
		"intent":         deleteCandidateIntentJSON(request.Intent),
		"scope":          deleteCandidateScopeJSON(request.Scope),
		"policy":         deleteCandidatePolicyJSON(request.Policy),
	})
	if err != nil {
		return DeleteCandidatesResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return DeleteCandidatesResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DeleteCandidatesResult{}, fmt.Errorf("sidecar delete candidates request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(data))
		if message == "" {
			return DeleteCandidatesResult{}, fmt.Errorf("sidecar delete candidates status %d", resp.StatusCode)
		}
		return DeleteCandidatesResult{}, fmt.Errorf("sidecar delete candidates status %d: %s", resp.StatusCode, message)
	}
	var response sidecarDeleteCandidatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return DeleteCandidatesResult{}, fmt.Errorf("sidecar delete candidates response decode: %w", err)
	}
	if response.SchemaVersion != sidecarDeleteCandidatesSchemaVersion {
		return DeleteCandidatesResult{}, fmt.Errorf("sidecar delete candidates response schema mismatch: %q", response.SchemaVersion)
	}
	if response.RequestID != requestID {
		return DeleteCandidatesResult{}, fmt.Errorf("sidecar delete candidates response request_id mismatch: %q", response.RequestID)
	}
	result := DeleteCandidatesResult{
		RequestID:       response.RequestID,
		Status:          response.Status,
		Degraded:        response.Degraded,
		FallbackReason:  response.FallbackReason,
		PreviewHashSeed: strings.TrimSpace(response.PreviewHashSeed),
		Candidates:      make([]DeleteCandidate, 0, len(response.Candidates)),
		Diagnostics:     response.Diagnostics,
	}
	limit := request.Policy.Limit
	if limit <= 0 {
		limit = 20
	}
	for _, candidate := range response.Candidates {
		if len(result.Candidates) >= limit {
			break
		}
		nodeType := strings.TrimSpace(candidate.NodeType)
		nodeID := strings.TrimSpace(candidate.NodeID)
		score, ok := normalizedCandidateScore(candidate.Score)
		if nodeType == "" || nodeID == "" || !ok {
			continue
		}
		result.Candidates = append(result.Candidates, DeleteCandidate{
			NodeType:    nodeType,
			NodeID:      nodeID,
			SafeSummary: strings.TrimSpace(candidate.SafeSummary),
			Score:       score,
			WhyMatched:  stringListJSON(candidate.WhyMatched),
			RiskFlags:   stringListJSON(candidate.RiskFlags),
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

func (c *SidecarClient) Rerank(ctx context.Context, request RerankRequest) (RerankResult, error) {
	endpoint, err := c.endpoint("/retrieval/rerank")
	if err != nil {
		return RerankResult{}, err
	}
	requestID := rerankRequestID(request)
	body, err := json.Marshal(map[string]any{
		"schema_version": sidecarRerankRequestSchemaVersion,
		"request_id":     requestID,
		"persona_id":     strings.TrimSpace(request.PersonaID),
		"query_text":     request.QueryText,
		"candidates":     rerankCandidatesJSON(request.Candidates),
	})
	if err != nil {
		return RerankResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return RerankResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return RerankResult{}, fmt.Errorf("sidecar rerank request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		message := strings.TrimSpace(string(data))
		if message == "" {
			return RerankResult{}, fmt.Errorf("sidecar rerank status %d", resp.StatusCode)
		}
		return RerankResult{}, fmt.Errorf("sidecar rerank status %d: %s", resp.StatusCode, message)
	}
	var response sidecarRerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return RerankResult{}, fmt.Errorf("sidecar rerank response decode: %w", err)
	}
	if response.SchemaVersion != sidecarRerankResponseSchemaVersion {
		return RerankResult{}, fmt.Errorf("sidecar rerank response schema mismatch: %q", response.SchemaVersion)
	}
	if response.RequestID != requestID {
		return RerankResult{}, fmt.Errorf("sidecar rerank response request_id mismatch: %q", response.RequestID)
	}
	result := RerankResult{
		Items:          make([]RerankItem, 0, len(response.Results)),
		Degraded:       response.Degraded,
		FallbackReason: response.FallbackReason,
	}
	allowed := map[string]string{}
	for _, candidate := range request.Candidates {
		if strings.TrimSpace(candidate.NodeID) == "" {
			continue
		}
		nodeType := strings.TrimSpace(candidate.NodeType)
		if nodeType == "" {
			nodeType = "fact"
		}
		allowed[candidate.NodeID] = nodeType
	}
	limit := len(allowed)
	for _, item := range response.Results {
		if limit > 0 && len(result.Items) >= limit {
			break
		}
		nodeID := strings.TrimSpace(item.NodeID)
		nodeType := strings.TrimSpace(item.NodeType)
		if nodeType == "" {
			nodeType = "fact"
		}
		if wantType, ok := allowed[nodeID]; !ok || wantType != nodeType {
			continue
		}
		score, ok := normalizedCandidateScore(item.RerankScore)
		if !ok {
			continue
		}
		result.Items = append(result.Items, RerankItem{
			NodeID:      nodeID,
			NodeType:    nodeType,
			RerankScore: score,
			DebugReason: sanitizeDebugReason(item.DebugReason, maxRerankDebugReasonRunes),
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
		TriviumNodeID   int64   `json:"trivium_node_id"`
		FusedScore      float64 `json:"fused_score"`
		PrimarySource   string  `json:"primary_source"`
		PrimaryPurpose  string  `json:"primary_purpose"`
		Rank            int     `json:"rank,omitempty"`
		HitCount        int     `json:"hit_count,omitempty"`
		SourceBreakdown []struct {
			Source  string  `json:"source,omitempty"`
			Purpose string  `json:"purpose,omitempty"`
			Rank    int     `json:"rank,omitempty"`
			Score   float64 `json:"score,omitempty"`
			Weight  float64 `json:"weight,omitempty"`
		} `json:"source_breakdown,omitempty"`
	} `json:"candidates"`
	Degraded            bool   `json:"degraded"`
	FallbackReason      string `json:"fallback_reason,omitempty"`
	EmbeddingCacheStats struct {
		Hits          int `json:"hits"`
		Misses        int `json:"misses"`
		LiveCallCount int `json:"live_call_count"`
	} `json:"embedding_cache_stats"`
	Diagnostics sidecarCandidateDiagnostics `json:"diagnostics,omitempty"`
}

type sidecarDedupSearchResponse struct {
	SchemaVersion  string `json:"schema_version"`
	RequestID      string `json:"request_id,omitempty"`
	Status         string `json:"status"`
	Degraded       bool   `json:"degraded"`
	FallbackReason string `json:"fallback_reason,omitempty"`
	Candidates     []struct {
		NodeType    string  `json:"node_type"`
		NodeID      string  `json:"node_id"`
		Similarity  float64 `json:"similarity"`
		MatchClass  string  `json:"match_class,omitempty"`
		MatchReason string  `json:"match_reason,omitempty"`
		MergeHint   string  `json:"merge_hint,omitempty"`
	} `json:"candidates"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
}

type sidecarDeleteCandidatesResponse struct {
	SchemaVersion   string `json:"schema_version"`
	RequestID       string `json:"request_id,omitempty"`
	Status          string `json:"status"`
	Degraded        bool   `json:"degraded"`
	FallbackReason  string `json:"fallback_reason,omitempty"`
	PreviewHashSeed string `json:"preview_hash_seed,omitempty"`
	Candidates      []struct {
		NodeType    string   `json:"node_type"`
		NodeID      string   `json:"node_id"`
		SafeSummary string   `json:"safe_summary,omitempty"`
		Score       float64  `json:"score"`
		WhyMatched  []string `json:"why_matched,omitempty"`
		RiskFlags   []string `json:"risk_flags,omitempty"`
	} `json:"candidates"`
	Diagnostics map[string]any `json:"diagnostics,omitempty"`
}

type sidecarCandidateDiagnostics struct {
	QueryCount                   int            `json:"query_count,omitempty"`
	RawQueryCount                int            `json:"raw_query_count,omitempty"`
	RewriteQueryCount            int            `json:"rewrite_query_count,omitempty"`
	AnchorQueryCount             int            `json:"anchor_query_count,omitempty"`
	MergedCandidateCount         int            `json:"merged_candidate_count,omitempty"`
	QueryTrims                   map[string]int `json:"query_trims,omitempty"`
	DenseEmbeddingWallLatencyMs  int64          `json:"dense_embedding_wall_latency_ms,omitempty"`
	DenseEmbeddingBatchLatencyMs int64          `json:"dense_embedding_batch_latency_ms,omitempty"`
	DenseSearchTotalLatencyMs    int64          `json:"dense_search_total_latency_ms,omitempty"`
	QueryCountTrimmedByBudget    int            `json:"query_count_trimmed_by_budget,omitempty"`
	PerQuery                     []struct {
		Source    string `json:"source,omitempty"`
		Purpose   string `json:"purpose,omitempty"`
		Count     int    `json:"count,omitempty"`
		LatencyMs int64  `json:"latency_ms,omitempty"`
	} `json:"per_query_counts,omitempty"`
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

type sidecarRerankResponse struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id,omitempty"`
	Results       []struct {
		NodeID      string  `json:"node_id"`
		NodeType    string  `json:"node_type"`
		RerankScore float64 `json:"rerank_score"`
		DebugReason string  `json:"debug_reason,omitempty"`
	} `json:"results"`
	Degraded       bool   `json:"degraded"`
	FallbackReason string `json:"fallback_reason,omitempty"`
}

type sidecarEvalConfigResponse struct {
	SchemaVersion           string            `json:"schema_version"`
	Status                  string            `json:"status"`
	Error                   string            `json:"error,omitempty"`
	TriviumDir              string            `json:"trivium_dir,omitempty"`
	EmbeddingCacheMode      string            `json:"embedding_cache_mode,omitempty"`
	EmbeddingCacheDBPath    string            `json:"embedding_cache_db_path,omitempty"`
	Embedding               map[string]string `json:"embedding,omitempty"`
	TriviumAdapterVersion   string            `json:"trivium_adapter_version,omitempty"`
	TriviumDBVersion        string            `json:"triviumdb_version,omitempty"`
	RerankProviderAvailable bool              `json:"rerank_provider_available"`
	RerankProviderMode      string            `json:"rerank_provider_mode,omitempty"`
	RerankCapabilityReason  string            `json:"rerank_capability_reason,omitempty"`
	RerankCache             bool              `json:"rerank_cache"`
	MirrorStatsAvailable    bool              `json:"mirror_stats_available"`
	MirrorStatsError        string            `json:"mirror_stats_error,omitempty"`
	MirrorNodeCount         int               `json:"mirror_node_count"`
	MirrorEdgeCount         int               `json:"mirror_edge_count"`
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
	queryText := request.Query.Raw
	if strings.TrimSpace(queryText) == "" {
		queryText = request.QueryText
	}
	return strings.Join([]string{
		"candidates",
		strings.TrimSpace(request.PersonaID),
		strings.TrimSpace(queryText),
		fmt.Sprintf("%d", request.Limit),
	}, ":")
}

func dedupSearchRequestID(request DedupSearchRequest) string {
	return strings.Join([]string{
		"dedup",
		strings.TrimSpace(request.PersonaID),
		strings.TrimSpace(request.Candidate.CandidateID),
		strings.TrimSpace(request.Candidate.SafeSummary),
		fmt.Sprintf("%d", request.Policy.Limit),
	}, ":")
}

func deleteCandidatesRequestID(request DeleteCandidatesRequest) string {
	limit := request.Policy.Limit
	if limit <= 0 {
		limit = 20
	}
	return "delete-candidates:" + shortStableHash(
		strings.TrimSpace(request.PersonaID),
		strings.TrimSpace(request.Intent.RawText),
		fmt.Sprintf("%d", limit),
	)
}

func shortStableHash(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	sum := fmt.Sprintf("%x", hash.Sum(nil))
	if len(sum) > 16 {
		return sum[:16]
	}
	return sum
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

func rerankRequestID(request RerankRequest) string {
	parts := []string{
		"rerank",
		strings.TrimSpace(request.PersonaID),
		strings.TrimSpace(request.QueryText),
		fmt.Sprintf("%d", len(request.Candidates)),
	}
	for _, candidate := range request.Candidates {
		parts = append(parts, strings.TrimSpace(candidate.NodeID))
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

func rerankCandidatesJSON(candidates []RerankCandidate) []map[string]any {
	result := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		item := map[string]any{
			"node_id":       candidate.NodeID,
			"node_type":     candidate.NodeType,
			"safe_summary":  candidate.SafeSummary,
			"current_score": candidate.CurrentScore,
			"anchor_energy": candidate.AnchorEnergy,
			"graph_energy":  candidate.GraphEnergy,
		}
		if len(candidate.SourceScores) > 0 {
			item["source_scores"] = candidate.SourceScores
		}
		result = append(result, item)
	}
	return result
}

func candidateQueryJSON(query QueryAnalysis, fallbackRaw string) map[string]any {
	raw := strings.TrimSpace(query.Raw)
	if raw == "" {
		raw = strings.TrimSpace(fallbackRaw)
	}
	normalized := strings.TrimSpace(query.Normalized)
	if normalized == "" {
		normalized = strings.ToLower(raw)
	}
	item := map[string]any{
		"raw_text":         raw,
		"normalized_text":  normalized,
		"time_mode":        strings.TrimSpace(query.TimeMode),
		"memory_domain":    strings.TrimSpace(query.MemoryDomain),
		"memory_ability":   strings.TrimSpace(query.MemoryAbility),
		"evidence_need":    strings.TrimSpace(query.EvidenceNeed),
		"signals":          stringListJSON(query.Signals),
		"rewrites":         queryRewritesJSON(query.QueryRewrites),
		"semantic_anchors": semanticAnchorsJSON(query.SemanticAnchors),
	}
	return item
}

func stringListJSON(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func queryRewritesJSON(values []QueryRewrite) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value.Text)
		if text == "" {
			continue
		}
		result = append(result, map[string]any{
			"text":    text,
			"purpose": strings.TrimSpace(value.Purpose),
			"weight":  value.Weight,
		})
	}
	return result
}

func semanticAnchorsJSON(values []SemanticAnchor) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value.Text)
		if text == "" {
			continue
		}
		result = append(result, map[string]any{
			"text":        text,
			"anchor_type": strings.TrimSpace(value.AnchorType),
			"entity_id":   strings.TrimSpace(value.EntityID),
			"weight":      value.Weight,
			"confidence":  value.Confidence,
		})
	}
	return result
}

func candidateDiagnosticsFromResponse(value sidecarCandidateDiagnostics) CandidateDiagnostics {
	out := CandidateDiagnostics{
		QueryCount:                   value.QueryCount,
		RawQueryCount:                value.RawQueryCount,
		RewriteQueryCount:            value.RewriteQueryCount,
		AnchorQueryCount:             value.AnchorQueryCount,
		MergedCandidateCount:         value.MergedCandidateCount,
		QueryTrimCount:               candidateQueryTrimCount(value.QueryTrims),
		DenseEmbeddingWallLatencyMs:  value.DenseEmbeddingWallLatencyMs,
		DenseEmbeddingBatchLatencyMs: value.DenseEmbeddingBatchLatencyMs,
		DenseSearchTotalLatencyMs:    value.DenseSearchTotalLatencyMs,
		QueryCountTrimmedByBudget:    value.QueryCountTrimmedByBudget,
		PerQuery:                     make([]CandidatePerQueryDiagnostics, 0, len(value.PerQuery)),
	}
	if out.DenseEmbeddingWallLatencyMs == 0 {
		out.DenseEmbeddingWallLatencyMs = value.DenseEmbeddingBatchLatencyMs
	}
	if out.DenseEmbeddingBatchLatencyMs == 0 {
		out.DenseEmbeddingBatchLatencyMs = out.DenseEmbeddingWallLatencyMs
	}
	for _, item := range value.PerQuery {
		out.PerQuery = append(out.PerQuery, CandidatePerQueryDiagnostics{
			Source:    strings.TrimSpace(item.Source),
			Purpose:   strings.TrimSpace(item.Purpose),
			Count:     item.Count,
			LatencyMs: item.LatencyMs,
		})
	}
	return out
}

func candidateQueryTrimCount(values map[string]int) int {
	total := 0
	for _, value := range values {
		if value > 0 {
			total += value
		}
	}
	return total
}

func candidateSourceBreakdownFromResponse(values []struct {
	Source  string  `json:"source,omitempty"`
	Purpose string  `json:"purpose,omitempty"`
	Rank    int     `json:"rank,omitempty"`
	Score   float64 `json:"score,omitempty"`
	Weight  float64 `json:"weight,omitempty"`
}) []CandidateSourceBreakdown {
	result := make([]CandidateSourceBreakdown, 0, len(values))
	for _, value := range values {
		source := strings.TrimSpace(value.Source)
		if source == "" {
			continue
		}
		score, ok := normalizedCandidateScore(value.Score)
		if !ok {
			score = 0
		}
		weight := value.Weight
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
			weight = 0
		}
		result = append(result, CandidateSourceBreakdown{
			Source:  source,
			Purpose: strings.TrimSpace(value.Purpose),
			Rank:    value.Rank,
			Score:   score,
			Weight:  weight,
		})
	}
	return result
}

func dedupSearchCandidateJSON(candidate DedupSearchCandidate) map[string]any {
	item := map[string]any{
		"candidate_id":       strings.TrimSpace(candidate.CandidateID),
		"safe_summary":       strings.TrimSpace(candidate.SafeSummary),
		"fact_type":          strings.TrimSpace(candidate.FactType),
		"predicate":          strings.TrimSpace(candidate.Predicate),
		"subject_entity_id":  strings.TrimSpace(candidate.SubjectEntityID),
		"object_literal":     strings.TrimSpace(candidate.ObjectLiteral),
		"source_episode_ids": stringListJSON(candidate.SourceEpisodeIDs),
	}
	if candidate.ObjectEntityID != nil && strings.TrimSpace(*candidate.ObjectEntityID) != "" {
		item["object_entity_id"] = strings.TrimSpace(*candidate.ObjectEntityID)
	}
	return item
}

func dedupSearchPolicyJSON(policy DedupSearchPolicy) map[string]any {
	limit := policy.Limit
	if limit <= 0 {
		limit = 12
	}
	return map[string]any{
		"limit":                limit,
		"same_subject_boost":   policy.SameSubjectBoost,
		"same_fact_type_boost": policy.SameFactTypeBoost,
		"threshold_profile":    strings.TrimSpace(policy.ThresholdProfile),
		"shadow":               policy.Shadow,
	}
}

func deleteCandidateIntentJSON(intent DeleteCandidateIntent) map[string]any {
	return map[string]any{
		"raw_text":              strings.TrimSpace(intent.RawText),
		"operation_purpose":     strings.TrimSpace(intent.OperationPurpose),
		"operation_target_only": intent.OperationTargetOnly,
	}
}

func deleteCandidateScopeJSON(scope DeleteCandidateScope) map[string]any {
	item := map[string]any{
		"recent_prompt_item_ids": stringListJSON(scope.RecentPromptItemIDs),
		"entity_ids":             stringListJSON(scope.EntityIDs),
	}
	if strings.TrimSpace(scope.SessionID) != "" {
		item["session_id"] = strings.TrimSpace(scope.SessionID)
	}
	if len(scope.TimeWindow) > 0 {
		item["time_window"] = scope.TimeWindow
	}
	return item
}

func deleteCandidatePolicyJSON(policy DeleteCandidatePolicy) map[string]any {
	limit := policy.Limit
	if limit <= 0 {
		limit = 20
	}
	allowEpisode := policy.AllowEpisodeCandidates
	allowFact := policy.AllowFactCandidates
	includeSafeSummary := policy.IncludeSafeSummary
	if !allowEpisode && !allowFact && !includeSafeSummary {
		allowEpisode = true
		allowFact = true
		includeSafeSummary = true
	}
	return map[string]any{
		"limit":                    limit,
		"allow_episode_candidates": allowEpisode,
		"allow_fact_candidates":    allowFact,
		"include_safe_summary":     includeSafeSummary,
	}
}

func sanitizeDebugReason(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		if r < 0x20 {
			return -1
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
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
		"max_hops":                      params.MaxHops,
		"hop_decay":                     params.HopDecay,
		"min_energy":                    params.MinEnergy,
		"max_active_nodes":              params.MaxActiveNodes,
		"hub_suppression_power":         params.HubSuppressionPower,
		"include_paths":                 params.IncludePaths,
		"max_edges_scanned_per_request": params.MaxEdgesScannedPerRequest,
		"max_neighbors_per_node":        params.MaxNeighborsPerNode,
		"max_activation_wall_ms":        params.MaxActivationWallMs,
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

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
