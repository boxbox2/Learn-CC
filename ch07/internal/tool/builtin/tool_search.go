package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"mewcode/internal/tool"
)

type ToolSearch struct {
	Registry *tool.Registry
}

func (t ToolSearch) Definition() tool.Definition {
	return tool.Definition{
		Name:        "ToolSearch",
		Description: "Look up the full schema for a deferred tool by exact name so it can be used in the next model turn.",
		Parameters: objectSchema(map[string]any{
			"name": stringProp("Exact tool name to discover."),
		}, "name"),
		Safety: tool.SafetyReadOnly,
	}
}

func (t ToolSearch) Execute(ctx context.Context, req tool.Request) tool.Result {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		return tool.Failure("ToolSearch", req.ID, "invalid_arguments", err.Error())
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return tool.Failure("ToolSearch", req.ID, "invalid_arguments", "name is required")
	}
	if t.Registry == nil {
		return tool.Failure("ToolSearch", req.ID, "tools_unavailable", "tool registry is not configured")
	}
	def, ok := t.Registry.DefinitionByName(name)
	if !ok {
		return tool.Failure("ToolSearch", req.ID, "tool_not_found", "tool "+name+" is not registered")
	}
	discovered := t.Registry.MarkDiscovered(name)
	return tool.Success("ToolSearch", req.ID, "tool definition found", map[string]any{
		"name":        def.Name,
		"description": def.Description,
		"parameters":  def.Parameters,
		"deferred":    t.Registry.IsDeferred(name),
		"discovered":  discovered || t.Registry.IsDiscovered(name),
	})
}
