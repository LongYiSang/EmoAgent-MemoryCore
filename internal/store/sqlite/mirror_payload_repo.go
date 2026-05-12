package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type MirrorPayloadRepository struct {
	db *sql.DB
}

type MirrorNodePayload struct {
	PersonaID      string
	NodeType       string
	SQLiteNodeID   string
	SearchableText string
	Payload        map[string]any
}

type MirrorEdgePayload struct {
	PersonaID    string
	SQLiteEdgeID string
	LinkType     string
	FromNodeType string
	FromNodeID   string
	ToNodeType   string
	ToNodeID     string
	Direction    string
	Confidence   float64
	Weight       float64
	Payload      map[string]any
}

func NewMirrorPayloadRepository(db *sql.DB) *MirrorPayloadRepository {
	return &MirrorPayloadRepository{db: db}
}

func (r *MirrorPayloadRepository) BuildNodePayload(ctx context.Context, personaID string, nodeType string, nodeID string) (MirrorNodePayload, bool, error) {
	switch nodeType {
	case "entity":
		return r.buildEntityPayload(ctx, personaID, nodeType, nodeID)
	case "fact":
		return r.buildFactPayload(ctx, personaID, nodeType, nodeID)
	case "narrative":
		return r.buildNarrativePayload(ctx, personaID, nodeType, nodeID)
	case "insight":
		return r.buildInsightPayload(ctx, personaID, nodeType, nodeID)
	default:
		return MirrorNodePayload{}, false, nil
	}
}

func (r *MirrorPayloadRepository) BuildEdgePayload(ctx context.Context, personaID string, edgeID string) (MirrorEdgePayload, bool, error) {
	var payload MirrorEdgePayload
	var visibility string
	var searchable int
	err := r.db.QueryRowContext(ctx, `
SELECT persona_id, id, link_type, from_node_type, from_node_id,
       to_node_type, to_node_id, direction, confidence, weight,
       visibility_status, searchable
FROM memory_links
WHERE persona_id = ? AND id = ?`, personaID, edgeID).Scan(
		&payload.PersonaID,
		&payload.SQLiteEdgeID,
		&payload.LinkType,
		&payload.FromNodeType,
		&payload.FromNodeID,
		&payload.ToNodeType,
		&payload.ToNodeID,
		&payload.Direction,
		&payload.Confidence,
		&payload.Weight,
		&visibility,
		&searchable,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MirrorEdgePayload{}, false, nil
	}
	if err != nil {
		return MirrorEdgePayload{}, false, err
	}
	if visibility != "visible" || searchable != 1 || !isMirrorEligibleLinkType(payload.LinkType) {
		return MirrorEdgePayload{}, false, nil
	}
	payload.Payload = map[string]any{
		"direction":  payload.Direction,
		"confidence": payload.Confidence,
		"weight":     payload.Weight,
	}
	return payload, true, nil
}

func (r *MirrorPayloadRepository) buildEntityPayload(ctx context.Context, personaID string, nodeType string, nodeID string) (MirrorNodePayload, bool, error) {
	var canonicalName, entityType string
	var description sql.NullString
	var visibility, sensitivity string
	var searchable int
	err := r.db.QueryRowContext(ctx, `
SELECT canonical_name, entity_type, description, visibility_status, sensitivity_level, searchable
FROM entities
WHERE persona_id = ? AND id = ?`, personaID, nodeID).Scan(
		&canonicalName,
		&entityType,
		&description,
		&visibility,
		&sensitivity,
		&searchable,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MirrorNodePayload{}, false, nil
	}
	if err != nil {
		return MirrorNodePayload{}, false, err
	}
	if visibility != "visible" || searchable != 1 {
		return MirrorNodePayload{}, false, nil
	}
	aliases, err := r.entityAliases(ctx, personaID, nodeID)
	if err != nil {
		return MirrorNodePayload{}, false, err
	}
	textParts := []string{canonicalName, entityType, description.String}
	textParts = append(textParts, aliases...)
	return MirrorNodePayload{
		PersonaID:      personaID,
		NodeType:       nodeType,
		SQLiteNodeID:   nodeID,
		SearchableText: joinSearchText(textParts...),
		Payload: map[string]any{
			"persona_id":        personaID,
			"node_type":         nodeType,
			"sqlite_node_id":    nodeID,
			"entity_type":       entityType,
			"sensitivity_level": sensitivity,
			"aliases":           aliases,
		},
	}, true, nil
}

func (r *MirrorPayloadRepository) buildFactPayload(ctx context.Context, personaID string, nodeType string, nodeID string) (MirrorNodePayload, bool, error) {
	var factType, predicate, summary string
	var subjectID, objectID, validFrom, validTo, updatedAt sql.NullString
	var subjectName, objectName sql.NullString
	var importance, valence, arousal float64
	var validity, visibility, lifecycle, sensitivity string
	var pinned, searchable int
	err := r.db.QueryRowContext(ctx, `
SELECT f.fact_type, f.predicate, f.subject_entity_id, se.canonical_name,
       f.object_entity_id, oe.canonical_name, f.content_summary,
       f.valid_from, f.valid_to, f.importance, f.valence, f.arousal,
       f.validity_status, f.visibility_status, f.lifecycle_status,
       f.sensitivity_level, f.pinned, f.searchable, f.updated_at
FROM facts f
LEFT JOIN entities se ON se.persona_id = f.persona_id AND se.id = f.subject_entity_id
LEFT JOIN entities oe ON oe.persona_id = f.persona_id AND oe.id = f.object_entity_id
WHERE f.persona_id = ? AND f.id = ?`, personaID, nodeID).Scan(
		&factType,
		&predicate,
		&subjectID,
		&subjectName,
		&objectID,
		&objectName,
		&summary,
		&validFrom,
		&validTo,
		&importance,
		&valence,
		&arousal,
		&validity,
		&visibility,
		&lifecycle,
		&sensitivity,
		&pinned,
		&searchable,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MirrorNodePayload{}, false, nil
	}
	if err != nil {
		return MirrorNodePayload{}, false, err
	}
	if visibility != "visible" || searchable != 1 {
		return MirrorNodePayload{}, false, nil
	}
	eligible, err := factSearchEvidenceEligible(ctx, r.db, personaID, nodeID)
	if err != nil {
		return MirrorNodePayload{}, false, err
	}
	if !eligible {
		return MirrorNodePayload{}, false, nil
	}
	return MirrorNodePayload{
		PersonaID:      personaID,
		NodeType:       nodeType,
		SQLiteNodeID:   nodeID,
		SearchableText: joinSearchText(summary, predicate, subjectName.String, objectName.String),
		Payload: map[string]any{
			"persona_id":        personaID,
			"node_type":         nodeType,
			"sqlite_node_id":    nodeID,
			"fact_type":         factType,
			"predicate":         predicate,
			"subject_entity_id": nullStringValue(subjectID),
			"object_entity_id":  nullStringValue(objectID),
			"valid_from":        nullStringValue(validFrom),
			"valid_to":          nullStringValue(validTo),
			"importance":        importance,
			"valence":           valence,
			"arousal":           arousal,
			"validity_status":   validity,
			"visibility_status": visibility,
			"lifecycle_status":  lifecycle,
			"sensitivity_level": sensitivity,
			"pinned":            pinned == 1,
			"updated_at":        nullStringValue(updatedAt),
		},
	}, true, nil
}

func (r *MirrorPayloadRepository) buildNarrativePayload(ctx context.Context, personaID string, nodeType string, nodeID string) (MirrorNodePayload, bool, error) {
	var scope, summary string
	var scopeRef, emotionalTone, validFrom, validTo sql.NullString
	var importance float64
	var visibility, lifecycle, sensitivity string
	var searchable int
	err := r.db.QueryRowContext(ctx, `
SELECT scope, scope_ref, summary, emotional_tone, importance,
       valid_from, valid_to, visibility_status, lifecycle_status,
       sensitivity_level, searchable
FROM narratives
WHERE persona_id = ? AND id = ?`, personaID, nodeID).Scan(
		&scope,
		&scopeRef,
		&summary,
		&emotionalTone,
		&importance,
		&validFrom,
		&validTo,
		&visibility,
		&lifecycle,
		&sensitivity,
		&searchable,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MirrorNodePayload{}, false, nil
	}
	if err != nil {
		return MirrorNodePayload{}, false, err
	}
	if visibility != "visible" || searchable != 1 {
		return MirrorNodePayload{}, false, nil
	}
	return MirrorNodePayload{
		PersonaID:      personaID,
		NodeType:       nodeType,
		SQLiteNodeID:   nodeID,
		SearchableText: joinSearchText(summary, scope, scopeRef.String, emotionalTone.String),
		Payload: map[string]any{
			"persona_id":        personaID,
			"node_type":         nodeType,
			"sqlite_node_id":    nodeID,
			"scope":             scope,
			"scope_ref":         nullStringValue(scopeRef),
			"importance":        importance,
			"valid_from":        nullStringValue(validFrom),
			"valid_to":          nullStringValue(validTo),
			"visibility_status": visibility,
			"lifecycle_status":  lifecycle,
			"sensitivity_level": sensitivity,
		},
	}, true, nil
}

func (r *MirrorPayloadRepository) buildInsightPayload(ctx context.Context, personaID string, nodeType string, nodeID string) (MirrorNodePayload, bool, error) {
	var insightType, content string
	var confidence, importance, valence, arousal float64
	var visibility, lifecycle, sensitivity string
	var searchable int
	err := r.db.QueryRowContext(ctx, `
SELECT insight_type, content, confidence, importance, valence, arousal,
       visibility_status, lifecycle_status, sensitivity_level, searchable
FROM insights
WHERE persona_id = ? AND id = ?`, personaID, nodeID).Scan(
		&insightType,
		&content,
		&confidence,
		&importance,
		&valence,
		&arousal,
		&visibility,
		&lifecycle,
		&sensitivity,
		&searchable,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MirrorNodePayload{}, false, nil
	}
	if err != nil {
		return MirrorNodePayload{}, false, err
	}
	if visibility != "visible" || searchable != 1 {
		return MirrorNodePayload{}, false, nil
	}
	return MirrorNodePayload{
		PersonaID:      personaID,
		NodeType:       nodeType,
		SQLiteNodeID:   nodeID,
		SearchableText: joinSearchText(content, insightType),
		Payload: map[string]any{
			"persona_id":        personaID,
			"node_type":         nodeType,
			"sqlite_node_id":    nodeID,
			"insight_type":      insightType,
			"confidence":        confidence,
			"importance":        importance,
			"valence":           valence,
			"arousal":           arousal,
			"visibility_status": visibility,
			"lifecycle_status":  lifecycle,
			"sensitivity_level": sensitivity,
		},
	}, true, nil
}

func (r *MirrorPayloadRepository) entityAliases(ctx context.Context, personaID string, entityID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT alias
FROM entity_aliases
WHERE persona_id = ? AND entity_id = ?
ORDER BY alias`, personaID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []string
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			return nil, err
		}
		aliases = append(aliases, alias)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return aliases, nil
}

func isMirrorEligibleLinkType(linkType string) bool {
	switch linkType {
	case "ABOUT_ENTITY", "CAUSED_BY", "CONTRIBUTED_TO", "EXPLAINS", "SUPPORTS",
		"CONTRADICTS", "INHIBITS", "SUPERSEDES", "DERIVED_FROM", "CO_OCCURS_WITH":
		return true
	default:
		return false
	}
}

func joinSearchText(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, " ")
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}
