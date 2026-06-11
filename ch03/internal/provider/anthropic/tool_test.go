package anthropic

import (
	"encoding/json"
	"testing"

	"mewcode/internal/provider"
	"mewcode/internal/tool"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
)

func TestToTools(t *testing.T) {
	tools := toTools([]tool.Definition{{
		Name:        "Read",
		Description: "Read file",
		Parameters:  tool.Schema{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}},
	}})
	data, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSON(data, `"name":"Read"`) || !containsJSON(data, `"input_schema"`) {
		t.Fatalf("tools json = %s", data)
	}
}

func TestToMessagesToolUseAndResults(t *testing.T) {
	msgs := toMessages([]provider.ChatMessage{{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "Read", Arguments: `{"path":"a.go"}`},
			{ID: "call_2", Name: "Bash", Arguments: `{"command":"go test ./..."}`},
		},
	}, {
		Role: provider.RoleUser,
		ToolResults: []provider.ToolResultMessage{
			{ID: "call_1", Name: "Read", Content: `{"ok":true}`},
			{ID: "call_2", Name: "Bash", Content: `{"ok":false}`},
		},
	}})
	data, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !containsJSON(data, `"tool_use"`) || !containsJSON(data, `"tool_result"`) {
		t.Fatalf("messages json = %s", data)
	}
	if !containsJSON(data, `"tool_use_id":"call_1"`) || !containsJSON(data, `"tool_use_id":"call_2"`) {
		t.Fatalf("missing tool result ids: %s", data)
	}
}

func TestConvertEventToolUseStartAndMessageDone(t *testing.T) {
	start := decodeEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}}`)
	events := convertEvent(start)
	assertAnthropicEvent(t, events, provider.StreamEventTypeToolCallStart, "")
	if events[0].ToolCall.Name != "Read" {
		t.Fatalf("tool call = %+v", events[0].ToolCall)
	}
	var message anthropicsdk.Message
	for _, raw := range []string{
		`{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","model":"claude","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.go\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_stop"}`,
	} {
		if err := message.Accumulate(decodeEvent(t, raw)); err != nil {
			t.Fatal(err)
		}
	}
	calls := toolCallsFromMessage(message)
	if len(calls) != 1 || calls[0].Arguments != `{"path":"a.go"}` {
		t.Fatalf("calls = %+v", calls)
	}
}

func containsJSON(data []byte, want string) bool {
	for i := 0; i+len(want) <= len(data); i++ {
		if string(data[i:i+len(want)]) == want {
			return true
		}
	}
	return false
}
