# Go源码解析：MewCode ch06 上下文管理与长会话压缩

## 模块概览

ch06 的目标是让 MewCode 从“每轮都把完整历史塞回模型”演进到“能在有限上下文窗口里长时间工作”。上一阶段 ch05 解决的是外部 MCP 工具接入和延迟 schema 暴露；这一阶段处理的是更底层的运行时问题：对话历史会越来越长，工具结果会反复占用 token，直到 provider 返回 `prompt_too_long`。

本章没有改 provider 的消息协议，也没有把压缩逻辑散落到 OpenAI、Anthropic 适配层。新增能力集中在 `internal/contextmgr`，Agent 主循环只在几个边界调用它：

```text
请求发出前:
  messages + allowedTools
    -> contextmgr.ManageBeforeRequest
    -> 第一层工具结果落盘替换
    -> 估算 token
    -> 必要时 LLM 摘要
    -> provider.ChatRequest

工具执行后:
  []tool.Result
    -> contextmgr.ObserveToolResults
    -> FileTracker 记录最近 Read 文件快照

请求结束后:
  provider.Usage
    -> contextmgr.RecordUsage
    -> Estimator 更新 usage anchor
```

整体设计是两层防御：

1. **轻量预防**：每次请求前扫描工具结果。单条结果超过 50KB，或同一条工具结果消息聚合超过 200KB，就把完整内容写到 `.mewcode/sessions/<session_id>/tool-results/<tool_use_id>`，对话里只留下稳定预览体。
2. **重量兜底**：估算 token 接近 `context_window` 时，发起一次无工具的 LLM 摘要，把早期历史压成结构化摘要，再补上恢复段和近期原文。

第一层控制工具结果膨胀，第二层控制整段会话膨胀。手动 `/compact` 和紧急 `prompt_too_long` 也复用第二层核心路径。

## 代码落点

| 文件 | ch06 职责 |
| --- | --- |
| `internal/config/config.go` | Provider 增加 `context_window`，提供 OpenAI/Anthropic 默认窗口 |
| `internal/contextmgr/constants.go` | 收纳固定阈值：50KB、200KB、33K 自动余量、3K 手动余量等 |
| `internal/contextmgr/session.go` | 创建 session id、会话目录、ledger、file tracker、auto tracker |
| `internal/contextmgr/store.go` | 工具结果按 `tool_use_id` 落盘，已存在则跳过写入 |
| `internal/contextmgr/offload.go` | 第一层 `OffloadAndSnip`：扫描工具结果、落盘、替换预览体 |
| `internal/contextmgr/estimate.go` | usage anchor + `bytes / 3.5` 的近似 token 估算 |
| `internal/contextmgr/split.go` | 摘要后近期原文保留边界，避免切开 tool call / tool result 对 |
| `internal/contextmgr/summary.go` | 无工具摘要请求、`<analysis>/<summary>` 抽取、PTL 丢组重试 |
| `internal/contextmgr/recovery.go` | 构造恢复三段：最近文件快照、当前工具列表、边界提示 |
| `internal/contextmgr/file_tracker.go` | 从成功 `Read` 工具结果记录纯净文件内容 |
| `internal/contextmgr/auto_tracker.go` | 自动摘要连续失败熔断 |
| `internal/contextmgr/manager.go` | 编排自动、手动、紧急三条压缩路径 |
| `internal/agent/runner.go` | 请求前压缩、usage 记录、Read 观察、PTL 紧急压缩重试 |
| `internal/chat/session.go` | slash 命令注册表，新增 `/compact`，并发互斥 |
| `internal/provider/provider.go` | `prompt_too_long` 错误识别 |
| `internal/provider/event.go` | 新增 context 管理事件 |
| `internal/tui/update.go` | 展示压缩状态和 token 前后变化 |
| `internal/app/app.go` | 创建 `contextmgr.Manager` 并注入 Chat / Agent |
| `internal/contextmgr/contextmgr_test.go` | 覆盖 offload、估算、恢复段、熔断、摘要请求 |
| `internal/agent/agent_test.go` | 覆盖紧急压缩和重试 |
| `internal/chat/session_test.go` | 覆盖 `/compact` 命令路径 |

## 本阶段解决的问题

| 问题 | 典型风险 | ch06 的处理 |
| --- | --- | --- |
| 工具结果反复占 token | 大文件、grep、bash 输出在每轮请求里重复出现 | 超阈值工具结果落盘，对话里只留稳定预览体 |
| 长会话撞上下文窗口 | provider 返回 `prompt_too_long`，最新用户输入无法处理 | 自动摘要 + 紧急压缩 + 一次重试 |
| 摘要丢失关键事实 | 模型靠摘要脑补文件细节或工具能力 | 恢复段补最近文件快照、当前工具列表、边界提示 |
| 估算成本过高 | 精确 tokenizer 增加依赖和运行时开销 | 锚定 provider usage，新增内容用 `bytes / 3.5` 估算 |
| 自动失败死循环 | 摘要连续失败时每轮都再次尝试 | 自动路径 3 次失败后熔断，手动/紧急绕过 |
| 用户想主动整理上下文 | 只能等系统自动触发 | `/compact` 无条件触发摘要，不发送普通 LLM 对话 |

## 为什么不是简单地“全量摘要”

最直接的做法是：只要上下文长了，就让 LLM 把所有历史摘要掉。这个方案看起来简单，但会丢掉三个关键工程性质。

| 简单全量摘要的问题 | 后果 | ch06 的选择 |
| --- | --- | --- |
| 用户原文被改写 | 需求、约束、纠错语气可能被摘要误伤 | 近期原文保留，摘要第 6 节优先保留用户原话 |
| 文件细节被压成自然语言 | 代码、错误、路径容易被模型补全错 | 最近 Read 文件快照 + 边界提示要求重读 |
| 工具结果仍然巨大 | 摘要请求自己也可能撞墙 | 先做第一层 offload，把大结果挪到磁盘 |
| 每次摘要字符串不稳定 | prompt cache 更难命中 | ledger 冻结同一个工具结果的替换字符串 |
| 自动失败会反复尝试 | 程序看起来卡死 | AutoTracker 熔断自动路径 |

所以 ch06 不是“摘要功能”，而是一套上下文生命周期管理：

```text
ToolResult{ID, Content}
  -> 大结果: ToolResultStore 写完整内容
  -> ChatMessage.ToolResults[i].Content = stable preview
  -> Conversation 继续增长
  -> Estimator 判断接近 context_window
  -> Summarizer 压缩早期历史
  -> Recovery 恢复关键运行事实
  -> Recent originals 保留尾部原文
```

## 核心抽象

| Type | File | Role | Critical fields | Produced by | Consumed by | Lifecycle / transformation |
| --- | --- | --- | --- | --- | --- | --- |
| `config.ProviderConfig.ContextWindow` | `internal/config/config.go` | provider/model 上下文窗口 | `ContextWindow int` | YAML loader | `contextmgr.NewManager` | 配置值 >0 时使用，否则 OpenAI 128K、Anthropic 200K |
| `contextmgr.Manager` | `internal/contextmgr/manager.go` | 上下文管理总编排器 | `Session`、`Store`、`Estimator`、`Summarizer`、`Auto` | App 启动 | Agent / Chat | 自动、手动、紧急三条路径共享同一状态 |
| `SessionState` | `internal/contextmgr/session.go` | 单进程会话状态 | `ID`、`RootDir`、`Ledger`、`Files`、`Auto`、`Usage` | `NewSessionState` | Manager | 进程内创建一次，目录退出后保留 |
| `ReplacementLedger` | `internal/contextmgr/session.go` | 工具结果替换决策账本 | `seenIds`、`replacements`、`paths` | SessionState | OffloadAndSnip | keep/replace 一旦写入，本会话内不翻转 |
| `ToolResultStore` | `internal/contextmgr/store.go` | 工具结果落盘器 | `Root` | Manager | OffloadAndSnip | `tool_use_id` 映射到同名文件，已存在跳过写入 |
| `OffloadReport` | `internal/contextmgr/types.go` | 第一层压缩报告 | `Replaced`、`Kept`、`BytesBefore`、`BytesAfter` | OffloadAndSnip | Agent / TUI event | 用于可观测输出和测试断言 |
| `Estimator` | `internal/contextmgr/estimate.go` | 粗略 token 估算 | `ContextWindow` | Manager | Manager / tests | 有 usage anchor 时只估新增消息；否则全量扫描 |
| `UsageAnchor` | `internal/contextmgr/types.go` | 最近一次真实 usage 锚点 | `Usage`、`MessageCount`、`Valid` | `RecordUsage` | Estimator | 每次 LLM 请求后替换，不累加 |
| `Summarizer` | `internal/contextmgr/summary.go` | LLM 摘要请求封装 | `Provider`、`Config` | Manager | Manager | 请求不带 tools，只抽取 `<summary>` |
| `SummaryRequest` | `internal/contextmgr/types.go` | 摘要输入 | `Messages`、`Mode`、`ContextWindow`、`SafetyMargin` | Manager | Summarizer | 自动/手动/紧急共享底层处理 |
| `FileTracker` | `internal/contextmgr/file_tracker.go` | 最近 Read 文件快照 | `files map[string]FileSnapshot` | SessionState | Recovery | 同一路径重复读取会覆盖内容和时间 |
| `AutoTracker` | `internal/contextmgr/auto_tracker.go` | 自动摘要熔断器 | `failures`、`tripped`、`lastErr` | SessionState | Manager | 自动失败 +1，成功清零；手动/紧急不计入 |
| `provider.ContextEvent` | `internal/provider/event.go` | 压缩状态事件 | `Mode`、`BeforeTokens`、`AfterTokens`、`ReplacedToolResults` | Agent / Chat | TUI | 让用户看见自动、手动、紧急压缩进度 |
| `builtinCommands` | `internal/chat/session.go` | slash 命令路由表 | `/plan`、`/do`、`/compact`、`/exit` | Chat 包静态定义 | `ParseCommand` | 未注册 slash 不进 LLM，直接报可用命令 |

### ReplacementLedger：缓存稳定性的关键

工具结果压缩不是每轮重新生成一段预览。对于同一个 `tool_use_id`：

```text
第一次 seen:
  content <= threshold -> CommitKeep(id)
  content > threshold  -> Write(id, content) -> BuildPreview -> CommitReplace(id)

后续 seen:
  keep    -> 原文保持原文
  replace -> 使用 ledger.replacements[id]，逐字节一致
```

这有两个目的：

1. 避免同一个工具结果在不同轮被替换成不同字符串，破坏 prompt cache。
2. 避免“第 N 轮保留、第 N+1 轮替换”的上下文漂移。

## 数据流

### 第一层：工具结果落盘替换

```text
provider.ChatMessage{ToolResults}
  -> candidate{index, tool_use_id, len(Content)}
  -> sort by bytes desc
  -> ReplacementLedger.Decision(id)
       seen replace: Content = frozen replacement
       seen keep:    Content unchanged
       unseen:
         bytes > 50000 OR aggregate > 200000
           -> ToolResultStore.Write(id, full content)
           -> BuildPreviewReplacement{bytes, preview, path, reread prompt}
           -> Ledger.CommitReplace(id, replacement)
           -> ToolResultMessage.Content = replacement
         otherwise:
           -> Ledger.CommitKeep(id)
```

落盘路径是：

```text
.mewcode/sessions/<session_id>/tool-results/<tool_use_id>
```

预览体只保留头部：

```text
first 20 lines
  -> then max 2048 UTF-8 bytes
  -> stable replacement string
```

### 第二层：摘要恢复

```text
messages after OffloadAndSnip
  -> Estimator.Estimate(anchor + bytes/3.5)
  -> if tokens < context_window - 33000:
       pass through
  -> else:
       SplitRecent(messages)
         older -> Summarizer(no tools)
         recent -> 原文保留
       BuildRecovery{
         FileTracker.Recent(5),
         same allowedTools,
         BoundaryPrompt,
       }
       new messages:
         system(summary)
         system(recovery)
         recent originals
```

摘要请求本身明确不传 tools：

```text
provider.ChatRequest{
  SystemPrompt: "must not call any tools..."
  Messages: [summary prompt]
  Tools: nil
}
```

模型被要求输出：

```text
<analysis>草稿</analysis>
<summary>正式摘要</summary>
```

系统只保留 `<summary>`，丢掉 `<analysis>`。

### Read 文件追踪

```text
ToolExecutor.ExecuteToolBatches
  -> []tool.Result
       Result{Tool:"Read", OK:true, Data{"path","content"}}
  -> ContextManager.ObserveToolResults
  -> FileTracker.files[path] = FileSnapshot{Content, ReadAt, Bytes}
  -> build provider.ToolResultMessage
  -> messages += RoleUser{ToolResults}
```

这里记录的是 `tool.Result.Data["content"]`，不是已经渲染或截断后的 `ToolResultMessage.Content`。这样恢复段能拿到更干净的文件片段。

## 主流程：Agent 请求前

Runner 每一轮开头只计算一次工具集合：

```text
allowedTools := AllowedDefinitions(...)
deferredToolNames := DeferredToolNames(...)
```

同一份 `allowedTools` 有两个去处：

```text
allowedTools
  -> contextmgr.ManageBeforeRequest(..., AllowedTools)
       Recovery.CurrentAvailableTools
  -> provider.ChatRequest.Tools
```

这个约束避免恢复段告诉模型“你有 A 工具”，但真正请求里传的是另一组工具。

完整请求路径：

```text
Runner.run iteration
  -> allowedTools / deferredToolNames
  -> ContextManager.ManageBeforeRequest
       OffloadAndSnip
       Estimate
       maybe SummarizeAndRestore
  -> Provider.StreamChat
  -> StreamCollector.Collect
  -> ContextManager.RecordUsage(round.Usage, len(messages))
  -> if tool calls:
       ToolExecutor.ExecuteToolBatches
       ContextManager.ObserveToolResults
       messages += ToolResults
```

## 状态机：自动、手动、紧急

```text
Idle
  -> Normal Run
       -> OffloadAndSnip
       -> Estimate < auto threshold
            -> Provider request
       -> Estimate >= auto threshold
            -> AutoTracker.Tripped?
                 yes -> Provider request without summary
                 no  -> Auto Summarize
                         success -> reset failures
                         failure -> failures++

Idle
  -> User types /compact
       -> command path, not LLM chat
       -> runMu locked
       -> ForceCompact
            skip OffloadAndSnip for this command
            skip auto threshold
            skip circuit breaker
       -> History replaced

Provider request
  -> prompt_too_long
       -> EmergencyCompact
            force OffloadAndSnip
            SummarizeAndRestore
            clear usage anchor
            estimate < context_window - 3000 ?
              yes -> retry original request once
              no  -> surface error
```

这个状态机里，熔断只管自动路径。手动 `/compact` 是用户明确动作，紧急压缩是错误恢复动作，都不受自动熔断限制。

## trick 1：先压工具结果，再摘要整段历史

紧急路径里最容易踩的坑是：普通请求已经太长了，直接发摘要请求可能也太长。ch06 在 `EmergencyCompact` 中强制先跑第一层：

```text
prompt_too_long
  -> EmergencyCompact
       -> OffloadAndSnip(messages)
       -> SummarizeAndRestore(offloaded messages)
```

这样大工具结果先被挪到磁盘，摘要请求的输入体积会明显下降。

自动路径也先做第一层，再判断是否需要摘要：

```text
OffloadAndSnip
  -> Estimate
  -> maybe summary
```

这意味着很多会话只靠轻量 offload 就能继续跑，不必频繁调用 LLM 摘要。

## trick 2：summary 请求没有 tools

摘要模型不应该读文件、不应该跑命令，也不应该在压缩过程中产生新的工具副作用。`BuildSummaryChatRequest` 固定把 `Tools` 设为空。

```text
summary request:
  Tools: nil
  SystemPrompt: "You must not call any tools..."
```

测试里直接抓 summary request：

```text
TestSummaryRequestHasNoTools
TestManagerAutoCompacts
TestCompactCommandUsesSummaryPath
TestRunnerEmergencyCompactsAndRetriesOnce
```

这把“摘要不调工具”从 prompt 约束变成了请求体约束。

## trick 3：近期原文不是任一满足即停

摘要后保留尾部原文时，规则是：

```text
tokens >= 10000 AND messages >= 5
```

两个下界都满足才停止。这样避免只保留 5 条很短消息，也避免只保留一两条超长消息。`SplitRecent` 从尾部向前累加，并检查 tool 边界：

```text
if recent[0] is ToolResults:
  move boundary backward until preceding assistant ToolCalls is included
```

这保证模型不会看到一个孤立的 tool result，却看不到对应的 tool call。

## trick 4：恢复段把“摘要不可逆”的部分补回来

摘要适合保存意图、进展和决策，不适合保存代码原文。恢复段有三块：

```markdown
## Recent Read File Snapshots
...

## Current Available Tools
...

## Boundary Prompt
...
```

其中最近文件快照最多 5 个，按读取时间倒序。单文件最多约 `5000 * 3.5` bytes，超出就追加：

```text
(content truncated)
```

边界提示固定要求模型：需要原文时重新读取，不要根据摘要猜代码细节。

## trick 5：手动 `/compact` 是命令，不是聊天

ch06 把 slash 输入统一放进 `builtinCommands`：

```text
/plan
/do
/compact
/exit
```

命令路径有三个约束：

1. 不写入 conversation。
2. 不作为普通用户消息发给 LLM。
3. 未注册 slash 命令返回可用命令提示。

`/compact` 本身调用 `ForceCompact`，跳过第一层、跳过自动阈值、跳过自动熔断。它代表用户主动整理上下文，而不是系统自己判断。

## trick 6：PTL 降载按用户提交分组

摘要请求自己也可能 `prompt_too_long`。这时不能随便从消息中间砍，因为会破坏对话结构。`summary.go` 先按“用户提交 -> 后续 assistant/tool 往返”分组：

```text
group 1:
  user request A
  assistant tool call
  tool result
  assistant response

group 2:
  user request B
  ...
```

PTL 重试策略：

```text
initial summary request
  -> PTL
     retry 1: drop oldest 1 group
     retry 2: drop oldest 1 group
     retry 3: drop oldest 1 group
     retry 4+: drop ceil(remaining * 0.2), at least 1
```

如果已经不能形成非空请求，就返回摘要失败，不发送空 messages。

## 和 ch05 的继承关系

ch06 没有推翻 ch05 的工具体系，而是在它之上管理上下文体积。

| ch05 能力 | ch06 如何继承 |
| --- | --- |
| Registry 输出当前可见 tools | 恢复段工具列表复用同一份 `allowedTools` |
| ToolSearch / 延迟 MCP 工具 | 当前可用工具可能随会话变化，恢复段按本轮实际 tools 写 |
| ToolExecutor 统一执行工具 | FileTracker 观察执行后的 `tool.Result`，不关心工具来自本地还是 MCP |
| provider.ChatMessage 是统一消息模型 | OffloadAndSnip 只改 `ToolResults.Content`，不改 provider 协议 |
| TUI 通过 StreamEvent 更新界面 | 新增 `StreamEventTypeContext` 展示压缩状态 |

可以把 ch05 和 ch06 的关系理解成：

```text
ch05: 外部工具如何进入 Agent Runtime
ch06: Agent Runtime 如何在长会话里保持上下文可控
```

## 工程边界

| 本阶段不做 | 原因 |
| --- | --- |
| 精确 tokenizer | 依赖更重，provider usage anchor 已足够稳定 |
| 摘要质量反馈优化 | 本阶段先固定 prompt 和结构，保证行为可测 |
| 跨进程会话恢复 | session id 只在进程内有效，目录保留用于调试 |
| `.mewcode/sessions` 自动清理 | 避免误删调试材料，清理由用户或外部脚本处理 |
| Skill 恢复段 | 当前项目还没有完整 Skill 子系统 |
| prompt cache 命中率监控 | 只通过替换字符串冻结提高稳定性，不采集指标 |
| 暴露所有阈值 | 只暴露 `context_window`，其余阈值保持代码常量 |

## 测试覆盖

| 测试 | 关注点 |
| --- | --- |
| `TestOffloadSingleToolResult` | 单条 50KB+ 工具结果落盘、预览体、路径和完整内容 |
| `TestOffloadAggregateLimitReplacesLargestMinimum` | 单条未超阈值但聚合超 200KB 时，只替换必要数量 |
| `TestOffloadDecisionFrozen` | keep 决策不翻转 |
| `TestPreviewLimits` | 20 行 / 2048 bytes / UTF-8 安全截断 |
| `TestEstimatorUsesAnchorAndIncrement` | usage anchor + 增量估算 |
| `TestSplitRecentKeepsToolPair` | 近期原文不切开 tool call / result |
| `TestRecoveryIncludesFilesToolsAndBoundary` | 恢复三段、工具列表、文件截断标记 |
| `TestFileTrackerObservesReadOnly` | 只记录成功 Read 工具的纯净内容 |
| `TestAutoTrackerTripsAndResets` | 自动摘要 3 次失败熔断，成功重置 |
| `TestSummaryRequestHasNoTools` | 摘要请求不携带 tools |
| `TestManagerAutoCompacts` | 自动阈值触发摘要，输出 summary + recovery |
| `TestIsPromptTooLong` | 常见 provider PTL 错误识别 |
| `TestCompactCommandUsesSummaryPath` | `/compact` 不走普通聊天，只走摘要路径 |
| `TestRunnerEmergencyCompactsAndRetriesOnce` | 普通请求 PTL 后紧急压缩并只重试一次 |

回归命令：

```powershell
go test ./...
go test -race ./internal/contextmgr ./internal/agent ./internal/chat
```

当前实现已通过这两组命令。

## 阅读顺序

建议按“配置 -> 状态 -> 第一层 -> 第二层 -> Agent 接入”的顺序读：

```text
internal/config/config.go
  -> internal/contextmgr/constants.go
  -> internal/contextmgr/session.go
  -> internal/contextmgr/store.go
  -> internal/contextmgr/offload.go
  -> internal/contextmgr/estimate.go
  -> internal/contextmgr/split.go
  -> internal/contextmgr/summary.go
  -> internal/contextmgr/recovery.go
  -> internal/contextmgr/file_tracker.go
  -> internal/contextmgr/auto_tracker.go
  -> internal/contextmgr/manager.go
  -> internal/agent/runner.go
  -> internal/chat/session.go
  -> internal/tui/update.go
  -> internal/app/app.go
```

如果只想抓住 ch06 主线，读四处就够：

```text
1. internal/contextmgr/offload.go
   看工具结果如何从对话中变成磁盘文件 + 稳定预览体。

2. internal/contextmgr/manager.go
   看自动、手动、紧急三条路径如何复用摘要恢复。

3. internal/contextmgr/summary.go + recovery.go
   看摘要请求如何禁止 tools，以及恢复段如何补回不可摘要的事实。

4. internal/agent/runner.go
   看上下文管理如何嵌进每轮 Agent 调用，而不改变 provider 消息协议。
```

这一章的核心可以压缩成一句话：

```text
MewCode 用“工具结果落盘 + LLM 结构化摘要 + 恢复段”管理长会话上下文，让 Agent 在有限 token 窗口里继续工作，同时保留可重读、可观测、可恢复的工程边界。
```
