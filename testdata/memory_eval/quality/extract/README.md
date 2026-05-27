# MemoryCore Live Extraction Quality Cases

本目录用于存放真实 LLM 抽取质量用例，定位等同于
`testdata/memory_eval/quality/retrieval/`：用于人工或 nightly 观察真实能力，
不是 controlled regression，也不应该伪造 provider 输出。

这里的用例必须满足：

- `suite: quality_extract`
- `quality_mode: true`
- `allow_stub: false`
- 使用 synthetic episodes，不放真实用户聊天记录
- 不提交 API key、provider 原始响应、完整 prompt 或敏感原文
- 真实 provider 使用 `openai-compatible`，key 只通过环境变量传入
- 调试输出放到 `artifacts/` 或本地临时目录，并用 `raw-log` 追踪

## 当前运行方式

`memory-eval` 会把 `--suite extract --mode live` 映射到本目录，并只在显式
live eval 时调用真实 LLM：

```powershell
$env:MEMORYCORE_LLM_API_KEY = "<key>"

go run ./cmd/memory-eval `
  --suite extract `
  --mode live `
  --provider openai-compatible `
  --base-url https://api.deepseek.com `
  --model deepseek-v4-flash `
  --api-key-env MEMORYCORE_LLM_API_KEY `
  --thinking false `
  --report-dir artifacts/memory_eval/live_extract
```

`--thinking` 默认是 `false`，会向 OpenAI-compatible 请求显式发送
`thinking.type=disabled`；需要打开 DeepSeek 思考模式时传 `--thinking true`。

需要形成可回归断言时，把真实 LLM 的响应固化为 JSON response fixture，再放到
`testdata/memory_eval/extraction_consolidation/responses/`，通过
`apply_extraction_response` 跑确定性回归。

## 文件约定

- `LE001_*.yaml`：正式 live extraction case，会被 `--suite extract --mode live` 发现。
- `LE001_*.yaml.example`：模板或草稿，不会被 fixture discover 自动执行。
- `case_id` 应与文件名主干一致，便于报告和 raw-log 对齐。

## 模板

参考 [LE001_identity_live.yaml.example](LE001_identity_live.yaml.example)。
