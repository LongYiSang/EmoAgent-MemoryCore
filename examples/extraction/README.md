# Extraction smoke demo

This demo exercises the Phase2B extraction adapter without calling an LLM. Phase 2C also adds `extract-run` and `extract-batch` for real extraction runtime smoke tests.

It proves:

- `extract-request` builds a request from visible/searchable SQLite episodes.
- `extract-dry-run` accepts an explicit preference candidate.
- `extract-apply` writes accepted fact candidates through `memorycore.ConsolidateCandidate`.
- manual forget responses route deletion intents only.
- agent affect leakage is rejected by the Go gate.
- `extract-run --mode dry-run` does not write memory, though audit is on by default unless `--audit off` is set.
- future EmoAgent integration injects a `memorycore.ExtractionLLM` adapter; MemoryCore CLI has no `--provider emoagent`.

Run:

```bash
bash examples/extraction/run.sh
```

Phase 2C mock runtime smoke:

```bash
go run ./cmd/memoryctl extract-run \
  --db ./data/memory.db \
  --session session_seed \
  --provider mock \
  --mode dry-run \
  --audit off \
  --format json
```
