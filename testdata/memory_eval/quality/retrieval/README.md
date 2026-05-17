# MemoryCore Retrieval Quality Worlds

本目录存放人工观察用的检索质量测试样例。每个 `W00X_*.yaml`
都是一个独立的 synthetic memory world：先构造一组记忆事实、关系和干扰项，
再提出若干检索问题，最后用断言描述“应该召回什么、不应该暴露什么、应当用什么上下文块表达”。

这些文件用于评估真实检索质量，不是 controlled regression。这里的样例必须满足：

- `suite: quality_retrieval`
- `quality_mode: true`
- `allow_stub: false`
- 不得使用 `mirror_stub`、`graph_activation_stub`、`rerank_stub`
- 不得使用真实用户聊天记录，即使脱敏也不要放进公开 fixture
- 预期结果只能通过 assertions 描述，不能把答案伪造成 sidecar 返回值

## 一键运行

跑整个目录的完整 profile matrix：

```powershell
cd D:\Dev\Project\Agent\EmoAgent-MemoryCore
scripts\memory_eval_matrix.cmd
```

只跑某一个 world：

```powershell
scripts\memory_eval_matrix.cmd `
  -Fixture testdata\memory_eval\quality\retrieval\W001_work_rhythm_no_stub.yaml
```

只跑 SQLite baseline：

```powershell
scripts\memory_eval_matrix.cmd `
  -Fixture testdata\memory_eval\quality\retrieval\W001_work_rhythm_no_stub.yaml `
  -Profiles sqlite_go
```

默认输出目录：

```text
artifacts\memory_eval\manual-YYYYMMDD-HHMMSS\
  reports\
    report.md
    detail.md
    report.json
    <case_id>\
      report.md
      detail.md
      report.json
  mirrors\
  embedding-cache\
  tmp\
  logs\
```

顶层 `reports\report.md` / `detail.md` 是本次运行所有 world 的汇总。
`reports\<case_id>\...` 是单个 world 的报告。

## World 文件结构

最小结构：

```yaml
schema_version: memory_eval.v0.2
suite: quality_retrieval
quality_mode: true
allow_stub: false
case_id: W999_example_no_stub
description: 简短说明这个 world 要测什么。

seed:
  sessions: []
  entities: []
  episodes: []

steps: []
assertions: []
```

推荐命名：

```text
W001_work_rhythm_no_stub.yaml
W002_memorycore_ops_no_stub.yaml
W003_relationship_building_no_stub.yaml
```

`case_id` 应与文件名主干一致，便于报告目录和日志定位。

## 编写 World 的推荐流程

1. 先定义测试主题。

   一个 world 只覆盖一个清晰主题，例如“工作节律记忆”、“MemoryCore 操作经验”、
   “关系建立与偏好”。不要把很多无关主题塞进一个文件。

2. 写 synthetic episodes。

   `seed.episodes` 是记忆来源，内容应像真实对话留下的事实来源，但必须是合成数据。
   如果要测试 provenance/source ref，相关 fact 必须挂到 episode。

3. 写 entities。

   至少包含 `user`。涉及地点、人物、宠物、Agent 等对象时，显式写 entity。
   如果要测 alias/exact match，可给 entity 添加 `aliases`。

4. 写 facts 和 links。

   在 `steps` 里用 `action: fact` 插入可检索事实，用 `action: link` 建立关系。
   对比项和错误项也要写进 world，但应通过 `visibility_status: hidden` 或
   `searchable: false` 变成 forbidden/distractor。

5. 执行 `rebuild_search`。

   质量检索通常需要在所有 fact/link 之后添加：

   ```yaml
   - id: rebuild_search
     action: rebuild_search
     rebuild_search: {}
   ```

   没有搜索文档时，FTS/sparse/ranking 行为可能不是你想测的内容。

6. 写 retrieve 问题。

   每个 `action: retrieve` 是一个问题。`id` 用 `q001_...` 这类稳定名字。
   `query_text` 要接近用户自然提问，不要只写 fact id 或过度贴合答案的机械关键词。

7. 写 assertions。

   每个问题至少要有一个正向断言；重要场景还应添加 forbidden 或 premise 断言。
   断言只描述应该出现/不出现的行为，不注入运行时结果。

8. 先跑 SQLite baseline，再看 mirror/graph/rerank。

   如果 `sqlite_go` 都失败，先修 world 或 baseline 预期，再看真实 sidecar profile。

## 常用字段

### `seed.sessions`

定义会话，retrieve step 可以通过 `session_id` 使用它。

```yaml
- id: s_q001_fact
  channel: api
```

### `seed.entities`

定义可被 fact/link 引用的实体。

```yaml
- id: user
  canonical_name: EvalUser
  entity_type: user
- id: xingqiao_apartment
  canonical_name: 星桥公寓
  entity_type: place
```

### `seed.episodes`

定义来源事件。

```yaml
- id: ep_evening_deep_work
  session_id: s_seed
  content: 我通常晚上九点以后更适合处理深度工作。
  occurred_at: "2026-04-20T21:00:00+08:00"
```

可选权限字段：

- `visibility_status: hidden`
- `searchable: false`
- `sensitivity_level: sensitive`

### `steps[].fact`

插入 fact。常用字段：

```yaml
- id: f_evening_deep_work
  action: fact
  fact:
    subject_entity_id: user
    predicate: prefers_work_time
    object_literal: 晚上九点以后深度工作
    content_summary: 用户通常晚上九点以后更适合处理深度工作。
    fact_type: stable_preference
    source_episode_ids: [ep_evening_deep_work]
    confidence: explicit
    confidence_score: 0.95
    importance: 0.9
```

常用控制字段：

- `visibility_status: hidden`：不可见，不应被选入上下文。
- `searchable: false`：不进入检索候选。
- `validity_status: invalidated`：历史/失效 fact，通常需要 `allow_historical` 才能查。
- `lifecycle_status: archived` / `deep_archived`：生命周期门控。
- `sensitivity_level`：敏感级别，受 retrieval policy 限制。

### `steps[].link`

建立 fact/entity 关系，常用于历史替代、因果链、关联链：

```yaml
- id: link_location_supersedes
  action: link
  link:
    from_node_id: $f_current_location_xingqiao.fact_id
    link_type: SUPERSEDES
    to_node_id: $f_old_location_qinghe.fact_id
    weight: 1.0
```

常用 link type：

- `SUPERSEDES`：当前事实替代旧事实。
- `CAUSED_BY`：一个事实由另一个事实导致。

### `steps[].retrieve`

发起检索：

```yaml
- id: q003_historical_old_location
  action: retrieve
  retrieve:
    session_id: s_q003_historical
    query_text: 青禾园 以前住在哪里
    policy:
      allow_historical: true
      final_memory_count: 4
```

常用 policy：

- `final_memory_count`：最终最多选入多少条记忆。
- `context_budget_tokens`：上下文预算。
- `allow_historical`：允许召回 invalidated/superseded 历史事实。
- `allow_deep_archive`：允许 deep archive。
- `sensitivity_permission`：允许的敏感级别。
- `use_fts`：是否使用 FTS。
- `use_mirror`：是否使用 mirror。matrix profile 通常会由 runner 控制，不建议在 quality world 里乱改。

## 常用断言

### `selected_recall_at_k`

检查最终 selected context 的前 `k` 条是否召回相关节点。

```yaml
- type: selected_recall_at_k
  name: q001 recalls target
  step: q001_fact_deep_work_time
  relevant_node_ids:
    - $f_evening_deep_work.fact_id
  at: 4
  min: 1.0
```

适合所有事实召回类问题。`min: 1.0` 表示相关节点必须全部命中。

### `context_precision_at_k`

检查前 `k` 条 selected context 中相关项占比。

```yaml
- type: context_precision_at_k
  name: q001 selected context stays focused
  step: q001_fact_deep_work_time
  relevant_node_ids:
    - $f_evening_deep_work.fact_id
  at: 4
  min: 0.5
```

适合测试“不要混进太多无关记忆”。

### `forbidden_recall_zero`

检查隐藏、错误、隐私或不应出现的节点没有被选中，也没有通过 source ref 泄漏。

```yaml
- type: forbidden_recall_zero
  name: q001 excludes hidden wrong preference
  step: q001_fact_deep_work_time
  forbidden_node_ids:
    - $f_morning_work_preference_wrong.fact_id
```

这是质量测试里最重要的安全断言之一。

### `query_analysis`

检查 query analyzer 是否把问题分到正确意图。

```yaml
- type: query_analysis
  name: q003 is routed as historical
  step: q003_historical_old_location
  time_mode: historical
  memory_ability: historical
  evidence_need: state_transition
```

常用字段：

- `time_mode`: `current` / `historical`
- `memory_domain`: 例如 `work_experience_memory`
- `memory_ability`: 例如 `historical`、`provenance`、`causal_explain`、`workflow`、`gotcha`、`premise_check`
- `evidence_need`: 例如 `state_transition`、`provenance_source`、`procedure_note`、`gotcha_note`、`premise_counterexample`

### `selected_chain_correct`

检查 selected item 的 block type、相关节点、关系方向、历史状态和 source ref。

```yaml
- type: selected_chain_correct
  name: q007 causal context includes cause link
  step: q007_causal_morning_avoidance
  block_type: causal_context
  node_id: $f_morning_task_avoidance.fact_id
  node_ids: [$f_dislikes_8am_meeting.fact_id]
  link_type: CAUSED_BY
  direction: outbound
  historical_status: current
  related_historical_status: current
```

适合测因果、历史替代、来源链和关系重构。

### `block_contains` / `block_not_contains`

检查某个上下文块是否包含或不包含某个节点。

```yaml
- type: block_contains
  name: q010 workflow retrieves worlds-first design order
  step: q010_workflow_eval_questions
  block_type: experience_context
  node_id: $f_synthetic_worlds_before_questions.fact_id
```

### `unsupported_premise_not_asserted`

检查系统没有顺着用户问题里的错误前提胡说，并能选择反例。

```yaml
- type: unsupported_premise_not_asserted
  name: q013 selects counterexample and avoids broad premise
  step: q013_premise_hates_work
  node_ids:
    - $f_pair_debug_positive_counterexample.fact_id
  forbidden_node_ids:
    - $f_hates_work_generalization.fact_id
  forbidden_contains:
    - 一直都讨厌上班
```

适合测“是不是一直讨厌上班”“是否暴露 redacted 原文”等带错误前提或隐私风险的问题。

## Profile 是什么

一键脚本默认比较这些检索条件：

| profile | 含义 |
| --- | --- |
| `sqlite_go` | 只使用 Go + SQLite 检索，作为 baseline。 |
| `mirror_real_dense` | 使用真实 sidecar + Trivium mirror candidate，不启用 graph，不启用 rerank。 |
| `mirror_real_graph` | 在 mirror candidate 基础上启用 graph activation。 |
| `mirror_real_graph_rerank` | mirror + graph activation + live rerank。 |
| `mirror_real_rerank_no_graph` | mirror + live rerank，但不启用 graph，用于隔离 rerank 增益。 |

每个 `W00X` 都是独立 fixture：

- SQLite seed DB 按 world/profile 独立创建。
- Trivium mirror artifact 按 world 的 stable hash 独立创建。
- 同一个 world 的多个 mirror profile 会尽量复用同一份 mirror artifact。
- embedding cache 在同一次 run 下可共享；相同文本、模型、维度和版本会复用向量。
- rerank 不做 cache；每个 rerank profile 的每个 retrieval 问题都会单独 live rerank。

## 报告怎么看

### `report.md`

摘要报告，适合先扫 profile 是否通过、指标是否异常。

每个 case/profile 会有：

```text
profile: mirror_real_graph
status: pass
capability: ready
selected_recall_at_8: 1.000
precision_at_8: 0.750
forbidden_recall_rate: 0.000
fallback_count: 0
graph_activation_used_count: 5
rerank_live_call_count: 0
embedding_cache_hits: 10
embedding_cache_misses: 2
embedding_live_call_count: 2
```

### `detail.md`

人工审阅主报告。结构是：

```text
question_id: q001_fact_deep_work_time
问题: 晚上九点以后 深度工作
期望:
  - ...

profile: sqlite_go
status: pass
结果:
  PASS ...
实际结果:
  selected:
    - ...

profile: mirror_real_graph
...
```

看这个文件可以直接回答：

- 这个问题问了什么？
- gold/期望是什么？
- 每个 profile 实际选了哪些记忆？
- 失败时 expected/actual 差在哪里？

### `report.json`

结构化报告，适合后续脚本分析、导表或做可视化。人工判断优先看 `report.md` 和 `detail.md`。

## 指标释义

| 指标 | 含义 | 判断方式 |
| --- | --- | --- |
| `status` | profile 结果，`pass` / `fail` / `skip`。 | `fail` 先看 `error` 和 `detail.md`。 |
| `capability` | 当前 profile 所需能力是否可用。 | real mirror/rerank 应为 `ready`。 |
| `selected_recall_at_8` | 根据 `selected_recall_at_k` 断言计算的最终召回率。 | 越高越好，目标通常是 1.0。 |
| `candidate_recall_at_80` | 候选池是否包含 relevant nodes。 | 高但 selected 低，说明候选找到了但排序/筛选没选上。 |
| `precision_at_8` | selected context 中 relevant nodes 占比。 | 越高越好，低说明混入无关记忆。 |
| `mrr` | 第一个 relevant node 的倒数排名均值。 | 越高说明相关答案越靠前。 |
| `ndcg_at_8` | 考虑排序位置的归一化增益。 | 越高说明相关项排序越好。 |
| `causal_chain_coverage` | `selected_chain_correct` 断言通过比例。 | graph profile 应重点观察。 |
| `context_precision` | `context_precision_at_k` 的平均值。 | 越高说明上下文更聚焦。 |
| `forbidden_recall_rate` | forbidden 节点是否被选中。 | 必须为 0。 |
| `authority_filter_violation_count` | forbidden 或 authority 过滤违规数量。 | 必须为 0。 |
| `forbidden_selected_count` | 被选中的 forbidden 节点数量。 | 必须为 0。 |
| `fallback_count` | profile 要求真实 sidecar/graph/rerank，但发生 fallback 的次数。 | real profile 应为 0。 |
| `sidecar_degraded_count` | sidecar 阶段 degraded 次数。 | 越少越好；严格质量测试通常应为 0。 |
| `graph_activation_used_count` | graph activation 实际使用次数。 | `mirror_real_graph*` 应大于 0。 |
| `graph_required_but_not_used_count` | 需要 graph 但未执行的次数。 | 必须为 0。 |
| `mirror_used_count` | mirror candidate 实际使用次数。 | mirror profile 中用于确认真实路径被走到。 |
| `rerank_live_call_count` | live rerank 实际调用次数。 | rerank profile 应大于 0。 |
| `rerank_required_but_not_used_count` | 需要 rerank 但未使用的次数。 | 必须为 0。 |
| `embedding_cache_hits` | embedding cache 命中次数。 | 复跑时通常会上升。 |
| `embedding_cache_misses` | embedding cache 未命中次数。 | 第一次 run 通常较高。 |
| `embedding_live_call_count` | 实际调用 embedding provider 次数。 | 成本观察指标。 |
| `stub_used_count` | 是否使用 eval stub。 | quality world 必须为 0。 |
| `p50_latency_ms` / `p95_latency_ms` | 延迟指标预留。 | 当前主要看 sidecar latency 聚合，解释时谨慎。 |
| `mirror_manifest_hash` | mirror artifact manifest 指纹。 | 用于确认复用的是哪份 mirror 快照。 |

## Deltas 怎么看

`report.md` 末尾可能出现：

```text
dense_vs_sqlite.selected_recall_at_8
graph_vs_dense.causal_chain_coverage
rerank_vs_graph.precision_at_8
```

含义：

- `dense_vs_sqlite.selected_recall_at_8`：Trivium dense 相比 SQLite baseline 的最终召回变化。
- `graph_vs_dense.causal_chain_coverage`：graph activation 对因果链/关系链覆盖的增益。
- `rerank_vs_graph.precision_at_8`：live rerank 对最终上下文精度的增益。

delta 为正通常是增益；为负说明新阶段可能引入排序或候选污染，需要打开 `detail.md` 看具体问题。

## 常见失败诊断

### `selected_recall_at_8` 低

先看 `detail.md` 的 selected 列表：

- 如果 relevant node 完全没进候选，检查 query wording、search document、entity alias、fact searchable。
- 如果候选存在但没进 selected，检查 ranking/MMR/context budget/fatigue。
- 如果 SQLite baseline 失败，先修 world 或 baseline，再分析 mirror。

### `precision_at_8` 低

说明选入了太多无关内容。常见原因：

- world 里干扰项太多但没有 forbidden/context precision 断言。
- query 过宽，无法区分目标事实。
- `final_memory_count` 过大。

### `forbidden_recall_rate > 0`

这是硬失败。检查：

- forbidden fact 是否设置了 `visibility_status: hidden` 或 `searchable: false`。
- 是否通过 `SourceRefs[].EpisodeID` 泄漏了 hidden/redacted episode。
- 是否有 relation 或 reconstruction 把 forbidden 内容带回上下文。

### `fallback_count > 0`

real profile 没有真正跑到它要求的能力。检查：

- sidecar 是否健康。
- Trivium mirror 是否构建成功。
- `mirror_real_graph*` 是否实际执行 graph activation。
- rerank profile 是否设置 `MEMORYCORE_RERANK_PROVIDER=dashscope-vl` 且有 `DASHSCOPE_API_KEY`。

### `capability: capability_missing`

这通常不是质量分数问题，而是环境能力缺失。先看 reason，例如：

- sidecar 没启动。
- embedding provider key 缺失。
- rerank provider 是 `none`。
- live rerank key 缺失。

## 编写经验

- 一个 world 测一个主题，不要变成大杂烩。
- 每个 retrieve 问题都要能映射到明确的 gold facts。
- 正向断言和 forbidden 断言要成对设计：既测“找得到”，也测“不乱说/不泄漏”。
- 历史问题必须加 `allow_historical: true`，并用 `selected_chain_correct` 检查 superseded/current。
- 来源问题要确保 fact 有 `source_episode_ids`，再用 `source_ref_count` 检查 provenance。
- 因果问题要显式建 `CAUSED_BY` link，否则 graph/chain 指标没有稳定 gold。
- premise/gotcha 问题要放入可选反例和隐藏错误前提，避免只测关键词召回。
- world 通过后再增加难度；不要一次性加太多事实和断言，否则失败定位成本高。
- 不要为了让测试过而降低 `min` 或移除 forbidden；先看 `detail.md` 中实际选了什么。
