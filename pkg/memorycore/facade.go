package memorycore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appcore "github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
	internalmirror "github.com/longyisang/emoagent-memorycore/internal/mirror"
)

var (
	ErrInvalidOptions = errors.New("memorycore: invalid options")
	ErrInvalidRequest = errors.New("memorycore: invalid request")
	ErrNotFound       = errors.New("memorycore: not found")
)

type Client struct {
	service appcore.Service
}

type SessionAPI interface {
	StartSession(context.Context, StartSessionRequest) (*Session, error)
	EndSession(context.Context, EndSessionRequest) (*Session, error)
	AppendEpisode(context.Context, AppendEpisodeRequest) (*Episode, error)
}

type RetrievalAPI interface {
	Retrieve(context.Context, RetrievalRequest) (*MemoryContext, error)
}

type MemoryWriteAPI interface {
	EnsureEntity(context.Context, EnsureEntityRequest) (*Entity, error)
	AddEntityAlias(context.Context, AddEntityAliasRequest) (*EntityAlias, error)
	ConsolidateCandidate(context.Context, ConsolidateCandidateRequest) (*ConsolidationResult, error)
	RunExtraction(context.Context, RunExtractionRequest) (*ExtractionRunResult, error)
	RunExtractionBatch(context.Context, ExtractionBatchRequest) (*ExtractionBatchResult, error)
}

type ForgetAPI interface {
	PreviewForget(context.Context, ForgetPreviewRequest) (*ForgetPreviewResult, error)
	ExecuteForget(context.Context, ForgetExecuteRequest) (*ForgetExecuteResult, error)
	VerifyForget(context.Context, ForgetVerifyRequest) (*ForgetVerifyResult, error)
	GetPendingManualForgetOperation(context.Context, GetPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error)
	CancelPendingManualForgetOperation(context.Context, CancelPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error)
}

type OpsAPI interface {
	RebuildSearchDocuments(context.Context, RebuildSearchDocumentsRequest) (*RebuildSearchDocumentsResult, error)
	RunRetention(context.Context, RunRetentionRequest) (*RunRetentionResult, error)
	RunRetentionJobs(context.Context, RunRetentionJobsRequest) (*RunRetentionJobsResult, error)
	RunNaturalMemoryCycle(context.Context, RunNaturalMemoryCycleRequest) (*RunNaturalMemoryCycleResult, error)
	RunNaturalMemoryTick(context.Context, RunNaturalMemoryTickRequest) (*RunNaturalMemoryCycleResult, error)
	ApplyCompression(context.Context, ApplyCompressionRequest) (*ApplyCompressionResult, error)
	RunCuration(context.Context, RunCurationRequest) (*RunCurationResult, error)
	RunMirrorSync(context.Context, RunMirrorSyncRequest) (*RunMirrorSyncResult, error)
	RebuildMirror(context.Context, RebuildMirrorRequest) (*RebuildMirrorResult, error)
}

func Open(ctx context.Context, opts Options) (*Client, error) {
	appOpts, err := toAppOptions(opts)
	if err != nil {
		return nil, err
	}
	service, err := appcore.Open(ctx, appOpts)
	if err != nil {
		return nil, translateAppError(err)
	}
	return &Client{service: service}, nil
}

func (c *Client) Close() error {
	if c == nil || c.service == nil {
		return nil
	}
	return c.service.Close()
}

func (c *Client) Sessions() SessionAPI {
	return sessionClient{client: c}
}

func (c *Client) Retrieval() RetrievalAPI {
	return retrievalClient{client: c}
}

func (c *Client) Writes() MemoryWriteAPI {
	return writeClient{client: c}
}

func (c *Client) Forget() ForgetAPI {
	return forgetClient{client: c}
}

func (c *Client) Ops() OpsAPI {
	return opsClient{client: c}
}

func ValidateSidecarLoopbackURL(baseURL string) error {
	return internalmirror.ValidateLoopbackURL(baseURL)
}

type sessionClient struct {
	client *Client
}

func (s sessionClient) StartSession(ctx context.Context, req StartSessionRequest) (*Session, error) {
	appReq, err := convertValue[appcore.StartSessionRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := s.client.service.StartSession(ctx, appReq)
	return convertPtr[Session](result, err)
}

func (s sessionClient) EndSession(ctx context.Context, req EndSessionRequest) (*Session, error) {
	appReq, err := convertValue[appcore.EndSessionRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := s.client.service.EndSession(ctx, appReq)
	return convertPtr[Session](result, err)
}

func (s sessionClient) AppendEpisode(ctx context.Context, req AppendEpisodeRequest) (*Episode, error) {
	appReq, err := convertValue[appcore.AppendEpisodeRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := s.client.service.AppendEpisode(ctx, appReq)
	return convertPtr[Episode](result, err)
}

type retrievalClient struct {
	client *Client
}

func (r retrievalClient) Retrieve(ctx context.Context, req RetrievalRequest) (*MemoryContext, error) {
	appReq, err := convertValue[appcore.RetrievalRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := r.client.service.Retrieve(ctx, appReq)
	return convertPtr[MemoryContext](result, err)
}

type writeClient struct {
	client *Client
}

func (w writeClient) EnsureEntity(ctx context.Context, req EnsureEntityRequest) (*Entity, error) {
	appReq, err := convertValue[appcore.EnsureEntityRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := w.client.service.EnsureEntity(ctx, appReq)
	return convertPtr[Entity](result, err)
}

func (w writeClient) AddEntityAlias(ctx context.Context, req AddEntityAliasRequest) (*EntityAlias, error) {
	appReq, err := convertValue[appcore.AddEntityAliasRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := w.client.service.AddEntityAlias(ctx, appReq)
	return convertPtr[EntityAlias](result, err)
}

func (w writeClient) ConsolidateCandidate(ctx context.Context, req ConsolidateCandidateRequest) (*ConsolidationResult, error) {
	appReq, err := convertValue[appcore.ConsolidateCandidateRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := w.client.service.ConsolidateCandidate(ctx, appReq)
	return convertPtr[ConsolidationResult](result, err)
}

func (w writeClient) RunExtraction(ctx context.Context, req RunExtractionRequest) (*ExtractionRunResult, error) {
	appReq, err := convertValue[appcore.RunExtractionRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := w.client.service.RunExtraction(ctx, appReq)
	return convertPtr[ExtractionRunResult](result, err)
}

func (w writeClient) RunExtractionBatch(ctx context.Context, req ExtractionBatchRequest) (*ExtractionBatchResult, error) {
	appReq, err := convertValue[appcore.ExtractionBatchRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := w.client.service.RunExtractionBatch(ctx, appReq)
	return convertPtr[ExtractionBatchResult](result, err)
}

type forgetClient struct {
	client *Client
}

func (f forgetClient) PreviewForget(ctx context.Context, req ForgetPreviewRequest) (*ForgetPreviewResult, error) {
	appReq, err := convertValue[appcore.ForgetPreviewRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := f.client.service.PreviewForget(ctx, appReq)
	return convertPtr[ForgetPreviewResult](result, err)
}

func (f forgetClient) ExecuteForget(ctx context.Context, req ForgetExecuteRequest) (*ForgetExecuteResult, error) {
	appReq, err := convertValue[appcore.ForgetExecuteRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := f.client.service.ExecuteForget(ctx, appReq)
	return convertPtr[ForgetExecuteResult](result, err)
}

func (f forgetClient) VerifyForget(ctx context.Context, req ForgetVerifyRequest) (*ForgetVerifyResult, error) {
	appReq, err := convertValue[appcore.ForgetVerifyRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := f.client.service.VerifyForget(ctx, appReq)
	return convertPtr[ForgetVerifyResult](result, err)
}

func (f forgetClient) GetPendingManualForgetOperation(ctx context.Context, req GetPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error) {
	appReq, err := convertValue[appcore.GetPendingManualForgetOperationRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := f.client.service.GetPendingManualForgetOperation(ctx, appReq)
	return convertPtr[PendingManualForgetOperation](result, err)
}

func (f forgetClient) CancelPendingManualForgetOperation(ctx context.Context, req CancelPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error) {
	appReq, err := convertValue[appcore.CancelPendingManualForgetOperationRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := f.client.service.CancelPendingManualForgetOperation(ctx, appReq)
	return convertPtr[PendingManualForgetOperation](result, err)
}

type opsClient struct {
	client *Client
}

func (o opsClient) RebuildSearchDocuments(ctx context.Context, req RebuildSearchDocumentsRequest) (*RebuildSearchDocumentsResult, error) {
	appReq, err := convertValue[appcore.RebuildSearchDocumentsRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.RebuildSearchDocuments(ctx, appReq)
	return convertPtr[RebuildSearchDocumentsResult](result, err)
}

func (o opsClient) RunRetention(ctx context.Context, req RunRetentionRequest) (*RunRetentionResult, error) {
	appReq, err := convertValue[appcore.RunRetentionRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.RunRetention(ctx, appReq)
	return convertPtr[RunRetentionResult](result, err)
}

func (o opsClient) RunRetentionJobs(ctx context.Context, req RunRetentionJobsRequest) (*RunRetentionJobsResult, error) {
	appReq, err := convertValue[appcore.RunRetentionJobsRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.RunRetentionJobs(ctx, appReq)
	return convertPtr[RunRetentionJobsResult](result, err)
}

func (o opsClient) RunNaturalMemoryCycle(ctx context.Context, req RunNaturalMemoryCycleRequest) (*RunNaturalMemoryCycleResult, error) {
	appReq, err := convertValue[appcore.RunNaturalMemoryCycleRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.RunNaturalMemoryCycle(ctx, appReq)
	return convertPtr[RunNaturalMemoryCycleResult](result, err)
}

func (o opsClient) RunNaturalMemoryTick(ctx context.Context, req RunNaturalMemoryTickRequest) (*RunNaturalMemoryCycleResult, error) {
	appReq, err := convertValue[appcore.RunNaturalMemoryTickRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.RunNaturalMemoryTick(ctx, appReq)
	return convertPtr[RunNaturalMemoryCycleResult](result, err)
}

func (o opsClient) ApplyCompression(ctx context.Context, req ApplyCompressionRequest) (*ApplyCompressionResult, error) {
	appReq, err := convertValue[appcore.ApplyCompressionRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.ApplyCompression(ctx, appReq)
	return convertPtr[ApplyCompressionResult](result, err)
}

func (o opsClient) RunCuration(ctx context.Context, req RunCurationRequest) (*RunCurationResult, error) {
	appReq, err := convertValue[appcore.RunCurationRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.RunCuration(ctx, appReq)
	return convertPtr[RunCurationResult](result, err)
}

func (o opsClient) RunMirrorSync(ctx context.Context, req RunMirrorSyncRequest) (*RunMirrorSyncResult, error) {
	appReq, err := convertValue[appcore.RunMirrorSyncRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.RunMirrorSync(ctx, appReq)
	return convertPtr[RunMirrorSyncResult](result, err)
}

func (o opsClient) RebuildMirror(ctx context.Context, req RebuildMirrorRequest) (*RebuildMirrorResult, error) {
	appReq, err := convertValue[appcore.RebuildMirrorRequest](req)
	if err != nil {
		return nil, err
	}
	result, err := o.client.service.RebuildMirror(ctx, appReq)
	return convertPtr[RebuildMirrorResult](result, err)
}

func toAppOptions(opts Options) (appcore.Options, error) {
	extraction, err := convertValue[appcore.ExtractionOptions](opts.Extraction)
	if err != nil {
		return appcore.Options{}, err
	}
	semanticOps, err := convertValue[appcore.SemanticOpsOptions](opts.SemanticOps)
	if err != nil {
		return appcore.Options{}, err
	}
	natural, err := convertValue[appcore.NaturalMemoryOptions](opts.NaturalMemory)
	if err != nil {
		return appcore.Options{}, err
	}
	sidecar, err := convertValue[appcore.SidecarResilienceOptions](opts.SidecarResilience)
	if err != nil {
		return appcore.Options{}, err
	}
	query, err := convertValue[appcore.QueryAnalysisOptions](opts.QueryAnalysis)
	if err != nil {
		return appcore.Options{}, err
	}
	if opts.QueryAnalysis.Cache != nil {
		query.Cache = opts.QueryAnalysis.Cache.appCache()
	}
	return appcore.Options{
		DBPath:            opts.DBPath,
		PersonaID:         opts.PersonaID,
		AutoMigrate:       opts.AutoMigrate,
		EnableFTS:         opts.EnableFTS,
		Timezone:          opts.Timezone,
		Now:               opts.Now,
		MirrorAdapter:     toAppMirrorBackend(opts.MirrorBackend),
		QueryAnalysis:     query,
		SidecarResilience: sidecar,
		Extraction:        extraction,
		SemanticOps:       semanticOps,
		NaturalMemory:     natural,
	}, nil
}

func convertValue[T any](value any) (T, error) {
	var out T
	raw, err := json.Marshal(value)
	if err != nil {
		return out, fmt.Errorf("%w: convert public request: %v", ErrInvalidRequest, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%w: convert internal request: %v", ErrInvalidRequest, err)
	}
	return out, nil
}

func convertPtr[T any](value any, callErr error) (*T, error) {
	if value == nil {
		return nil, translateAppError(callErr)
	}
	out, err := convertValue[T](value)
	if err != nil {
		return nil, err
	}
	return &out, translateAppError(callErr)
}

func translateAppError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, appcore.ErrInvalidOptions):
		return wrapPublicError(err, appcore.ErrInvalidOptions, ErrInvalidOptions)
	case errors.Is(err, appcore.ErrInvalidRequest):
		return wrapPublicError(err, appcore.ErrInvalidRequest, ErrInvalidRequest)
	case errors.Is(err, appcore.ErrNotFound):
		return wrapPublicError(err, appcore.ErrNotFound, ErrNotFound)
	default:
		return err
	}
}

func wrapPublicError(err error, internal error, public error) error {
	if err == internal {
		return public
	}
	message := err.Error()
	prefix := internal.Error() + ": "
	message = strings.TrimPrefix(message, prefix)
	return fmt.Errorf("%w: %s", public, message)
}
