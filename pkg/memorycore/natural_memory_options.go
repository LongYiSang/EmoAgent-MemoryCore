package memorycore

import "time"

type NaturalMemoryOptions struct {
	Enabled          bool
	AlgorithmVersion string
	SleepCycle       NaturalMemorySleepCycleOptions
	ManualTrigger    NaturalMemoryManualTriggerOptions
	Scoring          NaturalMemoryScoringOptions
	FactDefaults     map[FactType]NaturalMemoryTypeDefault
	Protection       NaturalMemoryProtectionOptions
	SearchTier       NaturalMemorySearchTierOptions
	Compression      NaturalMemoryCompressionOptions
	Limits           NaturalMemoryLimitsOptions
}

type NaturalMemorySleepCycleOptions struct {
	Enabled                  bool
	LocalTime                string
	Timezone                 string
	MinInterval              time.Duration
	RunMissedOnStart         bool
	Jitter                   time.Duration
	NightWindowStart         string
	NightWindowEnd           string
	WarnIfOutsideNightWindow bool
}

type NaturalMemoryManualTriggerOptions struct {
	Enabled                 bool
	AllowForce              bool
	AllowDryRun             bool
	MarkSleepCycleByDefault bool
}

type NaturalMemoryScoringOptions struct {
	Model                    string
	DefaultDecayExponent     float64
	ReactivationThreshold    float64
	ReactivationGain         float64
	FirstSleepBoost          float64
	FirstSleepWindow         time.Duration
	EmotionSalienceLambda    float64
	EmotionPersistenceLambda float64
}

type NaturalMemoryTypeDefault struct {
	TauDays    float64
	Alpha      float64
	Protected  bool
	UseValidTo bool
}

type NaturalMemoryProtectionOptions struct {
	ProtectPinned           bool
	ProtectCoreIdentity     bool
	ProtectActiveCommitment bool
	ProtectExplicitBoundary bool
	ProtectedMinTier        SearchTier
}

type NaturalMemorySearchTierOptions struct {
	Enabled                          bool
	WriteMemorySearchDocuments       bool
	UpdateOnlySearchTierAndUpdatedAt bool
	HotMin                           float64
	WarmMin                          float64
	ColdMin                          float64
	DefaultFadedTier                 SearchTier
	EnqueueMirrorUpsertOnTierChange  bool
}

type NaturalMemoryCompressionOptions struct {
	Enabled               bool
	EmitCandidates        bool
	ApplyWithoutLLM       bool
	MinClusterSize        int
	MaxCandidatesPerRun   int
	AllowedSourceTiers    []SearchTier
	ExcludeFactTypes      []FactType
	RequireMinConfidence  float64
	RequireLowSensitivity bool
}

type NaturalMemoryLimitsOptions struct {
	MaxCandidatesPerRun int
	MaxWritesPerRun     int
	BatchSize           int
	MaxExplainItems     int
}

func defaultNaturalMemoryOptions() NaturalMemoryOptions {
	return DefaultNaturalMemoryOptions()
}

func DefaultNaturalMemoryOptions() NaturalMemoryOptions {
	return NaturalMemoryOptions{
		Enabled:          true,
		AlgorithmVersion: NaturalMemoryAlgorithmPowerSleepV1,
		SleepCycle: NaturalMemorySleepCycleOptions{
			Enabled:                  true,
			LocalTime:                "03:30",
			MinInterval:              20 * time.Hour,
			NightWindowStart:         "01:00",
			NightWindowEnd:           "05:00",
			WarnIfOutsideNightWindow: true,
		},
		ManualTrigger: NaturalMemoryManualTriggerOptions{
			Enabled:     true,
			AllowForce:  true,
			AllowDryRun: true,
		},
		Scoring: NaturalMemoryScoringOptions{
			Model:                    "power_law_with_reactivation",
			DefaultDecayExponent:     0.6,
			ReactivationThreshold:    0.55,
			ReactivationGain:         0.35,
			FirstSleepBoost:          0.25,
			FirstSleepWindow:         36 * time.Hour,
			EmotionSalienceLambda:    0.15,
			EmotionPersistenceLambda: 0.15,
		},
		FactDefaults: map[FactType]NaturalMemoryTypeDefault{
			FactTypeCoreIdentity:        {TauDays: 0, Alpha: 0, Protected: true},
			FactTypeSignificantEvent:    {TauDays: 180, Alpha: 0.45},
			FactTypeStablePreference:    {TauDays: 90, Alpha: 0.55},
			FactTypeRelationalState:     {TauDays: 60, Alpha: 0.65},
			FactTypeCommitment:          {TauDays: 0, Alpha: 0.80, UseValidTo: true},
			FactTypeTransientContext:    {TauDays: 7, Alpha: 0.90},
			FactTypeTaskRelevantContext: {TauDays: 3, Alpha: 1.00},
		},
		Protection: NaturalMemoryProtectionOptions{
			ProtectPinned:           true,
			ProtectCoreIdentity:     true,
			ProtectActiveCommitment: true,
			ProtectExplicitBoundary: true,
			ProtectedMinTier:        SearchTierWarm,
		},
		SearchTier: NaturalMemorySearchTierOptions{
			Enabled:                          true,
			WriteMemorySearchDocuments:       true,
			UpdateOnlySearchTierAndUpdatedAt: true,
			HotMin:                           0.65,
			WarmMin:                          0.40,
			ColdMin:                          0.20,
			DefaultFadedTier:                 SearchTierDeepCold,
			EnqueueMirrorUpsertOnTierChange:  true,
		},
		Compression: NaturalMemoryCompressionOptions{
			Enabled:               true,
			EmitCandidates:        true,
			MinClusterSize:        3,
			MaxCandidatesPerRun:   20,
			AllowedSourceTiers:    []SearchTier{SearchTierCold, SearchTierDeepCold},
			ExcludeFactTypes:      []FactType{FactTypeCoreIdentity, FactTypeCommitment},
			RequireMinConfidence:  0.70,
			RequireLowSensitivity: true,
		},
		Limits: NaturalMemoryLimitsOptions{
			MaxCandidatesPerRun: 5000,
			MaxWritesPerRun:     1000,
			BatchSize:           200,
			MaxExplainItems:     100,
		},
	}
}
