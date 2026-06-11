# Go源码解析：MewCode ch03 Agent Loop、AgentEvent 与多轮工具编排

## 模块概览

ch03 的核心不是“多调几次工具”，而是把 MewCode 从 ch02 的单轮工具闭环升级为真正的 Agent Runtime。这个阶段开始，系统不再把工具调用视为一次特殊分支，而是把“模型思考、计划、调用工具、接收结果、继续决策”建模成一个可观测、可中止、可回放的循环。

演进关系可以这样看：

```text
ch01:
用户输入
  -> 模型流式文本
  -> TUI 渲染

ch02:
用户输入
  -> 模型请求一次工具
  -> 执行工具
  -> 回灌结果
  -> 模型最终回答

ch03:
用户输入
  -> Agent Loop 启动
  -> 多轮 LLM turn
  -> 每轮可能输出 plan / text / 多个 tool_use
  -> 同一轮多个工具可并行执行
  -> 工具结果稳定回灌
  -> UI 通过 AgentEvent 实时感知整个运行过程
  -> 循环直到 final answer / cancel / timeout / budget / max turns
```

ch02 的关键边界是“最多一次工具回合”。ch03 的关键边界则变成“允许模型自主多步执行，但必须把循环、工具、事件、UI 和资源预算全部显式收口”。

## 代码落点

ch03 保留 ch02 的 Provider、工具层和 TUI 基础能力，在此之上新增 `internal/agent` 作为 Agent Runtime：

| 文件 | 职责 |
| --- | --- |
| `internal/agent/runner.go` | 多轮 Agent Loop 主循环，负责模型调用、工具回灌和停止原因 |
| `internal/agent/collector.go` | 收集单轮 Provider 流，累计文本、工具调用和 usage，并转发运行事件 |
| `internal/agent/tools.go` | 工具批次规划、只读工具并行执行、结果稳定排序和回灌截断 |
| `internal/agent/agent_test.go` | 覆盖流收集、多轮循环、未知工具停止和工具批次规划 |
| `internal/provider/event.go` | 在 ch02 事件基础上扩展 progress 等运行期事件 |
| `internal/tool/*` | 继续提供路径、Limits、Edit 防误改和 Bash 进程清理等硬边界 |

这意味着 ch03 不是重写 ch02，而是把 ch02 的“一次工具回合”提升到可循环、可观测、可测试的 Agent Runner。

## 为什么 ch03 不是 ch02 的简单循环

ch02 的事件链路大致是：

```text
Provider StreamEvent
  -> TUI handleStream
  -> 遇到 tool_call_done
  -> Session 执行工具
  -> 第二次请求强制 Tools=nil
  -> 最终文本 done
```

这个设计很适合 ch02，因为它用工程手段阻断了 Agent Loop。模型拿到工具结果后，即使还想继续请求工具，系统也会拒绝。优点是边界硬，缺点是只能完成“一次工具回合”。

ch03 要解决的是更真实的开发任务：

```text
先读 README
再找工具实现
再读取关键文件
再运行测试
再根据失败结果修改文件
再重新测试
最后总结结果
```

这类任务天然不是一次工具调用能结束的。如果继续沿用 ch02 的模型，会出现几个结构性问题：

| 问题 | ch02 能否解决 | ch03 需要什么 |
| --- | --- | --- |
| 模型一轮返回多个 content block，其中混有 text 和多个 tool_use | 只能勉强处理单次工具完成 | 能解析一轮内多个 block，并保留顺序 |
| 工具结果后模型继续请求新工具 | ch02 会报错阻断 | 允许进入下一轮 turn |
| UI 需要展示 turn、plan、工具批次、最终答案 | ch02 只有工具行和文本流 | 需要 AgentEvent 作为统一 UI 协议 |
| 同一轮多个工具互不依赖 | ch02 串行执行 | 支持并行执行，同时稳定回灌顺序 |
| 长时间运行需要可观测性 | ch02 只能看到少量工具行 | 需要 loop_start、turn_start、usage、progress、loop_complete |

因此 ch03 的重点不是把 ch02 的递归开关打开，而是引入 Agent Runtime 的边界。

## 核心抽象

ch03 把运行时拆成三层流：

```text
Provider Stream
  -> 模型厂商原始流：OpenAI chunk / Anthropic event

Agent Internal Stream
  -> Runtime 内部事件：text delta、plan delta、tool_use block、usage、done

AgentEvent Stream
  -> 对 UI、日志、测试暴露的稳定事件协议
```

同时也有三类状态：

| 状态 | 保存位置 | 用途 |
| --- | --- | --- |
| `messages` | Agent Loop 内部 | 给下一轮 LLM 作为上下文 |
| `turn state` | 当前 turn 内部 | 聚合本轮文本、plan、tool_use、usage |
| `AgentEvent` | 输出流 | 给 UI、日志、测试消费 |

核心原则是：

```text
messages 是模型记忆
turn state 是当前轮解析缓存
AgentEvent 是 UI 直播协议
```

这个分层非常重要。不是所有运行事件都应该写进模型上下文。`usage`、`loop_start`、`turn_complete`、UI progress 这类事件对用户有价值，但对模型下一步推理未必有价值。反过来，tool result 必须写入 messages，因为模型需要基于结果继续决策。

## AgentEvent 与 UI 联动

### 为什么 UI 不应该直接消费 Provider StreamEvent

ch02 里 TUI 直接处理接近 Provider/Session 级别的事件：

```text
text_delta
thinking_delta
tool_call_start
tool_result
done
```

这些事件描述的是“底层流里发生了什么”。ch03 之后，UI 关心的已经不是 SDK chunk，而是 Agent 运行状态：

```text
Plan
1. 读取配置
2. 搜索工具实现
3. 并行读取关键文件
4. 运行测试

Turn 1
● Read(go.mod)
● Read(internal/chat/session.go)
● Grep(AgentEvent)

Turn 2
● Bash(go test ./...)

Final
测试通过，核心设计是...
```

这个视图不是 Provider 原始事件能直接表达的。它需要知道 run、turn、plan、工具批次、并行结果、累计 token、完成原因。这就是 `AgentEvent` 的职责。

### AgentEvent 类型表

| 事件类型 | 含义 | 携带数据 |
| --- | --- | --- |
| `loop_start` | 一个用户请求的 Agent Loop 开始 | run id、用户输入摘要 |
| `turn_start` | 第 N 轮 LLM 调用开始 | turn index |
| `plan_delta` | 模型正在输出计划增量 | 文本 delta |
| `plan_update` | 当前计划已稳定或完成 | plan text / steps |
| `stream_text` | 模型正在输出最终或阶段性文本 | 文本 delta |
| `tool_use` | 模型请求调用工具 | tool name、arguments、call id、turn |
| `tool_result` | 工具执行完成 | ok、summary、elapsed、truncated、error |
| `usage` | token 用量更新 | prompt/completion/total，累计值 |
| `turn_complete` | 一轮 LLM 调用结束 | turn、reason |
| `loop_complete` | 整个 Agent Loop 结束 | total turns、finish reason、usage |
| `error` | 发生错误 | code、message、recoverable |

## AgentEvent 结构设计

一个可用的 `AgentEvent` 不应该只靠 `type` 和 `content`。ch03 会有并行工具、多轮循环、可回放 UI，因此事件必须带运行标识和顺序信息：

```go
type AgentEvent struct {
    Type AgentEventType

    RunID string
    Turn  int
    Seq   int

    Delta string

    Plan *PlanSnapshot

    ToolCall   *ToolCallEvent
    ToolResult *ToolResultEvent

    Usage *UsageSnapshot
    Error *AgentError

    FinishReason string
}
```

字段边界如下：

| 字段 | 作用 |
| --- | --- |
| `RunID` | 标识一次用户请求，未来支持日志回放或并发运行 |
| `Turn` | 标识第几轮 LLM 调用，UI 可以按轮次折叠 |
| `Seq` | 标识事件全局顺序，保证 UI 可稳定重放 |
| `Delta` | 承载流式文本或计划增量 |
| `Plan` | 承载结构化计划快照 |
| `ToolCall` | 承载工具名、参数、调用 ID |
| `ToolResult` | 承载执行结果摘要、耗时、错误和截断状态 |
| `Usage` | 承载本轮或累计 token |
| `FinishReason` | 承载 turn 或 loop 的结束原因 |

为什么需要 `RunID / Turn / Seq`？因为 ch03 会出现这种情况：

```text
turn 1:
  tool_use Read A
  tool_use Read B
  tool_result Read B
  tool_result Read A
```

工具是并行执行的，完成顺序不一定等于发起顺序。UI 如果只靠接收顺序渲染，会越来越难调试。有了 `Seq`，事件可以按发生顺序回放；有了 `Turn`，事件可以归属到正确轮次；有了 `RunID`，未来多个 Agent Run 或日志文件不会混在一起。

## ch03 独有难点：一轮 content 中可能有多个 tool_use

ch02 基本可以把一次工具调用看成一个整体：

```text
LLM stream
  -> tool_call_done
  -> execute tools
  -> second request with Tools=nil
```

ch03 的一轮 LLM 输出可能是这样的 content 序列：

```text
content[0] = text: "我先检查项目结构"
content[1] = tool_use: Read(go.mod)
content[2] = tool_use: Read(README.md)
content[3] = tool_use: Grep("AgentEvent")
content[4] = text: "拿到这些结果后我会继续判断"
```

这带来三个问题：

| 问题 | 说明 |
| --- | --- |
| 顺序问题 | UI 要按模型提出工具的顺序展示 |
| 聚合问题 | OpenAI 工具参数可能分片，需要拼完整 JSON |
| 回灌问题 | 工具结果可能并发完成，但写回 messages 要保持 tool_use 顺序 |

所以 ch03 需要 `TurnAccumulator`。

## TurnAccumulator：本轮解析缓存

`TurnAccumulator` 的职责是把 Provider stream 拼成一个完整 turn，同时把可展示的信息实时转换为 AgentEvent。

```go
type TurnAccumulator struct {
    Turn int

    TextBuffer strings.Builder
    PlanBuffer strings.Builder

    ToolCalls []ToolCall
    ToolByID  map[string]*ToolCall

    Usage Usage
}
```

处理流式事件时，它做两件事：

```go
func (a *TurnAccumulator) Add(event ProviderEvent) []AgentEvent {
    switch event.Type {
    case TextDelta:
        a.TextBuffer.WriteString(event.Delta)
        return []AgentEvent{
            StreamText(event.Delta),
        }

    case PlanDelta:
        a.PlanBuffer.WriteString(event.Delta)
        return []AgentEvent{
            PlanDelta(event.Delta),
        }

    case ToolCallDelta:
        call := a.getOrCreateToolCall(event.ID)
        call.Name = merge(call.Name, event.Name)
        call.Arguments += event.ArgumentsFragment

        if call.FirstSeen {
            return []AgentEvent{
                ToolUseStarted(call),
            }
        }

    case Usage:
        a.Usage = a.Usage.Add(event.Usage)
        return []AgentEvent{
            UsageUpdated(a.Usage),
        }
    }

    return nil
}
```

一轮 Provider stream 结束后，Accumulator 产出完整 turn：

```go
func (a *TurnAccumulator) Complete() TurnResult {
    return TurnResult{
        AssistantText: a.TextBuffer.String(),
        Plan:          a.PlanBuffer.String(),
        ToolCalls:     a.ToolCalls,
        Usage:         a.Usage,
    }
}
```

这里的核心 trick 是：UI 需要实时事件，messages 需要完整结果。

```text
text delta
  -> 立即 emit stream_text 给 UI
  -> 同时写入 TextBuffer，turn 结束后生成 assistant message

tool_use delta
  -> 累积完整 arguments
  -> 首次识别工具名时 emit tool_use 给 UI
  -> turn 结束后生成 assistant tool_calls message
```

## Agent Loop 状态机

ch03 的状态机从“处理一次流”变成“管理一次运行”：

| 状态 | 输入事件 | 动作 | 下一状态 |
| --- | --- | --- | --- |
| `Idle` | 用户输入 | 初始化 run，emit `loop_start` | `LLMStreaming` |
| `LLMStreaming` | text delta | emit `stream_text` | `LLMStreaming` |
| `LLMStreaming` | plan delta | emit `plan_delta` | `LLMStreaming` |
| `LLMStreaming` | tool call done | emit `tool_use` | `ToolBatchExecuting` |
| `ToolBatchExecuting` | tool result | emit `tool_result`，暂存结果 | `ToolBatchExecuting` |
| `ToolBatchExecuting` | 当前批次完成 | 稳定排序，写入 messages，emit `turn_complete` | `LLMStreaming` |
| `LLMStreaming` | no tool calls + done | commit final answer | `Completed` |
| 任意状态 | cancel / timeout / error | emit `error` 或 `loop_complete` | `Stopped` |

完整流程可以写成伪代码：

```go
func RunAgent(ctx context.Context, input string) <-chan AgentEvent {
    out := make(chan AgentEvent)

    go func() {
        defer close(out)

        runID := newRunID()
        seq := NewSeq()

        messages := []Message{
            UserMessage(input),
        }

        emit(out, AgentEvent{
            Type:  "loop_start",
            RunID: runID,
            Seq:   seq.Next(),
        })

        for turn := 1; turn <= maxTurns; turn++ {
            emit(out, TurnStart(runID, turn, seq.Next()))

            turnResult := runLLMTurn(ctx, messages, runID, turn, seq, out)

            messages = append(messages, turnResult.AssistantMessage())

            if len(turnResult.ToolCalls) == 0 {
                emit(out, LoopComplete(runID, turn, "final_answer", seq.Next()))
                return
            }

            results := executeToolBatch(ctx, turnResult.ToolCalls, runID, turn, seq, out)

            messages = append(messages, ToolResultMessages(results)...)

            emit(out, TurnComplete(runID, turn, "tool_result", seq.Next()))
        }

        emit(out, LoopComplete(runID, maxTurns, "max_turns", seq.Next()))
    }()

    return out
}
```

## 并行工具执行与 UI 顺序

同一轮模型可能请求：

```text
call_1: Read(go.mod)
call_2: Read(README.md)
call_3: Grep("AgentEvent")
```

执行时可以并行：

```text
Grep 先完成
Read README 后完成
Read go.mod 最后完成
```

但回灌给模型时，最好保持原始 tool call 顺序：

```text
tool_result call_1
tool_result call_2
tool_result call_3
```

否则模型可能难以对应自己的调用顺序。

并行执行的核心伪代码是：

```go
func executeToolBatch(calls []ToolCall) []ToolResult {
    results := make([]ToolResult, len(calls))

    parallelForEach(calls, func(i int, call ToolCall) {
        emit(tool_use(call))

        result := executeOneTool(call)

        results[i] = result

        emit(tool_result(result))
    })

    return results
}
```

这里有两个顺序：

| 顺序 | 用途 |
| --- | --- |
| `tool_result AgentEvent` 完成顺序 | UI 实时显示谁先完成 |
| `ToolResultMessage` 回灌顺序 | 按 tool call 原顺序稳定写回 messages |

这是 ch03 很重要的 trick：UI 可以实时，模型上下文必须稳定。

### 并行边界

并行工具不是无限放开：

| 边界 | 说明 |
| --- | --- |
| `maxParallelTools` | 限制同时执行的工具数量 |
| 写工具串行或 path lock | `Write` / `Edit` 不能无脑并行改同一个文件 |
| 每个工具独立 timeout | 单个工具卡住不拖死整批 |
| 批次级 context | 用户取消时整批工具一起取消 |
| 结果回灌稳定排序 | 并发完成顺序不影响 messages |

## Plan 模式与 UI

### 为什么 Plan 是事件，而不是普通文本

如果 Plan 混在普通 `stream_text` 里，UI 很难区分：

```text
哪些是执行计划？
哪些是最终回答？
哪些只是中间说明？
```

所以 ch03 应该把 Plan 做成独立事件：

```text
plan_delta
plan_update
```

UI 可以把 Plan 放在固定区域或 scrollback 中：

```text
Plan
1. Inspect files
2. Search AgentEvent
3. Run tests
```

### Plan 的边界

| 边界 | 说明 |
| --- | --- |
| Plan 是意图，不是事实 | 不能当成执行结果 |
| Plan 可更新 | 工具结果改变判断时可以 emit 新计划 |
| Plan 不绕过工具层 | 所有读写执行仍必须走 Tool |
| Plan 不一定进 messages | UI 需要看到，模型不一定需要反复看到 |

## AgentEvent 到 UI 的渲染策略

ch03 的 UI 不再直接绑定 Provider stream，而是消费 AgentEvent：

| AgentEvent | UI 行为 |
| --- | --- |
| `loop_start` | 清空当前运行状态，显示 running |
| `turn_start` | 打印或折叠一个 Turn 标题 |
| `plan_delta` | 更新 Plan 区域 |
| `plan_update` | 固化 Plan 快照 |
| `stream_text` | 写入当前 assistant 动态区 |
| `tool_use` | 立刻打印 `● Tool(args)` 到 scrollback |
| `tool_result` | 打印 `ok/failed: summary` |
| `usage` | 更新状态栏 token 统计 |
| `turn_complete` | 收起当前 turn 或打印轮次摘要 |
| `loop_complete` | 渲染最终 Markdown，状态回到 idle |
| `error` | 打印错误块，状态回到 error/idle |

和 ch02 对比：

| 维度 | ch02 | ch03 |
| --- | --- | --- |
| UI 输入源 | `StreamEvent` | `AgentEvent` |
| 轮次概念 | 基本没有 | 明确 `turn` |
| 工具展示 | 单轮工具行 | 多轮工具批次 |
| Plan 展示 | 无 | 独立 plan 区 / plan event |
| 工具结果顺序 | 串行自然有序 | 并发完成，但回灌顺序稳定 |
| 完成事件 | Provider `done` | `loop_complete` |
| 状态栏 | 当前模型 usage | 累计 usage、turn、当前工具、运行状态 |
| 调试能力 | 看一轮流 | 可回放整个 Agent Run |

## 事件回放能力

因为 AgentEvent 有 `RunID / Turn / Seq`，一次运行可以完整记录为 JSONL：

```jsonl
{"seq":1,"type":"loop_start","run_id":"run_1"}
{"seq":2,"type":"turn_start","turn":1}
{"seq":3,"type":"plan_delta","delta":"先读取 README"}
{"seq":4,"type":"tool_use","tool":"Read","id":"call_1"}
{"seq":5,"type":"tool_result","tool":"Read","id":"call_1","ok":true}
{"seq":6,"type":"turn_complete","turn":1}
{"seq":7,"type":"turn_start","turn":2}
{"seq":8,"type":"stream_text","delta":"根据 README..."}
{"seq":9,"type":"loop_complete","reason":"final_answer"}
```

这带来三个收益：

| 收益 | 说明 |
| --- | --- |
| UI 可恢复 | 崩溃后可重放到某个状态 |
| 测试可断言 | 不只断言最终文本，还能断言中间行为 |
| 调试可定位 | 知道 Agent 在哪一轮、哪个工具、哪个结果后走偏 |

## 工程边界

ch03 允许多轮自主执行，但边界必须比 ch02 更清楚：

| 风险 | 边界 |
| --- | --- |
| 无限循环 | `maxTurns`、`maxToolCalls`、`maxDuration` |
| 并行工具过多 | `maxParallelTools` |
| 写冲突 | 写工具串行或按 path 加锁 |
| 上下文爆炸 | `MaxResultBytes` 继续兜底 |
| UI 刷屏 | 工具结果只展示摘要，完整结果只进受限消息 |
| 模型误计划 | Plan 只是可见意图，不赋予额外权限 |
| 事件乱序 | `Seq` 保证可排序，`Turn` 保证可归属 |
| 取消不彻底 | context 级联取消 Provider 和 tools |

## 和 ch02 的继承关系

ch03 不是推翻 ch02，而是把 ch02 的能力放进循环：

| ch02 能力 | ch03 继承方式 |
| --- | --- |
| 工具注册中心 | 继续作为 AgentRunner 的工具执行层 |
| Provider 工具协议转换 | 继续负责 tool schema / tool call / tool result 转换 |
| PathPolicy / Limits | 继续作为每个工具请求的硬边界 |
| TUI 工具行 | 扩展为 AgentEvent 驱动的完整运行视图 |
| 单轮工具回灌 | 推广为多轮循环回灌 |

ch03 的核心设计可以概括为一句话：

```text
让模型可以多步自主执行，但让每一步都通过 AgentEvent 被看见、被限制、被回放。
```
