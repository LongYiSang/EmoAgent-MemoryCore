-- 0013_natural_memory_quota_index.sql
-- Harden Natural Memory sleep-cycle quota after the initial ledger rollout.

UPDATE memory_natural_runs
SET mark_sleep_cycle = 0
WHERE run_kind != 'sleep_cycle'
  AND mark_sleep_cycle = 1
  AND status = 'completed'
  AND force = 0
  AND EXISTS (
      SELECT 1
      FROM memory_natural_runs AS sleep
      WHERE sleep.persona_id = memory_natural_runs.persona_id
        AND sleep.local_date = memory_natural_runs.local_date
        AND sleep.run_kind = 'sleep_cycle'
        AND sleep.status = 'completed'
        AND sleep.force = 0
  );

UPDATE memory_natural_runs
SET mark_sleep_cycle = 0
WHERE run_kind != 'sleep_cycle'
  AND mark_sleep_cycle = 1
  AND status = 'completed'
  AND force = 0
  AND rowid NOT IN (
      SELECT MIN(rowid)
      FROM memory_natural_runs
      WHERE run_kind != 'sleep_cycle'
        AND mark_sleep_cycle = 1
        AND status = 'completed'
        AND force = 0
      GROUP BY persona_id, local_date
  );

CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_natural_quota_once_per_day
ON memory_natural_runs(persona_id, local_date)
WHERE (run_kind = 'sleep_cycle' OR mark_sleep_cycle = 1) AND status = 'completed' AND force = 0;

INSERT OR IGNORE INTO schema_migrations(version, description)
VALUES ('0013', 'Natural Memory quota-consuming run uniqueness');
