package memorycore

import (
	"math"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type naturalMemoryNode struct {
	PersonaID                  string
	NodeType                   core.NodeType
	NodeID                     string
	FactType                   core.FactType
	Importance                 float64
	Confidence                 float64
	SensitivityLevel           core.SensitivityLevel
	ValidityStatus             core.ValidityStatus
	VisibilityStatus           core.VisibilityStatus
	LifecycleStatus            core.LifecycleStatus
	Pinned                     bool
	Searchable                 bool
	AccessCount                int
	ReinforcementCount         int
	CongruentAccessCount       int
	RecentAccessEventCount     int
	RecentPromptInjectedCount  int
	RecentReinforcedEventCount int
	StructuralAssociationCount int
	CreatedAt                  time.Time
	UpdatedAt                  *time.Time
	IngestedAt                 *time.Time
	LastAccessedAt             *time.Time
	ValidTo                    *time.Time
	CurrentSearchTier          core.SearchTier
	PreviousNaturalState       NaturalMemoryState
	FirstSleepConsolidated     bool
	EmotionSalienceHint        float64
	EmotionPersistenceHint     float64
	ReactivationCount          int
	LastReactivatedAt          *time.Time
	LastStrengthenedAt         *time.Time
	ClusterKey                 string
}

type naturalMemoryScore struct {
	NaturalStrength        float64
	Retrievability         float64
	StabilityDays          float64
	DecayExponent          float64
	ReactivationScore      float64
	NaturalState           NaturalMemoryState
	NaturalTier            core.SearchTier
	EffectiveSearchTier    core.SearchTier
	Protected              bool
	ProtectedReason        string
	Reactivated            bool
	FirstSleepConsolidated bool
	ReasonCodes            []string
	SafeReasonSummary      string
}

func NaturalMemoryNodeForTest(factType core.FactType, importance float64, confidence float64) naturalMemoryNode {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return naturalMemoryNode{
		PersonaID:         "default",
		NodeType:          core.NodeTypeFact,
		NodeID:            "fact_test",
		FactType:          factType,
		Importance:        importance,
		Confidence:        confidence,
		SensitivityLevel:  core.SensitivityNormal,
		ValidityStatus:    core.ValidityValid,
		VisibilityStatus:  core.VisibilityVisible,
		LifecycleStatus:   core.LifecycleActive,
		Searchable:        true,
		CreatedAt:         created,
		CurrentSearchTier: core.SearchTierHot,
	}
}

func scoreNaturalMemoryNode(node naturalMemoryNode, opts NaturalMemoryOptions, now time.Time) naturalMemoryScore {
	opts = normalizeNaturalMemoryOptions(opts)
	score := naturalMemoryScore{}
	protected, reason := naturalProtectedReason(node, opts, now)
	if protected {
		score.Protected = true
		score.ProtectedReason = reason
		score.NaturalStrength = 1
		score.Retrievability = 1
		score.StabilityDays = naturalBaseTau(node, opts, now)
		score.DecayExponent = naturalAlpha(node, opts)
		score.NaturalState = NaturalMemoryStateSalient
		score.NaturalTier = warmerTier(core.SearchTierHot, opts.Protection.ProtectedMinTier)
		score.EffectiveSearchTier = colderTier(score.NaturalTier, lifecycleSearchTierCap(node.LifecycleStatus))
		score.ReasonCodes = []string{reason}
		score.SafeReasonSummary = "protected memory"
		return score
	}

	baseTau := naturalBaseTau(node, opts, now)
	alpha := naturalAlpha(node, opts)
	if alpha == 0 {
		alpha = opts.Scoring.DefaultDecayExponent
	}
	if baseTau <= 0 {
		baseTau = 1
	}
	prior := naturalRetentionPrior(node)
	graph := clamp(float64(node.StructuralAssociationCount)/3, 0, 1)
	tau := baseTau * (1 + 0.8*clamp(node.Importance, 0, 1) + 0.5*graph + 0.4*clamp(node.Confidence, 0, 1) + 0.25*math.Log1p(float64(node.ReinforcementCount)))
	tau *= 1 + opts.Scoring.EmotionSalienceLambda*clamp(node.EmotionSalienceHint, 0, 1) + opts.Scoring.EmotionPersistenceLambda*clamp(node.EmotionPersistenceHint, 0, 1)

	q := naturalReactivationScore(node, now)
	reasonCodes := []string{"scored"}
	reactivated := q >= opts.Scoring.ReactivationThreshold
	if reactivated {
		tau *= 1 + opts.Scoring.ReactivationGain*q
		reasonCodes = append(reasonCodes, "reactivated")
	}
	firstSleep := false
	if !node.FirstSleepConsolidated && reactivated && !node.CreatedAt.IsZero() && now.Sub(node.CreatedAt) >= 0 && now.Sub(node.CreatedAt) <= opts.Scoring.FirstSleepWindow {
		tau *= 1 + opts.Scoring.FirstSleepBoost
		firstSleep = true
		reasonCodes = append(reasonCodes, "first_sleep_consolidated")
	}

	delta := now.Sub(naturalLastStrengthenedAt(node))
	if delta < 0 {
		delta = 0
	}
	deltaDays := delta.Hours() / 24
	retrievability := clamp(prior*math.Pow(1+deltaDays/tau, -alpha), 0, 1.5)
	state, tier := naturalStateAndTier(retrievability, firstSleep, opts.SearchTier)
	return naturalMemoryScore{
		NaturalStrength:        retrievability,
		Retrievability:         retrievability,
		StabilityDays:          tau,
		DecayExponent:          alpha,
		ReactivationScore:      q,
		NaturalState:           state,
		NaturalTier:            tier,
		EffectiveSearchTier:    colderTier(tier, lifecycleSearchTierCap(node.LifecycleStatus)),
		Reactivated:            reactivated,
		FirstSleepConsolidated: firstSleep,
		ReasonCodes:            reasonCodes,
		SafeReasonSummary:      "natural retrievability scored",
	}
}

func naturalStateAndTier(score float64, sleepConsolidated bool, opts NaturalMemorySearchTierOptions) (NaturalMemoryState, core.SearchTier) {
	if score >= opts.HotMin {
		if sleepConsolidated {
			return NaturalMemoryStateSleepConsolidated, core.SearchTierHot
		}
		return NaturalMemoryStateSalient, core.SearchTierHot
	}
	if score >= opts.WarmMin {
		if sleepConsolidated {
			return NaturalMemoryStateSleepConsolidated, core.SearchTierWarm
		}
		return NaturalMemoryStateAvailable, core.SearchTierWarm
	}
	if score >= opts.ColdMin {
		return NaturalMemoryStateLatent, core.SearchTierCold
	}
	if opts.DefaultFadedTier != "" {
		return NaturalMemoryStateFaded, opts.DefaultFadedTier
	}
	return NaturalMemoryStateFaded, core.SearchTierDeepCold
}

func naturalProtectedReason(node naturalMemoryNode, opts NaturalMemoryOptions, now time.Time) (bool, string) {
	if opts.Protection.ProtectPinned && node.Pinned {
		return true, "protected_pinned"
	}
	if opts.Protection.ProtectCoreIdentity && node.FactType == core.FactTypeCoreIdentity {
		return true, "protected_core_identity"
	}
	if opts.Protection.ProtectActiveCommitment && node.FactType == core.FactTypeCommitment && node.ValidityStatus == core.ValidityValid {
		if node.ValidTo == nil || node.ValidTo.After(now.Add(-24*time.Hour)) {
			return true, "protected_active_commitment"
		}
	}
	if opts.Protection.ProtectExplicitBoundary && node.NodeType == core.NodeTypeInsight && node.ClusterKey == "boundary" {
		return true, "protected_explicit_boundary"
	}
	defaults := naturalTypeDefault(node, opts)
	if defaults.Protected {
		return true, "protected_type"
	}
	return false, ""
}

func naturalRetentionPrior(node naturalMemoryNode) float64 {
	typeWeight := map[core.FactType]float64{
		core.FactTypeSignificantEvent:    1.0,
		core.FactTypeStablePreference:    0.95,
		core.FactTypeRelationalState:     0.90,
		core.FactTypeCommitment:          0.90,
		core.FactTypeTransientContext:    0.80,
		core.FactTypeTaskRelevantContext: 0.75,
	}
	weight := typeWeight[node.FactType]
	if weight == 0 {
		weight = 0.85
	}
	safety := 1.0
	switch node.SensitivityLevel {
	case core.SensitivitySensitive:
		safety = 0.85
	case core.SensitivityHighlySensitive:
		safety = 0.65
	}
	pin := 1.0
	if node.Pinned {
		pin = 1.2
	}
	return clamp(weight*(0.55+0.45*clamp(node.Importance, 0, 1))*(0.60+0.40*clamp(node.Confidence, 0, 1))*pin*safety, 0, 1.2)
}

func naturalBaseTau(node naturalMemoryNode, opts NaturalMemoryOptions, now time.Time) float64 {
	defaults := naturalTypeDefault(node, opts)
	if defaults.UseValidTo && node.ValidTo != nil && !node.ValidTo.IsZero() {
		days := node.ValidTo.Sub(now).Hours() / 24
		if days > 1 {
			return days
		}
	}
	if defaults.TauDays > 0 {
		return defaults.TauDays
	}
	return 30
}

func naturalAlpha(node naturalMemoryNode, opts NaturalMemoryOptions) float64 {
	defaults := naturalTypeDefault(node, opts)
	if defaults.Alpha > 0 || defaults.Protected {
		return defaults.Alpha
	}
	return opts.Scoring.DefaultDecayExponent
}

func naturalTypeDefault(node naturalMemoryNode, opts NaturalMemoryOptions) NaturalMemoryTypeDefault {
	switch node.NodeType {
	case core.NodeTypeNarrative:
		if defaults, ok := naturalNarrativeDefaults()[strings.TrimSpace(node.ClusterKey)]; ok {
			return defaults
		}
	case core.NodeTypeInsight:
		if defaults, ok := naturalInsightDefaults()[strings.TrimSpace(node.ClusterKey)]; ok {
			return defaults
		}
	default:
		if defaults, ok := opts.FactDefaults[node.FactType]; ok {
			return defaults
		}
	}
	return NaturalMemoryTypeDefault{TauDays: 30, Alpha: opts.Scoring.DefaultDecayExponent}
}

func naturalNarrativeDefaults() map[string]NaturalMemoryTypeDefault {
	return map[string]NaturalMemoryTypeDefault{
		"day":                {TauDays: 14, Alpha: 0.70},
		"week":               {TauDays: 30, Alpha: 0.60},
		"month":              {TauDays: 120, Alpha: 0.50},
		"topic":              {TauDays: 90, Alpha: 0.55},
		"relationship_phase": {TauDays: 240, Alpha: 0.40},
		"project":            {TauDays: 60, Alpha: 0.65},
	}
}

func naturalInsightDefaults() map[string]NaturalMemoryTypeDefault {
	return map[string]NaturalMemoryTypeDefault{
		"preference":      {TauDays: 180, Alpha: 0.45},
		"boundary":        {TauDays: 365, Alpha: 0.30},
		"coping_strategy": {TauDays: 180, Alpha: 0.50},
		"pattern":         {TauDays: 120, Alpha: 0.60},
		"trait":           {TauDays: 180, Alpha: 0.55},
		"risk_signal":     {TauDays: 30, Alpha: 0.90},
	}
}

func naturalLastStrengthenedAt(node naturalMemoryNode) time.Time {
	for _, value := range []*time.Time{
		node.LastReactivatedAt,
		node.LastStrengthenedAt,
		node.LastAccessedAt,
		node.UpdatedAt,
		node.IngestedAt,
		&node.CreatedAt,
	} {
		if value != nil && !value.IsZero() {
			return *value
		}
	}
	return time.Now()
}

func naturalReactivationScore(node naturalMemoryNode, now time.Time) float64 {
	access := 0.0
	if node.LastAccessedAt != nil && !node.LastAccessedAt.IsZero() {
		ageHours := now.Sub(*node.LastAccessedAt).Hours()
		if ageHours < 0 {
			ageHours = 0
		}
		access = clamp(1-ageHours/(7*24), 0, 1)
	}
	if node.AccessCount > 1 {
		access = math.Max(access, clamp(float64(node.AccessCount)/5, 0, 1))
	}
	if node.RecentAccessEventCount > 0 {
		access = math.Max(access, clamp(float64(node.RecentAccessEventCount)/3, 0, 1))
	}
	if node.RecentPromptInjectedCount > 0 {
		access = math.Max(access, clamp(0.75+0.15*float64(node.RecentPromptInjectedCount), 0, 1))
	}
	user := clamp(float64(node.ReinforcementCount)/3, 0, 1)
	if node.RecentReinforcedEventCount > 0 {
		user = math.Max(user, clamp(float64(node.RecentReinforcedEventCount)/2, 0, 1))
	}
	structural := clamp(float64(node.StructuralAssociationCount), 0, 1)
	role := 0.0
	switch node.FactType {
	case core.FactTypeCommitment, core.FactTypeCoreIdentity, core.FactTypeStablePreference:
		role = 0.8
	case core.FactTypeRelationalState:
		role = 0.5
	}
	emotion := 0.0
	return clamp(0.40*access+0.25*user+0.20*structural+0.10*role+0.05*emotion, 0, 1)
}

func lifecycleSearchTierCap(status core.LifecycleStatus) core.SearchTier {
	switch status {
	case core.LifecycleDormant:
		return core.SearchTierWarm
	case core.LifecycleConsolidated, core.LifecycleArchived:
		return core.SearchTierCold
	case core.LifecycleDeepArchived:
		return core.SearchTierDeepCold
	default:
		return core.SearchTierHot
	}
}

func colderTier(a core.SearchTier, b core.SearchTier) core.SearchTier {
	if searchTierRank(a) >= searchTierRank(b) {
		return a
	}
	return b
}

func warmerTier(a core.SearchTier, b core.SearchTier) core.SearchTier {
	if searchTierRank(a) <= searchTierRank(b) {
		return a
	}
	return b
}

func searchTierRank(tier core.SearchTier) int {
	switch tier {
	case core.SearchTierHot:
		return 0
	case core.SearchTierWarm:
		return 1
	case core.SearchTierCold:
		return 2
	case core.SearchTierDeepCold:
		return 3
	default:
		return 1
	}
}

func normalizeNaturalMemoryOptions(opts NaturalMemoryOptions) NaturalMemoryOptions {
	defaults := DefaultNaturalMemoryOptions()
	if isZeroNaturalMemoryOptions(opts) {
		return defaults
	}
	if opts.AlgorithmVersion == "" {
		opts.AlgorithmVersion = defaults.AlgorithmVersion
	}
	if opts.FactDefaults == nil {
		opts.FactDefaults = defaults.FactDefaults
	}
	if opts.Scoring.DefaultDecayExponent == 0 {
		opts.Scoring = defaults.Scoring
	}
	if opts.SearchTier.HotMin == 0 {
		opts.SearchTier = defaults.SearchTier
	}
	if opts.Protection.ProtectedMinTier == "" {
		opts.Protection = defaults.Protection
	}
	if opts.Limits.MaxCandidatesPerRun == 0 {
		opts.Limits = defaults.Limits
	}
	if opts.Compression.MinClusterSize == 0 {
		opts.Compression = defaults.Compression
	}
	if opts.SleepCycle.LocalTime == "" {
		opts.SleepCycle = defaults.SleepCycle
	}
	if !opts.ManualTrigger.Enabled && !opts.Enabled {
		opts.ManualTrigger = defaults.ManualTrigger
	}
	return opts
}

func isZeroNaturalMemoryOptions(opts NaturalMemoryOptions) bool {
	return !opts.Enabled &&
		opts.AlgorithmVersion == "" &&
		opts.FactDefaults == nil &&
		opts.SleepCycle.LocalTime == "" &&
		opts.Scoring.Model == "" &&
		opts.SearchTier.HotMin == 0 &&
		opts.Limits.MaxCandidatesPerRun == 0
}

func clamp(value float64, min float64, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
