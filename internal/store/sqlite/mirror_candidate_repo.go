package sqlite

import (
	"context"
	"database/sql"
	"math"
	"sort"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type MirrorCandidateRepository struct {
	db *sql.DB
}

type MirrorCandidate struct {
	TriviumNodeID  int64
	Score          float64
	Source         string
	PrimaryPurpose string
	Rank           int
	HitCount       int
}

type RetrievalMirrorCandidate struct {
	FactID         string
	TriviumNodeID  int64
	Score          float64
	Source         string
	PrimaryPurpose string
	Rank           int
	HitCount       int
}

type ActivationSeed struct {
	NodeID        string
	NodeType      core.NodeType
	SeedEnergy    float64
	TriviumNodeID int64
}

type ActivationCandidate struct {
	TriviumNodeID int64
	Score         float64
	Source        string
	Rank          int
	Paths         []GraphActivationPath
}

type MirrorCandidateDiagnostic struct {
	TriviumNodeID  int64
	SQLiteFactID   string
	Score          float64
	Source         string
	PrimaryPurpose string
	Rank           int
	HitCount       int
	DropReason     string
}

type MirrorCandidateMappingReport struct {
	Mapped                []RetrievalMirrorCandidate
	Diagnostics           []MirrorCandidateDiagnostic
	SidecarCandidateCount int
	MappedCandidateCount  int
	DroppedCandidateCount int
}

type ActivationCandidateMappingReport struct {
	Mapped                []RetrievalActivationCandidate
	Diagnostics           []GraphActivationCandidateDiagnostic
	SidecarCandidateCount int
	MappedCandidateCount  int
	DroppedCandidateCount int
}

func NewMirrorCandidateRepository(db *sql.DB) *MirrorCandidateRepository {
	return &MirrorCandidateRepository{db: db}
}

func (r *MirrorCandidateRepository) MapFactCandidates(ctx context.Context, personaID string, candidates []MirrorCandidate) ([]RetrievalMirrorCandidate, error) {
	report, err := r.MapFactCandidatesWithDiagnostics(ctx, personaID, candidates)
	if err != nil {
		return nil, err
	}
	return report.Mapped, nil
}

func (r *MirrorCandidateRepository) MapFactCandidatesWithDiagnostics(ctx context.Context, personaID string, candidates []MirrorCandidate) (MirrorCandidateMappingReport, error) {
	report := MirrorCandidateMappingReport{
		SidecarCandidateCount: len(candidates),
	}
	normalized := make([]MirrorCandidate, 0, len(candidates))
	for idx, candidate := range candidates {
		score, ok := normalizeMirrorCandidateScore(candidate.Score)
		if candidate.Rank <= 0 {
			candidate.Rank = idx + 1
		}
		if candidate.TriviumNodeID <= 0 || !ok {
			report.Diagnostics = append(report.Diagnostics, MirrorCandidateDiagnostic{
				TriviumNodeID:  candidate.TriviumNodeID,
				Score:          candidate.Score,
				Source:         candidate.Source,
				PrimaryPurpose: candidate.PrimaryPurpose,
				Rank:           candidate.Rank,
				HitCount:       candidate.HitCount,
				DropReason:     "invalid_candidate",
			})
			report.DroppedCandidateCount++
			continue
		}
		candidate.Score = score
		normalized = append(normalized, candidate)
	}
	if len(normalized) == 0 {
		return report, nil
	}

	placeholders := make([]string, 0, len(normalized))
	args := make([]any, 0, len(normalized)+1)
	args = append(args, personaID)
	for _, candidate := range normalized {
		placeholders = append(placeholders, "?")
		args = append(args, candidate.TriviumNodeID)
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT trivium_node_id, node_id, index_status
FROM memory_index_map
WHERE persona_id = ?
  AND node_type = 'fact'
  AND trivium_node_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return MirrorCandidateMappingReport{}, err
	}
	defer rows.Close()

	type mirrorMapRow struct {
		factID string
		status string
	}
	mappedIDs := map[int64]mirrorMapRow{}
	for rows.Next() {
		var triviumNodeID int64
		var factID string
		var status string
		if err := rows.Scan(&triviumNodeID, &factID, &status); err != nil {
			return MirrorCandidateMappingReport{}, err
		}
		mappedIDs[triviumNodeID] = mirrorMapRow{factID: factID, status: status}
	}
	if err := rows.Err(); err != nil {
		return MirrorCandidateMappingReport{}, err
	}

	best := map[string]RetrievalMirrorCandidate{}
	for _, candidate := range normalized {
		row, ok := mappedIDs[candidate.TriviumNodeID]
		if !ok {
			report.Diagnostics = append(report.Diagnostics, MirrorCandidateDiagnostic{
				TriviumNodeID:  candidate.TriviumNodeID,
				Score:          candidate.Score,
				Source:         candidate.Source,
				PrimaryPurpose: candidate.PrimaryPurpose,
				Rank:           candidate.Rank,
				HitCount:       candidate.HitCount,
				DropReason:     "unmapped_trivium_node",
			})
			report.DroppedCandidateCount++
			continue
		}
		if row.status != "indexed" {
			report.Diagnostics = append(report.Diagnostics, MirrorCandidateDiagnostic{
				TriviumNodeID:  candidate.TriviumNodeID,
				SQLiteFactID:   row.factID,
				Score:          candidate.Score,
				Source:         candidate.Source,
				PrimaryPurpose: candidate.PrimaryPurpose,
				Rank:           candidate.Rank,
				HitCount:       candidate.HitCount,
				DropReason:     "stale_mapping_status_" + row.status,
			})
			report.DroppedCandidateCount++
			continue
		}
		report.Diagnostics = append(report.Diagnostics, MirrorCandidateDiagnostic{
			TriviumNodeID:  candidate.TriviumNodeID,
			SQLiteFactID:   row.factID,
			Score:          candidate.Score,
			Source:         candidate.Source,
			PrimaryPurpose: candidate.PrimaryPurpose,
			Rank:           candidate.Rank,
			HitCount:       candidate.HitCount,
		})
		existing, ok := best[row.factID]
		if !ok || candidate.Rank < existing.Rank || (candidate.Rank == existing.Rank && candidate.Score > existing.Score) {
			best[row.factID] = RetrievalMirrorCandidate{
				FactID:         row.factID,
				TriviumNodeID:  candidate.TriviumNodeID,
				Score:          candidate.Score,
				Source:         candidate.Source,
				PrimaryPurpose: candidate.PrimaryPurpose,
				Rank:           candidate.Rank,
				HitCount:       candidate.HitCount,
			}
		}
	}
	report.MappedCandidateCount = len(best)
	result := make([]RetrievalMirrorCandidate, 0, len(best))
	for _, candidate := range best {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Rank == result[j].Rank {
			return result[i].FactID < result[j].FactID
		}
		return result[i].Rank < result[j].Rank
	})
	report.Mapped = result
	return report, nil
}

func (r *MirrorCandidateRepository) MapActivationSeeds(ctx context.Context, personaID string, anchors []FusedAnchor) ([]ActivationSeed, error) {
	seeds := make([]ActivationSeed, 0, len(anchors))
	seen := map[string]struct{}{}
	for _, anchor := range anchors {
		if anchor.NodeID == "" || anchor.NodeType == "" || anchor.SeedEnergy <= 0 {
			continue
		}
		key := string(anchor.NodeType) + "\x00" + anchor.NodeID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		var triviumNodeID int64
		var status string
		err := r.db.QueryRowContext(ctx, `
SELECT trivium_node_id, index_status
FROM memory_index_map
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?
  AND trivium_node_id > 0
ORDER BY indexed_at DESC
LIMIT 1`, personaID, string(anchor.NodeType), anchor.NodeID).Scan(&triviumNodeID, &status)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if status != "indexed" {
			continue
		}
		seeds = append(seeds, ActivationSeed{
			NodeID:        anchor.NodeID,
			NodeType:      anchor.NodeType,
			SeedEnergy:    anchor.SeedEnergy,
			TriviumNodeID: triviumNodeID,
		})
	}
	return seeds, nil
}

func (r *MirrorCandidateRepository) MapActivationCandidatesWithDiagnostics(ctx context.Context, personaID string, candidates []ActivationCandidate) (ActivationCandidateMappingReport, error) {
	report := ActivationCandidateMappingReport{
		SidecarCandidateCount: len(candidates),
	}
	normalized := make([]ActivationCandidate, 0, len(candidates))
	for idx, candidate := range candidates {
		score, ok := normalizeMirrorCandidateScore(candidate.Score)
		if candidate.Rank <= 0 {
			candidate.Rank = idx + 1
		}
		if candidate.TriviumNodeID <= 0 || !ok {
			report.Diagnostics = append(report.Diagnostics, GraphActivationCandidateDiagnostic{
				TriviumNodeID: candidate.TriviumNodeID,
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
				Paths:         cloneGraphActivationPaths(candidate.Paths),
				DropReason:    "invalid_candidate",
			})
			report.DroppedCandidateCount++
			continue
		}
		candidate.Score = score
		normalized = append(normalized, candidate)
	}
	if len(normalized) == 0 {
		return report, nil
	}

	placeholders := make([]string, 0, len(normalized))
	args := make([]any, 0, len(normalized)+1)
	args = append(args, personaID)
	for _, candidate := range normalized {
		placeholders = append(placeholders, "?")
		args = append(args, candidate.TriviumNodeID)
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT trivium_node_id, node_type, node_id, index_status
FROM memory_index_map
WHERE persona_id = ?
  AND trivium_node_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return ActivationCandidateMappingReport{}, err
	}
	defer rows.Close()

	type mirrorMapRow struct {
		nodeType core.NodeType
		nodeID   string
		status   string
	}
	mappedIDs := map[int64]mirrorMapRow{}
	for rows.Next() {
		var triviumNodeID int64
		var nodeType string
		var nodeID string
		var status string
		if err := rows.Scan(&triviumNodeID, &nodeType, &nodeID, &status); err != nil {
			return ActivationCandidateMappingReport{}, err
		}
		row := mirrorMapRow{nodeType: core.NodeType(nodeType), nodeID: nodeID, status: status}
		existing, ok := mappedIDs[triviumNodeID]
		if !ok || activationMapRowPriority(row) > activationMapRowPriority(existing) {
			mappedIDs[triviumNodeID] = row
		}
	}
	if err := rows.Err(); err != nil {
		return ActivationCandidateMappingReport{}, err
	}

	best := map[string]RetrievalActivationCandidate{}
	for _, candidate := range normalized {
		row, ok := mappedIDs[candidate.TriviumNodeID]
		if !ok {
			report.Diagnostics = append(report.Diagnostics, GraphActivationCandidateDiagnostic{
				TriviumNodeID: candidate.TriviumNodeID,
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
				Paths:         cloneGraphActivationPaths(candidate.Paths),
				DropReason:    "unmapped_trivium_node",
			})
			report.DroppedCandidateCount++
			continue
		}
		if row.status != "indexed" {
			report.Diagnostics = append(report.Diagnostics, GraphActivationCandidateDiagnostic{
				TriviumNodeID: candidate.TriviumNodeID,
				SQLiteNodeID:  row.nodeID,
				NodeType:      string(row.nodeType),
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
				Paths:         cloneGraphActivationPaths(candidate.Paths),
				DropReason:    "stale_mapping_status_" + row.status,
			})
			report.DroppedCandidateCount++
			continue
		}
		if row.nodeType != core.NodeTypeFact {
			report.Diagnostics = append(report.Diagnostics, GraphActivationCandidateDiagnostic{
				TriviumNodeID: candidate.TriviumNodeID,
				SQLiteNodeID:  row.nodeID,
				NodeType:      string(row.nodeType),
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
				Paths:         cloneGraphActivationPaths(candidate.Paths),
				DropReason:    "non_fact_candidate",
			})
			report.DroppedCandidateCount++
			continue
		}
		report.Diagnostics = append(report.Diagnostics, GraphActivationCandidateDiagnostic{
			TriviumNodeID: candidate.TriviumNodeID,
			SQLiteNodeID:  row.nodeID,
			NodeType:      string(row.nodeType),
			Score:         candidate.Score,
			Source:        candidate.Source,
			Rank:          candidate.Rank,
			Paths:         cloneGraphActivationPaths(candidate.Paths),
		})
		existing, ok := best[row.nodeID]
		if !ok || candidate.Rank < existing.Rank || (candidate.Rank == existing.Rank && candidate.Score > existing.Score) {
			best[row.nodeID] = RetrievalActivationCandidate{
				FactID:        row.nodeID,
				TriviumNodeID: candidate.TriviumNodeID,
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
				Paths:         cloneGraphActivationPaths(candidate.Paths),
			}
		}
	}
	report.MappedCandidateCount = len(best)
	result := make([]RetrievalActivationCandidate, 0, len(best))
	for _, candidate := range best {
		result = append(result, candidate)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Rank == result[j].Rank {
			return result[i].FactID < result[j].FactID
		}
		return result[i].Rank < result[j].Rank
	})
	report.Mapped = result
	return report, nil
}

func activationMapRowPriority(row struct {
	nodeType core.NodeType
	nodeID   string
	status   string
}) int {
	if row.status == "indexed" && row.nodeType == core.NodeTypeFact {
		return 4
	}
	if row.status == "indexed" {
		return 3
	}
	if row.nodeType == core.NodeTypeFact {
		return 2
	}
	return 1
}

func normalizeMirrorCandidateScore(score float64) (float64, bool) {
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
		return 0, false
	}
	if score > 1 {
		return 1, true
	}
	return score, true
}
