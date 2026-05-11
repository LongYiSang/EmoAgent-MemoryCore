package extractionruntime

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type SQLiteAuditStore struct {
	db *sql.DB
}

func NewSQLiteAuditStore(db *sql.DB) *SQLiteAuditStore {
	return &SQLiteAuditStore{db: db}
}

func (s *SQLiteAuditStore) FindSuccessfulRun(ctx context.Context, fingerprint string, mode memorycore.ExtractionRunMode) (*memorycore.ExtractionRunAuditRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	statuses := successfulStatusesForMode(mode)
	placeholders := strings.TrimRight(strings.Repeat("?,", len(statuses)), ",")
	args := []any{fingerprint, string(mode)}
	for _, status := range statuses {
		args = append(args, status)
	}
	row := s.db.QueryRowContext(ctx, `
SELECT id, request_id, persona_id, session_id, trigger, mode, status, fingerprint,
       provider_id, provider_kind, model, prompt_version, prefilter_prompt_version,
       repair_prompt_version, original_episode_count, kept_episode_count,
       skipped_episode_count, accepted_count, review_count, rejected_count,
       routed_count, not_applied_count, applied_count, failure_count,
       prompt_hash, response_hash, repaired_response_hash, prefilter_hash,
       usage_prompt_tokens, usage_completion_tokens, usage_total_tokens,
       latency_ms, duration_ms, sanitized_error_code, sanitized_error_message,
       created_at, updated_at
FROM extraction_runs
WHERE fingerprint = ?
  AND mode = ?
  AND status IN (`+placeholders+`)
ORDER BY created_at DESC
LIMIT 1`, args...)
	var rec memorycore.ExtractionRunAuditRecord
	var sessionID, model, promptHash, responseHash, repairedHash, prefilterHash, errorCode, errorMessage sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(
		&rec.ID,
		&rec.RequestID,
		&rec.PersonaID,
		&sessionID,
		&rec.Trigger,
		&rec.Mode,
		&rec.Status,
		&rec.Fingerprint,
		&rec.ProviderID,
		&rec.ProviderKind,
		&model,
		&rec.PromptVersion,
		&rec.PreFilterPromptVersion,
		&rec.RepairPromptVersion,
		&rec.OriginalEpisodeCount,
		&rec.KeptEpisodeCount,
		&rec.SkippedEpisodeCount,
		&rec.AcceptedCount,
		&rec.ReviewCount,
		&rec.RejectedCount,
		&rec.RoutedCount,
		&rec.NotAppliedCount,
		&rec.AppliedCount,
		&rec.FailureCount,
		&promptHash,
		&responseHash,
		&repairedHash,
		&prefilterHash,
		&rec.Usage.PromptTokens,
		&rec.Usage.CompletionTokens,
		&rec.Usage.TotalTokens,
		&rec.LatencyMS,
		&rec.DurationMS,
		&errorCode,
		&errorMessage,
		&createdAt,
		&updatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	rec.SessionID = nullStringPtr(sessionID)
	rec.Model = model.String
	rec.PromptHash = promptHash.String
	rec.ResponseHash = responseHash.String
	rec.RepairedResponseHash = repairedHash.String
	rec.PreFilterHash = prefilterHash.String
	rec.SanitizedErrorCode = errorCode.String
	rec.SanitizedErrorMessage = errorMessage.String
	rec.CreatedAt = parseAuditTime(createdAt)
	rec.UpdatedAt = parseAuditTime(updatedAt)
	return &rec, nil
}

func (s *SQLiteAuditStore) RecordRun(ctx context.Context, rec memorycore.ExtractionRunAuditRecord) error {
	if s == nil || s.db == nil {
		return nil
	}
	if rec.ID == "" {
		rec.ID = "xrun_" + uuid.NewString()
	}
	now := time.Now().UTC()
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO extraction_runs(
    id, request_id, persona_id, session_id, trigger, mode, status, fingerprint,
    provider_id, provider_kind, model, prompt_version, prefilter_prompt_version,
    repair_prompt_version, original_episode_count, kept_episode_count,
    skipped_episode_count, accepted_count, review_count, rejected_count,
    routed_count, not_applied_count, applied_count, failure_count,
    prompt_hash, response_hash, repaired_response_hash, prefilter_hash,
    usage_prompt_tokens, usage_completion_tokens, usage_total_tokens,
    latency_ms, duration_ms, sanitized_error_code, sanitized_error_message,
    created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID,
		rec.RequestID,
		rec.PersonaID,
		nullableStringPtr(rec.SessionID),
		rec.Trigger,
		string(rec.Mode),
		string(rec.Status),
		rec.Fingerprint,
		rec.ProviderID,
		rec.ProviderKind,
		nullableString(rec.Model),
		rec.PromptVersion,
		rec.PreFilterPromptVersion,
		rec.RepairPromptVersion,
		rec.OriginalEpisodeCount,
		rec.KeptEpisodeCount,
		rec.SkippedEpisodeCount,
		rec.AcceptedCount,
		rec.ReviewCount,
		rec.RejectedCount,
		rec.RoutedCount,
		rec.NotAppliedCount,
		rec.AppliedCount,
		rec.FailureCount,
		nullableString(rec.PromptHash),
		nullableString(rec.ResponseHash),
		nullableString(rec.RepairedResponseHash),
		nullableString(rec.PreFilterHash),
		rec.Usage.PromptTokens,
		rec.Usage.CompletionTokens,
		rec.Usage.TotalTokens,
		rec.LatencyMS,
		rec.DurationMS,
		nullableString(rec.SanitizedErrorCode),
		nullableString(rec.SanitizedErrorMessage),
		formatAuditTime(rec.CreatedAt),
		formatAuditTime(rec.UpdatedAt),
	)
	return err
}

func successfulStatusesForMode(mode memorycore.ExtractionRunMode) []string {
	switch mode {
	case memorycore.ExtractionRunModeApply:
		return []string{string(memorycore.ExtractionRunStatusApplied)}
	case memorycore.ExtractionRunModeValidate:
		return []string{string(memorycore.ExtractionRunStatusValidated), string(memorycore.ExtractionRunStatusSkipped)}
	default:
		return []string{string(memorycore.ExtractionRunStatusDryRun), string(memorycore.ExtractionRunStatusSkipped)}
	}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableStringPtr(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func formatAuditTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseAuditTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed
	}
	parsed, _ = time.Parse(time.RFC3339, value)
	return parsed
}
