package provider

import "mewcode/internal/tool"

type StreamEventType string

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

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func (u Usage) IsZero() bool {
	return u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0
}

func (u Usage) Add(v Usage) Usage {
	return Usage{
		PromptTokens:     u.PromptTokens + v.PromptTokens,
		CompletionTokens: u.CompletionTokens + v.CompletionTokens,
		TotalTokens:      u.TotalTokens + v.TotalTokens,
	}
}

type StreamEvent struct {
	Type       StreamEventType
	Delta      string
	Usage      Usage
	ErrorText  string
	ToolCall   *ToolCall
	ToolCalls  []ToolCall
	ToolResult *tool.Result
}
