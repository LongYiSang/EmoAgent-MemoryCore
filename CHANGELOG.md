# Changelog

## v0.1.0-alpha.1

### Supported

- SQLite authoritative memory store.
- Go public facade under `pkg/memorycore`.
- Session / Episode / Entity / Fact consolidation.
- Extraction runtime with mock and OpenAI-compatible provider.
- Retrieval v5 with SQLite authority filtering and optional sidecar candidates.
- Exact fact/episode forget MVP.
- Retention jobs.
- Config loader and validation.
- About / Capabilities API.
- Observability snapshot API.

### Experimental

- Broad forget preview.
- Natural memory cycle.
- Compression storage contract.
- Delta curation runner.

### Optional

- Mirror sync / rebuild path.
- Sidecar retrieval candidates, graph activation, and rerank signals.

### Host-owned / Out of scope

- Agent Affect is owned by EmoAgent.
- User Mood / Relationship Affect write loop is owned by EmoAgent.
- Scheduler / daemon ownership stays in EmoAgent.

### Not supported yet

- Full entity/topic/semantic forget cascade.
- Review queue for `llm_check` / `needs_review`.
- Production-grade multi-instance scheduling.
- Prometheus/OpenTelemetry metrics.
