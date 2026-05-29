-- Semantic operation metadata and safe decision audit.

CREATE TABLE IF NOT EXISTS semantic_mirror_meta (
    id                    TEXT PRIMARY KEY,
    persona_id            TEXT NOT NULL,
    node_type             TEXT NOT NULL,
    node_id               TEXT NOT NULL,
    doc_kind              TEXT NOT NULL,
    mirror_node_id         TEXT,
    content_fingerprint   TEXT NOT NULL,
    text_hash             TEXT NOT NULL,
    embedding_model       TEXT,
    embedding_dims        INTEGER,
    embedding_version     TEXT,
    index_status          TEXT NOT NULL DEFAULT 'pending',
    indexed_at            TEXT,
    deleted_at            TEXT,
    updated_at            TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(persona_id, node_type, node_id, doc_kind)
);

CREATE TABLE IF NOT EXISTS semantic_decision_audit (
    id                  TEXT PRIMARY KEY,
    request_id           TEXT NOT NULL,
    persona_id           TEXT NOT NULL,
    decision_type        TEXT NOT NULL,
    actor                TEXT NOT NULL DEFAULT 'system',
    reason_code          TEXT,
    candidate_hash       TEXT,
    selected_node_ids    TEXT,
    preview_hash         TEXT,
    policy_snapshot      TEXT,
    similarity_scores    TEXT,
    sidecar_status       TEXT NOT NULL,
    diagnostics_json     TEXT,
    created_at           TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0011', 'Semantic mirror metadata and decision audit');
