package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type LinkRepository struct {
	db *sql.DB
}

func NewLinkRepository(db *sql.DB) *LinkRepository {
	return &LinkRepository{db: db}
}

func (r *LinkRepository) Insert(ctx context.Context, link core.MemoryLink) error {
	link = normalizeLink(link)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = requireNodeExists(ctx, tx, link.PersonaID, link.FromNodeType, link.FromNodeID); err != nil {
		return err
	}
	if err = requireNodeExists(ctx, tx, link.PersonaID, link.ToNodeType, link.ToNodeID); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO memory_links (
    id, persona_id, from_node_type, from_node_id, link_type,
    to_node_type, to_node_id, direction, confidence, weight,
    reasoning, created_by, visibility_status, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		link.ID,
		link.PersonaID,
		string(link.FromNodeType),
		link.FromNodeID,
		string(link.LinkType),
		string(link.ToNodeType),
		link.ToNodeID,
		string(link.Direction),
		link.Confidence,
		link.Weight,
		nullableString(link.Reasoning),
		string(link.CreatedBy),
		string(link.VisibilityStatus),
		boolInt(link.Searchable),
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows > 0 {
		ok, eligibilityErr := mirrorEligibleLinkTx(ctx, tx, link.PersonaID, link.ID)
		if eligibilityErr != nil {
			return eligibilityErr
		}
		if ok {
			if err = enqueueLinkUpsertTx(ctx, tx, link.PersonaID, link.ID); err != nil {
				return err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (r *LinkRepository) ListFrom(ctx context.Context, personaID string, nodeType core.NodeType, nodeID string) ([]core.MemoryLink, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, persona_id, from_node_type, from_node_id, link_type,
       to_node_type, to_node_id, direction, confidence, weight,
       reasoning, created_by, visibility_status, searchable
FROM memory_links
WHERE persona_id = ? AND from_node_type = ? AND from_node_id = ?
ORDER BY created_at ASC`, personaID, string(nodeType), nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []core.MemoryLink
	for rows.Next() {
		link, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return links, nil
}

type linkScanner interface {
	Scan(dest ...any) error
}

func scanLink(row linkScanner) (core.MemoryLink, error) {
	var link core.MemoryLink
	var reasoning sql.NullString
	var searchable int
	if err := row.Scan(
		&link.ID,
		&link.PersonaID,
		&link.FromNodeType,
		&link.FromNodeID,
		&link.LinkType,
		&link.ToNodeType,
		&link.ToNodeID,
		&link.Direction,
		&link.Confidence,
		&link.Weight,
		&reasoning,
		&link.CreatedBy,
		&link.VisibilityStatus,
		&searchable,
	); err != nil {
		return core.MemoryLink{}, err
	}
	link.Reasoning = stringPtr(reasoning)
	link.Searchable = intBool(searchable)
	return link, nil
}

func normalizeLink(link core.MemoryLink) core.MemoryLink {
	if link.Direction == "" {
		link.Direction = core.LinkDirectionForward
	}
	if link.Confidence == 0 {
		link.Confidence = 1
	}
	if link.Weight == 0 {
		link.Weight = 1
	}
	if link.CreatedBy == "" {
		link.CreatedBy = core.LinkCreatedBySystem
	}
	if link.VisibilityStatus == "" {
		link.VisibilityStatus = core.VisibilityVisible
		link.Searchable = true
	}
	return link
}

func mirrorEligibleLinkTx(ctx context.Context, tx *sql.Tx, personaID string, linkID string) (bool, error) {
	var linkType string
	var fromNodeType, fromNodeID string
	var toNodeType, toNodeID string
	var visibility string
	var searchable int
	err := tx.QueryRowContext(ctx, `
SELECT link_type, from_node_type, from_node_id, to_node_type, to_node_id, visibility_status, searchable
FROM memory_links
WHERE persona_id = ? AND id = ?`, personaID, linkID).Scan(
		&linkType,
		&fromNodeType,
		&fromNodeID,
		&toNodeType,
		&toNodeID,
		&visibility,
		&searchable,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if visibility != string(core.VisibilityVisible) || searchable != 1 || !isMirrorEligibleLinkType(linkType) {
		return false, nil
	}
	fromOK, err := mirrorEligibleEndpointTx(ctx, tx, personaID, fromNodeType, fromNodeID)
	if err != nil || !fromOK {
		return false, err
	}
	toOK, err := mirrorEligibleEndpointTx(ctx, tx, personaID, toNodeType, toNodeID)
	if err != nil || !toOK {
		return false, err
	}
	return true, nil
}

func mirrorEligibleEndpointTx(ctx context.Context, tx *sql.Tx, personaID string, nodeType string, nodeID string) (bool, error) {
	switch core.NodeType(nodeType) {
	case core.NodeTypeEntity:
		return rowVisibleSearchableTx(ctx, tx, `
SELECT visibility_status, searchable
FROM entities
WHERE persona_id = ? AND id = ?`, personaID, nodeID)
	case core.NodeTypeFact:
		ok, err := rowVisibleSearchableTx(ctx, tx, `
SELECT visibility_status, searchable
FROM facts
WHERE persona_id = ? AND id = ?`, personaID, nodeID)
		if err != nil || !ok {
			return ok, err
		}
		return factSearchEvidenceEligible(ctx, tx, personaID, nodeID)
	case core.NodeTypeNarrative:
		return rowVisibleSearchableTx(ctx, tx, `
SELECT visibility_status, searchable
FROM narratives
WHERE persona_id = ? AND id = ?`, personaID, nodeID)
	case core.NodeTypeInsight:
		return rowVisibleSearchableTx(ctx, tx, `
SELECT visibility_status, searchable
FROM insights
WHERE persona_id = ? AND id = ?`, personaID, nodeID)
	default:
		return false, nil
	}
}

func rowVisibleSearchableTx(ctx context.Context, tx *sql.Tx, query string, personaID string, nodeID string) (bool, error) {
	var visibility string
	var searchable int
	err := tx.QueryRowContext(ctx, query, personaID, nodeID).Scan(&visibility, &searchable)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return visibility == string(core.VisibilityVisible) && searchable == 1, nil
}

func enqueueLinkUpsertTx(ctx context.Context, tx *sql.Tx, personaID string, linkID string) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO index_sync_queue (id, persona_id, node_type, node_id, operation)
VALUES (lower(hex(randomblob(16))), ?, 'memory_link', ?, 'upsert_edge')`,
		personaID,
		linkID,
	)
	return err
}
