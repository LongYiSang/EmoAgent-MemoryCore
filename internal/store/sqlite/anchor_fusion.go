package sqlite

import (
	"sort"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const (
	AnchorSourceAliasMatch       = "alias_match"
	AnchorSourceEntityExact      = "entity_exact"
	AnchorSourceSQLiteFTS        = "sqlite_fts"
	AnchorSourceSQLiteSparse     = "sqlite_sparse"
	AnchorSourceTriviumDense     = "trivium_dense"
	AnchorSourcePinnedCore       = "pinned_core"
	AnchorSourceRecentImportant  = "recent_important"
	AnchorSourceNarrativeInsight = "narrative_insight"
)

type AnchorFusionConfig struct {
	SourceWeights    map[string]float64
	RankConstant     float64
	MaxSeedTotal     int
	MaxSeedPerSource int
	NodeTypePriors   map[core.NodeType]float64
}

type AnchorHit struct {
	NodeID      string
	NodeType    core.NodeType
	Source      string
	Rank        int
	RawScore    float64
	DebugReason string
}

type FusedAnchor struct {
	NodeID           string
	NodeType         core.NodeType
	FusedAnchorScore float64
	SeedEnergy       float64
	SourceBreakdown  []AnchorSourceBreakdown
}

type AnchorSourceBreakdown struct {
	Source          string
	Rank            int
	RawScore        float64
	Weight          float64
	RRFContribution float64
	DebugReason     string
}

type AnchorFusionDiagnostics struct {
	Seeds []FusedAnchor
}

func DefaultAnchorFusionConfig() AnchorFusionConfig {
	return AnchorFusionConfig{
		RankConstant:     60,
		MaxSeedTotal:     80,
		MaxSeedPerSource: 40,
		SourceWeights: map[string]float64{
			AnchorSourceEntityExact:      2.0,
			AnchorSourceAliasMatch:       1.6,
			AnchorSourceSQLiteFTS:        1.0,
			AnchorSourceSQLiteSparse:     1.0,
			AnchorSourceTriviumDense:     1.0,
			AnchorSourcePinnedCore:       1.8,
			AnchorSourceRecentImportant:  1.2,
			AnchorSourceNarrativeInsight: 1.1,
		},
		NodeTypePriors: map[core.NodeType]float64{
			core.NodeTypeEntity:    0.7,
			core.NodeTypeFact:      1.0,
			core.NodeTypeNarrative: 0.9,
			core.NodeTypeInsight:   1.1,
			core.NodeTypeEpisode:   0.4,
		},
	}
}

func FuseAnchors(hits []AnchorHit, cfg AnchorFusionConfig) []FusedAnchor {
	cfg = normalizeAnchorFusionConfig(cfg)
	hits = applyAnchorSourceCaps(hits, cfg)

	type anchorKey struct {
		nodeType core.NodeType
		nodeID   string
	}
	byAnchor := map[anchorKey]*FusedAnchor{}
	for _, hit := range hits {
		hit.Source = strings.TrimSpace(hit.Source)
		hit.NodeID = strings.TrimSpace(hit.NodeID)
		if hit.NodeID == "" || hit.NodeType == "" || hit.Source == "" {
			continue
		}
		rank := hit.Rank
		if rank <= 0 {
			rank = 1
		}
		weight := cfg.SourceWeights[hit.Source]
		if weight <= 0 {
			weight = 1
		}
		contribution := weight / (cfg.RankConstant + float64(rank))
		key := anchorKey{nodeType: hit.NodeType, nodeID: hit.NodeID}
		anchor := byAnchor[key]
		if anchor == nil {
			anchor = &FusedAnchor{
				NodeID:   hit.NodeID,
				NodeType: hit.NodeType,
			}
			byAnchor[key] = anchor
		}
		anchor.FusedAnchorScore += contribution
		anchor.SourceBreakdown = append(anchor.SourceBreakdown, AnchorSourceBreakdown{
			Source:          hit.Source,
			Rank:            rank,
			RawScore:        hit.RawScore,
			Weight:          weight,
			RRFContribution: contribution,
			DebugReason:     sanitizeAnchorDebugReason(hit.DebugReason),
		})
	}

	fused := make([]FusedAnchor, 0, len(byAnchor))
	var maxScore float64
	for _, anchor := range byAnchor {
		sort.Slice(anchor.SourceBreakdown, func(i, j int) bool {
			left := anchor.SourceBreakdown[i]
			right := anchor.SourceBreakdown[j]
			if left.RRFContribution == right.RRFContribution {
				if left.Source == right.Source {
					return left.Rank < right.Rank
				}
				return left.Source < right.Source
			}
			return left.RRFContribution > right.RRFContribution
		})
		if anchor.FusedAnchorScore > maxScore {
			maxScore = anchor.FusedAnchorScore
		}
		fused = append(fused, *anchor)
	}
	if maxScore > 0 {
		for i := range fused {
			prior := cfg.NodeTypePriors[fused[i].NodeType]
			if prior <= 0 {
				prior = 1
			}
			fused[i].SeedEnergy = (fused[i].FusedAnchorScore / maxScore) * prior
		}
	}
	sortFusedAnchors(fused)
	if cfg.MaxSeedTotal > 0 && len(fused) > cfg.MaxSeedTotal {
		fused = fused[:cfg.MaxSeedTotal]
	}
	return fused
}

func normalizeAnchorFusionConfig(cfg AnchorFusionConfig) AnchorFusionConfig {
	defaults := DefaultAnchorFusionConfig()
	if cfg.RankConstant <= 0 {
		cfg.RankConstant = defaults.RankConstant
	}
	if cfg.MaxSeedTotal <= 0 {
		cfg.MaxSeedTotal = defaults.MaxSeedTotal
	}
	if cfg.MaxSeedPerSource <= 0 {
		cfg.MaxSeedPerSource = defaults.MaxSeedPerSource
	}
	if len(cfg.SourceWeights) == 0 {
		cfg.SourceWeights = defaults.SourceWeights
	}
	if len(cfg.NodeTypePriors) == 0 {
		cfg.NodeTypePriors = defaults.NodeTypePriors
	}
	return cfg
}

func applyAnchorSourceCaps(hits []AnchorHit, cfg AnchorFusionConfig) []AnchorHit {
	if cfg.MaxSeedPerSource <= 0 {
		return hits
	}
	countBySource := map[string]int{}
	capped := make([]AnchorHit, 0, len(hits))
	for _, hit := range hits {
		source := strings.TrimSpace(hit.Source)
		if source == "" {
			continue
		}
		if countBySource[source] >= cfg.MaxSeedPerSource {
			continue
		}
		countBySource[source]++
		capped = append(capped, hit)
	}
	return capped
}

func sortFusedAnchors(fused []FusedAnchor) {
	sort.Slice(fused, func(i, j int) bool {
		if fused[i].FusedAnchorScore == fused[j].FusedAnchorScore {
			if fused[i].NodeType == fused[j].NodeType {
				return fused[i].NodeID < fused[j].NodeID
			}
			return fused[i].NodeType < fused[j].NodeType
		}
		return fused[i].FusedAnchorScore > fused[j].FusedAnchorScore
	})
}

func sanitizeAnchorDebugReason(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 160 {
		value = value[:160]
	}
	return value
}
