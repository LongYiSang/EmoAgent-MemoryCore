package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type SearchRepository struct {
	db *sql.DB
}

type RebuildSearchDocumentsResult struct {
	Upserted int
}

func NewSearchRepository(db *sql.DB) *SearchRepository {
	return &SearchRepository{db: db}
}

func (r *SearchRepository) UpsertDocument(ctx context.Context, doc core.SearchDocument) error {
	return upsertSearchDocument(ctx, r.db, doc)
}

func (r *SearchRepository) UpsertSearchDocument(ctx context.Context, doc core.SearchDocument) error {
	return r.UpsertDocument(ctx, doc)
}

func (r *SearchRepository) DeleteSearchDocument(ctx context.Context, personaID string, nodeType core.NodeType, nodeID string) error {
	if err := deleteSearchDocument(ctx, r.db, personaID, nodeType, nodeID); err != nil {
		return err
	}
	if err := deleteSearchFTS(ctx, r.db, personaID, nodeType, nodeID); err != nil {
		return err
	}
	return nil
}

func (r *SearchRepository) UpsertFactDocument(ctx context.Context, personaID string, factID string) error {
	doc, err := buildFactSearchDocument(ctx, r.db, personaID, factID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r.DeleteSearchDocument(ctx, personaID, core.NodeTypeFact, factID)
		}
		return err
	}
	if !isSearchDocumentVisibleAndSearchable(doc) {
		return r.DeleteSearchDocument(ctx, personaID, core.NodeTypeFact, factID)
	}
	return r.UpsertDocument(ctx, doc)
}

func (r *SearchRepository) UpsertNarrativeDocument(ctx context.Context, personaID string, narrativeID string) error {
	return upsertNarrativeSearchDocumentTx(ctx, r.db, personaID, narrativeID)
}

func (r *SearchRepository) UpsertInsightDocument(ctx context.Context, personaID string, insightID string) error {
	return upsertInsightSearchDocumentTx(ctx, r.db, personaID, insightID)
}

func (r *SearchRepository) RebuildSearchDocuments(ctx context.Context, personaID string) (RebuildSearchDocumentsResult, error) {
	if _, err := r.db.ExecContext(ctx, `
DELETE FROM memory_search_documents
WHERE persona_id = ?
  AND node_type = 'fact'
  AND NOT EXISTS (
      SELECT 1
      FROM facts f
      WHERE f.persona_id = memory_search_documents.persona_id
        AND f.id = memory_search_documents.node_id
        AND f.visibility_status = 'visible'
        AND f.searchable = 1
        AND (
            EXISTS (
                SELECT 1
                FROM memory_links l
                JOIN episodes e
                  ON e.persona_id = l.persona_id
                 AND e.id = l.to_node_id
                WHERE l.persona_id = f.persona_id
                  AND l.from_node_type = 'fact'
                  AND l.from_node_id = f.id
                  AND l.link_type = 'EVIDENCED_BY'
                  AND l.to_node_type = 'episode'
                  AND e.visibility_status = 'visible'
                  AND e.searchable = 1
            )
            OR NOT EXISTS (
                SELECT 1
                FROM memory_links l
                WHERE l.persona_id = f.persona_id
                  AND l.from_node_type = 'fact'
                  AND l.from_node_id = f.id
                  AND l.link_type = 'EVIDENCED_BY'
                  AND l.to_node_type = 'episode'
            )
        )
  )`, personaID); err != nil {
		return RebuildSearchDocumentsResult{}, err
	}

	rows, err := r.db.QueryContext(ctx, `
SELECT id
FROM facts f
WHERE f.persona_id = ?
  AND f.visibility_status = 'visible'
  AND f.searchable = 1
  AND (
      EXISTS (
          SELECT 1
          FROM memory_links l
          JOIN episodes e
            ON e.persona_id = l.persona_id
           AND e.id = l.to_node_id
          WHERE l.persona_id = f.persona_id
            AND l.from_node_type = 'fact'
            AND l.from_node_id = f.id
            AND l.link_type = 'EVIDENCED_BY'
            AND l.to_node_type = 'episode'
            AND e.visibility_status = 'visible'
            AND e.searchable = 1
      )
      OR NOT EXISTS (
          SELECT 1
          FROM memory_links l
          WHERE l.persona_id = f.persona_id
            AND l.from_node_type = 'fact'
            AND l.from_node_id = f.id
            AND l.link_type = 'EVIDENCED_BY'
            AND l.to_node_type = 'episode'
      )
  )
ORDER BY created_at ASC`, personaID)
	if err != nil {
		return RebuildSearchDocumentsResult{}, err
	}
	defer rows.Close()

	var factIDs []string
	for rows.Next() {
		var factID string
		if err := rows.Scan(&factID); err != nil {
			return RebuildSearchDocumentsResult{}, err
		}
		factIDs = append(factIDs, factID)
	}
	if err := rows.Err(); err != nil {
		return RebuildSearchDocumentsResult{}, err
	}
	if err := rows.Close(); err != nil {
		return RebuildSearchDocumentsResult{}, err
	}

	var result RebuildSearchDocumentsResult
	for _, factID := range factIDs {
		if err := r.UpsertFactDocument(ctx, personaID, factID); err != nil {
			return RebuildSearchDocumentsResult{}, err
		}
		result.Upserted++
	}
	if err := rebuildSearchFTS(ctx, r.db, nil); err != nil {
		return RebuildSearchDocumentsResult{}, err
	}
	return result, nil
}

func (r *SearchRepository) SearchDocuments(ctx context.Context, personaID string, query string, useFTS bool, limit int) ([]core.SearchDocument, error) {
	if useFTS {
		docs, err := r.searchFTS(ctx, personaID, query, limit)
		if err == nil && len(docs) > 0 {
			return docs, nil
		}
		if err != nil && !isSearchIndexUnavailable(err) {
			return nil, err
		}
	}
	return r.KeywordSearch(ctx, personaID, query, limit)
}

func (r *SearchRepository) SearchDocumentsForRetrieval(ctx context.Context, personaID string, query string, useFTS bool, limit int, policy RetrievalPolicy) ([]core.SearchDocument, error) {
	if useFTS {
		docs, err := r.searchFTSForRetrieval(ctx, personaID, query, limit, policy)
		if err == nil && len(docs) > 0 {
			return docs, nil
		}
		if err != nil && !isSearchIndexUnavailable(err) {
			return nil, err
		}
	}
	return r.keywordSearchForRetrieval(ctx, personaID, query, limit, policy)
}

func (r *SearchRepository) searchFTS(ctx context.Context, personaID string, query string, limit int) ([]core.SearchDocument, error) {
	if limit <= 0 {
		limit = 10
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if ok, err := searchFTSExists(ctx, r.db); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT d.id, d.persona_id, d.node_type, d.node_id, d.search_text, d.search_tier,
       d.visibility_status, d.sensitivity_level, d.lifecycle_status, d.searchable
FROM memory_search_fts f
JOIN memory_search_documents d
  ON d.persona_id = f.persona_id
 AND d.node_type = f.node_type
 AND d.node_id = f.node_id
WHERE f.persona_id = ?
  AND memory_search_fts MATCH ?
  AND d.visibility_status = 'visible'
  AND d.searchable = 1
ORDER BY d.updated_at DESC
LIMIT ?`, personaID, ftsQuery(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchDocuments(rows)
}

func (r *SearchRepository) searchFTSForRetrieval(ctx context.Context, personaID string, query string, limit int, policy RetrievalPolicy) ([]core.SearchDocument, error) {
	if limit <= 0 {
		limit = 10
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if ok, err := searchFTSExists(ctx, r.db); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT d.id, d.persona_id, d.node_type, d.node_id, d.search_text, d.search_tier,
       d.visibility_status, d.sensitivity_level, d.lifecycle_status, d.searchable
FROM memory_search_fts fts
JOIN memory_search_documents d
  ON d.persona_id = fts.persona_id
 AND d.node_type = fts.node_type
 AND d.node_id = fts.node_id
JOIN facts facts
  ON facts.persona_id = d.persona_id
 AND facts.id = d.node_id
WHERE fts.persona_id = ?
  AND memory_search_fts MATCH ?
  AND d.node_type = 'fact'
  AND d.visibility_status = 'visible'
  AND d.searchable = 1
  AND facts.visibility_status = 'visible'
  AND facts.searchable = 1
  AND (facts.validity_status != 'invalidated' OR ? = 1)
  AND (facts.lifecycle_status != 'archived' OR ? = 1)
  AND (facts.lifecycle_status != 'deep_archived' OR ? = 1)
ORDER BY bm25(memory_search_fts), d.updated_at DESC, d.node_id ASC
LIMIT ?`,
		personaID,
		ftsQuery(query),
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowDeepArchive),
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSearchDocuments(rows)
}

func upsertSearchDocument(ctx context.Context, runner sqlRunner, doc core.SearchDocument) error {
	doc = normalizeSearchDocument(doc)
	if !isSearchDocumentVisibleAndSearchable(doc) {
		return deleteSearchDocument(ctx, runner, doc.PersonaID, doc.NodeType, doc.NodeID)
	}
	_, err := runner.ExecContext(ctx, `
INSERT INTO memory_search_documents (
    id, persona_id, node_type, node_id, search_text, search_tier,
    visibility_status, sensitivity_level, lifecycle_status, searchable
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(persona_id, node_type, node_id) DO UPDATE SET
    search_text = excluded.search_text,
    search_tier = excluded.search_tier,
    visibility_status = excluded.visibility_status,
    sensitivity_level = excluded.sensitivity_level,
    lifecycle_status = excluded.lifecycle_status,
    searchable = excluded.searchable,
    updated_at = CURRENT_TIMESTAMP`,
		doc.ID,
		doc.PersonaID,
		string(doc.NodeType),
		doc.NodeID,
		doc.SearchText,
		string(doc.SearchTier),
		string(doc.VisibilityStatus),
		string(doc.SensitivityLevel),
		string(doc.LifecycleStatus),
		boolInt(doc.Searchable),
	)
	if err != nil {
		return err
	}
	return upsertSearchFTS(ctx, runner, doc)
}

func (r *SearchRepository) KeywordSearch(ctx context.Context, personaID string, query string, limit int) ([]core.SearchDocument, error) {
	if limit <= 0 {
		limit = 10
	}
	like := "%" + strings.TrimSpace(query) + "%"
	rows, err := r.db.QueryContext(ctx, `
SELECT id, persona_id, node_type, node_id, search_text, search_tier,
       visibility_status, sensitivity_level, lifecycle_status, searchable
FROM memory_search_documents
WHERE persona_id = ?
  AND search_text LIKE ?
  AND visibility_status = 'visible'
  AND searchable = 1
ORDER BY updated_at DESC
LIMIT ?`, personaID, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSearchDocuments(rows)
}

func (r *SearchRepository) keywordSearchForRetrieval(ctx context.Context, personaID string, query string, limit int, policy RetrievalPolicy) ([]core.SearchDocument, error) {
	if limit <= 0 {
		limit = 10
	}
	trimmed := strings.TrimSpace(query)
	terms := keywordSearchTerms(trimmed)
	if len(terms) == 0 {
		return nil, nil
	}
	whereClauses := make([]string, 0, len(terms))
	whereArgs := make([]any, 0, len(terms))
	scoreClauses := []string{fmt.Sprintf("CASE WHEN d.search_text LIKE ? THEN %d ELSE 0 END", len(terms)+1)}
	scoreArgs := []any{"%" + trimmed + "%"}
	for _, term := range terms {
		like := "%" + term + "%"
		whereClauses = append(whereClauses, "d.search_text LIKE ?")
		whereArgs = append(whereArgs, like)
		scoreClauses = append(scoreClauses, "CASE WHEN d.search_text LIKE ? THEN 1 ELSE 0 END")
		scoreArgs = append(scoreArgs, like)
	}
	args := make([]any, 0, 1+len(whereArgs)+3+len(scoreArgs)+1)
	args = append(args, personaID)
	args = append(args, whereArgs...)
	args = append(args,
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowHistorical),
		boolInt(policy.AllowDeepArchive),
	)
	args = append(args, scoreArgs...)
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, `
SELECT d.id, d.persona_id, d.node_type, d.node_id, d.search_text, d.search_tier,
       d.visibility_status, d.sensitivity_level, d.lifecycle_status, d.searchable
FROM memory_search_documents d
JOIN facts facts
  ON facts.persona_id = d.persona_id
 AND facts.id = d.node_id
WHERE d.persona_id = ?
  AND (`+strings.Join(whereClauses, " OR ")+`)
  AND d.node_type = 'fact'
  AND d.visibility_status = 'visible'
  AND d.searchable = 1
  AND facts.visibility_status = 'visible'
  AND facts.searchable = 1
  AND (facts.validity_status != 'invalidated' OR ? = 1)
  AND (facts.lifecycle_status != 'archived' OR ? = 1)
  AND (facts.lifecycle_status != 'deep_archived' OR ? = 1)
ORDER BY (`+strings.Join(scoreClauses, " + ")+`) DESC, d.updated_at DESC, d.node_id ASC
LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSearchDocuments(rows)
}

func keywordSearchTerms(query string) []string {
	fields := strings.Fields(query)
	if len(fields) == 0 && query != "" {
		fields = []string{query}
	}
	seen := map[string]struct{}{}
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
	}
	return terms
}

func scanSearchDocuments(rows *sql.Rows) ([]core.SearchDocument, error) {
	var docs []core.SearchDocument
	for rows.Next() {
		doc, err := scanSearchDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return docs, nil
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type searchDocumentScanner interface {
	Scan(dest ...any) error
}

func scanSearchDocument(row searchDocumentScanner) (core.SearchDocument, error) {
	var doc core.SearchDocument
	var searchable int
	if err := row.Scan(
		&doc.ID,
		&doc.PersonaID,
		&doc.NodeType,
		&doc.NodeID,
		&doc.SearchText,
		&doc.SearchTier,
		&doc.VisibilityStatus,
		&doc.SensitivityLevel,
		&doc.LifecycleStatus,
		&searchable,
	); err != nil {
		return core.SearchDocument{}, err
	}
	doc.Searchable = intBool(searchable)
	return doc, nil
}

func normalizeSearchDocument(doc core.SearchDocument) core.SearchDocument {
	if doc.SearchTier == "" {
		doc.SearchTier = core.SearchTierHot
	}
	if doc.VisibilityStatus == "" {
		doc.VisibilityStatus = core.VisibilityVisible
		doc.Searchable = true
	}
	doc.SensitivityLevel = defaultSensitivity(doc.SensitivityLevel)
	doc.LifecycleStatus = defaultLifecycle(doc.LifecycleStatus)
	return doc
}

func upsertFactSearchDocumentTx(ctx context.Context, tx *sql.Tx, personaID string, factID string) error {
	doc, err := buildFactSearchDocument(ctx, tx, personaID, factID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return deleteSearchDocument(ctx, tx, personaID, core.NodeTypeFact, factID)
		}
		return err
	}
	if !isSearchDocumentVisibleAndSearchable(doc) {
		return deleteSearchDocument(ctx, tx, personaID, core.NodeTypeFact, factID)
	}
	return upsertSearchDocument(ctx, tx, doc)
}

func upsertNarrativeSearchDocumentTx(ctx context.Context, runner sqlRunner, personaID string, narrativeID string) error {
	doc, err := buildNarrativeSearchDocument(ctx, runner, personaID, narrativeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return deleteSearchDocument(ctx, runner, personaID, core.NodeTypeNarrative, narrativeID)
		}
		return err
	}
	if !isSearchDocumentVisibleAndSearchable(doc) {
		return deleteSearchDocument(ctx, runner, personaID, core.NodeTypeNarrative, narrativeID)
	}
	return upsertSearchDocument(ctx, runner, doc)
}

func upsertInsightSearchDocumentTx(ctx context.Context, runner sqlRunner, personaID string, insightID string) error {
	doc, err := buildInsightSearchDocument(ctx, runner, personaID, insightID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return deleteSearchDocument(ctx, runner, personaID, core.NodeTypeInsight, insightID)
		}
		return err
	}
	if !isSearchDocumentVisibleAndSearchable(doc) {
		return deleteSearchDocument(ctx, runner, personaID, core.NodeTypeInsight, insightID)
	}
	return upsertSearchDocument(ctx, runner, doc)
}

func buildFactSearchDocument(ctx context.Context, runner sqlRunner, personaID string, factID string) (core.SearchDocument, error) {
	var doc core.SearchDocument
	var objectLiteral, objectEntityName sql.NullString
	var predicate string
	var searchable int
	err := runner.QueryRowContext(ctx, `
SELECT f.persona_id, f.id, f.content_summary, f.predicate,
       f.object_literal, oe.canonical_name,
       f.visibility_status, f.sensitivity_level, f.lifecycle_status, f.searchable
FROM facts f
LEFT JOIN entities oe
  ON oe.persona_id = f.persona_id
 AND oe.id = f.object_entity_id
WHERE f.persona_id = ? AND f.id = ?`, personaID, factID).Scan(
		&doc.PersonaID,
		&doc.NodeID,
		&doc.SearchText,
		&predicate,
		&objectLiteral,
		&objectEntityName,
		&doc.VisibilityStatus,
		&doc.SensitivityLevel,
		&doc.LifecycleStatus,
		&searchable,
	)
	if err != nil {
		return core.SearchDocument{}, err
	}
	eligible, err := factSearchEvidenceEligible(ctx, runner, personaID, factID)
	if err != nil {
		return core.SearchDocument{}, err
	}
	if !eligible {
		return core.SearchDocument{}, sql.ErrNoRows
	}
	doc.ID = fmt.Sprintf("search_%s", factID)
	doc.NodeType = core.NodeTypeFact
	doc.SearchTier = searchTierForLifecycle(doc.LifecycleStatus)
	doc.SearchText = strings.Join(nonEmptyStrings(
		doc.SearchText,
		predicate,
		stringPtrValue(objectLiteral),
		stringPtrValue(objectEntityName),
	), " ")
	doc.Searchable = intBool(searchable)
	return doc, nil
}

func buildNarrativeSearchDocument(ctx context.Context, runner sqlRunner, personaID string, narrativeID string) (core.SearchDocument, error) {
	var doc core.SearchDocument
	var scope string
	var scopeRef, emotionalTone sql.NullString
	var searchable int
	err := runner.QueryRowContext(ctx, `
SELECT persona_id, id, summary, scope, scope_ref, emotional_tone,
       visibility_status, sensitivity_level, lifecycle_status, searchable
FROM narratives
WHERE persona_id = ? AND id = ?`, personaID, narrativeID).Scan(
		&doc.PersonaID,
		&doc.NodeID,
		&doc.SearchText,
		&scope,
		&scopeRef,
		&emotionalTone,
		&doc.VisibilityStatus,
		&doc.SensitivityLevel,
		&doc.LifecycleStatus,
		&searchable,
	)
	if err != nil {
		return core.SearchDocument{}, err
	}
	doc.ID = fmt.Sprintf("search_narrative_%s", narrativeID)
	doc.NodeType = core.NodeTypeNarrative
	doc.SearchTier = searchTierForLifecycle(doc.LifecycleStatus)
	doc.SearchText = strings.Join(nonEmptyStrings(
		doc.SearchText,
		scope,
		stringPtrValue(scopeRef),
		stringPtrValue(emotionalTone),
	), " ")
	doc.Searchable = intBool(searchable)
	return doc, nil
}

func buildInsightSearchDocument(ctx context.Context, runner sqlRunner, personaID string, insightID string) (core.SearchDocument, error) {
	var doc core.SearchDocument
	var insightType string
	var searchable int
	err := runner.QueryRowContext(ctx, `
SELECT persona_id, id, content, insight_type,
       visibility_status, sensitivity_level, lifecycle_status, searchable
FROM insights
WHERE persona_id = ? AND id = ?`, personaID, insightID).Scan(
		&doc.PersonaID,
		&doc.NodeID,
		&doc.SearchText,
		&insightType,
		&doc.VisibilityStatus,
		&doc.SensitivityLevel,
		&doc.LifecycleStatus,
		&searchable,
	)
	if err != nil {
		return core.SearchDocument{}, err
	}
	doc.ID = fmt.Sprintf("search_insight_%s", insightID)
	doc.NodeType = core.NodeTypeInsight
	doc.SearchTier = searchTierForLifecycle(doc.LifecycleStatus)
	doc.SearchText = strings.Join(nonEmptyStrings(doc.SearchText, insightType), " ")
	doc.Searchable = intBool(searchable)
	return doc, nil
}

func factSearchEvidenceEligible(ctx context.Context, runner sqlRunner, personaID string, factID string) (bool, error) {
	var evidenceCount int
	var visibleEvidenceCount int
	err := runner.QueryRowContext(ctx, `
SELECT COUNT(*),
       COALESCE(SUM(CASE WHEN e.visibility_status = 'visible' AND e.searchable = 1 THEN 1 ELSE 0 END), 0)
FROM memory_links l
JOIN episodes e
  ON e.persona_id = l.persona_id
 AND e.id = l.to_node_id
WHERE l.persona_id = ?
  AND l.from_node_type = 'fact'
  AND l.from_node_id = ?
  AND l.link_type = 'EVIDENCED_BY'
  AND l.to_node_type = 'episode'`, personaID, factID).Scan(&evidenceCount, &visibleEvidenceCount)
	if err != nil {
		return false, err
	}
	return evidenceCount == 0 || visibleEvidenceCount > 0, nil
}

func isSearchDocumentVisibleAndSearchable(doc core.SearchDocument) bool {
	return doc.VisibilityStatus == core.VisibilityVisible && doc.Searchable
}

func searchTierForLifecycle(status core.LifecycleStatus) core.SearchTier {
	switch status {
	case core.LifecycleDormant, core.LifecycleConsolidated:
		return core.SearchTierWarm
	case core.LifecycleArchived:
		return core.SearchTierCold
	case core.LifecycleDeepArchived:
		return core.SearchTierDeepCold
	default:
		return core.SearchTierHot
	}
}

func deleteSearchDocument(ctx context.Context, runner sqlRunner, personaID string, nodeType core.NodeType, nodeID string) error {
	_, err := runner.ExecContext(ctx, `
DELETE FROM memory_search_documents
WHERE persona_id = ? AND node_type = ? AND node_id = ?`, personaID, string(nodeType), nodeID)
	if err != nil {
		return err
	}
	return deleteSearchFTS(ctx, runner, personaID, nodeType, nodeID)
}

func upsertSearchFTS(ctx context.Context, runner sqlRunner, doc core.SearchDocument) error {
	if !isSearchDocumentVisibleAndSearchable(doc) {
		return deleteSearchFTS(ctx, runner, doc.PersonaID, doc.NodeType, doc.NodeID)
	}
	if ok, err := searchFTSExists(ctx, runner); err != nil || !ok {
		if err != nil {
			return err
		}
		return nil
	}
	if err := deleteSearchFTS(ctx, runner, doc.PersonaID, doc.NodeType, doc.NodeID); err != nil {
		return err
	}
	_, err := runner.ExecContext(ctx, `
INSERT INTO memory_search_fts (search_text, persona_id, node_type, node_id)
VALUES (?, ?, ?, ?)`,
		doc.SearchText,
		doc.PersonaID,
		string(doc.NodeType),
		doc.NodeID,
	)
	if isSearchIndexUnavailable(err) {
		return nil
	}
	return err
}

func deleteSearchFTS(ctx context.Context, runner sqlRunner, personaID string, nodeType core.NodeType, nodeID string) error {
	if ok, err := searchFTSExists(ctx, runner); err != nil || !ok {
		if err != nil {
			return err
		}
		return nil
	}
	return rebuildSearchFTS(ctx, runner, &searchDocumentKey{
		personaID: personaID,
		nodeType:  nodeType,
		nodeID:    nodeID,
	})
}

type searchDocumentKey struct {
	personaID string
	nodeType  core.NodeType
	nodeID    string
}

func rebuildSearchFTS(ctx context.Context, runner sqlRunner, exclude *searchDocumentKey) error {
	if ok, err := searchFTSExists(ctx, runner); err != nil || !ok {
		if err != nil {
			return err
		}
		return nil
	}
	for _, table := range []string{
		"memory_search_fts",
		"memory_search_fts_data",
		"memory_search_fts_idx",
		"memory_search_fts_content",
		"memory_search_fts_docsize",
		"memory_search_fts_config",
	} {
		if _, err := runner.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			if isMissingSearchIndex(err) {
				return nil
			}
			return err
		}
	}
	if _, err := runner.ExecContext(ctx, `
CREATE VIRTUAL TABLE IF NOT EXISTS memory_search_fts USING fts5(
    search_text,
    persona_id UNINDEXED,
    node_type UNINDEXED,
    node_id UNINDEXED,
    tokenize = 'unicode61'
)`); err != nil {
		if isSearchIndexUnavailable(err) {
			return nil
		}
		return err
	}
	query := `
INSERT INTO memory_search_fts (search_text, persona_id, node_type, node_id)
SELECT search_text, persona_id, node_type, node_id
FROM memory_search_documents
WHERE visibility_status = 'visible'
  AND searchable = 1`
	args := []any{}
	if exclude != nil {
		query += `
  AND NOT (persona_id = ? AND node_type = ? AND node_id = ?)`
		args = append(args, exclude.personaID, string(exclude.nodeType), exclude.nodeID)
	}
	_, err := runner.ExecContext(ctx, query, args...)
	if isSearchIndexUnavailable(err) {
		return nil
	}
	return err
}

func isMissingSearchIndex(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "fts5")
}

func searchFTSExists(ctx context.Context, runner sqlRunner) (bool, error) {
	var count int
	if err := runner.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM sqlite_master
WHERE name = 'memory_search_fts'
  AND type IN ('table', 'virtual table')`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func ftsQuery(query string) string {
	terms := strings.Fields(strings.TrimSpace(query))
	if len(terms) == 0 {
		return strings.TrimSpace(query)
	}
	return strings.Join(terms, " OR ")
}

func isSearchIndexUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "fts5") ||
		strings.Contains(msg, "virtual table") ||
		strings.Contains(msg, "malformed match")
}

func stringPtrValue(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func nonEmptyStrings(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}
