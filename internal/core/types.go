package core

type NodeType string

const (
	NodeTypeEpisode                 NodeType = "episode"
	NodeTypeEntity                  NodeType = "entity"
	NodeTypeFact                    NodeType = "fact"
	NodeTypeNarrative               NodeType = "narrative"
	NodeTypeInsight                 NodeType = "insight"
	NodeTypeMoodState               NodeType = "mood_state"
	NodeTypeAffectEvent             NodeType = "affect_event"
	NodeTypeAgentAffectProfile      NodeType = "agent_affect_profile"
	NodeTypeAgentAffectState        NodeType = "agent_affect_state"
	NodeTypeAgentAppraisal          NodeType = "agent_appraisal"
	NodeTypeAgentAffectEvent        NodeType = "agent_affect_event"
	NodeTypeAgentExpressionDecision NodeType = "agent_expression_decision"
	NodeTypeDeletionEvent           NodeType = "deletion_event"
)

type Channel string

const (
	ChannelWebUI    Channel = "webui"
	ChannelTelegram Channel = "telegram"
	ChannelQQ       Channel = "qq"
	ChannelCLI      Channel = "cli"
	ChannelAPI      Channel = "api"
	ChannelImported Channel = "imported"
	ChannelOther    Channel = "other"
)

type Role string

const (
	RoleUser        Role = "user"
	RoleAssistant   Role = "assistant"
	RoleSystem      Role = "system"
	RoleToolSummary Role = "tool_summary"
	RoleWorkReport  Role = "work_report"
)

type SourceType string

const (
	SourceTypeChat          SourceType = "chat"
	SourceTypeWorkCandidate SourceType = "work_candidate"
	SourceTypePlugin        SourceType = "plugin"
	SourceTypeSystem        SourceType = "system"
	SourceTypeImported      SourceType = "imported"
)

type VisibilityStatus string

const (
	VisibilityVisible   VisibilityStatus = "visible"
	VisibilityHidden    VisibilityStatus = "hidden"
	VisibilityRedacted  VisibilityStatus = "redacted"
	VisibilityForgotten VisibilityStatus = "forgotten"
	VisibilityPurged    VisibilityStatus = "purged"
)

type LifecycleStatus string

const (
	LifecycleActive       LifecycleStatus = "active"
	LifecycleDormant      LifecycleStatus = "dormant"
	LifecycleConsolidated LifecycleStatus = "consolidated"
	LifecycleArchived     LifecycleStatus = "archived"
	LifecycleDeepArchived LifecycleStatus = "deep_archived"
)

type ValidityStatus string

const (
	ValidityValid       ValidityStatus = "valid"
	ValidityInvalidated ValidityStatus = "invalidated"
	ValidityUncertain   ValidityStatus = "uncertain"
)

type SensitivityLevel string

const (
	SensitivityNormal          SensitivityLevel = "normal"
	SensitivitySensitive       SensitivityLevel = "sensitive"
	SensitivityHighlySensitive SensitivityLevel = "highly_sensitive"
)

type EntityType string

const (
	EntityTypeUser       EntityType = "user"
	EntityTypeAgent      EntityType = "agent"
	EntityTypePerson     EntityType = "person"
	EntityTypePlace      EntityType = "place"
	EntityTypeOrg        EntityType = "org"
	EntityTypeConcept    EntityType = "concept"
	EntityTypeObject     EntityType = "object"
	EntityTypeEventTopic EntityType = "event_topic"
)

type AliasType string

const (
	AliasTypeSurface      AliasType = "surface"
	AliasTypeNickname     AliasType = "nickname"
	AliasTypeTranslation  AliasType = "translation"
	AliasTypeAbbreviation AliasType = "abbreviation"
)

type FactType string

const (
	FactTypeCoreIdentity        FactType = "core_identity"
	FactTypeSignificantEvent    FactType = "significant_event"
	FactTypeStablePreference    FactType = "stable_preference"
	FactTypeRelationalState     FactType = "relational_state"
	FactTypeCommitment          FactType = "commitment"
	FactTypeTransientContext    FactType = "transient_context"
	FactTypeTaskRelevantContext FactType = "task_relevant_context"
)

type ExtractionConfidence string

const (
	ExtractionConfidenceExplicit  ExtractionConfidence = "explicit"
	ExtractionConfidenceInferred  ExtractionConfidence = "inferred"
	ExtractionConfidenceAmbiguous ExtractionConfidence = "ambiguous"
)

type ConflictPolicy string

const (
	ConflictPolicySupersede    ConflictPolicy = "supersede"
	ConflictPolicyCoexist      ConflictPolicy = "coexist"
	ConflictPolicyMerge        ConflictPolicy = "merge"
	ConflictPolicyLLMCheck     ConflictPolicy = "llm_check"
	ConflictPolicyExpireByTime ConflictPolicy = "expire_by_time"
)

type LinkType string

const (
	LinkTypeEvidencedBy LinkType = "EVIDENCED_BY"
	LinkTypeDerivedFrom LinkType = "DERIVED_FROM"
	LinkTypeSupersedes  LinkType = "SUPERSEDES"
	LinkTypeAboutEntity LinkType = "ABOUT_ENTITY"
)

type LinkDirection string

const (
	LinkDirectionForward       LinkDirection = "forward"
	LinkDirectionBackward      LinkDirection = "backward"
	LinkDirectionBidirectional LinkDirection = "bidirectional"
)

type LinkCreatedBy string

const (
	LinkCreatedBySystem        LinkCreatedBy = "system"
	LinkCreatedByLLM           LinkCreatedBy = "llm"
	LinkCreatedByUser          LinkCreatedBy = "user"
	LinkCreatedByConsolidation LinkCreatedBy = "consolidation"
)

type SearchTier string

const (
	SearchTierHot      SearchTier = "hot"
	SearchTierWarm     SearchTier = "warm"
	SearchTierCold     SearchTier = "cold"
	SearchTierDeepCold SearchTier = "deep_cold"
)

const (
	MemorySuppressionReasonFatigue       = "fatigue"
	MemorySuppressionReasonMMRDuplicate  = "mmr_duplicate"
	MemorySuppressionReasonContextBudget = "context_budget"
)
