package eval

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

const defaultPersonaID = "default"

var fixedNow = time.Date(2026, 4, 28, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))

type RunnerOptions struct {
	TempDir string
}

type Runner struct {
	opts RunnerOptions
}

type runState struct {
	fixture       *Fixture
	service       memorycore.Service
	db            *sql.DB
	refs          map[string]string
	steps         map[string]stepResult
	persona       string
	stepID        string
	caseID        string
	tempRoot      string
	nextTriviumID int64
}

type stepResult struct {
	Consolidation *memorycore.ConsolidationResult
	Retrieval     *memorycore.MemoryContext
	Forget        *memorycore.ForgetResult
	RetentionRun  *memorycore.RunRetentionResult
	RebuildSearch *memorycore.RebuildSearchDocumentsResult
}

func NewRunner(opts RunnerOptions) *Runner {
	return &Runner{opts: opts}
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
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      dbPath,
		PersonaID:   defaultPersonaID,
		AutoMigrate: true,
		EnableFTS:   true,
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
		nextTriviumID: 1,
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
	return report
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
	if err := s.applyMirrorStub(ctx, step); err != nil {
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
		result, err := s.runRetrieve(ctx, step)
		if err != nil {
			return err
		}
		s.steps[step.ID] = stepResult{Retrieval: result}
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
	case "rebuild_search":
		result, err := s.service.RebuildSearchDocuments(ctx, memorycore.RebuildSearchDocumentsRequest{
			PersonaID: defaultString(step.RebuildSearch.PersonaID, s.persona),
		})
		if err != nil {
			return fmt.Errorf("case %s step %s rebuild search: %w", s.caseID, step.ID, err)
		}
		s.steps[step.ID] = stepResult{RebuildSearch: result}
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
	result, err := s.service.Retrieve(ctx, memorycore.RetrievalRequest{
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
		PersonaID: defaultString(body.PersonaID, s.persona),
		Now:       now,
		DryRun:    body.DryRun,
	})
	if err != nil {
		return nil, fmt.Errorf("case %s step %s retention_run: %w", s.caseID, step.ID, err)
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
	if step.MirrorStub == nil || step.MirrorStub.IndexMappedNodeID == "" {
		return nil
	}
	nodeID, err := s.resolveString(step.MirrorStub.IndexMappedNodeID)
	if err != nil {
		return err
	}
	nodeType := defaultString(step.MirrorStub.IndexMappedType, "fact")
	triviumNodeID := s.nextTriviumID
	s.nextTriviumID++
	_, err = s.db.ExecContext(ctx, `
INSERT OR IGNORE INTO memory_index_map (id, persona_id, node_type, node_id, trivium_node_id, index_status)
VALUES (?, ?, ?, ?, ?, 'indexed')`,
		"map_"+sanitizeFileName(nodeID), s.persona, nodeType, nodeID, triviumNodeID)
	if err != nil {
		return fmt.Errorf("case %s step %s mirror stub: %w", s.caseID, step.ID, err)
	}
	return nil
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

func allowedFactOverrideColumn(column string) bool {
	switch column {
	case "visibility_status", "validity_status", "lifecycle_status", "sensitivity_level", "searchable", "pinned":
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
