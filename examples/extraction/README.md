# Extraction smoke demo

This demo exercises the Phase2B extraction adapter without calling an LLM.

It proves:

- `extract-request` builds a request from visible/searchable SQLite episodes.
- `extract-dry-run` accepts an explicit preference candidate.
- `extract-apply` writes accepted fact candidates through `memorycore.ConsolidateCandidate`.
- manual forget responses route deletion intents only.
- agent affect leakage is rejected by the Go gate.

Run:

```bash
bash examples/extraction/run.sh
```

