package sqlite

import (
	"context"
	"database/sql"
	"strings"
)

type MirrorCandidateRepository struct {
	db *sql.DB
}

type MirrorCandidate struct {
	TriviumNodeID int64
	Score         float64
	Source        string
}

type RetrievalMirrorCandidate struct {
	FactID string
	Score  float64
	Source string
}

func NewMirrorCandidateRepository(db *sql.DB) *MirrorCandidateRepository {
	return &MirrorCandidateRepository{db: db}
}

func (r *MirrorCandidateRepository) MapFactCandidates(ctx context.Context, personaID string, candidates []MirrorCandidate) ([]RetrievalMirrorCandidate, error) {
	normalized := make([]MirrorCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		score, ok := normalizeMirrorCandidateScore(candidate.Score)
		if candidate.TriviumNodeID <= 0 || !ok {
			continue
		}
		candidate.Score = score
		normalized = append(normalized, candidate)
	}
	if len(normalized) == 0 {
		return nil, nil
	}

	placeholders := make([]string, 0, len(normalized))
	args := make([]any, 0, len(normalized)+1)
	args = append(args, personaID)
	for _, candidate := range normalized {
		placeholders = append(placeholders, "?")
		args = append(args, candidate.TriviumNodeID)
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT trivium_node_id, node_id
FROM memory_index_map
WHERE persona_id = ?
  AND node_type = 'fact'
  AND index_status = 'indexed'
  AND trivium_node_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	mappedIDs := map[int64]string{}
	for rows.Next() {
		var triviumNodeID int64
		var factID string
		if err := rows.Scan(&triviumNodeID, &factID); err != nil {
			return nil, err
		}
		mappedIDs[triviumNodeID] = factID
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	best := map[string]RetrievalMirrorCandidate{}
	for _, candidate := range normalized {
		factID := mappedIDs[candidate.TriviumNodeID]
		if factID == "" {
			continue
		}
		existing, ok := best[factID]
		if !ok || candidate.Score > existing.Score {
			best[factID] = RetrievalMirrorCandidate{
				FactID: factID,
				Score:  candidate.Score,
				Source: candidate.Source,
			}
		}
	}
	result := make([]RetrievalMirrorCandidate, 0, len(best))
	for _, candidate := range best {
		result = append(result, candidate)
	}
	return result, nil
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
