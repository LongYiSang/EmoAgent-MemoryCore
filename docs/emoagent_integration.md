# EmoAgent Integration Guide

本文档描述当前代码实际支持的外部接入面，目标读者是 EmoAgent 主仓库的接入层。MemoryCore 当前是一个 Go module + SQLite 权威库 + 可选 Python sidecar，而不是独立 HTTP 服务。

当前代码基准：2026-05-24。

## 接入结论

主仓库应优先嵌入 Go facade：

```go
import "github.com/longyisang/emoagent-memorycore/pkg/memorycore"
```

公共入口是 `memorycore.Open(ctx, memorycore.Options)`。不要 import `internal/...`。Sidecar 只用于可选检索增强，不是权威记忆源，也不是主仓库写入事实的入口。

最小运行链路：

1. 启动时打开 `memorycore.Service`。
2. 对话开始时调用 `StartSession`。
3. 每轮对话用 `AppendEpisode` 追加用户/助手事件。
4. 需要注入长期记忆前调用 `Retrieve`，把返回的 `MemoryContext` 转成主仓库 prompt 片段。
5. 会话结束时调用 `EndSession`。
6. 需要把 episode 固化为可检索长期事实时，调用 `ConsolidateCandidate` 或使用 `pkg/memorycore/extractionruntime`。
7. 用户要求删除/遗忘时调用 `Forget`。

## 模块依赖

如果 EmoAgent 和 MemoryCore 是相邻本地仓库，主仓库 `go.mod` 可临时使用本地 replace：

```go
require github.com/longyisang/emoagent-memorycore v0.0.0

replace github.com/longyisang/emoagent-memorycore => ../EmoAgent-MemoryCore
```

生产发布时再改成真实 tag 或固定 commit。

## 初始化方式

### 方式一：直接 Options

适合先接入 SQLite-only 最小链路。

```go
svc, err := memorycore.Open(ctx, memorycore.Options{
	DBPath:      "./data/memory.db",
	PersonaID:   "default",
	AutoMigrate: true,
	EnableFTS:   true,
})
if err != nil {
	return err
}
defer svc.Close()
```

`DBPath` 必填。`PersonaID` 为空时默认为 `default`。`AutoMigrate=true` 会自动迁移数据库；只有此时 `EnableFTS=true` 才会安装可选 FTS 迁移。

### 方式二：配置文件打开

适合主仓库保留 MemoryCore 独立配置文件，并允许运行时覆盖。

```go
package memoryhost

import (
	"context"

	memconfig "github.com/longyisang/emoagent-memorycore/config"
)

func OpenMemory(ctx context.Context, configPath string) (*memconfig.ConfiguredService, error) {
	return memconfig.Open(ctx, memconfig.ConfigOpenOptions{
		ConfigPath: configPath,
		Runtime: memconfig.RuntimeValidationOptions{
			CheckEnv: true,
		},
	})
}
```

`ConfiguredService` 内嵌 `memorycore.Service`，并额外返回：

- `Config`：生效后的配置。
- `RetrievalPolicy`：每次 `Retrieve` 可直接使用的默认检索策略。
- `RetentionJobs`：当前配置启用的保留任务名。
- `MirrorSyncLimit`：镜像同步默认批量大小。

当前配置加载链：

```text
DefaultConfig()
  -> LoadYAML / LoadJSON
  -> ProviderRegistry
  -> ConfigOverrides
  -> ValidateRuntime
  -> Config.Runtime() / ToOptions()
  -> memorycore.Open()
```

注意：`RetrievalPolicy` 不在 `memorycore.Options` 中，必须在每次 `Retrieve` 请求里传入。

## 对话生命周期示例

```go
package memoryhost

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

func RunTurn(ctx context.Context, svc memorycore.Service, sessionID string, userText string) (string, error) {
	mc, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
		SessionID: &sessionID,
		QueryText: userText,
		Policy: memorycore.RetrievalPolicy{
			SensitivityPermission: memorycore.SensitivityNormal,
			FinalMemoryCount:      8,
			ContextBudgetTokens:   1200,
			UseFTS:                true,
			UseMirror:             false,
		},
	})
	if err != nil {
		return "", err
	}

	return FormatMemoryContext(mc), nil
}

func StartConversation(ctx context.Context, svc memorycore.Service) (*memorycore.Session, error) {
	return svc.StartSession(ctx, memorycore.StartSessionRequest{
		Channel:   memorycore.ChannelAPI,
		StartedAt: time.Now(),
	})
}

func AppendChatTurn(ctx context.Context, svc memorycore.Service, sessionID string, userText string, assistantText string) error {
	if _, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		SessionID:  sessionID,
		Role:       memorycore.RoleUser,
		Content:    userText,
		SourceType: memorycore.SourceTypeChat,
		OccurredAt: time.Now(),
	}); err != nil {
		return err
	}

	if assistantText == "" {
		return nil
	}
	_, err := svc.AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
		SessionID:  sessionID,
		Role:       memorycore.RoleAssistant,
		Content:    assistantText,
		SourceType: memorycore.SourceTypeChat,
		OccurredAt: time.Now(),
	})
	return err
}

func EndConversation(ctx context.Context, svc memorycore.Service, sessionID string, summary string) error {
	_, err := svc.EndSession(ctx, memorycore.EndSessionRequest{
		SessionID: sessionID,
		Summary:   &summary,
		EndedAt:   time.Now(),
	})
	return err
}

func FormatMemoryContext(mc *memorycore.MemoryContext) string {
	if mc == nil || len(mc.Blocks) == 0 {
		return ""
	}
	var b strings.Builder
	for _, block := range mc.Blocks {
		if len(block.Items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "[%s]\n", block.BlockType)
		for _, item := range block.Items {
			fmt.Fprintf(&b, "- %s", item.Summary)
			if item.UsageGuidance != "" {
				fmt.Fprintf(&b, " (%s)", item.UsageGuidance)
			}
			if item.HistoricalStatus != "" && item.HistoricalStatus != memorycore.MemoryHistoricalStatusCurrent {
				fmt.Fprintf(&b, " [%s]", item.HistoricalStatus)
			}
			b.WriteByte('\n')
		}
	}
	return strings.TrimSpace(b.String())
}
```

`AppendEpisode` 只记录原始事件。它不会自动生成长期事实。检索可返回的事实需要通过抽取/整合链路写入。

## 可写入能力

### Episode

`AppendEpisode` 是对话事件流入口，必填：

- `SessionID`
- `Content`

常用可选字段：

- `Role`：`user`、`assistant`、`system`、`tool_summary`、`work_report`
- `SourceType`：`chat`、`work_candidate`、`plugin`、`system`、`imported`
- `VisibilityStatus`：默认 `visible`
- `SensitivityLevel`：默认 `normal`
- `Searchable`：默认 `true`

非 visible episode 会在存储层变成不可搜索。

### Entity 和 Alias

主仓库可在已知用户、项目、人名、地点时先建立实体：

```go
entity, err := svc.EnsureEntity(ctx, memorycore.EnsureEntityRequest{
	CanonicalName: "用户",
	EntityType:    memorycore.EntityTypeUser,
})
if err != nil {
	return err
}

_, err = svc.AddEntityAlias(ctx, memorycore.AddEntityAliasRequest{
	EntityID:   entity.ID,
	Alias:      "我",
	AliasType:  memorycore.AliasTypeSurface,
	Confidence: 1,
})
```

### Fact

如果主仓库已经有经过审批的 memory candidate，可直接走整合入口：

```go
object := "手冲咖啡"
result, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{
	SessionID: &sessionID,
	Trigger:   memorycore.ConsolidationTriggerManual,
	Candidate: memorycore.ManualFactCandidate{
		SubjectEntityID:  userEntityID,
		Predicate:        "likes",
		ObjectLiteral:    &object,
		ContentSummary:   "用户喜欢手冲咖啡。",
		FactType:         memorycore.FactTypeStablePreference,
		Confidence:       memorycore.ConfidenceExplicit,
		ConfidenceScore:  0.9,
		Importance:       0.7,
		Sensitivity:      memorycore.SensitivityNormal,
		SourceEpisodeIDs: []string{episodeID},
	},
	Policy: memorycore.ConsolidationPolicy{
		Approved: true,
	},
})
if err != nil {
	return err
}
_ = result
```

常规约束：

- 普通长期事实应有 `SourceEpisodeIDs`，保证可追溯。
- `ManualPin` 场景如果没有来源，需要显式 `AllowManualPinWithoutSource`。
- Work 不能直接写长期记忆，应先形成 `work_candidate` 并经过 Emotion 侧审批。

## 抽取运行时

如果主仓库想从 episode 窗口自动抽取事实，使用 public 子包：

```go
import "github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime"
```

`extractionruntime.Runner` 需要宿主提供：

- `*sql.DB`：访问同一个 MemoryCore SQLite 数据库。
- `memorycore.Service`：用于把 accepted facts 写入整合入口。
- `memorycore.ExtractionLLM`：宿主自己的 LLM client，或使用内置 OpenAI-compatible client。
- `AuditStore`：通常使用 `extractionruntime.NewSQLiteAuditStore(sqlDB)`。

示例：

```go
package memoryhost

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/longyisang/emoagent-memorycore/pkg/memorycore"
	"github.com/longyisang/emoagent-memorycore/pkg/memorycore/extractionruntime"
	_ "modernc.org/sqlite"
)

func ExtractSession(ctx context.Context, dbPath string, svc memorycore.Service, sessionID string) error {
	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(1)

	req, err := extractionruntime.BuildRequest(ctx, sqlDB, extractionruntime.BuildRequestOptions{
		PersonaID:      "default",
		SessionID:      &sessionID,
		Trigger:        memorycore.ExtractionTriggerSessionEnd,
		Limit:          50,
		Timezone:       "Asia/Shanghai",
		AllowInference: true,
		MaxFacts:       12,
		MaxLinks:       20,
		Now:            time.Now(),
	})
	if err != nil {
		return err
	}

	llm := extractionruntime.NewOpenAICompatibleLLM(extractionruntime.OpenAICompatibleOptions{
		BaseURL:   "https://api.example.com/compatible-mode",
		APIKeyEnv: "MEMORYCORE_LLM_API_KEY",
		Model:     "memory-extractor",
		Timeout:   60 * time.Second,
		MaxTokens: 4096,
	})

	runner := extractionruntime.NewRunner(extractionruntime.RunnerOptions{
		DB:         sqlDB,
		Service:    svc,
		LLM:        llm,
		AuditStore: extractionruntime.NewSQLiteAuditStore(sqlDB),
	})

	result, err := runner.Run(ctx, memorycore.ExtractionRunRequest{
		Request:       req,
		Mode:          memorycore.ExtractionRunModeApply,
		ProviderID:    "default_llm",
		ProviderKind:  "openai-compatible",
		Model:         "memory-extractor",
		Temperature:   0,
		MaxTokens:     4096,
		Timeout:       60 * time.Second,
		RepairEnabled: true,
		Audit:         memorycore.ExtractionAuditOn,
	})
	if err != nil {
		return err
	}
	switch result.Status {
	case memorycore.ExtractionRunStatusApplied,
		memorycore.ExtractionRunStatusNothingApplied,
		memorycore.ExtractionRunStatusSkipped:
		return nil
	default:
		return fmt.Errorf("memory extraction status=%s code=%s", result.Status, result.SanitizedErrorCode)
	}
}
```

如果接入初期不想在主仓库内跑抽取，可以先用 CLI 的 `extract-run` / `extract-batch` 做离线验证。

## 检索接口

调用：

```go
mc, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{
	SessionID: &sessionID,
	QueryText: "用户最近为什么抗拒早会？",
	Policy: memorycore.RetrievalPolicy{
		SensitivityPermission: memorycore.SensitivityNormal,
		AllowHistorical:       true,
		FinalMemoryCount:      8,
		ContextBudgetTokens:   1200,
		UseFTS:                true,
		UseMirror:             false,
	},
	Context: memorycore.RetrievalAffectContext{
		UserMoodLabel:         "tired",
		RelationshipMoodLabel: "stable",
	},
})
```

返回 `MemoryContext`：

- `Blocks`：可注入 prompt 的分组记忆。
- `DoNotMention`：因疲劳、预算、去重等原因不应提及的节点。
- `TokenEstimate`：估算 token。
- `QueryAnalysis`：规则或 semantic 合并后的查询分析。
- `AnchorFusion`、`Mirror`、`GraphActivation`、`Rerank`、`RetrievalConfidence`：诊断字段，可用于日志和调试，不建议直接暴露给用户。

当前常见 block 类型：

- `facts`
- `relevant_causal_memory`
- `historical_transition_memory`
- `provenance_memory`
- `premise_check_memory`
- `relationship_arc_memory`
- `supportive_memory`
- `experience_context`

安全边界：

- SQLite 仍是 prompt 注入前的权威过滤层。
- hidden、forgotten、purged、不可搜索、超敏权限的内容不得进入 prompt。
- Sidecar、TriviumDB、semantic query analysis、graph activation、rerank 都只能提供候选或排序信号。

## 删除和遗忘

用户主动要求遗忘时，主仓库应调用 `Forget`，不要只在主仓库侧屏蔽文本。

```go
_, err := svc.Forget(ctx, memorycore.ForgetRequest{
	Actor:      memorycore.ForgetActorUser,
	ReasonCode: memorycore.ForgetReasonUserRequested,
	Level:      memorycore.ForgetLevelSoft,
	Target: memorycore.ForgetTarget{
		ScopeMode: memorycore.ForgetScopeExactNode,
		NodeType:  memorycore.ForgetNodeFact,
		NodeID:    factID,
	},
})
```

当前支持范围：

- `soft_forget`：fact。
- `hard_forget`：fact。
- `source_redact`：episode。
- `purge`：fact 或 episode。
- `scope_mode` 当前使用 `exact_node`。

建议主仓库策略：

- 普通“别再提了”：`soft_forget`。
- “忘掉这个偏好/事实”：`hard_forget`。
- “这段原文不要保留”：`source_redact`。
- “彻底删除”：`purge`，主仓库应二次确认。

## 运维接口

### SQLite search 重建

```go
_, err := svc.RebuildSearchDocuments(ctx, memorycore.RebuildSearchDocumentsRequest{
	PersonaID: "default",
})
```

CLI：

```powershell
go run ./cmd/memoryctl rebuild-search --db ./data/memory.db --format json --pretty
```

### Retention

```go
_, err := svc.RunRetentionJobs(ctx, memorycore.RunRetentionJobsRequest{
	PersonaID: "default",
	Jobs: []memorycore.RetentionJobName{
		memorycore.RetentionJobDailyTTLExpiry,
	},
	DryRun: false,
})
```

当前 public job 名只有：

- `daily_ttl_expiry`
- `monthly_deep_archive`

`retention.auto_delete=true` 当前会被配置校验拒绝。

### Mirror

如果启用 Python sidecar / TriviumDB mirror：

```go
svc, err := memorycore.Open(ctx, memorycore.Options{
	DBPath:        "./data/memory.db",
	AutoMigrate:   true,
	EnableFTS:     true,
	MirrorAdapter: memorycore.NewSidecarMirrorAdapter("http://127.0.0.1:8765"),
})
```

重建和增量同步：

```go
_, err = svc.RebuildMirror(ctx, memorycore.RebuildMirrorRequest{PersonaID: "default"})
_, err = svc.RunMirrorSync(ctx, memorycore.RunMirrorSyncRequest{PersonaID: "default", Limit: 100})
```

CLI：

```powershell
go run ./cmd/memoryctl mirror-rebuild --db ./data/memory.db --sidecar-url http://127.0.0.1:8765
go run ./cmd/memoryctl mirror-sync-run --db ./data/memory.db --sidecar-url http://127.0.0.1:8765 --limit 100
go run ./cmd/memoryctl retrieve --db ./data/memory.db --query "coffee preference" --use-mirror --sidecar-url http://127.0.0.1:8765
```

`sidecar.url` 必须是 loopback HTTP URL。远程 sidecar URL 会被校验拒绝。

## Go 配置方案

推荐主仓库保存独立 MemoryCore 配置，例如 `config/memorycore.yaml`：

```yaml
schema_version: memorycore.config.v0.2
enabled: true

core:
  db_path: ./data/memory.db
  persona_id: default
  auto_migrate: true
  enable_fts: true
  timezone: Asia/Shanghai

retrieval:
  use_fts: true
  use_mirror: false
  allow_historical: false
  allow_deep_archive: false
  sensitivity_permission: normal
  final_memory_count: 8
  context_budget_tokens: 1200

sidecar:
  enabled: false
  url: http://127.0.0.1:8765
  adapter: trivium

mirror:
  enabled: false
  sync_limit: 100

pipelines:
  query_analysis:
    enabled: false
    mode: rule_only
    runtime_mode: rule_only
    fallback_mode: rule_only
    timeout_ms: 1500
```

配置校验：

```powershell
go run ./cmd/memoryctl validate-config --config config/memorycore.yaml
go run ./cmd/memoryctl validate-config --config config/memorycore.yaml --check-env
go run ./cmd/memoryctl config-docs --format markdown
```

重要边界：

- Go loader 当前不会展开 `${ENV}` 字符串。
- 密钥不要写入 YAML；只写 `api_key_env`，由宿主进程环境提供实际值。
- `enabled: false` 是宿主嵌入开关；显式运行 `memoryctl --config` 不会因为它被拦截。
- CLI 对 config 的消费仍是命令级部分覆盖，不等于所有命令都完整走 `config.Open`。

## Sidecar 配置方案

Sidecar 是独立 Python 进程，配置文件是 TOML，不是 Go YAML。

启动 fake adapter：

```powershell
cd sidecar
uv run python -m memorycore_sidecar.server --adapter fake --host 127.0.0.1 --port 8765
```

启动 real Trivium adapter：

```powershell
cd sidecar
$env:DASHSCOPE_API_KEY = "<dashscope-api-key>"
uv run python -m memorycore_sidecar.server --adapter trivium --config config.toml --host 127.0.0.1 --port 8765
```

TOML 示例：

```toml
[trivium]
dir = "../data/trivium"
dtype = "f32"
sync_mode = "normal"

[embedding]
provider = "openai-compatible"
base_url = "https://dashscope.aliyuncs.com/compatible-mode/v1"
api_key_env = "DASHSCOPE_API_KEY"
model = "text-embedding-v4"
dimensions = 1024

[query_analysis]
provider = "none"
base_url = "https://dashscope.aliyuncs.com/compatible-mode/v1"
api_key_env = "DASHSCOPE_API_KEY"
model = "qwen-plus"
max_tokens = 768
temperature = 0
response_format = "json_object"

[rerank]
provider = "none"
```

Sidecar HTTP endpoints 当前包括：

- `GET /health`
- `POST /mirror/operation`
- `POST /mirror/clear-namespace`
- `POST /retrieval/query-analysis`
- `POST /retrieval/candidates`
- `POST /retrieval/activate`
- `POST /retrieval/rerank`

这些 endpoints 是 Go 服务的可选辅助协议。主仓库通常不应直接调用它们。

## 启用 Sidecar 检索增强

Go YAML 侧：

```yaml
providers:
  llm:
    - id: sidecar_control
      provider: openai
      protocol: openai_compatible
      api_key_env: MEMORYCORE_LLM_API_KEY
      enabled: true

pipelines:
  query_analysis:
    enabled: true
    mode: sidecar
    runtime_mode: adaptive_safe
    provider_id: sidecar_control
    fallback_mode: rule_only
    timeout_ms: 1500

retrieval:
  use_fts: true
  use_mirror: true

sidecar:
  enabled: true
  url: http://127.0.0.1:8765
  adapter: trivium

mirror:
  enabled: true
  sync_limit: 100
```

当前实现细节：通过 `config.Config` 启用非 `rule_only` query analysis 时，配置校验要求 `pipelines.query_analysis.provider_id` 指向 enabled provider；Go 运行时实际调用 semantic analyzer 走 `sidecar.url`，真实 provider 配置仍在 sidecar TOML。

如果直接构造 `memorycore.Options`，可跳过 Go YAML 的 provider registry：

```go
svc, err := memorycore.Open(ctx, memorycore.Options{
	DBPath:        "./data/memory.db",
	AutoMigrate:   true,
	EnableFTS:     true,
	MirrorAdapter: memorycore.NewSidecarMirrorAdapter("http://127.0.0.1:8765"),
	QueryAnalysis: memorycore.QueryAnalysisOptions{
		Provider:   memorycore.QueryAnalysisProviderSidecar,
		Mode:       memorycore.QueryAnalysisModeAdaptiveSafe,
		SidecarURL: "http://127.0.0.1:8765",
	},
})
```

请求时仍需：

```go
Policy: memorycore.RetrievalPolicy{
	FinalMemoryCount:    8,
	ContextBudgetTokens: 1200,
	UseFTS:              true,
	UseMirror:           true,
}
```

失败、超时、degraded、预算耗尽或非法响应时，Go 检索会回退到 SQLite authority retrieval，并在 diagnostics 里保留状态。

## CLI 对接与调试

CLI 适合初始化、烟雾测试、人工排查和离线抽取，不建议作为主仓库热路径。

常用命令：

```powershell
go run ./cmd/memoryctl init-db --db ./data/memory.db --enable-fts
go run ./cmd/memoryctl start-session --db ./data/memory.db --format json --pretty
go run ./cmd/memoryctl append-episode --db ./data/memory.db --session <session-id> --role user --content "我喜欢咖啡。"
go run ./cmd/memoryctl retrieve --db ./data/memory.db --query "咖啡" --use-fts --format json --pretty
go run ./cmd/memoryctl forget --db ./data/memory.db --level soft_forget --node-type fact --node-id <fact-id>
go run ./cmd/memoryctl extract-run --db ./data/memory.db --session <session-id> --mode dry-run --provider mock --format json --pretty
```

`extract-run` / `extract-batch` 当前 provider、base URL、API key env 是独立 CLI flags，不走 `config.Config` 的 provider registry。

## 不要依赖的面

- 不要 import `internal/app/memorycore`、`internal/store/sqlite`、`internal/mirror`。
- 不要把 Sidecar 当成事实来源；Sidecar 只返回候选、activation、semantic hint 或 rerank signal。
- 不要假设 `AppendEpisode` 后马上能被长期检索命中；需要抽取/整合事实。
- 不要把 raw provider response、prompt、chain-of-thought 或 conversation window 暴露给用户。
- 不要把明文 API key 放入 YAML/TOML。
- 不要开启 `retention.auto_delete`；当前配置校验会拒绝。
- 不要让 Work 直接写长期记忆；Work 只能产出候选，最终由 Emotion/主仓库审批后写入。

## 接入检查清单

- [ ] 主仓库只依赖 `pkg/memorycore` 和可选 `pkg/memorycore/extractionruntime`。
- [ ] 启动时能打开 SQLite，`AutoMigrate=true` 或运维已跑过 `init-db`。
- [ ] 每个对话有 `StartSession` / `EndSession`。
- [ ] 每轮原始对话写入 `AppendEpisode`。
- [ ] prompt 注入前调用 `Retrieve`，并只使用 `MemoryContext.Blocks` 转成可见上下文。
- [ ] 用户遗忘请求调用 `Forget`。
- [ ] 自动抽取链路先用 `dry-run` 验证，再切 `apply`。
- [ ] Sidecar 未启动时主链路仍可 SQLite-only 工作。
- [ ] 配置校验通过 `memoryctl validate-config`。
- [ ] 不在日志或配置中保存明文密钥、原始 LLM provider payload 或敏感原文。
