CREATE TABLE IF NOT EXISTS consolidation_session_fact_writes (
    persona_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    fact_id    TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (persona_id, session_id, fact_id),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_consolidation_session_fact_writes_fact
    ON consolidation_session_fact_writes(persona_id, fact_id);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0009', 'Session-scoped consolidation fact write idempotency');
