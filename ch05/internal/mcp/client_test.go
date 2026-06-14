package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestClientInitializeSendsInitializedNotification(t *testing.T) {
	transport := newMemoryTransport()
	client := &Client{ServerName: "test", Transport: transport}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := <-transport.sent
		if req.Method != "initialize" {
			t.Errorf("first method = %s, want initialize", req.Method)
			return
		}
		transport.incoming <- rpcResult(req.ID, InitializeResult{
			ProtocolVersion: ProtocolVersion,
			ServerInfo:      Implementation{Name: "server", Version: "1"},
		})
		notify := <-transport.sent
		if notify.Method != "notifications/initialized" || notify.ID != nil {
			t.Errorf("notify = %+v, want initialized notification", notify)
		}
	}()
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
}

func TestClientListToolsPagination(t *testing.T) {
	transport := newMemoryTransport()
	client := initializedClient(t, transport)
	go func() {
		req := <-transport.sent
		if req.Method != "tools/list" {
			t.Errorf("method = %s, want tools/list", req.Method)
			return
		}
		transport.incoming <- rpcResult(req.ID, ListToolsResult{
			Tools:      []Tool{{Name: "a", Description: "A", InputSchema: map[string]any{"type": "object"}}},
			NextCursor: "next",
		})
		req = <-transport.sent
		var params ListToolsParams
		json.Unmarshal(req.Params, &params)
		if params.Cursor != "next" {
			t.Errorf("cursor = %q, want next", params.Cursor)
		}
		transport.incoming <- rpcResult(req.ID, ListToolsResult{
			Tools: []Tool{{Name: "b", Description: "B", InputSchema: map[string]any{"type": "object"}}},
		})
	}()
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 || tools[0].Name != "a" || tools[1].Name != "b" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestClientCallTool(t *testing.T) {
	transport := newMemoryTransport()
	client := initializedClient(t, transport)
	go func() {
		req := <-transport.sent
		if req.Method != "tools/call" {
			t.Errorf("method = %s, want tools/call", req.Method)
		}
		var params CallToolParams
		json.Unmarshal(req.Params, &params)
		if params.Name != "remote" {
			t.Errorf("name = %s, want remote", params.Name)
		}
		transport.incoming <- rpcResult(req.ID, ToolCallResult{Content: []ToolContent{{Type: "text", Text: "ok"}}})
	}()
	result, err := client.CallTool(context.Background(), "remote", []byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "ok" {
		t.Fatalf("result = %+v", result)
	}
}

func initializedClient(t *testing.T, transport *memoryTransport) *Client {
	t.Helper()
	client := &Client{ServerName: "test", Transport: transport, Session: NewSession(transport)}
	go client.Session.Run(context.Background())
	return client
}
