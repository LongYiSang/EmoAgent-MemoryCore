# Phase 2C Extraction Runtime

Phase 2C adds a real extraction runtime in the MemoryCore repository without changing the Phase 2B downstream write path.

Core boundary:

- LLM providers only return candidate JSON.
- `ParseResponse`, `ParsePreFilterResponse`, `ValidateExtraction`, `ApplyAcceptedFacts`, and `ConsolidateCandidate` remain the authority.
- `gate.Status == blocked` never applies.
- Only accepted facts are written; review, rejected, route-only, and not-applied items are never inserted as facts.
- Links, affect events, deletion intents, and correction hints remain preview, route, or not-applied.

## CLI

Single run:

```bash
go run ./cmd/memoryctl extract-run \
  --db ./data/memory.db \
  --session session_123 \
  --provider mock \
  --mode dry-run \
  --format json
```

Batch run:

```bash
go run ./cmd/memoryctl extract-batch \
  --db ./data/memory.db \
  --provider mock \
  --mode dry-run \
  --session-limit 20 \
  --episode-limit 50 \
  --format json
```

`dry-run` means no memory writes: it does not call `ApplyAcceptedFacts`. It may still write a sanitized `extraction_runs` audit row for idempotency. Use `--audit off` when the run must avoid audit writes too.

`--mode apply` is required to write accepted facts. `--require-clean-gate` makes apply stricter: any review or rejected candidate prevents applying accepted facts.

`extract-batch --session-limit` controls how many eligible sessions are selected. `--episode-limit` controls how many visible/searchable episodes are included per session. `--limit` is retained only as a compatibility alias for session limit.

Batch `partial_failure` exits non-zero by default. Use `--allow-partial-failure` only when partial completion should be treated as a successful CLI exit.

## Prefilter

The prefilter wire schema is `memory_extraction_protocol.v0.1.prefilter`.

`routing_hint` officially supports:

- `extract`
- `forget_manager`
- `pin_manager`
- `skip`
- `review`

Only `skip` with `keep=false` can drop an episode. `forget_manager`, `pin_manager`, and `review` are kept for extraction or routing. Legacy `route` responses are accepted as a backward-compatible alias for keep/review, but new prompts and integrations should not emit it.

## Providers

MemoryCore includes:

- `mock`: deterministic local provider for tests and smoke runs.
- `openai-compatible`: optional standalone HTTP provider for `/v1/chat/completions` smoke testing.

The OpenAI-compatible provider reads the key from an environment variable named by `--api-key-env`. Errors and audit rows may include the env var name, but never the key value or raw provider response body.

## Audit

`extraction_runs` stores metadata only: ids, mode/status, fingerprint, provider/model identifiers, prompt versions, counts, hashes, token usage, duration, and sanitized error fields.

It must not store raw prompts, raw model responses, episode content, candidate reasoning, HTTP response bodies, or API keys.

LLM request metadata is also non-sensitive: it carries identifiers such as request id, prompt version, purpose, and schema version, but not the full request JSON or episode content.

## Future EmoAgent Integration

Future EmoAgent integration should inject an adapter implementing `memorycore.ExtractionLLM` and call `pkg/memorycore/extractionruntime`.

MemoryCore CLI does not provide `--provider emoagent`, and MemoryCore does not import EmoAgent `internal/llm` or any other EmoAgent internal package.
