package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"mewcode/internal/tool"
)

type fakeToolClient struct {
	name string
	args json.RawMessage
	res  ToolCallResult
	err  error
}

func (f *fakeToolClient) CallTool(ctx context.Context, name string, arguments json.RawMessage) (ToolCallResult, error) {
	f.name = name
	f.args = arguments
	return f.res, f.err
}

func TestToolWrapperDefinitionAndExecute(t *testing.T) {
	client := &fakeToolClient{res: ToolCallResult{Content: []ToolContent{{Type: "text", Text: "hello"}}}}
	wrapper := ToolWrapper{
		RegisteredName: "mcp__docs__search",
		RemoteName:     "search",
		ServerName:     "docs",
		Client:         client,
		RemoteTool: Tool{
			Name:        "search",
			Description: "Search docs",
			InputSchema: tool.Schema{"type": "object"},
		},
	}
	def := wrapper.Definition()
	if def.Name != "mcp__docs__search" || def.Safety != tool.SafetySideEffect {
		t.Fatalf("definition = %+v", def)
	}
	if !wrapper.ShouldDefer() {
		t.Fatal("wrapper should defer")
	}
	result := wrapper.Execute(context.Background(), tool.Request{ID: "call-1", Arguments: []byte(`{"q":"x"}`)})
	if !result.OK || result.Summary != "hello" {
		t.Fatalf("result = %+v", result)
	}
	if client.name != "search" {
		t.Fatalf("remote name = %s, want search", client.name)
	}
}

func TestRegisteredToolNameSanitizes(t *testing.T) {
	if got := RegisteredToolName("my server", "do.thing"); got != "mcp__my_server__do_thing" {
		t.Fatalf("registered name = %s", got)
	}
}
