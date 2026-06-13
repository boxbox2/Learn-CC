package openai

import (
	"encoding/json"
	"strings"
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
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"prompt_tokens_details":{"cached_tokens":4}}
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
	if events[0].Usage.CachedTokens != 4 {
		t.Fatalf("cached tokens = %d, want 4", events[0].Usage.CachedTokens)
	}
}

func TestConvertChunkWithoutUsageDoesNotEmitUsage(t *testing.T) {
	raw := []byte(`{
		"id":"chunk",
		"object":"chat.completion.chunk",
		"created":1,
		"model":"gpt",
		"choices":[{"index":0,"finish_reason":"","delta":{"content":"hello"}}]
	}`)
	var chunk openaisdk.ChatCompletionChunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		t.Fatal(err)
	}
	events := convertChunk(chunk)
	for _, event := range events {
		if event.Type == provider.StreamEventTypeUsage {
			t.Fatalf("unexpected usage event: %+v", event)
		}
	}
}

func TestConvertFinalUsageChunkWithoutChoices(t *testing.T) {
	raw := []byte(`{
		"id":"chunk",
		"object":"chat.completion.chunk",
		"created":1,
		"model":"gpt",
		"choices":[],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`)
	var chunk openaisdk.ChatCompletionChunk
	if err := json.Unmarshal(raw, &chunk); err != nil {
		t.Fatal(err)
	}
	events := convertChunk(chunk)
	if len(events) != 1 || events[0].Type != provider.StreamEventTypeUsage {
		t.Fatalf("events = %+v", events)
	}
}

func TestRequestMessagesSystemOrder(t *testing.T) {
	messages := requestMessages(provider.ChatRequest{
		SystemPrompt: "stable",
		SystemNotes: []provider.ChatMessage{
			{Role: provider.RoleSystem, Content: "note"},
		},
		Messages: []provider.ChatMessage{
			{Role: provider.RoleUser, Content: "user"},
		},
	})
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(messages))
	}
	encoded, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	stable := jsonIndex(text, "stable")
	note := jsonIndex(text, "note")
	user := jsonIndex(text, "user")
	if !(stable < note && note < user) {
		t.Fatalf("message order wrong: %s", text)
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

func jsonIndex(text, value string) int {
	idx := strings.Index(text, value)
	if idx < 0 {
		return len(text) + 1
	}
	return idx
}
