# MemoryCore Python Sidecar

Loopback HTTP sidecar for optional retrieval assistance. SQLite remains the
authoritative memory store; TriviumDB can be cleared and rebuilt from SQLite at
any time.

Current sidecar responsibilities:

- Phase 4 mirror indexing and dense candidate retrieval.
- Phase 5C graph activation from Go-provided seed energy.
- Phase 5G safe rerank endpoint, with the real provider disabled by default.

Go / SQLite still owns authority filtering, final score, fatigue, MMR, context
budget, context reconstruction, and prompt injection. Sidecar output is only a
candidate or ranking signal and must be safe to ignore.

Sidecar retrieval is an optional enhancement path. Go bounds mirror candidates,
graph activation, and rerank with short stage timeouts, a total sidecar budget,
and persona/stage circuit breakers. Timeout, budget, or degraded statuses fall
back to SQLite authority retrieval and remain visible in retrieval diagnostics.
Python graph activation also honors request budgets for wall time, scanned
edges, and per-node neighbors, returning safe partial degraded results instead
of spinning.

## Setup

Use Python 3.12 with uv from this directory:

```powershell
cd sidecar
uv python pin 3.12
uv sync
```

`pyproject.toml` pins the runtime dependency to `triviumdb==0.7.1`.

## Config

Copy `config.example.toml` when you need a local config:

```powershell
Copy-Item config.example.toml config.toml
```

The example defaults to Bailian/DashScope OpenAI-compatible embeddings:

```toml
[embedding]
provider = "openai-compatible"
base_url = "https://dashscope.aliyuncs.com/compatible-mode/v1"
api_key_env = "DASHSCOPE_API_KEY"
model = "text-embedding-v4"
dimensions = 1024
```

Do not put plaintext API keys in config. Set the environment variable named by
`api_key_env` instead.

TriviumDB files default to `../data/trivium`, relative to `sidecar/`. The real
adapter initializes one sanitized `.tdb` file per persona under that directory.

Rerank defaults to disabled:

```toml
[rerank]
provider = "none"
```

Use `provider = "dashscope-vl"` only when you explicitly want the optional
DashScope `qwen3-vl-rerank` provider. The API key is still read from the env var
named by `api_key_env`; missing key, timeout, HTTP error, malformed response, or
degraded provider status returns a fallback result instead of blocking Go
retrieval.

## Run Fake Adapter

The fake adapter has no embedding or TriviumDB dependency and returns
deterministic positive `trivium_node_id` values.

```powershell
cd sidecar
uv run python -m memorycore_sidecar.server --adapter fake --host 127.0.0.1 --port 8765
```

Use this for fast sync smoke tests with `go run ./cmd/memoryctl mirror-sync-run --fake-adapter`.

## Run Real Trivium Adapter

```powershell
cd sidecar
$env:DASHSCOPE_API_KEY = "<dashscope-api-key>"
uv run python -m memorycore_sidecar.server --adapter trivium --config config.toml --host 127.0.0.1 --port 8765
```

`--config` is optional; without it the built-in defaults match
`config.example.toml`.

The real adapter needs an embedding provider and a TriviumDB install. Keep API
keys in env vars only.

## Manual Smoke

Start either sidecar server above, then run these from the repo root in another
terminal. Replace `./data/memory.db` and the query with a database that already
contains memory data.

```powershell
go run ./cmd/memoryctl mirror-sync-run --db ./data/memory.db --sidecar-url http://127.0.0.1:8765 --limit 100
go run ./cmd/memoryctl mirror-rebuild --db ./data/memory.db --sidecar-url http://127.0.0.1:8765
go run ./cmd/memoryctl retrieve --db ./data/memory.db --query "coffee preference" --use-mirror --sidecar-url http://127.0.0.1:8765
```

`retrieve --use-mirror` can exercise Phase 4 mirror candidates, Phase 5C graph
activation, and Phase 5G rerank depending on persona mirror readiness and sidecar
configuration. With the default rerank provider (`none`), rerank returns a
degraded fallback and Go final ranking continues without a rerank boost.

`mirror-sync-run` processes queued `upsert_node`, `delete_node`, `upsert_edge`,
and `delete_edge` rows. `rebuild_persona` is not worker-supported in this pass;
use explicit rebuild instead.

`delete_edge` rows are valid only when the queue payload carries the real link
endpoint identity. Thin edge-id-only deletes are rejected. If the adapter has
no unlink API, rebuild the persona namespace.

For health checks, call `GET /health` on the sidecar and inspect
`index_sync_queue` for pending/failed rows.

If the sidecar is down or the mirror is degraded, stop using `--use-mirror` and
let retrieval fall back to SQLite. SQLite stays authoritative.

## Protocol

- `GET /health` returns `{"status":"ok"}`.
- `POST /mirror/operation` accepts `schema_version` `memory_mirror_operation.v0.1`.
- `POST /mirror/clear-namespace` clears one persona namespace.
- `POST /retrieval/candidates` returns mirror candidates for Go to map back
  through SQLite before prompt use.
- `POST /retrieval/activate` accepts Phase 5B activation seeds and returns graph
  activation candidates plus optional paths.
- `POST /retrieval/rerank` accepts only safe summaries after Go / SQLite
  authority filtering and returns optional rerank scores or a degraded fallback.

## Test

```powershell
cd sidecar
uv run pytest
```
