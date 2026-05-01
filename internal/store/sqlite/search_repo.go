package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/longyisang/emoagent-memorycore/internal/core"
)

type SearchRepository struct {
	db *sql.DB
}

func NewSearchRepository(db *sql.DB) *SearchRepository {
	return &SearchRepository{db: db}
}

func (r *SearchRepository) UpsertDocument(ctx context.Context, doc core.SearchDocument) error {
	doc = normalizeSearchDocument(doc)
	_, err := r.db.ExecContext(ctx, `
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
	return err
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
  AND lifecycle_status IN ('active', 'dormant', 'consolidated')
ORDER BY updated_at DESC
LIMIT ?`, personaID, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
