# Learn-CC
本项目通过多个流程学习并尝试设计一个Coding Agent。

需要注意的是本项目通过gpt5.5进行Vibe Coding，因此主要是学习设计理念，整体只把关了功能需求，技术栈，架构设计，模型层和数据流通等方向。
其余如命令行操作之类的则全由模型提供方案进行选择。


## 开发方式

本项目采用 Spec 驱动开发（你也可以根据按照习惯的Vibe方式进行）。
```text
spec.md -> plan.md -> task.md -> checklist.md

- spec.md：要做什么，边界是什么，如何验收。
- plan.md：架构上怎么做，模块如何拆分。
- task.md：按什么顺序实现，每一步如何验证。
- checklist.md：最终如何确认行为正确。
```
仓库不会长期保留每一次开发过程中的 `spec.md`、`plan.md`、`task.md`、`checklist.md`，这些文件属于本地工作文档。

如果你想基于本项目继续开发，请先根据你的简要需求与模型沟通，之后拉紧边界明确需求。

## 章节

| 章节 | 主题 | 说明 |
| --- | --- | --- |
| [ch01](./ch01) | 单轮/多轮聊天客户端基础链路 | 打通配置加载、Provider 抽象、OpenAI-compatible / Anthropic 流式响应、thinking 展示和 Bubble Tea TUI。 |
| [ch02](./ch02) | 工具系统与 Agent 雏形 | 新增统一工具层，支持 Read、Write、Edit、Bash、Glob、Grep，并完成单轮工具调用、结果回灌和 Agent Loop 阻断。 |

