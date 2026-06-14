# Go源码解析：MewCode ch05 MCP Client 与延迟工具发现

## 模块概览

ch05 的核心目标是让 MewCode 从“只能使用内置工具”演进到“启动时自动发现外部 MCP Server 的工具，并把它们接进同一个工具中心”。上一阶段 ch04 已经把所有本地工具调用接入权限裁决链路；这一阶段不重写 Agent Loop，也不改 Provider 协议，而是在工具注册层和工具执行层之间插入一个 MCP Client 适配层。

演进关系可以这样看：

```text
ch03:
model tool call
  -> Registry.Get(name)
  -> Tool.Execute
  -> tool.Result
  -> messages += ToolResultMessage

ch04:
model tool call
  -> Permission Manager
  -> allow: Tool.Execute
  -> deny: tool.Failure(permission_denied)
  -> messages += ToolResultMessage

ch05:
config.mcp.servers
  -> MCP Manager initializes servers
  -> tools/list discovers remote tools
  -> Registry.Register(MCP ToolWrapper)
  -> ToolSearch reveals schema lazily
  -> model calls mcp__server__tool
  -> ToolWrapper sends tools/call
  -> tool.Result returns to Agent Loop
```

这里有两个关键约束：

1. MCP 工具必须像普通 `tool.Tool` 一样被 Agent 使用，Runner 不应该知道某个工具来自本地还是远端。
2. 外部 Server 可能暴露大量工具，不能把全部 schema 在首轮塞进模型上下文，所以 MCP 工具默认延迟暴露。

最终形成的是一条双阶段管道：

```text
启动期:
  config -> MCP Client initialize -> tools/list -> Registry 注册延迟工具

运行期:
  system-reminder 列出延迟工具名
  -> ToolSearch(name)
  -> Registry.MarkDiscovered(name)
  -> 下一轮 Provider tools 包含完整 schema
  -> model 正常调用该工具
```

## 代码落点

ch05 的主要代码集中在 `internal/mcp`、工具 Registry 延迟状态、ToolSearch、配置合并和 Agent system-reminder 接入。Provider、TUI、权限管道的主体逻辑没有被重写。

| 文件 | ch05 职责 |
| --- | --- |
| `internal/config/config.go` | 定义 `MCPConfig` / `MCPServerConfig`，校验 stdio 与 HTTP Server 配置 |
| `internal/config/merge.go` | 合并 `mcp.servers`，项目级同名 Server 完整覆盖用户级 |
| `internal/config/loader.go` | 保留 local provider/permissions 合并，但恢复 user/project 的 MCP Server 列表 |
| `internal/mcp/types.go` | 定义 JSON-RPC、initialize、tools/list、tools/call、MCP Tool 等协议数据结构 |
| `internal/mcp/rpc.go` | `RPCSession`：生成 id、维护 pending map、按 id 分发响应 |
| `internal/mcp/stdio.go` | stdio transport：启动子进程，用 stdin/stdout 交换 JSON-RPC |
| `internal/mcp/http.go` | Streamable HTTP transport：POST JSON-RPC，支持 JSON 和 POST 返回 SSE |
| `internal/mcp/client.go` | MCP 语义客户端：`initialize`、`notifications/initialized`、`tools/list`、`tools/call` |
| `internal/mcp/tool.go` | `ToolWrapper`：把远端 MCP Tool 适配成 MewCode `tool.Tool` |
| `internal/mcp/manager.go` | 多 Server 并发初始化、工具注册、连接缓存和生命周期关闭 |
| `internal/tool/registry.go` | Registry 维护延迟工具发现状态，区分全部 definitions 与可见 definitions |
| `internal/tool/builtin/tool_search.go` | `ToolSearch`：模型按名称拉取延迟工具完整 schema |
| `internal/agent/tools.go` | Agent 每轮只导出可见工具 schema，并收集尚未发现的延迟工具名 |
| `internal/prompt/notes.go` | system-reminder 展示 `Available tools` 与 `Searchable deferred tools` |
| `internal/app/app.go` | 启动时注册内置工具、ToolSearch，再启动 MCP Manager |
| `mewcode.yaml.example` | 展示 stdio 和 Streamable HTTP MCP Server 配置 |

最关键的数据交叉点是 Registry：

```text
内置工具
  -> Registry.Register(Read/Write/Edit/Bash/Glob/Grep/ToolSearch)

MCP tools/list
  -> ToolWrapper{RegisteredName, RemoteName, Client, RemoteTool}
  -> Registry.Register(wrapper)

Agent 每轮
  -> Registry.VisibleDefinitions()
  -> Registry.DeferredNames()
```

这让 MCP 只是一种新的工具来源，而不是一套新的 Agent 执行协议。

## 本阶段解决的问题

只把 MCP Server “连上”还不够。真正的问题有四个：

| 问题 | 典型风险 | ch05 的处理 |
| --- | --- | --- |
| 外部工具接入 | 每个 Server 协议、传输、工具 schema 不同 | 用 MCP Client 标准化 initialize / tools/list / tools/call |
| 大量 schema 注入 | Server 暴露几十个工具，首轮上下文被工具 schema 塞满 | MCP 工具默认延迟，只在 system-reminder 里列名字 |
| 并发工具调用 | 同一 Server 的多个 `tools/call` 乱序返回 | `RPCSession.pending` 按 id 配对，map 由 mutex 保护 |
| Server 故障隔离 | 一个远端超时拖慢启动，或初始化失败影响其他 Server | Manager 并发初始化，单 Server 失败只记诊断 |

ch05 的主线不是“多支持一种工具”，而是把远端能力纳入现有 Agent Runtime，同时控制上下文膨胀、生命周期和失败边界。

## 为什么不是启动后全量注册 schema

最直接的做法是：`tools/list` 拿到所有工具后，全部放进 Provider 的 tools 列表。这个方案简单，但对 Agent 很不友好。

```text
Server A: 20 tools
Server B: 35 tools
Server C: 15 tools
  -> 首轮 model request 携带 70 个完整 JSON Schema
  -> token 增长
  -> 工具选择噪音变大
  -> 与真正相关的本地工具互相干扰
```

ch05 选择了“先名字、后 schema”的延迟发现：

```text
首轮:
  Available tools:
    Read, Grep, ToolSearch, ...
  Searchable deferred tools:
    mcp__docs__search, mcp__repo__query

模型判断需要 docs search:
  ToolSearch({"name":"mcp__docs__search"})

下一轮:
  Available tools:
    Read, Grep, ToolSearch, mcp__docs__search
```

这不是懒加载连接。Server 仍在启动期初始化并完成 `tools/list`，所以 MewCode 已经知道工具存在。延迟的是“完整 schema 是否暴露给模型”。

## 核心抽象

ch05 的关键抽象分成三组：配置、协议会话、工具可见性。

| Type | File | Role | Critical fields | Produced by | Consumed by | Lifecycle / transformation |
| --- | --- | --- | --- | --- | --- | --- |
| `config.MCPConfig` | `internal/config/config.go` | MCP Server 配置入口 | `Servers map[string]MCPServerConfig` | YAML loader | `mcp.Manager` | user/project 合并后进入 AppConfig；local 层不会改变它 |
| `config.MCPServerConfig` | `internal/config/config.go` | 单个 Server 连接配置 | `Type`、`Command`、`Args`、`Env`、`URL`、`Headers` | YAML loader | `Manager.createClient` | 校验后转换为 stdio 或 HTTP transport |
| `mcp.JSONRPCMessage` | `internal/mcp/types.go` | JSON-RPC 2.0 通用消息 | `ID`、`Method`、`Params`、`Result`、`Error` | RPCSession / Transport | RPCSession / Client | 请求、响应、通知共用同一结构 |
| `mcp.RPCSession` | `internal/mcp/rpc.go` | 请求-响应配对状态机 | `nextID`、`pending`、`mu`、`transport` | `Client.Initialize` | `Client.ListTools` / `CallTool` | Call 注册 pending；Run 读响应并按 id 投递；Close 清理 pending |
| `mcp.Transport` | `internal/mcp/types.go` | 传输抽象 | `Start`、`Send`、`Receive`、`Close` | stdio/http 实现 | `RPCSession` | 隐藏子进程和 HTTP 差异，向上只暴露 JSON-RPC 消息 |
| `mcp.Client` | `internal/mcp/client.go` | MCP 语义客户端 | `ServerName`、`Transport`、`Session`、`ServerInfo` | `Manager.createClient` | `Manager`、`ToolWrapper` | 初始化后缓存会话，后续工具调用复用 |
| `mcp.Tool` | `internal/mcp/types.go` | 远端工具描述 | `Name`、`Description`、`InputSchema`、`ServerName` | `Client.ListTools` | `ToolWrapper` | 从 MCP schema 转成 MewCode `tool.Definition` |
| `mcp.ToolWrapper` | `internal/mcp/tool.go` | 远端工具适配器 | `RegisteredName`、`RemoteName`、`Client`、`RemoteTool` | `Manager.Start` | Registry / ToolExecutor | 默认 `ShouldDefer=true`，执行时转发 `tools/call` |
| `mcp.Manager` | `internal/mcp/manager.go` | 多 Server 生命周期管理 | `Config`、`Registry`、`clients`、`diagnostics`、`Timeout` | App 启动 | App / ToolWrapper | 并发初始化 Server，注册工具，退出时关闭连接 |
| `tool.DeferredTool` | `internal/tool/tool.go` | 可选延迟标记接口 | `ShouldDefer()` | ToolWrapper | Registry | 不改原 `Tool` 接口，延迟能力作为可选扩展 |
| `tool.Registry` | `internal/tool/registry.go` | 工具注册和可见性状态 | `tools`、`discovered` | App / Manager / ToolSearch | Agent / ToolExecutor | `Definitions()` 全量；`VisibleDefinitions()` 给 Provider；`MarkDiscovered()` 改变会话可见性 |
| `builtin.ToolSearch` | `internal/tool/builtin/tool_search.go` | 延迟工具发现工具 | `Registry`、参数 `name` | `RegisterDefaults` | 模型调用 | 返回完整 definition，并把工具标记为已发现 |
| `prompt.DynamicContext` | `internal/prompt/notes.go` | 每轮 system-reminder 输入 | `ToolSummary`、`DeferredToolNames` | Runner | prompt notes | 把可直接调用和可搜索工具分开展示 |

### MCPServerConfig：配置是连接单元

`MCPServerConfig` 同时覆盖 stdio 和 HTTP：

```go
type MCPServerConfig struct {
    Type    string
    Command string
    Args    []string
    Env     map[string]string
    URL     string
    Headers map[string]string
}
```

校验规则刻意较硬：

| type | 必填 | 禁止 |
| --- | --- | --- |
| `stdio` | `command` | `url` |
| `http` | `url` | `command`、`args` |

合并也不是字段级 merge，而是同名 Server 完整覆盖。原因是 Server 配置代表一个连接单元，如果项目级只改 `url` 却意外继承用户级 headers，可能连到错误环境。

### RPCSession：把异步协议变成同步 Call

MCP 基于 JSON-RPC。请求带 id，响应可能乱序回来。`RPCSession` 把这种异步协议收束成：

```go
Call(ctx, "tools/list", params, &result)
```

内部状态是：

```go
type RPCSession struct {
    transport Transport
    mu        sync.Mutex
    nextID    int64
    pending   map[string]chan JSONRPCMessage
    closed    bool
}
```

`mu` 是这里最重要的字段。模型可以并行产生多个工具调用，底层 `Run` goroutine 也在持续读响应。没有锁保护，Go 的 map 会在并发读写时直接崩溃。

### ToolWrapper：远端工具的本地形态

MCP 工具进 Registry 后长这样：

```text
RegisteredName = "mcp__docs__search"
RemoteName     = "search"
ServerName     = "docs"
Client         = *mcp.Client
RemoteTool     = mcp.Tool{Name, Description, InputSchema}
```

对 Provider 暴露的是 `RegisteredName`，避免与内置工具和其他 Server 冲突；对远端 Server 调用时仍使用 `RemoteName`。

```text
model calls mcp__docs__search
  -> ToolWrapper.Execute
  -> Client.CallTool("search", arguments)
  -> JSON-RPC method tools/call
```

## 数据流

### 配置到工具注册

```text
~/.mewcode/config.yaml
  -> AppConfig.MCP.Servers
  -> mergeConfig(base, global)

<project>/mewcode.yaml
  -> AppConfig.MCP.Servers
  -> mergeConfig(global, project)
       same server name: project replaces global

<project>/mewcode.local.yaml
  -> provider / permissions still merge
  -> MCP.Servers restored to user+project snapshot

App.RunChat
  -> builtin.RegisterDefaults(registry)
       Read / Write / Edit / Bash / Glob / Grep / ToolSearch
  -> mcp.Manager.Start(cfg.MCP, registry)
       initialize each server
       tools/list
       Registry.Register(ToolWrapper)
```

这个流程说明了一个边界：`mewcode.local.yaml` 可以继续承载本地权限授权，但不会悄悄接入新的外部 MCP Server。

### JSON-RPC 请求配对

```text
Client.ListTools
  -> RPCSession.Call(method="tools/list")
       lock:
         nextID++
         pending[id] = response channel
       Transport.Send(JSONRPCMessage{ID, Method, Params})

Transport.Receive
  -> RPCSession.Run
       msg.ID -> formatID
       lock:
         ch = pending[id]
       ch <- response

Call waiting goroutine
  -> <-ch
  -> if Error: return JSONRPCError
  -> json.Unmarshal(Result, &ListToolsResult)
  -> lock:
       delete(pending, id)
```

这里 `formatID` 处理了一个实际问题：JSON 解码后数字 id 可能变成 `float64`，而本地生成的是 `int64`。统一转成字符串 key 后，响应配对不受 JSON 数字类型影响。

### 延迟工具发现

```text
Registry state:
  tools = {
    "Read": builtin.ReadTool,
    "ToolSearch": builtin.ToolSearch,
    "mcp__docs__search": MCP ToolWrapper{ShouldDefer=true},
  }
  discovered = {}

Runner iteration 1:
  VisibleDefinitions()
    -> Read, ToolSearch
  DeferredNames()
    -> mcp__docs__search
  prompt note:
    Available tools: Read, ToolSearch
    Searchable deferred tools: mcp__docs__search

model:
  ToolSearch({"name":"mcp__docs__search"})

ToolSearch.Execute:
  DefinitionByName("mcp__docs__search")
  MarkDiscovered("mcp__docs__search")
  return full schema

Runner iteration 2:
  VisibleDefinitions()
    -> Read, ToolSearch, mcp__docs__search
```

注意：ToolSearch 成功后，当前轮已经发给模型的 tools 列表不会变化。完整 schema 从下一轮开始进入 Provider 请求。

### 远端工具调用

```text
provider.ToolCall{
  Name="mcp__docs__search",
  Arguments={"query":"cache"}
}
  -> ToolExecutor
  -> ch04 Permission Manager
       Safety=side_effect by default
  -> allow
  -> ToolWrapper.Execute
       RemoteName="search"
       Client.CallTool("search", arguments)
  -> RPCSession.Call("tools/call")
  -> Transport.Send(JSON-RPC)
  -> Transport.Receive(JSON-RPC response)
  -> ToolCallResult{Content, StructuredContent, IsError}
  -> tool.Success / tool.Failure
  -> provider.ToolResultMessage
  -> messages += tool result
```

MCP 工具没有绕过 ch04 权限系统。`ToolWrapper.Definition()` 默认返回 `SafetySideEffect`，因此远端工具先进入权限裁决，再真正发出 `tools/call`。

## 主流程：启动期状态

MCP Manager 负责把多个 Server 的不确定性隔离开：

```text
Manager.Start
  |
  | for each config server
  v
goroutine per server
  -> context.WithTimeout(10s)
  -> create transport
  -> Client.Initialize
       initialize request
       initialized notification
  -> Client.ListTools
       tools/list page 1
       tools/list nextCursor...
  -> result{client, tools} OR result{err}

collector goroutine
  -> success:
       Registry.Register(ToolWrapper)
       clients[server] = client
  -> failure:
       diagnostics += Diagnostic{Server, Message}
  -> conflict:
       skip MCP tool
       diagnostics += duplicate message
```

这里有两个工程选择：

| 选择 | 作用 |
| --- | --- |
| 每个 Server 独立 goroutine | 慢 Server 不阻塞快 Server 初始化 |
| 注册阶段串行落库 | 避免 Registry map 并发写 |

Manager 会等待所有 goroutine 完成或超时。也就是说，一个慢 Server 最多拖到自己的 timeout，不会无限挂住启动。

## 主流程：运行期状态

延迟工具在运行期有一个小状态机：

```text
RegisteredDeferred
  state:
    tools[name] = ToolWrapper
    discovered[name] absent
  visible to model:
    system-reminder name only
  transition:
    ToolSearch(name)

Discovered
  state:
    discovered[name] = true
  visible to model:
    full tool schema in Provider tools list
  transition:
    session ends -> Registry discarded

ToolCallExecuting
  state:
    model calls registered name
  transition:
    permission deny -> tool.Failure(permission_denied)
    permission allow -> ToolWrapper -> tools/call
```

`discovered` 存在 Registry 里，所以它天然是会话内状态。重新启动应用或新建 Registry 后，延迟工具又回到“只列名字”的状态。

## trick 1：不改 Tool 接口，用可选接口扩展延迟能力

原来的工具接口很小：

```go
type Tool interface {
    Definition() Definition
    Execute(ctx context.Context, req Request) Result
}
```

如果为了延迟加载直接修改 `Tool`，所有内置工具都要被迫实现新方法。ch05 选择可选接口：

```go
type DeferredTool interface {
    ShouldDefer() bool
}
```

Registry 判断时做类型断言：

```text
tool implements DeferredTool && ShouldDefer()
  -> default hidden from Provider tools
else
  -> normal visible tool
```

这让内置工具不需要变化，只有 MCP `ToolWrapper` 固定返回 `true`。

## trick 2：Definitions 和 VisibleDefinitions 分离

Registry 现在有两类“工具列表”：

| 方法 | 语义 | 使用者 |
| --- | --- | --- |
| `Definitions()` | 全部已注册工具，包括未发现延迟工具 | 测试、ToolSearch、管理视角 |
| `VisibleDefinitions()` | 当前可以发给 Provider 的完整 schema | Agent Runner |
| `DeferredNames()` | 尚未发现、只应作为名字提示的工具 | system-reminder |

这个分离避免了一个常见错误：为了让 ToolSearch 查得到延迟工具，就把它们也暴露给模型。ch05 的实现是“Registry 知道全部，Provider 只看可见子集”。

```text
Registry.tools
  -> DefinitionByName(name): full schema for ToolSearch
  -> VisibleDefinitions(): schema exposed to Provider
  -> DeferredNames(): names exposed in system-reminder
```

## trick 3：MCP 注册名与远端名分离

MCP Server 只知道自己的工具叫 `search`，但 MewCode 里可能已经有 `Search`、另一个 Server 也可能有 `search`。所以注册名采用：

```text
mcp__<server>__<tool>
```

例子：

```text
Server "docs", remote tool "search"
  -> RegisteredName "mcp__docs__search"
  -> RemoteName     "search"
```

这带来三个好处：

| 好处 | 说明 |
| --- | --- |
| 不覆盖内置工具 | Registry duplicate 时跳过 MCP 工具 |
| 来源可读 | system-reminder 里能看出工具来自哪个 Server |
| 远端协议干净 | `tools/call` 仍发送 MCP 原始工具名 |

`sanitizeName` 会把空格、点号等字符替换成 `_`，保证注册名稳定。

## trick 4：Streamable HTTP 只做当前规范主路径

ch05 的 HTTP transport 实现的是 Streamable HTTP：

```text
POST <configured MCP endpoint>
Headers:
  Content-Type: application/json
  Accept: application/json, text/event-stream
Body:
  JSON-RPC request / notification

Response:
  200 application/json       -> JSON-RPC response
  200 text/event-stream      -> event: message / data: JSON-RPC response
  202 Accepted notification  -> no response
```

它没有实现旧版 SSE transport 的“先 GET SSE 拿 endpoint，再向 endpoint POST”模式。这个边界很重要：两者都叫 SSE，但不是同一个传输模型。

| 传输 | 本阶段状态 |
| --- | --- |
| stdio | 支持 |
| Streamable HTTP POST JSON | 支持 |
| Streamable HTTP POST SSE response | 支持 |
| Streamable HTTP GET server push | 不依赖 |
| 旧 SSE endpoint discovery | 不支持 |

这样做避免把两套协议混在一起。如果后续要兼容旧 Server，应单独新增 `type: sse`。

## trick 5：stdio 的 stderr 不是协议通道

stdio transport 只有 stdout 参与 JSON-RPC：

```text
client stdin  -> server reads JSON-RPC
server stdout -> client reads JSON-RPC
server stderr -> diagnostics, discarded by protocol parser
```

实现上：

```text
cmd.StdinPipe()
cmd.StdoutPipe()
cmd.StderrPipe()
go io.Copy(io.Discard, stderr)
```

如果把 stderr 当成协议流解析，任何日志行都会破坏 JSON 解码。ch05 明确把 stderr 从协议路径旁路掉。

## trick 6：pending map 用锁保护

并发工具调用会触发多个 goroutine 同时 `RPCSession.Call`：

```text
goroutine A:
  pending["1"] = chA

goroutine B:
  pending["2"] = chB

reader goroutine:
  msg.ID="2"
  ch := pending["2"]
```

Go map 不能并发读写，所以 `nextID`、`pending`、`closed` 都在 `sync.Mutex` 下访问。测试里还专门跑了：

```powershell
go test -race ./internal/mcp -run RPC
```

这个点不是性能优化，而是防止 Agent 在真实并发工具调用下直接进程崩溃。

## 和 ch04 的继承关系

ch05 没有绕开 ch04 权限系统，而是把 MCP 工具纳入同一条工具执行路径：

| ch04 能力 | ch05 如何继承 |
| --- | --- |
| `ToolExecutor` 统一执行入口 | MCP ToolWrapper 也是 `tool.Tool` |
| 权限裁决在工具执行前发生 | MCP 工具调用远端前先过 Authorizer |
| 工具安全级别 | MCP 工具默认 `SafetySideEffect` |
| 拒绝作为 tool result 回灌 | MCP 工具被拒绝时同样不发起远端 `tools/call` |
| Registry 是工具中心 | 内置工具、ToolSearch、MCP 工具共用一个 Registry |

换句话说，MCP 增加的是“工具来源”，不是“新的执行特权”。

## 工程边界

ch05 只实现 MCP 工具能力，不把 MCP 全协议一次性吃完：

| 本阶段不做 | 原因 |
| --- | --- |
| MCP resources | 与工具调用链路不同，先不扩展上下文资源模型 |
| MCP prompts | 需要新的提示词选择和注入语义 |
| MCP sampling | 会引入 Server 反向请求模型调用的安全边界 |
| MCP Server 端 | 本阶段只做 client |
| 健康检查和自动重连 | 先保证启动发现与调用闭环可测 |
| 工具热更新 | 当前工具列表在启动期发现，会话内只做 schema 延迟暴露 |
| 旧 SSE endpoint discovery | 与 Streamable HTTP 分离，后续可用独立 transport |
| 从 `mewcode.local.yaml` 接 MCP Server | 避免本地临时配置悄悄接入外部工具 |

这些边界让 ch05 保持在一个可验证的范围内：启动发现、延迟暴露、调用转发、失败隔离。

## 测试覆盖

ch05 的测试重点覆盖配置、协议并发、传输行为、延迟发现和 Agent 集成：

| 测试 | 关注点 |
| --- | --- |
| `TestLoadMCPServersProjectOverridesGlobalAndIgnoresLocal` | user/project 合并、local 忽略、环境变量展开 |
| `TestValidateMCPServers` | stdio/http 配置校验和错误信息 |
| `TestRegistryDeferredVisibility` | 延迟工具首轮隐藏、发现后可见 |
| `TestToolSearchDiscoversDeferredTool` | ToolSearch 返回完整 definition 并标记 discovered |
| `TestRPCSessionMatchesOutOfOrderResponses` | 并发请求乱序响应按 id 配对 |
| `TestRPCSessionCallContextCancel` | context 取消清理 pending |
| `TestHTTPTransportJSONResponse` | Streamable HTTP POST JSON 响应 |
| `TestHTTPTransportSSEResponse` | POST 返回 SSE message event |
| `TestStdioTransportRoundTripAndEnv` | stdio 子进程、env、stderr 旁路 |
| `TestClientInitializeSendsInitializedNotification` | initialize 后发送 initialized notification |
| `TestClientListToolsPagination` | `tools/list` 分页 |
| `TestClientCallTool` | `tools/call` 参数和结果 |
| `TestToolWrapperDefinitionAndExecute` | 注册名/远端名分离，执行转发 |
| `TestManagerRegistersSuccessfulServersAndIsolatesFailures` | 多 Server 并发、失败隔离、timeout |
| `TestManagerDoesNotOverwriteExistingTool` | 冲突不覆盖已有工具 |
| `TestRunnerDeferredToolSearchMakesToolVisibleNextRound` | 首轮只列名字，ToolSearch 后下一轮 schema 可见 |

回归命令：

```powershell
go test ./...
go test -race ./internal/mcp -run RPC
```

## 阅读顺序

建议按“配置进入系统 -> Server 初始化 -> 工具延迟暴露 -> 远端调用”的顺序读：

```text
internal/config/config.go
  -> internal/config/merge.go
  -> internal/config/loader.go
  -> internal/app/app.go
  -> internal/mcp/manager.go
  -> internal/mcp/client.go
  -> internal/mcp/rpc.go
  -> internal/mcp/stdio.go
  -> internal/mcp/http.go
  -> internal/mcp/tool.go
  -> internal/tool/registry.go
  -> internal/tool/builtin/tool_search.go
  -> internal/agent/tools.go
  -> internal/agent/runner.go
  -> internal/prompt/notes.go
```

如果只想抓住 ch05 的主线，读三处就够：

```text
1. internal/mcp/manager.go
   看外部 Server 如何变成 Registry 里的 ToolWrapper

2. internal/tool/registry.go + internal/tool/builtin/tool_search.go
   看延迟工具如何从名字变成完整 schema

3. internal/mcp/rpc.go
   看 JSON-RPC 并发响应如何按 id 回到正确调用方
```

这一章的核心可以压缩成一句话：

```text
MewCode 把外部 MCP Server 变成普通工具来源，但用延迟 schema、统一权限和失败隔离把远端能力收进可控的 Agent Runtime。
```
