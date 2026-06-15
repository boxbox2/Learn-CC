package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Client struct {
	ServerName string
	Transport  Transport
	Session    *RPCSession

	ProtocolVersion string
	ServerInfo      Implementation
}

func (c *Client) Initialize(ctx context.Context) error {
	if c.Transport == nil {
		return fmt.Errorf("mcp transport is required")
	}
	if err := c.Transport.Start(ctx); err != nil {
		return err
	}
	c.Session = NewSession(c.Transport)
	go c.Session.Run(ctx)
	params := InitializeParams{
		ProtocolVersion: ProtocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo:      Implementation{Name: ClientName, Version: ClientVersion},
	}
	var result InitializeResult
	if err := c.Session.Call(ctx, "initialize", params, &result); err != nil {
		_ = c.Close(context.Background())
		return err
	}
	if strings.TrimSpace(result.ProtocolVersion) == "" {
		_ = c.Close(context.Background())
		return fmt.Errorf("mcp server %q returned empty protocolVersion", c.ServerName)
	}
	if strings.TrimSpace(result.ServerInfo.Name) == "" {
		_ = c.Close(context.Background())
		return fmt.Errorf("mcp server %q returned empty serverInfo.name", c.ServerName)
	}
	c.ProtocolVersion = result.ProtocolVersion
	c.ServerInfo = result.ServerInfo
	if err := c.Session.Notify(ctx, "notifications/initialized", map[string]any{}); err != nil {
		_ = c.Close(context.Background())
		return err
	}
	return nil
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	if c.Session == nil {
		return nil, fmt.Errorf("mcp client is not initialized")
	}
	var all []Tool
	cursor := ""
	for {
		var result ListToolsResult
		params := ListToolsParams{Cursor: cursor}
		if err := c.Session.Call(ctx, "tools/list", params, &result); err != nil {
			return nil, err
		}
		for _, t := range result.Tools {
			if strings.TrimSpace(t.Name) == "" {
				return nil, fmt.Errorf("mcp server %q returned tool without name", c.ServerName)
			}
			if len(t.InputSchema) == 0 {
				return nil, fmt.Errorf("mcp server %q tool %q has no inputSchema", c.ServerName, t.Name)
			}
			t.ServerName = c.ServerName
			all = append(all, t)
		}
		if strings.TrimSpace(result.NextCursor) == "" {
			break
		}
		cursor = result.NextCursor
	}
	return all, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (ToolCallResult, error) {
	if c.Session == nil {
		return ToolCallResult{}, fmt.Errorf("mcp client is not initialized")
	}
	if len(arguments) == 0 {
		arguments = []byte(`{}`)
	}
	params := CallToolParams{Name: name, Arguments: arguments}
	var result ToolCallResult
	if err := c.Session.Call(ctx, "tools/call", params, &result); err != nil {
		return ToolCallResult{}, err
	}
	raw, _ := json.Marshal(result)
	result.Raw = raw
	return result, nil
}

func (c *Client) Close(ctx context.Context) error {
	if c.Session != nil {
		return c.Session.Close(ctx)
	}
	if c.Transport != nil {
		return c.Transport.Close(ctx)
	}
	return nil
}
