# Go源码解析：MewCode ch08 Skill 技能包系统

## 模块概览

ch07 把 MewCode 推到“可持续工作的本地 Agent”：有会话恢复、长期记忆、上下文压缩和一套内置斜杠命令。ch08 解决的是另一个更贴近日常使用的问题：很多 AI 操作其实是可复用 SOP，不能一直硬编码在源码里，也不应该让用户每次反复输入同一段长提示词。

这一章新增 `internal/skill`，把“可复用 AI 操作”封装成目录型 Skill：

```text
<skill-name>/
  SKILL.md        # YAML frontmatter + Markdown SOP
  tool.json       # 可选：当前 Skill 专属工具声明
  references/     # 可选：专属工具脚本和参考资料
```

启动时，Agent 只看到 Skill 的名字和一句说明；需要执行时，再通过 `LoadSkill` 工具或 `/<skill>` 短命令把完整 SOP 和专属工具加载进当前会话。这样既避免 system prompt 被所有 SOP 塞满，也让模型在执行某类任务时只看到更窄的工具集合。

ch08 的核心变化可以概括为：

```text
启动期
  -> 扫描 builtin/user/project 三层 Skill
  -> 校验 frontmatter 和 allowed_tools
  -> 注入 Available Skills{name, description}
  -> 注册 LoadSkill system tool
  -> 注册 /commit /review /test 等 Skill 命令

运行期
  -> 自然语言：模型调用 LoadSkill{name}
  -> 斜杠命令：TUI 调用 Session.SubmitSkill(name,args)
  -> ActiveStore 记录完整 SOP + session-local tools
  -> 下一轮 system prompt 注入 Active Skill Instructions
```

## 代码落点

| 文件 | 本章职责 |
| --- | --- |
| `internal/skill/types.go` | 定义 Skill 元数据、Catalog 快照、ActiveTool 等核心类型 |
| `internal/skill/parser.go` | 解析 `SKILL.md` frontmatter、正文和 `tool.json` |
| `internal/skill/catalog.go` | 扫描 builtin/user/project 三层目录，处理覆盖、warning 和白名单校验 |
| `internal/skill/prompt.go` | 生成第一阶段 `Available Skills` prompt |
| `internal/skill/active.go` | 维护当前会话已激活 Skill 和专属工具 overlay |
| `internal/skill/tools.go` | 实现 `LoadSkill` system tool |
| `internal/skill/exec_tool.go` | 把 `tool.json` 中的脚本声明适配成 `tool.Tool` |
| `internal/skill/executor.go` | 渲染 `$ARGUMENTS`，编排 inline/fork 两种执行模式 |
| `internal/skill/command.go` | 把每个 Skill 注册成 `/<name>` 斜杠短命令 |
| `internal/skill/builtin/*/SKILL.md` | 内置 `commit`、`review`、`test` 三个样板 Skill |
| `internal/agent/tools.go` | 支持工具白名单和 session-local overlay 合成视图 |
| `internal/agent/runner.go` | 将 Skill catalog 和 active SOP 注入 system prompt |
| `internal/prompt/sections.go` | 增加 Skills Catalog 和 Active Skill Instructions 两段 |
| `internal/chat/session.go` | 持有 Catalog/ActiveStore，执行 Skill，`/clear` 时清空激活态 |
| `internal/command/*` | 命令支持尾随参数、KindLocal、SkillName 和热更新清理 |
| `internal/command/builtin/*` | 移除硬编码 `/review`，新增 `/skill` 列表命令 |
| `internal/tui/update.go` | 把 Skill 命令分发到 `ChatRunner.SubmitSkill` |
| `internal/app/app.go` | 启动期装配 Skill catalog、LoadSkill 工具和 Skill 命令 |

## 本章解决的问题

1. **Prompt 不能再散落在源码里**

   ch07 的 `/review` 是一个硬编码 prompt 命令。ch08 删除这个特例，让 review 变成内置 Skill。之后新增或调整 SOP，只需要改 `SKILL.md`，不需要改 Go 代码。

2. **启动 prompt 不能无限膨胀**

   如果所有 Skill 的完整正文都在启动时注入，prompt cache 和上下文窗口都会很快被消耗。ch08 采用两阶段加载：启动时只注入 `name + description`，完整 SOP 只有在显式激活后才进入 Active Skills 区块。

3. **工具数量变多后模型容易选错工具**

   MCP 接入后工具数量会继续增加。Skill 的 `allowed_tools` 提供了一个任务级工具白名单：模型执行某个 SOP 时，只看到该任务需要的工具，以及永远可见的 system tool。

4. **专属工具不能污染全局工具注册表**

   目录型 Skill 可以带 `tool.json` 和 `references/` 脚本，但这些工具只应该在当前会话、当前激活 Skill 内可见。ch08 引入 `ActiveStore` overlay，避免把专属工具直接写入全局 `tool.Registry`。

5. **有些任务需要隔离执行**

   `commit`、`test` 这类 SOP 适合 inline 模式，直接进入主对话；`review` 更适合 fork 模式，在子 Agent 里独立跑完，只把最终审查报告回流主历史。

## 为什么不是简单加几个命令

如果只继续给 `command/builtin` 增加硬编码命令，会很快遇到三个问题：

| 表面方案 | 问题 |
| --- | --- |
| 每个 prompt 写一个 Go handler | 用户无法本地定制，新增命令必须重新编译 |
| 启动时注入所有 prompt | system prompt 膨胀，且模型一直被无关 SOP 干扰 |
| Skill 工具直接注册到全局 registry | 不同会话、不同 Skill 的同名工具会互相覆盖 |

ch08 的设计重点不是“多几个快捷命令”，而是把 prompt、工具、执行模式和可见性边界组合成一个可扩展能力包。

## 核心抽象

| Type | 文件 | 角色 | 关键字段 | 生产者 | 消费者 |
| --- | --- | --- | --- | --- | --- |
| `skill.Metadata` | `internal/skill/types.go` | `SKILL.md` frontmatter 的规范化结果 | `Name`、`Description`、`AllowedTools`、`Mode`、`ForkContext`、`Model` | `ParseSkill` | `Catalog`、`Executor`、命令注册器 |
| `skill.Definition` | `internal/skill/types.go` | 一个已解析 Skill 的内存快照 | `Metadata`、`Body`、`Dir`、`Entry`、`Tools` | `ParseSkill`、`Catalog.Reload` | `LoadSkillTool`、`SubmitSkill` |
| `skill.ToolSpec` | `internal/skill/types.go` | `tool.json` 中的专属工具声明 | `Name`、`Description`、`InputSchema`、`Command` | `parseToolJSON` | `ExecTool` |
| `skill.Catalog` | `internal/skill/types.go` | 当前可用 Skill 的一致快照 | `Snapshot{Skills, Ordered, Warnings}` | `skill.Load`、`Reload` | prompt、命令注册、LoadSkill |
| `skill.ActiveStore` | `internal/skill/active.go` | 会话级已激活 Skill 状态 | `order`、`bodies`、`tools` | `LoadSkill`、`SubmitSkill` | `agent.ToolSet.Overlay`、prompt context |
| `skill.ExecTool` | `internal/skill/exec_tool.go` | 专属脚本工具适配器 | `Spec`、`BaseDir`、`Timeout` | `makeExecTools` | `ToolExecutor` |
| `agent.ToolSet` | `internal/agent/tools.go` | 一次 Run 的工具视图约束 | `Mode`、`Names`、`Overlay` | `chat.Session`、fork executor | `AllowedDefinitions`、`ToolExecutor` |
| `command.Definition` | `internal/command/types.go` | 支持 Skill 的斜杠命令元数据 | `AcceptsArgs`、`SkillName` | builtin/skill command 注册 | parser、TUI |

### `skill.Catalog` 生命周期

```text
LoadOptions{ProjectDir, HomeDir, BuiltinFS, Tools, Commands}
  -> Catalog.Reload
       scan builtin -> user -> project
       parse SKILL.md + optional tool.json
       validate name / command conflict / allowed_tools
       later layer overrides earlier layer by name
  -> Snapshot{Skills, Ordered, Warnings}
  -> PromptCatalog() / RegisterSkillCommands() / LoadSkillTool
```

解析失败只跳过单个 Skill，warning 写到 stderr，不阻断整个应用启动。这一点很重要：用户目录里一个坏 Skill 不应该让 MewCode 完全无法打开。

### `skill.ActiveStore` 生命周期

```text
inactive session
  -> LoadSkill{name} or /<name>
  -> ActiveStore.Activate(name, body, tools)
       first activation: append name to order
       same skill refresh: replace body and own tools
       different skill same tool name: reject with conflict
  -> prompt.ActiveSkillText = PromptText()
  -> agent.ToolSet.Overlay = ActiveStore
  -> /clear: ActiveStore.Clear()
```

`ActiveStore` 既是 prompt 状态，也是工具 overlay。它不持久化到历史里，而是在每轮请求时从会话状态重新构造 system prompt。

## 数据流

### 两阶段加载

```text
Catalog.Snapshot
  -> PromptCatalog()
       "## Available Skills"
       "- review: Review current code changes..."
  -> prompt.Options.SkillsCatalog
  -> provider.ChatRequest.SystemPrompt

model decides skill is useful
  -> ToolCall{Name:"LoadSkill", Arguments:{"name":"review"}}
  -> LoadSkillTool.Execute
       Catalog.Get("review")
       ParseSkill(def.Entry)          # 热更新：执行时重读磁盘
       ActiveStore.Activate(...)
  -> ToolResult{ok:true}

next model iteration
  -> prompt.Options.ActiveSkillText
       "### Skill: review"
       full SOP body
  -> provider.ChatRequest.SystemPrompt
```

### 显式斜杠命令

```text
textarea "/commit fix parser"
  -> command.Registry.Parse
       Invocation{Name:"/commit", Args:"fix parser", SkillName:"commit"}
  -> command handler
       Controller.RunSkill("commit","fix parser")
  -> chat.Session.SubmitSkill
       Executor.Definition("commit")
       ActiveStore.Activate(...)
       RenderExecutionPrompt(def,args)
  -> inline:
       messages += User{full SOP}
       agent.Run(main conversation)
  -> fork:
       child messages = selected parent context + User{full SOP}
       child agent.Run
       parent history += Assistant{child finalText}
```

### 工具合成视图

```text
global tool.Registry
  Bash, Read, Grep, Edit, MCP tools, LoadSkill(system)

ActiveStore overlay
  parse-resume      # 来自某个已激活 Skill 的 tool.json

Skill allowed_tools = [Read, Grep, parse-resume]
  -> AllowedDefinitions
       include Read, Grep
       include parse-resume from overlay
       include LoadSkill because system tool
       exclude Write/Bash/Edit unless named
  -> model visible tools

ToolExecutor.ExecuteToolCall
  -> overlay.Get(name) first
  -> registry.Get(name) second
  -> permission manager still authorizes side-effect tools
```

## 主流程与状态

### 启动装配

```text
app.RunChat
  -> tool.NewRegistry + builtin.RegisterDefaults
  -> mcp.Manager.Start             # MCP tools join global registry
  -> command.NewRegistry + commandbuiltin.Register
  -> skill.Load                    # allowed_tools can see builtin + MCP tools
  -> registry.Register(LoadSkill)
  -> skill.RegisterSkillCommands
  -> chat.NewSessionWithOptions
       Skills: Catalog
       ActiveSkills: ActiveStore
       PromptCtx: sessionPromptContext
```

注意顺序：Skill catalog 在 `LoadSkill` 注册前加载，因为 `allowed_tools` 校验只需要普通工具和 Skill 自己的 `tool.json` 工具；`LoadSkill` 是系统级加载工具，不应该成为用户声明白名单的依赖。

### inline / fork 模式

| 模式 | 历史写入 | 工具范围 | 适用场景 |
| --- | --- | --- | --- |
| `inline` | 渲染后的 SOP 作为 user 消息进入主历史 | 主 registry + active overlay，再按 `allowed_tools` 收窄 | commit、test、带状态延续的任务 |
| `fork` | 子 Agent 中间消息不进主历史，只回流 finalText | 子 Agent 使用同一套白名单和 overlay | review、分析类、希望隔离推理过程的任务 |

fork 的 `fork_context` 控制子 Agent 带多少主历史：

| `fork_context` | 行为 |
| --- | --- |
| `none` | 只传 Skill SOP |
| `recent` | 复制主历史最后 5 条消息，再追加 Skill SOP |
| `full` | 通过 context manager 生成压缩摘要，再追加 Skill SOP |

## trick 1：专属工具只进会话 overlay

`tool.Registry` 仍然只管理全局工具。Skill 专属工具由 `ActiveStore` 实现 `tool.Overlay`，在每次 agent run 时与主 registry 合成。

这个设计避免了一个很隐蔽的 bug：两个 Skill 都声明 `parse_file`，如果直接写入全局 registry，后加载者会覆盖先加载者。ch08 的规则是：

- 同一个 Skill 重新激活，可以替换自己的工具；
- 不同 Skill 激活同名专属工具，直接失败；
- 失败通过 tool result 或命令错误返回，不污染全局状态。

对应测试在 `internal/skill/skill_test.go` 的 `TestActiveStoreConflictAndRefresh`。

## trick 2：`LoadSkill` 是 system tool

Skill 的 `allowed_tools` 是为了收窄任务工具，但 `LoadSkill` 本身必须始终可见，否则模型在白名单很窄时反而无法加载 Skill。为此 `internal/tool/tool.go` 增加：

```go
type SystemTool interface {
    IsSystemTool() bool
}
```

`AllowedDefinitions` 按名字过滤工具时，总是保留 system tool。`LoadSkillTool` 同时是 read-only 和 system tool，所以在 plan/read-only 模式下也能被调用。

## trick 3：执行时重读 `SKILL.md`

Catalog 保存的是启动期快照，但 `LoadSkill` 和 `Executor.Definition` 会在执行时尝试重新读取 `SKILL.md`。这让用户可以修改本地 Skill 后通过 `/skill` 刷新命令，或在下一次激活时拿到最新 SOP。

重读失败时不会让整个会话崩掉：执行器会回退到 catalog 中的缓存版本。这样 Skill 文件短暂不可读时，已有能力仍然可用。

## trick 4：斜杠命令支持参数但保持旧命令严格

ch07 的内置命令都是零参数。ch08 只给 Skill 命令打开尾随参数：

```text
/commit fix parser warning     -> Args = "fix parser warning"
/status now                    -> unknown command
```

这靠 `command.Definition.AcceptsArgs` 区分。内置命令继续严格匹配，Skill 命令可以把参数替换到 `$ARGUMENTS`，或者追加到 `## User Request`。

## 与上一阶段的继承关系

| ch07 能力 | ch08 如何复用 |
| --- | --- |
| `command.Registry` | 扩展 `AcceptsArgs`、`SkillName`、`RemoveSkillCommands` |
| TUI 回车分流 | Skill 命令沿用同一条 `dispatchCommand` 路径 |
| `chat.Session` | 增加 Catalog/ActiveStore，并继续负责历史和 archive |
| `agent.Runner` | 不感知磁盘 Skill，只消费 PromptContext 和 ToolSet |
| context manager | fork full 借用压缩摘要能力 |
| permission manager | 专属工具最终仍走 `ToolExecutor` 授权链路 |

## 工程边界

本章明确不做：

- Skill 市场分发、远程安装、zip 下载或版本管理；
- Skill 沙箱隔离，专属工具脚本默认信任本地文件；
- 单文件 Skill，只支持目录型 Skill；
- Skill 启用/禁用命令，需要禁用时删除目录；
- TUI 详情页，`/skill` 只输出简洁列表。

这些边界让 ch08 聚焦在“本地可编辑能力包 + 两阶段加载 + 会话级工具 overlay”，避免把分发、安全沙箱和版本协议混进同一个阶段。

## 测试覆盖

| 行为 | 测试 |
| --- | --- |
| frontmatter、默认值、`tool.json` 解析 | `internal/skill/skill_test.go::TestParseSkillDefaultsAndToolJSON` |
| builtin/user/project 覆盖优先级 | `TestCatalogPriorityAndSkipsMissingAllowedTool` |
| 不存在的 `allowed_tools` 跳过单个 Skill | `TestCatalogPriorityAndSkipsMissingAllowedTool` |
| ActiveStore 同名工具冲突和同 Skill 刷新 | `TestActiveStoreConflictAndRefresh` |
| `LoadSkill` 激活 SOP 和 overlay 工具 | `TestLoadSkillToolActivatesPromptAndOverlay` |
| 内置 commit/review/test 加载 | `TestBuiltinCatalogLoadsCommitReviewTest` |
| `/skill` 本地命令输出 | `internal/command/builtin/builtin_test.go` |
| TUI Skill 命令分发 | `internal/tui/tui_test.go::TestSkillCommandRunsSkill` |
| 全项目回归 | `go test ./...` |

## 阅读顺序

1. 从 `internal/skill/types.go` 开始，先看 `Metadata`、`Definition`、`Catalog`、`ActiveStore` 的数据形状。
2. 读 `internal/skill/parser.go` 和 `catalog.go`，理解目录扫描、覆盖顺序和 warning 策略。
3. 读 `internal/skill/active.go`、`tools.go`、`exec_tool.go`，看 SOP 激活和专属工具 overlay。
4. 读 `internal/agent/tools.go`，理解白名单、system tool 和 overlay 的合成规则。
5. 读 `internal/chat/session.go` 的 `SubmitSkill`，看 inline/fork 如何回到主会话。
6. 读 `internal/command` 与 `internal/tui/update.go`，确认 `/skill` 和 `/<name>` 如何从 UI 进入执行器。
7. 最后读 `internal/app/app.go`，把启动装配顺序串起来。
