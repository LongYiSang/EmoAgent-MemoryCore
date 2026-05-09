# EmoAgent MemoryCore

<p align="center">
  <em><strong>EmoAgent 的长期关系记忆核心。</strong></em>
</p>

<p align="center">
  <img alt="Language" src="https://img.shields.io/badge/语言-Go-00ADD8?logo=go&logoColor=white">
  <img alt="License" src="https://img.shields.io/badge/许可-Apache%202.0-blue">
  <img alt="Status" src="https://img.shields.io/badge/状态-alpha-orange">
  <img alt="Go Version" src="https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go">
</p>

---

## 概述

EmoAgent MemoryCore 是 EmoAgent 的**长期记忆子系统**，提供持久化的长期记忆能力。

---

## RoadMap

### 阶段 1：独立仓库骨架

- [x] 初始化 Go module 与基础目录结构。
- [x] 引入 SQLite schema 与迁移框架。
- [x] 建立 `personas` / `sessions` / `episodes` / `entities` / `facts` / `memory_links` 基础仓储。
- [x] 建立 `predicate_schemas` / `memory_index_map` / `index_sync_queue` 等策略与镜像边界表。
- [x] 提供 `memoryctl init-db`。
- [ ] 提供 `memoryctl append-episode`。
- [ ] 提供 `memoryctl list-facts`。
- [ ] 提供 `memoryctl get-node`。

### 阶段 2：写入路径 MVP

- [ ] 跑通 Episode append 的 CLI 与测试闭环。
- [ ] 实现手动 pin 写入 fact。
- [ ] 实现简单 mock / rule-based memory candidate 输入。
- [ ] 实现 entity resolve 的最小版本：精确 canonical name / alias match。
- [ ] 实现 consolidation 最小策略：`insert` / `discard_duplicate` / `supersede` / `coexist`。
- [ ] 写入 `EVIDENCED_BY` link。
- [ ] 写入 `ABOUT_ENTITY` link。
- [ ] 验证 supersede 会 invalidated 旧 fact，并保留新旧事实关系。

### 阶段 3：SQLite-only Retrieval

- [ ] 实现 entity alias match 召回。
- [ ] 实现 keyword search / SQLite fallback search。
- [ ] 实现 importance / recency baseline ranking。
- [ ] 实现 SQLite authority filter：只返回 `visible + searchable + valid/uncertain`。
- [ ] 组装 prompt-ready `MemoryContext`，避免向 Emotion 暴露表结构。
- [ ] 记录最小 access event，用于后续检索审计。

### 阶段 4：Forget / Privacy 闭环

- [ ] 实现 `soft_forget`：隐藏但不清空内容。
- [ ] 实现 `hard_forget`：清 fact 内容、link reasoning、search document。
- [ ] 实现 `source_redact`：清 Episode 原文并写入 tombstone。
- [ ] 实现 `purge`：全链路语义清理，仅保留不含原文的审计锚点。
- [ ] 写入 `deletion_events`。
- [ ] 清理 SQLite fallback search rows。
- [ ] 验证 hard forget / purge 后 retrieval 不再返回目标内容。

### 阶段 5：TriviumDB Retrieval Mirror

- [ ] 实现 index sync queue worker。
- [ ] 实现 Trivium `upsert_node` / `delete_node`。
- [ ] 实现 Trivium `upsert_edge` / `delete_edge`。
- [ ] 实现 mirror rebuild。
- [ ] 保证所有 mirror 结果回传前都经过 SQLite 权威过滤。
- [ ] 验证 delete sync 失败时，相关节点仍不会进入 prompt。

### 阶段 6：高级 Retrieval Activation

- [ ] 实现 Hybrid Anchor recall。
- [ ] 实现 Spreading Activation。
- [ ] 实现 PPR teleport。
- [ ] 实现 Hub suppression。
- [ ] 实现 Refractory fatigue。
- [ ] 实现 MMR / diversity reranking。
- [ ] 实现 Context Reconstruction。
- [ ] 建立检索质量与遗忘质量评估集。

---

## 架构

MemoryCore 采用**三层时序知识图谱（TKG-Lite）**，层次清晰、职责明确：

```
┌──────────────────────────────────────────────────────┐
│                   第三层：叙事层 (Narrative)           │
│  高层关系叙事与洞察                                   │
│  周/月 LLM 整合（规划中）                             │
│  "这段关系最近整体感觉如何？"                          │
└──────────────────────────────────────────────────────┘
                          ▲
┌──────────────────────────────────────────────────────┐
│                   第二层：事实层 (Fact)               │
│  双时间知识节点（主体 / 谓词 / 客体）                  │
│  溯源于 Episode 的完整链路                            │
│  三层状态：真实性 / 可见性 / 生命周期                  │
│  核心身份、偏好、边界、承诺等                          │
└──────────────────────────────────────────────────────┘
                          ▲
┌──────────────────────────────────────────────────────┐
│                   第一层：事件层 (Episode)             │
│  不可变原始事件流 — ground truth 锚点                  │
│  永不删除、永不修改                                   │
│  聊天消息、用户行为、系统事件                          │
└──────────────────────────────────────────────────────┘
```

### 数据流

**写入路径**（对话 → 存储）：

```
对话
  → Episode 同步写入（不可变）
  → 触发检测（会话结束 / 空闲 / 手动标记）
  → 预过滤（低成本 LLM，规划中）
  → 抽取（主力 LLM，规划中）
  → 整合（Go 逻辑：插入 / 替代 / 合并 / 丢弃）
  → Fact 存储（SQLite）
  → 定期晋升为 Narrative（规划中）
```

**读取路径**（存储 → 对话）：

```
新对话开始
  → 查询分析（实体、关键词、意图提取）
  → 多源检索（SQLite FTS5 + 未来向量检索）
  → 混合打分 + 情绪安全过滤
  → 上下文组装 → System Prompt 注入
```

### 系统拓扑

```
┌───────────────────────────────┐         ┌──────────────────────────────────┐
│         Go Service            │  gRPC   │      Python Sidecar（规划中）      │
│                               │◄───────►│                                  │
│  • 触发检测                    │         │  • 预过滤（低成本 LLM）              │
│  • Episode 同步写入            │         │  • 记忆抽取（主力 LLM）              │
│  • 整合逻辑                    │         │  • Embedding 生成                  │
│  • SQLite（source of truth）  │         │  • Qdrant 向量存储                  │
│  • FTS5 关键词检索             │         │  • 向量检索                         │
│  • 衰减懒计算                  │         │  • 周度整合（LLM）                  │
│  • 上下文组装 → prompt         │         │  • 叙事生成（LLM）                  │
│  • Pin / Forget 通道          │         │                                   │
└───────────────────────────────┘         └──────────────────────────────────┘
```

> **Phase 1 仅 SQLite。** Python Sidecar、TriviumDB（图检索镜像）、Embedding 生成、gRPC 服务均为后续阶段规划。当前聚焦于构建稳固的权威基础层。

### 三层状态模型

每条事实均维护三个独立的状态维度，永远不合并为单一状态：

| 维度 | 字段 | 含义 | 典型转换 |
|------|------|------|---------|
| **真实性** | `validity_status` | 该事实在现实世界中是否仍为真 | `valid` → `invalidated`（用户搬家了） |
| **可见性** | `visibility_status` | 系统是否允许使用该记忆 | `visible` → `hidden` → `forgotten` → `purged` |
| **生命周期** | `lifecycle_status` | 记忆处于热/温/冷哪个阶段 | `active` → `dormant` → `consolidated` → `archived` |

### 遗忘级别

遗忘被设计为一级功能，而非故障模式：

| 级别 | 触发 | 效果 |
|------|------|------|
| `soft_forget` | "别再老提这件事了" | 检索隐藏，内容保留 |
| `hard_forget` | "忘掉这个偏好" | 清空语义内容，保留最小锚点 |
| `source_redact` | "这段对话原文不要保留" | 删除 Episode 原文，派生事实可选保留 |
| `purge` | "彻底删除此事" | 全链路级联清理：事实 + 来源 + 派生 + 镜像 + 搜索 |

---

## 当前进度

**Phase 1 基础层**（✅ 已完成）：

- ✅ Go 模块（`github.com/longyisang/emoagent-memorycore`）
- ✅ CLI 入口（`cmd/memoryctl`）
- ✅ 嵌入式 SQLite 迁移（0001–0004）
- ✅ 完整 Schema：personas、sessions、episodes、entities、aliases、facts、narratives、insights
- ✅ 图层：predicate_schemas、memory_links、entity_cooccurrences
- ✅ Agent Affect v0 占位表
- ✅ 可选 FTS5 搜索降级

**规划中（Phase 2+）**：

- ⬜ 抽取管道（预过滤 + LLM 抽取 SubAgent）
- ⬜ 整合引擎（insert / supersede / merge / coexist 决策）
- ⬜ 遗忘与隐私管理器（级联删除）
- ⬜ 检索管道（SQLite 优先，关键词 + FTS5）
- ⬜ 情绪耦合（mood-safe 检索，情绪事件）
- ⬜ Python Sidecar（gRPC、Embedding、Qdrant）
- ⬜ 保留生命周期（自然衰减、归档）
- ⬜ 性能测评框架

---

## 快速开始

### 环境要求

- Go 1.26 或更高版本

### 初始化本地数据库

```bash
go run ./cmd/memoryctl init-db --db ./data/memory.db
```

该命令创建包含全部基础表、索引和种子谓词语法的 SQLite 数据库。

### 运行测试

```bash
go test ./...
```

---

## 项目结构

```
EmoAgent-MemoryCore/
├── cmd/
│   └── memoryctl/             # CLI 入口，数据库管理与运维
├── internal/
│   ├── core/                  # 领域类型：Episode、Fact、Entity、Link 等
│   └── store/
│       └── sqlite/            # SQLite 驱动、迁移、仓储层
├── migrations/                # 嵌入式 SQL 迁移文件
│   ├── 0001_foundation.sql    # Personas、sessions、episodes、entities、facts
│   ├── 0002_graph_policy.sql  # Predicate schemas、memory links、同步队列
│   ├── 0003_affect_audit.sql  # 情绪状态、Agent Affect 占位、删除审计
│   └── 0004_search_fallback.sql  # SQLite FTS5 降级搜索
├── testdata/                  # 测试夹具与 golden labels
├── docs/
│   └── architecture/          # 完整架构文档
├── go.mod
├── go.sum
├── LICENSE                    # Apache 2.0
└── README.md
```

---

## 核心概念

### Episode 是证据，不是事实

Episode 是对话中不可变的、仅追加的原始事件记录，是所有上层推理的 ground truth 锚点。Episode 永远不被修改或删除——可见性变更只影响内容是否被使用，不影响记录是否存在。

### Fact 是节点，不是边

Fact 是独立的知识节点，具有 `主体 → 谓词 → 客体` 结构，携带完整元数据：Provenance（来自哪些 Episode）、时间线（在现实世界中何时成立）、置信度、重要性、情感效价，以及三层状态。

### Predicate Schema 治理冲突处理

事实之间如何相互作用由 `predicate_schemas` 决定，而非简单的 `UPSERT(subject, predicate, object)`。例如：

- `lives_in`（单一基数，替代策略）：新居住地替代旧居住地。
- `likes`（多重基数，共存策略）：多个喜欢对象共存。
- `has_boundary`（多重基数，合并策略）：边界表达合并为更完整的边界事实。
- `is_busy_with`（时态性，过期策略）：短期上下文自然过期。

### SQLite 是 source of truth

SQLite 持有所有事实、状态、策略、删除审计、双时间线和图关系的**权威状态**。未来的 TriviumDB 或 Qdrant 镜像仅为**检索层**，可随时从 SQLite 清空并重建。

### 遗忘即功能

不会遗忘的陪伴系统不是陪伴，而是监控日志。高情感事件永久保留，琐碎细节自然衰减。用户可选择软遗忘（隐藏）、硬遗忘（清空内容）或彻底清除（全链路级联删除）。

---

## 架构文档

完整架构文档位于 `docs/architecture/`：

| 文档 | 内容范围 |
|------|---------|
| `记忆总体架构/memory_architecture_spec.md` | 完整系统架构、数据流、三层设计 |
| `SQLite schema/memory_schema.md` | SQLite Schema 设计、迁移策略、索引设计、约束边界 |
| `记忆抽取(Pre)架构/memory_extraction_protocol.md` | 抽取管道、预过滤、LLM Prompt、JSON Schema |
| `记忆整合/memory_consolidation.md` | 谓词语法、冲突处理、insert/supersede/merge 决策 |
| `记忆检索-激活/memory_retrieval_activation.md` | 检索管道、混合打分、图激活、MMR |
| `记忆删除(用户要求)/memory_forgetting_privacy.md` | 用户主动遗忘、级联删除、隐私保障 |
| `记忆删除(自然淡化)/memory_retention_lifecycle.md` | 自然衰减、TTL、生命周期状态转换 |
| `记忆-情绪预留接口/memory_emotion_coupling.md` | 情绪 ↔ 记忆耦合、mood-safe 检索 |
| `性能测评/memory_eval.md` | 测评框架、测试夹具、golden labels |

---

## 工程不变量

以下架构约束在所有开发阶段均不可破坏：

1. **SQLite 是 source of truth。** 其他所有存储层均为可检索镜像。
2. **Episode 是证据，不是事实。** 不可变锚点。
3. **Fact 是节点，不是边。** 图边（`memory_links`）是独立一层。
4. **每条 Fact 必须有 Provenance。** 通过 `EVIDENCED_BY` 链追溯到 Episode。
5. **三层状态不可合并。** 真实性、可见性、生命周期相互独立。
6. **遗忘优先于检索。** Hidden/forgotten/purged 内容绝不可泄漏到 Prompt 中。
7. **Agent Affect 不是用户记忆。** Agent 自身情绪不能写成用户事实。
8. **Work 不能直接写长期记忆。** 仅接受经审批的 memory candidates。

---

## 许可

Apache License 2.0。详见 [LICENSE](LICENSE)。

---
