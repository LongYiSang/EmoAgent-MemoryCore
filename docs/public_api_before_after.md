# MemoryCore Public API Before / After

本文只说明本次 breaking API 改动前后的外部接入差异。

## Before

旧 public API 把 `internal/app/memorycore` 的大 `Service` interface 和 DTO 直接暴露给外部：

```go
svc, err := memorycore.Open(ctx, memorycore.Options{DBPath: "./data/memory.db"})
session, err := svc.StartSession(ctx, memorycore.StartSessionRequest{})
ctx, err := svc.Retrieve(ctx, memorycore.RetrievalRequest{QueryText: "coffee"})
result, err := svc.ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{})
forget, err := svc.Forget(ctx, memorycore.ForgetRequest{})
```

`pkg/memorycore/aliases.go` 中大量 `type = appcore.Type` 让 internal DTO 字段变更等同于 public API 变更。

## After

新 public API 仍保留 `pkg/memorycore`，但入口固定为 `*memorycore.Client`，能力按接口分组：

```go
client, err := memorycore.Open(ctx, memorycore.Options{DBPath: "./data/memory.db"})
session, err := client.Sessions().StartSession(ctx, memorycore.StartSessionRequest{})
mc, err := client.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{QueryText: "coffee"})
result, err := client.Writes().ConsolidateCandidate(ctx, memorycore.ConsolidateCandidateRequest{})
```

分组接口：

- `Sessions()`：session 和 episode 生命周期。
- `Retrieval()`：长期记忆检索。
- `Writes()`：entity、consolidation、extraction 写入能力。
- `Forget()`：preview / execute / verify 遗忘流程。
- `Ops()`：retention、natural memory、compression、curation、mirror、search rebuild 等运维能力。

public DTO 现在在 `pkg/memorycore` 内独立定义。`internal/app/memorycore` 只允许出现在 public facade / adapter / converter 实现文件中。

## Host Migration

对话生命周期：

```go
session, err := client.Sessions().StartSession(ctx, memorycore.StartSessionRequest{
	Channel: memorycore.ChannelAPI,
})
_, err = client.Sessions().AppendEpisode(ctx, memorycore.AppendEpisodeRequest{
	SessionID: session.ID,
	Role:      memorycore.RoleUser,
	Content:   userText,
})
```

检索：

```go
mc, err := client.Retrieval().Retrieve(ctx, memorycore.RetrievalRequest{
	SessionID: &session.ID,
	QueryText: userText,
})
```

抽取：

```go
result, err := client.Writes().RunExtraction(ctx, memorycore.RunExtractionRequest{
	SessionID: &session.ID,
	Trigger:   memorycore.ExtractionTriggerSessionEnd,
	Build:     &memorycore.ExtractionBuildSelector{SessionID: &session.ID, Limit: 50},
	Mode:      memorycore.ExtractionRunModeApply,
})
```

遗忘：

```go
previewReq := memorycore.ForgetPreviewRequest{
	RequestedLevel: memorycore.ForgetLevelSoft,
	ScopeMode:      memorycore.ForgetScopeExactNode,
	NodeType:       memorycore.ForgetNodeFact,
	NodeID:         factID,
}
preview, err := client.Forget().PreviewForget(ctx, previewReq)
if err != nil {
	return err
}
_, err = client.Forget().ExecuteForget(ctx, memorycore.ForgetExecuteRequest{
	Level:          memorycore.ForgetLevelSoft,
	PreviewRequest: previewReq,
	PreviewHash:    preview.PreviewHash,
	ConfirmedTargets: []memorycore.ExactNodeRef{{
		NodeType: memorycore.ForgetNodeFact,
		NodeID:   factID,
	}},
	Confirmed: true,
})
```

## Extraction Runtime Note

`pkg/memorycore/extractionruntime` is no longer part of the public host API. Extraction request construction, provider execution, audit, and apply all go through `client.Writes().RunExtraction` or `RunExtractionBatch`.
