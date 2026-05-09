package sqlite

import (
	"context"
	"database/sql"

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
	if err := requireNodeExists(ctx, r.db, link.PersonaID, link.FromNodeType, link.FromNodeID); err != nil {
		return err
	}
	if err := requireNodeExists(ctx, r.db, link.PersonaID, link.ToNodeType, link.ToNodeID); err != nil {
		return err
	}

	_, err := r.db.ExecContext(ctx, `
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
	return err
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
