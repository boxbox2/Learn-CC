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
		for stream.Next() {
			event := stream.Current()
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
		events <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	}()
	return events, nil
}

func toMessages(messages []provider.ChatMessage) []anthropicsdk.MessageParam {
	out := make([]anthropicsdk.MessageParam, 0, len(messages))
	for _, message := range messages {
		block := anthropicsdk.NewTextBlock(message.Content)
		switch message.Role {
		case provider.RoleAssistant:
			out = append(out, anthropicsdk.NewAssistantMessage(block))
		default:
			out = append(out, anthropicsdk.NewUserMessage(block))
		}
	}
	return out
}

func convertEvent(event anthropicsdk.MessageStreamEventUnion) []provider.StreamEvent {
	switch e := event.AsAny().(type) {
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
