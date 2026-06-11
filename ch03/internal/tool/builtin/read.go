package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"mewcode/internal/tool"
)

type ReadTool struct{}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (ReadTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "Read",
		Description: "Read a text file inside the current project.",
		Safety:      tool.SafetyReadOnly,
		Parameters: objectSchema(map[string]any{
			"path":   stringProp("Path to the file to read."),
			"offset": intProp("Optional byte offset to start reading from."),
			"limit":  intProp("Optional maximum bytes to return."),
		}, "path"),
	}
}

func (ReadTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	var args readArgs
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		return tool.Failure("Read", req.ID, "invalid_arguments", err.Error())
	}
	path, err := req.PathPolicy.Resolve(args.Path)
	if err != nil {
		return tool.Failure("Read", req.ID, "invalid_path", err.Error())
	}
	info, err := os.Stat(path)
	if err != nil {
		return tool.Failure("Read", req.ID, "read_failed", err.Error())
	}
	if info.IsDir() {
		return tool.Failure("Read", req.ID, "path_is_directory", "Read requires a file path, not a directory")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tool.Failure("Read", req.ID, "read_failed", err.Error())
	}
	if args.Offset < 0 {
		return tool.Failure("Read", req.ID, "invalid_offset", "offset must be non-negative")
	}
	if args.Offset > len(data) {
		data = nil
	} else if args.Offset > 0 {
		data = data[args.Offset:]
	}
	limits := req.Limits
	maxBytes := limits.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = tool.DefaultLimits().MaxFileBytes
	}
	if args.Limit > 0 && args.Limit < maxBytes {
		maxBytes = args.Limit
	}
	limited := tool.LimitText(string(data), maxBytes)
	display := req.PathPolicy.DisplayPath(path)
	result := tool.Success("Read", req.ID, fmt.Sprintf("Read %d bytes from %s", limited.ReturnedBytes, display), map[string]any{
		"path":    display,
		"content": limited.Text,
		"size":    info.Size(),
	})
	result.Truncated = limited.Truncated
	result.OriginalBytes = limited.OriginalBytes
	result.ReturnedBytes = limited.ReturnedBytes
	return result
}
