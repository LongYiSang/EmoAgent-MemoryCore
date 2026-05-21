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

**读取路径（当前实现，Phase 5 读取增强已接入）**（存储 → 对话）：

```
新对话开始
  → QueryAnalysis（Go rule baseline + 可选 sidecar semantic analyzer，先于 mirror candidates）
  → /retrieval/candidates v0.2（raw query + semantic rewrites + capped semantic anchors，sidecar 可选）
  → 多源 Anchor 召回（SQLite search / FTS fallback / entity exact / pinned / recent / narrative-insight + 可选 TriviumDB mirror candidates）
  → Weighted RRF 融合为 seed 分布
  → 可选 Graph Activation Sidecar（UseMirror 且 persona mirror ready 时）
  → SQLite 权威过滤（visibility / lifecycle / sensitivity / provenance / linked entities）
  → Go 侧 final score、fatigue、MMR、context budget
  → 可选 Safe Sidecar Reranker boost（只接收 SQLite-authorized safe summary 和 bounded query surface；不接收 rewrites / anchors，失败时退化）
  → context reconstruction blocks（facts / relevant_causal_memory / historical_transition_memory / provenance_memory / supportive_memory / experience_context）→ System Prompt 注入
```

**Phase 5 可选 / 退化路径**：

- `UseMirror=false`、sidecar 不可用、persona mirror 未 ready、sidecar degraded 或候选 unmapped/stale 时，读取链路退回 SQLite search / anchor / Go ranking。
- Semantic query analysis 默认关闭：`query_analysis.provider=none`、`query_analysis.mode=rule_only`。启用后也只产生 request-local routing hints；失败、超时、degraded 或非法响应时退回 rule analysis，并继续用 raw query 跑 mirror dense。
- `/retrieval/candidates` 当前协议为 breaking v0.2：raw query 永远参与，semantic rewrites / anchors 受数量、权重、generic text 和 generated weight cap 约束；sidecar 使用 Weighted Max + bounded RRF support 合并多路 dense 结果。
- TriviumDB mirror candidates、Graph Activation、semantic rewrites 和 sidecar rerank 都只是候选或排序信号；SQLite authority filter 仍是 Prompt 注入前的最终安全边界。
- Sidecar retrieval 是可选增强路径。Mirror candidates、Graph Activation 和 rerank 受 Go 侧短 stage timeout、总 sidecar budget、persona/stage circuit breaker 约束；timeout 或 degraded 状态会回退到 SQLite authority retrieval，并保留在检索 diagnostics 中。
- Reranker 默认 `provider=none`，DashScope provider 需要显式配置环境变量；缺 key、超时、HTTP 错误或 malformed response 时不阻断检索，只是不提供 rerank boost。
- 真实 mirror 质量、夜间大规模延迟评估仍属于可选增强；SQLite-only deterministic eval 是当前基线。
- 写入侧预过滤、真实 LLM 抽取、周/月叙事生成不属于 Phase 5 已实现读取链路。

### 系统拓扑

```
┌───────────────────────────────┐         ┌──────────────────────────────────┐
│         Go Service            │  HTTP   │      Python Sidecar（可选辅助）      │
│                               │◄───────►│                                  │
│  • 触发检测                    │         │  • Embedding 生成                  │
│  • Episode 同步写入            │         │  • TriviumDB 检索镜像（Phase 4）      │
│  • 整合逻辑                    │         │  • Graph Activation（Phase 5C，可选） │
│  • SQLite（权威记忆库）        │         │  • Semantic Query Analyzer（可选）     │
│  • QueryAnalysis merge/clamp  │         │  • Candidates v0.2 multi-query dense │
│  • FTS5 / fallback 检索        │         │  • Safe Rerank（Phase 5G，可选）      │
│  • 衰减懒计算                  │         │  • DashScope provider（默认关闭）     │
│  • Go final ranking / MMR      │         │  • degraded fallback               │
│  • 上下文组装 → prompt         │         │  • 只返回候选 / signal               │
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

> 本节中的 `docs/Phase5参考/`、`docs/architecture/`、`docs/refactor/` 和 `docs/TODO/` 路径是本仓库的本地参考文档面；这些目录按当前 `.gitignore` 默认不进入普通提交。若需要把其中某份文档纳入历史，必须有意使用 force-add / 调整 ignore 规则，而不是把这些链接理解为已随包发布的 shipped docs。

- [x] Phase 3A Privacy / Purge MVP：exact fact / episode purge、search/FTS cleanup、safe deletion audit、purged-only evidence retrieval block。
- [x] Phase 3B Retention Lifecycle MVP：manual `RunRetention` / `memoryctl retention-run`、`valid_to` expiry、invalidated + archived state transition、search tier sync、historical retrieval gate。
- [x] Phase 3C Retention 后续：SQLite-only deep archive transition、retention job runner、compression storage contract、narrative / insight provenance integration。
- [x] Phase 4 TriviumDB Retrieval Mirror：sidecar adapter / sync worker / mirror rebuild / upsert-delete node-edge / UseMirror diagnostics；SQLite 权威过滤保持最后防线。当前运维说明见 `sidecar/README.md`。
- [x] Phase 5 高级 Retrieval Activation：Phase 4 mirror 之后的安全检索激活层；SQLite 仍是权威库，TriviumDB / sidecar 只产出候选、activation 或可选 rerank signal。当前 5A-5G-B 已落地；MVP 只增强读取与上下文组装，不新增 Work / 环境经验主存储，不引入外部 memory framework 依赖。边界说明见 `docs/Phase5参考/phase5_mvp_scope_and_anti_overdesign.md`。
  - [x] Phase 5A Retrieval Contract / QueryAnalyzer：扩展 `RetrievalRequest` / `QueryAnalysis`，识别 entity、time、causal、historical、provenance、sensitivity、debug 等检索控制信号；补充轻量 `memory_domain`、`memory_ability`、`evidence_need`，区分关系记忆、用户画像、Work / 环境经验、workflow / gotcha / premise check / relationship arc 等查询。这些字段只作为检索路由和 anchor 开关。当前链路先完成 QueryAnalysis merge/clamp，再调用 mirror candidates v0.2。参考设计见 `docs/Phase5参考/phase5a_query_contract_memory_experience.md`。
  - [x] Phase 5B Hybrid Anchor / Weighted RRF：各 anchor source 独立产生 ranked hits，保留 source / rank / raw_score / debug_reason；通过 Weighted RRF 融合 entity exact、SQLite FTS / sparse、Trivium dense、pinned / core、recent important、narrative / insight 等异构来源，输出 `fused_anchor_score`、`seed_energy`、`source_breakdown`；不直接混加不同 source 的 raw score。参考设计见 `docs/Phase5参考/phase5b_rrf_anchor_fusion.md`。
  - [x] Phase 5C Graph Activation Sidecar：在 Python sidecar 中消费 Phase 5B 的 seed 分布，实现 HippoRAG-style seeded PPR / Spreading Activation、Hub suppression、edge weights，返回 candidate ids、score breakdown、activation paths；debug 可透传 `source_breakdown`，但算法核心不重新解释各 anchor source，也不裁决是否进入 Prompt。
  - [x] Phase 5D Go Authority Ranking + MMR：交付 Go 侧 baseline final score、lifecycle multiplier、fatigue、MMR、多样性与 context budget 控制；sidecar reranker 仅作为通过 SQLite hard filters 后的可选增强，只返回 `rerank_score` / debug 信息，不能成为最终裁决器。DPP / ScalDPP 等集合选择算法不进入 MVP。参考设计见 `docs/Phase5参考/phase5d_authority_ranking_reranker_mmr.md`。
  - [x] Phase 5E Context Reconstruction：基于 5D selected facts 和 1-hop safe `memory_links` 按 query mode 重构 `relevant_causal_memory` / `historical_transition_memory` / `provenance_memory` / `premise_check_memory` / `relationship_arc_memory` / `supportive_memory` / `experience_context` blocks；输出薄 Memory Context，显式标注 current / historical / superseded，并只暴露安全 source refs，不输出 episode 原文。参考设计见 `docs/Phase5参考/phase5e_context_reconstruction_eval_notes.md`。
  - [x] Phase 5F Eval / Regression Baseline：已扩展 fixture / eval，覆盖 forbidden recall = 0、selected_recall@8、context_precision、MMR 去重、graph activation fallback、selected chain correctness、premise check 与 deterministic ablation；SQLite-only deterministic eval 已可作为基线，真实 mirror 质量、夜间大规模延迟评估保留为后续增强。参考设计见 `docs/Phase5参考/phase5e_context_reconstruction_eval_notes.md`。
  - [x] Phase 5G-A Optional Safe Sidecar Reranker MVP：已落地协议、安全接入、fake / deterministic reranker 与 eval；reranker 仅接收 SQLite authority filter 后的 safe summary，不接收未授权候选或 episode 原文，只返回 `rerank_score` / `debug_reason` / fallback 信息。Go 仍负责 final score、MMR、fatigue、lifecycle、context budget 与最终 Prompt 注入；5G-A 不需要真实 API key，真实 provider 接入留作后续阶段。参考设计见 `docs/Phase5参考/phase5g_safe_sidecar_reranker.md`。
  - [x] Phase 5G-B DashScope qwen3-vl-rerank Provider：可选接入真实 DashScope rerank provider，默认不启用且不要求 API key；API key 仅通过环境变量注入，CI 仍使用 fake / mocked deterministic tests。真实 smoke 需手动设置 `MEMORYCORE_RERANK_SMOKE=1` 与 `DASHSCOPE_API_KEY`；provider 失败、超时、未配置或 degraded 时完全 fallback，且只发送 SQLite authority filter 后的 safe summary。

**Phase 5 当前对外口径**

已实现的是读取增强链路：QueryAnalysis-before-mirror、Hybrid Anchor / Weighted RRF、`/retrieval/candidates` v0.2 multi-query dense、可选 Graph Activation Sidecar、Go authority ranking + MMR、Context Reconstruction、deterministic eval、Safe Sidecar Reranker 协议与可选 DashScope provider。默认和安全退化路径仍是 SQLite-only retrieval；任何 semantic analysis、sidecar、TriviumDB、activation 或 rerank 结果都必须经过 SQLite 权威过滤，并且只能影响候选或排序信号，不能直接决定 Prompt 注入。ADR 见 `docs/refactor/2026-05-19-semantic-query-analyzer-v0.2.md`。

---

## 快速开始

### 环境要求

- Go 1.26 或更高版本

### 初始化本地数据库

```bash
go run ./cmd/memoryctl init-db --db ./data/memory.db
```

该命令创建包含全部基础表、索引和种子谓词语法的 SQLite 数据库。

### 配置契约

MemoryCore 提供可嵌入的 v0.1 配置契约，示例见 `examples/config/memorycore.yaml`。部署前可用 CLI 校验配置或导出字段参考：

```bash
go run ./cmd/memoryctl validate-config --config examples/config/memorycore.yaml
go run ./cmd/memoryctl config-docs --format markdown
```

`enabled` 是嵌入方开关；`memoryctl --config` 执行显式命令时不会因为 `enabled: false` 被拦截。显式 CLI flags 会覆盖配置文件中的对应字段，并在 stderr 输出 warning。

`query_analysis` 默认 `provider: none` / `mode: rule_only`，即只使用 Go 规则分析。启用 sidecar provider 时，MemoryCore 会在 mirror candidate retrieval 之前调用 `/retrieval/query-analysis`，把通过 Go 验证、合并和 policy clamp 的 `QueryAnalysis` 传给 `/retrieval/candidates` v0.2。API key 只通过环境变量读取；semantic timeout、budget exhausted、degraded 或非法响应都会回退到 rule-only，不阻断 retrieval。

Phase 9 后的灰度模式是 `legacy_only`、`shadow_adaptive`、`adaptive_safe`、`adaptive_full`。旧的 `semantic_on_low_confidence` / `min_confidence_to_override` 仍可加载用于兼容，但新配置应把 adaptive 路由阈值放到 `query_analysis.thresholds.*`，把调用上限放到 `query_analysis.budget.*`，并用 `query_analysis.diagnostics.*` 控制 score breakdown / reason codes 的输出采样。不要在同一份配置里混用旧的 flat adaptive 阈值和新的 `thresholds`，加载器会报迁移错误以避免静默优先级。

### 检索质量评测

`cmd/memory-eval` 是人工观察检索质量的入口，默认运行 `testdata/memory_eval/quality/retrieval/` 下的 no-stub fixture：

```powershell
scripts\memory_eval_quality.cmd -Mode full
go run ./cmd/memory-eval --suite retrieval --mode full
```

质量评测 fixture 禁止使用 `mirror_stub`、`graph_activation_stub`、`rerank_stub`。需要 stub 隔离下游行为的用例放在 `testdata/memory_eval/controlled/`，由 `go test ./internal/memory/eval` 作为受控回归执行。

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
│   ├── memoryctl/             # CLI 入口，数据库管理与运维
│   └── memory-eval/           # 人工检索质量评测入口
├── internal/
│   ├── app/
│   │   └── memorycore/            # Application service 层：Service 实现、DTO、use case 编排
│   ├── core/                  # 领域类型：Episode、Fact、Entity、Link 等
│   ├── memory/
│   │   └── eval/              # YAML fixture 回归框架与质量报告格式化
│   └── store/
│       └── sqlite/            # SQLite 驱动、迁移、仓储层
├── pkg/
│   └── memorycore/            # 外部项目使用的 public API facade（alias / forwarding）
├── migrations/                # 嵌入式 SQL 迁移文件
│   ├── 0001_foundation.sql    # Personas、sessions、episodes、entities、facts
│   ├── 0002_graph_policy.sql  # Predicate schemas、memory links、同步队列
│   ├── 0003_affect_audit.sql  # 情绪状态、Agent Affect 占位、删除审计
│   └── 0004_search_fallback.sql  # SQLite FTS5 降级搜索
├── testdata/
│   └── memory_eval/
│       ├── controlled/        # 允许 stub 的受控回归 fixture
│       ├── quality/retrieval/ # 禁止 stub 的人工检索质量评测 fixture
│       └── ...                # consolidation / retrieval / forgetting / retention fixtures
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

本地架构参考文档位于 `docs/architecture/`。该目录当前被 `.gitignore` 忽略，默认作为本机参考面；如需把其中某份文档纳入仓库历史，需要有意 force-add / 调整 ignore 规则。

| 文档 | 内容范围 |
|------|---------|
| `记忆总体架构/memory_architecture_spec.md` | 完整系统架构、数据流、三层设计 |
| `SQLite schema/memory_schema.md` | SQLite Schema 设计、迁移策略、索引设计、约束边界 |
| `记忆抽取(Pre)架构/memory_extraction_protocol.md` | 抽取管道、预过滤、LLM Prompt、JSON Schema |
| `记忆整合/memory_consolidation.md` | 谓词语法、冲突处理、insert/supersede/merge 决策 |
| `记忆检索-激活/memory_retrieval_activation.md` | 检索管道、混合打分、图激活、MMR |
| `记忆检索-激活/semantic_query_analyzer_architecture.md` | QueryAnalysis-before-mirror、sidecar semantic analyzer、candidates v0.2 |
| `记忆删除(用户要求)/memory_forgetting_privacy.md` | 用户主动遗忘、级联删除、隐私保障 |
| `记忆删除(自然淡化)/memory_retention_lifecycle.md` | 自然衰减、TTL、生命周期状态转换 |
| `记忆-情绪预留接口/memory_emotion_coupling.md` | 情绪 ↔ 记忆耦合、mood-safe 检索 |
| `性能测评/memory_eval.md` | 测评框架、测试夹具、golden labels |

Phase 5 的 MVP 边界与参考设计位于本地忽略目录 `docs/Phase5参考/`；其中 `phase5_mvp_scope_and_anti_overdesign.md` 用于约束“只增强读取激活，不扩展长期记忆主存储”的实现范围。QueryAnalysis-before-mirror 与 candidates v0.2 的 ADR 位于本地忽略目录 `docs/refactor/2026-05-19-semantic-query-analyzer-v0.2.md`。这些参考面默认不进入普通提交。

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
