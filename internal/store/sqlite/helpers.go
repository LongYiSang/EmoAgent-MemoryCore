package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

const timeFormat = time.RFC3339Nano

func formatTime(value time.Time) string {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return value.UTC().Format(timeFormat)
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(timeFormat, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func nullableString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullableTime(value *time.Time) sql.NullString {
	if value == nil || value.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: formatTime(*value), Valid: true}
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timePtr(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := parseTime(value.String)
	return &parsed
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func intBool(value int) bool {
	return value != 0
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func defaultVisibility(status core.VisibilityStatus) core.VisibilityStatus {
	if status == "" {
		return core.VisibilityVisible
	}
	return status
}

func defaultSensitivity(level core.SensitivityLevel) core.SensitivityLevel {
	if level == "" {
		return core.SensitivityNormal
	}
	return level
}

func defaultLifecycle(status core.LifecycleStatus) core.LifecycleStatus {
	if status == "" {
		return core.LifecycleActive
	}
	return status
}

func defaultValidity(status core.ValidityStatus) core.ValidityStatus {
	if status == "" {
		return core.ValidityValid
	}
	return status
}

func rowExists(ctx context.Context, db queryer, query string, args ...any) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func requireNodeExists(ctx context.Context, db queryer, personaID string, nodeType core.NodeType, nodeID string) error {
	table, idColumn, err := nodeTable(nodeType)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE persona_id = ? AND %s = ?`, table, idColumn)
	exists, err := rowExists(ctx, db, query, personaID, nodeID)
	if err != nil {
		return fmt.Errorf("check %s node %s: %w", nodeType, nodeID, err)
	}
	if !exists {
		return fmt.Errorf("%s node %s does not exist", nodeType, nodeID)
	}
	return nil
}

func nodeTable(nodeType core.NodeType) (table string, idColumn string, err error) {
	switch nodeType {
	case core.NodeTypeEpisode:
		return "episodes", "id", nil
	case core.NodeTypeEntity:
		return "entities", "id", nil
	case core.NodeTypeFact:
		return "facts", "id", nil
	case core.NodeTypeNarrative:
		return "narratives", "id", nil
	case core.NodeTypeInsight:
		return "insights", "id", nil
	case core.NodeTypeMoodState:
		return "mood_states", "id", nil
	case core.NodeTypeAffectEvent:
		return "affect_events", "id", nil
	case core.NodeTypeAgentAffectProfile:
		return "agent_affect_profiles", "id", nil
	case core.NodeTypeAgentAffectState:
		return "agent_affect_states", "id", nil
	case core.NodeTypeAgentAppraisal:
		return "agent_appraisals", "id", nil
	case core.NodeTypeAgentAffectEvent:
		return "agent_affect_events", "id", nil
	case core.NodeTypeAgentExpressionDecision:
		return "agent_expression_decisions", "id", nil
	case core.NodeTypeDeletionEvent:
		return "deletion_events", "id", nil
	default:
		return "", "", fmt.Errorf("unsupported node type %q", nodeType)
	}
}
