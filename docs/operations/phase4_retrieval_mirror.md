# Phase 4 Retrieval Mirror

This note is the operator runbook for the Phase 4 TriviumDB retrieval mirror.
SQLite remains authoritative. The mirror is rebuildable and may be cleared at
any time.

## Invariants

- SQLite owns truth, queue state, and retrieval fallback decisions.
- TriviumDB is a retrieval mirror only.
- Queue sync handles `upsert_node`, `delete_node`, `upsert_edge`, and
  `delete_edge`.
- `rebuild_persona` is not worker-supported in this pass; rebuild is explicit
  through `RebuildMirror` or `memoryctl mirror-rebuild`.

## Sidecar setup

Fast smoke path:

```powershell
cd sidecar
uv run python -m memorycore_sidecar.server --adapter fake --host 127.0.0.1 --port 8765
```

Real Trivium path:

```powershell
cd sidecar
$env:DASHSCOPE_API_KEY = "<dashscope-api-key>"
uv run python -m memorycore_sidecar.server --adapter trivium --config config.toml --host 127.0.0.1 --port 8765
```

The real adapter needs an embedding provider and a local TriviumDB install.
Keep keys in env vars only.

## Sync worker

Process queued mirror operations:

```powershell
go run ./cmd/memoryctl mirror-sync-run --db ./data/memory.db --limit 100 --fake-adapter
go run ./cmd/memoryctl mirror-sync-run --db ./data/memory.db --limit 100 --sidecar-url http://127.0.0.1:8765
```

Use `--fake-adapter` for deterministic sync smoke tests. Use `--sidecar-url`
for real loopback sidecar sync. The worker rejects remote URLs.

## Rebuild persona namespace

Explicit rebuild clears the persona namespace and replays eligible SQLite state:

```powershell
go run ./cmd/memoryctl mirror-rebuild --db ./data/memory.db --sidecar-url http://127.0.0.1:8765
```

Use this for full namespace rebuilds. Do not wait for a queued `rebuild_persona`
row in this pass.

## Queue health checks

Quick SQL checks:

```sql
SELECT status, COUNT(*) FROM index_sync_queue GROUP BY status;
SELECT operation, status, COUNT(*) FROM index_sync_queue GROUP BY operation, status;
SELECT persona_id, COUNT(*) FROM index_sync_queue WHERE status IN ('pending', 'failed') GROUP BY persona_id;
```

Stale processing rows are failed automatically after the lease window and use
the message `mirror queue lease expired`.

## Retrieval diagnostics

Mirror-backed retrieval:

```powershell
go run ./cmd/memoryctl retrieve --db ./data/memory.db --query "coffee preference" --use-mirror --sidecar-url http://127.0.0.1:8765
```

If the mirror returns no candidates or the sidecar is degraded, inspect:

- `memory_index_map` for SQLite-to-Trivium mappings.
- `mirror_persona_state` for `ready`, `rebuilding`, or `degraded` state.
- `GET /health` on the sidecar.
- `/retrieval/candidates` for the mirror candidate response.

If the mirror is unavailable, remove `--use-mirror` and keep retrieval on
SQLite. SQLite stays authoritative in every fallback path.

## delete_edge behavior

Queued `delete_edge` entries must carry endpoint identity in payload JSON.
Thin edge-id-only deletes are rejected. If the adapter does not expose an
unlink API, rebuild the persona namespace instead of trying to force a partial
delete.

## Fast tests

```powershell
go test ./cmd/memoryctl -run 'TestRunMirror|TestRunRetrieveWithMirror' -count=1
cd sidecar
uv run pytest tests/test_protocol.py tests/test_fake_adapter.py -q
```

## Optional real smoke

1. Start the real sidecar with `--adapter trivium`.
2. Run `go run ./cmd/memoryctl mirror-sync-run --db ./data/memory.db --sidecar-url http://127.0.0.1:8765`.
3. Run `go run ./cmd/memoryctl mirror-rebuild --db ./data/memory.db --sidecar-url http://127.0.0.1:8765`.
4. Run `go run ./cmd/memoryctl retrieve --db ./data/memory.db --query "coffee preference" --use-mirror --sidecar-url http://127.0.0.1:8765`.
