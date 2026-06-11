package anthropic

import (
	"context"
	"errors"

	"mewcode/internal/provider"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
)

func (p *Provider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, error) {
	events := make(chan provider.StreamEvent)
	go func() {
		defer close(events)

		params := anthropicsdk.MessageNewParams{
			Model:     anthropicsdk.Model(req.Model),
			MaxTokens: 4096,
			Messages:  toMessages(req.Messages),
		}
		if len(req.Tools) > 0 {
			params.Tools = toTools(req.Tools)
		}
		if req.Thinking.Enabled {
			budget := req.Thinking.BudgetTokens
			if budget == 0 {
				budget = 1024
			}
			params.Thinking = anthropicsdk.ThinkingConfigParamUnion{
				OfEnabled: &anthropicsdk.ThinkingConfigEnabledParam{BudgetTokens: int64(budget)},
			}
		}

		stream := p.client.Messages.NewStreaming(ctx, params)
		var message anthropicsdk.Message
		for stream.Next() {
			event := stream.Current()
			if err := message.Accumulate(event); err != nil {
				events <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
				return
			}
			for _, converted := range convertEvent(event) {
				events <- converted
			}
		}
		if err := stream.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				events <- provider.StreamEvent{Type: provider.StreamEventTypeCancelled}
				return
			}
			events <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
			return
		}
		if calls := toolCallsFromMessage(message); len(calls) > 0 {
			events <- provider.StreamEvent{Type: provider.StreamEventTypeToolCallDone, ToolCalls: calls}
		}
		events <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	}()
	return events, nil
}

func toMessages(messages []provider.ChatMessage) []anthropicsdk.MessageParam {
	out := make([]anthropicsdk.MessageParam, 0, len(messages))
	for _, message := range messages {
		if len(message.ToolResults) > 0 {
			out = append(out, anthropicsdk.NewUserMessage(toolResultBlocks(message.ToolResults)...))
			continue
		}
		switch message.Role {
		case provider.RoleAssistant:
			if len(message.ToolCalls) > 0 {
				out = append(out, anthropicsdk.NewAssistantMessage(toolUseBlocks(message.ToolCalls)...))
			} else {
				out = append(out, anthropicsdk.NewAssistantMessage(anthropicsdk.NewTextBlock(message.Content)))
			}
		default:
			out = append(out, anthropicsdk.NewUserMessage(anthropicsdk.NewTextBlock(message.Content)))
		}
	}
	return out
}

func convertEvent(event anthropicsdk.MessageStreamEventUnion) []provider.StreamEvent {
	switch e := event.AsAny().(type) {
	case anthropicsdk.ContentBlockStartEvent:
		if toolUse, ok := e.ContentBlock.AsAny().(anthropicsdk.ToolUseBlock); ok {
			call := provider.ToolCall{ID: toolUse.ID, Name: toolUse.Name, Arguments: string(toolUse.Input)}
			return []provider.StreamEvent{{Type: provider.StreamEventTypeToolCallStart, ToolCall: &call}}
		}
	case anthropicsdk.ContentBlockDeltaEvent:
		switch delta := e.Delta.AsAny().(type) {
		case anthropicsdk.TextDelta:
			return []provider.StreamEvent{{Type: provider.StreamEventTypeTextDelta, Delta: delta.Text}}
		case anthropicsdk.ThinkingDelta:
			return []provider.StreamEvent{{Type: provider.StreamEventTypeThinkingDelta, Delta: delta.Thinking}}
		}
	case anthropicsdk.MessageDeltaEvent:
		if e.Usage.InputTokens != 0 || e.Usage.OutputTokens != 0 || e.Usage.CacheCreationInputTokens != 0 || e.Usage.CacheReadInputTokens != 0 {
			prompt := int(e.Usage.InputTokens + e.Usage.CacheCreationInputTokens + e.Usage.CacheReadInputTokens)
			completion := int(e.Usage.OutputTokens)
			return []provider.StreamEvent{{Type: provider.StreamEventTypeUsage, Usage: provider.Usage{
				PromptTokens:     prompt,
				CompletionTokens: completion,
				TotalTokens:      prompt + completion,
			}}}
		}
	}
	return nil
}
