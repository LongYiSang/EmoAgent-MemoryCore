package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"strings"
)

type MirrorCandidateRepository struct {
	db *sql.DB
}

type MirrorCandidate struct {
	TriviumNodeID int64
	Score         float64
	Source        string
	Rank          int
}

type RetrievalMirrorCandidate struct {
	FactID        string
	TriviumNodeID int64
	Score         float64
	Source        string
	Rank          int
}

type MirrorCandidateDiagnostic struct {
	TriviumNodeID int64
	SQLiteFactID  string
	Score         float64
	Source        string
	Rank          int
	DropReason    string
}

type MirrorCandidateMappingReport struct {
	Mapped                []RetrievalMirrorCandidate
	Diagnostics           []MirrorCandidateDiagnostic
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
				TriviumNodeID: candidate.TriviumNodeID,
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
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
				TriviumNodeID: candidate.TriviumNodeID,
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
				DropReason:    "unmapped_trivium_node",
			})
			report.DroppedCandidateCount++
			continue
		}
		if row.status != "indexed" {
			report.Diagnostics = append(report.Diagnostics, MirrorCandidateDiagnostic{
				TriviumNodeID: candidate.TriviumNodeID,
				SQLiteFactID:  row.factID,
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
				DropReason:    "stale_mapping_status_" + row.status,
			})
			report.DroppedCandidateCount++
			continue
		}
		report.Diagnostics = append(report.Diagnostics, MirrorCandidateDiagnostic{
			TriviumNodeID: candidate.TriviumNodeID,
			SQLiteFactID:  row.factID,
			Score:         candidate.Score,
			Source:        candidate.Source,
			Rank:          candidate.Rank,
		})
		existing, ok := best[row.factID]
		if !ok || candidate.Rank < existing.Rank || (candidate.Rank == existing.Rank && candidate.Score > existing.Score) {
			best[row.factID] = RetrievalMirrorCandidate{
				FactID:        row.factID,
				TriviumNodeID: candidate.TriviumNodeID,
				Score:         candidate.Score,
				Source:        candidate.Source,
				Rank:          candidate.Rank,
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

func normalizeMirrorCandidateScore(score float64) (float64, bool) {
	if score < 0 {
		return 0, false
	}
	if score > 1 {
		return 1, true
	}
	return score, true
}
