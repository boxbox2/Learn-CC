package mcp

import (
	"context"
	"testing"
	"time"

	"mewcode/internal/config"
	"mewcode/internal/tool"
)

func TestManagerRegistersSuccessfulServersAndIsolatesFailures(t *testing.T) {
	reg := tool.NewRegistry()
	manager := NewManager(config.MCPConfig{Servers: map[string]config.MCPServerConfig{
		"ok":   {Type: config.MCPTransportHTTP, URL: "memory://ok"},
		"bad":  {Type: config.MCPTransportHTTP, URL: "memory://bad"},
		"slow": {Type: config.MCPTransportHTTP, URL: "memory://slow"},
	}}, reg)
	manager.Timeout = 20 * time.Millisecond
	manager.newClient = func(name string, cfg config.MCPServerConfig) (*Client, error) {
		transport := newMemoryTransport()
		switch name {
		case "ok":
			serveInitializeAndTools(t, transport, []Tool{{Name: "remote", Description: "Remote", InputSchema: tool.Schema{"type": "object"}}})
		case "bad":
			go func() {
				req := <-transport.sent
				transport.incoming <- JSONRPCMessage{JSONRPC: "2.0", ID: req.ID, Error: &JSONRPCError{Code: -32000, Message: "boom"}}
			}()
		case "slow":
			// Leave requests unanswered so the per-server timeout fires.
		}
		return &Client{ServerName: name, Transport: transport}, nil
	}

	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("mcp__ok__remote"); !ok {
		t.Fatal("successful server tool was not registered")
	}
	if _, ok := reg.Get("mcp__bad__remote"); ok {
		t.Fatal("failed server tool should not be registered")
	}
	diagnostics := manager.Diagnostics()
	if len(diagnostics) < 2 {
		t.Fatalf("diagnostics = %+v, want failures for bad and slow", diagnostics)
	}
}

func TestManagerDoesNotOverwriteExistingTool(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(managerFakeTool{name: "mcp__ok__remote"}); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config.MCPConfig{Servers: map[string]config.MCPServerConfig{
		"ok": {Type: config.MCPTransportHTTP, URL: "memory://ok"},
	}}, reg)
	manager.newClient = func(name string, cfg config.MCPServerConfig) (*Client, error) {
		transport := newMemoryTransport()
		serveInitializeAndTools(t, transport, []Tool{{Name: "remote", Description: "Remote", InputSchema: tool.Schema{"type": "object"}}})
		return &Client{ServerName: name, Transport: transport}, nil
	}
	if err := manager.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, ok := reg.Get("mcp__ok__remote")
	if !ok {
		t.Fatal("existing tool missing")
	}
	if _, ok := got.(managerFakeTool); !ok {
		t.Fatalf("existing tool was overwritten by %T", got)
	}
	if len(manager.Diagnostics()) == 0 {
		t.Fatal("expected conflict diagnostic")
	}
}

type managerFakeTool struct {
	name string
}

func (f managerFakeTool) Definition() tool.Definition {
	return tool.Definition{Name: f.name, Description: "fake", Parameters: tool.Schema{"type": "object"}}
}

func (f managerFakeTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success(f.name, req.ID, "ok", nil)
}

func serveInitializeAndTools(t *testing.T, transport *memoryTransport, tools []Tool) {
	t.Helper()
	go func() {
		req := <-transport.sent
		if req.Method != "initialize" {
			t.Errorf("method = %s, want initialize", req.Method)
			return
		}
		transport.incoming <- rpcResult(req.ID, InitializeResult{ProtocolVersion: ProtocolVersion, ServerInfo: Implementation{Name: "server", Version: "1"}})
		<-transport.sent // notifications/initialized
		req = <-transport.sent
		if req.Method != "tools/list" {
			t.Errorf("method = %s, want tools/list", req.Method)
			return
		}
		transport.incoming <- rpcResult(req.ID, ListToolsResult{Tools: tools})
	}()
}
