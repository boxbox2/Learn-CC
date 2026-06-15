package openai

import (
	"sort"

	"mewcode/internal/provider"
	"mewcode/internal/tool"

	openaisdk "github.com/openai/openai-go/v3"
)

func toTools(defs []tool.Definition) []openaisdk.ChatCompletionToolUnionParam {
	out := make([]openaisdk.ChatCompletionToolUnionParam, 0, len(defs))
	for _, def := range defs {
		out = append(out, openaisdk.ChatCompletionFunctionTool(openaisdk.FunctionDefinitionParam{
			Name:        def.Name,
			Description: openaisdk.String(def.Description),
			Parameters:  openaisdk.FunctionParameters(def.Parameters),
		}))
	}
	return out
}

func assistantToolCallMessage(calls []provider.ToolCall) openaisdk.ChatCompletionMessageParamUnion {
	toolCalls := make([]openaisdk.ChatCompletionMessageToolCallUnionParam, 0, len(calls))
	for _, call := range calls {
		function := openaisdk.ChatCompletionMessageFunctionToolCallParam{
			ID: call.ID,
			Function: openaisdk.ChatCompletionMessageFunctionToolCallFunctionParam{
				Name:      call.Name,
				Arguments: call.Arguments,
			},
		}
		toolCalls = append(toolCalls, openaisdk.ChatCompletionMessageToolCallUnionParam{OfFunction: &function})
	}
	return openaisdk.ChatCompletionMessageParamUnion{
		OfAssistant: &openaisdk.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls},
	}
}

type toolCallAccumulator struct {
	calls   map[int64]*provider.ToolCall
	started map[int64]bool
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{
		calls:   map[int64]*provider.ToolCall{},
		started: map[int64]bool{},
	}
}

func (a *toolCallAccumulator) add(delta openaisdk.ChatCompletionChunkChoiceDeltaToolCall) []provider.StreamEvent {
	call, ok := a.calls[delta.Index]
	if !ok {
		call = &provider.ToolCall{}
		a.calls[delta.Index] = call
	}
	if delta.ID != "" {
		call.ID = delta.ID
	}
	if delta.Function.Name != "" {
		call.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		call.Arguments += delta.Function.Arguments
	}
	if !a.started[delta.Index] && (call.ID != "" || call.Name != "") {
		a.started[delta.Index] = true
		copy := *call
		return []provider.StreamEvent{{Type: provider.StreamEventTypeToolCallStart, ToolCall: &copy}}
	}
	return nil
}

func (a *toolCallAccumulator) done() []provider.ToolCall {
	indexes := make([]int64, 0, len(a.calls))
	for index := range a.calls {
		indexes = append(indexes, index)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })
	calls := make([]provider.ToolCall, 0, len(indexes))
	for _, index := range indexes {
		calls = append(calls, *a.calls[index])
	}
	return calls
}

func convertChunkWithTools(chunk openaisdk.ChatCompletionChunk, acc *toolCallAccumulator) []provider.StreamEvent {
	out := convertChunk(chunk)
	if len(chunk.Choices) == 0 {
		return out
	}
	choice := chunk.Choices[0]
	for _, call := range choice.Delta.ToolCalls {
		out = append(out, acc.add(call)...)
	}
	if choice.FinishReason == "tool_calls" {
		out = append(out, provider.StreamEvent{Type: provider.StreamEventTypeToolCallDone, ToolCalls: acc.done()})
	}
	return out
}
