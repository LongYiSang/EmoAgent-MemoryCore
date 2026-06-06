DROP INDEX IF EXISTS idx_pending_manual_forget_lookup;
DROP INDEX IF EXISTS idx_pending_manual_forget_updated;

CREATE TABLE pending_manual_forget_operations_v2 (
    id                    TEXT PRIMARY KEY,
    persona_id            TEXT NOT NULL,
    session_id            TEXT,
    chat_session_id       TEXT,
    request_episode_id    TEXT,
    status                TEXT NOT NULL
        CHECK (status IN ('pending_confirmation', 'executed', 'cancelled', 'failed', 'expired')),
    requested_level       TEXT NOT NULL
        CHECK (requested_level IN ('soft_forget', 'hard_forget', 'source_redact', 'purge')),
    scope_mode            TEXT,
    requires_confirmation INTEGER NOT NULL DEFAULT 0
        CHECK (requires_confirmation IN (0, 1)),
    candidates_json       TEXT NOT NULL,
    confirmation_policy_json TEXT,
    preview_hash          TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL,
    expires_at            TEXT NOT NULL,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL,
    FOREIGN KEY (request_episode_id) REFERENCES episodes(id) ON DELETE SET NULL
);

INSERT INTO pending_manual_forget_operations_v2 (
    id, persona_id, session_id, chat_session_id, request_episode_id, status,
    requested_level, scope_mode, requires_confirmation, candidates_json,
    confirmation_policy_json, preview_hash, created_at, updated_at, expires_at
)
SELECT
    id, persona_id, session_id, chat_session_id, request_episode_id, status,
    requested_level, scope_mode, requires_confirmation, candidates_json,
    confirmation_policy_json, NULL, created_at, updated_at, expires_at
FROM pending_manual_forget_operations;

DROP TABLE pending_manual_forget_operations;
ALTER TABLE pending_manual_forget_operations_v2 RENAME TO pending_manual_forget_operations;

CREATE INDEX IF NOT EXISTS idx_pending_manual_forget_lookup
    ON pending_manual_forget_operations(persona_id, session_id, chat_session_id, status, expires_at);

CREATE INDEX IF NOT EXISTS idx_pending_manual_forget_updated
    ON pending_manual_forget_operations(persona_id, updated_at);
