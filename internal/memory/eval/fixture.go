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
	RetentionRun  *RetentionRunStep   `yaml:"retention_run"`
	Compression   *CompressionStep    `yaml:"compression_apply"`
	RebuildSearch *RebuildSearchStep  `yaml:"rebuild_search"`
	MirrorRebuild *MirrorRebuildStep  `yaml:"mirror_rebuild"`
	MirrorSync    *MirrorSyncStep     `yaml:"mirror_sync"`
	Link          *LinkStep           `yaml:"link"`
	Fact          *FactStep           `yaml:"fact"`
	FactOverride  *FactOverride       `yaml:"fact_override"`
	MirrorStub    *MirrorStubSettings `yaml:"mirror_stub"`
	GraphStub     *GraphStubSettings  `yaml:"graph_activation_stub"`
	RerankStub    *RerankStubSettings `yaml:"rerank_stub"`
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
	ValidFrom        string   `yaml:"valid_from"`
	ValidTo          string   `yaml:"valid_to"`
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
	UseMirror             *bool  `yaml:"use_mirror"`
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

type RetentionRunStep struct {
	PersonaID            string `yaml:"persona_id"`
	Now                  string `yaml:"now"`
	DryRun               bool   `yaml:"dry_run"`
	DeepArchiveAfterDays int    `yaml:"deep_archive_after_days"`
}

type CompressionStep struct {
	PersonaID     string                     `yaml:"persona_id"`
	SourceFactIDs []string                   `yaml:"source_fact_ids"`
	Narrative     *CompressionNarrativeDraft `yaml:"narrative"`
	Insights      []CompressionInsightDraft  `yaml:"insights"`
	Now           string                     `yaml:"now"`
	DryRun        bool                       `yaml:"dry_run"`
}

type CompressionNarrativeDraft struct {
	ID               string   `yaml:"id"`
	Scope            string   `yaml:"scope"`
	ScopeRef         string   `yaml:"scope_ref"`
	Summary          string   `yaml:"summary"`
	EmotionalTone    string   `yaml:"emotional_tone"`
	ValenceAvg       *float64 `yaml:"valence_avg"`
	ArousalAvg       *float64 `yaml:"arousal_avg"`
	Importance       float64  `yaml:"importance"`
	ValidFrom        string   `yaml:"valid_from"`
	ValidTo          string   `yaml:"valid_to"`
	SensitivityLevel string   `yaml:"sensitivity_level"`
}

type CompressionInsightDraft struct {
	ID               string  `yaml:"id"`
	InsightType      string  `yaml:"insight_type"`
	Content          string  `yaml:"content"`
	Confidence       float64 `yaml:"confidence"`
	Importance       float64 `yaml:"importance"`
	Valence          float64 `yaml:"valence"`
	Arousal          float64 `yaml:"arousal"`
	SensitivityLevel string  `yaml:"sensitivity_level"`
}

type RebuildSearchStep struct {
	PersonaID string `yaml:"persona_id"`
}

type MirrorRebuildStep struct {
	PersonaID string `yaml:"persona_id"`
}

type MirrorSyncStep struct {
	PersonaID string `yaml:"persona_id"`
	Limit     int    `yaml:"limit"`
}

type LinkStep struct {
	ID               string  `yaml:"id"`
	PersonaID        string  `yaml:"persona_id"`
	FromNodeType     string  `yaml:"from_node_type"`
	FromNodeID       string  `yaml:"from_node_id"`
	LinkType         string  `yaml:"link_type"`
	ToNodeType       string  `yaml:"to_node_type"`
	ToNodeID         string  `yaml:"to_node_id"`
	Weight           float64 `yaml:"weight"`
	VisibilityStatus string  `yaml:"visibility_status"`
	Searchable       *bool   `yaml:"searchable"`
}

// FactStep seeds facts that normal consolidation intentionally rejects, such as
// missing-entity or hidden-source authority edge cases.
type FactStep struct {
	ID               string   `yaml:"id"`
	PersonaID        string   `yaml:"persona_id"`
	SubjectEntityID  string   `yaml:"subject_entity_id"`
	Predicate        string   `yaml:"predicate"`
	ObjectEntityID   string   `yaml:"object_entity_id"`
	ObjectLiteral    *string  `yaml:"object_literal"`
	ContentSummary   string   `yaml:"content_summary"`
	FactType         string   `yaml:"fact_type"`
	ValidFrom        string   `yaml:"valid_from"`
	ValidTo          string   `yaml:"valid_to"`
	Confidence       string   `yaml:"confidence"`
	ConfidenceScore  float64  `yaml:"confidence_score"`
	Importance       float64  `yaml:"importance"`
	Valence          float64  `yaml:"valence"`
	Arousal          float64  `yaml:"arousal"`
	SensitivityLevel string   `yaml:"sensitivity_level"`
	ValidityStatus   string   `yaml:"validity_status"`
	VisibilityStatus string   `yaml:"visibility_status"`
	LifecycleStatus  string   `yaml:"lifecycle_status"`
	Searchable       *bool    `yaml:"searchable"`
	Pinned           bool     `yaml:"pinned"`
	PinReason        string   `yaml:"pin_reason"`
	PinActor         string   `yaml:"pin_actor"`
	SourceEpisodeIDs []string `yaml:"source_episode_ids"`
}

type FactOverride struct {
	FactID           string `yaml:"fact_id"`
	VisibilityStatus string `yaml:"visibility_status"`
	ValidityStatus   string `yaml:"validity_status"`
	LifecycleStatus  string `yaml:"lifecycle_status"`
	SensitivityLevel string `yaml:"sensitivity_level"`
	UpdatedAt        string `yaml:"updated_at"`
	Searchable       *bool  `yaml:"searchable"`
	Pinned           *bool  `yaml:"pinned"`
}

type MirrorStubSettings struct {
	IndexMappedNodeID string            `yaml:"index_mapped_node_id"`
	IndexMappedType   string            `yaml:"index_mapped_type"`
	IndexMappedNodes  []MirrorMapStub   `yaml:"index_mapped_nodes"`
	CandidateNodeID   string            `yaml:"candidate_node_id"`
	CandidateNodeType string            `yaml:"candidate_node_type"`
	CandidateScore    float64           `yaml:"candidate_score"`
	Candidates        []MirrorCandidate `yaml:"candidates"`
	Unavailable       bool              `yaml:"unavailable"`
}

type MirrorMapStub struct {
	NodeID   string `yaml:"node_id"`
	NodeType string `yaml:"node_type"`
}

type MirrorCandidate struct {
	NodeID        string  `yaml:"node_id"`
	NodeType      string  `yaml:"node_type"`
	TriviumNodeID int64   `yaml:"trivium_node_id"`
	Score         float64 `yaml:"score"`
	Source        string  `yaml:"source"`
	Rank          int     `yaml:"rank"`
}

type GraphStubSettings struct {
	Candidates     []GraphCandidateStub `yaml:"candidates"`
	Unavailable    bool                 `yaml:"unavailable"`
	Degraded       bool                 `yaml:"degraded"`
	FallbackReason string               `yaml:"fallback_reason"`
}

type GraphCandidateStub struct {
	NodeID             string   `yaml:"node_id"`
	NodeType           string   `yaml:"node_type"`
	TriviumNodeID      int64    `yaml:"trivium_node_id"`
	Score              float64  `yaml:"score"`
	Source             string   `yaml:"source"`
	Rank               int      `yaml:"rank"`
	PathNodeIDs        []string `yaml:"path_node_ids"`
	PathTriviumNodeIDs []int64  `yaml:"path_trivium_node_ids"`
	PathLinkTypes      []string `yaml:"path_link_types"`
}

type RerankStubSettings struct {
	Items          []RerankItemStub `yaml:"items"`
	Unavailable    bool             `yaml:"unavailable"`
	Degraded       bool             `yaml:"degraded"`
	FallbackReason string           `yaml:"fallback_reason"`
}

type RerankItemStub struct {
	NodeID      string  `yaml:"node_id"`
	NodeType    string  `yaml:"node_type"`
	Score       float64 `yaml:"score"`
	DebugReason string  `yaml:"debug_reason"`
}

type Assertion struct {
	Type                    string   `yaml:"type"`
	Name                    string   `yaml:"name"`
	Step                    string   `yaml:"step"`
	NodeID                  string   `yaml:"node_id"`
	NodeType                string   `yaml:"node_type"`
	NodeIDs                 []string `yaml:"node_ids"`
	RelevantNodeIDs         []string `yaml:"relevant_node_ids"`
	ForbiddenNodeIDs        []string `yaml:"forbidden_node_ids"`
	BlockType               string   `yaml:"block_type"`
	Summary                 string   `yaml:"summary"`
	UsageGuidanceContains   string   `yaml:"usage_guidance_contains"`
	Action                  string   `yaml:"action"`
	Status                  string   `yaml:"status"`
	Content                 string   `yaml:"content"`
	FactID                  string   `yaml:"fact_id"`
	Predicate               string   `yaml:"predicate"`
	Column                  string   `yaml:"column"`
	Equals                  string   `yaml:"equals"`
	FromNodeID              string   `yaml:"from_node_id"`
	FromNodeType            string   `yaml:"from_node_type"`
	LinkType                string   `yaml:"link_type"`
	Direction               string   `yaml:"direction"`
	ToNodeID                string   `yaml:"to_node_id"`
	ToNodeType              string   `yaml:"to_node_type"`
	SearchText              string   `yaml:"search_text"`
	DeletionEventID         string   `yaml:"deletion_event_id"`
	ForbiddenContains       []string `yaml:"forbidden_contains"`
	EpisodeID               string   `yaml:"episode_id"`
	TimeMode                string   `yaml:"time_mode"`
	Signals                 []string `yaml:"signals"`
	MemoryDomain            string   `yaml:"memory_domain"`
	MemoryAbility           string   `yaml:"memory_ability"`
	EvidenceNeed            string   `yaml:"evidence_need"`
	EntityMentions          []string `yaml:"entity_mentions"`
	Source                  string   `yaml:"source"`
	Rank                    int      `yaml:"rank"`
	At                      int      `yaml:"at"`
	Min                     float64  `yaml:"min"`
	Max                     float64  `yaml:"max"`
	CompareStep             string   `yaml:"compare_step"`
	HistoricalStatus        string   `yaml:"historical_status"`
	RelatedHistoricalStatus string   `yaml:"related_historical_status"`
	SourceRefCount          int      `yaml:"source_ref_count"`
	SuppressionReason       string   `yaml:"suppression_reason"`
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
		case "retention_run":
			if step.RetentionRun == nil {
				return fmt.Errorf("case %s step %s missing retention_run body", caseID, step.ID)
			}
		case "compression_apply":
			if step.Compression == nil {
				return fmt.Errorf("case %s step %s missing compression_apply body", caseID, step.ID)
			}
		case "rebuild_search":
			if step.RebuildSearch == nil {
				return fmt.Errorf("case %s step %s missing rebuild_search body", caseID, step.ID)
			}
		case "mirror_rebuild":
			if step.MirrorRebuild == nil {
				return fmt.Errorf("case %s step %s missing mirror_rebuild body", caseID, step.ID)
			}
		case "mirror_sync":
			if step.MirrorSync == nil {
				return fmt.Errorf("case %s step %s missing mirror_sync body", caseID, step.ID)
			}
		case "link":
			if step.Link == nil {
				return fmt.Errorf("case %s step %s missing link body", caseID, step.ID)
			}
		case "fact":
			if step.Fact == nil {
				return fmt.Errorf("case %s step %s missing fact body", caseID, step.ID)
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
		"query_analysis",
		"anchor_fusion",
		"fact_count",
		"fact_column",
		"link_exists",
		"narrative_exists",
		"insight_exists",
		"derived_link_count",
		"search_absent",
		"deletion_event_safe",
		"episode_tombstone_exists",
		"mirror_index_status",
		"queue_count",
		"queue_status",
		"selected_recall_at_k",
		"context_precision_at_k",
		"forbidden_recall_zero",
		"block_contains",
		"block_not_contains",
		"selected_chain_correct",
		"suppression_event",
		"mirror_candidate",
		"graph_activation_candidate",
		"rerank_status",
		"rerank_input",
		"unsupported_premise_not_asserted",
		"ablation_improves":
		return true
	default:
		return false
	}
}
