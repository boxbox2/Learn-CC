# Go源码解析：MewCode ch07 启动恢复、长期记忆与内置命令框架

## 模块概览

ch06 解决的是单进程长会话里的上下文膨胀：工具结果落盘、自动/手动压缩、紧急压缩重试。ch07 往前后各补一段能力：启动时能恢复项目规则、会话现场和长期记忆；运行时能用斜杠命令绕过 Agent，直接触发确定性本地操作。

这一章可以理解为把 MewCode 从“能跑一轮 Agent Loop”推进到“像一个可持续工作的本地 Coding Agent”：

```text
进程启动
  -> instructions.Load
       AGENTS.md + .mewcode/instructions.md + ~/.mewcode/instructions.md
  -> sessionstore.Create / Restore
       .mewcode/sessions/<YYYYMMDD-HHMMSS-xxxx>.jsonl
  -> memory.NewManager
       user/project memory notes + index

用户输入
  -> "/" 开头: command.Registry.Parse -> TUI 本地分发
  -> 普通文本: chat.Session.Submit(mode) -> agent.Run

Agent 最终回复
  -> sessionstore 追加 JSONL
  -> memory.UpdateAsync(snapshot)
```

## 代码落点

| 文件 | 本章职责 |
| --- | --- |
| `internal/instructions/loader.go` | 加载三层静态指令并按优先级拼接 |
| `internal/instructions/include.go` | 展开 `@include`，处理深度、循环和路径逃逸 |
| `internal/sessionstore/writer.go` | 追加写 JSONL，会话重置时提供 `Close` 生命周期 |
| `internal/sessionstore/restore.go` | 恢复 JSONL，跳过坏行并截断孤立工具调用 |
| `internal/memory/manager.go` | 异步更新用户级/项目级长期记忆 |
| `internal/memory/extractor.go` | 调 LLM 生成结构化 memory changes |
| `internal/agent/runner.go` | 注入 PromptContext，记录 assistant/tool 历史 |
| `internal/chat/session.go` | 普通提交、计划提交、压缩、新会话重置、状态查询 |
| `internal/command/registry.go` | 命令注册中心与别名冲突校验 |
| `internal/command/parser.go` | 零参数斜杠命令解析和 Running 门禁 |
| `internal/command/completion.go` | 命令补全候选生成 |
| `internal/command/builtin/handlers.go` | `/help`、`/clear`、`/review`、`/exit` 等内置命令 |
| `internal/tui/update.go` | 回车入口分流、补全按键、命令到 UI 的适配 |
| `internal/app/app.go` | 启动时装配 instructions、memory、sessionstore、command registry |

## 本章解决的问题

1. **启动后缺上下文**
   旧版本每次启动都像第一次进入项目。ch07 增加静态指令加载、会话恢复和长期记忆索引注入，让 Agent 启动后能接上项目规则和用户偏好。

2. **长会话缺持久化现场**
   用户消息、assistant 回复、tool calls、tool results 都追加写入 JSONL。恢复时跳过坏行，遇到孤立工具调用会截断到安全边界，避免 provider 收到半截工具协议。

3. **长期偏好不应阻塞主对话**
   记忆更新发生在最终回复后，后台 goroutine 只拿只读快照，不持有 `chat.Session` 指针。更新失败只记录错误，不影响当前回复。

4. **清屏、查状态不该消耗 Agent token**
   斜杠命令在 TUI 回车入口先分流。命中本地命令时不会进入 Agent，只有 `/review` 这种预设提示词命令才会发送给 AI。

5. **Running 状态下要防止并发改历史**
   `/clear`、`/compact`、`/plan`、`/do`、`/review` 只能在 Idle 执行；Running 时只允许 `/status` 等只读命令和 `/exit` 安全关停。

## 核心抽象

| Type | 文件 | 作用 | 关键字段 |
| --- | --- | --- | --- |
| `sessionstore.Line` | `internal/sessionstore/line.go` | JSONL 单行消息格式 | `SessionID`、`Seq`、`Role`、`ToolCalls`、`ToolResults` |
| `sessionstore.Writer` | `internal/sessionstore/writer.go` | 会话追加写入器 | `id`、`path`、`seq`、`mu` |
| `memory.Snapshot` | `internal/memory/types.go` | 后台记忆更新输入 | `SessionID`、`Messages`、`FinalText` |
| `memory.ChangeSet` | `internal/memory/types.go` | LLM 返回的记忆变更 | `Action`、`Type`、`Scope`、`Filename`、`Content` |
| `agent.PromptContextProvider` | `internal/agent/runner.go` | 动态 prompt 上下文来源 | `CustomInstructions`、`LongTermMemory` |
| `chat.SubmitMode` | `internal/chat/session.go` | 普通输入的运行模式 | `default`、`plan` |
| `command.Definition` | `internal/command/types.go` | 命令元数据 | `Name`、`Aliases`、`Kind`、`Hidden`、`Handler` |
| `command.Controller` | `internal/command/types.go` | 命令与 UI/会话交互的抽象边界 | `SendUserMessage`、`ClearAndResetSession`、`Shutdown` |
| `command.CompletionState` | `internal/command/completion.go` | TUI 补全菜单状态 | `Active`、`Items`、`Highlighted`、`NoMatch` |

## 数据流

### 启动恢复链路

```text
App.RunChat
  -> instructions.Load
       LoadResult{Text, Files, Diagnostics}
  -> sessionstore.Create / Restore
       []provider.ChatMessage
  -> contextmgr.ForceCompact?      # 仅历史过大时
       compacted messages
  -> memory.NewManager
       user/project index
  -> chat.NewSessionWithOptions
       History + Archive + PromptContext + Memory
```

### 请求前 Prompt 注入

```text
agent.Runner
  -> PromptContext.CustomInstructions(ctx)
  -> PromptContext.LongTermMemory(ctx)
  -> prompt.BuildSystemPrompt
       Custom Instructions
       Long Term Memory
  -> provider.ChatRequest
```

### 后台记忆更新

```text
agent final answer
  -> chat.Session.maybeUpdateMemory
  -> memory.Snapshot{Messages, FinalText}
  -> memory.Extractor.Extract
       LLM JSON: ChangeSet
  -> memory.Manager.Apply
       user notes / project notes
  -> RewriteIndex
```

### 斜杠命令分流

```text
textarea.Value
  -> command.Registry.Parse
       Empty | Chat | Command | Unknown
  -> Chat:
       chat.Session.Submit(input, ChatMode)
  -> Command:
       CanExecute(Kind, AgentState)
         deny: local message
         allow: command.Handler(controller)
```

## 命令状态机

```text
Idle
  -> /status           read-only local output
  -> /plan             mode = [PLAN]
  -> /do               mode = [DEFAULT]
  -> /clear            reset visible output + new JSONL session
  -> /review           fixed prompt -> AI
  -> /exit             close writer + quit

Running
  -> /status           allowed
  -> /clear            rejected: 请等待当前任务完成
  -> /review           rejected: 请等待当前任务完成
  -> /exit             StreamCancel -> Close -> Quit
```

## 内置命令

本章固定内置十一个零参数命令：

```text
/help /compact /clear /plan /do /session /memory /permission /status /review /exit
```

命令系统不做参数解析。`/review internal/tui` 不会被理解成“审查 internal/tui”，而是未命中命令并提示 `/help`。这样第一期命令框架只承担路由分发职责，避免 tokenizer、转义、多行参数和参数补全把复杂度提前引入。

补全也遵循终端习惯：Tab 只补全文本，不执行命令；Enter 才执行当前高亮命令。

```text
/st + Tab    -> textarea = "/status"
/status + Enter -> execute /status
```

## 关键设计

### 1. 指令加载只处理静态文本

`internal/instructions` 不读会话、不写记忆、不调用 LLM。它只处理三层 Markdown、`@include` 展开、循环检测、深度限制和路径安全。这样启动阶段的规则加载保持可预测，也便于测试。

### 2. JSONL 不维护 meta 文件

会话标题、消息数、更新时间和坏行数都通过扫描 JSONL 计算。少一份 meta 文件，就少一份同步状态；崩溃恢复时最多损坏最后一行。

### 3. 记忆更新只拿快照

`memory.UpdateAsync` 接收深拷贝后的 `Snapshot`。后台任务不会持有 `chat.Session`，也不会阻塞用户看到最终回复。失败只写入 `LastError` 和日志。

### 4. `/clear` 是新会话边界

`/clear` 不只是清屏。它会清空可见输出、重置 usage、关闭旧 writer、创建新的 Session ID 和 JSONL writer，并清空内存会话历史。UI 和持久化状态保持一致。

### 5. `/exit` 是 Running 门禁例外

大多数会修改状态的命令在 Running 时被拒绝，但 `/exit` 必须能执行。它会先取消当前流式任务，再关闭会话 writer，并触发 TUI 退出。

## 工程边界

| 不做 | 原因 |
| --- | --- |
| 用户自定义命令 | 留给后续 Skill 系统 |
| 命令参数解析 | 第一阶段保持命令系统只是路由层 |
| `/review <path>` | 避免参数和文件读取耦合进命令框架 |
| 命令级权限控制 | 现阶段仍由工具权限系统负责 |
| 向量数据库/RAG | 本章只做小索引注入和本地 Markdown 记忆 |
| 团队共享记忆 | 用户级/项目级本地隔离已经够本阶段使用 |

## 测试覆盖

```text
go test ./internal/instructions
go test ./internal/sessionstore
go test ./internal/memory
go test ./internal/command/...
go test ./internal/chat
go test ./internal/tui
go test ./...
```

重点行为：
- include 路径防逃逸、循环和深度限制
- JSONL 坏行跳过、孤立工具调用截断
- memory change 校验、笔记写入、索引限制
- 命令注册冲突 panic
- 零参数解析和未知命令引导
- Running 状态门禁
- `/clear` 新会话边界
- `/exit` 安全关停
- Tab 补全不执行命令

## 阅读顺序

1. `internal/app/app.go`：看启动时如何装配 instructions、sessionstore、memory、command registry。
2. `internal/instructions/loader.go`：看三层指令如何合并。
3. `internal/sessionstore/line.go`、`writer.go`、`restore.go`：看会话如何落盘和恢复。
4. `internal/memory/types.go`、`manager.go`、`extractor.go`：看长期记忆如何异步生成和写入。
5. `internal/agent/runner.go`：看 PromptContext 和 Recorder 如何接入 Agent Loop。
6. `internal/command/types.go`、`registry.go`、`parser.go`：看命令系统的核心抽象。
7. `internal/command/builtin/handlers.go`：看十一个内置命令如何只依赖 Controller。
8. `internal/tui/update.go`：看回车分流、Running 门禁、补全和 `/exit` 如何落到界面层。
