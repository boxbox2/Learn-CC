# Go源码解析：MewCode ch02 工具系统与 Agent 雏形

## 模块概览

MewCode 是一个用 Go 编写的终端 AI 对话客户端实验项目。ch01 打通了“终端输入 -> 模型流式输出 -> Markdown 渲染”的最小聊天链路；ch02 在这条链路上加入工具系统，把项目推进到能处理本地文件、执行受控命令、并把工具结果回灌给模型的单轮闭环 Agent 雏形。

```text
用户在终端输入问题
  -> 读取配置
  -> 创建模型 Provider
  -> 维护进程内多轮会话
  -> 向模型暴露统一工具定义
  -> 接收模型流式文本、thinking 或工具调用事件
  -> 执行受控本地工具并硬截断结果
  -> 把 tool result 回灌给模型继续生成
  -> 在 Bubble Tea TUI 中展示工具行和增量回复
  -> 完成后用 Markdown 渲染输出
```

项目代码主要分布在 `cmd` 和 `internal` 下，核心文件如下：

| 文件 | 职责 |
| --- | --- |
| `cmd/mewcode/main.go` | 程序入口，创建 Cobra 根命令并执行 |
| `internal/cli/root.go` | CLI 根命令定义，默认进入聊天 TUI |
| `internal/app/app.go` | 装配层：加载配置、创建 Provider、Session、Renderer 和 TUI |
| `internal/config/config.go` | 配置结构、协议常量、配置校验 |
| `internal/config/loader.go` | 读取全局配置和项目配置，展开环境变量 |
| `internal/config/merge.go` | 合并全局配置与项目配置 |
| `internal/provider/provider.go` | Provider 统一接口 |
| `internal/provider/event.go` | 统一流式事件类型和 token usage 结构 |
| `internal/provider/message.go` | 内部消息模型和 ChatRequest |
| `internal/provider/factory.go` | Provider 注册表和工厂创建逻辑 |
| `internal/provider/openai/client.go` | OpenAI-compatible 客户端创建与注册 |
| `internal/provider/openai/stream.go` | OpenAI Chat Completions 流式事件适配 |
| `internal/provider/openai/tool.go` | OpenAI-compatible 工具定义、tool call 和 tool result 转换 |
| `internal/provider/anthropic/client.go` | Anthropic 客户端创建与注册 |
| `internal/provider/anthropic/stream.go` | Anthropic Messages 流式事件适配 |
| `internal/provider/anthropic/tool.go` | Anthropic 工具定义、tool_use 和 tool_result 转换 |
| `internal/chat/session.go` | 多轮历史、上次请求、重试和 assistant 消息提交 |
| `internal/tool/tool.go` | Provider 无关的工具接口、定义和执行请求 |
| `internal/tool/registry.go` | 工具注册中心 |
| `internal/tool/limits.go` | 工具输出大小、文件大小和命令超时限制 |
| `internal/tool/path.go` | 项目根目录内路径解析和越界防护 |
| `internal/tool/builtin/*.go` | Read、Write、Edit、Bash、Glob、Grep 六个内置工具 |
| `internal/tui/model.go` | Bubble Tea Model、UI 状态和依赖接口 |
| `internal/tui/update.go` | 按键处理、提交、重试、取消和流式事件消费 |
| `internal/tui/tool.go` | Claude Code 风格工具行和结果摘要渲染 |
| `internal/tui/view.go` | 终端界面渲染、状态栏、用户块和错误块 |
| `internal/tui/stream.go` | 将 Provider channel 转成 Bubble Tea message |
| `internal/tui/keymap.go` | 快捷键和命令常量 |
| `internal/tui/styles.go` | Lip Gloss 终端样式 |
| `internal/markdown/renderer.go` | Glamour Markdown 终端渲染 |

整体目录关系可以看成：

```text
cmd/mewcode
  -> internal/cli        Cobra 命令入口
  -> internal/app        程序装配层
  -> internal/config     配置读取、合并、校验
  -> internal/provider   统一模型协议抽象
      -> openai          OpenAI-compatible adapter
      -> anthropic       Anthropic adapter
  -> internal/tool       Provider 无关的本地工具系统
      -> builtin         Read/Write/Edit/Bash/Glob/Grep
  -> internal/chat       当前进程内多轮会话
  -> internal/tui        Bubble Tea 终端交互界面
  -> internal/markdown   Markdown 渲染
```

## ch02：工具系统与 Agent 雏形

ch02 的关键变化不是“多加几个函数”，而是把 MewCode 的职责边界重新切开：模型 Provider 只负责协议适配，工具层只负责本地能力和安全边界，Session 才负责单轮编排。这样做的目的很明确：工具能力不能绑死在某一家模型 SDK 上，本地执行也不能被模型输出直接穿透。

### 从纯聊天到单轮闭环 Agent

ch01 的数据流是线性的：用户输入之后，Provider 持续吐出文本，TUI 增量展示。ch02 变成了一个带分支的闭环：

```text
用户输入
  -> Session 构造 ChatRequest，首轮带 Tools
  -> Provider 把统一工具定义转换为厂商参数
  -> 模型流式返回文本或 tool call
  -> Provider 汇聚完整工具名、ID、JSON arguments
  -> Session 串行执行本地工具
  -> 工具返回结构化 Result，并应用路径、超时、字节数限制
  -> Session 把 assistant tool call + user tool result 写入历史
  -> Session 再次请求模型，但强制 Tools 为空
  -> 模型基于工具结果生成本轮最终回复
```

| 维度 | ch01：纯聊天 | ch02：单轮 Agent |
| --- | --- | --- |
| 模型输出 | 只处理文本和 thinking 增量 | 额外处理 `tool_call_start`、`tool_call_done`、`tool_result` |
| 本地副作用 | 无 | 文件读写、精确编辑、命令执行、搜索 |
| Provider 职责 | 流式文本适配 | 流式文本 + 工具协议适配 |
| Session 职责 | 保存历史、提交 assistant 文本 | 编排工具执行、回灌结果、阻断连续工具调用 |
| UI 策略 | 动态区域展示 assistant 文本 | 工具调用写入 scrollback，assistant 文本仍独立流式展示 |

### 统一工具层：把本地能力和模型厂商解耦

工具系统集中在 `internal/tool`。每个工具只暴露三类信息：名称、描述、JSON Schema 参数、执行入口。这个抽象故意不引用 OpenAI 或 Anthropic SDK 类型：

```go
type Tool interface {
    Definition() Definition
    Execute(ctx context.Context, req Request) Result
}
```

Provider 看到的是 `[]tool.Definition`，不是工具执行器。执行器只存在于 Session 所持有的 `tool.Registry` 中。

| 层级 | 允许知道什么 | 不允许知道什么 |
| --- | --- | --- |
| `internal/tool` | 工具名、参数 Schema、路径策略、执行限制、结构化结果 | OpenAI/Anthropic SDK 类型 |
| `internal/provider/openai` | 如何把 `Definition` 转成 Chat Completions tools，如何拼接 `delta.tool_calls` | 本地文件如何读取、命令如何执行 |
| `internal/provider/anthropic` | 如何把 `Definition` 转成 `ToolParam`，如何映射 `tool_use/tool_result` block | 工具注册表和本地副作用 |
| `internal/chat` | 何时暴露工具、何时执行工具、何时回灌结果 | 厂商 API 的原始事件结构 |

这个设计让新增工具的路径非常短：实现 `Tool`，注册到 `Registry`，Provider 自动拿到新的工具定义。反过来，替换或新增 Provider 时，只需要实现工具协议转换，不需要重写 Read/Edit/Bash 这类本地能力。

### 六个内置工具：能力、边界和 trick

ch02 内置的不是“万能本地执行器”，而是一组刻意收窄的开发工具。它们覆盖最常见的代码任务闭环：看文件、写文件、局部修改、跑命令、找文件、搜内容。

| 工具 | 解决的问题 | 参数 | 硬边界 |
| --- | --- | --- | --- |
| `Read` | 读取项目内文本文件 | `path`，可选 `offset`、`limit` | 只能读项目根目录内文件；拒绝目录；内容受 `MaxFileBytes` 和调用级 `limit` 控制 |
| `Write` | 创建或覆盖项目内文本文件 | `path`、`content` | 只能写项目根目录内路径；语义是完整覆盖，不提供 append 或 patch |
| `Edit` | 对已有文件做一次精确替换 | `path`、`old_string`、`new_string` | `old_string` 必须唯一匹配；匹配不到或匹配多次都不写文件 |
| `Bash` | 在项目目录执行命令 | `command`，可选 `timeout_ms` | 工作目录固定为项目目录；有超时；stdout/stderr 分别限流；超时清理进程树 |
| `Glob` | 按文件名或 glob-like 模式找文件 | `pattern` | 只遍历项目根目录；跳过 `.git`；最多返回 `MaxMatches` |
| `Grep` | 搜索项目文本内容 | `pattern`，可选 `path`、`regex` | 只搜项目内路径；跳过 `.git` 和二进制文件；最多返回 `MaxMatches`，单行片段限长 |

这些边界的共同点是：模型可以提出意图，但不能扩大执行域。路径、字节数、匹配次数、进程生命周期都由工具层收口。

#### Read：读内容，但不放任上下文膨胀

`Read` 做三层控制：

```text
path -> PathPolicy.Resolve -> os.Stat 拒绝目录 -> os.ReadFile
  -> offset/limit 裁剪
  -> LimitText(MaxFileBytes)
  -> Result{content, size, truncated, original_bytes, returned_bytes}
```

它的 trick 是稳定 head/tail 截断：不是简单砍掉尾部，而是保留文件开头和结尾，中间插入截断标记。这样模型既能看到上下文入口，也能看到末尾错误、日志尾巴或闭合结构。

#### Write：完整覆盖，而不是隐式 patch

`Write` 的语义非常硬：给完整 `content`，创建父目录，然后覆盖目标文件。它不提供“追加一行”“插入某段”“局部 patch”这类模糊动作，因为这些动作需要更强的冲突检测。需要局部修改时，应该走 `Edit`。

边界是 `PathPolicy.Resolve`：即使模型传入绝对路径或 `../../`，最终也必须落在项目根目录内，否则返回 `invalid_path`。

#### Edit：精准防误改

`Edit` 是 ch02 里最强调工程边界的工具。它不是“找到差不多的位置改一下”，而是：

```text
读取原文件
  -> old_string 精确匹配
  -> count == 1 才替换
  -> count == 0 尝试换行归一化匹配
  -> count > 1 直接拒绝
  -> 成功后一次性写回完整文件
```

这里有两个 trick：

| trick | 价值 |
| --- | --- |
| 唯一匹配 | 防止模型给出过短片段时误改多个位置 |
| 换行归一化 + 位置映射 | 允许模型用 `\n` 描述片段，同时保留原文件 CRLF/LF 风格 |

失败路径不会写文件，因此“匹配不到”“匹配多次”“归一化后仍无法映射”都不会产生半截修改。

#### Bash：能执行，但必须可回收

`Bash` 的目标不是模拟完整终端，而是给 Agent 一个受控的“跑测试/查状态”能力：

```text
command -> powershell/sh -c
  -> WorkingDir 固定为项目目录
  -> context.WithTimeout
  -> stdout/stderr limitedBuffer
  -> timeout 时清理进程组/进程树
  -> Result{exit_code, stdout, stderr, timeout}
```

关键 trick 有两个：

| trick | Unix | Windows |
| --- | --- | --- |
| 孤儿进程清理 | `Setpgid: true` 后 kill `-pid` | `CREATE_NEW_PROCESS_GROUP` 后 `taskkill /T /F /PID` |
| 输出限流 | stdout/stderr 写入 `limitedBuffer`，达到上限后继续吞输入但不继续增长内存 | 同左 |

也就是说，命令就算持续输出或超时卡住，也不能无限占内存，不能把子进程留在后台继续跑。

#### Glob：宽松匹配，但结果有上限

`Glob` 服务于“先找到可能相关的文件”。它会遍历项目目录，跳过 `.git`，并按相对路径排序返回。匹配策略比标准 glob 更宽松：

| 输入模式 | 匹配方式 |
| --- | --- |
| `*.go` | 可匹配文件名 |
| `internal/**/*.go` | 支持常见 `**/` 风格后缀处理 |
| `session` | 退化为相对路径 substring 查询 |

这个宽松策略是为了让模型不必第一次就生成完美 glob，但边界仍然明确：只在项目内找，最多返回 `MaxMatches`。

#### Grep：搜索内容，但避开二进制和刷屏

`Grep` 支持普通 substring 和可选正则：

```text
pattern + optional path
  -> PathPolicy.Resolve
  -> regex=true 时先 regexp.Compile
  -> 文件或目录遍历
  -> 跳过 .git
  -> looksBinary 检测 NUL 字节
  -> 返回 path + line + snippet
```

它的 trick 是“搜索结果不是原文复制”：每条命中只保留行号和 240 字节内的片段，再叠加 `MaxMatches`。这让模型能定位代码位置，但不会因为一个宽泛关键词把整个项目塞回上下文。

#### 统一结果兜底：进入模型历史前再截一次

工具内部会各自控制输出，但 Session 在回灌工具结果前还有最后一道 `MaxResultBytes` 闸门：

```text
tool.Result
  -> result.JSON()
  -> LimitText(MaxResultBytes)
  -> 若仍超限，包装成 truncated Result
  -> 写入 ToolResultMessage.Content
```

这层兜底很重要：即使某个工具未来忘记做局部截断，也不能把超大 JSON 直接塞进第二次模型请求。

### Provider 参数转换：同一套工具，两种协议形态

OpenAI-compatible 和 Anthropic 对工具的消息结构差异很大，但内部模型保持一致：

| 内部结构 | OpenAI-compatible | Anthropic |
| --- | --- | --- |
| `tool.Definition` | `ChatCompletionFunctionTool(FunctionDefinitionParam)` | `ToolParam{Name, Description, InputSchema}` |
| assistant 工具调用 | assistant message 中的 `tool_calls` | assistant message 中的 `tool_use` block |
| 工具结果 | 多条 role=`tool` message，带 `tool_call_id` | 一个 user message 内的多个 `tool_result` block |
| 参数格式 | 流式 `delta.tool_calls[index].function.arguments` 字符串片段 | SDK accumulate 后得到完整 `ToolUseBlock.Input` |

内部的 `ToolCall{ID, Name, Arguments}` 是协议收敛点。Session 不关心 arguments 是从 OpenAI 的若干 chunk 拼出来的，还是从 Anthropic 的 content block accumulate 出来的；它只接收完整 JSON 字符串，再交给工具层做 typed unmarshal 和语义校验。

### 流式 JSON 参数汇聚：难点在“完整性边界”

工具调用参数不是天然一次性到达。尤其 OpenAI-compatible 流式响应中，`arguments` 可能被拆成多个片段：

```text
chunk1: {"path":
chunk2: "internal/chat/session.go"
chunk3: }
```

MewCode 在 OpenAI adapter 内按 tool call `index` 做累积：

```text
delta.tool_calls[index]
  -> 补齐 call ID
  -> 补齐 function name
  -> 追加 arguments fragment
  -> 首次得到 ID 或 name 时发出 tool_call_start
  -> finish_reason == "tool_calls" 时发出 tool_call_done
```

这里的边界感很重要：Provider 可以拼接字符串，但不执行工具；工具层可以解析 JSON，但不理解模型协议；Session 可以决定何时执行，但不接触 SDK chunk。这样每一层都只承担自己能可靠测试的责任。

### 工程防御：本地文件和进程不能裸奔

让模型触达本地文件和命令执行，真正的风险不在“功能能不能跑”，而在失败时是否可控。ch02 的防御策略是多层叠加：

| 风险 | 防御点 | 行为 |
| --- | --- | --- |
| 越权读写项目外文件 | `PathPolicy.Resolve` | 统一清理路径、解析相对路径，拒绝 `..` 逃逸项目根目录 |
| Edit 误改多处 | `ReplaceUnique` | `old_string` 必须唯一匹配；0 次或多次匹配都拒绝写入 |
| Windows/Unix 换行差异导致误判 | `ReplaceUnique` 换行归一化 | 精确匹配失败后尝试换行归一化，并把替换内容转换回原文件换行风格 |
| 命令长时间不退出 | `context.WithTimeout` | 超时返回结构化 `ok=false`，错误码为 `timeout` |
| 命令产生孤儿子进程 | Bash 进程组/进程树清理 | Unix 下新建进程组并 kill `-pid`；Windows 下新建进程组并用 `taskkill /T /F` 清理子进程树 |
| stdout/stderr 无限输出 | `limitedBuffer` | stdout/stderr 分别有硬上限，超过后停止收集并标记截断 |
| 文件或搜索结果撑爆上下文 | `Limits` + `LimitText` | 保留 head/tail，中间插入截断标记，记录原始字节数和返回字节数 |

工具结果统一序列化为 JSON：

```json
{
  "tool": "Read",
  "call_id": "call_1",
  "ok": true,
  "summary": "Read 40960 bytes from internal/chat/session.go",
  "truncated": true,
  "original_bytes": 98304,
  "returned_bytes": 40960
}
```

这不是为了“好看”，而是为了让模型在失败时也能恢复：未知工具、非法参数、路径越界、匹配失败、命令超时，都以同形结构回灌，而不是让会话崩溃或把 Go error 直接散落到 UI。

### 单轮编排状态机：用空 Tools 拦住 Agent Loop

ch02 明确不做多轮自动 Agent Loop。实现上没有依赖提示词祈祷模型“不要再调用工具”，而是在第二次模型请求时强行置空 `Tools`：

```text
Submit(input)
  -> request #1: messages + Tools
  -> 模型返回 tool calls
  -> 执行工具并写入 tool results
  -> request #2: messages + tool results + Tools=nil
  -> 若仍出现 tool_call_done：返回阶段限制错误
```

| 状态 | 输入事件 | 动作 | 下一状态 |
| --- | --- | --- | --- |
| `StreamingFirstModel` | `text_delta` | 转发给 TUI | `StreamingFirstModel` |
| `StreamingFirstModel` | `tool_call_done` | 执行本轮所有工具，回灌结果 | `StreamingFinalModelNoTools` |
| `StreamingFinalModelNoTools` | `text_delta` | 转发最终回复 | `StreamingFinalModelNoTools` |
| `StreamingFinalModelNoTools` | `tool_call_done` | 直接报错：当前阶段不支持连续工具调用 | `Stopped` |
| 任意状态 | `error/cancelled/done` | 结束本轮 | `Idle/Error` |

这个选择很硬，但非常适合本阶段：一个用户回合最多一次工具调用回合，可以包含同一次模型响应中的多个 tool call，但执行仍是串行的。系统先获得可验证的闭环，再考虑更复杂的计划器、并行执行和多轮循环。

### UI 交互：工具行进入 scrollback，动态区只服务生成中回复

TUI 没有把工具执行塞进 assistant 文本块里，而是在工具调用开始时立即用 `tea.Println` 打印 Claude Code 风格行：

```text
● Read(internal/chat/session.go)
● Bash(go test ./...)
  ok: Command exited with code 0
```

| UI 元素 | 渲染策略 | 原因 |
| --- | --- | --- |
| 工具调用开始 | `● ToolName(args)` 写入 scrollback | 用户能看到模型正在触发什么本地动作 |
| 工具结果 | 缩略摘要写入 scrollback | 避免大 stdout、Read 内容或 Grep 结果刷屏 |
| assistant 文本增量 | 保留在当前动态区域 | 不和工具日志互相挤压 |
| 最终 assistant 回复 | 完成后 Markdown 渲染并打印 | 保持 ch01 的阅读体验 |

`toolCallPreview` 只展示关键参数：Read/Write/Edit 展示 path，Bash 展示 command，Glob/Grep 展示 pattern，其他工具退回到 JSON 单行预览。这个策略的重点不是“尽量多展示”，而是“让用户在 scrollback 里快速审计模型做过什么”。

## 核心类型

### Provider 接口

整个模型层只向上暴露一个接口：

```go
type Provider interface {
    StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
```

调用方传入上下文和 `ChatRequest`，拿到一个只读的 `StreamEvent` channel。上层不关心底层是 OpenAI-compatible API，还是 Anthropic Messages API，只消费统一事件即可。

这种设计把厂商差异关在 Provider 层：

| 协议 | 适配文件 | 对外表现 |
| --- | --- | --- |
| `openai` | `internal/provider/openai/stream.go` | 把 Chat Completions chunk 转成统一事件 |
| `anthropic` | `internal/provider/anthropic/stream.go` | 把 Messages stream event 转成统一事件 |

### StreamEvent：流式事件族

模型返回的流被统一成文本、thinking、工具、用量和结束状态等事件：

```go
const (
    StreamEventTypeTextDelta     StreamEventType = "text_delta"
    StreamEventTypeThinkingDelta StreamEventType = "thinking_delta"
    StreamEventTypeToolCallStart StreamEventType = "tool_call_start"
    StreamEventTypeToolCallDone  StreamEventType = "tool_call_done"
    StreamEventTypeToolResult    StreamEventType = "tool_result"
    StreamEventTypeUsage         StreamEventType = "usage"
    StreamEventTypeDone          StreamEventType = "done"
    StreamEventTypeCancelled     StreamEventType = "cancelled"
    StreamEventTypeError         StreamEventType = "error"
)
```

每个事件用同一个结构承载：

```go
type StreamEvent struct {
    Type       StreamEventType
    Delta      string
    Usage      Usage
    ErrorText  string
    ToolCall   *ToolCall
    ToolCalls  []ToolCall
    ToolResult *tool.Result
}
```

事件含义如下：

| 事件 | 含义 |
| --- | --- |
| `text_delta` | 普通回复文本增量 |
| `thinking_delta` | reasoning/thinking 增量 |
| `tool_call_start` | Provider 已识别到工具调用的 ID 或名称，TUI 可以立即打印工具行 |
| `tool_call_done` | Provider 已聚合出完整工具调用，Session 可以执行工具 |
| `tool_result` | Session 已完成工具执行，向 TUI 输出结构化结果摘要 |
| `usage` | token 用量统计 |
| `done` | 流正常结束 |
| `cancelled` | 用户取消或 context 取消 |
| `error` | 网络、鉴权、SDK 或流式读取错误 |

这里最重要的是 `text_delta` 和 `thinking_delta` 分离。DeepSeek 等 OpenAI-compatible 服务可能把推理内容放在 `reasoning_content`、`reasoning` 或 `reasoning_delta` 字段里；Anthropic 则可能返回 `ThinkingDelta`。MewCode 在 Provider 层把它们都规整成 `thinking_delta`。

### Message 与 ChatRequest

内部消息模型很小：

```go
type ChatMessage struct {
    Role        Role
    Content     string
    ToolCalls   []ToolCall
    ToolResults []ToolResultMessage
}

type ChatRequest struct {
    Messages []ChatMessage
    Model    string
    Thinking config.ThinkingConfig
    Tools    []tool.Definition
}
```

`Role` 只有三种：

```go
const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
    RoleSystem    Role = "system"
)
```

ch02 之后，内部消息模型开始显式表达工具调用和工具结果，但仍然刻意保持窄接口：`ToolCall` 只保存调用 ID、工具名和完整 JSON 参数，`ToolResultMessage` 只保存调用 ID、工具名和序列化后的 JSON 结果。多模态 block、长期记忆和跨进程会话持久化仍不进入本阶段。

### AppConfig 与 ProviderConfig

配置结构由两层组成：

```go
type AppConfig struct {
    Active    string
    Providers map[string]ProviderConfig
}

type ProviderConfig struct {
    Protocol string
    Model    string
    BaseURL  string
    APIKey   string
    Thinking ThinkingConfig
}
```

`protocol` 当前只支持：

| protocol | 说明 |
| --- | --- |
| `openai` | OpenAI 以及 DeepSeek、Kimi、Ark 等 OpenAI-compatible 服务 |
| `anthropic` | Anthropic Claude Messages API |

`ThinkingConfig` 用来控制 thinking 能力：

```go
type ThinkingConfig struct {
    Enabled       bool
    BudgetTokens  int
    ShowByDefault bool
}
```

对 Anthropic 来说，如果开启 thinking 且未配置 `budget_tokens`，Provider 会默认使用 `1024`。校验层也要求 Anthropic thinking budget 一旦设置，必须至少为 `1024`。

## 主流程走读

### 第一步：main 创建根命令

入口文件很薄：

```go
func main() {
    root := cli.NewRootCommand(app.New())
    if err := root.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

`main` 不直接知道配置、Provider、TUI 的细节，只负责创建命令并执行。

### 第二步：CLI 默认进入聊天

`internal/cli/root.go` 定义了一个很小的 `App` 接口：

```go
type App interface {
    RunChat(ctx context.Context) error
}
```

Cobra 根命令没有子命令时，直接调用：

```go
return app.RunChat(cmd.Context())
```

因此当前阶段 `mewcode` 的默认行为就是进入终端聊天界面。

### 第三步：App 装配运行依赖

`internal/app/app.go` 是装配中心：

```text
os.Getwd()
  -> config.LoadConfig(projectDir)
  -> cfg.ActiveProvider()
  -> provider.NewFactory().Create(...)
  -> tool.NewRegistry()
  -> builtin.RegisterDefaults(...)
  -> chat.NewSessionWithOptions(...)
  -> markdown.NewRenderer()
  -> tui.Run(...)
```

这一层负责把分散模块串起来，但不直接参与聊天逻辑。ch02 以后，它还会创建默认工具注册中心，并把当前项目目录作为 `WorkingDir` 和 `PathPolicy.Root` 注入 Session。它导入了：

```go
_ "mewcode/internal/provider/anthropic"
_ "mewcode/internal/provider/openai"
```

这两个空导入会触发各 Provider 包的 `init()`，把对应协议注册到 Provider 工厂里。工具注册则显式发生在 app 层：`Read`、`Write`、`Edit`、`Bash`、`Glob`、`Grep` 被登记到同一个 `Registry`，Provider 只接收它们的 definition，不能直接执行它们。

### 第四步：加载并合并配置

配置读取顺序是：

```text
~/.mewcode/config.yaml
  -> 当前项目的 mewcode.yaml
  -> merge
  -> Validate
```

项目配置覆盖全局配置。合并规则是字段级覆盖：

| 字段 | 行为 |
| --- | --- |
| `active` | 项目配置非空时覆盖全局 |
| `protocol` | 非空时覆盖 |
| `model` | 非空时覆盖 |
| `base_url` | 非空时覆盖 |
| `api_key` | 非空时覆盖 |
| `thinking.enabled` | 只能从 false 覆盖为 true |
| `thinking.budget_tokens` | 非 0 时覆盖 |
| `thinking.show_by_default` | 只能从 false 覆盖为 true |

配置文件读取后会先做环境变量展开：

```go
expanded := os.ExpandEnv(string(data))
```

所以可以在配置里写：

```yaml
api_key: ${OPENAI_API_KEY}
```

### 第五步：Factory 创建 Provider

Provider 注册表是一个 map：

```go
var constructors = map[string]Constructor{}
```

OpenAI adapter 注册：

```go
provider.Register(config.ProtocolOpenAI, func(name string, cfg config.ProviderConfig) provider.Provider {
    return NewProvider(name, cfg)
})
```

Anthropic adapter 注册：

```go
provider.Register(config.ProtocolAnthropic, func(name string, cfg config.ProviderConfig) provider.Provider {
    return NewProvider(name, cfg)
})
```

创建时按 `cfg.Protocol` 查表：

```go
constructor, ok := constructors[cfg.Protocol]
if !ok {
    return nil, fmt.Errorf("provider %q protocol %q is not supported", name, cfg.Protocol)
}
return constructor(name, cfg), nil
```

这意味着新协议的接入方式很清楚：新增一个 adapter 包，在 `init()` 中注册协议，并实现 `StreamChat`。

### 第六步：Session 构建请求

用户提交输入后，`chat.Session.Submit` 做四件事：

```text
trim input
  -> 追加 user 消息到 History
  -> 用 History + model + thinking 创建 ChatRequest
  -> 保存 LastRequest
  -> 调用 provider.StreamChat
```

对应代码形态：

```go
s.History = append(s.History, provider.ChatMessage{
    Role: provider.RoleUser,
    Content: input,
})

req := provider.ChatRequest{
    Messages: append([]provider.ChatMessage(nil), s.History...),
    Model:    s.cfg.Model,
    Thinking: s.cfg.Thinking,
}

s.LastRequest = &req
return s.provider.StreamChat(ctx, req)
```

`Retry` 不会重新拼输入，而是复制上一次 `LastRequest` 再请求一次：

```go
req := *s.LastRequest
req.Messages = append([]provider.ChatMessage(nil), s.LastRequest.Messages...)
return s.provider.StreamChat(ctx, req)
```

当模型正常完成后，TUI 会调用：

```go
s.CommitAssistant(content)
```

把 assistant 回复追加到 `History`，下一轮请求就自然带上多轮上下文。

## 两层消息模型

### 内层：Provider 消息

内层消息是对模型请求友好的结构：

```text
[]provider.ChatMessage
  -> RoleUser / RoleAssistant / RoleSystem
  -> Content string
```

它服务于 `ChatRequest`，最终会被具体 Provider 转成 SDK 所需格式。

### 外层：TUI 当前消息

TUI 不保存完整聊天历史，只保存当前正在生成的消息：

```go
type UIMessage struct {
    ID        string
    Role      provider.Role
    Content   string
    Thinking  string
    ErrorText string
    Usage     provider.Usage
    Status    MessageStatus
}
```

完成的用户输入、assistant 回复和错误会通过 `tea.Println(...)` 打印到终端原生 scrollback。TUI model 只负责当前动态区域：

```text
thinking 区域
reply 区域
error 区域
textarea 输入框
status bar 状态栏
```

这种设计让状态机保持很小，也避免在每次 token 到来时重绘一整段历史消息列表。

## 流式响应处理

### 生产者-消费者模型

Provider 是生产者，TUI 是消费者：

```text
Provider.StreamChat
  -> goroutine 读取 SDK stream
  -> 转换成 provider.StreamEvent
  -> 写入 events channel

TUI waitForEvent
  -> 从 events channel 读一个事件
  -> 包装成 streamMsg
  -> 交给 Model.Update
```

关键桥接函数是：

```go
func waitForEvent(ch <-chan provider.StreamEvent) tea.Cmd {
    return func() tea.Msg {
        event, ok := <-ch
        if !ok {
            return streamMsg{Type: provider.StreamEventTypeDone}
        }
        return streamMsg(event)
    }
}
```

Bubble Tea 的 `Update` 每处理完一个流式事件，如果流还没结束，就继续返回 `waitForEvent(m.events)`，形成连续消费。

### Provider 流：ch01 基础能力，ch02 只补工具分支

ch01 已经讲过 OpenAI-compatible 和 Anthropic 如何把 `text_delta`、`thinking_delta`、`usage`、`done` 统一成内部事件。ch02 文档里不需要重复展开 SDK 基础流式读取，真正新增的是工具分支：

| Provider | ch01 已有 | ch02 新增 |
| --- | --- | --- |
| OpenAI-compatible | `content`、reasoning 私有字段、usage | 发送 `tools`；按 `delta.tool_calls[index]` 拼接 ID、name、arguments；`finish_reason=tool_calls` 时发出 `tool_call_done` |
| Anthropic | `TextDelta`、`ThinkingDelta`、usage | 发送 `ToolParam`；识别 `tool_use` block；把内部工具结果转成 `tool_result` block |

OpenAI-compatible 的关键点是“分片拼接”：

```text
stream chunk
  -> convertChunk 处理 text/thinking/usage
  -> convertChunkWithTools 处理 delta.tool_calls
  -> toolCallAccumulator[index].Arguments += fragment
  -> finish_reason == "tool_calls"
  -> StreamEventTypeToolCallDone
```

Anthropic 的关键点是“block 语义映射”：

```text
Message stream event
  -> message.Accumulate(event)
  -> ContentBlockStartEvent(tool_use) 发 tool_call_start
  -> stream 结束后从完整 message 提取 tool_use
  -> StreamEventTypeToolCallDone
```

所以基础 text/thinking 流式说明不是无价值，但它属于 ch01 的背景；ch02 的主线应该是工具定义如何下发、工具参数如何聚合、工具结果如何回灌。

### 错误、取消与完成

两个 Provider 都把 context 取消统一成：

```go
StreamEventTypeCancelled
```

其他 SDK 或网络错误统一成：

```go
StreamEventTypeError
```

正常流结束时发送：

```go
StreamEventTypeDone
```

因此 TUI 只需要处理统一语义，不需要区分底层厂商。

## TUI 状态机

### Model 的关键字段

`internal/tui/model.go` 中的 `Model` 保存了终端界面的全部动态状态：

```go
type Model struct {
    Config      config.AppConfig
    Active      string
    ProviderCfg config.ProviderConfig
    Runner      ChatRunner
    Renderer    MarkdownRenderer

    textarea     textarea.Model
    Current      UIMessage
    Width        int
    Height       int
    Status       RunStatus
    ShowThinking bool
    Usage        provider.Usage
    LastError    string
    StreamCancel context.CancelFunc
    events       <-chan provider.StreamEvent
}
```

运行状态只有三种：

```go
const (
    StatusIdle      RunStatus = "idle"
    StatusStreaming RunStatus = "streaming"
    StatusError     RunStatus = "error"
)
```

### 按键处理

核心快捷键如下：

| 操作 | 快捷键 |
| --- | --- |
| 提交输入 | `Enter` |
| 切换 thinking 显示 | `Ctrl+T` |
| 重试上一轮请求 | `Ctrl+R` |
| 流式生成中取消 | `Ctrl+C` |
| 空闲状态退出 | `Ctrl+C` |
| 输入命令退出 | `/exit` 后回车 |

按键分发在 `handleKey`：

```text
Ctrl+C
  -> streaming 时取消当前请求
  -> idle/error 时退出程序

Ctrl+T
  -> 切换 ShowThinking

Ctrl+R
  -> 非 streaming 时调用 startRetry

Enter
  -> 非 streaming 时调用 startSubmit
  -> 输入 /exit 时退出
```

### 提交流程

用户按 `Enter` 后，TUI 会：

```text
读取 textarea
  -> 创建可取消 context
  -> Runner.Submit(ctx, input)
  -> 保存 cancel 和 events channel
  -> 设置 Current 为 streaming assistant 消息
  -> 清空输入框
  -> 打印用户输入块
  -> 开始 waitForEvent
```

对应状态变化：

```text
idle/error
  -> streaming
  -> done/cancelled/error
  -> idle 或 error
```

### 事件消费

`handleStream` 是 TUI 的核心消费逻辑：

| 事件 | TUI 行为 |
| --- | --- |
| `text_delta` | 追加到 `Current.Content` |
| `thinking_delta` | 追加到 `Current.Thinking` |
| `usage` | 更新状态栏 token 统计 |
| `error` | 标记错误状态，打印错误块 |
| `cancelled` | 回到 idle，打印当前已生成内容 |
| `done` | 提交 assistant 历史，Markdown 渲染后打印 |

正常完成时有一个重要步骤：

```go
if strings.TrimSpace(content) != "" {
    m.Runner.CommitAssistant(content)
}
```

只有完成后的 assistant 内容才会进入会话历史。流式中途展示的内容只是 UI 当前状态。

## Markdown 渲染策略

MewCode 不在流式阶段实时渲染 Markdown，而是在 `done` 后渲染完整回复：

```go
if out, err := m.Renderer.Render(content, m.Width); err == nil {
    rendered = out
}
```

渲染器使用 Glamour：

```go
renderer, err := glamour.NewTermRenderer(
    glamour.WithStandardStyle("dark"),
    glamour.WithWordWrap(width),
)
```

这样做有两个好处：

1. 流式输出期间界面稳定，不会因为半截 Markdown 语法导致抖动。
2. 完整回复落到终端 scrollback 时，代码块、列表和换行会更接近最终阅读效果。

## 配置示例

项目配置文件是当前目录下的 `mewcode.yaml`。仓库提供了 `mewcode.yaml.example`：

```yaml
active: openai
providers:
  openai:
    protocol: openai
    model: gpt-4o
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_API_KEY}

  claude:
    protocol: anthropic
    model: claude-sonnet-4-5
    base_url: https://api.anthropic.com
    api_key: ${ANTHROPIC_API_KEY}
    thinking:
      enabled: true
      budget_tokens: 1024
      show_by_default: false

  deepseek:
    protocol: openai
    model: deepseek-chat
    base_url: https://api.deepseek.com
    api_key: ${DEEPSEEK_API_KEY}
    thinking:
      enabled: true
      show_by_default: false
```

OpenAI、DeepSeek、Kimi、Ark 都走 `openai` 协议，只是 `base_url`、`model` 和 `api_key` 不同。Claude 走 `anthropic` 协议。

## 测试覆盖

测试文件分布如下：

| 测试文件 | 覆盖内容 |
| --- | --- |
| `internal/config/loader_test.go` | 全局/项目配置合并、环境变量展开、校验错误不泄漏 API key |
| `internal/provider/factory_test.go` | Provider factory 按协议分发 |
| `internal/provider/openai/stream_test.go` | OpenAI chunk 到 text/reasoning/usage 事件的转换 |
| `internal/provider/anthropic/stream_test.go` | Anthropic event 到 text/thinking/usage 事件的转换 |
| `internal/chat/session_test.go` | History、LastRequest、Retry 语义 |
| `internal/tui/tui_test.go` | 初始视图、流式更新、thinking 切换、usage、done、retry、cancel |
| `internal/markdown/renderer_test.go` | Markdown 渲染基本行为 |
| `internal/e2e/chat_test.go` | fake provider 下的端到端聊天链路 |

运行全部测试：

```powershell
go test ./...
```

## 模型解析与能力探测

当前项目没有单独的模型解析器文件，也没有像 `model_resolver.go` 那样把模型别名解析成完整 ID。模型名来自配置：

```yaml
providers:
  openai:
    model: gpt-4o
```

Provider 层直接把这个字符串传给对应 SDK：

```go
Model: openaisdk.ChatModel(req.Model)
```

或：

```go
Model: anthropicsdk.Model(req.Model)
```

能力探测目前也不是运行时自动发现，而是由配置显式表达：

| 能力 | 来源 |
| --- | --- |
| 是否启用 thinking | `providers.<name>.thinking.enabled` |
| thinking token 预算 | `providers.<name>.thinking.budget_tokens` |
| 默认是否显示 thinking | `providers.<name>.thinking.show_by_default` |
| 是否是 OpenAI-compatible | `providers.<name>.protocol: openai` |
| 是否是 Anthropic | `providers.<name>.protocol: anthropic` |

这让项目继续保持显式配置优先：不做模型元数据查询，不维护模型能力矩阵，而是把协议适配、工具定义转换和流式事件统一先跑通。

## 当前边界

当前代码已经允许模型触发受控本地工具，但边界仍然收得很紧：

- 每个用户回合最多一次工具调用回合
- 不做连续自动 Agent Loop
- 不做并行工具执行
- 不做模型自主扩大工作目录或访问项目根目录外路径
- 不把未截断的大文件、命令输出或搜索结果直接塞进对话历史
- 不生成或应用代码补丁
- 不持久化长期会话历史
- 不做 IDE 集成
- 不做多用户或服务端部署

这些不是代码结构遗漏，而是阶段性边界。这个仓库当前最值得阅读的是：如何把配置、模型 Provider、流式工具协议、本地工具执行、进程内会话和 TUI scrollback 组织成一个可恢复、可测试、可审计的单轮闭环。

## 小结

MewCode 的核心设计可以概括为三句话：

1. 用 `Provider` 接口隔离厂商 API 差异。
2. 用 `internal/tool` 把本地文件、搜索和命令执行收敛成 Provider 无关的受控能力。
3. 用 `StreamEvent` 和 Session 状态机把文本、thinking、tool call、tool result、完成、取消和错误统一成一组可编排事件。

如果按源码阅读顺序，建议从这里开始：

```text
cmd/mewcode/main.go
  -> internal/app/app.go
  -> internal/config/loader.go
  -> internal/tool/tool.go
  -> internal/tool/limits.go
  -> internal/tool/builtin/edit.go
  -> internal/tool/builtin/bash.go
  -> internal/provider/event.go
  -> internal/provider/openai/tool.go
  -> internal/provider/openai/stream.go
  -> internal/provider/anthropic/tool.go
  -> internal/provider/anthropic/stream.go
  -> internal/chat/session.go
  -> internal/tui/tool.go
  -> internal/tui/update.go
  -> internal/tui/view.go
```

这条路径基本覆盖了从启动到流式回复落屏的完整主流程。
