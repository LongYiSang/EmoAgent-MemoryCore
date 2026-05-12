package sqlite

import (
	"context"
	"database/sql"
	"hash/fnv"
	"strconv"
	"strings"
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
	if _, err := r.db.ExecContext(ctx, `
DELETE FROM memory_index_map
WHERE persona_id = ?
  AND trivium_node_id = ?
  AND (node_type != ? OR node_id != ?)
  AND index_status != 'indexed'`,
		node.PersonaID,
		node.TriviumNodeID,
		node.NodeType,
		node.NodeID,
	); err != nil {
		return err
	}
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

func (r *MirrorIndexRepository) MarkPersonaDeleted(ctx context.Context, personaID string) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE memory_index_map
SET index_status = 'deleted',
    updated_at = ?,
    error_message = NULL
WHERE persona_id = ?
  AND index_status != 'deleted'`,
		formatTime(time.Now().UTC()),
		personaID,
	)
	return err
}

func (r *MirrorIndexRepository) MarkNodeFailed(ctx context.Context, personaID string, nodeType string, nodeID string, message string) error {
	id := ""
	if r.newID != nil {
		id = r.newID()
	}
	if id == "" {
		id = personaID + ":" + nodeType + ":" + nodeID + ":failed"
	}
	now := formatTime(time.Now().UTC())
	_, err := r.db.ExecContext(ctx, `
INSERT INTO memory_index_map (
    id, persona_id, node_type, node_id, trivium_node_id,
    index_status, updated_at, error_message
) VALUES (?, ?, ?, ?, ?, 'failed', ?, ?)
ON CONFLICT(persona_id, node_type, node_id) DO UPDATE SET
    index_status = 'failed',
    updated_at = excluded.updated_at,
    error_message = excluded.error_message`,
		id,
		personaID,
		nodeType,
		nodeID,
		failedMirrorID(personaID, nodeType, nodeID),
		now,
		sanitizeMirrorIndexError(message),
	)
	return err
}

func failedMirrorID(personaID string, nodeType string, nodeID string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(personaID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(nodeType))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(nodeID))
	value := int64(hash.Sum64() & (1<<63 - 1))
	if value == 0 {
		return -1
	}
	return -value
}

func sanitizeMirrorIndexError(message string) string {
	cleaned := strings.Join(strings.Fields(message), " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "mirror index operation failed"
	}

	lower := strings.ToLower(cleaned)
	categories := make([]string, 0, 3)
	switch {
	case strings.Contains(lower, "sidecar"):
		categories = append(categories, "sidecar unavailable")
	case strings.Contains(lower, "adapter"):
		categories = append(categories, "adapter error")
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") {
		categories = append(categories, "timeout")
	}
	if strings.Contains(lower, "schema") || strings.Contains(lower, "validation") {
		categories = append(categories, "schema validation")
	}
	if status := extractMirrorIndexErrorStatus(lower); status != "" {
		categories = append(categories, status)
	}
	if len(categories) == 0 {
		return "mirror index operation failed"
	}
	return "mirror index operation failed: " + strings.Join(categories, "; ")
}

func extractMirrorIndexErrorStatus(message string) string {
	for _, marker := range []string{"status ", "status=", "http ", "http=", "code ", "code="} {
		index := strings.Index(message, marker)
		if index < 0 {
			continue
		}
		start := index + len(marker)
		end := start
		for end < len(message) && message[end] >= '0' && message[end] <= '9' {
			end++
		}
		if end > start {
			return "status " + message[start:end]
		}
	}
	if code := firstThreeDigitCode(message); code != "" {
		return "status " + code
	}
	return ""
}

func firstThreeDigitCode(message string) string {
	for i := 0; i+3 <= len(message); i++ {
		part := message[i : i+3]
		if _, err := strconv.Atoi(part); err == nil && part[0] >= '1' && part[0] <= '5' {
			return part
		}
	}
	return ""
}
