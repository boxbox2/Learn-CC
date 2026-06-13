package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mewcode/internal/tool"
)

type WriteTool struct{}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (WriteTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "Write",
		Description: "Create or overwrite a text file inside the current project. Confirm the path and intent before using.",
		Safety:      tool.SafetySideEffect,
		Parameters: objectSchema(map[string]any{
			"path":    stringProp("Path to write."),
			"content": stringProp("Complete file content."),
		}, "path", "content"),
	}
}

func (WriteTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	var args writeArgs
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		return tool.Failure("Write", req.ID, "invalid_arguments", err.Error())
	}
	path, err := req.PathPolicy.Resolve(args.Path)
	if err != nil {
		return tool.Failure("Write", req.ID, "invalid_path", err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return tool.Failure("Write", req.ID, "write_failed", err.Error())
	}
	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return tool.Failure("Write", req.ID, "write_failed", err.Error())
	}
	display := req.PathPolicy.DisplayPath(path)
	return tool.Success("Write", req.ID, fmt.Sprintf("Wrote %d bytes to %s", len(args.Content), display), map[string]any{
		"path":  display,
		"bytes": len(args.Content),
	})
}
