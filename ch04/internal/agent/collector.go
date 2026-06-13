package agent

import (
	"context"
	"errors"

	"mewcode/internal/provider"
)

type StopReason string

const (
	StopFinal            StopReason = "final"
	StopMaxIterations    StopReason = "max_iterations"
	StopCancelled        StopReason = "cancelled"
	StopStreamError      StopReason = "stream_error"
	StopUnknownToolLimit StopReason = "unknown_tool_limit"
)

type RoundResult struct {
	Text      string
	Thinking  string
	ToolCalls []provider.ToolCall
	Usage     provider.Usage
	Stop      StopReason
	Messages  []provider.ChatMessage
}

type StreamCollector struct {
	TotalUsage provider.Usage
}

func (c *StreamCollector) Collect(ctx context.Context, events <-chan provider.StreamEvent, out chan<- provider.StreamEvent) (RoundResult, error) {
	var result RoundResult
	for {
		select {
		case <-ctx.Done():
			forward(out, provider.StreamEvent{Type: provider.StreamEventTypeCancelled})
			result.Stop = StopCancelled
			return result, ctx.Err()
		case event, ok := <-events:
			if !ok {
				result.Stop = StopFinal
				return result, nil
			}
			switch event.Type {
			case provider.StreamEventTypeTextDelta:
				result.Text += event.Delta
				forward(out, event)
			case provider.StreamEventTypeThinkingDelta:
				result.Thinking += event.Delta
				forward(out, event)
			case provider.StreamEventTypeToolCallStart:
				forward(out, event)
			case provider.StreamEventTypeToolCallDone:
				result.ToolCalls = append([]provider.ToolCall(nil), event.ToolCalls...)
				forward(out, event)
			case provider.StreamEventTypeUsage:
				result.Usage = result.Usage.Add(event.Usage)
				c.TotalUsage = c.TotalUsage.Add(event.Usage)
				forward(out, provider.StreamEvent{Type: provider.StreamEventTypeUsage, Usage: c.TotalUsage})
			case provider.StreamEventTypeCancelled:
				forward(out, event)
				result.Stop = StopCancelled
				return result, context.Canceled
			case provider.StreamEventTypeError:
				forward(out, event)
				result.Stop = StopStreamError
				return result, errors.New(event.ErrorText)
			case provider.StreamEventTypeDone:
				result.Stop = StopFinal
				return result, nil
			default:
				forward(out, event)
			}
		}
	}
}

func forward(out chan<- provider.StreamEvent, event provider.StreamEvent) {
	if out != nil {
		out <- event
	}
}
