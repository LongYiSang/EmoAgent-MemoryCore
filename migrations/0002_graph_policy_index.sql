-- 0002_memory_graph_policy_index.sql
-- Predicate schemas, graph links, entity co-occurrence, Trivium mirror mapping and async sync queue.

PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS predicate_schemas (
    predicate             TEXT PRIMARY KEY,
    canonical_label       TEXT,
    default_fact_type     TEXT
        CHECK (default_fact_type IS NULL OR default_fact_type IN (
            'core_identity',
            'significant_event',
            'stable_preference',
            'relational_state',
            'commitment',
            'transient_context',
            'task_relevant_context'
        )),
    cardinality           TEXT NOT NULL
        CHECK (cardinality IN ('single', 'multi', 'temporal_multi')),
    conflict_policy       TEXT NOT NULL
        CHECK (conflict_policy IN ('supersede', 'coexist', 'merge', 'llm_check', 'expire_by_time')),
    temporal_behavior     TEXT NOT NULL
        CHECK (temporal_behavior IN ('state', 'event', 'preference', 'habit', 'commitment')),
    object_kind           TEXT NOT NULL
        CHECK (object_kind IN ('entity', 'literal', 'either')),
    default_tau_days      REAL
        CHECK (default_tau_days IS NULL OR default_tau_days > 0),
    default_importance    REAL NOT NULL DEFAULT 0.5
        CHECK (default_importance >= 0.0 AND default_importance <= 1.0),
    allow_inference       INTEGER NOT NULL DEFAULT 1
        CHECK (allow_inference IN (0, 1)),
    sensitive_by_default  INTEGER NOT NULL DEFAULT 0
        CHECK (sensitive_by_default IN (0, 1))
);

CREATE TABLE IF NOT EXISTS memory_links (
    id                 TEXT PRIMARY KEY,
    persona_id         TEXT NOT NULL,
    from_node_type     TEXT NOT NULL
        CHECK (from_node_type IN (
            'episode', 'entity', 'fact', 'narrative', 'insight', 'mood_state', 'affect_event',
            'agent_affect_profile', 'agent_affect_state', 'agent_appraisal', 'agent_affect_event',
            'agent_expression_decision', 'deletion_event'
        )),
    from_node_id       TEXT NOT NULL,
    link_type          TEXT NOT NULL
        CHECK (link_type IN (
            'EVIDENCED_BY', 'DERIVED_FROM', 'SUPERSEDES', 'CONTRADICTS',
            'CAUSED_BY', 'CONTRIBUTED_TO', 'TRIGGERED_BY', 'EXPLAINS',
            'ABOUT_ENTITY', 'CO_OCCURS_WITH', 'TEMPORAL_PREV', 'TEMPORAL_NEXT',
            'SUPPORTS', 'INHIBITS', 'REDACTED_BY'
        )),
    to_node_type       TEXT NOT NULL
        CHECK (to_node_type IN (
            'episode', 'entity', 'fact', 'narrative', 'insight', 'mood_state', 'affect_event',
            'agent_affect_profile', 'agent_affect_state', 'agent_appraisal', 'agent_affect_event',
            'agent_expression_decision', 'deletion_event'
        )),
    to_node_id         TEXT NOT NULL,
    direction          TEXT NOT NULL DEFAULT 'forward'
        CHECK (direction IN ('forward', 'backward', 'bidirectional')),
    confidence         REAL NOT NULL DEFAULT 1.0
        CHECK (confidence >= 0.0 AND confidence <= 1.0),
    weight             REAL NOT NULL DEFAULT 1.0
        CHECK (weight >= -1.0 AND weight <= 1.0),
    reasoning          TEXT,
    valid_from         TEXT,
    valid_to           TEXT,
    created_at         TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by         TEXT NOT NULL DEFAULT 'system'
        CHECK (created_by IN ('system', 'llm', 'user', 'consolidation')),
    visibility_status  TEXT NOT NULL DEFAULT 'visible'
        CHECK (visibility_status IN ('visible', 'hidden', 'forgotten', 'purged')),
    searchable         INTEGER NOT NULL DEFAULT 1
        CHECK (searchable IN (0, 1)),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    UNIQUE (persona_id, from_node_type, from_node_id, link_type, to_node_type, to_node_id),
    CHECK (valid_to IS NULL OR valid_from IS NULL OR valid_to >= valid_from)
);

CREATE TABLE IF NOT EXISTS entity_cooccurrences (
    id               TEXT PRIMARY KEY,
    persona_id       TEXT NOT NULL,
    entity_a_id      TEXT NOT NULL,
    entity_b_id      TEXT NOT NULL,
    co_count         INTEGER NOT NULL DEFAULT 1
        CHECK (co_count >= 1),
    last_seen_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    context_scope    TEXT NOT NULL DEFAULT 'session'
        CHECK (context_scope IN ('message_window', 'session', 'week', 'topic')),
    confidence       REAL NOT NULL DEFAULT 0.5
        CHECK (confidence >= 0.0 AND confidence <= 1.0),
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    FOREIGN KEY (entity_a_id) REFERENCES entities(id) ON DELETE CASCADE,
    FOREIGN KEY (entity_b_id) REFERENCES entities(id) ON DELETE CASCADE,
    UNIQUE (persona_id, entity_a_id, entity_b_id, context_scope),
    CHECK (entity_a_id <> entity_b_id)
);

CREATE TABLE IF NOT EXISTS memory_index_map (
    id                TEXT PRIMARY KEY,
    persona_id        TEXT NOT NULL,
    node_type         TEXT NOT NULL
        CHECK (node_type IN (
            'episode', 'entity', 'fact', 'narrative', 'insight', 'mood_state', 'affect_event',
            'agent_affect_profile', 'agent_affect_state', 'agent_appraisal', 'agent_affect_event',
            'agent_expression_decision', 'deletion_event'
        )),
    node_id           TEXT NOT NULL,
    trivium_node_id   INTEGER NOT NULL,
    index_status      TEXT NOT NULL
        CHECK (index_status IN ('pending', 'indexed', 'stale', 'deleted', 'failed', 'stale_delete_failed')),
    indexed_at        TEXT,
    updated_at        TEXT,
    error_message     TEXT,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE,
    UNIQUE (persona_id, node_type, node_id),
    UNIQUE (persona_id, trivium_node_id)
);

CREATE TABLE IF NOT EXISTS index_sync_queue (
    id             TEXT PRIMARY KEY,
    persona_id     TEXT NOT NULL,
    node_type      TEXT NOT NULL
        CHECK (node_type IN (
            'episode', 'entity', 'fact', 'narrative', 'insight', 'mood_state', 'affect_event',
            'agent_affect_profile', 'agent_affect_state', 'agent_appraisal', 'agent_affect_event',
            'agent_expression_decision', 'deletion_event', 'memory_link', 'persona'
        )),
    node_id        TEXT NOT NULL,
    operation      TEXT NOT NULL
        CHECK (operation IN ('upsert_node', 'delete_node', 'upsert_edge', 'delete_edge', 'rebuild_persona')),
    priority       INTEGER NOT NULL DEFAULT 5
        CHECK (priority >= 0 AND priority <= 10),
    payload_json   TEXT,
    status         TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts       INTEGER NOT NULL DEFAULT 0
        CHECK (attempts >= 0),
    created_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TEXT,
    error_message  TEXT,
    FOREIGN KEY (persona_id) REFERENCES personas(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_predicate_schemas_type
    ON predicate_schemas(default_fact_type, cardinality, conflict_policy);

CREATE INDEX IF NOT EXISTS idx_memory_links_from
    ON memory_links(persona_id, from_node_type, from_node_id);
CREATE INDEX IF NOT EXISTS idx_memory_links_to
    ON memory_links(persona_id, to_node_type, to_node_id);
CREATE INDEX IF NOT EXISTS idx_memory_links_type
    ON memory_links(persona_id, link_type);
CREATE INDEX IF NOT EXISTS idx_memory_links_from_type
    ON memory_links(persona_id, from_node_type, from_node_id, link_type);
CREATE INDEX IF NOT EXISTS idx_memory_links_to_type
    ON memory_links(persona_id, to_node_type, to_node_id, link_type);
CREATE INDEX IF NOT EXISTS idx_memory_links_visible_searchable
    ON memory_links(persona_id, link_type, weight DESC)
    WHERE visibility_status = 'visible' AND searchable = 1;

CREATE INDEX IF NOT EXISTS idx_entity_cooccurrences_a
    ON entity_cooccurrences(persona_id, entity_a_id, context_scope, co_count DESC);
CREATE INDEX IF NOT EXISTS idx_entity_cooccurrences_b
    ON entity_cooccurrences(persona_id, entity_b_id, context_scope, co_count DESC);
CREATE INDEX IF NOT EXISTS idx_entity_cooccurrences_last_seen
    ON entity_cooccurrences(persona_id, last_seen_at DESC);

CREATE INDEX IF NOT EXISTS idx_memory_index_map_node
    ON memory_index_map(persona_id, node_type, node_id);
CREATE INDEX IF NOT EXISTS idx_memory_index_map_trivium
    ON memory_index_map(persona_id, trivium_node_id);
CREATE INDEX IF NOT EXISTS idx_memory_index_map_status
    ON memory_index_map(persona_id, index_status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_index_sync_queue_pending
    ON index_sync_queue(status, priority ASC, created_at ASC)
    WHERE status IN ('pending', 'failed');
CREATE INDEX IF NOT EXISTS idx_index_sync_queue_persona_node
    ON index_sync_queue(persona_id, node_type, node_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_index_sync_queue_operation
    ON index_sync_queue(persona_id, operation, status, priority ASC);

INSERT OR IGNORE INTO predicate_schemas (
    predicate, canonical_label, default_fact_type, cardinality, conflict_policy,
    temporal_behavior, object_kind, default_tau_days, default_importance,
    allow_inference, sensitive_by_default
) VALUES
    ('lives_in', '居住地', 'core_identity', 'single', 'supersede', 'state', 'entity', 3650, 0.80, 1, 0),
    ('works_at', '工作单位', 'core_identity', 'single', 'supersede', 'state', 'entity', 1825, 0.75, 1, 0),
    ('prefers_name', '偏好称呼', 'core_identity', 'single', 'supersede', 'preference', 'literal', 3650, 0.85, 1, 0),
    ('likes', '喜欢', 'stable_preference', 'multi', 'coexist', 'preference', 'either', 365, 0.55, 1, 0),
    ('dislikes', '不喜欢', 'stable_preference', 'multi', 'coexist', 'preference', 'either', 365, 0.60, 1, 0),
    ('has_pet', '宠物', 'core_identity', 'multi', 'coexist', 'state', 'entity', 1825, 0.65, 1, 0),
    ('is_busy_with', '近期忙于', 'transient_context', 'temporal_multi', 'expire_by_time', 'state', 'either', 21, 0.50, 1, 0),
    ('feels_about_agent', '对 Agent 的感受', 'relational_state', 'single', 'llm_check', 'preference', 'literal', 180, 0.65, 1, 0),
    ('promised_to', '承诺/约定', 'commitment', 'temporal_multi', 'expire_by_time', 'commitment', 'literal', 30, 0.70, 1, 0),
    ('is_considering', '正在考虑', 'significant_event', 'temporal_multi', 'expire_by_time', 'event', 'either', 90, 0.65, 1, 0),
    ('has_boundary', '边界/禁忌', 'relational_state', 'multi', 'merge', 'preference', 'literal', 365, 0.80, 1, 1),
    ('prefers_communication_style', '沟通风格偏好', 'stable_preference', 'multi', 'merge', 'preference', 'literal', 365, 0.65, 1, 0),
    ('uses_coping_strategy', '应对方式', 'stable_preference', 'multi', 'coexist', 'habit', 'literal', 365, 0.65, 1, 0);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0002', 'MemoryGraph v2 graph/policy/index mirror tables and base predicate schemas');
