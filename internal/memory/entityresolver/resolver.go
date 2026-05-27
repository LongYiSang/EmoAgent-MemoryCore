package entityresolver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/longyisang/emoagent-memorycore/internal/app/memorycore"
)

type Resolver struct {
	Service memorycore.Service
	DB      *sql.DB
}

type Input struct {
	PersonaID        string
	KnownEntities    []memorycore.ExtractionKnownEntity
	ResponseEntities []memorycore.ExtractedEntityCandidate
	CandidateID      string
	AllowSensitive   bool
	Predicate        string
	ObjectKind       string
}

type Result struct {
	CandidateID  string
	EntityID     string
	Status       string
	CanonicalKey string
	ReasonCodes  []string
}

func (r Resolver) Resolve(ctx context.Context, in Input) (Result, error) {
	candidateID := strings.TrimSpace(in.CandidateID)
	if r.Service == nil || r.DB == nil {
		return Result{}, fmt.Errorf("service and db are required")
	}
	if candidateID == "user" {
		entityID, err := r.resolveUser(ctx, in.PersonaID)
		return Result{CandidateID: candidateID, EntityID: entityID, Status: "resolved", CanonicalKey: "special:user"}, err
	}
	if candidateID == "agent" {
		return Result{CandidateID: candidateID, Status: "rejected", CanonicalKey: "special:agent", ReasonCodes: []string{"agent_affect_boundary"}}, fmt.Errorf("agent entity cannot be used for user memory fact apply")
	}
	for _, entity := range in.KnownEntities {
		if entity.EntityID != candidateID {
			continue
		}
		if err := requireVisibleSearchableEntity(ctx, r.DB, in.PersonaID, entity.EntityID); err != nil {
			return Result{CandidateID: candidateID, Status: "needs_review", ReasonCodes: []string{"known_entity_not_visible_searchable"}}, err
		}
		return Result{CandidateID: candidateID, EntityID: entity.EntityID, Status: "resolved", CanonicalKey: normalizeEntityText(entity.CanonicalName)}, nil
	}
	candidate, ok := findCandidate(in.ResponseEntities, candidateID)
	if !ok {
		return Result{CandidateID: candidateID, Status: "rejected", ReasonCodes: []string{"entity_candidate_not_found"}}, fmt.Errorf("entity candidate %s was not found", candidateID)
	}
	if candidate.EntityType == memorycore.EntityTypeAgent {
		return Result{CandidateID: candidateID, Status: "rejected", CanonicalKey: normalizeEntityText(candidate.CanonicalName), ReasonCodes: []string{"agent_affect_boundary"}}, fmt.Errorf("agent entity cannot be used for user memory fact apply")
	}
	if candidate.SensitivityLevel == memorycore.SensitivityHighlySensitive && !in.AllowSensitive {
		return Result{CandidateID: candidateID, Status: "needs_review", CanonicalKey: normalizeEntityText(candidate.CanonicalName), ReasonCodes: []string{"highly_sensitive_entity_requires_review"}}, fmt.Errorf("highly sensitive entity candidate requires review")
	}
	if candidate.KnownEntityID != nil && strings.TrimSpace(*candidate.KnownEntityID) != "" {
		entityID := strings.TrimSpace(*candidate.KnownEntityID)
		if err := requireVisibleSearchableEntity(ctx, r.DB, in.PersonaID, entityID); err != nil {
			return Result{CandidateID: candidateID, Status: "needs_review", CanonicalKey: normalizeEntityText(candidate.CanonicalName), ReasonCodes: []string{"known_entity_not_visible_searchable"}}, err
		}
		return Result{CandidateID: candidateID, EntityID: entityID, Status: "resolved", CanonicalKey: normalizeEntityText(candidate.CanonicalName)}, nil
	}
	if candidate.MergeHint == "ambiguous" {
		return Result{CandidateID: candidateID, Status: "needs_review", CanonicalKey: normalizeEntityText(candidate.CanonicalName), ReasonCodes: []string{"ambiguous_entity"}}, fmt.Errorf("ambiguous entity candidate cannot apply")
	}
	if candidate.MergeHint == "known_entity" || candidate.MergeHint == "maybe_existing" || candidate.MergeHint == "new_entity" {
		matches, err := findEntitiesByNormalizedNameOrAlias(ctx, r.DB, in.PersonaID, candidate.CanonicalName, candidate.Aliases)
		if err != nil {
			return Result{}, err
		}
		if len(matches) == 0 && canResolveNamedPetAlias(candidate, in) {
			matches, err = findPetAliasMatches(ctx, r.DB, in.PersonaID, candidate.CanonicalName, candidate.Aliases)
			if err != nil {
				return Result{}, err
			}
		}
		if len(matches) == 1 {
			return Result{CandidateID: candidateID, EntityID: matches[0], Status: "resolved", CanonicalKey: normalizeEntityText(candidate.CanonicalName)}, nil
		}
		if len(matches) > 1 {
			return Result{CandidateID: candidateID, Status: "needs_review", CanonicalKey: normalizeEntityText(candidate.CanonicalName), ReasonCodes: []string{"ambiguous_entity"}}, fmt.Errorf("ambiguous entity candidate cannot apply")
		}
		if candidate.MergeHint == "known_entity" {
			return Result{CandidateID: candidateID, Status: "needs_review", CanonicalKey: normalizeEntityText(candidate.CanonicalName), ReasonCodes: []string{"known_entity_not_visible_searchable"}}, fmt.Errorf("known entity candidate did not match visible searchable entity")
		}
	}
	entityType := candidate.EntityType
	if strings.TrimSpace(entityType) == "" {
		entityType = memorycore.EntityTypeConcept
	}
	entity, err := r.Service.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		ID:               "ent_" + uuid.NewString(),
		PersonaID:        in.PersonaID,
		CanonicalName:    candidate.CanonicalName,
		EntityType:       entityType,
		SensitivityLevel: candidate.SensitivityLevel,
		Aliases:          aliasesForEnsure(candidate.Aliases),
	})
	if err != nil {
		return Result{}, err
	}
	return Result{CandidateID: candidateID, EntityID: entity.ID, Status: "created", CanonicalKey: normalizeEntityText(candidate.CanonicalName)}, nil
}

func canResolveNamedPetAlias(candidate memorycore.ExtractedEntityCandidate, in Input) bool {
	return candidate.EntityType == memorycore.EntityTypeObject &&
		strings.TrimSpace(in.Predicate) == "has_pet" &&
		strings.TrimSpace(in.ObjectKind) == "entity" &&
		candidate.SensitivityLevel != memorycore.SensitivityHighlySensitive
}

func (r Resolver) resolveUser(ctx context.Context, personaID string) (string, error) {
	id, err := findSingleEntityByType(ctx, r.DB, personaID, memorycore.EntityTypeUser)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	entity, err := r.Service.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
		ID:            "ent_user",
		PersonaID:     personaID,
		CanonicalName: "User",
		EntityType:    memorycore.EntityTypeUser,
	})
	if err != nil {
		return "", err
	}
	return entity.ID, nil
}

func findCandidate(candidates []memorycore.ExtractedEntityCandidate, candidateID string) (memorycore.ExtractedEntityCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.CandidateID == candidateID {
			return candidate, true
		}
	}
	return memorycore.ExtractedEntityCandidate{}, false
}

func findSingleEntityByType(ctx context.Context, db *sql.DB, personaID string, entityType string) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, `
SELECT id
FROM entities
WHERE persona_id = ?
  AND entity_type = ?
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY created_at ASC
LIMIT 1`, personaID, entityType).Scan(&id)
	return id, err
}

func requireVisibleSearchableEntity(ctx context.Context, db *sql.DB, personaID string, entityID string) error {
	var id string
	return db.QueryRowContext(ctx, `
SELECT id
FROM entities
WHERE persona_id = ?
  AND id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`, personaID, entityID).Scan(&id)
}

func findEntitiesByNormalizedNameOrAlias(ctx context.Context, db *sql.DB, personaID string, name string, aliases []string) ([]string, error) {
	needles := map[string]struct{}{}
	for _, value := range append([]string{name}, aliases...) {
		key := normalizeEntityText(value)
		if key != "" {
			needles[key] = struct{}{}
		}
	}
	if len(needles) == 0 {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, canonical_name
FROM entities
WHERE persona_id = ?
  AND visibility_status = 'visible'
  AND searchable = 1`, personaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches := map[string]struct{}{}
	for rows.Next() {
		var id, canonical string
		if err := rows.Scan(&id, &canonical); err != nil {
			return nil, err
		}
		if _, ok := needles[normalizeEntityText(canonical)]; ok {
			matches[id] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	aliasRows, err := db.QueryContext(ctx, `
SELECT e.id, a.alias
FROM entity_aliases a
JOIN entities e ON e.id = a.entity_id AND e.persona_id = a.persona_id
WHERE a.persona_id = ?
  AND e.visibility_status = 'visible'
  AND e.searchable = 1`, personaID)
	if err != nil {
		return nil, err
	}
	defer aliasRows.Close()
	for aliasRows.Next() {
		var id, alias string
		if err := aliasRows.Scan(&id, &alias); err != nil {
			return nil, err
		}
		if _, ok := needles[normalizeEntityText(alias)]; ok {
			matches[id] = struct{}{}
		}
	}
	if err := aliasRows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for id := range matches {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func findPetAliasMatches(ctx context.Context, db *sql.DB, personaID string, name string, aliases []string) ([]string, error) {
	needles := petSurfaceKeys(append([]string{name}, aliases...)...)
	if len(needles) == 0 {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, canonical_name
FROM entities
WHERE persona_id = ?
  AND entity_type = ?
  AND visibility_status = 'visible'
  AND searchable = 1`, personaID, memorycore.EntityTypeObject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	matches := map[string]struct{}{}
	for rows.Next() {
		var id, canonical string
		if err := rows.Scan(&id, &canonical); err != nil {
			return nil, err
		}
		if surfaceMatches(needles, petSurfaceKeys(canonical)) {
			matches[id] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	aliasRows, err := db.QueryContext(ctx, `
SELECT e.id, a.alias
FROM entity_aliases a
JOIN entities e ON e.id = a.entity_id AND e.persona_id = a.persona_id
WHERE a.persona_id = ?
  AND e.entity_type = ?
  AND e.visibility_status = 'visible'
  AND e.searchable = 1`, personaID, memorycore.EntityTypeObject)
	if err != nil {
		return nil, err
	}
	defer aliasRows.Close()
	for aliasRows.Next() {
		var id, alias string
		if err := aliasRows.Scan(&id, &alias); err != nil {
			return nil, err
		}
		if surfaceMatches(needles, petSurfaceKeys(alias)) {
			matches[id] = struct{}{}
		}
	}
	if err := aliasRows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for id := range matches {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func petSurfaceKeys(values ...string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		for _, candidate := range normalizePetSurfaceName(value) {
			key := normalizeEntityText(candidate)
			if key != "" {
				out[key] = struct{}{}
			}
		}
	}
	return out
}

func surfaceMatches(left map[string]struct{}, right map[string]struct{}) bool {
	for key := range left {
		if _, ok := right[key]; ok {
			return true
		}
	}
	return false
}

func normalizePetSurfaceName(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	candidates := []string{value}
	trimmed := strings.TrimSpace(value)
	for _, prefix := range []string{"我家", "家里", "我的"} {
		trimmed = strings.TrimPrefix(trimmed, prefix)
	}
	candidates = append(candidates, strings.TrimSpace(trimmed))
	for _, prefix := range []string{"橘猫", "小猫", "小狗", "宠物", "cat ", "dog "} {
		candidates = append(candidates, strings.TrimSpace(strings.TrimPrefix(trimmed, prefix)))
	}
	for _, suffix := range []string{"橘猫", "猫", "狗", "小猫", "小狗", "宠物", " cat", " dog", "cat", "dog"} {
		candidates = append(candidates, strings.TrimSpace(strings.TrimSuffix(trimmed, suffix)))
	}
	return uniqueEntityTexts(candidates)
}

func uniqueEntityTexts(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		key := normalizeEntityText(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func aliasesForEnsure(values []string) []memorycore.EntityAliasInput {
	aliases := make([]memorycore.EntityAliasInput, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := normalizeEntityText(value)
		if value == "" || key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		aliases = append(aliases, memorycore.EntityAliasInput{
			ID:         "alias_" + uuid.NewString(),
			Alias:      value,
			AliasType:  memorycore.AliasTypeSurface,
			Confidence: 1,
		})
	}
	return aliases
}

func normalizeEntityText(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range value {
		switch {
		case unicode.IsSpace(r):
			if !lastSpace {
				b.WriteByte(' ')
			}
			lastSpace = true
		case isHarmlessPunctuation(r):
			continue
		default:
			b.WriteRune(unicode.ToLower(r))
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}

func isHarmlessPunctuation(r rune) bool {
	switch r {
	case '"', '\'', '`', '(', ')', '[', ']', '{', '}', '<', '>', ',', '.':
		return true
	default:
		return false
	}
}
