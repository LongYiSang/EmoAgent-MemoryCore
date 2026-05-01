-- 0004_memory_search_fallback_optional.sql
-- Optional SQLite fallback search layer. Use this when TriviumDB/Python Sidecar is unavailable.
-- Requires SQLite FTS5 for memory_search_fts. If FTS5 is not available, keep only memory_search_documents.

PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS memory_search_documents (
    id                 TEXT PRIMARY KEY,
    persona_id         TEXT NOT NULL,
    node_type          TEXT NOT NULL
        CHECK (node_type IN ('episode', 'entity', 'fact', 'narrative', 'insight')),
    node_id            TEXT NOT NULL,
    search_text        TEXT NOT NULL,
    search_tier        TEXT NOT NULL DEFAULT 'hot'
        CHECK (search_tier IN ('hot', 'warm', 'cold', 'deep_cold')),
    visibility_status  TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible', 'hidden', 'forgotten', 'purged')),
    sensitivity_level  TEXT NOT NULL DEFAULT 'normal'
        CHECK (sensitivity_level IN ('normal', 'sensitive', 'highly_sensitive')),
    lifecycle_status   TEXT NOT NULL DEFAULT 'active'
        CHECK (lifecycle_status IN ('active', 'dormant', 'consolidated', 'archived', 'deep_archived')),
    searchable         INTEGER NOT NULL DEFAULT 1
        CHECK (searchable IN (0, 1)),
    updated_at         TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    UNIQUE (persona_id, node_type, node_id)
);

CREATE INDEX IF NOT EXISTS idx_memory_search_documents_node
    ON memory_search_documents(persona_id, node_type, node_id);
CREATE INDEX IF NOT EXISTS idx_memory_search_documents_visible
    ON memory_search_documents(persona_id, search_tier, updated_at DESC)
    WHERE visibility_status = 'visible' AND searchable = 1;

CREATE VIRTUAL TABLE IF NOT EXISTS memory_search_fts USING fts5(
    search_text,
    persona_id UNINDEXED,
    node_type UNINDEXED,
    node_id UNINDEXED,
    tokenize = 'unicode61'
);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0004', 'Optional SQLite fallback search document table and FTS5 index');
