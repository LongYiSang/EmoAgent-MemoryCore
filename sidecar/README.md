# MemoryCore Python Sidecar

Loopback HTTP sidecar for optional retrieval assistance. SQLite remains the
authoritative memory store; TriviumDB can be cleared and rebuilt from SQLite at
any time.

Current sidecar responsibilities:

- Phase 4 mirror indexing and dense candidate retrieval.
- Phase 5 semantic query analysis through `/retrieval/query-analysis`.
- Phase 5 mirror candidate retrieval through `/retrieval/candidates` v0.2.
- Phase 5C graph activation from Go-provided seed energy.
- Phase 5G safe rerank endpoint, with the real provider disabled by default.

Go / SQLite still owns query-analysis merge/clamp, authority filtering, final
score, fatigue, MMR, context budget, context reconstruction, and prompt
injection. Sidecar output is only a candidate, routing hint, or ranking signal
and must be safe to ignore.

Sidecar retrieval is an optional enhancement path. Go bounds mirror candidates,
graph activation, query analysis, and rerank with independent stage timeouts,
sidecar budgets, and persona/stage circuit breakers. Timeout, budget, degraded
status, provider error, or malformed response falls back to SQLite authority
retrieval and remains visible in retrieval diagnostics. Python graph activation
also honors request budgets for wall time, scanned edges, and per-node
neighbors, returning safe partial degraded results instead of spinning.

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

### Query Analysis

Semantic query analysis is disabled by default:

```toml
[query_analysis]
provider = "none"
base_url = "https://dashscope.aliyuncs.com/compatible-mode/v1"
api_key_env = "DASHSCOPE_API_KEY"
model = "qwen-plus"
timeout_seconds = 30
temperature = 0
response_format = "json_object"
prompt_version = "query-analysis-v0.1"
```

Supported providers are `none` and `openai-compatible`. The API key is read
only from the configured environment variable. Missing key, provider timeout,
HTTP error, invalid JSON, failed schema validation, or degraded provider status
returns a degraded query-analysis result; Go ignores the semantic result,
keeps the rule analysis, and still calls `/retrieval/candidates` with raw dense
query input.

The OpenAI-compatible provider is expected to support
`response_format={"type":"json_object"}`. The sidecar validates the returned
analysis object, retries once after validation failure, and then returns a
degraded fallback. It does not repair malformed JSON and does not log API keys,
full prompts, full provider responses, chain-of-thought, provider payloads, or
conversation summaries.

### Rerank

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

Rerank receives only SQLite-authorized safe summaries and a bounded query
surface. It does not receive semantic rewrite text, semantic anchor text, raw
provider payloads, rationale summaries, or conversation windows.

## Run Fake Adapter

The fake adapter has no embedding or TriviumDB dependency and returns
deterministic positive `trivium_node_id` values.

```powershell
cd sidecar
uv run python -m memorycore_sidecar.server --adapter fake --host 127.0.0.1 --port 8765
```

Use this for fast sync smoke tests with
`go run ./cmd/memoryctl mirror-sync-run --fake-adapter`.

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

`retrieve --use-mirror` can exercise Phase 4 mirror candidates, optional
semantic query analysis, Phase 5C graph activation, and Phase 5G rerank
depending on persona mirror readiness and sidecar configuration. With the
default query-analysis and rerank providers (`none`), those stages return
degraded or rule-only fallback and Go final ranking continues without semantic
rewrite dense inputs or rerank boost.

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
- `POST /retrieval/query-analysis` accepts
  `memory_query_analysis_request.v0.1` and returns
  `memory_query_analysis_result.v0.1`.
- `POST /retrieval/candidates` accepts
  `memory_mirror_candidate_request.v0.2` and returns
  `memory_mirror_candidates.v0.2`.
- `POST /retrieval/activate` accepts Phase 5B activation seeds and returns graph
  activation candidates plus optional paths.
- `POST /retrieval/rerank` accepts only safe summaries after Go / SQLite
  authority filtering and returns optional rerank scores or a degraded fallback.

### `/retrieval/query-analysis`

The request contains the raw query, rule analysis, retrieval policy caps,
allowed enums, safe conversation window, visible entity hints, and debug flags.
The sidecar does not read SQLite or TriviumDB for authority decisions.

The result contains status, degraded/fallback fields, provider/model/prompt
metadata, and an analysis object with time/domain/ability/evidence fields,
signals, entity mentions, query rewrites, semantic anchors, context block
hints, policy hints, confidence, and optional rationale summary. Rationale is
disabled by default and should not be exposed in normal public APIs.

### `/retrieval/candidates` v0.2

Candidate retrieval accepts one query object:

- `raw_text`, always included with source `raw_dense` and weight `1.0`.
- `rewrites`, up to 5 generated semantic rewrite texts, weight-clamped to
  `0.1..0.9`.
- `semantic_anchors`, up to 4 dense anchor texts after validation, each capped
  at weight `0.65`.
- merged QueryAnalysis fields such as `time_mode`, `memory_domain`,
  `memory_ability`, `evidence_need`, and `signals`.

Generated rewrite and anchor texts are deduped by normalized text. Generic
filtering applies only to whole LLM-generated rewrite/anchor texts; it never
filters or edits the raw query, and it does not delete common words inside an
otherwise useful generated text. Go config caps generated dense weight through
`query_analysis.max_generated_dense_weight_sum` while the raw query remains
uncompressed.

The sidecar internally over-fetches each dense input before merge:

- raw query: `max(limit * 2, 32)`
- rewrite query: `max(limit * 2, 32)`
- anchor query: `max(limit, 16)`
- merged output: `limit`

Dense merge uses Weighted Max + bounded RRF support:

```text
primary_score(node) = max_i(query_weight_i * normalized_dense_score_i(node))
rrf_support_raw(node) = sum_i(query_weight_i / (rrf_k + rank_i(node)))
rrf_support_norm(node) = rrf_support_raw(node) / max_possible_rrf_support
support_bonus(node) = min(max_support_bonus, support_beta * rrf_support_norm(node))
fused_score(node) = clamp(primary_score(node) + support_bonus(node), 0, 1)
```

Default parameters are `rrf_k=60`, `support_beta=0.18`, and
`max_support_bonus=0.12`. Sorting is `fused_score desc`, `primary_score desc`,
`hit_count desc`, source priority (`raw_dense`, `semantic_rewrite_dense`,
`semantic_anchor_dense`), then `trivium_node_id asc`.

`hit_count` is diagnostic only for Go. It does not amplify Go AnchorFusion.

When QueryAnalysis signals include `forget_delete`, semantic rewrites must use
purpose `operation_target`; broad semantic expansion such as
`relationship_arc_dense` or generic `semantic_dense` is ignored. Candidate
diagnostics label dense results as `operation_target_candidates`, and those
results are not injected as ordinary remembered context.

Query diagnostics include query counts, per-query candidate counts, and merged
candidate count. In debug mode only, candidate diagnostics may include
`score_breakdown` with `primary_score`, `rrf_support_norm`, `support_bonus`, and
`score_norm_method`. Default public APIs must not expose raw LLM responses,
full prompts, chain-of-thought, provider payloads, or conversation summaries.

## Test

```powershell
cd sidecar
uv run pytest
```
