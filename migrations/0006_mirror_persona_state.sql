-- 0006_mirror_persona_state.sql
-- Persona-level mirror readiness gate for rebuild and retrieval fallback.

CREATE TABLE IF NOT EXISTS mirror_persona_state (
    persona_id   TEXT PRIMARY KEY,
    state        TEXT NOT NULL DEFAULT 'ready'
        CHECK (state IN ('ready', 'rebuilding', 'degraded')),
    reason       TEXT,
    updated_at   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_mirror_persona_state_state
    ON mirror_persona_state(state, updated_at DESC);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0006', 'Persona-level mirror rebuild readiness and degraded-state tracking');
