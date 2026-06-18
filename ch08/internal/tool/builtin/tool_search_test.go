package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"mewcode/internal/tool"
)

type deferredFakeTool struct{}

func (deferredFakeTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "DeferredRemote",
		Description: "remote",
		Parameters:  tool.Schema{"type": "object"},
		Safety:      tool.SafetySideEffect,
	}
}

func (deferredFakeTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success("DeferredRemote", req.ID, "ok", nil)
}

func (deferredFakeTool) ShouldDefer() bool {
	return true
}

func TestToolSearchDiscoversDeferredTool(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(deferredFakeTool{}); err != nil {
		t.Fatal(err)
	}
	search := ToolSearch{Registry: reg}
	args, _ := json.Marshal(map[string]string{"name": "DeferredRemote"})
	result := search.Execute(context.Background(), tool.Request{ID: "call-1", Arguments: args})
	if !result.OK {
		t.Fatalf("result = %+v, want ok", result)
	}
	if !reg.IsDiscovered("DeferredRemote") {
		t.Fatal("tool was not marked discovered")
	}
	if result.Data["name"] != "DeferredRemote" {
		t.Fatalf("data = %+v", result.Data)
	}
}

func TestToolSearchMissingTool(t *testing.T) {
	search := ToolSearch{Registry: tool.NewRegistry()}
	args, _ := json.Marshal(map[string]string{"name": "Missing"})
	result := search.Execute(context.Background(), tool.Request{ID: "call-1", Arguments: args})
	if result.OK || result.Error == nil || result.Error.Code != "tool_not_found" {
		t.Fatalf("result = %+v, want tool_not_found", result)
	}
}

func TestRegisterDefaultsIncludesToolSearch(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterDefaults(reg); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("ToolSearch"); !ok {
		t.Fatal("ToolSearch not registered")
	}
	if reg.IsDeferred("ToolSearch") {
		t.Fatal("ToolSearch should not be deferred")
	}
}
