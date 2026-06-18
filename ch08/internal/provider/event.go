package provider

import (
	"mewcode/internal/permission"
	"mewcode/internal/tool"
)

type StreamEventType string

const (
	StreamEventTypeTextDelta         StreamEventType = "text_delta"
	StreamEventTypeThinkingDelta     StreamEventType = "thinking_delta"
	StreamEventTypeToolCallStart     StreamEventType = "tool_call_start"
	StreamEventTypeToolCallDone      StreamEventType = "tool_call_done"
	StreamEventTypeToolResult        StreamEventType = "tool_result"
	StreamEventTypeUsage             StreamEventType = "usage"
	StreamEventTypeProgress          StreamEventType = "progress"
	StreamEventTypeContext           StreamEventType = "context"
	StreamEventTypePermissionRequest StreamEventType = "permission_request"
	StreamEventTypeDone              StreamEventType = "done"
	StreamEventTypeCancelled         StreamEventType = "cancelled"
	StreamEventTypeError             StreamEventType = "error"
)

type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CachedTokens     int
}

func (u Usage) IsZero() bool {
	return u.PromptTokens == 0 && u.CompletionTokens == 0 && u.TotalTokens == 0 && u.CachedTokens == 0
}

func (u Usage) Add(v Usage) Usage {
	return Usage{
		PromptTokens:     u.PromptTokens + v.PromptTokens,
		CompletionTokens: u.CompletionTokens + v.CompletionTokens,
		TotalTokens:      u.TotalTokens + v.TotalTokens,
		CachedTokens:     u.CachedTokens + v.CachedTokens,
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
	Progress   *Progress
	Context    *ContextEvent
	Permission *permission.Prompt
}

type Progress struct {
	Phase        string
	Iteration    int
	MaxIteration int
	Message      string
}

type ContextEvent struct {
	Phase               string
	Mode                string
	Message             string
	BeforeTokens        int
	AfterTokens         int
	ReplacedToolResults int
	ErrorText           string
}
