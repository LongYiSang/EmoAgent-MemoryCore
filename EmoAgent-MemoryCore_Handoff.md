# EmoAgent-MemoryCore 构建交接文件

> 用途：把本文件交给新的 Agent 对话。新的 Agent 在你创建好 `EmoAgent-MemoryCore` 文件夹后，先读取本文件，再按这里的范围进行构建。
>
> 读取要求：所有中文文档用 UTF-8 读取，避免乱码。

---

## 1. 当前决策

采用独立核心仓库方案：

```text
工程实现：独立项目 EmoAgent-MemoryCore
语义归属：EmoAgent / Emotion 长期状态层
调用方式：EmoAgent 通过 Memory Client / Adapter 调用 MemoryCore
第一阶段：Go + SQLite + CLI + 单测闭环
```

核心判断：

- MemoryCore 可以独立开发、调试、测试。
- 它不是通用 Agent 记忆平台，而是 EmoAgent 专用的长期关系记忆核心。
- EmoAgent 负责“怎么使用记忆来回复用户”。
- MemoryCore 负责“怎么存、怎么合并、怎么检索、怎么忘记”。

---

## 2. 关键架构约束

必须保持这些边界：

1. SQLite 是 source of truth。
2. TriviumDB 只是 retrieval mirror，可重建、可丢弃、可延迟同步。
3. Memory 属于 Emotion 长期状态层，不属于 Work 执行日志层。
4. Work 不能直接写长期记忆，只能提交 `memory_candidates`，由 Emotion / Memory policy 审批。
5. Episode 是证据，不是事实。
6. Fact 是节点，不是边。
7. Fact 必须保留三层状态：`validity_status` / `visibility_status` / `lifecycle_status`，不能合并。
8. 删除是功能，不是故障；任何可检索文本都必须能被 `hard_forget` / `purge` 清理。
9. Tombstone 不保存原文，不进入搜索索引，不进入 prompt。
10. 检索结果必须在回传 Emotion 前经过 SQLite 权威过滤。
11. Agent Affect 只做 v0 占位；不能写成用户事实，不能绕过 visibility / sensitivity / forget / purge。
12. 第一阶段不要实现 Python Sidecar、TriviumDB、gRPC、PPR/MMR/Hub Suppression、自动 retention job、Agent 情感状态机。

---

## 3. 必读文档路径

原始文档目录：

```text
D:\Dev\Project\Agent\Memory Design
```

新 Agent 不要一次性探索全部文档。按阶段读取下面文件。

### 3.1 第一优先级

```text
D:\Dev\Project\Agent\Memory Design\记忆总体架构\EmoAgent_LongTermMemory_FinalArchitecture_v2.2_AgentAffectPlaceholders.md
D:\Dev\Project\Agent\Memory Design\SQLite schema\memory_schema.md
D:\Dev\Project\Agent\Memory Design\SQLite schema\memory_schema_0001_foundation.sql
D:\Dev\Project\Agent\Memory Design\SQLite schema\memory_schema_0002_graph_policy_index.sql
D:\Dev\Project\Agent\Memory Design\SQLite schema\memory_schema_0003_affect_audit.sql
D:\Dev\Project\Agent\Memory Design\SQLite schema\memory_schema_0004_search_fallback_optional.sql
```

读取重点：

- 总架构第 1、2、4、5、7、8、10、13、16、17、19、20 节。
- SQLite schema 的 ID、状态枚举、表分组、索引原则、应用层约束。
- SQL migration 文件可以作为第一版迁移的基础，但要根据新项目的迁移框架整理。

### 3.2 写入链路需要时读取

```text
D:\Dev\Project\Agent\Memory Design\记忆抽取(Pre)架构\memory_extraction_protocol.md
D:\Dev\Project\Agent\Memory Design\记忆整合\memory_consolidation.md
```

读取重点：

- `Extractor proposes. SQLite / Consolidation disposes.`
- `Extraction proposes. Consolidation decides. SQLite records. Trivium mirrors.`
- Extraction 输出 JSON schema。
- Predicate schema、冲突处理、`insert / discard_duplicate / supersede / merge / coexist`。

### 3.3 读取和删除链路需要时读取

```text
D:\Dev\Project\Agent\Memory Design\记忆检索-激活\memory_retrieval_activation.md
D:\Dev\Project\Agent\Memory Design\记忆删除(用户要求)\memory_forgetting_privacy.md
D:\Dev\Project\Agent\Memory Design\记忆删除(自然淡化)\memory_retention_lifecycle.md
```

读取重点：

- `Trivium retrieves. SQLite validates. Memory assembles. Emotion speaks.`
- Phase 1 只做 SQLite-only retrieval，不做 Trivium activation。
- 删除权威在 SQLite。
- `soft_forget` / `hard_forget` / `source_redact` / `purge` 的语义和级联边界。
- 自然淡化第一阶段不做自动 purge；若要 purge，必须走 Forget/Purge Manager。

### 3.4 Agent Affect 占位需要时读取

```text
D:\Dev\Project\Agent\Memory Design\记忆-情绪预留接口\agent_affect_schema_api.md
D:\Dev\Project\Agent\Memory Design\记忆-情绪预留接口\memory_emotion_coupling.md
D:\Dev\Project\Agent\Memory Design\记忆-情绪预留接口\agent_affect_simulation.md
```

读取重点：

- 第一阶段只做 neutral stub、DTO、可选表占位。
- 不实现 appraisal model、PAD/VA 状态机、情绪惯性、策略学习。

---

## 4. 建议新项目路径

```text
D:\Dev\Project\Agent\EmoAgent-MemoryCore
```

建议 Go module：

```text
github.com/longyisang/emoagent-memorycore
```

建议技术选择：

- Go 1.26.1 主项目。
- SQLite driver 使用 `modernc.org/sqlite`，保持纯 Go、无 CGO，方便与 EmoAgent 主项目一致。
- 配置可先用 YAML 或 flags；第一阶段优先 CLI 参数。
- 第一阶段不启动服务进程；先做 `memoryctl` 和单测。

原项目目录

```text
D:\Dev\Project\Agent\EmoAgent
```

---

## 5. 第一阶段项目结构

先建这个最小结构：

```text
EmoAgent-MemoryCore/
├── cmd/
│   └── memoryctl/
│       └── main.go
├── internal/
│   ├── core/
│   │   ├── episode.go
│   │   ├── entity.go
│   │   ├── fact.go
│   │   ├── link.go
│   │   ├── memory_context.go
│   │   └── types.go
│   ├── store/
│   │   └── sqlite/
│   │       ├── db.go
│   │       ├── migrations.go
│   │       ├── episode_repo.go
│   │       ├── entity_repo.go
│   │       ├── fact_repo.go
│   │       ├── link_repo.go
│   │       └── search_repo.go
│   ├── write/
│   │   ├── candidates.go
│   │   ├── consolidator.go
│   │   ├── orchestrator.go
│   │   └── pin.go
│   ├── read/
│   │   ├── query.go
│   │   ├── retriever.go
│   │   ├── reconstructor.go
│   │   └── assembler.go
│   ├── forgetting/
│   │   ├── manager.go
│   │   └── policy.go
│   └── affect/
│       └── neutral.go
├── migrations/
│   ├── 0001_foundation.sql
│   ├── 0002_graph_policy_index.sql
│   ├── 0003_affect_audit.sql
│   └── 0004_search_fallback_optional.sql
├── testdata/
│   ├── fixtures/
│   └── golden/
├── docs/
│   └── architecture/
├── go.mod
└── README.md
```

暂时不要创建 `python_sidecar/`、`cmd/memoryd/`、`api/proto/` 的实装代码。可以在 README 写成 Phase 2。

---

## 6. 第一阶段目标

目标是跑通 SQLite 权威闭环：

```text
append episode
→ manual pin 或 mock/rule candidate
→ consolidate facts/entities/links
→ retrieve MemoryContext
→ forget/purge
→ 再 retrieve 时不能召回已删除内容
```

第一阶段命令建议：

```text
memoryctl init-db --db ./data/memory.db
memoryctl append-episode --db ./data/memory.db --persona default --session s1 --role user --content "..."
memoryctl pin --db ./data/memory.db --persona default --session s1 --fact-json ./testdata/fixtures/pin_fact.json
memoryctl list-facts --db ./data/memory.db --persona default
memoryctl retrieve --db ./data/memory.db --persona default --query "..."
memoryctl forget --db ./data/memory.db --persona default --target fact_x --level hard_forget
memoryctl get-node --db ./data/memory.db --persona default --type fact --id fact_x
```

---

## 7. 第一阶段非目标

不要在第一阶段实现：

- Python Sidecar。
- TriviumDB 实际接入。
- gRPC / Protobuf 服务。
- HTTP 服务完整化。
- Dense embedding。
- Spreading Activation / PPR / Hub Suppression / Refractory Fatigue。
- MMR / DPP。
- Narrative / Insight 自动生成。
- Deep Archive Worker。
- 定时 retention job。
- Agent Affect 实际情感算法。
- 多 persona 复杂隔离策略。
- 通用化插件平台。

可以保留：

- `index_sync_queue` 表。
- Trivium adapter interface 或 no-op stub。
- Agent Affect DTO / neutral service。
- Future API contract 文档。

---

## 8. 对外接口边界

第一阶段先设计 Go interface，不必立刻做 HTTP/gRPC。

建议接口：

```go
type Core interface {
    AppendEpisode(ctx context.Context, req AppendEpisodeRequest) (AppendEpisodeResult, error)
    SubmitMemoryCandidates(ctx context.Context, req SubmitMemoryCandidatesRequest) (SubmitMemoryCandidatesResult, error)
    PinMemory(ctx context.Context, req PinMemoryRequest) (PinMemoryResult, error)
    ForgetMemory(ctx context.Context, req ForgetMemoryRequest) (ForgetMemoryResult, error)
    RetrieveMemoryContext(ctx context.Context, req RetrieveMemoryContextRequest) (MemoryContext, error)
    DebugGetMemoryNode(ctx context.Context, req DebugGetMemoryNodeRequest) (DebugMemoryNode, error)
}
```

不要把这些内部步骤暴露给 EmoAgent 作为稳定外部 API：

```text
ExtractMemoryCandidates
ConsolidateCandidates
RunActivation
ApplyMMR
SyncTrivium
```

这些属于 MemoryCore 内部 pipeline。EmoAgent 只需要交 episode、交候选、请求上下文、请求删除/记住。

---

## 9. 初始实现顺序

### Step 1: 初始化项目

- 创建 `go.mod`。
- 引入 `modernc.org/sqlite`。
- 建 `cmd/memoryctl`。
- 建 SQLite open/close/migrate 框架。
- 添加 `go test ./...` 可跑通。

### Step 2: 迁移和 Repository

- 从 `SQLite schema` 目录迁移 0001-0004 SQL。
- 先确保 schema 可在空库执行。
- 实现基本 repo：
  - episodes
  - entities
  - entity_aliases
  - facts
  - memory_links
  - predicate_schemas
  - memory_search_documents

### Step 3: CLI 最小调试

- `init-db`
- `append-episode`
- `list-facts`
- `get-node`

### Step 4: 写入闭环

- 实现 manual pin。
- 实现 mock/rule candidate 输入。
- 实现 entity resolve 的最小版本：精确 canonical name / alias match，不做 embedding 自动合并。
- 实现 consolidation 的最小版本：
  - insert
  - discard duplicate
  - supersede
  - coexist
- 写入 `EVIDENCED_BY` 和 `ABOUT_ENTITY` links。

### Step 5: SQLite-only Retrieval

- entity alias match。
- keyword search 或 `memory_search_documents` fallback。
- 只返回 `visible + searchable + valid`。
- 按 importance / recency 简单排序。
- 输出 `MemoryContext`，不让 Emotion 直接看表结构。

### Step 6: Forget / Purge

- 先做 exact fact / exact episode scope。
- `soft_forget`：隐藏，不主动提起。
- `hard_forget`：清事实内容、search docs、links 可见性、sync delete queue。
- `source_redact`：清 episode 原文，写 tombstone。
- `purge`：语义 purge + retained audit stub。
- 验证删除后 retrieve 不返回目标内容。

### Step 7: 测试

至少覆盖：

- migration from empty DB。
- append episode preserves prev/next relation。
- pin fact writes evidence/about links。
- supersede invalidates old fact and links new fact.
- retrieve obeys visibility/searchable/validity filters。
- hard_forget removes searchable text。
- purge tombstone does not preserve original content。
- Agent Affect neutral fallback does not affect retrieval。

---

## 10. EmoAgent 集成预留

未来 EmoAgent 主仓库只应保留轻量 client/adapter，例如：

```text
EmoAgent/
└── internal/
    └── memoryclient/
        ├── client.go
        └── dto.go
```

集成点：

- 用户消息存储后：`AppendEpisode`
- Emotion 回复前：`RetrieveMemoryContext`
- 用户要求记住：`PinMemory`
- 用户要求忘记/删除：`ForgetMemory`
- Work 返回候选后：Emotion 审批，再 `SubmitMemoryCandidates`

不要让 EmoAgent 直接写 MemoryCore 的 SQLite 表。

---

## 11. 验收标准

新项目第一轮完成时，至少应满足：

```text
go test ./...
memoryctl init-db 成功
memoryctl append-episode 成功
memoryctl pin 成功写入 fact + links
memoryctl retrieve 能返回 prompt-ready MemoryContext
memoryctl hard_forget 后 retrieve 不再返回目标内容
```

README 应清楚写明：

- 这是 EmoAgent 专用 MemoryCore。
- 第一阶段是 SQLite-only。
- TriviumDB/Python/gRPC 是 Phase 2。
- 删除和隐私链路优先级高于高级检索。

---

## 12. 给新 Agent 的工作方式要求

1. 先读取本文件。
2. 只按“必读文档路径”读取相关文档，不要全目录扫描。
3. 如果需要更多上下文，先说明需要哪个文档的哪个章节。
4. 优先构建可运行骨架，不要一开始实现最终架构所有组件。
5. 每一步都用测试或 CLI 验证。
6. 遇到 schema 与实现冲突时，以 SQLite source-of-truth、删除可验证、Emotion/Work 边界为最高优先级。
