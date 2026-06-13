# Go源码解析：MewCode ch04 工具权限与安全沙箱

## 模块概览

ch04 的核心不是“给危险操作加一个确认弹窗”，而是把 ch03 已经形成的 Agent Runtime 接入统一权限决策层。上一阶段解决的是 Agent Loop：模型可以多轮思考、调用工具、接收结果并继续决策；这一阶段解决的是 Agent Loop 放开之后更尖锐的问题：每一次本地读写和命令执行，是否都经过了可解释、可测试、可短路的授权边界。

演进关系可以这样看：

```text
ch02:
用户输入
  -> 模型请求一次工具
  -> 工具执行
  -> 结果回灌
  -> 最终回答

ch03:
用户输入
  -> Agent Loop
  -> 多轮模型调用
  -> 多轮工具执行
  -> 工具结果持续回灌
  -> 直到 final / cancel / max iterations

ch04:
用户输入
  -> Agent Loop
  -> 模型提出工具调用
  -> Permission Manager 串行裁决
  -> allow: 执行真实工具
  -> deny: 返回结构化 tool result
  -> 模型基于拒绝结果继续推理
```

ch04 的重点是增量安全边界。它不重写 Provider，不重写工具协议，不改变 Agent Loop 的主循环语义，而是在工具真正执行前增加一条串行权限漏斗：

```text
Tool Call
  -> Dangerous Command Hard Block
  -> Path Sandbox
  -> Configurable Rules
  -> Permission Mode
  -> Human-in-the-loop
  -> allow / deny
```

这不是为了宣称彻底解决安全问题，而是通过 Defense-in-Depth 提高越权访问、指令注入、路径逃逸和敏感数据泄露的攻击成本，并保证拒绝行为不会把 Agent Loop 打崩。

## 代码落点

ch04 的主要代码增量集中在权限核心、路径沙箱、Agent 工具入口、配置加载和 TUI 确认交互。和前几章不同，这里不再重复 Provider、Markdown、基础 TUI、普通工具实现这些已经讲过的非核心代码。

| 文件 | ch04 职责 |
| --- | --- |
| `internal/permission/types.go` | 定义 `Request`、`Decision`、`Rule`、`Layer`、`Prompt` 等权限数据结构，是 ch04 的数据模型中心 |
| `internal/permission/manager.go` | 维护 `Manager` 和 session rules 状态，把 `Request` 转成最终 `Decision` |
| `internal/permission/hardblock.go` | 用高置信正则把危险 `Bash(command)` 直接转成 `DecisionDeny` |
| `internal/permission/rules.go` | 将 `Rule{Action, Pattern}` 编译成工具名、字段名和 matcher，并按 layer 裁决 |
| `internal/permission/mode.go` | 根据 `Mode` 和工具 `Safety` 生成默认 `allow` 或 `ask` 决策 |
| `internal/permission/store.go` | 把 permanent grant 转成 YAML AST 节点，追加到 `mewcode.local.yaml` |
| `internal/tool/path.go` | 将输入路径转成真实路径，处理不存在目标、symlink 和遍历实体边界 |
| `internal/agent/tools.go` | 从 `provider.ToolCall` 提取 `Fields`，构造 `permission.Request`，把 deny 转成 `tool.Result` |
| `internal/agent/runner.go` | 为每次运行注入带 TUI prompter 的 `Authorizer`，把确认请求桥接成事件 |
| `internal/provider/event.go` | 在流式事件模型中承载 `permission.Prompt` payload |
| `internal/tui/update.go` | 维护 `PermissionQueue` / `ActivePermission` 状态，并把按键转回 `UserGrant` |
| `internal/config/loader.go` | 把 user/project/local 配置文件转成 `permission.Layer`，保留规则来源 |
| `mewcode.yaml.example` | 展示 `permissions.mode` 和 `permissions.rules` 如何进入配置数据流 |

关键接入点在 `ToolExecutor.ExecuteToolCall`：

```text
provider.ToolCall{ID, Name, Arguments}
  -> ToolExecutor.permissionFields
  -> permission.Request{Tool, Fields, Safety, PathPolicy}
  -> permission.Decision{Status, Source, Rule, Suggestions}
  -> allow: tool.Request{Arguments, WorkingDir, PathPolicy, Limits}
  -> deny: tool.Result{OK=false, Error.Code=permission_denied}
```

也就是说，权限层卡在“模型已经提出工具调用”和“真实副作用发生”之间。这是最小改动点，也是最关键的安全边界。

## 本阶段解决的问题

ch03 之后，Agent 可以连续做很多步。这让开发任务更自然，也让风险被放大：

```text
模型读 README
  -> 搜索配置
  -> 读取源码
  -> 写文件
  -> 运行命令
  -> 根据失败继续修改
  -> 继续运行命令
```

如果每一步只依赖工具自己的局部校验，就会出现几个结构性缺口：

| 风险 | 典型场景 | ch04 防线 |
| --- | --- | --- |
| Prompt Injection 与高危破坏 | 上下文里诱导模型执行 `rm -rf /`、`format`、危险 `dd` | hard block 先于所有配置执行 |
| Path Traversal | 模型传入 `../../secret` 或绝对路径 | 路径沙箱限制真实目标必须在项目根内 |
| symlink 绕过 | 项目内路径实际指向项目外敏感目录 | 校验前解析真实路径 |
| 敏感数据泄露 | 大范围读取 `.env`、密钥、私有配置 | 可配置 deny 规则收窄读权限 |
| 意外覆写 | 模型误写关键配置、锁文件或源码 | 权限模式和规则决定写入是否自动允许 |
| 未知边界行为 | 系统无法静态判断风险 | HITL 作为最后一道防线 |

ch04 的核心判断是：安全边界不能只靠提示词表达，也不能只靠用户临场判断。提示词负责约束模型意图，权限层负责约束真实执行。

## 为什么不是简单确认弹窗

只做确认弹窗会把所有风险都推给用户，而且容易在多轮 Agent Loop 中变成确认疲劳。ch04 把人工确认放在最后一层，而不是第一层。

| 问题 | 单纯确认弹窗的问题 | ch04 的处理 |
| --- | --- | --- |
| 已知破坏命令 | 用户可能误放行 | hard block 不可覆盖 |
| symlink 逃逸 | UI 展示的是项目内路径，真实目标可能在项目外 | 先解析真实路径再裁决 |
| 配置冲突 | 全局 allow 可能覆盖项目 deny | 保留规则来源，按层级裁决 |
| Plan Mode | 提示词只读不是执行边界 | `plan` 模式继续使用 `default` 权限矩阵 |
| 拒绝结果 | 返回 Go error 会中断 loop | 拒绝变成标准 tool result |

所以 ch04 的核心不是 UI，而是一条确定顺序的权限决策链。UI 只负责处理前几层无法直接裁决的请求。

## 核心抽象

ch04 的源码解析不能只看函数调用顺序，真正的主线是几种数据结构如何在 Agent、权限层、TUI 和模型上下文之间流动。

| Type | File | Role | Critical fields | Produced by | Consumed by | Lifecycle / transformation |
| --- | --- | --- | --- | --- | --- | --- |
| `permission.Request` | `internal/permission/types.go` | 归一化后的工具授权输入 | `Tool`、`Fields`、`Safety`、`PathPolicy` | `ToolExecutor.ExecuteToolCall` | `Manager`、`CheckHardBlock`、`RuleEngine`、`EvaluateMode` | 从 `provider.ToolCall.Arguments` 提取字段后生成，只存在于一次授权裁决内 |
| `permission.Decision` | `internal/permission/types.go` | 授权裁决结果 | `Status`、`Source`、`Rule`、`Suggestions` | hard block、rule engine、mode、prompter | `ToolExecutor` | `allow` 继续执行工具；`deny` 转成 `tool.Failure`；`ask` 只在内部流转到下一层 |
| `permission.Rule` | `internal/permission/types.go` | 用户/项目/会话策略单元 | `Action`、`Pattern`、`Source` | YAML 配置、session grant、permanent grant | `RuleEngine` | 加载时保留来源；session/permanent grant 会动态生成 allow 规则 |
| `permission.Layer` | `internal/permission/types.go` | 带来源的一组规则 | `Source.Kind`、`Source.Path`、`Rules` | `config.LoadDetailedWithOptions`、`Manager.layersWithSession` | `RuleEngine` | 按 session/local/project/user 排序；不被扁平化，避免丢失优先级 |
| `compiledRule` | `internal/permission/rules.go` | 可执行 matcher | `tool`、`field`、`pattern`、`rule` | `ParseRule` | `RuleEngine.Evaluate` | 每次裁决时从 `Rule.Pattern` 编译，匹配 `Request.Fields` |
| `permission.Prompt` | `internal/permission/types.go` | HITL 请求 payload | `ID`、`Tool`、`Summary`、`Reason`、`Response` | `Manager.Authorize` | `eventPrompter`、TUI | 通过事件进入 TUI 队列，用户按键后通过 `Response` channel 回到 Manager |
| `permission.UserGrant` | `internal/permission/types.go` | 用户确认结果 | `once`、`session`、`permanent`、`deny` | TUI 按键处理 | `Manager.Authorize` | `session` 写入内存规则；`permanent` 写入本地 YAML 并同步为 session rule |
| `tool.PathPolicy` | `internal/tool/path.go` | 项目路径沙箱 | `Root` | App/Runner 初始化 | 文件工具、搜索工具、permission request | 把输入路径或遍历实体解析成真实路径，再判断是否仍在项目根内 |
| `provider.StreamEvent` | `internal/provider/event.go` | Agent 到 TUI 的运行事件 | `Type`、`ToolResult`、`Permission`、`Progress` | Runner、ToolExecutor、eventPrompter | TUI `handleStream` | ch04 新增 `permission_request`，承载 `permission.Prompt` |
| `PermissionQueue` / `ActivePermission` | `internal/tui/model.go` | TUI 侧权限确认状态 | queue、active prompt、response channel | TUI `enqueuePermission` | TUI `handlePermissionKey`、`denyAllPermissions` | FIFO 展示确认请求，按键后清 active 并推进下一个 prompt |

### Request：把工具调用变成可裁决对象

模型输出的是 provider 层的 `ToolCall`，权限层需要的是可匹配、可解释的请求。因此 ch04 增加了 `permission.Request`：

```go
type Request struct {
    CallID     string
    Tool       string
    Arguments  json.RawMessage
    Fields     map[string]string
    Safety     tool.Safety
    WorkingDir string
    PathPolicy tool.PathPolicy
}
```

这里最重要的是 `Fields`。权限规则不直接匹配整段 JSON，而是提取每个工具的关键字段：

| 工具 | 主字段 | 用途 |
| --- | --- | --- |
| `Bash` | `command` | 匹配命令前缀或危险命令 |
| `Read` | `path` | 限制读取路径 |
| `Write` | `path` | 限制写入路径 |
| `Edit` | `path` | 限制编辑路径 |
| `Glob` | `pattern` | 限制文件查找模式 |
| `Grep` | `path` 或 `pattern` | 优先限制搜索范围，否则限制搜索模式 |

这个设计避免规则引擎理解每个工具的完整业务语义。它只需要知道“哪个字段代表主要风险面”。

### Decision：统一 allow / deny / ask

权限层的输出是 `Decision`：

```go
type Decision struct {
    Status      DecisionStatus
    Reason      string
    Source      SourceKind
    Rule        *Rule
    Suggestions []string
}
```

`Status` 只有三种：

| 状态 | 含义 |
| --- | --- |
| `allow` | 可以执行真实工具 |
| `deny` | 不执行真实工具，返回拒绝 tool result |
| `ask` | 当前层无法裁决，进入下一层或 HITL |

`Suggestions` 是给模型看的，不是给 UI 美化用的。比如写入被拒绝时，模型会收到“先读取相关文件并提出更小 edit”“请求用户允许具体 path pattern”等建议，这让它有机会调整策略。

### Manager：唯一授权入口

`Manager.Authorize` 是 ch04 的核心入口：

```text
Request{Tool, Fields, Safety}
  -> hardblock decision? Decision{deny, Source=hardblock}
  -> rule decision?      Decision{allow|deny, Source=session|local|project|user}
  -> mode decision?      Decision{allow|ask, Source=mode}
  -> prompt needed?      Prompt{Summary, Options, Response}
  -> final               Decision{allow|deny, Suggestions}
```

调用方不需要知道规则优先级、模式矩阵或 TUI 交互细节。Agent 只依赖 `permission.Authorizer` 接口。

## 数据流

ch04 最关键的数据流不是“哪个函数调哪个函数”，而是 `ToolCall` 如何被改造成权限请求，权限请求如何变成决策，决策又如何回到工具结果和模型上下文。

```text
LLM output
  |
  | provider.ToolCall{ID, Name, Arguments}
  v
ToolExecutor
  |
  | extract Fields from Arguments:
  |   Bash.command / Read.path / Grep.path|pattern / ...
  v
permission.Request{
  CallID,
  Tool,
  Arguments,
  Fields,
  Safety,
  WorkingDir,
  PathPolicy,
}
  |
  v
permission.Manager
  |
  | Decision{Status=allow, Source=mode|rule|user}
  |---------------------------------------------+
  |                                             v
  |                                  tool.Request{PathPolicy, Limits}
  |                                             |
  |                                             v
  |                                  real Tool.Execute
  |                                             |
  |                                             v
  |                                  tool.Result{OK=true|false}
  |
  | Decision{Status=deny, Source=hardblock|rule|user}
  |---------------------------------------------+
                                                v
                                     tool.Failure{
                                       Error.Code=permission_denied
                                       Data.suggestions=[...]
                                     }
                                                |
                                                v
provider.ToolResultMessage{ID, Name, Content=result.JSON()}
                                                |
                                                v
messages += ChatMessage{Role=user, ToolResults=[...]}
                                                |
                                                v
next model turn can recover
```

配置数据流是另一条线。它决定 `RuleEngine` 看见的不是扁平规则表，而是带来源的规则层：

```text
~/.mewcode/config.yaml
  -> AppConfig.Permissions.Rules
  -> Layer{Source=user, Rules=[...]}

<project>/mewcode.yaml
  -> AppConfig.Permissions.Rules
  -> Layer{Source=project, Rules=[...]}

<project>/mewcode.local.yaml
  -> AppConfig.Permissions.Rules
  -> Layer{Source=local, Rules=[...]}

Manager.sessionRules
  -> Layer{Source=session, Rules=[...]}

[]Layer
  -> RuleEngine orders as:
       session -> local -> project -> user
  -> Decision{allow|deny|ask}
```

HITL 的事件数据流则跨过 Runner 和 TUI：

```text
Decision{Status=ask}
  -> Prompt{ID, Tool, Summary, Reason, Options, Response}
  -> StreamEvent{Type=permission_request, Permission=*Prompt}
  -> TUI.PermissionQueue += Prompt
  -> ActivePermission = queue.pop()
  -> key(o|s|p|n)
  -> UserGrant{once|session|permanent|deny}
  -> Prompt.Response <- UserGrant
  -> Manager resumes Authorize
```

## 串行漏斗式权限管道

五层防线不是并行检查，也不是独立开关，而是一条 Chain of Responsibility。每层只有两个职责：能裁决就返回，不能裁决就把请求交给下一层。

```text
                Request{Tool, Fields, Safety}
                    |
                    v
        +-------------------------+
        | 1. hard block           |
        | Bash.command regex      |
        +-------------------------+
          | deny: Decision{hardblock}
          | ask: no hard match
                    |
                    v
        +-------------------------+
        | 2. path sandbox         |
        | realpath target/entity  |
        +-------------------------+
          | deny: invalid_path / escaped root
          | pass: real path inside root
                    |
                    v
        +-------------------------+
        | 3. rules                |
        | match Request.Fields    |
        +-------------------------+
          | deny/allow: Decision{Rule, Source}
          | ask: no rule matched
                    |
                    v
        +-------------------------+
        | 4. permission mode      |
        | mode + tool.Safety      |
        +-------------------------+
          | allow: read-only / accept / bypass
          | ask: side-effect tool
                    |
                    v
        +-------------------------+
        | 5. HITL                 |
        | Prompt -> UserGrant     |
        +-------------------------+
          | once: Decision{allow}
          | session/permanent: add Rule then allow
          | deny: Decision{deny, Suggestions}
                    |
                    v
            final Decision{allow|deny}
```

短路规则很重要：

| 层 | 明确 deny 后是否继续 |
| --- | --- |
| hard block | 不继续 |
| path sandbox | 不继续 |
| rules | 不继续 |
| mode | `allow` 不继续，`ask` 进入 HITL |
| HITL | 最终裁决 |

这种顺序保证了越靠前的边界越硬。用户确认不能覆盖 hard block，`bypassPermissions` 也不能覆盖路径沙箱。

## trick 1：hard block 不是普通规则

危险命令黑名单位于权限管道最前面：

```go
if hard := CheckHardBlock(req); hard.Status == DecisionDeny {
    return hard
}
```

这意味着它不受以下机制影响：

| 机制 | 是否能覆盖 hard block |
| --- | --- |
| user allow rule | 不能 |
| project allow rule | 不能 |
| session allow rule | 不能 |
| `bypassPermissions` | 不能 |
| HITL once/session/permanent | 不能 |

第一版黑名单只覆盖高置信危险集合，例如：

```text
rm -rf /
rm -rf *
rm -rf ../*
format
mkfs
dd ... of=/dev/...
chmod -R 777 /
chown -R ... /
PowerShell Remove-Item -Recurse -Force ...
```

这里的 trick 是克制：它不假装自己是 shell parser，不尝试理解所有命令组合，只对高破坏、低误伤的模式做不可恢复前的拦截。

## trick 2：路径沙箱先解析真实路径

路径安全最容易被字符串比较骗过：

```text
project/
  src/
  outside -> /Users/alice/.ssh

Read("outside/id_rsa")
```

`outside/id_rsa` 字面上在项目目录内，但真实目标已经逃逸。ch04 的 `PathPolicy.Resolve` 做的是真实路径裁决：

```text
input path
  -> trim / clean / from slash
  -> join with real project root
  -> resolve existing target
  -> if target does not exist:
       find nearest existing parent
       EvalSymlinks(parent)
       append missing suffix back
  -> filepath.Rel(realRoot, realTarget)
  -> reject if rel escapes root
```

这里有两个细节：

| 细节 | 价值 |
| --- | --- |
| root 也要 `EvalSymlinks` | 项目根本身可能就是 symlink |
| 新文件解析最近存在父目录 | `Write(new/file.go)` 不能因为目标不存在就跳过父目录 symlink 校验 |

这比“清理 `..` 然后看字符串前缀”更硬。攻击者不能通过项目内 symlink 把真实访问目标引到项目外。

## trick 3：Glob/Grep 做遍历时校验

`Read`、`Write`、`Edit` 面对的是单一路径；`Glob` 和 `Grep` 面对的是一片搜索空间。对于这类工具，不能只校验输入 pattern。

错误直觉是：

```text
Glob("**/*.go")
  -> pattern 看起来没有 ..
  -> 放行
```

真正的问题在遍历过程中：

```text
project/
  safe.go
  linked-dir -> /tmp/outside
```

如果 WalkDir 进入 `linked-dir`，就可能把项目外文件返回给模型。ch04 给 `PathPolicy` 增加了遍历实体校验：

```text
WalkDir(root)
  -> ResolveVisited(abs)
  -> if symlink dir escapes root:
       skip subtree
  -> if file escapes root:
       reject or skip
  -> then match / read
```

这个 trick 的关键是：安全判断延迟到“实际访问实体”发生时，而不是停留在 pattern 字符串上。

## trick 4：规则保留来源，不扁平化

权限配置来自多层：

```text
~/.mewcode/config.yaml       -> user
<project>/mewcode.yaml       -> project
<project>/mewcode.local.yaml -> local
session grants               -> session
```

如果把这些规则 merge 成一个扁平数组，会丢失“谁覆盖谁”的语义。ch04 保留 `Layer`：

```go
type Layer struct {
    Source Source
    Rules  []Rule
}
```

裁决顺序固定为：

```text
session
  -> local
  -> project
  -> user
```

同一层内 deny 优先：

```text
for current layer:
  if any deny matches:
      deny
  if any allow matches:
      allow
  continue next layer
```

这让策略更可预测：

| 场景 | 结果 |
| --- | --- |
| user allow `Bash(*)`，project deny `Bash(rm *)` | 项目 deny 生效 |
| project allow `Write(docs/*)`，local deny `Write(docs/secrets.md)` | 本地 deny 生效 |
| session allow `Bash(go test ./...)` | 当前会话优先生效 |

越靠近当前运行上下文的规则越具体，冲突时选择更保守的一侧。

## trick 5：拒绝也是 tool result

ch04 不把权限拒绝当作 Agent 错误。`ToolExecutor` 拿到 deny 后不会执行真实工具，而是合成一个结构化失败结果：

```go
code := "permission_denied"
if decision.IsHardBlock() {
    code = "permission_hard_denied"
}
result := tool.Failure(call.Name, call.ID, code, decision.Reason)
result.Data = map[string]any{
    "suggestions": decision.Suggestions,
}
```

闭环是这样的：

```text
model:
  call Write("mewcode.yaml", ...)

permission:
  deny by rule

tool result:
  ok=false
  error.code=permission_denied
  suggestions=[
    "Read the relevant file first and propose a smaller edit.",
    "Ask the user to allow a specific path pattern."
  ]

next model turn:
  shrink operation
  switch to read-only tool
  ask user for guidance
```

这个设计非常关键。拒绝不会触发 panic，不会中断 Agent Loop，也不会变成 UI 层散落的 Go error。它和普通工具失败一样进入 messages，让模型知道“环境拒绝了什么，以及可以怎么改”。

## HITL：人在回路是最后防线

当 hard block、路径沙箱、规则和模式都无法直接给出最终结果时，才进入人工确认。

```text
permission.Manager
  -> Prompter.Confirm
  -> StreamEventTypePermissionRequest
  -> TUI enqueue permission prompt
  -> user key
  -> response channel
  -> Manager resumes
```

TUI 支持四种选择：

| 按键 | 授权 | 行为 |
| --- | --- | --- |
| `o` | once | 只放行当前工具调用 |
| `s` | session | 生成会话级 allow 规则 |
| `p` | permanent | 写入当前项目 `mewcode.local.yaml` |
| `n` | deny | 返回 `permission_denied` |

这里有两个工程细节：

| 细节 | 价值 |
| --- | --- |
| FIFO 权限队列 | 多个请求不会互相覆盖 |
| Ctrl+C 拒绝所有 pending prompt | 避免 goroutine 永久等待用户响应 |

HITL 不是“安全系统的全部”，只是未知边界行为的最后一道人工闸门。

## 状态流转

ch04 的运行状态主要出现在两个地方：Agent 工具执行状态，以及 TUI 权限确认状态。它们通过 `permission.Prompt.Response` channel 连接。

```text
Agent ToolExecution
  |
  | Request needs confirmation
  v
PermissionPending{
  Prompt.ID=CallID,
  Prompt.Response=chan UserGrant,
}
  |
  | StreamEvent{Type=permission_request, Permission=*Prompt}
  v
TUI QueueState
  |
  | enqueue PermissionQueue += Prompt
  v
WaitingUserInput{
  ActivePermission=Prompt,
  normal chat enter disabled,
}
  |
  +-- key "o" --> GrantOnce
  |               -> Decision{allow}
  |               -> ToolExecuting
  |
  +-- key "s" --> GrantSession
  |               -> Manager.sessionRules += Rule{allow, pattern}
  |               -> Decision{allow}
  |               -> ToolExecuting
  |
  +-- key "p" --> GrantPermanent
  |               -> YAMLRuleStore.AppendAllowRule(pattern)
  |               -> Manager.sessionRules += Rule{allow, pattern}
  |               -> Decision{allow}
  |               -> ToolExecuting
  |
  +-- key "n" --> GrantDeny
  |               -> Decision{deny, Suggestions}
  |               -> ToolResultAppended(permission_denied)
  |
  +-- Ctrl+C --> deny active + queued prompts
                  -> context cancellation
                  -> blocked tool calls return deny/cancelled
```

这个状态流转说明了两个边界：

| 状态边界 | 作用 |
| --- | --- |
| `ActivePermission != nil` 时禁用普通 Enter 提交 | 防止确认按键和聊天输入混在一起 |
| Ctrl+C 拒绝 active 和 queued prompts | 防止等待用户确认的工具 goroutine 泄漏 |

## 权限模式

模式处理的是“规则未命中时默认怎么做”：

| 模式 | 默认行为 |
| --- | --- |
| `default` | 只读工具自动允许，`Write` / `Edit` / `Bash` 询问 |
| `acceptEdits` | 只读和文件编辑自动允许，`Bash` 询问 |
| `plan` | 权限矩阵与 `default` 一致 |
| `bypassPermissions` | 普通未命中调用自动允许 |

`bypassPermissions` 不是完全裸奔。它仍然不能绕过：

```text
dangerous command hard block
path sandbox realpath check
explicit deny rule
```

这让 bypass 更像“减少确认噪音的危险模式”，而不是“关闭所有安全边界”。

## 配置形态

权限配置集中在 `permissions` 字段下：

```yaml
permissions:
  mode: default
  rules:
    - action: allow
      pattern: Bash(git *)
    - action: deny
      pattern: Bash(rm *)
    - action: allow
      pattern: Write(docs/*)
    - action: deny
      pattern: Read(.env*)
```

规则 pattern 支持简写和显式字段：

| 写法 | 含义 |
| --- | --- |
| `Bash(git *)` | 匹配 Bash 主字段 `command` |
| `Write(docs/*)` | 匹配 Write 主字段 `path` |
| `Write(path=docs/*)` | 显式匹配 `path` |
| `Grep(pattern=TODO*)` | 显式匹配搜索 pattern |

永久授权写入当前项目的 `mewcode.local.yaml`，而不是全局配置。这是一个有意的风险收窄：一次项目里的永久放行，不应该自动扩大到用户所有项目。

## Agent Loop 的韧性变化

ch03 的 Agent Loop 已经能多轮运行；ch04 改变的是“被拒绝时怎么继续”。

以前更容易出现的是：

```text
tool execution error
  -> stream error
  -> loop stops
```

ch04 期望的是：

```text
permission denied
  -> tool_result(permission_denied)
  -> append to messages
  -> model continues
```

这让模型具备三种恢复路径：

| 恢复路径 | 示例 |
| --- | --- |
| 缩小范围 | 从 `Write(*)` 改成 `Write(docs/notes.md)` |
| 改用只读工具 | 写入被拒后先 `Read` / `Grep` 分析 |
| 请求用户指导 | 明确说明需要哪条授权规则 |

安全层因此不是 Agent Loop 的刹车，而是运行时反馈的一部分。

## 和 ch03 的继承关系

ch04 不推翻 ch03 的 Agent Runtime，而是给每个工具调用补上执行前授权：

| ch03 能力 | ch04 增量 |
| --- | --- |
| 多轮 Agent Loop | 每轮每个工具调用都经过 `Authorizer` |
| 只读工具并行 | 并行工具也各自独立裁决 |
| 工具结果稳定回灌 | 权限拒绝同样作为 tool result 回灌 |
| progress / tool_result 事件 | 新增 `permission_request` 事件 |
| Plan Mode | 继续由权限矩阵兜底写入和 Bash |
| 工具局部边界 | 升级为统一决策层 + 工具内部硬边界 |

可以把 ch04 总结成一句话：

```text
让 Agent 可以继续自主执行，但每一步本地副作用都必须先穿过一条可短路、可解释、可恢复的权限管道。
```

## 工程边界

ch04 聚焦本地工具权限，不把问题扩大成完整操作系统沙箱：

| 本阶段不做 | 原因 |
| --- | --- |
| 网络请求限制 | 当前内置工具没有独立网络网关 |
| CPU / 内存 / 进程数量配额 | 资源治理应由后续 runtime 层处理 |
| 审计日志和安全报告 | 本阶段先完成实时裁决和 UI 可见性 |
| shell 级系统调用沙箱 | Bash 仍是受控命令执行，不是容器隔离 |
| Bash 参数路径完整解析 | 避免伪安全感，只做高置信 hard block |
| 权限配置 UI | 先稳定 YAML 语义和 TUI 确认链路 |

这些边界是刻意收窄的工程选择。ch04 先把最关键的决策点插到工具执行前，把不可恢复破坏、路径逃逸、策略裁决、权限模式和人工确认连成串行漏斗。

## 测试覆盖

ch04 的测试重点不在 Provider 协议，而在权限裁决和拒绝恢复：

| 测试区域 | 关注点 |
| --- | --- |
| `internal/permission` | 规则解析、deny 优先、hard block、模式矩阵、永久授权写入 |
| `internal/tool/path_test.go` | `../` 逃逸、symlink 逃逸、新文件父目录真实路径 |
| `internal/tool/builtin/search_test.go` | `Glob` / `Grep` 遍历 symlink 时不泄露项目外内容 |
| `internal/config/loader_test.go` | user/project/local 权限配置加载和非法 action 校验 |
| `internal/agent/agent_test.go` | deny 时不执行真实工具，拒绝结果回灌 |
| `internal/tui/tui_test.go` | 权限队列、按键响应、pending prompt 取消 |
| `internal/e2e/chat_test.go` | 用户拒绝后 Agent Loop 继续生成替代响应 |

运行全量测试：

```powershell
go test ./...
```

## 阅读顺序

如果按源码阅读 ch04，建议从权限链路入口往两边看：

```text
internal/agent/tools.go
  -> internal/permission/types.go
  -> internal/permission/manager.go
  -> internal/permission/hardblock.go
  -> internal/permission/rules.go
  -> internal/permission/mode.go
  -> internal/tool/path.go
  -> internal/tool/builtin/glob.go
  -> internal/tool/builtin/grep.go
  -> internal/agent/runner.go
  -> internal/provider/event.go
  -> internal/tui/update.go
  -> internal/permission/store.go
  -> internal/config/loader.go
```

这条路径基本覆盖了从模型提出工具调用，到权限裁决、真实工具执行或拒绝回灌、TUI 人工确认、永久授权落盘的完整主流程。
