package extractionruntime

import (
	"context"
	"database/sql"
	"strings"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func (r *Runner) RunBatch(ctx context.Context, batch memorycore.ExtractionBatchRequest) (memorycore.ExtractionBatchResult, error) {
	if batch.Mode == "" {
		batch.Mode = memorycore.ExtractionRunModeDryRun
	}
	if batch.Audit == "" {
		batch.Audit = memorycore.ExtractionAuditOn
	}
	if batch.Trigger == "" {
		batch.Trigger = memorycore.ExtractionTriggerSessionEnd
	}
	if batch.Timezone == "" {
		batch.Timezone = "Asia/Singapore"
	}
	if batch.EpisodeLimit == 0 {
		batch.EpisodeLimit = 50
	}
	if batch.MaxFacts == 0 {
		batch.MaxFacts = 12
	}
	if batch.MaxLinks == 0 {
		batch.MaxLinks = 20
	}
	personaID := batch.PersonaID
	if strings.TrimSpace(personaID) == "" {
		personaID = "default"
	}
	sessionIDs := append([]string(nil), batch.SessionIDs...)
	var err error
	if len(sessionIDs) == 0 {
		sessionIDs, err = eligibleSessions(ctx, r.db, personaID, batch)
		if err != nil {
			return memorycore.ExtractionBatchResult{Mode: batch.Mode, Status: "failed"}, err
		}
	}
	result := memorycore.ExtractionBatchResult{Mode: batch.Mode, Status: "ok", Results: []memorycore.ExtractionRunResult{}}
	for _, sessionID := range sessionIDs {
		sid := sessionID
		req, err := BuildRequest(ctx, r.db, BuildRequestOptions{
			PersonaID:                personaID,
			SessionID:                &sid,
			Trigger:                  batch.Trigger,
			Limit:                    batch.EpisodeLimit,
			Since:                    batch.Since,
			Until:                    batch.Until,
			Timezone:                 batch.Timezone,
			AllowSensitiveExtraction: batch.AllowSensitiveExtraction,
			AllowInference:           batch.AllowInference,
			ManualPin:                batch.ManualPin,
			ManualForget:             batch.ManualForget,
			MaxFacts:                 batch.MaxFacts,
			MaxLinks:                 batch.MaxLinks,
		})
		if err != nil {
			result.FailedCount++
			if batch.StopOnError {
				result.Status = "failed"
				return result, err
			}
			continue
		}
		run, err := r.Run(ctx, memorycore.ExtractionRunRequest{
			Request:          req,
			Mode:             batch.Mode,
			ProviderID:       batch.ProviderID,
			ProviderKind:     batch.ProviderKind,
			Model:            batch.Model,
			Temperature:      batch.Temperature,
			MaxTokens:        batch.MaxTokens,
			Timeout:          batch.Timeout,
			UsePreFilter:     batch.UsePreFilter,
			RepairEnabled:    batch.RepairEnabled,
			RequireCleanGate: batch.RequireCleanGate,
			Audit:            batch.Audit,
			Force:            batch.Force,
			Window: memorycore.ExtractionRunWindow{
				Since: batch.Since,
				Until: batch.Until,
				Limit: batch.EpisodeLimit,
			},
		})
		if run.SkippedByFingerprint {
			result.SkippedCount++
		} else if err != nil || run.Status == memorycore.ExtractionRunStatusFailed {
			result.FailedCount++
		} else {
			result.ProcessedCount++
		}
		result.Results = append(result.Results, run)
		if err != nil && batch.StopOnError {
			result.Status = "failed"
			return result, err
		}
	}
	if result.FailedCount > 0 {
		result.Status = "partial_failure"
	}
	return result, nil
}

func eligibleSessions(ctx context.Context, db *sql.DB, personaID string, batch memorycore.ExtractionBatchRequest) ([]string, error) {
	limit := batch.Limit
	if limit == 0 {
		limit = 50
	}
	query := `
SELECT e.session_id
FROM episodes e
JOIN sessions s ON s.id = e.session_id AND s.persona_id = e.persona_id
WHERE e.persona_id = ?
  AND e.visibility_status = 'visible'
  AND e.searchable = 1`
	args := []any{personaID}
	if batch.Since != nil && !batch.Since.IsZero() {
		query += ` AND e.occurred_at >= ?`
		args = append(args, batch.Since.UTC().Format(timeFormatRFC3339Nano))
	}
	if batch.Until != nil && !batch.Until.IsZero() {
		query += ` AND e.occurred_at <= ?`
		args = append(args, batch.Until.UTC().Format(timeFormatRFC3339Nano))
	}
	query += ` GROUP BY e.session_id ORDER BY MIN(e.occurred_at) ASC LIMIT ?`
	args = append(args, limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

const timeFormatRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"
