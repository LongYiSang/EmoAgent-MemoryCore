package eval

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Fixture struct {
	CaseID      string      `yaml:"case_id"`
	Description string      `yaml:"description"`
	Seed        Seed        `yaml:"seed"`
	Steps       []Step      `yaml:"steps"`
	Assertions  []Assertion `yaml:"assertions"`
}

type Seed struct {
	Personas []PersonaSeed `yaml:"personas"`
	Sessions []SessionSeed `yaml:"sessions"`
	Entities []EntitySeed  `yaml:"entities"`
	Episodes []EpisodeSeed `yaml:"episodes"`
}

type PersonaSeed struct {
	ID          string `yaml:"id"`
	DisplayName string `yaml:"display_name"`
}

type SessionSeed struct {
	ID        string `yaml:"id"`
	PersonaID string `yaml:"persona_id"`
	Channel   string `yaml:"channel"`
	StartedAt string `yaml:"started_at"`
}

type EntitySeed struct {
	ID               string            `yaml:"id"`
	PersonaID        string            `yaml:"persona_id"`
	CanonicalName    string            `yaml:"canonical_name"`
	EntityType       string            `yaml:"entity_type"`
	VisibilityStatus string            `yaml:"visibility_status"`
	SensitivityLevel string            `yaml:"sensitivity_level"`
	Searchable       *bool             `yaml:"searchable"`
	Aliases          []EntityAliasSeed `yaml:"aliases"`
}

type EntityAliasSeed struct {
	ID         string  `yaml:"id"`
	Alias      string  `yaml:"alias"`
	AliasType  string  `yaml:"alias_type"`
	Confidence float64 `yaml:"confidence"`
}

type EpisodeSeed struct {
	ID               string `yaml:"id"`
	PersonaID        string `yaml:"persona_id"`
	SessionID        string `yaml:"session_id"`
	Role             string `yaml:"role"`
	Content          string `yaml:"content"`
	OccurredAt       string `yaml:"occurred_at"`
	SourceType       string `yaml:"source_type"`
	VisibilityStatus string `yaml:"visibility_status"`
	SensitivityLevel string `yaml:"sensitivity_level"`
	Searchable       *bool  `yaml:"searchable"`
}

type Step struct {
	ID            string              `yaml:"id"`
	Action        string              `yaml:"action"`
	Consolidate   *ConsolidateStep    `yaml:"consolidate"`
	Retrieve      *RetrieveStep       `yaml:"retrieve"`
	Forget        *ForgetStep         `yaml:"forget"`
	RebuildSearch *RebuildSearchStep  `yaml:"rebuild_search"`
	FactOverride  *FactOverride       `yaml:"fact_override"`
	MirrorStub    *MirrorStubSettings `yaml:"mirror_stub"`
}

type ConsolidateStep struct {
	PersonaID string              `yaml:"persona_id"`
	SessionID string              `yaml:"session_id"`
	Trigger   string              `yaml:"trigger"`
	Candidate ManualFactCandidate `yaml:"candidate"`
	Policy    ConsolidationPolicy `yaml:"policy"`
}

type ManualFactCandidate struct {
	SubjectEntityID  string   `yaml:"subject_entity_id"`
	Predicate        string   `yaml:"predicate"`
	ObjectEntityID   string   `yaml:"object_entity_id"`
	ObjectLiteral    *string  `yaml:"object_literal"`
	ContentSummary   string   `yaml:"content_summary"`
	FactType         string   `yaml:"fact_type"`
	Confidence       string   `yaml:"confidence"`
	ConfidenceScore  float64  `yaml:"confidence_score"`
	Importance       float64  `yaml:"importance"`
	Valence          float64  `yaml:"valence"`
	Arousal          float64  `yaml:"arousal"`
	Sensitivity      string   `yaml:"sensitivity"`
	SourceEpisodeIDs []string `yaml:"source_episode_ids"`
	Pinned           bool     `yaml:"pinned"`
	UserRequested    bool     `yaml:"user_requested"`
}

type ConsolidationPolicy struct {
	Action                      string `yaml:"action"`
	Approved                    bool   `yaml:"approved"`
	AllowManualPinWithoutSource bool   `yaml:"allow_manual_pin_without_source"`
}

type RetrieveStep struct {
	PersonaID string                 `yaml:"persona_id"`
	SessionID string                 `yaml:"session_id"`
	QueryText string                 `yaml:"query_text"`
	Now       string                 `yaml:"now"`
	Policy    RetrievalPolicy        `yaml:"policy"`
	Context   RetrievalAffectContext `yaml:"context"`
}

type RetrievalPolicy struct {
	SensitivityPermission string `yaml:"sensitivity_permission"`
	AllowHistorical       bool   `yaml:"allow_historical"`
	AllowDeepArchive      bool   `yaml:"allow_deep_archive"`
	FinalMemoryCount      int    `yaml:"final_memory_count"`
	ContextBudgetTokens   int    `yaml:"context_budget_tokens"`
	UseFTS                *bool  `yaml:"use_fts"`
}

type RetrievalAffectContext struct {
	UserMoodLabel         string `yaml:"user_mood_label"`
	RelationshipMoodLabel string `yaml:"relationship_mood_label"`
}

type ForgetStep struct {
	PersonaID  string       `yaml:"persona_id"`
	Actor      string       `yaml:"actor"`
	ReasonCode string       `yaml:"reason_code"`
	Level      string       `yaml:"level"`
	Target     ForgetTarget `yaml:"target"`
}

type ForgetTarget struct {
	ScopeMode string `yaml:"scope_mode"`
	NodeType  string `yaml:"node_type"`
	NodeID    string `yaml:"node_id"`
}

type RebuildSearchStep struct {
	PersonaID string `yaml:"persona_id"`
}

type FactOverride struct {
	FactID           string `yaml:"fact_id"`
	VisibilityStatus string `yaml:"visibility_status"`
	ValidityStatus   string `yaml:"validity_status"`
	LifecycleStatus  string `yaml:"lifecycle_status"`
	SensitivityLevel string `yaml:"sensitivity_level"`
	Searchable       *bool  `yaml:"searchable"`
	Pinned           *bool  `yaml:"pinned"`
}

type MirrorStubSettings struct {
	IndexMappedNodeID string `yaml:"index_mapped_node_id"`
	IndexMappedType   string `yaml:"index_mapped_type"`
}

type Assertion struct {
	Type                  string   `yaml:"type"`
	Name                  string   `yaml:"name"`
	Step                  string   `yaml:"step"`
	NodeID                string   `yaml:"node_id"`
	NodeType              string   `yaml:"node_type"`
	Summary               string   `yaml:"summary"`
	UsageGuidanceContains string   `yaml:"usage_guidance_contains"`
	Action                string   `yaml:"action"`
	Status                string   `yaml:"status"`
	FactID                string   `yaml:"fact_id"`
	Column                string   `yaml:"column"`
	Equals                string   `yaml:"equals"`
	FromNodeID            string   `yaml:"from_node_id"`
	FromNodeType          string   `yaml:"from_node_type"`
	LinkType              string   `yaml:"link_type"`
	ToNodeID              string   `yaml:"to_node_id"`
	ToNodeType            string   `yaml:"to_node_type"`
	SearchText            string   `yaml:"search_text"`
	DeletionEventID       string   `yaml:"deletion_event_id"`
	ForbiddenContains     []string `yaml:"forbidden_contains"`
	EpisodeID             string   `yaml:"episode_id"`
}

func LoadFixtureBytes(data []byte) (*Fixture, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var fixture Fixture
	if err := decoder.Decode(&fixture); err != nil {
		return nil, fmt.Errorf("decode fixture: %w", err)
	}
	if err := fixture.Validate(); err != nil {
		return nil, err
	}
	return &fixture, nil
}

func LoadFixtureFile(path string) (*Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read fixture %s: %w", path, err)
	}
	return LoadFixtureBytes(data)
}

func DiscoverFixtureFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".yaml") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func (f *Fixture) Validate() error {
	if f == nil {
		return fmt.Errorf("fixture is nil")
	}
	caseID := strings.TrimSpace(f.CaseID)
	if caseID == "" {
		return fmt.Errorf("case_id is required")
	}
	stepIDs := map[string]struct{}{}
	for index, step := range f.Steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("case %s step %d id is required", caseID, index)
		}
		if _, exists := stepIDs[step.ID]; exists {
			return fmt.Errorf("case %s duplicate step id %q", caseID, step.ID)
		}
		stepIDs[step.ID] = struct{}{}
		switch step.Action {
		case "consolidate":
			if step.Consolidate == nil {
				return fmt.Errorf("case %s step %s missing consolidate body", caseID, step.ID)
			}
		case "retrieve":
			if step.Retrieve == nil {
				return fmt.Errorf("case %s step %s missing retrieve body", caseID, step.ID)
			}
		case "forget":
			if step.Forget == nil {
				return fmt.Errorf("case %s step %s missing forget body", caseID, step.ID)
			}
		case "rebuild_search":
			if step.RebuildSearch == nil {
				return fmt.Errorf("case %s step %s missing rebuild_search body", caseID, step.ID)
			}
		default:
			return fmt.Errorf("case %s step %s unknown action %q", caseID, step.ID, step.Action)
		}
	}
	for index, assertion := range f.Assertions {
		if !knownAssertionType(assertion.Type) {
			return fmt.Errorf("case %s assertion %d unknown type %q", caseID, index, assertion.Type)
		}
	}
	return nil
}

func knownAssertionType(value string) bool {
	switch value {
	case "consolidation_result",
		"memory_contains",
		"memory_not_contains",
		"fact_column",
		"link_exists",
		"search_absent",
		"deletion_event_safe",
		"episode_tombstone_exists":
		return true
	default:
		return false
	}
}
