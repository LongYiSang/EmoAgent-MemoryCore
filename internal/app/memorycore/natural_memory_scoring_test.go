package memorycore

import (
	"testing"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

func TestNaturalPowerLawMonotonic(t *testing.T) {
	node := NaturalMemoryNodeForTest(core.FactTypeStablePreference, 0.7, 0.8)
	now := time.Date(2026, 6, 5, 3, 30, 0, 0, time.UTC)
	recent := node
	recent.LastStrengthenedAt = naturalTimePtr(now.AddDate(0, 0, -3))
	old := node
	old.LastStrengthenedAt = naturalTimePtr(now.AddDate(0, 0, -30))

	earlier := scoreNaturalMemoryNode(recent, defaultNaturalMemoryOptions(), now)
	later := scoreNaturalMemoryNode(old, defaultNaturalMemoryOptions(), now)

	if !(earlier.Retrievability > later.Retrievability) {
		t.Fatalf("retrievability should decay monotonically: earlier=%f later=%f", earlier.Retrievability, later.Retrievability)
	}
}

func TestNaturalPowerLawLargerTauDecaysSlower(t *testing.T) {
	now := time.Date(2026, 6, 5, 3, 30, 0, 0, time.UTC)
	opts := defaultNaturalMemoryOptions()
	shortTau := NaturalMemoryNodeForTest(core.FactTypeTransientContext, 0.7, 0.8)
	longTau := NaturalMemoryNodeForTest(core.FactTypeSignificantEvent, 0.7, 0.8)
	shortTau.LastStrengthenedAt = naturalTimePtr(now.AddDate(0, 0, -30))
	longTau.LastStrengthenedAt = naturalTimePtr(now.AddDate(0, 0, -30))

	shortScore := scoreNaturalMemoryNode(shortTau, opts, now)
	longScore := scoreNaturalMemoryNode(longTau, opts, now)

	if !(longScore.Retrievability > shortScore.Retrievability) {
		t.Fatalf("larger tau should decay slower: long=%f short=%f", longScore.Retrievability, shortScore.Retrievability)
	}
}

func TestNaturalTauIncludesStructuralAssociationSignificance(t *testing.T) {
	now := time.Date(2026, 6, 5, 3, 30, 0, 0, time.UTC)
	base := NaturalMemoryNodeForTest(core.FactTypeTransientContext, 0.5, 0.5)
	base.LastStrengthenedAt = naturalTimePtr(now)
	structural := base
	structural.StructuralAssociationCount = 3

	baseScore := scoreNaturalMemoryNode(base, defaultNaturalMemoryOptions(), now)
	structuralScore := scoreNaturalMemoryNode(structural, defaultNaturalMemoryOptions(), now)

	if !(structuralScore.StabilityDays > baseScore.StabilityDays) {
		t.Fatalf("structural tau = %f, want greater than base tau %f", structuralScore.StabilityDays, baseScore.StabilityDays)
	}
}

func TestNaturalReactivationUsesRecentAccessPromptInjectedAndStructuralSignals(t *testing.T) {
	now := time.Date(2026, 6, 5, 3, 30, 0, 0, time.UTC)
	opts := defaultNaturalMemoryOptions()
	node := NaturalMemoryNodeForTest(core.FactTypeTransientContext, 0.7, 0.8)
	node.RecentAccessEventCount = 2
	node.RecentPromptInjectedCount = 1
	node.StructuralAssociationCount = 2

	score := scoreNaturalMemoryNode(node, opts, now)

	if !score.Reactivated {
		t.Fatalf("reactivated = false, want true from recent access/prompt/structure signals; q=%f", score.ReactivationScore)
	}
	if score.ReactivationScore < opts.Scoring.ReactivationThreshold {
		t.Fatalf("reactivation score = %f, want >= threshold %f", score.ReactivationScore, opts.Scoring.ReactivationThreshold)
	}
}

func TestNaturalReactivationAndFirstSleepBoostWorkOnce(t *testing.T) {
	now := time.Date(2026, 6, 5, 3, 30, 0, 0, time.UTC)
	opts := defaultNaturalMemoryOptions()
	node := NaturalMemoryNodeForTest(core.FactTypeStablePreference, 0.7, 0.8)
	node.CreatedAt = now.Add(-12 * time.Hour)
	node.LastAccessedAt = &now
	node.AccessCount = 3
	node.ReinforcementCount = 1

	first := scoreNaturalMemoryNode(node, opts, now)
	if !first.Reactivated {
		t.Fatalf("reactivated = false, want true")
	}
	if !first.FirstSleepConsolidated {
		t.Fatalf("first sleep consolidated = false, want true")
	}
	if first.StabilityDays <= opts.FactDefaults[core.FactTypeStablePreference].TauDays {
		t.Fatalf("stability days = %f, want boosted beyond base tau", first.StabilityDays)
	}

	node.FirstSleepConsolidated = true
	second := scoreNaturalMemoryNode(node, opts, now.Add(2*time.Hour))
	if second.FirstSleepConsolidated {
		t.Fatalf("first sleep consolidated twice")
	}
}

func TestNaturalCommitmentValidToUsesRunNow(t *testing.T) {
	now := time.Date(2035, 1, 1, 12, 0, 0, 0, time.UTC)
	node := NaturalMemoryNodeForTest(core.FactTypeCommitment, 0, 0)
	node.ValidTo = naturalTimePtr(now.Add(10 * 24 * time.Hour))
	node.LastStrengthenedAt = naturalTimePtr(now)

	score := scoreNaturalMemoryNode(node, defaultNaturalMemoryOptions(), now)

	if score.StabilityDays < 9.9 || score.StabilityDays > 10.1 {
		t.Fatalf("commitment stability days = %f, want about 10 from run now", score.StabilityDays)
	}
}

func TestNaturalNarrativeAndInsightDefaultsUseSubtype(t *testing.T) {
	now := time.Date(2026, 6, 5, 3, 30, 0, 0, time.UTC)
	narrative := NaturalMemoryNodeForTest("", 0, 0)
	narrative.NodeType = core.NodeTypeNarrative
	narrative.NodeID = "narrative_relationship"
	narrative.ClusterKey = "relationship_phase"
	narrative.LastStrengthenedAt = naturalTimePtr(now)

	narrativeScore := scoreNaturalMemoryNode(narrative, defaultNaturalMemoryOptions(), now)
	if narrativeScore.StabilityDays < 239.9 || narrativeScore.StabilityDays > 240.1 || narrativeScore.DecayExponent != 0.40 {
		t.Fatalf("narrative score = tau %f alpha %f, want relationship_phase 240/0.40", narrativeScore.StabilityDays, narrativeScore.DecayExponent)
	}

	insight := NaturalMemoryNodeForTest("", 0, 0)
	insight.NodeType = core.NodeTypeInsight
	insight.NodeID = "insight_preference"
	insight.ClusterKey = "preference"
	insight.LastStrengthenedAt = naturalTimePtr(now)

	insightScore := scoreNaturalMemoryNode(insight, defaultNaturalMemoryOptions(), now)
	if insightScore.StabilityDays < 179.9 || insightScore.StabilityDays > 180.1 || insightScore.DecayExponent != 0.45 {
		t.Fatalf("insight score = tau %f alpha %f, want preference 180/0.45", insightScore.StabilityDays, insightScore.DecayExponent)
	}
}

func TestNaturalStateToSearchTierMapping(t *testing.T) {
	opts := defaultNaturalMemoryOptions()
	tests := []struct {
		score float64
		tier  core.SearchTier
		state NaturalMemoryState
	}{
		{score: 0.80, tier: core.SearchTierHot, state: NaturalMemoryStateSalient},
		{score: 0.50, tier: core.SearchTierWarm, state: NaturalMemoryStateAvailable},
		{score: 0.25, tier: core.SearchTierCold, state: NaturalMemoryStateLatent},
		{score: 0.10, tier: core.SearchTierDeepCold, state: NaturalMemoryStateFaded},
	}

	for _, tc := range tests {
		state, tier := naturalStateAndTier(tc.score, false, opts.SearchTier)
		if state != tc.state || tier != tc.tier {
			t.Fatalf("score %f mapped to %s/%s, want %s/%s", tc.score, state, tier, tc.state, tc.tier)
		}
	}
}

func naturalTimePtr(value time.Time) *time.Time {
	return &value
}
