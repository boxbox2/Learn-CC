package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"mewcode/internal/tool"
)

type EditTool struct{}

type editArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (EditTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "Edit",
		Description: "Replace exactly one matching text fragment in a project file.",
		Parameters: objectSchema(map[string]any{
			"path":       stringProp("Path to edit."),
			"old_string": stringProp("Text that must match exactly once."),
			"new_string": stringProp("Replacement text."),
		}, "path", "old_string", "new_string"),
	}
}

func (EditTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	var args editArgs
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		return tool.Failure("Edit", req.ID, "invalid_arguments", err.Error())
	}
	path, err := req.PathPolicy.Resolve(args.Path)
	if err != nil {
		return tool.Failure("Edit", req.ID, "invalid_path", err.Error())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tool.Failure("Edit", req.ID, "read_failed", err.Error())
	}
	replaced, err := tool.ReplaceUnique(string(data), args.OldString, args.NewString)
	if err != nil {
		return tool.Failure("Edit", req.ID, "match_failed", err.Error())
	}
	if err := os.WriteFile(path, []byte(replaced.Content), 0o644); err != nil {
		return tool.Failure("Edit", req.ID, "write_failed", err.Error())
	}
	display := req.PathPolicy.DisplayPath(path)
	return tool.Success("Edit", req.ID, fmt.Sprintf("Edited %s", display), map[string]any{
		"path":       display,
		"normalized": replaced.Normalized,
		"old_bytes":  len(data),
		"new_bytes":  len(replaced.Content),
	})
}
