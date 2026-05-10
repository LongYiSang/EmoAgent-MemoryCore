# MemoryCore CLI Smoke Demo

This smoke demo exercises the Phase2A operational CLI path:

- initialize a fresh SQLite DB
- create a CLI session
- append an episode
- ensure a `User` entity
- consolidate a manual fact
- retrieve the fact by query
- hard-forget the fact
- rebuild search documents
- inspect the forgotten fact safely
- verify retrieval and default listing do not leak forgotten content

Run from the repository root:

```bash
bash examples/smoke/run.sh
```

Expected success output:

```text
SMOKE PASS
```

Preserve the DB for manual inspection:

```bash
MEMORY_DB=./tmp/memory-smoke.db bash examples/smoke/run.sh
```

Manual inspection after a preserved run:

```bash
go run ./cmd/memoryctl list-facts --db ./tmp/memory-smoke.db
go run ./cmd/memoryctl get-node --db ./tmp/memory-smoke.db --node-type fact --id <fact_id> --include-forgotten
```

Troubleshooting:

- If `go run` fails, run `go test ./...` first to verify the Go toolchain and module cache.
- If the retrieval assertion fails, check that `consolidate-fact --format id` returned a non-empty fact ID.
- If forgotten content appears after `hard_forget`, inspect `get-node --include-forgotten`; it must only show status fields and placeholders for forgotten semantic content.
