package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

const defaultPersonaID = "default"

var fixedNow = time.Date(2026, 4, 28, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))

type RunnerOptions struct {
	TempDir            string
	Profile            Profile
	MirrorAdapter      memorycore.MirrorAdapter
	MirrorArtifact     *MirrorArtifactManager
	Strict             bool
	EmbeddingCacheMode string
	QueryAnalysis      memorycore.QueryAnalysisOptions
	SidecarResilience  memorycore.SidecarResilienceOptions
}

type Runner struct {
	opts RunnerOptions
}

type runState struct {
	fixture        *Fixture
	service        memorycore.Service
	db             *sql.DB
	refs           map[string]string
	steps          map[string]stepResult
	persona        string
	stepID         string
	caseID         string
	tempRoot       string
	dbPath         string
	nextTriviumID  int64
	mirror         *evalMirrorAdapter
	profile        Profile
	mirrorReady    bool
	artifact       *MirrorArtifactManager
	mirrorArtifact MirrorArtifactReport
	mirrorAdapter  memorycore.MirrorAdapter
	semantic       *evalSemanticSidecar
	resilience     memorycore.SidecarResilienceOptions
}

type stepResult struct {
	Consolidation   *memorycore.ConsolidationResult
	Retrieval       *memorycore.MemoryContext
	ScoreBreakdowns []RetrievalScoreBreakdownReport
	RerankRequest   *memorycore.MirrorRerankRequest
	Forget          *memorycore.ForgetResult
	RetentionRun    *memorycore.RunRetentionResult
	Compression     *memorycore.ApplyCompressionResult
	RebuildSearch   *memorycore.RebuildSearchDocumentsResult
	MirrorRebuild   *memorycore.RebuildMirrorResult
	MirrorSync      *memorycore.RunMirrorSyncResult
}

func NewRunner(opts RunnerOptions) *Runner {
	return &Runner{opts: opts}
}

func defaultEvalSidecarResilience(overrides memorycore.SidecarResilienceOptions) memorycore.SidecarResilienceOptions {
	out := memorycore.SidecarResilienceOptions{
		Timeouts: memorycore.SidecarStageTimeouts{
			Total:      45 * time.Second,
			Mirror:     15 * time.Second,
			Activation: 15 * time.Second,
			Rerank:     15 * time.Second,
		},
		Breaker: memorycore.SidecarBreakerOptions{
			Mode: memorycore.SidecarBreakerModeDisabled,
		},
		ActivationBudget: memorycore.SidecarActivationBudgetOptions{
			MaxEdgesScannedPerRequest: 50000,
			MaxNeighborsPerNode:       500,
			MaxActivationWall:         5 * time.Second,
		},
	}
	if overrides.Timeouts.Total > 0 {
		out.Timeouts.Total = overrides.Timeouts.Total
	}
	if overrides.Timeouts.Mirror > 0 {
		out.Timeouts.Mirror = overrides.Timeouts.Mirror
	}
	if overrides.Timeouts.Activation > 0 {
		out.Timeouts.Activation = overrides.Timeouts.Activation
	}
	if overrides.Timeouts.Rerank > 0 {
		out.Timeouts.Rerank = overrides.Timeouts.Rerank
	}
	if overrides.Breaker.Mode != "" {
		out.Breaker.Mode = overrides.Breaker.Mode
	}
	if overrides.Breaker.Window > 0 {
		out.Breaker.Window = overrides.Breaker.Window
	}
	if overrides.Breaker.FailureThreshold > 0 {
		out.Breaker.FailureThreshold = overrides.Breaker.FailureThreshold
	}
	if overrides.Breaker.OpenFor > 0 {
		out.Breaker.OpenFor = overrides.Breaker.OpenFor
	}
	if overrides.ActivationBudget.MaxEdgesScannedPerRequest > 0 {
		out.ActivationBudget.MaxEdgesScannedPerRequest = overrides.ActivationBudget.MaxEdgesScannedPerRequest
	}
	if overrides.ActivationBudget.MaxNeighborsPerNode > 0 {
		out.ActivationBudget.MaxNeighborsPerNode = overrides.ActivationBudget.MaxNeighborsPerNode
	}
	if overrides.ActivationBudget.MaxActivationWall > 0 {
		out.ActivationBudget.MaxActivationWall = overrides.ActivationBudget.MaxActivationWall
	}
	return out
}

func (r *Runner) Run(ctx context.Context, fixture *Fixture) Report {
	report := Report{}
	if fixture != nil {
		report.CaseID = fixture.CaseID
	}
	if fixture == nil {
		report.Err = fmt.Errorf("fixture is nil")
		return report
	}
	if err := fixture.Validate(); err != nil {
		report.Err = err
		return report
	}

	tempRoot := r.opts.TempDir
	if strings.TrimSpace(tempRoot) == "" {
		dir, err := os.MkdirTemp("", "memory-eval-*")
		if err != nil {
			report.Err = fmt.Errorf("create temp dir: %w", err)
			return report
		}
		defer os.RemoveAll(dir)
		tempRoot = dir
	}

	dbPath := filepath.Join(tempRoot, sanitizeFileName(fixture.CaseID)+".db")
	var semanticSidecar *evalSemanticSidecar
	if fixture.UsesSemanticEvalStub() {
		semanticSidecar = newEvalSemanticSidecar()
		defer semanticSidecar.Close()
	}
	var mirror *evalMirrorAdapter
	adapter := r.opts.MirrorAdapter
	if adapter == nil && strings.TrimSpace(string(r.opts.Profile)) == "" {
		mirror = &evalMirrorAdapter{nextID: 1000}
		adapter = mirror
	}
	if adapter == nil && r.opts.Profile.UsesMirror() {
		report.Err = fmt.Errorf("profile %s requires a mirror adapter", r.opts.Profile)
		return report
	}
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:            dbPath,
		PersonaID:         defaultPersonaID,
		AutoMigrate:       true,
		EnableFTS:         true,
		MirrorAdapter:     adapter,
		QueryAnalysis:     r.opts.QueryAnalysis,
		SidecarResilience: defaultEvalSidecarResilience(r.opts.SidecarResilience),
		Now: func() time.Time {
			return fixedNow
		},
	})
	if err != nil {
		report.Err = fmt.Errorf("open service: %w", err)
		return report
	}
	defer svc.Close()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		report.Err = fmt.Errorf("open assertion db: %w", err)
		return report
	}
	defer db.Close()

	state := &runState{
		fixture:       fixture,
		service:       svc,
		db:            db,
		refs:          map[string]string{},
		steps:         map[string]stepResult{},
		persona:       defaultPersonaID,
		caseID:        fixture.CaseID,
		tempRoot:      tempRoot,
		dbPath:        dbPath,
		nextTriviumID: 1,
		mirror:        mirror,
		profile:       r.opts.Profile,
		artifact:      r.opts.MirrorArtifact,
		mirrorAdapter: adapter,
		semantic:      semanticSidecar,
		resilience:    r.opts.SidecarResilience,
	}
	if err := state.seed(ctx); err != nil {
		report.Err = err
		return report
	}
	for _, step := range fixture.Steps {
		state.stepID = step.ID
		if err := state.runStep(ctx, step); err != nil {
			report.Err = err
			return report
		}
		report.Steps = append(report.Steps, state.stepReport(step))
	}
	for _, assertion := range fixture.Assertions {
		name := assertion.Name
		if name == "" {
			name = assertion.Type
		}
		result := AssertionResult{Name: name, Type: assertion.Type}
		result.Err = state.assert(ctx, assertion)
		report.Results = append(report.Results, result)
	}
	report.MirrorArtifact = state.mirrorArtifact
	return report
}

func (s *runState) stepReport(step Step) StepReport {
	result := s.steps[step.ID]
	out := StepReport{
		ID:     step.ID,
		Action: step.Action,
	}
	if step.Retrieve != nil {
		out.QueryText = step.Retrieve.QueryText
		out.FusionMode = step.Retrieve.FusionMode
		out.Retrieval = result.Retrieval
		out.ScoreBreakdowns = append([]RetrievalScoreBreakdownReport(nil), result.ScoreBreakdowns...)
	}
	return out
}

func (r *Runner) RunFile(ctx context.Context, path string) Report {
	fixture, err := LoadFixtureFile(path)
	if err != nil {
		return Report{Err: err}
	}
	return r.Run(ctx, fixture)
}

func (s *runState) seed(ctx context.Context) error {
	for _, persona := range s.fixture.Seed.Personas {
		id := defaultString(persona.ID, defaultPersonaID)
		displayName := defaultString(persona.DisplayName, id)
		if _, err := s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO personas (id, display_name)
VALUES (?, ?)`, id, displayName); err != nil {
			return fmt.Errorf("case %s seed persona %s: %w", s.caseID, id, err)
		}
	}
	for _, session := range s.fixture.Seed.Sessions {
		startedAt, err := parseOptionalTime(session.StartedAt)
		if err != nil {
			return fmt.Errorf("case %s seed session %s: %w", s.caseID, session.ID, err)
		}
		created, err := s.service.StartSession(ctx, memorycore.StartSessionRequest{
			ID:        session.ID,
			PersonaID: defaultString(session.PersonaID, s.persona),
			Channel:   session.Channel,
			StartedAt: startedAt,
		})
		if err != nil {
			return fmt.Errorf("case %s seed session %s: %w", s.caseID, session.ID, err)
		}
		s.refs["session."+session.ID+".id"] = created.ID
	}
	for _, entity := range s.fixture.Seed.Entities {
		aliases := make([]memorycore.EntityAliasInput, 0, len(entity.Aliases))
		for _, alias := range entity.Aliases {
			aliases = append(aliases, memorycore.EntityAliasInput{
				ID:         alias.ID,
				Alias:      alias.Alias,
				AliasType:  alias.AliasType,
				Confidence: alias.Confidence,
			})
		}
		created, err := s.service.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
			ID:               entity.ID,
			PersonaID:        defaultString(entity.PersonaID, s.persona),
			CanonicalName:    entity.CanonicalName,
			EntityType:       entity.EntityType,
			VisibilityStatus: entity.VisibilityStatus,
			SensitivityLevel: entity.SensitivityLevel,
			Searchable:       entity.Searchable,
			Aliases:          aliases,
		})
		if err != nil {
			return fmt.Errorf("case %s seed entity %s: %w", s.caseID, entity.ID, err)
		}
		s.refs["entity."+entity.ID+".id"] = created.ID
	}
	for _, episode := range s.fixture.Seed.Episodes {
		occurredAt, err := parseOptionalTime(episode.OccurredAt)
		if err != nil {
			return fmt.Errorf("case %s seed episode %s: %w", s.caseID, episode.ID, err)
		}
		created, err := s.service.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
			ID:               episode.ID,
			PersonaID:        defaultString(episode.PersonaID, s.persona),
			SessionID:        s.resolveOrLiteral(episode.SessionID),
			Role:             episode.Role,
			Content:          episode.Content,
			OccurredAt:       occurredAt,
			SourceType:       episode.SourceType,
			VisibilityStatus: episode.VisibilityStatus,
			SensitivityLevel: episode.SensitivityLevel,
			Searchable:       episode.Searchable,
		})
		if err != nil {
			return fmt.Errorf("case %s seed episode %s: %w", s.caseID, episode.ID, err)
		}
		s.refs["episode."+episode.ID+".id"] = created.ID
	}
	return nil
}

func (s *runState) runStep(ctx context.Context, step Step) error {
	if s.mirror != nil {
		s.mirror.resetForStep()
		if step.Retrieve != nil {
			s.mirror.fusionMode = step.Retrieve.FusionMode
		}
	}
	if err := s.applyMirrorStub(ctx, step); err != nil {
		return err
	}
	if err := s.applyGraphStub(ctx, step); err != nil {
		return err
	}
	if err := s.applyRerankStub(ctx, step); err != nil {
		return err
	}
	switch step.Action {
	case "consolidate":
		result, err := s.runConsolidate(ctx, step)
		if err != nil {
			return err
		}
		s.steps[step.ID] = stepResult{Consolidation: result}
		if result.Fact != nil {
			s.refs[step.ID+".fact_id"] = result.Fact.ID
		}
		s.refs[step.ID+".action"] = result.Action
		s.refs[step.ID+".status"] = result.Status
		if step.FactOverride != nil {
			override := *step.FactOverride
			if override.FactID == "" && result.Fact != nil {
				override.FactID = result.Fact.ID
			}
			if err := s.applyFactOverride(ctx, override); err != nil {
				return err
			}
		}
	case "retrieve":
		if err := s.prepareProfileMirror(ctx); err != nil {
			return err
		}
		accessRowID, err := s.maxMemoryAccessEventRowID(ctx)
		if err != nil {
			return err
		}
		result, err := s.runRetrieve(ctx, step)
		if err != nil {
			return err
		}
		if err := s.validateProfileRetrieval(result); err != nil {
			return err
		}
		var rerankRequest *memorycore.MirrorRerankRequest
		if s.mirror != nil && s.mirror.lastRerankRequest.PersonaID != "" {
			captured := s.mirror.lastRerankRequest
			rerankRequest = &captured
		}
		scoreBreakdowns, err := s.memoryAccessScoreBreakdownsSince(ctx, accessRowID)
		if err != nil {
			return err
		}
		s.steps[step.ID] = stepResult{Retrieval: result, ScoreBreakdowns: scoreBreakdowns, RerankRequest: rerankRequest}
	case "forget":
		result, err := s.runForget(ctx, step)
		if err != nil {
			return err
		}
		s.steps[step.ID] = stepResult{Forget: result}
		s.refs[step.ID+".deletion_event_id"] = result.DeletionEventID
		s.refs[step.ID+".target_node_id"] = result.TargetNodeID
	case "retention_run":
		result, err := s.runRetention(ctx, step)
		if err != nil {
			return err
		}
		s.steps[step.ID] = stepResult{RetentionRun: result}
	case "compression_apply":
		result, err := s.runCompressionApply(ctx, step)
		if err != nil {
			return err
		}
		s.steps[step.ID] = stepResult{Compression: result}
		if result.NarrativeID != "" {
			s.refs[step.ID+".narrative_id"] = result.NarrativeID
		}
		for index, insightID := range result.InsightIDs {
			if index == 0 {
				s.refs[step.ID+".insight_id"] = insightID
			}
			s.refs[fmt.Sprintf("%s.insight_id_%d", step.ID, index)] = insightID
		}
	case "rebuild_search":
		result, err := s.service.RebuildSearchDocuments(ctx, memorycore.RebuildSearchDocumentsRequest{
			PersonaID: defaultString(step.RebuildSearch.PersonaID, s.persona),
		})
		if err != nil {
			return fmt.Errorf("case %s step %s rebuild search: %w", s.caseID, step.ID, err)
		}
		s.steps[step.ID] = stepResult{RebuildSearch: result}
	case "mirror_rebuild":
		result, err := s.service.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{
			PersonaID: defaultString(step.MirrorRebuild.PersonaID, s.persona),
		})
		if err != nil {
			return fmt.Errorf("case %s step %s mirror rebuild: %w", s.caseID, step.ID, err)
		}
		s.steps[step.ID] = stepResult{MirrorRebuild: result}
	case "mirror_sync":
		result, err := s.service.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{
			PersonaID: defaultString(step.MirrorSync.PersonaID, s.persona),
			Limit:     step.MirrorSync.Limit,
		})
		if err != nil {
			return fmt.Errorf("case %s step %s mirror sync: %w", s.caseID, step.ID, err)
		}
		s.steps[step.ID] = stepResult{MirrorSync: result}
	case "link":
		if err := s.runLink(ctx, step); err != nil {
			return err
		}
		s.steps[step.ID] = stepResult{}
	case "fact":
		if err := s.runFact(ctx, step); err != nil {
			return err
		}
		s.steps[step.ID] = stepResult{}
	default:
		return fmt.Errorf("case %s step %s unknown action %q", s.caseID, step.ID, step.Action)
	}
	return nil
}

func (s *runState) runConsolidate(ctx context.Context, step Step) (*memorycore.ConsolidationResult, error) {
	body := step.Consolidate
	candidate := body.Candidate
	var objectEntityID *string
	if strings.TrimSpace(candidate.ObjectEntityID) != "" {
		value, err := s.resolveString(candidate.ObjectEntityID)
		if err != nil {
			return nil, err
		}
		objectEntityID = &value
	}
	sourceIDs := make([]string, 0, len(candidate.SourceEpisodeIDs))
	for _, source := range candidate.SourceEpisodeIDs {
		value, err := s.resolveString(source)
		if err != nil {
			return nil, err
		}
		sourceIDs = append(sourceIDs, value)
	}
	var sessionID *string
	if strings.TrimSpace(body.SessionID) != "" {
		value, err := s.resolveString(body.SessionID)
		if err != nil {
			return nil, err
		}
		sessionID = &value
	}
	subjectID, err := s.resolveString(candidate.SubjectEntityID)
	if err != nil {
		return nil, err
	}
	validFrom, err := parseOptionalTimePointer(candidate.ValidFrom)
	if err != nil {
		return nil, fmt.Errorf("case %s step %s consolidate valid_from: %w", s.caseID, step.ID, err)
	}
	validTo, err := parseOptionalTimePointer(candidate.ValidTo)
	if err != nil {
		return nil, fmt.Errorf("case %s step %s consolidate valid_to: %w", s.caseID, step.ID, err)
	}
	result, err := s.service.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
		PersonaID: defaultString(body.PersonaID, s.persona),
		SessionID: sessionID,
		Trigger:   body.Trigger,
		Candidate: memorycore.ManualFactCandidate{
			SubjectEntityID:  subjectID,
			Predicate:        candidate.Predicate,
			ObjectEntityID:   objectEntityID,
			ObjectLiteral:    candidate.ObjectLiteral,
			ContentSummary:   candidate.ContentSummary,
			FactType:         candidate.FactType,
			ValidFrom:        validFrom,
			ValidTo:          validTo,
			Confidence:       candidate.Confidence,
			ConfidenceScore:  candidate.ConfidenceScore,
			Importance:       candidate.Importance,
			Valence:          candidate.Valence,
			Arousal:          candidate.Arousal,
			Sensitivity:      candidate.Sensitivity,
			SourceEpisodeIDs: sourceIDs,
			Pinned:           candidate.Pinned,
			UserRequested:    candidate.UserRequested,
		},
		Policy: memorycore.ConsolidationPolicy{
			Action:                      body.Policy.Action,
			Approved:                    body.Policy.Approved,
			AllowManualPinWithoutSource: body.Policy.AllowManualPinWithoutSource,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("case %s step %s consolidate: %w", s.caseID, step.ID, err)
	}
	return result, nil
}

func (s *runState) runFact(ctx context.Context, step Step) error {
	body := step.Fact
	factID := strings.TrimSpace(body.ID)
	if factID == "" {
		factID = step.ID
	}
	subjectEntityID, err := s.resolveOptionalString(body.SubjectEntityID)
	if err != nil {
		return err
	}
	objectEntityID, err := s.resolveOptionalString(body.ObjectEntityID)
	if err != nil {
		return err
	}
	validFrom, err := parseOptionalTime(body.ValidFrom)
	if err != nil {
		return fmt.Errorf("case %s step %s fact valid_from: %w", s.caseID, step.ID, err)
	}
	validTo, err := parseOptionalTime(body.ValidTo)
	if err != nil {
		return fmt.Errorf("case %s step %s fact valid_to: %w", s.caseID, step.ID, err)
	}
	confidenceScore := body.ConfidenceScore
	if confidenceScore == 0 {
		confidenceScore = 0.5
	}
	importance := body.Importance
	if importance == 0 {
		importance = 0.5
	}
	searchable := true
	if body.Searchable != nil {
		searchable = *body.Searchable
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO facts (
    id, persona_id, subject_entity_id, predicate, object_entity_id, object_literal,
    content_summary, fact_type, valid_from, valid_to,
    extraction_confidence, extraction_confidence_score, extraction_reasoning,
    importance, valence, arousal, sensitivity_level,
    validity_status, visibility_status, lifecycle_status,
    pinned, pin_reason, pin_actor, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		factID,
		defaultString(body.PersonaID, s.persona),
		nullableStringValue(subjectEntityID),
		body.Predicate,
		nullableStringValue(objectEntityID),
		nullableStringPointer(body.ObjectLiteral),
		body.ContentSummary,
		defaultString(body.FactType, "stable_preference"),
		nullableTimeValue(validFrom),
		nullableTimeValue(validTo),
		defaultString(body.Confidence, "explicit"),
		confidenceScore,
		importance,
		body.Valence,
		body.Arousal,
		defaultString(body.SensitivityLevel, "normal"),
		defaultString(body.ValidityStatus, "valid"),
		defaultString(body.VisibilityStatus, "visible"),
		defaultString(body.LifecycleStatus, "active"),
		boolToInt(body.Pinned),
		nullableStringValue(body.PinReason),
		nullableStringValue(body.PinActor),
		boolToInt(searchable),
	)
	if err != nil {
		return fmt.Errorf("case %s step %s fact: %w", s.caseID, step.ID, err)
	}
	s.refs[step.ID+".fact_id"] = factID
	for index, rawSourceID := range body.SourceEpisodeIDs {
		sourceID, err := s.resolveString(rawSourceID)
		if err != nil {
			return err
		}
		linkID := fmt.Sprintf("%s_source_%d", step.ID, index+1)
		if _, err := s.db.ExecContext(ctx, `
INSERT INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    created_by, visibility_status, searchable
) VALUES (?, ?, 'fact', ?, 'EVIDENCED_BY', 'episode', ?, 'forward', 1.0, 1.0, 'system', 'visible', 1)`,
			linkID,
			defaultString(body.PersonaID, s.persona),
			factID,
			sourceID,
		); err != nil {
			return fmt.Errorf("case %s step %s fact evidence link: %w", s.caseID, step.ID, err)
		}
	}
	return nil
}

func (s *runState) runLink(ctx context.Context, step Step) error {
	body := step.Link
	linkID := strings.TrimSpace(body.ID)
	if linkID == "" {
		linkID = step.ID
	}
	fromNodeID, err := s.resolveString(body.FromNodeID)
	if err != nil {
		return err
	}
	toNodeID, err := s.resolveString(body.ToNodeID)
	if err != nil {
		return err
	}
	weight := body.Weight
	if weight == 0 {
		weight = 1
	}
	searchable := true
	if body.Searchable != nil {
		searchable = *body.Searchable
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    created_by, visibility_status, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, 'forward', 1.0, ?, 'system', ?, ?)`,
		linkID,
		defaultString(body.PersonaID, s.persona),
		defaultString(body.FromNodeType, "fact"),
		fromNodeID,
		body.LinkType,
		defaultString(body.ToNodeType, "fact"),
		toNodeID,
		weight,
		defaultString(body.VisibilityStatus, "visible"),
		boolToInt(searchable),
	)
	if err != nil {
		return fmt.Errorf("case %s step %s link: %w", s.caseID, step.ID, err)
	}
	s.refs[step.ID+".link_id"] = linkID
	return nil
}

func (s *runState) runRetrieve(ctx context.Context, step Step) (*memorycore.MemoryContext, error) {
	body := step.Retrieve
	var sessionID *string
	if strings.TrimSpace(body.SessionID) != "" {
		value, err := s.resolveString(body.SessionID)
		if err != nil {
			return nil, err
		}
		sessionID = &value
	}
	now, err := parseOptionalTime(body.Now)
	if err != nil {
		return nil, fmt.Errorf("case %s step %s retrieve now: %w", s.caseID, step.ID, err)
	}
	policy := memorycore.RetrievalPolicy{
		SensitivityPermission: body.Policy.SensitivityPermission,
		AllowHistorical:       body.Policy.AllowHistorical,
		AllowDeepArchive:      body.Policy.AllowDeepArchive,
		FinalMemoryCount:      body.Policy.FinalMemoryCount,
		ContextBudgetTokens:   body.Policy.ContextBudgetTokens,
	}
	if body.Policy.UseFTS != nil {
		policy.UseFTS = *body.Policy.UseFTS
	}
	if body.Policy.UseMirror != nil {
		policy.UseMirror = *body.Policy.UseMirror
	}
	if strings.TrimSpace(string(s.profile)) != "" {
		policy.UseMirror = s.profile.UsesMirror()
	}
	service := s.service
	var closeService func()
	if step.SemanticStub != nil {
		semanticService, err := s.openSemanticRetrieveService(ctx, step.SemanticStub)
		if err != nil {
			return nil, err
		}
		service = semanticService
		closeService = func() {
			_ = semanticService.Close()
		}
	}
	if closeService != nil {
		defer closeService()
	}
	result, err := service.Retrieve(ctx, memorycore.RetrievalRequest{
		PersonaID: defaultString(body.PersonaID, s.persona),
		SessionID: sessionID,
		QueryText: body.QueryText,
		Now:       now,
		Policy:    policy,
		Context: memorycore.RetrievalAffectContext{
			UserMoodLabel:         body.Context.UserMoodLabel,
			RelationshipMoodLabel: body.Context.RelationshipMoodLabel,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("case %s step %s retrieve: %w", s.caseID, step.ID, err)
	}
	return result, nil
}

func (s *runState) maxMemoryAccessEventRowID(ctx context.Context) (int64, error) {
	var rowID sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(rowid), 0) FROM memory_access_events`).Scan(&rowID); err != nil {
		return 0, fmt.Errorf("case %s step %s access event watermark: %w", s.caseID, s.stepID, err)
	}
	if !rowID.Valid {
		return 0, nil
	}
	return rowID.Int64, nil
}

func (s *runState) memoryAccessScoreBreakdownsSince(ctx context.Context, afterRowID int64) ([]RetrievalScoreBreakdownReport, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT node_id,
       access_type,
       COALESCE(context_block_type, ''),
       COALESCE(score_breakdown_json, '')
FROM memory_access_events
WHERE rowid > ?
  AND persona_id = ?
ORDER BY rowid`, afterRowID, s.persona)
	if err != nil {
		return nil, fmt.Errorf("case %s step %s access event score breakdowns: %w", s.caseID, s.stepID, err)
	}
	defer rows.Close()
	var out []RetrievalScoreBreakdownReport
	for rows.Next() {
		var item RetrievalScoreBreakdownReport
		var rawBreakdown string
		if err := rows.Scan(&item.NodeID, &item.AccessType, &item.ContextBlockType, &rawBreakdown); err != nil {
			return nil, fmt.Errorf("case %s step %s access event score breakdown scan: %w", s.caseID, s.stepID, err)
		}
		if strings.TrimSpace(rawBreakdown) != "" {
			var parsed struct {
				CompletionSource string  `json:"completion_source"`
				ReflectionBoost  float64 `json:"reflection_boost"`
			}
			if err := json.Unmarshal([]byte(rawBreakdown), &parsed); err == nil {
				item.CompletionSource = parsed.CompletionSource
				item.ReflectionBoost = parsed.ReflectionBoost
			}
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("case %s step %s access event score breakdown rows: %w", s.caseID, s.stepID, err)
	}
	return out, nil
}

func (s *runState) openSemanticRetrieveService(ctx context.Context, stub *SemanticStubSettings) (memorycore.Service, error) {
	if s.semantic == nil {
		return nil, fmt.Errorf("case %s step %s semantic_query_analysis_stub requires eval semantic sidecar", s.caseID, s.stepID)
	}
	s.semantic.setStub(stub)
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:        s.dbPath,
		PersonaID:     defaultPersonaID,
		AutoMigrate:   true,
		EnableFTS:     true,
		MirrorAdapter: s.mirrorAdapter,
		QueryAnalysis: memorycore.QueryAnalysisOptions{
			Provider:   memorycore.QueryAnalysisProviderSidecar,
			Mode:       memorycore.QueryAnalysisModeSemanticAlways,
			SidecarURL: s.semantic.URL(),
		},
		SidecarResilience: defaultEvalSidecarResilience(s.resilience),
		Now: func() time.Time {
			return fixedNow
		},
	})
	if err != nil {
		return nil, fmt.Errorf("case %s step %s open semantic retrieve service: %w", s.caseID, s.stepID, err)
	}
	return svc, nil
}

func (s *runState) prepareProfileMirror(ctx context.Context) error {
	if !s.profile.UsesMirror() || s.mirrorReady {
		return nil
	}
	if s.artifact != nil {
		artifactReport, err := s.artifact.Ensure(ctx, s)
		if err != nil {
			return fmt.Errorf("case %s profile %s prepare mirror artifact: %w", s.caseID, s.profile, err)
		}
		s.mirrorArtifact = artifactReport
		s.mirrorReady = true
		return nil
	}
	result, err := s.service.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{PersonaID: s.persona})
	if err != nil {
		return fmt.Errorf("case %s profile %s rebuild mirror: %w", s.caseID, s.profile, err)
	}
	if result.Failed > 0 {
		return fmt.Errorf("case %s profile %s rebuild mirror failed nodes=%d", s.caseID, s.profile, result.Failed)
	}
	s.mirrorReady = true
	return nil
}

func (s *runState) validateProfileRetrieval(result *memorycore.MemoryContext) error {
	switch s.profile {
	case "", ProfileSQLiteGo:
		return nil
	case ProfileMirrorRealDense,
		ProfileRuleOnlyRaw,
		ProfileSemanticParseOnly,
		ProfileSemanticRewriteOnly,
		ProfileSemanticFullCurrent,
		ProfileSemanticFullSoftGated,
		ProfileRerankOff,
		ProfileRerankSelective,
		ProfileSoftRoutingEnabled:
		return requireMirrorUsed(s.caseID, s.profile, result)
	case ProfileMirrorRealGraph:
		if err := requireMirrorUsed(s.caseID, s.profile, result); err != nil {
			return err
		}
		return requireGraphUsed(s.caseID, s.profile, result)
	case ProfileMirrorRealGraphRerank:
		if err := requireMirrorUsed(s.caseID, s.profile, result); err != nil {
			return err
		}
		if err := requireGraphUsed(s.caseID, s.profile, result); err != nil {
			return err
		}
		return requireRerankUsed(s.caseID, s.profile, result)
	case ProfileMirrorRealRerankNoGraph:
		if err := requireMirrorUsed(s.caseID, s.profile, result); err != nil {
			return err
		}
		return requireRerankUsed(s.caseID, s.profile, result)
	default:
		return nil
	}
}

func requireMirrorUsed(caseID string, profile Profile, result *memorycore.MemoryContext) error {
	if result == nil || result.Mirror == nil {
		return fmt.Errorf("case %s profile %s requires mirror diagnostics", caseID, profile)
	}
	if result.Mirror.Status != "used" {
		return fmt.Errorf("case %s profile %s requires mirror status used, got %s", caseID, profile, result.Mirror.Status)
	}
	if result.Mirror.Degraded {
		return fmt.Errorf("case %s profile %s mirror degraded: %s", caseID, profile, result.Mirror.FallbackReason)
	}
	return nil
}

func requireGraphUsed(caseID string, profile Profile, result *memorycore.MemoryContext) error {
	if result == nil || result.GraphActivation == nil {
		return fmt.Errorf("case %s profile %s requires graph activation diagnostics", caseID, profile)
	}
	if result.GraphActivation.Status != "used" {
		return fmt.Errorf("case %s profile %s requires graph activation status used, got %s", caseID, profile, result.GraphActivation.Status)
	}
	if result.GraphActivation.Degraded {
		return fmt.Errorf("case %s profile %s graph activation degraded: %s", caseID, profile, result.GraphActivation.FallbackReason)
	}
	return nil
}

func requireRerankUsed(caseID string, profile Profile, result *memorycore.MemoryContext) error {
	if result == nil || result.Rerank == nil {
		return fmt.Errorf("case %s profile %s requires rerank diagnostics", caseID, profile)
	}
	if result.Rerank.Status != "used" {
		return fmt.Errorf("case %s profile %s requires rerank status used, got %s", caseID, profile, result.Rerank.Status)
	}
	if result.Rerank.Degraded {
		return fmt.Errorf("case %s profile %s rerank degraded: %s", caseID, profile, result.Rerank.FallbackReason)
	}
	return nil
}

func (s *runState) runForget(ctx context.Context, step Step) (*memorycore.ForgetResult, error) {
	body := step.Forget
	nodeID, err := s.resolveString(body.Target.NodeID)
	if err != nil {
		return nil, err
	}
	result, err := s.service.Forget(ctx, memorycore.ForgetRequest{
		PersonaID:  defaultString(body.PersonaID, s.persona),
		Actor:      body.Actor,
		ReasonCode: body.ReasonCode,
		Level:      body.Level,
		Target: memorycore.ForgetTarget{
			ScopeMode: body.Target.ScopeMode,
			NodeType:  body.Target.NodeType,
			NodeID:    nodeID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("case %s step %s forget: %w", s.caseID, step.ID, err)
	}
	return result, nil
}

func (s *runState) runRetention(ctx context.Context, step Step) (*memorycore.RunRetentionResult, error) {
	body := step.RetentionRun
	now, err := parseOptionalTime(body.Now)
	if err != nil {
		return nil, fmt.Errorf("case %s step %s retention_run now: %w", s.caseID, step.ID, err)
	}
	result, err := s.service.RunRetention(ctx, memorycore.RunRetentionRequest{
		PersonaID:            defaultString(body.PersonaID, s.persona),
		Now:                  now,
		DryRun:               body.DryRun,
		DeepArchiveAfterDays: body.DeepArchiveAfterDays,
	})
	if err != nil {
		return nil, fmt.Errorf("case %s step %s retention_run: %w", s.caseID, step.ID, err)
	}
	return result, nil
}

func (s *runState) runCompressionApply(ctx context.Context, step Step) (*memorycore.ApplyCompressionResult, error) {
	body := step.Compression
	now, err := parseOptionalTime(body.Now)
	if err != nil {
		return nil, fmt.Errorf("case %s step %s compression_apply now: %w", s.caseID, step.ID, err)
	}
	sourceIDs := make([]string, 0, len(body.SourceFactIDs))
	for _, sourceID := range body.SourceFactIDs {
		resolved, err := s.resolveString(sourceID)
		if err != nil {
			return nil, err
		}
		sourceIDs = append(sourceIDs, resolved)
	}
	var narrative *memorycore.NarrativeDraft
	if body.Narrative != nil {
		validFrom, err := parseOptionalTimePointer(body.Narrative.ValidFrom)
		if err != nil {
			return nil, fmt.Errorf("case %s step %s compression_apply narrative valid_from: %w", s.caseID, step.ID, err)
		}
		validTo, err := parseOptionalTimePointer(body.Narrative.ValidTo)
		if err != nil {
			return nil, fmt.Errorf("case %s step %s compression_apply narrative valid_to: %w", s.caseID, step.ID, err)
		}
		narrative = &memorycore.NarrativeDraft{
			ID:               body.Narrative.ID,
			Scope:            body.Narrative.Scope,
			ScopeRef:         body.Narrative.ScopeRef,
			Summary:          body.Narrative.Summary,
			EmotionalTone:    body.Narrative.EmotionalTone,
			ValenceAvg:       body.Narrative.ValenceAvg,
			ArousalAvg:       body.Narrative.ArousalAvg,
			Importance:       body.Narrative.Importance,
			ValidFrom:        validFrom,
			ValidTo:          validTo,
			SensitivityLevel: body.Narrative.SensitivityLevel,
		}
	}
	insights := make([]memorycore.InsightDraft, 0, len(body.Insights))
	for _, insight := range body.Insights {
		insights = append(insights, memorycore.InsightDraft{
			ID:               insight.ID,
			InsightType:      insight.InsightType,
			Content:          insight.Content,
			Confidence:       insight.Confidence,
			Importance:       insight.Importance,
			Valence:          insight.Valence,
			Arousal:          insight.Arousal,
			SensitivityLevel: insight.SensitivityLevel,
		})
	}
	result, err := s.service.ApplyCompression(ctx, memorycore.ApplyCompressionRequest{
		PersonaID:     defaultString(body.PersonaID, s.persona),
		SourceFactIDs: sourceIDs,
		Narrative:     narrative,
		Insights:      insights,
		Now:           now,
		DryRun:        body.DryRun,
	})
	if err != nil {
		return nil, fmt.Errorf("case %s step %s compression_apply: %w", s.caseID, step.ID, err)
	}
	return result, nil
}

func (s *runState) applyFactOverride(ctx context.Context, override FactOverride) error {
	factID, err := s.resolveString(override.FactID)
	if err != nil {
		return err
	}
	columns := map[string]any{}
	if override.VisibilityStatus != "" {
		columns["visibility_status"] = override.VisibilityStatus
	}
	if override.ValidityStatus != "" {
		columns["validity_status"] = override.ValidityStatus
	}
	if override.LifecycleStatus != "" {
		columns["lifecycle_status"] = override.LifecycleStatus
	}
	if override.SensitivityLevel != "" {
		columns["sensitivity_level"] = override.SensitivityLevel
	}
	if override.UpdatedAt != "" {
		updatedAt, err := parseOptionalTime(override.UpdatedAt)
		if err != nil {
			return fmt.Errorf("case %s step %s override updated_at: %w", s.caseID, s.stepID, err)
		}
		columns["updated_at"] = updatedAt.UTC().Format(time.RFC3339Nano)
	}
	if override.Searchable != nil {
		columns["searchable"] = boolToInt(*override.Searchable)
	}
	if override.Pinned != nil {
		columns["pinned"] = boolToInt(*override.Pinned)
	}
	for column, value := range columns {
		if !allowedFactOverrideColumn(column) {
			return fmt.Errorf("case %s step %s unsupported fact override column %s", s.caseID, s.stepID, column)
		}
		if _, err := s.db.ExecContext(ctx, "UPDATE facts SET "+column+" = ? WHERE id = ?", value, factID); err != nil {
			return fmt.Errorf("case %s step %s override fact %s.%s: %w", s.caseID, s.stepID, factID, column, err)
		}
	}
	return nil
}

func (s *runState) applyMirrorStub(ctx context.Context, step Step) error {
	if step.MirrorStub == nil {
		return nil
	}
	if s.mirror == nil {
		return fmt.Errorf("case %s step %s mirror_stub requires eval stub adapter", s.caseID, s.stepID)
	}
	if step.MirrorStub.Unavailable {
		s.mirror.unavailable = true
	}
	if step.MirrorStub.IndexMappedNodeID != "" {
		if _, err := s.insertMirrorMap(ctx, step.MirrorStub.IndexMappedNodeID, step.MirrorStub.IndexMappedType); err != nil {
			return err
		}
	}
	for _, item := range step.MirrorStub.IndexMappedNodes {
		if _, err := s.insertMirrorMap(ctx, item.NodeID, item.NodeType); err != nil {
			return err
		}
	}
	if step.MirrorStub.CandidateNodeID != "" {
		candidate := MirrorCandidate{
			NodeID:   step.MirrorStub.CandidateNodeID,
			NodeType: step.MirrorStub.CandidateNodeType,
			Score:    step.MirrorStub.CandidateScore,
		}
		if err := s.addMirrorCandidate(ctx, candidate); err != nil {
			return err
		}
	}
	for _, candidate := range step.MirrorStub.Candidates {
		if err := s.addMirrorCandidate(ctx, candidate); err != nil {
			return err
		}
	}
	return nil
}

func (s *runState) insertMirrorMap(ctx context.Context, rawNodeID string, rawNodeType string) (int64, error) {
	nodeID, err := s.resolveString(rawNodeID)
	if err != nil {
		return 0, err
	}
	nodeType := defaultString(rawNodeType, "fact")
	triviumNodeID := s.nextTriviumID
	s.nextTriviumID++
	_, err = s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES (?, ?, ?, ?, ?, 'indexed')`,
		"map_"+sanitizeFileName(nodeID), s.persona, nodeType, nodeID, triviumNodeID)
	if err != nil {
		return 0, fmt.Errorf("case %s step %s mirror stub: %w", s.caseID, s.stepID, err)
	}
	return s.lookupTriviumNodeID(ctx, nodeID, nodeType)
}

func (s *runState) addMirrorCandidate(ctx context.Context, candidate MirrorCandidate) error {
	score := candidate.Score
	if score == 0 {
		score = 0.8
	}
	source := defaultString(candidate.Source, "eval_stub")
	nodeType := defaultString(candidate.NodeType, "fact")
	triviumNodeID := candidate.TriviumNodeID
	if triviumNodeID == 0 {
		nodeID, err := s.resolveString(candidate.NodeID)
		if err != nil {
			return err
		}
		triviumNodeID, err = s.lookupTriviumNodeID(ctx, nodeID, nodeType)
		if err != nil {
			return fmt.Errorf("case %s step %s mirror candidate map: %w", s.caseID, s.stepID, err)
		}
	}
	s.mirror.candidates = append(s.mirror.candidates, memorycore.MirrorCandidate{
		TriviumNodeID: triviumNodeID,
		Score:         score,
		Source:        source,
		Rank:          candidate.Rank,
	})
	return nil
}

func (s *runState) lookupTriviumNodeID(ctx context.Context, nodeID string, nodeType string) (int64, error) {
	var triviumNodeID int64
	err := s.db.QueryRowContext(ctx, `
SELECT trivium_node_id
FROM memory_index_map
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?`, s.persona, nodeType, nodeID).Scan(&triviumNodeID)
	return triviumNodeID, err
}

func (s *runState) applyGraphStub(ctx context.Context, step Step) error {
	if step.GraphStub == nil {
		return nil
	}
	if s.mirror == nil {
		return fmt.Errorf("case %s step %s graph_activation_stub requires eval stub adapter", s.caseID, s.stepID)
	}
	if step.GraphStub.Unavailable {
		s.mirror.activationUnavailable = true
	}
	if step.GraphStub.Degraded {
		s.mirror.activationDegraded = true
		s.mirror.activationFallbackReason = step.GraphStub.FallbackReason
	}
	for _, candidate := range step.GraphStub.Candidates {
		mapped, err := s.graphActivationCandidate(ctx, candidate)
		if err != nil {
			return err
		}
		s.mirror.activationCandidates = append(s.mirror.activationCandidates, mapped)
	}
	return nil
}

func (s *runState) applyRerankStub(ctx context.Context, step Step) error {
	if step.RerankStub == nil {
		return nil
	}
	if s.mirror == nil {
		return fmt.Errorf("case %s step %s rerank_stub requires eval stub adapter", s.caseID, s.stepID)
	}
	if step.RerankStub.Unavailable {
		s.mirror.rerankUnavailable = true
	}
	if step.RerankStub.Degraded {
		s.mirror.rerankDegraded = true
		s.mirror.rerankFallbackReason = step.RerankStub.FallbackReason
	}
	for _, item := range step.RerankStub.Items {
		nodeID, err := s.resolveString(item.NodeID)
		if err != nil {
			return err
		}
		nodeType := defaultString(item.NodeType, "fact")
		score := item.Score
		if score == 0 {
			score = 0.8
		}
		s.mirror.rerankItems = append(s.mirror.rerankItems, memorycore.MirrorRerankItem{
			NodeID:      nodeID,
			NodeType:    nodeType,
			RerankScore: score,
			DebugReason: item.DebugReason,
		})
	}
	return nil
}

func (s *runState) graphActivationCandidate(ctx context.Context, candidate GraphCandidateStub) (memorycore.MirrorActivationCandidate, error) {
	nodeType := defaultString(candidate.NodeType, "fact")
	triviumNodeID := candidate.TriviumNodeID
	if triviumNodeID == 0 {
		nodeID, err := s.resolveString(candidate.NodeID)
		if err != nil {
			return memorycore.MirrorActivationCandidate{}, err
		}
		triviumID, err := s.lookupTriviumNodeID(ctx, nodeID, nodeType)
		if err != nil {
			return memorycore.MirrorActivationCandidate{}, fmt.Errorf("case %s step %s graph candidate map: %w", s.caseID, s.stepID, err)
		}
		triviumNodeID = triviumID
	}
	score := candidate.Score
	if score == 0 {
		score = 0.8
	}
	source := defaultString(candidate.Source, "graph_activation")
	paths, err := s.graphActivationPaths(ctx, candidate)
	if err != nil {
		return memorycore.MirrorActivationCandidate{}, err
	}
	return memorycore.MirrorActivationCandidate{
		TriviumNodeID: triviumNodeID,
		Score:         score,
		Source:        source,
		Rank:          candidate.Rank,
		Paths:         paths,
	}, nil
}

func (s *runState) graphActivationPaths(ctx context.Context, candidate GraphCandidateStub) ([]memorycore.MirrorActivationPath, error) {
	if len(candidate.PathNodeIDs) == 0 && len(candidate.PathTriviumNodeIDs) == 0 && len(candidate.PathLinkTypes) == 0 {
		return nil, nil
	}
	ids := append([]int64(nil), candidate.PathTriviumNodeIDs...)
	for _, rawNodeID := range candidate.PathNodeIDs {
		nodeID, err := s.resolveString(rawNodeID)
		if err != nil {
			return nil, err
		}
		nodeType := defaultString(candidate.NodeType, "fact")
		triviumID, err := s.lookupTriviumNodeID(ctx, nodeID, nodeType)
		if err != nil {
			return nil, fmt.Errorf("case %s step %s graph path map: %w", s.caseID, s.stepID, err)
		}
		ids = append(ids, triviumID)
	}
	return []memorycore.MirrorActivationPath{{
		TriviumNodeIDs: ids,
		LinkTypes:      append([]string(nil), candidate.PathLinkTypes...),
	}}, nil
}

func (s *runState) resolveString(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "$") {
		return s.resolveOrLiteral(value), nil
	}
	key := strings.TrimPrefix(value, "$")
	resolved, ok := s.refs[key]
	if !ok {
		return "", fmt.Errorf("case %s step %s unresolved reference %s", s.caseID, s.stepID, value)
	}
	return resolved, nil
}

func (s *runState) resolveOptionalString(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return s.resolveString(value)
}

func (s *runState) resolveOrLiteral(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	for _, prefix := range []string{"entity.", "episode.", "session."} {
		key := prefix + value + ".id"
		if resolved, ok := s.refs[key]; ok {
			return resolved
		}
	}
	return value
}

func parseOptionalTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time %q", value)
}

func parseOptionalTimePointer(value string) (*time.Time, error) {
	parsed, err := parseOptionalTime(value)
	if err != nil {
		return nil, err
	}
	if parsed.IsZero() {
		return nil, nil
	}
	return &parsed, nil
}

func nullableStringValue(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableStringPointer(value *string) any {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return *value
}

func nullableTimeValue(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func allowedFactOverrideColumn(column string) bool {
	switch column {
	case "visibility_status", "validity_status", "lifecycle_status", "sensitivity_level", "updated_at", "searchable", "pinned":
		return true
	default:
		return false
	}
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func sanitizeFileName(value string) string {
	var builder strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	result := builder.String()
	if result == "" {
		return "case"
	}
	return result
}
