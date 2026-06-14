package mcp

import (
	"context"
	"encoding/json"

	"mewcode/internal/tool"
)

const (
	ProtocolVersion = "2025-11-25"
	ClientName      = "MewCode"
	ClientVersion   = "0.1.0"
)

type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *JSONRPCError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type Transport interface {
	Start(ctx context.Context) error
	Send(ctx context.Context, msg JSONRPCMessage) error
	Receive(ctx context.Context) (JSONRPCMessage, error)
	Close(ctx context.Context) error
}

type Implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      Implementation `json:"clientInfo"`
}

type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      Implementation `json:"serverInfo"`
}

type ListToolsParams struct {
	Cursor string `json:"cursor,omitempty"`
}

type ListToolsResult struct {
	Tools      []Tool `json:"tools"`
	NextCursor string `json:"nextCursor,omitempty"`
}

type Tool struct {
	Name         string      `json:"name"`
	Title        string      `json:"title,omitempty"`
	Description  string      `json:"description,omitempty"`
	InputSchema  tool.Schema `json:"inputSchema"`
	OutputSchema tool.Schema `json:"outputSchema,omitempty"`
	ServerName   string      `json:"-"`
}

type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ToolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

type ToolCallResult struct {
	Content           []ToolContent   `json:"content,omitempty"`
	StructuredContent map[string]any  `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
	Raw               json.RawMessage `json:"-"`
}
