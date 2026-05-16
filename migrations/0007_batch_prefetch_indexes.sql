CREATE INDEX IF NOT EXISTS idx_memory_access_events_session_node_access
    ON memory_access_events(session_id, node_type, node_id, access_type);

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0007', 'batch prefetch access-event lookup index');
