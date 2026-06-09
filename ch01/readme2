# Go源码解析：MewCode 终端 AI 对话客户端

## 模块概览

MewCode 是一个用 Go 编写的终端 AI 对话客户端实验项目。当前阶段先不做文件修改、命令执行、工具调用和 IDE 集成，而是专注打通一条最小但完整的链路：

```text
用户在终端输入问题
  -> 读取配置
  -> 创建模型 Provider
  -> 维护进程内多轮会话
  -> 接收模型流式事件
  -> 在 Bubble Tea TUI 中展示增量回复
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
| `internal/provider/anthropic/client.go` | Anthropic 客户端创建与注册 |
| `internal/provider/anthropic/stream.go` | Anthropic Messages 流式事件适配 |
| `internal/chat/session.go` | 多轮历史、上次请求、重试和 assistant 消息提交 |
| `internal/tui/model.go` | Bubble Tea Model、UI 状态和依赖接口 |
| `internal/tui/update.go` | 按键处理、提交、重试、取消和流式事件消费 |
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
  -> internal/chat       当前进程内多轮会话
  -> internal/tui        Bubble Tea 终端交互界面
  -> internal/markdown   Markdown 渲染
```

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

模型返回的流被统一成六类事件：

```go
const (
    StreamEventTypeTextDelta     StreamEventType = "text_delta"
    StreamEventTypeThinkingDelta StreamEventType = "thinking_delta"
    StreamEventTypeUsage         StreamEventType = "usage"
    StreamEventTypeDone          StreamEventType = "done"
    StreamEventTypeCancelled     StreamEventType = "cancelled"
    StreamEventTypeError         StreamEventType = "error"
)
```

每个事件用同一个结构承载：

```go
type StreamEvent struct {
    Type      StreamEventType
    Delta     string
    Usage     Usage
    ErrorText string
}
```

事件含义如下：

| 事件 | 含义 |
| --- | --- |
| `text_delta` | 普通回复文本增量 |
| `thinking_delta` | reasoning/thinking 增量 |
| `usage` | token 用量统计 |
| `done` | 流正常结束 |
| `cancelled` | 用户取消或 context 取消 |
| `error` | 网络、鉴权、SDK 或流式读取错误 |

这里最重要的是 `text_delta` 和 `thinking_delta` 分离。DeepSeek 等 OpenAI-compatible 服务可能把推理内容放在 `reasoning_content`、`reasoning` 或 `reasoning_delta` 字段里；Anthropic 则可能返回 `ThinkingDelta`。MewCode 在 Provider 层把它们都规整成 `thinking_delta`。

### Message 与 ChatRequest

内部消息模型很小：

```go
type ChatMessage struct {
    Role    Role
    Content string
}

type ChatRequest struct {
    Messages []ChatMessage
    Model    string
    Thinking config.ThinkingConfig
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

当前项目没有复杂的 tool message、function call message 或多模态 block。这样做是刻意收窄范围：先把文本对话和流式响应链路做扎实。

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
  -> chat.NewSession(...)
  -> markdown.NewRenderer()
  -> tui.Run(...)
```

这一层负责把分散模块串起来，但不直接参与聊天逻辑。它导入了：

```go
_ "mewcode/internal/provider/anthropic"
_ "mewcode/internal/provider/openai"
```

这两个空导入会触发各 Provider 包的 `init()`，把对应协议注册到 Provider 工厂里。

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

### OpenAI-compatible 流

OpenAI adapter 使用官方 SDK：

```go
stream := p.client.Chat.Completions.NewStreaming(ctx, params)
for stream.Next() {
    chunk := stream.Current()
    for _, event := range convertChunk(chunk) {
        events <- event
    }
}
```

它会处理三类信息：

| SDK chunk 内容 | 内部事件 |
| --- | --- |
| `usage.prompt_tokens` 等 | `usage` |
| `choices[0].delta.content` | `text_delta` |
| `reasoning_content` / `reasoning` / `reasoning_delta` | `thinking_delta` |

reasoning 字段通过 `delta.RawJSON()` 再解析，兼容不同 OpenAI-compatible 厂商的私有字段。

### Anthropic 流

Anthropic adapter 同样使用官方 SDK：

```go
stream := p.client.Messages.NewStreaming(ctx, params)
for stream.Next() {
    event := stream.Current()
    for _, converted := range convertEvent(event) {
        events <- converted
    }
}
```

事件映射如下：

| Anthropic event | 内部事件 |
| --- | --- |
| `TextDelta` | `text_delta` |
| `ThinkingDelta` | `thinking_delta` |
| `MessageDeltaEvent.Usage` | `usage` |

如果配置开启 thinking，则请求参数会增加：

```go
params.Thinking = anthropicsdk.ThinkingConfigParamUnion{
    OfEnabled: &anthropicsdk.ThinkingConfigEnabledParam{
        BudgetTokens: int64(budget),
    },
}
```

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

这让项目第一阶段保持简单：不做模型元数据查询，不维护模型能力矩阵，而是把协议适配和流式事件统一先跑通。

## 当前边界

当前代码明确聚焦在纯对话 TUI，暂时不包含：

- 不读取、搜索或修改本地项目文件
- 不执行 shell 命令
- 不做 tool use
- 不生成或应用代码补丁
- 不持久化长期会话历史
- 不做 IDE 集成
- 不做多用户或服务端部署

这些不是代码结构遗漏，而是阶段性边界。这个仓库当前最值得阅读的是：如何把配置、模型 Provider、流式事件、进程内会话和 TUI 状态机组织成一条清楚的最小链路。

## 小结

MewCode 的核心设计可以概括为三句话：

1. 用 `Provider` 接口隔离厂商 API 差异。
2. 用 `StreamEvent` 把文本、thinking、usage、完成、取消和错误统一成一组事件。
3. 用 Bubble Tea TUI 消费事件流，只维护当前动态消息，把完成内容交给终端 scrollback。

如果按源码阅读顺序，建议从这里开始：

```text
cmd/mewcode/main.go
  -> internal/app/app.go
  -> internal/config/loader.go
  -> internal/provider/event.go
  -> internal/provider/openai/stream.go
  -> internal/provider/anthropic/stream.go
  -> internal/chat/session.go
  -> internal/tui/update.go
  -> internal/tui/view.go
```

这条路径基本覆盖了从启动到流式回复落屏的完整主流程。
