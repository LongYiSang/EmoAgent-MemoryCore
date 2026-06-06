package memorycore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const pendingManualForgetOperationTTL = 24 * time.Hour

type pendingManualForgetPolicy struct {
	RequestID      string               `json:"request_id,omitempty"`
	Actor          string               `json:"actor,omitempty"`
	ReasonCode     string               `json:"reason_code,omitempty"`
	PreviewRequest ForgetPreviewRequest `json:"preview_request"`
	RiskFlags      []string             `json:"risk_flags,omitempty"`
	SidecarStatus  string               `json:"sidecar_status,omitempty"`
}

func (s *service) recordPendingManualForgetOperation(ctx context.Context, req ForgetPreviewRequest, result *ForgetPreviewResult) error {
	if s == nil || s.sqlDB == nil || result == nil {
		return nil
	}
	chatSessionID := strings.TrimSpace(req.ChatSessionID)
	if !req.RequireConfirmation || chatSessionID == "" || len(result.Targets) == 0 || strings.TrimSpace(result.PreviewHash) == "" {
		return nil
	}
	personaID := defaultString(result.PersonaID, req.PersonaID)
	personaID = defaultString(personaID, s.persona)
	level := defaultString(result.RequestedLevel, req.RequestedLevel)
	if strings.TrimSpace(level) == "" {
		level = ForgetLevelSoft
	}
	scopeMode := defaultString(result.ScopeMode, req.ScopeMode)
	operationID := "manual_forget_operation_" + uuid.NewString()
	now := s.now()
	expiresAt := now.Add(pendingManualForgetOperationTTL)
	targetsJSON, err := json.Marshal(result.Targets)
	if err != nil {
		return fmt.Errorf("marshal pending manual forget targets: %w", err)
	}
	policyJSON, err := json.Marshal(pendingManualForgetPolicy{
		RequestID:      result.RequestID,
		Actor:          defaultString(req.Actor, ForgetActorUser),
		ReasonCode:     ForgetReasonUserRequested,
		PreviewRequest: req,
		RiskFlags:      append([]string(nil), result.RiskFlags...),
		SidecarStatus:  result.SidecarStatus,
	})
	if err != nil {
		return fmt.Errorf("marshal pending manual forget policy: %w", err)
	}
	if _, err := s.sqlDB.ExecContext(ctx, `
UPDATE pending_manual_forget_operations
SET status = ?, updated_at = ?
WHERE persona_id = ?
  AND chat_session_id = ?
  AND status = ?`,
		ManualForgetOperationStatusCancelled,
		formatManualForgetOperationTime(now),
		personaID,
		chatSessionID,
		ManualForgetOperationStatusPendingConfirmation,
	); err != nil {
		return fmt.Errorf("cancel previous pending manual forget operation: %w", err)
	}
	if _, err := s.sqlDB.ExecContext(ctx, `
INSERT INTO pending_manual_forget_operations (
    id, persona_id, session_id, chat_session_id, request_episode_id, status,
    requested_level, scope_mode, requires_confirmation, candidates_json,
    confirmation_policy_json, preview_hash, created_at, updated_at, expires_at
)
VALUES (?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		operationID,
		personaID,
		strings.TrimSpace(req.SessionID),
		chatSessionID,
		strings.TrimSpace(req.RequestEpisodeID),
		ManualForgetOperationStatusPendingConfirmation,
		level,
		scopeMode,
		boolToInt(result.RequiresConfirmation),
		string(targetsJSON),
		string(policyJSON),
		result.PreviewHash,
		formatManualForgetOperationTime(now),
		formatManualForgetOperationTime(now),
		formatManualForgetOperationTime(expiresAt),
	); err != nil {
		return fmt.Errorf("insert pending manual forget operation: %w", err)
	}
	result.OperationID = operationID
	return nil
}

func (s *service) GetPendingManualForgetOperation(ctx context.Context, req GetPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error) {
	if s == nil || s.sqlDB == nil {
		return nil, fmt.Errorf("memory service is not configured")
	}
	personaID := defaultString(req.PersonaID, s.persona)
	chatSessionID := strings.TrimSpace(req.ChatSessionID)
	if chatSessionID == "" {
		return nil, fmt.Errorf("%w: chat session id is required", ErrInvalidRequest)
	}
	op, err := s.pendingManualForgetOperationByChat(ctx, personaID, chatSessionID)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, nil
	}
	return s.expirePendingManualForgetOperationIfNeeded(ctx, op)
}

func (s *service) CancelPendingManualForgetOperation(ctx context.Context, req CancelPendingManualForgetOperationRequest) (*PendingManualForgetOperation, error) {
	if s == nil || s.sqlDB == nil {
		return nil, fmt.Errorf("memory service is not configured")
	}
	personaID := defaultString(req.PersonaID, s.persona)
	operationID := strings.TrimSpace(req.OperationID)
	if operationID == "" {
		return nil, fmt.Errorf("%w: operation id is required", ErrInvalidRequest)
	}
	op, err := s.pendingManualForgetOperationByID(ctx, personaID, operationID)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, nil
	}
	if chatSessionID := strings.TrimSpace(req.ChatSessionID); chatSessionID != "" && chatSessionID != strings.TrimSpace(op.ChatSessionID) {
		return nil, fmt.Errorf("%w: chat_session_mismatch", ErrInvalidRequest)
	}
	op, err = s.expirePendingManualForgetOperationIfNeeded(ctx, op)
	if err != nil || op == nil || op.Status != ManualForgetOperationStatusPendingConfirmation {
		return op, err
	}
	return s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusCancelled)
}

func (s *service) pendingManualForgetOperationByChat(ctx context.Context, personaID string, chatSessionID string) (*PendingManualForgetOperation, error) {
	row := s.sqlDB.QueryRowContext(ctx, `
SELECT id, persona_id, session_id, chat_session_id, request_episode_id, status,
       requested_level, scope_mode, requires_confirmation, candidates_json,
       preview_hash, created_at, updated_at, expires_at
FROM pending_manual_forget_operations
WHERE persona_id = ?
  AND chat_session_id = ?
  AND status = ?
ORDER BY updated_at DESC, created_at DESC
LIMIT 1`,
		personaID,
		chatSessionID,
		ManualForgetOperationStatusPendingConfirmation,
	)
	return scanPendingManualForgetOperation(row)
}

func (s *service) pendingManualForgetOperationByID(ctx context.Context, personaID string, operationID string) (*PendingManualForgetOperation, error) {
	row := s.sqlDB.QueryRowContext(ctx, `
SELECT id, persona_id, session_id, chat_session_id, request_episode_id, status,
       requested_level, scope_mode, requires_confirmation, candidates_json,
       preview_hash, created_at, updated_at, expires_at
FROM pending_manual_forget_operations
WHERE persona_id = ?
  AND id = ?
LIMIT 1`,
		personaID,
		operationID,
	)
	return scanPendingManualForgetOperation(row)
}

type pendingManualForgetScanner interface {
	Scan(dest ...any) error
}

func scanPendingManualForgetOperation(row pendingManualForgetScanner) (*PendingManualForgetOperation, error) {
	var op PendingManualForgetOperation
	var sessionID, chatSessionID, requestEpisodeID, previewHash sql.NullString
	var candidatesJSON string
	var requiresConfirmation int
	var createdAt, updatedAt, expiresAt string
	if err := row.Scan(
		&op.OperationID,
		&op.PersonaID,
		&sessionID,
		&chatSessionID,
		&requestEpisodeID,
		&op.Status,
		&op.RequestedLevel,
		&op.ScopeMode,
		&requiresConfirmation,
		&candidatesJSON,
		&previewHash,
		&createdAt,
		&updatedAt,
		&expiresAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("scan pending manual forget operation: %w", err)
	}
	op.SessionID = manualForgetNullStringValue(sessionID)
	op.ChatSessionID = manualForgetNullStringValue(chatSessionID)
	op.RequestEpisodeID = manualForgetNullStringValue(requestEpisodeID)
	op.PreviewHash = manualForgetNullStringValue(previewHash)
	op.RequiresConfirmation = requiresConfirmation != 0
	if strings.TrimSpace(candidatesJSON) != "" {
		if err := json.Unmarshal([]byte(candidatesJSON), &op.Targets); err != nil {
			return nil, fmt.Errorf("decode pending manual forget targets: %w", err)
		}
	}
	op.CreatedAt = parseManualForgetOperationTime(createdAt)
	op.UpdatedAt = parseManualForgetOperationTime(updatedAt)
	op.ExpiresAt = parseManualForgetOperationTime(expiresAt)
	return &op, nil
}

func (s *service) expirePendingManualForgetOperationIfNeeded(ctx context.Context, op *PendingManualForgetOperation) (*PendingManualForgetOperation, error) {
	if op == nil || op.Status != ManualForgetOperationStatusPendingConfirmation || op.ExpiresAt.IsZero() || s.now().Before(op.ExpiresAt) {
		return op, nil
	}
	return s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusExpired)
}

func (s *service) updatePendingManualForgetOperationStatus(ctx context.Context, operationID string, status string) (*PendingManualForgetOperation, error) {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return nil, fmt.Errorf("%w: operation id is required", ErrInvalidRequest)
	}
	now := s.now()
	if _, err := s.sqlDB.ExecContext(ctx, `
UPDATE pending_manual_forget_operations
SET status = ?, updated_at = ?
WHERE id = ?`,
		status,
		formatManualForgetOperationTime(now),
		operationID,
	); err != nil {
		return nil, fmt.Errorf("update pending manual forget operation status: %w", err)
	}
	row := s.sqlDB.QueryRowContext(ctx, `
SELECT id, persona_id, session_id, chat_session_id, request_episode_id, status,
       requested_level, scope_mode, requires_confirmation, candidates_json,
       preview_hash, created_at, updated_at, expires_at
FROM pending_manual_forget_operations
WHERE id = ?
LIMIT 1`, operationID)
	return scanPendingManualForgetOperation(row)
}

func (s *service) failPendingManualForgetOperation(ctx context.Context, operationID string) {
	operationID = strings.TrimSpace(operationID)
	if s == nil || s.sqlDB == nil || operationID == "" {
		return
	}
	_, _ = s.updatePendingManualForgetOperationStatus(ctx, operationID, ManualForgetOperationStatusFailed)
}

func (s *service) executePendingManualForgetOperation(ctx context.Context, req ForgetExecuteRequest) (*ForgetExecuteResult, error) {
	operationID := strings.TrimSpace(req.OperationID)
	if operationID == "" {
		return nil, fmt.Errorf("%w: operation id is required", ErrInvalidRequest)
	}
	personaID := defaultString(req.PersonaID, s.persona)
	op, err := s.pendingManualForgetOperationByID(ctx, personaID, operationID)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, fmt.Errorf("%w: pending manual forget operation not found", ErrInvalidRequest)
	}
	if op.Status == ManualForgetOperationStatusPendingConfirmation && !op.ExpiresAt.IsZero() && !s.now().Before(op.ExpiresAt) {
		op, err = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusExpired)
		if err != nil {
			return nil, err
		}
	}
	if op.Status != ManualForgetOperationStatusPendingConfirmation {
		return nil, fmt.Errorf("%w: pending manual forget operation is %s", ErrInvalidRequest, op.Status)
	}
	expectedHash := strings.TrimSpace(req.PreviewHash)
	if expectedHash == "" {
		expectedHash = strings.TrimSpace(req.Preview.PreviewHash)
	}
	if expectedHash == "" {
		_, _ = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusFailed)
		return nil, fmt.Errorf("%w: execute requires preview hash", ErrInvalidRequest)
	}
	if expectedHash != strings.TrimSpace(op.PreviewHash) {
		_, _ = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusFailed)
		return nil, fmt.Errorf("%w: preview_hash_mismatch", ErrInvalidRequest)
	}
	if !req.Confirmed {
		_, _ = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusFailed)
		return nil, fmt.Errorf("%w: forget preview requires confirmation", ErrInvalidRequest)
	}
	level := strings.TrimSpace(req.Level)
	if level == "" {
		level = strings.TrimSpace(op.RequestedLevel)
	}
	if level == "" {
		_, _ = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusFailed)
		return nil, fmt.Errorf("%w: execute requires preview requested level", ErrInvalidRequest)
	}
	if op.RequestedLevel != "" && level != op.RequestedLevel {
		_, _ = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusFailed)
		return nil, fmt.Errorf("%w: preview_level_mismatch", ErrInvalidRequest)
	}
	preview := ForgetPreviewResult{
		PersonaID:            op.PersonaID,
		OperationID:          op.OperationID,
		PreviewHash:          op.PreviewHash,
		RequestedLevel:       op.RequestedLevel,
		ScopeMode:            op.ScopeMode,
		Status:               "resolved",
		RequiresConfirmation: op.RequiresConfirmation,
		Targets:              append([]ForgetResolvedTarget(nil), op.Targets...),
	}
	targets, err := confirmedForgetTargets(req, preview)
	if err != nil {
		_, _ = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusFailed)
		return nil, err
	}
	actor := defaultString(req.Actor, ForgetActorUser)
	reason := defaultString(req.ReasonCode, ForgetReasonUserRequested)
	result := &ForgetExecuteResult{PersonaID: op.PersonaID, OperationID: op.OperationID, PreviewHash: op.PreviewHash}
	for _, target := range targets {
		forget, err := s.Forget(ctx, ForgetRequest{
			PersonaID:  op.PersonaID,
			Actor:      actor,
			ReasonCode: reason,
			Level:      level,
			Target: ForgetTarget{
				ScopeMode: ForgetScopeExactNode,
				NodeType:  target.NodeType,
				NodeID:    target.NodeID,
			},
		})
		if err != nil {
			_, _ = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusFailed)
			return nil, err
		}
		result.Executed++
		result.Results = append(result.Results, *forget)
	}
	if err := s.recordSemanticDecisionAudit(ctx, semanticDecisionAuditRecord{
		RequestID:       op.OperationID,
		PersonaID:       op.PersonaID,
		DecisionType:    "forget_execute",
		Actor:           actor,
		ReasonCode:      reason,
		CandidateHash:   forgetPreviewCandidateHash(op.Targets),
		SelectedNodeIDs: exactRefsFromResolvedTargets(targets),
		PreviewHash:     op.PreviewHash,
		PolicySnapshot: map[string]any{
			"level":           level,
			"requested_level": strings.TrimSpace(op.RequestedLevel),
			"scope_mode":      op.ScopeMode,
			"operation_id":    op.OperationID,
		},
		DiagnosticsJSON: map[string]any{"executed": result.Executed, "operation_id": op.OperationID},
	}); err != nil {
		_, _ = s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusFailed)
		return result, err
	}
	if _, err := s.updatePendingManualForgetOperationStatus(ctx, op.OperationID, ManualForgetOperationStatusExecuted); err != nil {
		return result, err
	}
	return result, nil
}

func formatManualForgetOperationTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now()
	}
	return value.Format(time.RFC3339Nano)
}

func parseManualForgetOperationTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed
	}
	return time.Time{}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func manualForgetNullStringValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}
