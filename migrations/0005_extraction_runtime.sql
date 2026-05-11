-- 0005_extraction_runtime.sql
-- Phase 2C sanitized extraction run audit and idempotency metadata.

CREATE TABLE IF NOT EXISTS extraction_runs (
    id                         TEXT PRIMARY KEY,
    request_id                 TEXT NOT NULL,
    persona_id                 TEXT NOT NULL,
    session_id                 TEXT,
    trigger                    TEXT NOT NULL,
    mode                       TEXT NOT NULL
        CHECK (mode IN ('validate', 'dry-run', 'apply')),
    status                     TEXT NOT NULL
        CHECK (status IN (
            'skipped', 'validated', 'dry_run', 'applied',
            'nothing_applied', 'blocked', 'failed', 'partially_failed'
        )),
    fingerprint                TEXT NOT NULL,
    provider_id                TEXT NOT NULL DEFAULT '',
    provider_kind              TEXT NOT NULL DEFAULT '',
    model                      TEXT,
    prompt_version             TEXT NOT NULL DEFAULT '',
    prefilter_prompt_version   TEXT NOT NULL DEFAULT '',
    repair_prompt_version      TEXT NOT NULL DEFAULT '',
    original_episode_count     INTEGER NOT NULL DEFAULT 0,
    kept_episode_count         INTEGER NOT NULL DEFAULT 0,
    skipped_episode_count      INTEGER NOT NULL DEFAULT 0,
    accepted_count             INTEGER NOT NULL DEFAULT 0,
    review_count               INTEGER NOT NULL DEFAULT 0,
    rejected_count             INTEGER NOT NULL DEFAULT 0,
    routed_count               INTEGER NOT NULL DEFAULT 0,
    not_applied_count          INTEGER NOT NULL DEFAULT 0,
    applied_count              INTEGER NOT NULL DEFAULT 0,
    failure_count              INTEGER NOT NULL DEFAULT 0,
    prompt_hash                TEXT,
    response_hash              TEXT,
    repaired_response_hash     TEXT,
    prefilter_hash             TEXT,
    usage_prompt_tokens        INTEGER NOT NULL DEFAULT 0,
    usage_completion_tokens    INTEGER NOT NULL DEFAULT 0,
    usage_total_tokens         INTEGER NOT NULL DEFAULT 0,
    latency_ms                 INTEGER NOT NULL DEFAULT 0,
    duration_ms                INTEGER NOT NULL DEFAULT 0,
    sanitized_error_code       TEXT,
    sanitized_error_message    TEXT,
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_extraction_runs_fingerprint_mode_status
    ON extraction_runs(fingerprint, mode, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_extraction_runs_persona_status
    ON extraction_runs(persona_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_extraction_runs_session
    ON extraction_runs(persona_id, session_id, trigger, created_at DESC);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0005', 'Phase 2C sanitized extraction runtime audit and idempotency metadata');
