# MemoryCore Python Sidecar

Phase 4B provides a loopback HTTP protocol for mirror operations that Go can call
without importing Python or TriviumDB directly.

## Protocol

- `GET /health` returns `{"status":"ok"}`.
- `POST /mirror/operation` accepts `schema_version` `memory_mirror_operation.v0.1`.
- Responses use `schema_version` `memory_mirror_operation_result.v0.1`.
- Supported operations are `upsert_node`, `delete_node`, `upsert_edge`, and
  `delete_edge`.

The fake adapter has no TriviumDB dependency and returns deterministic positive
`trivium_node_id` values for node upserts.

## Run

```powershell
uv run python -m memorycore_sidecar.server --adapter fake --host 127.0.0.1 --port 8765
```

Then point the Go CLI at it:

```powershell
memoryctl mirror-sync-run --db memory.db --sidecar-url http://127.0.0.1:8765
```

## Test

```powershell
uv run python -m pytest
```
