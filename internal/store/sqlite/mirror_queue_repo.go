package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type MirrorQueueStatus string

const (
	MirrorQueueStatusPending    MirrorQueueStatus = "pending"
	MirrorQueueStatusProcessing MirrorQueueStatus = "processing"
	MirrorQueueStatusDone       MirrorQueueStatus = "done"
	MirrorQueueStatusFailed     MirrorQueueStatus = "failed"
)

type MirrorQueueOperation string

const (
	MirrorQueueOperationUpsertNode     MirrorQueueOperation = "upsert_node"
	MirrorQueueOperationDeleteNode     MirrorQueueOperation = "delete_node"
	MirrorQueueOperationUpsertEdge     MirrorQueueOperation = "upsert_edge"
	MirrorQueueOperationDeleteEdge     MirrorQueueOperation = "delete_edge"
	MirrorQueueOperationRebuildPersona MirrorQueueOperation = "rebuild_persona"
)

const (
	mirrorQueueLeaseDuration       = 15 * time.Minute
	mirrorQueueLeaseExpiredMessage = "mirror queue lease expired"
)

type MirrorQueueRow struct {
	ID           string
	PersonaID    string
	NodeType     string
	NodeID       string
	Operation    MirrorQueueOperation
	Priority     int
	PayloadJSON  string
	Status       MirrorQueueStatus
	Attempts     int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ErrorMessage string
}

type MirrorQueueRepository struct {
	db *sql.DB
}

func NewMirrorQueueRepository(db *sql.DB) *MirrorQueueRepository {
	return &MirrorQueueRepository{db: db}
}

func (r *MirrorQueueRepository) Claim(ctx context.Context, limit int) ([]MirrorQueueRow, error) {
	return r.claim(ctx, "", limit)
}

func (r *MirrorQueueRepository) ClaimForPersona(ctx context.Context, personaID string, limit int) ([]MirrorQueueRow, error) {
	return r.claim(ctx, strings.TrimSpace(personaID), limit)
}

func (r *MirrorQueueRepository) claim(ctx context.Context, personaID string, limit int) ([]MirrorQueueRow, error) {
	if limit <= 0 {
		return nil, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		rollbackUnlessCommitted(tx)
	}()

	now := time.Now().UTC()
	if err := expireStaleMirrorQueueLeases(ctx, tx, now, personaID); err != nil {
		return nil, err
	}

	query := `
SELECT id, persona_id, node_type, node_id, operation, priority, COALESCE(payload_json, ''),
       status, attempts, created_at, COALESCE(updated_at, ''), COALESCE(error_message, '')
FROM index_sync_queue
WHERE status IN (?, ?)`
	args := []any{
		string(MirrorQueueStatusPending),
		string(MirrorQueueStatusFailed),
	}
	if personaID != "" {
		query += ` AND persona_id = ?`
		args = append(args, personaID)
	}
	query += `
ORDER BY priority ASC, created_at ASC
LIMIT ?`
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	claimed := make([]MirrorQueueRow, 0, limit)
	for rows.Next() {
		row, err := scanMirrorQueueRow(rows)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	nowText := formatTime(now)
	for i := range claimed {
		result, err := tx.ExecContext(ctx, `
UPDATE index_sync_queue
SET status = ?,
    error_message = NULL,
    updated_at = ?
WHERE id = ?
  AND status IN (?, ?)`,
			string(MirrorQueueStatusProcessing),
			nowText,
			claimed[i].ID,
			string(MirrorQueueStatusPending),
			string(MirrorQueueStatusFailed),
		)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 0 {
			return nil, fmt.Errorf("claim queue row %s: %w", claimed[i].ID, sql.ErrNoRows)
		}
		claimed[i].Status = MirrorQueueStatusProcessing
		claimed[i].UpdatedAt = parseTime(nowText)
		claimed[i].ErrorMessage = ""
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return claimed, nil
}

func expireStaleMirrorQueueLeases(ctx context.Context, tx *sql.Tx, now time.Time, personaID string) error {
	query := `
UPDATE index_sync_queue
SET status = ?,
    attempts = attempts + 1,
    error_message = ?,
    updated_at = ?
WHERE status = ?
  AND datetime(replace(replace(COALESCE(updated_at, created_at), 'T', ' '), 'Z', '')) < datetime(replace(replace(?, 'T', ' '), 'Z', ''))`
	args := []any{
		string(MirrorQueueStatusFailed),
		mirrorQueueLeaseExpiredMessage,
		formatTime(now),
		string(MirrorQueueStatusProcessing),
		formatTime(now.Add(-mirrorQueueLeaseDuration)),
	}
	if personaID != "" {
		query += ` AND persona_id = ?`
		args = append(args, personaID)
	}
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func (r *MirrorQueueRepository) Complete(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE index_sync_queue
SET status = ?,
    error_message = NULL,
    updated_at = ?
WHERE id = ?`,
		string(MirrorQueueStatusDone),
		formatTime(time.Now().UTC()),
		id,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(result)
}

func (r *MirrorQueueRepository) Fail(ctx context.Context, id string, message string) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE index_sync_queue
SET status = ?,
    attempts = attempts + 1,
    error_message = ?,
    updated_at = ?
WHERE id = ?`,
		string(MirrorQueueStatusFailed),
		sanitizeMirrorQueueError(message),
		formatTime(time.Now().UTC()),
		id,
	)
	if err != nil {
		return err
	}
	return requireRowsAffected(result)
}

type mirrorQueueScanner interface {
	Scan(dest ...any) error
}

func scanMirrorQueueRow(row mirrorQueueScanner) (MirrorQueueRow, error) {
	var item MirrorQueueRow
	var createdAt, updatedAt string
	if err := row.Scan(
		&item.ID,
		&item.PersonaID,
		&item.NodeType,
		&item.NodeID,
		&item.Operation,
		&item.Priority,
		&item.PayloadJSON,
		&item.Status,
		&item.Attempts,
		&createdAt,
		&updatedAt,
		&item.ErrorMessage,
	); err != nil {
		return MirrorQueueRow{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func sanitizeMirrorQueueError(message string) string {
	cleaned := strings.Join(strings.Fields(message), " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "mirror queue operation failed"
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
	if status := extractMirrorErrorStatus(lower); status != "" {
		categories = append(categories, status)
	}
	if len(categories) == 0 {
		return "mirror queue operation failed"
	}
	return "mirror queue operation failed: " + strings.Join(categories, "; ")
}

func extractMirrorErrorStatus(message string) string {
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
	return ""
}

func requireRowsAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func rollbackUnlessCommitted(tx *sql.Tx) {
	if tx == nil {
		return
	}
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		return
	}
}
