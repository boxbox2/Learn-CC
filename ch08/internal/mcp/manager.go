package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"mewcode/internal/config"
	"mewcode/internal/tool"
)

type Diagnostic struct {
	Server  string
	Message string
}

type Manager struct {
	Config   config.MCPConfig
	Registry *tool.Registry
	Timeout  time.Duration

	mu          sync.Mutex
	clients     map[string]*Client
	diagnostics []Diagnostic

	newClient func(name string, cfg config.MCPServerConfig) (*Client, error)
}

func NewManager(cfg config.MCPConfig, registry *tool.Registry) *Manager {
	return &Manager{
		Config:   cfg,
		Registry: registry,
		Timeout:  10 * time.Second,
		clients:  map[string]*Client{},
	}
}

func (m *Manager) Start(ctx context.Context) error {
	if m.Registry == nil {
		return fmt.Errorf("tool registry is required")
	}
	type result struct {
		name   string
		client *Client
		tools  []Tool
		err    error
	}
	results := make(chan result)
	var wg sync.WaitGroup
	for name, server := range m.Config.Servers {
		name, server := name, server
		wg.Add(1)
		go func() {
			defer wg.Done()
			timeout := m.Timeout
			if timeout <= 0 {
				timeout = 10 * time.Second
			}
			serverCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			client, err := m.createClient(name, server)
			if err != nil {
				results <- result{name: name, err: err}
				return
			}
			if err := client.Initialize(serverCtx); err != nil {
				_ = client.Close(context.Background())
				results <- result{name: name, err: err}
				return
			}
			tools, err := client.ListTools(serverCtx)
			if err != nil {
				_ = client.Close(context.Background())
				results <- result{name: name, err: err}
				return
			}
			results <- result{name: name, client: client, tools: tools}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	for result := range results {
		if result.err != nil {
			m.addDiagnostic(result.name, result.err.Error())
			continue
		}
		registered := 0
		for _, remote := range result.tools {
			registeredName := RegisteredToolName(result.name, remote.Name)
			wrapper := ToolWrapper{
				RegisteredName: registeredName,
				RemoteName:     remote.Name,
				ServerName:     result.name,
				Client:         result.client,
				RemoteTool:     remote,
			}
			if err := m.Registry.Register(wrapper); err != nil {
				m.addDiagnostic(result.name, err.Error())
				continue
			}
			registered++
		}
		if registered > 0 {
			m.mu.Lock()
			m.clients[result.name] = result.client
			m.mu.Unlock()
		} else {
			_ = result.client.Close(context.Background())
		}
	}
	return nil
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	clients := make([]*Client, 0, len(m.clients))
	for _, client := range m.clients {
		clients = append(clients, client)
	}
	m.clients = map[string]*Client{}
	m.mu.Unlock()
	var first error
	for _, client := range clients {
		if err := client.Close(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (m *Manager) Diagnostics() []Diagnostic {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Diagnostic(nil), m.diagnostics...)
}

func (m *Manager) addDiagnostic(server, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diagnostics = append(m.diagnostics, Diagnostic{Server: server, Message: message})
}

func (m *Manager) createClient(name string, cfg config.MCPServerConfig) (*Client, error) {
	if m.newClient != nil {
		return m.newClient(name, cfg)
	}
	var transport Transport
	switch cfg.Type {
	case config.MCPTransportStdio:
		transport = &StdioTransport{Command: cfg.Command, Args: cfg.Args, Env: cfg.Env}
	case config.MCPTransportHTTP:
		transport = &HTTPTransport{URL: cfg.URL, Headers: cfg.Headers}
	default:
		return nil, fmt.Errorf("mcp server %q type %q is not supported", name, cfg.Type)
	}
	return &Client{ServerName: name, Transport: transport}, nil
}
