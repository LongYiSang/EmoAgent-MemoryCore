# Quality Retrieval Eval Fixtures

Fixtures in this directory must not use `mirror_stub`,
`graph_activation_stub`, or `rerank_stub`.

They seed synthetic memory worlds, rebuild SQLite search documents, and then
exercise the real local retrieval path. These tests are intended to measure
whether MemoryCore can retrieve the expected facts from its own SQLite search,
anchor, ranking, and context reconstruction pipeline.

Run them manually with:

```powershell
scripts\memory_eval_quality.cmd -Mode full
```
