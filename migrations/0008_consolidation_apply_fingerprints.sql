CREATE TABLE IF NOT EXISTS consolidation_apply_fingerprints (
    persona_id   TEXT NOT NULL,
    fingerprint  TEXT NOT NULL,
    fact_id      TEXT,
    candidate_id TEXT,
    request_id   TEXT,
    session_id   TEXT,
    action       TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (persona_id, fingerprint),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_consolidation_apply_fingerprints_fact
    ON consolidation_apply_fingerprints(persona_id, fact_id);

CREATE INDEX IF NOT EXISTS idx_consolidation_apply_fingerprints_session
    ON consolidation_apply_fingerprints(persona_id, session_id, created_at);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0008', 'Consolidation apply fingerprints for repeat-run idempotency');
