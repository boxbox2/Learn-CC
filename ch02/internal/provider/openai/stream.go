package openai

import (
	"context"
	"encoding/json"
	"errors"

	"mewcode/internal/provider"

	openaisdk "github.com/openai/openai-go/v3"
)

func (p *Provider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, error) {
	events := make(chan provider.StreamEvent)
	go func() {
		defer close(events)

		params := openaisdk.ChatCompletionNewParams{
			Model:         openaisdk.ChatModel(req.Model),
			Messages:      toMessages(req.Messages),
			StreamOptions: openaisdk.ChatCompletionStreamOptionsParam{IncludeUsage: openaisdk.Bool(true)},
		}
		if len(req.Tools) > 0 {
			params.Tools = toTools(req.Tools)
			params.ParallelToolCalls = openaisdk.Bool(true)
		}
		stream := p.client.Chat.Completions.NewStreaming(ctx, params)
		toolCalls := newToolCallAccumulator()
		for stream.Next() {
			chunk := stream.Current()
			for _, event := range convertChunkWithTools(chunk, toolCalls) {
				events <- event
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

func convertChunk(chunk openaisdk.ChatCompletionChunk) []provider.StreamEvent {
	var out []provider.StreamEvent
	if chunk.Usage.PromptTokens != 0 || chunk.Usage.CompletionTokens != 0 || chunk.Usage.TotalTokens != 0 {
		out = append(out, provider.StreamEvent{Type: provider.StreamEventTypeUsage, Usage: provider.Usage{
			PromptTokens:     int(chunk.Usage.PromptTokens),
			CompletionTokens: int(chunk.Usage.CompletionTokens),
			TotalTokens:      int(chunk.Usage.TotalTokens),
		}})
	}
	if len(chunk.Choices) == 0 {
		return out
	}
	delta := chunk.Choices[0].Delta
	if delta.Content != "" {
		out = append(out, provider.StreamEvent{Type: provider.StreamEventTypeTextDelta, Delta: delta.Content})
	}
	if reasoning := reasoningDelta(delta.RawJSON()); reasoning != "" {
		out = append(out, provider.StreamEvent{Type: provider.StreamEventTypeThinkingDelta, Delta: reasoning})
	}
	return out
}

func toMessages(messages []provider.ChatMessage) []openaisdk.ChatCompletionMessageParamUnion {
	out := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, message := range messages {
		if len(message.ToolResults) > 0 {
			for _, result := range message.ToolResults {
				out = append(out, openaisdk.ToolMessage(result.Content, result.ID))
			}
			continue
		}
		switch message.Role {
		case provider.RoleAssistant:
			if len(message.ToolCalls) > 0 {
				out = append(out, assistantToolCallMessage(message.ToolCalls))
			} else {
				out = append(out, openaisdk.AssistantMessage(message.Content))
			}
		case provider.RoleSystem:
			out = append(out, openaisdk.SystemMessage(message.Content))
		default:
			out = append(out, openaisdk.UserMessage(message.Content))
		}
	}
	return out
}

func reasoningDelta(rawJSON string) string {
	var fields map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &fields); err != nil {
		return ""
	}
	for _, key := range []string{"reasoning_content", "reasoning", "reasoning_delta"} {
		if value, ok := fields[key].(string); ok {
			return value
		}
	}
	return ""
}
