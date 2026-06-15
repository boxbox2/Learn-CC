package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"mewcode/internal/tool"
)

type ToolClient interface {
	CallTool(ctx context.Context, name string, arguments json.RawMessage) (ToolCallResult, error)
}

type ToolWrapper struct {
	RegisteredName string
	RemoteName     string
	ServerName     string
	Client         ToolClient
	RemoteTool     Tool
}

func (w ToolWrapper) Definition() tool.Definition {
	description := strings.TrimSpace(w.RemoteTool.Description)
	if description == "" {
		description = strings.TrimSpace(w.RemoteTool.Title)
	}
	if description == "" {
		description = "MCP tool " + w.RemoteName + " from " + w.ServerName + "."
	}
	return tool.Definition{
		Name:        w.RegisteredName,
		Description: description,
		Parameters:  w.RemoteTool.InputSchema,
		Safety:      tool.SafetySideEffect,
	}
}

func (w ToolWrapper) ShouldDefer() bool {
	return true
}

func (w ToolWrapper) Execute(ctx context.Context, req tool.Request) tool.Result {
	if w.Client == nil {
		return tool.Failure(w.RegisteredName, req.ID, "mcp_client_unavailable", "mcp client is not available")
	}
	result, err := w.Client.CallTool(ctx, w.RemoteName, req.Arguments)
	if err != nil {
		return tool.Failure(w.RegisteredName, req.ID, "mcp_tool_call_failed", err.Error())
	}
	if result.IsError {
		return tool.Failure(w.RegisteredName, req.ID, "mcp_tool_error", summarizeToolResult(result))
	}
	return tool.Success(w.RegisteredName, req.ID, summarizeToolResult(result), map[string]any{
		"server":             w.ServerName,
		"remote_tool":        w.RemoteName,
		"content":            result.Content,
		"structured_content": result.StructuredContent,
	})
}

func summarizeToolResult(result ToolCallResult) string {
	for _, item := range result.Content {
		if strings.TrimSpace(item.Text) != "" {
			return item.Text
		}
	}
	if len(result.StructuredContent) > 0 {
		data, err := json.Marshal(result.StructuredContent)
		if err == nil {
			return string(data)
		}
	}
	return "mcp tool returned a result"
}

func RegisteredToolName(serverName, toolName string) string {
	return "mcp__" + sanitizeName(serverName) + "__" + sanitizeName(toolName)
}

func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "unnamed"
	}
	return b.String()
}
