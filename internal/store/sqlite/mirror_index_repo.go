package sqlite

import (
	"context"
	"database/sql"
	"time"
)

type MirrorIndexRepository struct {
	db    *sql.DB
	newID func() string
}

type MirrorIndexedNode struct {
	PersonaID     string
	NodeType      string
	NodeID        string
	TriviumNodeID int64
}

func NewMirrorIndexRepository(db *sql.DB, newID func() string) *MirrorIndexRepository {
	return &MirrorIndexRepository{db: db, newID: newID}
}

func (r *MirrorIndexRepository) RecordNodeIndexed(ctx context.Context, node MirrorIndexedNode) error {
	id := ""
	if r.newID != nil {
		id = r.newID()
	}
	if id == "" {
		id = node.PersonaID + ":" + node.NodeType + ":" + node.NodeID
	}
	now := formatTime(time.Now().UTC())
	_, err := r.db.ExecContext(ctx, `
INSERT INTO memory_index_map (
    id, persona_id, node_type, node_id, trivium_node_id,
    index_status, indexed_at, updated_at, error_message
) VALUES (?, ?, ?, ?, ?, 'indexed', ?, ?, NULL)
ON CONFLICT(persona_id, node_type, node_id) DO UPDATE SET
    trivium_node_id = excluded.trivium_node_id,
    index_status = 'indexed',
    indexed_at = excluded.indexed_at,
    updated_at = excluded.updated_at,
    error_message = NULL`,
		id,
		node.PersonaID,
		node.NodeType,
		node.NodeID,
		node.TriviumNodeID,
		now,
		now,
	)
	return err
}

func (r *MirrorIndexRepository) MarkNodeDeleted(ctx context.Context, personaID string, nodeType string, nodeID string) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE memory_index_map
SET index_status = 'deleted',
    updated_at = ?,
    error_message = NULL
WHERE persona_id = ?
  AND node_type = ?
  AND node_id = ?`,
		formatTime(time.Now().UTC()),
		personaID,
		nodeType,
		nodeID,
	)
	return err
}
