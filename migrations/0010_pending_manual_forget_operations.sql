CREATE TABLE IF NOT EXISTS pending_manual_forget_operations (
    id                    TEXT PRIMARY KEY,
    persona_id            TEXT NOT NULL,
    session_id            TEXT,
    chat_session_id       TEXT,
    request_episode_id    TEXT,
    status                TEXT NOT NULL
        CHECK (status IN ('pending_confirmation', 'executed', 'cancelled', 'failed')),
    requested_level       TEXT NOT NULL
        CHECK (requested_level IN ('soft_forget', 'hard_forget', 'source_redact', 'purge')),
    scope_mode            TEXT,
    requires_confirmation INTEGER NOT NULL DEFAULT 0
        CHECK (requires_confirmation IN (0, 1)),
    candidates_json       TEXT NOT NULL,
    confirmation_policy_json TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    expires_at            TEXT NOT NULL,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL,
    FOREIGN KEY (request_episode_id) REFERENCES episodes(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_pending_manual_forget_lookup
    ON pending_manual_forget_operations(persona_id, session_id, chat_session_id, status, expires_at);

CREATE INDEX IF NOT EXISTS idx_pending_manual_forget_updated
    ON pending_manual_forget_operations(persona_id, updated_at);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0010', 'Pending manual forget operations');
