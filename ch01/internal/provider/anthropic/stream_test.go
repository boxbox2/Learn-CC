package anthropic

import (
	"encoding/json"
	"testing"

	"mewcode/internal/provider"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
)

func TestConvertEventTextThinkingAndUsage(t *testing.T) {
	textEvent := decodeEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`)
	thinkingEvent := decodeEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think"}}`)
	usageEvent := decodeEvent(t, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`)

	events := append(convertEvent(textEvent), convertEvent(thinkingEvent)...)
	events = append(events, convertEvent(usageEvent)...)

	assertAnthropicEvent(t, events, provider.StreamEventTypeTextDelta, "hello")
	assertAnthropicEvent(t, events, provider.StreamEventTypeThinkingDelta, "think")
	assertAnthropicEvent(t, events, provider.StreamEventTypeUsage, "")
	if events[2].Usage.TotalTokens != 15 {
		t.Fatalf("usage total = %d, want 15", events[2].Usage.TotalTokens)
	}
}

func decodeEvent(t *testing.T, raw string) anthropicsdk.MessageStreamEventUnion {
	t.Helper()
	var event anthropicsdk.MessageStreamEventUnion
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func assertAnthropicEvent(t *testing.T, events []provider.StreamEvent, typ provider.StreamEventType, delta string) {
	t.Helper()
	for _, event := range events {
		if event.Type == typ && (delta == "" || event.Delta == delta) {
			return
		}
	}
	t.Fatalf("missing event type=%s delta=%q in %+v", typ, delta, events)
}
