package sqlite_test

import (
	"math"
	"testing"

	"github.com/longyisang/emoagent-memorycore/internal/core"
	memsqlite "github.com/longyisang/emoagent-memorycore/internal/store/sqlite"
)

func TestFuseAnchorsWeightedRRFUsesSourceRankWeightOnly(t *testing.T) {
	cfg := memsqlite.AnchorFusionConfig{
		SourceWeights: map[string]float64{
			"fts":    2.0,
			"mirror": 0.5,
		},
	}
	hits := []memsqlite.AnchorHit{
		{NodeID: "fact_a", NodeType: core.NodeTypeFact, Source: "fts", Rank: 1, RawScore: 0.01, DebugReason: "fts-low-raw"},
		{NodeID: "fact_a", NodeType: core.NodeTypeFact, Source: "mirror", Rank: 4, RawScore: 999, DebugReason: "mirror-high-raw"},
		{NodeID: "fact_b", NodeType: core.NodeTypeFact, Source: "fts", Rank: 2, RawScore: 9999, DebugReason: "raw-score-must-not-win"},
	}

	fused := memsqlite.FuseAnchors(hits, cfg)
	if got, want := len(fused), 2; got != want {
		t.Fatalf("len(fused) = %d, want %d", got, want)
	}
	if got, want := fused[0].NodeID, "fact_a"; got != want {
		t.Fatalf("top fused anchor = %q, want %q", got, want)
	}

	wantA := 2.0/61.0 + 0.5/64.0
	wantB := 2.0 / 62.0
	assertFloatNear(t, fused[0].FusedAnchorScore, wantA)
	assertFloatNear(t, fused[1].FusedAnchorScore, wantB)

	breakdown := fused[0].SourceBreakdown
	if got, want := len(breakdown), 2; got != want {
		t.Fatalf("len(source breakdown) = %d, want %d", got, want)
	}
	if got, want := breakdown[0].Source, "fts"; got != want {
		t.Fatalf("first breakdown source = %q, want %q", got, want)
	}
	assertFloatNear(t, breakdown[0].Weight, 2.0)
	assertFloatNear(t, breakdown[0].RRFContribution, 2.0/61.0)
	if got, want := breakdown[0].Rank, 1; got != want {
		t.Fatalf("first breakdown rank = %d, want %d", got, want)
	}
	if got, want := breakdown[0].RawScore, 0.01; got != want {
		t.Fatalf("first breakdown raw score = %v, want %v", got, want)
	}
	if got, want := breakdown[0].DebugReason, "fts-low-raw"; got != want {
		t.Fatalf("first breakdown reason = %q, want %q", got, want)
	}

	rawScoreMutated := []memsqlite.AnchorHit{
		{NodeID: "fact_a", NodeType: core.NodeTypeFact, Source: "fts", Rank: 1, RawScore: 1000000},
		{NodeID: "fact_a", NodeType: core.NodeTypeFact, Source: "mirror", Rank: 4, RawScore: -1000000},
		{NodeID: "fact_b", NodeType: core.NodeTypeFact, Source: "fts", Rank: 2, RawScore: 1000000000},
	}
	mutated := memsqlite.FuseAnchors(rawScoreMutated, cfg)
	if got, want := anchorIDs(mutated), anchorIDs(fused); !equalAnchorStrings(got, want) {
		t.Fatalf("raw score changed fused ordering: got %v, want %v", got, want)
	}
	for i := range fused {
		assertFloatNear(t, mutated[i].FusedAnchorScore, fused[i].FusedAnchorScore)
		assertFloatNear(t, mutated[i].SeedEnergy, fused[i].SeedEnergy)
	}
}

func TestFuseAnchorsSortsFusedAnchorsAndBreakdownDeterministically(t *testing.T) {
	cfg := memsqlite.AnchorFusionConfig{
		SourceWeights: map[string]float64{
			"asource": 1,
			"main":    1,
			"zsource": 1,
		},
	}
	hits := []memsqlite.AnchorHit{
		{NodeID: "fact_b", NodeType: core.NodeTypeFact, Source: "main", Rank: 1},
		{NodeID: "fact_a", NodeType: core.NodeTypeFact, Source: "main", Rank: 1},
		{NodeID: "entity_a", NodeType: core.NodeTypeEntity, Source: "main", Rank: 1},
		{NodeID: "fact_breakdown", NodeType: core.NodeTypeFact, Source: "zsource", Rank: 3},
		{NodeID: "fact_breakdown", NodeType: core.NodeTypeFact, Source: "asource", Rank: 3},
	}

	fused := memsqlite.FuseAnchors(hits, cfg)
	if got, want := anchorIDs(fused), []string{"fact_breakdown", "entity_a", "fact_a", "fact_b"}; !equalAnchorStrings(got, want) {
		t.Fatalf("fused order = %v, want %v", got, want)
	}

	breakdown := fused[0].SourceBreakdown
	if got, want := len(breakdown), 2; got != want {
		t.Fatalf("len(source breakdown) = %d, want %d", got, want)
	}
	if got, want := []string{breakdown[0].Source, breakdown[1].Source}, []string{"asource", "zsource"}; !equalAnchorStrings(got, want) {
		t.Fatalf("breakdown order = %v, want %v", got, want)
	}
}

func TestFuseAnchorsSeedEnergyUsesNormalizedScoreAndNodeTypePriors(t *testing.T) {
	hits := []memsqlite.AnchorHit{
		{NodeID: "fact", NodeType: core.NodeTypeFact, Source: "main", Rank: 1},
		{NodeID: "narrative", NodeType: core.NodeTypeNarrative, Source: "main", Rank: 1},
		{NodeID: "insight", NodeType: core.NodeTypeInsight, Source: "main", Rank: 1},
		{NodeID: "entity", NodeType: core.NodeTypeEntity, Source: "main", Rank: 1},
		{NodeID: "episode", NodeType: core.NodeTypeEpisode, Source: "main", Rank: 1},
	}

	byID := fusedByID(memsqlite.FuseAnchors(hits, memsqlite.AnchorFusionConfig{}))
	for _, anchor := range byID {
		if anchor.SeedEnergy < 0 {
			t.Fatalf("seed energy for %s is negative: %v", anchor.NodeID, anchor.SeedEnergy)
		}
		assertFloatNear(t, anchor.FusedAnchorScore, 1.0/61.0)
	}
	assertFloatNear(t, byID["fact"].SeedEnergy, 1.0)
	assertFloatNear(t, byID["narrative"].SeedEnergy, 0.9)
	assertFloatNear(t, byID["insight"].SeedEnergy, 1.1)
	assertFloatNear(t, byID["entity"].SeedEnergy, 0.7)
	assertFloatNear(t, byID["episode"].SeedEnergy, 0.4)
}

func TestFuseAnchorsAppliesMaxSeedCaps(t *testing.T) {
	t.Run("per source", func(t *testing.T) {
		cfg := memsqlite.DefaultAnchorFusionConfig()
		cfg.MaxSeedTotal = 10
		cfg.MaxSeedPerSource = 2

		fused := memsqlite.FuseAnchors([]memsqlite.AnchorHit{
			{NodeID: "fts_1", NodeType: core.NodeTypeFact, Source: "fts", Rank: 1},
			{NodeID: "fts_2", NodeType: core.NodeTypeFact, Source: "fts", Rank: 2},
			{NodeID: "fts_3", NodeType: core.NodeTypeFact, Source: "fts", Rank: 3},
			{NodeID: "mirror_1", NodeType: core.NodeTypeFact, Source: "mirror", Rank: 1},
		}, cfg)

		if got, want := anchorIDs(fused), []string{"fts_1", "mirror_1", "fts_2"}; !equalAnchorStrings(got, want) {
			t.Fatalf("fused order with per-source cap = %v, want %v", got, want)
		}
		if _, ok := fusedByID(fused)["fts_3"]; ok {
			t.Fatalf("fts_3 should be dropped by MaxSeedPerSource")
		}
	})

	t.Run("total", func(t *testing.T) {
		cfg := memsqlite.DefaultAnchorFusionConfig()
		cfg.MaxSeedTotal = 2
		cfg.MaxSeedPerSource = 10

		fused := memsqlite.FuseAnchors([]memsqlite.AnchorHit{
			{NodeID: "fact_1", NodeType: core.NodeTypeFact, Source: "fts", Rank: 1},
			{NodeID: "fact_2", NodeType: core.NodeTypeFact, Source: "fts", Rank: 2},
			{NodeID: "fact_3", NodeType: core.NodeTypeFact, Source: "fts", Rank: 3},
		}, cfg)

		if got, want := anchorIDs(fused), []string{"fact_1", "fact_2"}; !equalAnchorStrings(got, want) {
			t.Fatalf("fused order with total cap = %v, want %v", got, want)
		}
	})
}

func assertFloatNear(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("got %0.12f, want %0.12f", got, want)
	}
}

func anchorIDs(fused []memsqlite.FusedAnchor) []string {
	ids := make([]string, 0, len(fused))
	for _, anchor := range fused {
		ids = append(ids, anchor.NodeID)
	}
	return ids
}

func fusedByID(fused []memsqlite.FusedAnchor) map[string]memsqlite.FusedAnchor {
	byID := map[string]memsqlite.FusedAnchor{}
	for _, anchor := range fused {
		byID[anchor.NodeID] = anchor
	}
	return byID
}

func equalAnchorStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
