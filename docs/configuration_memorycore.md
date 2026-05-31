# MemoryCore Configuration

MemoryCore uses one unified v0.2 configuration entry point:

- Full example: `examples/config/memorycore.yaml`
- Minimal example: `examples/config/memorycore.min.yaml`
- Schema version: `memorycore.config.v0.2`

The configuration is owned by MemoryCore and can be loaded independently by tools, tests, or host applications such as EmoAgent. MemoryCore does not import or depend on the EmoAgent repository.

## Priority

Effective configuration is resolved in this order:

1. Built-in defaults from `config.DefaultConfig()`
2. YAML or JSON file loaded through `config.LoadYAML`, `config.LoadJSON`, or `config.LoadEffective`
3. EmoAgent or host overrides passed as `config.ConfigOverrides`
4. Environment-backed runtime checks for secrets such as `api_key_env`
5. Explicit CLI or test overrides

CLI commands keep the existing rule that explicit flags override file values and print a warning. `enabled: false` is an embedding switch for hosts; it does not block a user who explicitly runs `memoryctl`.

## Provider And Pipeline Setup

Providers are declared under:

- `providers.llm`
- `providers.embedder`
- `providers.reranker`

Pipelines reference providers by `provider_id`:

- `pipelines.prefilter`
- `pipelines.extraction`
- `pipelines.extraction_repair`
- `pipelines.query_analysis`
- `pipelines.embedding`
- `pipelines.rerank`
- `pipelines.narrative_insight`
- `semantic_ops.curation`

LLM model parameters live in each pipeline under `params`, including `temperature`, `top_p`, `max_output_tokens`, `response_format`, `stream`, `seed`, and `timeout_ms`. `pipelines.extraction` defaults to deterministic JSON-schema extraction with one schema-repair attempt.

`pipelines.query_analysis.fallback_mode` must remain `rule_only`. If LLM or sidecar query analysis fails, safe SQLite-backed retrieval still uses the deterministic rule analyzer.

`semantic_ops.curation` controls delta memory curation for facts created after the last curation checkpoint. It is disabled by default, defaults to `dry_run`, and only auto-applies high-confidence `same` or `refinement` decisions with no/small answer gain. Manual CLI runs use `memoryctl curation-run`; scheduled loops remain host-owned. Curation raw logs live under `semantic_ops.curation.raw_log` and are off by default because they contain full prompt and provider payloads. `semantic_ops.curation.llm.thinking.type` defaults to `disabled`; when set to `enabled`, OpenAI-compatible curation parsing reads `reasoning_content` instead of the normal message content.

## Secrets

Do not put plaintext secrets in config files. Use `api_key_env`:

```yaml
providers:
  llm:
    - id: default_llm
      provider: openai
      protocol: openai_compatible
      base_url: ${MEMORYCORE_LLM_BASE_URL}
      api_key_env: MEMORYCORE_LLM_API_KEY
      enabled: true
```

`validate-config --check-env` verifies that enabled providers with `api_key_env` have the referenced environment variable available.

## EmoAgent Injection

EmoAgent can integrate without a MemoryCore dependency on EmoAgent:

- Use `config.LoadEffective` to load a MemoryCore `config_path`, apply `ConfigOverrides`, and map an upstream provider registry through `ProviderRegistry`.
- Use `config.Open` when the host wants MemoryCore to open the service directly and return the effective config plus runtime defaults.
- Use `Config.Runtime()` or `Config.ToOptions()` when the host wants to open `memorycore.Open` itself.

Provider mapping is intentionally structural: EmoAgent passes provider IDs, protocol, base URL, and `api_key_env`; MemoryCore stores the effective registry and pipelines reference IDs. Host-owned provider clients remain host-owned.

## Hot Update Boundaries

Usually safe to hot update in a host process:

- Provider enabled flags and model names for host-owned LLM calls
- LLM `params`
- `semantic_ops.curation` enabled/mode/limits/model fields before starting a new run
- Write-policy thresholds and max candidate counts
- Retrieval count, budget, FTS/mirror switches, sensitivity permission, and MMR/ranking knobs
- Observability detail flags

Require restart or service re-open:

- `core.db_path`
- `core.auto_migrate`
- `core.enable_fts`
- Sidecar adapter type or base URL when an adapter instance has already been created
- Provider registry changes when the host keeps long-lived provider clients
- Retention or mirror startup jobs

## Non-Configurable Safety Invariants

These cannot be disabled by config:

- SQLite remains the source of truth.
- TriviumDB and sidecar data are retrieval mirrors only.
- Hidden, forgotten, purged, unsearchable, or over-sensitive nodes cannot enter the prompt.
- Delta curation does not delete source facts; merged sources stay visible in SQLite but become consolidated and unsearchable.
- Manual forget routing cannot depend on extraction LLM success.
- Work logs cannot directly write long-term facts; only Emotion-approved `work_candidate` flows may be considered.
- Agent Affect cannot write user facts, bypass sensitivity, bypass forget or purge, or increase negative memory retention.
- Plaintext secrets cannot be stored in config files.
