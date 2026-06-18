package openai

import (
	"encoding/json"
	"testing"

	"mewcode/internal/provider"
	"mewcode/internal/tool"

	openaisdk "github.com/openai/openai-go/v3"
)

func TestToTools(t *testing.T) {
	tools := toTools([]tool.Definition{{
		Name:        "Read",
		Description: "Read file",
		Parameters:  tool.Schema{"type": "object"},
	}})
	data, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonContains(data, `"name":"Read"`) || !jsonContains(data, `"type":"function"`) {
		t.Fatalf("tool json = %s", data)
	}
}

func TestToMessagesExpandsToolResults(t *testing.T) {
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
			{ID: "call_2", Name: "Bash", Content: `{"ok":true}`},
		},
	}})
	data, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonContains(data, `"tool_call_id":"call_1"`) || !jsonContains(data, `"tool_call_id":"call_2"`) {
		t.Fatalf("messages json = %s", data)
	}
	if !jsonContains(data, `"tool_calls"`) {
		t.Fatalf("missing assistant tool calls: %s", data)
	}
}

func TestConvertChunkWithToolsStartAndDone(t *testing.T) {
	acc := newToolCallAccumulator()
	first := openaisdk.ChatCompletionChunk{Choices: []openaisdk.ChatCompletionChunkChoice{{
		Delta: openaisdk.ChatCompletionChunkChoiceDelta{ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{{
			Index: 0,
			ID:    "call_1",
			Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
				Name:      "Read",
				Arguments: `{"`,
			},
		}}},
	}}}
	events := convertChunkWithTools(first, acc)
	assertEvent(t, events, provider.StreamEventTypeToolCallStart, "")
	if events[len(events)-1].ToolCall.Name != "Read" {
		t.Fatalf("start call = %+v", events[len(events)-1].ToolCall)
	}
	second := openaisdk.ChatCompletionChunk{Choices: []openaisdk.ChatCompletionChunkChoice{{
		FinishReason: "tool_calls",
		Delta: openaisdk.ChatCompletionChunkChoiceDelta{ToolCalls: []openaisdk.ChatCompletionChunkChoiceDeltaToolCall{{
			Index: 0,
			Function: openaisdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
				Arguments: `path":"a.go"}`,
			},
		}}},
	}}}
	events = convertChunkWithTools(second, acc)
	assertEvent(t, events, provider.StreamEventTypeToolCallDone, "")
	done := events[len(events)-1]
	if len(done.ToolCalls) != 1 || done.ToolCalls[0].Arguments != `{"path":"a.go"}` {
		t.Fatalf("done calls = %+v", done.ToolCalls)
	}
}

func jsonContains(data []byte, want string) bool {
	return string(data) != "" && contains(string(data), want)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
