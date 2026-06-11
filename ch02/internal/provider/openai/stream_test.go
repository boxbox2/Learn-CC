package openai

import (
	"encoding/json"
	"testing"

	"mewcode/internal/provider"

	openaisdk "github.com/openai/openai-go/v3"
)

func TestConvertChunkTextReasoningAndUsage(t *testing.T) {
	raw := []byte(`{
		"id":"chunk",
		"object":"chat.completion.chunk",
		"created":1,
		"model":"deepseek-reasoner",
		"choices":[{"index":0,"finish_reason":"","delta":{"content":"hello","reasoning_content":"think"}}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`)
	var chunk openaisdk.ChatCompletionChunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		t.Fatal(err)
	}
	events := convertChunk(chunk)
	assertEvent(t, events, provider.StreamEventTypeUsage, "")
	assertEvent(t, events, provider.StreamEventTypeTextDelta, "hello")
	assertEvent(t, events, provider.StreamEventTypeThinkingDelta, "think")
	if events[0].Usage.TotalTokens != 15 {
		t.Fatalf("usage total = %d, want 15", events[0].Usage.TotalTokens)
	}
}

func assertEvent(t *testing.T, events []provider.StreamEvent, typ provider.StreamEventType, delta string) {
	t.Helper()
	for _, event := range events {
		if event.Type == typ && (delta == "" || event.Delta == delta) {
			return
		}
	}
	t.Fatalf("missing event type=%s delta=%q in %+v", typ, delta, events)
}
