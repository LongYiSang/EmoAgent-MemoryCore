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

**读取路径（当前实现）**（存储 → 对话）：

```
新对话开始
  → 轻量查询分析（关键词 / entity alias）
  → SQLite search documents / FTS fallback + 可选 TriviumDB mirror candidate
  → SQLite 权威过滤（visibility / lifecycle / sensitivity / provenance）
  → Go 侧打分、fatigue、context budget
  → facts memory block → System Prompt 注入
```

**读取路径（Phase 5 目标）**：

```
新对话开始
  → 查询分析（实体、关键词、意图提取）
  → 多源 Anchor 召回（entity exact / SQLite FTS5 / TriviumDB dense / pinned / recent / narrative）
  → Weighted RRF 融合为 seed 分布
  → 图激活扩散 + SQLite 权威过滤
  → 可选模型重排 + Go 最终排序 / MMR / context budget
  → 上下文组装 → System Prompt 注入
```

### 系统拓扑

```
┌───────────────────────────────┐         ┌──────────────────────────────────┐
│         Go Service            │  HTTP   │      Python Sidecar（部分实现）     │
│                               │◄───────►│                                  │
│  • 触发检测                    │         │  • 预过滤（低成本 LLM）              │
│  • Episode 同步写入            │         │  • 记忆抽取（主力 LLM）              │
│  • 整合逻辑                    │         │  • Embedding 生成                  │
│  • SQLite（权威记忆库）        │         │  • TriviumDB 检索镜像（Phase 4）      │
│  • FTS5 / fallback 检索        │         │  • 向量 / 图激活检索（Phase 5 规划中） │
│  • 衰减懒计算                  │         │  • 周度整合（LLM）                  │
│  • 上下文组装 → prompt         │         │  • 叙事生成（LLM）                  │
│  • Pin / Forget 通道          │         │                                   │
└───────────────────────────────┘         └──────────────────────────────────┘
```

> **当前实现以 SQLite 为权威库。** SQLite is the authoritative memory store. TriviumDB is a rebuildable retrieval mirror / activation index foundation. No retrieval mirror is authoritative.

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
| `source_redact` | "这段对话原文不要保留" | 隐私例外：Episode 锚点 / tombstone 保留，原文可占位脱敏；仅有脱敏证据的派生事实可保留，但普通检索不返回 |
| `purge` | "彻底删除此事" | 全链路级联清理：事实 + 来源 + 派生 + 镜像 + 搜索 |

---

## 当前进度

**已完成**

- [x] Phase 1 基础层：Go module、SQLite migrations/schema、基础仓储、memoryctl init-db、可选 FTS5。
- [x] Phase 1.5 Core Runtime：Public API facade、deterministic consolidation、SQLite retrieval MVP、forget baseline、eval fixtures、search/privacy hardening。
- [x] Phase 2A Operational CLI：start/end session、append-episode、ensure-entity/add-alias、consolidate-fact、retrieve、forget、rebuild-search、list-facts、get-node、smoke demo。
- [x] Phase 2B Extraction Adapter + Dry-run Pipeline：extract-request / validate / dry-run / apply，strict JSON protocol，Go gate，accepted facts 通过 ConsolidateCandidate 写入；不接真实 LLM。
- [x] Phase 2C 真实抽取运行时：public `ExtractionLLM` 注入接口、mock / OpenAI-compatible standalone provider、prefilter、one-shot repair、extract-run / extract-batch、sanitized extraction_runs audit；runtime implemented, hardening follow-up applied。

**后续 RoadMap**

- [x] Phase 3A Privacy / Purge MVP：exact fact / episode purge、search/FTS cleanup、safe deletion audit、purged-only evidence retrieval block。
- [x] Phase 3B Retention Lifecycle MVP：manual `RunRetention` / `memoryctl retention-run`、`valid_to` expiry、invalidated + archived state transition、search tier sync、historical retrieval gate。
- [x] Phase 3C Retention 后续：SQLite-only deep archive transition、retention job runner、compression storage contract、narrative / insight provenance integration。
- [x] Phase 4 TriviumDB Retrieval Mirror：sidecar adapter / sync worker / mirror rebuild / upsert-delete node-edge / UseMirror diagnostics；SQLite 权威过滤保持最后防线。运维说明见 `docs/operations/phase4_retrieval_mirror.md`。
- [ ] Phase 5 高级 Retrieval Activation：Phase 4 mirror 之后的安全检索激活层；SQLite 仍是权威库，TriviumDB 只产出候选。MVP 只增强读取与上下文组装，不新增 Work / 环境经验主存储，不引入外部 memory framework 依赖。边界说明见 `docs/Phase5参考/phase5_mvp_scope_and_anti_overdesign.md`。
  - [ ] Phase 5A Retrieval Contract / QueryAnalyzer：扩展 `RetrievalRequest` / `QueryAnalysis`，识别 entity、time、causal、historical、provenance、sensitivity、debug 等检索控制信号；补充轻量 `memory_domain`、`memory_ability`、`evidence_need`，区分关系记忆、用户画像、Work / 环境经验、workflow / gotcha / premise check 等查询。这些字段只作为检索路由和 anchor 开关。参考设计见 `docs/Phase5参考/phase5a_query_contract_memory_experience.md`。
  - [ ] Phase 5B Hybrid Anchor / Weighted RRF：各 anchor source 独立产生 ranked hits，保留 source / rank / raw_score / debug_reason；通过 Weighted RRF 融合 entity exact、SQLite FTS / sparse、Trivium dense、pinned / core、recent important、narrative / insight 等异构来源，输出 `fused_anchor_score`、`seed_energy`、`source_breakdown`；不直接混加不同 source 的 raw score。参考设计见 `docs/Phase5参考/phase5b_rrf_anchor_fusion.md`。
  - [ ] Phase 5C Graph Activation Sidecar：在 Python sidecar 中消费 Phase 5B 的 seed 分布，实现 HippoRAG-style seeded PPR / Spreading Activation、Hub suppression、edge weights，返回 candidate ids、score breakdown、activation paths；debug 可透传 `source_breakdown`，但算法核心不重新解释各 anchor source，也不裁决是否进入 Prompt。
  - [ ] Phase 5D Go Authority Ranking + MMR：先交付 Go 侧 baseline final score、lifecycle multiplier、fatigue、MMR、多样性与 context budget 控制；sidecar reranker 仅作为通过 SQLite hard filters 后的可选增强，只返回 `rerank_score` / debug 信息，不能成为最终裁决器。DPP / ScalDPP 等集合选择算法不进入 MVP。参考设计见 `docs/Phase5参考/phase5d_authority_ranking_reranker_mmr.md`。
  - [ ] Phase 5E Context Reconstruction：基于 `memory_links` 按 query mode 重构 causal / temporal / provenance / supportive context blocks；work-experience block 仅在已有独立经验池、narrative / insight 或明确证据来源时启用。输出薄 Memory Context，并显式标注 current / historical / superseded 与证据来源。参考设计见 `docs/Phase5参考/phase5e_context_reconstruction_eval_notes.md`。
  - [ ] Phase 5F Eval / Nightly Regression：扩展 fixture / eval，覆盖 forbidden recall = 0、selected_recall@8、MMR 去重、PPR 防漂移、stale mirror purge 不泄漏，并增加 RRF / activation ablation、selected chain correctness、premise check、workflow / gotcha 类环境经验检索回归；SQLite-only deterministic eval 先进入 CI，真实 mirror / 大规模延迟评估放 nightly。参考设计见 `docs/Phase5参考/phase5e_context_reconstruction_eval_notes.md`。

---

## 快速开始

### 环境要求

- Go 1.26 或更高版本

### 初始化本地数据库

```bash
go run ./cmd/memoryctl init-db --db ./data/memory.db
```

该命令创建包含全部基础表、索引和种子谓词语法的 SQLite 数据库。

### Public API 使用示例

```go
package main

import (
	"context"
	"path/filepath"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func main() {
	ctx := context.Background()
	svc, err := memorycore.Open(ctx, memorycore.Options{
		DBPath:      filepath.Join("data", "memory.db"),
		AutoMigrate: true,
		EnableFTS:   true,
	})
	if err != nil {
		panic(err)
	}
	defer svc.Close()

	session, err := svc.StartSession(ctx, memorycore.StartSessionRequest{})
	if err != nil {
		panic(err)
	}
	_, err = svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		SessionID: session.ID,
		Content:   "我喜欢咖啡。",
	})
	if err != nil {
		panic(err)
	}
}
```

### 运行测试

```bash
go test ./...
```

### Phase 4 侧车烟雾测试

```bash
cd sidecar
uv run python -m memorycore_sidecar.server --adapter fake --host 127.0.0.1 --port 8765
```

```bash
go run ./cmd/memoryctl mirror-sync-run --db ./data/memory.db --fake-adapter --limit 100
go run ./cmd/memoryctl mirror-rebuild --db ./data/memory.db --sidecar-url http://127.0.0.1:8765
go run ./cmd/memoryctl retrieve --db ./data/memory.db --query "coffee preference" --use-mirror --sidecar-url http://127.0.0.1:8765
```

---

## 项目结构

```
EmoAgent-MemoryCore/
├── cmd/
│   └── memoryctl/             # CLI 入口，数据库管理与运维
├── internal/
│   ├── core/                  # 领域类型：Episode、Fact、Entity、Link 等
│   ├── memory/
│   │   └── eval/              # YAML fixture 回归测试框架
│   └── store/
│       └── sqlite/            # SQLite 驱动、迁移、仓储层
├── pkg/
│   └── memorycore/            # 外部项目使用的 public API facade
├── migrations/                # 嵌入式 SQL 迁移文件
│   ├── 0001_foundation.sql    # Personas、sessions、episodes、entities、facts
│   ├── 0002_graph_policy.sql  # Predicate schemas、memory links、同步队列
│   ├── 0003_affect_audit.sql  # 情绪状态、Agent Affect 占位、删除审计
│   └── 0004_search_fallback.sql  # SQLite FTS5 降级搜索
├── testdata/
│   └── memory_eval/           # consolidation / retrieval / forgetting fixtures
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

Episode 是对话中不可变的、仅追加的原始事件记录，是所有上层推理的 ground truth 锚点。Episode 永远不被删除；`source_redact` 是隐私例外：Episode 锚点与 tombstone 保留，原文内容可以替换为占位脱敏内容。只依赖已脱敏证据的派生事实可以保留用于审计 / 后续治理，但普通检索不得返回。

### Fact 是节点，不是边

Fact 是独立的知识节点，具有 `主体 → 谓词 → 客体` 结构，携带完整元数据：Provenance（来自哪些 Episode）、时间线（在现实世界中何时成立）、置信度、重要性、情感效价，以及三层状态。

### Predicate Schema 治理冲突处理

事实之间如何相互作用由 `predicate_schemas` 决定，而非简单的 `UPSERT(subject, predicate, object)`。例如：

- `lives_in`（单一基数，替代策略）：新居住地替代旧居住地。
- `likes`（多重基数，共存策略）：多个喜欢对象共存。
- `has_boundary`（多重基数，合并策略）：边界表达合并为更完整的边界事实。
- `is_busy_with`（时态性，过期策略）：短期上下文自然过期。

### SQLite 是 authoritative memory store

SQLite 持有所有事实、状态、策略、删除审计、双时间线和图关系的**权威状态**。TriviumDB 是可重建的 retrieval mirror / activation index foundation，可随时从 SQLite 清空并重建；任何检索镜像都不是权威事实来源。

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

Phase 5 的 MVP 边界与参考设计位于 `docs/Phase5参考/`；其中 `phase5_mvp_scope_and_anti_overdesign.md` 用于约束“只增强读取激活，不扩展长期记忆主存储”的实现范围。

---

## 工程不变量

以下架构约束在所有开发阶段均不可破坏：

1. **SQLite 是 authoritative memory store。** TriviumDB 是可重建的 retrieval mirror / activation index foundation；任何检索镜像都不是权威事实来源。
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
