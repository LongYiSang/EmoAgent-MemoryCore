CREATE TABLE IF NOT EXISTS memory_natural_states (
    persona_id                  TEXT NOT NULL,
    node_type                   TEXT NOT NULL
        CHECK (node_type IN ('fact', 'narrative', 'insight')),
    node_id                     TEXT NOT NULL,
    algorithm_version           TEXT NOT NULL DEFAULT 'natural_power_sleep_v1',
    natural_strength            REAL NOT NULL DEFAULT 1.0
        CHECK (natural_strength >= 0.0 AND natural_strength <= 1.5),
    retrievability              REAL NOT NULL DEFAULT 1.0
        CHECK (retrievability >= 0.0 AND retrievability <= 1.5),
    stability_days              REAL NOT NULL DEFAULT 1.0
        CHECK (stability_days >= 0.0),
    decay_exponent              REAL NOT NULL DEFAULT 0.6
        CHECK (decay_exponent >= 0.0),
    natural_state               TEXT NOT NULL DEFAULT 'salient'
        CHECK (natural_state IN ('salient', 'available', 'latent', 'faded', 'sleep_consolidated')),
    last_simulated_at           TEXT,
    last_reactivated_at         TEXT,
    last_strengthened_at        TEXT,
    first_sleep_consolidated    INTEGER NOT NULL DEFAULT 0
        CHECK (first_sleep_consolidated IN (0, 1)),
    reactivation_count          INTEGER NOT NULL DEFAULT 0
        CHECK (reactivation_count >= 0),
    protected_reason            TEXT,
    emotion_salience_hint       REAL NOT NULL DEFAULT 0.0
        CHECK (emotion_salience_hint >= 0.0 AND emotion_salience_hint <= 1.0),
    emotion_persistence_hint    REAL NOT NULL DEFAULT 0.0
        CHECK (emotion_persistence_hint >= 0.0 AND emotion_persistence_hint <= 1.0),
    score_breakdown_json        TEXT,
    updated_at                  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (persona_id, node_type, node_id)
);

CREATE TABLE IF NOT EXISTS memory_natural_events (
    id                          TEXT PRIMARY KEY,
    run_id                      TEXT,
    persona_id                  TEXT NOT NULL,
    node_type                   TEXT NOT NULL,
    node_id                     TEXT NOT NULL,
    event_type                  TEXT NOT NULL
        CHECK (event_type IN (
            'scored',
            'protected',
            'decayed',
            'reactivated',
            'first_sleep_consolidated',
            'natural_state_changed',
            'search_tier_updated',
            'compression_candidate_emitted',
            'storage_rewarm_candidate',
            'skipped'
        )),
    from_natural_state          TEXT,
    to_natural_state            TEXT,
    from_search_tier            TEXT,
    to_search_tier              TEXT,
    natural_strength            REAL,
    retrievability              REAL,
    stability_days              REAL,
    decay_exponent              REAL,
    reactivation_score          REAL,
    reason_codes_json           TEXT,
    safe_reason_summary         TEXT,
    created_at                  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS memory_natural_runs (
    id                          TEXT PRIMARY KEY,
    persona_id                  TEXT NOT NULL,
    run_kind                    TEXT NOT NULL
        CHECK (run_kind IN ('sleep_cycle', 'manual', 'api', 'test')),
    algorithm_version           TEXT NOT NULL DEFAULT 'natural_power_sleep_v1',
    local_date                  TEXT,
    local_time                  TEXT,
    timezone                    TEXT,
    dry_run                     INTEGER NOT NULL DEFAULT 0
        CHECK (dry_run IN (0, 1)),
    force                       INTEGER NOT NULL DEFAULT 0
        CHECK (force IN (0, 1)),
    mark_sleep_cycle            INTEGER NOT NULL DEFAULT 0
        CHECK (mark_sleep_cycle IN (0, 1)),
    started_at                  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at                TEXT,
    status                      TEXT NOT NULL
        CHECK (status IN ('running', 'completed', 'failed', 'skipped')),
    evaluated_nodes             INTEGER NOT NULL DEFAULT 0,
    scored_nodes                INTEGER NOT NULL DEFAULT 0,
    protected_nodes             INTEGER NOT NULL DEFAULT 0,
    decayed_nodes               INTEGER NOT NULL DEFAULT 0,
    reactivated_nodes           INTEGER NOT NULL DEFAULT 0,
    first_sleep_consolidated_nodes INTEGER NOT NULL DEFAULT 0,
    search_tier_updates         INTEGER NOT NULL DEFAULT 0,
    search_documents_created    INTEGER NOT NULL DEFAULT 0,
    mirror_updates_enqueued     INTEGER NOT NULL DEFAULT 0,
    compression_candidates      INTEGER NOT NULL DEFAULT 0,
    narratives_created          INTEGER NOT NULL DEFAULT 0,
    insights_created            INTEGER NOT NULL DEFAULT 0,
    config_hash                 TEXT,
    error_message               TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_natural_runs_once_per_day
ON memory_natural_runs(persona_id, run_kind, local_date)
WHERE run_kind = 'sleep_cycle' AND status = 'completed' AND force = 0;

CREATE TABLE IF NOT EXISTS memory_natural_compression_candidates (
    id                          TEXT PRIMARY KEY,
    run_id                      TEXT,
    persona_id                  TEXT NOT NULL,
    cluster_key                 TEXT NOT NULL,
    target_node_type            TEXT NOT NULL
        CHECK (target_node_type IN ('narrative', 'insight')),
    source_refs_json            TEXT NOT NULL,
    candidate_summary           TEXT,
    avg_retrievability          REAL,
    avg_importance              REAL,
    min_confidence              REAL,
    status                      TEXT NOT NULL DEFAULT 'emitted'
        CHECK (status IN ('emitted', 'applied', 'rejected', 'skipped')),
    reason_codes_json           TEXT,
    created_at                  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                  TEXT
);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0010_natural_memory_dynamics', 'Natural Memory Dynamics v1 state, events, runs, and compression candidates');
