-- 0012_memory_delta_curation.sql
-- Audit and checkpoint tables for Delta Memory Curation.

PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS memory_curation_checkpoints (
    persona_id                 TEXT PRIMARY KEY,
    last_successful_run_id      TEXT,
    cursor_created_at           TEXT,
    cursor_fact_id              TEXT,
    updated_at                  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS memory_curation_runs (
    id                          TEXT PRIMARY KEY,
    persona_id                  TEXT NOT NULL,
    mode                        TEXT NOT NULL
        CHECK (mode IN ('dry_run', 'apply')),
    trigger                     TEXT NOT NULL
        CHECK (trigger IN ('manual', 'scheduled', 'cli', 'test')),
    status                      TEXT NOT NULL
        CHECK (status IN ('running', 'succeeded', 'partially_failed', 'failed', 'skipped')),

    cursor_from_created_at       TEXT,
    cursor_from_fact_id          TEXT,
    cursor_to_created_at         TEXT,
    cursor_to_fact_id            TEXT,

    new_fact_count               INTEGER NOT NULL DEFAULT 0,
    group_count                  INTEGER NOT NULL DEFAULT 0,
    llm_group_count              INTEGER NOT NULL DEFAULT 0,
    applied_group_count          INTEGER NOT NULL DEFAULT 0,
    review_group_count           INTEGER NOT NULL DEFAULT 0,
    noop_group_count             INTEGER NOT NULL DEFAULT 0,
    error_count                  INTEGER NOT NULL DEFAULT 0,

    provider_id                  TEXT,
    provider_kind                TEXT,
    model                       TEXT,
    usage_json                   TEXT,

    sanitized_error_code          TEXT,
    sanitized_error_message       TEXT,

    started_at                   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at                  TEXT,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS memory_curation_groups (
    id                          TEXT PRIMARY KEY,
    run_id                      TEXT NOT NULL,
    persona_id                  TEXT NOT NULL,

    group_status                TEXT NOT NULL
        CHECK (group_status IN ('planned', 'applied', 'noop', 'needs_review', 'failed')),

    decision                    TEXT NOT NULL
        CHECK (decision IN (
            'no_op',
            'reinforce_existing',
            'merge_into_existing',
            'create_canonical_fact',
            'coexist_related',
            'conflict_needs_review',
            'needs_review'
        )),

    semantic_relation            TEXT NOT NULL
        CHECK (semantic_relation IN (
            'same',
            'refinement',
            'overlap',
            'complement',
            'distinct',
            'conflict',
            'unclear'
        )),

    answer_gain                  TEXT NOT NULL
        CHECK (answer_gain IN ('none', 'small', 'material', 'unknown')),

    confidence                   REAL NOT NULL DEFAULT 0.0
        CHECK (confidence >= 0.0 AND confidence <= 1.0),

    canonical_fact_id            TEXT,
    merged_content_summary       TEXT,
    canonical_subject_entity_id  TEXT,
    canonical_predicate          TEXT,
    canonical_object_literal     TEXT,
    canonical_object_entity_id   TEXT,
    fact_type                    TEXT,

    reason_codes_json            TEXT,
    llm_response_hash            TEXT,
    sanitized_error_code          TEXT,
    sanitized_error_message       TEXT,

    created_at                   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                   TEXT,

    FOREIGN KEY (run_id) REFERENCES memory_curation_runs(id) ON DELETE CASCADE,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS memory_curation_group_facts (
    id                          TEXT PRIMARY KEY,
    group_id                    TEXT NOT NULL,
    persona_id                  TEXT NOT NULL,
    fact_id                     TEXT NOT NULL,
    role                        TEXT NOT NULL
        CHECK (role IN ('new_delta', 'existing_candidate', 'canonical', 'source')),
    latest_evidence_at           TEXT,
    created_at                   TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (group_id) REFERENCES memory_curation_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (fact_id) REFERENCES facts(id) ON DELETE CASCADE,
    UNIQUE (group_id, fact_id, role)
);

CREATE INDEX IF NOT EXISTS idx_memory_curation_runs_persona_started
    ON memory_curation_runs(persona_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_curation_groups_run
    ON memory_curation_groups(run_id, group_status);
CREATE INDEX IF NOT EXISTS idx_memory_curation_group_facts_group
    ON memory_curation_group_facts(group_id, role);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0012', 'Delta Memory Curation checkpoints, runs, groups and group fact audit');
