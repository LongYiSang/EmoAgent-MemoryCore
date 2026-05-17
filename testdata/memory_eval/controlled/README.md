# Controlled Eval Fixtures

Fixtures in this directory intentionally use one or more eval stubs:

- `mirror_stub`
- `graph_activation_stub`
- `rerank_stub`

These are controlled regression tests. They bypass parts of candidate
generation so the test can isolate downstream behavior such as SQLite authority
filtering, source visibility, historical status, causal/context reconstruction,
rerank safety, degradation handling, and forbidden recall.

Do not treat these files as end-to-end retrieval quality benchmarks.
