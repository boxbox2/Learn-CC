package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

func toolCallLine(call provider.ToolCall) string {
	return fmt.Sprintf("● %s(%s)", call.Name, toolCallPreview(call))
}

func toolCallPreview(call provider.ToolCall) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return trimOneLine(call.Arguments, 80)
	}
	switch call.Name {
	case "Read", "Write", "Edit":
		if path, ok := args["path"].(string); ok {
			return trimOneLine(path, 80)
		}
	case "Bash":
		if command, ok := args["command"].(string); ok {
			return trimOneLine(command, 80)
		}
	case "Glob", "Grep":
		if pattern, ok := args["pattern"].(string); ok {
			return trimOneLine(pattern, 80)
		}
	}
	return trimOneLine(call.Arguments, 80)
}

func toolResultSummary(result tool.Result) string {
	status := "ok"
	if !result.OK {
		status = "failed"
	}
	summary := result.Summary
	if summary == "" && result.Error != nil {
		summary = result.Error.Message
	}
	if summary == "" {
		summary = result.Tool
	}
	if result.Error != nil && result.Error.Code == "permission_denied" {
		summary = "permission denied: " + summary
	}
	if result.Error != nil && result.Error.Code == "permission_hard_denied" {
		summary = "hard permission denied: " + summary
	}
	if result.Truncated {
		summary += fmt.Sprintf(" (truncated %d -> %d bytes)", result.OriginalBytes, result.ReturnedBytes)
	}
	return fmt.Sprintf("  %s: %s", status, trimOneLine(summary, 180))
}

func trimOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	limited := tool.LimitText(s, max)
	return limited.Text
}
