-- 0003_memory_affect_audit.sql
-- User/relationship mood, Agent Affect placeholders, deletion audit and access/fatigue events.

PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS mood_states (
    id             TEXT PRIMARY KEY,
    persona_id     TEXT NOT NULL,
    scope          TEXT NOT NULL DEFAULT 'user'
        CHECK (scope IN ('user', 'relationship', 'conversation')),
    valence        REAL NOT NULL
        CHECK (valence >= -1.0 AND valence <= 1.0),
    arousal        REAL NOT NULL
        CHECK (arousal >= 0.0 AND arousal <= 1.0),
    confidence     REAL NOT NULL DEFAULT 0.5
        CHECK (confidence >= 0.0 AND confidence <= 1.0),
    label          TEXT,
    updated_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS affect_events (
    id                 TEXT PRIMARY KEY,
    persona_id         TEXT NOT NULL,
    scope              TEXT NOT NULL DEFAULT 'user'
        CHECK (scope IN ('user', 'relationship', 'conversation')),
    trigger_type       TEXT NOT NULL
        CHECK (trigger_type IN ('user_message', 'memory_recall', 'idle_check', 'plugin', 'work_report', 'system_event')),
    trigger_ref        TEXT,
    mood_before_v      REAL CHECK (mood_before_v IS NULL OR (mood_before_v >= -1.0 AND mood_before_v <= 1.0)),
    mood_before_a      REAL CHECK (mood_before_a IS NULL OR (mood_before_a >= 0.0 AND mood_before_a <= 1.0)),
    mood_after_v       REAL CHECK (mood_after_v IS NULL OR (mood_after_v >= -1.0 AND mood_after_v <= 1.0)),
    mood_after_a       REAL CHECK (mood_after_a IS NULL OR (mood_after_a >= 0.0 AND mood_after_a <= 1.0)),
    delta_v            REAL CHECK (delta_v IS NULL OR (delta_v >= -2.0 AND delta_v <= 2.0)),
    delta_a            REAL CHECK (delta_a IS NULL OR (delta_a >= -1.0 AND delta_a <= 1.0)),
    emotion_label      TEXT,
    significance       REAL NOT NULL DEFAULT 0.5
        CHECK (significance >= 0.0 AND significance <= 1.0),
    reasoning          TEXT,
    created_at         TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    visibility_status  TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible', 'hidden', 'forgotten', 'purged')),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS agent_affect_profiles (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    profile_name               TEXT NOT NULL DEFAULT 'default',
    baseline_valence           REAL NOT NULL DEFAULT 0.0
        CHECK (baseline_valence >= -1.0 AND baseline_valence <= 1.0),
    baseline_arousal           REAL NOT NULL DEFAULT 0.2
        CHECK (baseline_arousal >= 0.0 AND baseline_arousal <= 1.0),
    baseline_dominance         REAL NOT NULL DEFAULT 0.0
        CHECK (baseline_dominance >= -1.0 AND baseline_dominance <= 1.0),
    baseline_warmth            REAL NOT NULL DEFAULT 0.6
        CHECK (baseline_warmth >= 0.0 AND baseline_warmth <= 1.0),
    baseline_concern           REAL NOT NULL DEFAULT 0.3
        CHECK (baseline_concern >= 0.0 AND baseline_concern <= 1.0),
    baseline_playfulness       REAL NOT NULL DEFAULT 0.2
        CHECK (baseline_playfulness >= 0.0 AND baseline_playfulness <= 1.0),
    baseline_curiosity         REAL NOT NULL DEFAULT 0.3
        CHECK (baseline_curiosity >= 0.0 AND baseline_curiosity <= 1.0),
    inertia                    REAL NOT NULL DEFAULT 0.7
        CHECK (inertia >= 0.0 AND inertia <= 1.0),
    decay_half_life_seconds    INTEGER NOT NULL DEFAULT 1800
        CHECK (decay_half_life_seconds > 0),
    affect_policy_version      TEXT NOT NULL DEFAULT 'agent_affect_v0',
    expression_policy_version  TEXT NOT NULL DEFAULT 'expression_policy_v0',
    config_json                TEXT,
    visibility_status          TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible', 'hidden', 'purged')),
    searchable                 INTEGER NOT NULL DEFAULT 0
        CHECK (searchable IN (0, 1)),
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                 TEXT,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    UNIQUE (persona_id, profile_name)
);

CREATE TABLE IF NOT EXISTS agent_affect_states (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    session_id                 TEXT,
    profile_id                 TEXT,
    valence                    REAL NOT NULL DEFAULT 0.0
        CHECK (valence >= -1.0 AND valence <= 1.0),
    arousal                    REAL NOT NULL DEFAULT 0.2
        CHECK (arousal >= 0.0 AND arousal <= 1.0),
    dominance                  REAL NOT NULL DEFAULT 0.0
        CHECK (dominance >= -1.0 AND dominance <= 1.0),
    warmth                     REAL NOT NULL DEFAULT 0.0
        CHECK (warmth >= 0.0 AND warmth <= 1.0),
    concern                    REAL NOT NULL DEFAULT 0.0
        CHECK (concern >= 0.0 AND concern <= 1.0),
    curiosity                  REAL NOT NULL DEFAULT 0.0
        CHECK (curiosity >= 0.0 AND curiosity <= 1.0),
    playfulness                REAL NOT NULL DEFAULT 0.0
        CHECK (playfulness >= 0.0 AND playfulness <= 1.0),
    frustration                REAL NOT NULL DEFAULT 0.0
        CHECK (frustration >= 0.0 AND frustration <= 1.0),
    attachment                 REAL NOT NULL DEFAULT 0.0
        CHECK (attachment >= 0.0 AND attachment <= 1.0),
    uncertainty                REAL NOT NULL DEFAULT 0.0
        CHECK (uncertainty >= 0.0 AND uncertainty <= 1.0),
    label                      TEXT,
    confidence                 REAL NOT NULL DEFAULT 0.5
        CHECK (confidence >= 0.0 AND confidence <= 1.0),
    state_vector_json          TEXT,
    affect_policy_version      TEXT NOT NULL DEFAULT 'agent_affect_v0',
    expression_policy_version  TEXT NOT NULL DEFAULT 'expression_policy_v0',
    updated_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at                 TEXT,
    visibility_status          TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible', 'hidden', 'purged')),
    searchable                 INTEGER NOT NULL DEFAULT 0
        CHECK (searchable IN (0, 1)),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL,
    FOREIGN KEY (profile_id) REFERENCES agent_affect_profiles(id) ON DELETE SET NULL,
    CHECK (expires_at IS NULL OR expires_at >= updated_at)
);

CREATE TABLE IF NOT EXISTS agent_appraisals (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    session_id                 TEXT,
    trigger_type               TEXT NOT NULL
        CHECK (trigger_type IN (
            'user_message', 'memory_recall', 'relationship_event', 'safety_boundary',
            'self_reflection', 'work_report_summary', 'system_event'
        )),
    trigger_ref_type           TEXT,
    trigger_ref_id             TEXT,
    trigger_ref_hash           TEXT,
    target_node_type           TEXT,
    target_node_id             TEXT,
    appraisal_model            TEXT NOT NULL DEFAULT 'placeholder',
    appraisal_policy_version   TEXT NOT NULL DEFAULT 'agent_appraisal_v0',
    novelty                    REAL CHECK (novelty IS NULL OR (novelty >= 0.0 AND novelty <= 1.0)),
    goal_relevance             REAL CHECK (goal_relevance IS NULL OR (goal_relevance >= 0.0 AND goal_relevance <= 1.0)),
    relationship_impact        REAL CHECK (relationship_impact IS NULL OR (relationship_impact >= -1.0 AND relationship_impact <= 1.0)),
    boundary_violation         REAL CHECK (boundary_violation IS NULL OR (boundary_violation >= 0.0 AND boundary_violation <= 1.0)),
    controllability            REAL CHECK (controllability IS NULL OR (controllability >= 0.0 AND controllability <= 1.0)),
    uncertainty                REAL CHECK (uncertainty IS NULL OR (uncertainty >= 0.0 AND uncertainty <= 1.0)),
    expected_user_need         TEXT,
    safety_risk                REAL CHECK (safety_risk IS NULL OR (safety_risk >= 0.0 AND safety_risk <= 1.0)),
    appraisal_json             TEXT,
    confidence                 REAL NOT NULL DEFAULT 0.5
        CHECK (confidence >= 0.0 AND confidence <= 1.0),
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    visibility_status          TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible', 'hidden', 'purged')),
    searchable                 INTEGER NOT NULL DEFAULT 0
        CHECK (searchable IN (0, 1)),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS agent_affect_events (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    session_id                 TEXT,
    trigger_type               TEXT NOT NULL,
    trigger_ref_type           TEXT,
    trigger_ref_id             TEXT,
    trigger_ref_hash           TEXT,
    appraisal_id               TEXT,
    before_state_id            TEXT,
    after_state_id             TEXT,
    delta_json                 TEXT,
    label                      TEXT,
    significance               REAL NOT NULL DEFAULT 0.5
        CHECK (significance >= 0.0 AND significance <= 1.0),
    reason_summary             TEXT,
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    visibility_status          TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible', 'hidden', 'purged')),
    searchable                 INTEGER NOT NULL DEFAULT 0
        CHECK (searchable IN (0, 1)),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL,
    FOREIGN KEY (appraisal_id) REFERENCES agent_appraisals(id) ON DELETE SET NULL,
    FOREIGN KEY (before_state_id) REFERENCES agent_affect_states(id) ON DELETE SET NULL,
    FOREIGN KEY (after_state_id) REFERENCES agent_affect_states(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS agent_expression_decisions (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    session_id                 TEXT,
    message_id                 TEXT,
    agent_affect_state_id       TEXT,
    user_mood_state_id          TEXT,
    relationship_mood_state_id  TEXT,
    expression_policy_version   TEXT NOT NULL DEFAULT 'expression_policy_v0',
    guidance_json               TEXT NOT NULL,
    safety_overrides_json       TEXT,
    created_at                  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    visibility_status           TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible', 'hidden', 'purged')),
    searchable                  INTEGER NOT NULL DEFAULT 0
        CHECK (searchable IN (0, 1)),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL,
    FOREIGN KEY (agent_affect_state_id) REFERENCES agent_affect_states(id) ON DELETE SET NULL,
    FOREIGN KEY (user_mood_state_id) REFERENCES mood_states(id) ON DELETE SET NULL,
    FOREIGN KEY (relationship_mood_state_id) REFERENCES mood_states(id) ON DELETE SET NULL
);

CREATE TABLE IF NOT EXISTS deletion_events (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    session_id                 TEXT,
    request_episode_id          TEXT,
    deletion_level             TEXT NOT NULL
        CHECK (deletion_level IN ('soft_forget', 'hard_forget', 'source_redact', 'purge')),
    target_node_type            TEXT,
    target_node_id              TEXT,
    actor                       TEXT NOT NULL
        CHECK (actor IN ('user', 'system', 'admin')),
    reason_code                 TEXT NOT NULL
        CHECK (reason_code IN ('user_requested', 'retention_policy', 'safety', 'admin_policy')),
    scope_json                  TEXT,
    cascade_summary_json        TEXT,
    status                      TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    created_at                  TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at                TEXT,
    audit_note                  TEXT,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL,
    FOREIGN KEY (request_episode_id) REFERENCES episodes(id) ON DELETE SET NULL,
    CHECK (completed_at IS NULL OR completed_at >= created_at)
);

CREATE TABLE IF NOT EXISTS memory_access_events (
    id                         TEXT PRIMARY KEY,
    persona_id                 TEXT NOT NULL,
    session_id                 TEXT,
    message_id                 TEXT,
    node_type                  TEXT NOT NULL
        CHECK (node_type IN ('episode', 'entity', 'fact', 'narrative', 'insight', 'mood_state', 'affect_event')),
    node_id                    TEXT NOT NULL,
    access_type                TEXT NOT NULL DEFAULT 'retrieved'
        CHECK (access_type IN ('retrieved', 'prompt_injected', 'reinforced', 'suppressed', 'filtered')),
    retrieval_score            REAL,
    rank_position              INTEGER,
    score_breakdown_json       TEXT,
    activation_path_json       TEXT,
    context_block_type         TEXT,
    user_mood_label            TEXT,
    relationship_affect_label  TEXT,
    agent_affect_state_id       TEXT,
    created_at                 TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE SET NULL,
    FOREIGN KEY (agent_affect_state_id) REFERENCES agent_affect_states(id) ON DELETE SET NULL,
    CHECK (rank_position IS NULL OR rank_position >= 0)
);

CREATE INDEX IF NOT EXISTS idx_mood_states_current
    ON mood_states(persona_id, scope, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_affect_events_time
    ON affect_events(persona_id, scope, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_affect_events_trigger
    ON affect_events(persona_id, trigger_type, trigger_ref);

CREATE INDEX IF NOT EXISTS idx_agent_affect_profiles_persona
    ON agent_affect_profiles(persona_id, profile_name);
CREATE INDEX IF NOT EXISTS idx_agent_affect_states_current
    ON agent_affect_states(persona_id, session_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_states_profile
    ON agent_affect_states(persona_id, profile_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_appraisals_trigger
    ON agent_appraisals(persona_id, trigger_type, trigger_ref_type, trigger_ref_id);
CREATE INDEX IF NOT EXISTS idx_agent_appraisals_target
    ON agent_appraisals(persona_id, target_node_type, target_node_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_events_time
    ON agent_affect_events(persona_id, session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_affect_events_appraisal
    ON agent_affect_events(persona_id, appraisal_id);
CREATE INDEX IF NOT EXISTS idx_agent_expression_decisions_message
    ON agent_expression_decisions(persona_id, session_id, message_id);

CREATE INDEX IF NOT EXISTS idx_deletion_events_persona_time
    ON deletion_events(persona_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deletion_events_target
    ON deletion_events(persona_id, target_node_type, target_node_id);
CREATE INDEX IF NOT EXISTS idx_deletion_events_status
    ON deletion_events(persona_id, status, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_memory_access_events_node
    ON memory_access_events(persona_id, node_type, node_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_access_events_session
    ON memory_access_events(persona_id, session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_access_events_type
    ON memory_access_events(persona_id, access_type, created_at DESC);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0003', 'MemoryGraph v2 mood/affect placeholders, deletion audit and memory access events');
