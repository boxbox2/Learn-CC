package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewcode/internal/chat"
	"mewcode/internal/config"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
	"mewcode/internal/tool/builtin"
)

type fakeProvider struct {
	requests  []provider.ChatRequest
	events    []provider.StreamEvent
	responses [][]provider.StreamEvent
}

func (f *fakeProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, error) {
	f.requests = append(f.requests, req)
	events := f.events
	if len(f.responses) > 0 {
		events = f.responses[0]
		f.responses = f.responses[1:]
	}
	ch := make(chan provider.StreamEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

func TestToolReadRoundTrip(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "note.txt"), "hello from file")
	reg := tool.NewRegistry()
	if err := builtin.RegisterDefaults(reg); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{
			{ID: "call_read", Name: "Read", Arguments: `{"path":"note.txt"}`},
		}}},
		{
			{Type: provider.StreamEventTypeTextDelta, Delta: "I read it."},
			{Type: provider.StreamEventTypeDone},
		},
	}}
	session := chat.NewSessionWithOptions(fp, config.ProviderConfig{Model: "fake-model"}, chat.SessionOptions{
		Tools:      reg,
		WorkingDir: root,
		Limits:     tool.DefaultLimits(),
		PathPolicy: tool.PathPolicy{Root: root},
	})
	events, err := session.Submit(context.Background(), "read note")
	if err != nil {
		t.Fatal(err)
	}
	var content string
	var sawToolResult bool
	for event := range events {
		if event.Type == provider.StreamEventTypeToolResult {
			sawToolResult = true
			if event.ToolResult == nil || !event.ToolResult.OK {
				t.Fatalf("tool result = %+v", event.ToolResult)
			}
		}
		if event.Type == provider.StreamEventTypeTextDelta {
			content += event.Delta
		}
	}
	if !sawToolResult {
		t.Fatal("missing tool result event")
	}
	if content != "I read it." {
		t.Fatalf("content = %q", content)
	}
	if len(fp.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(fp.requests))
	}
	if len(fp.requests[1].Tools) != 0 {
		t.Fatalf("second request should not include tools")
	}
	if len(session.History) != 3 || len(session.History[2].ToolResults) != 1 {
		t.Fatalf("history = %+v", session.History)
	}
}

func TestFakeStreamingConversationAndRetry(t *testing.T) {
	fp := &fakeProvider{events: []provider.StreamEvent{
		{Type: provider.StreamEventTypeThinkingDelta, Delta: "thinking"},
		{Type: provider.StreamEventTypeTextDelta, Delta: "hello "},
		{Type: provider.StreamEventTypeTextDelta, Delta: "from fake model"},
		{Type: provider.StreamEventTypeUsage, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
		{Type: provider.StreamEventTypeDone},
	}}
	session := chat.NewSession(fp, config.ProviderConfig{Model: "fake-model", Thinking: config.ThinkingConfig{Enabled: true}})
	events, err := session.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	var content, thinking string
	var usage provider.Usage
	for event := range events {
		switch event.Type {
		case provider.StreamEventTypeTextDelta:
			content += event.Delta
		case provider.StreamEventTypeThinkingDelta:
			thinking += event.Delta
		case provider.StreamEventTypeUsage:
			usage = event.Usage
		}
	}
	if content != "hello from fake model" {
		t.Fatalf("content = %q", content)
	}
	if thinking != "thinking" {
		t.Fatalf("thinking = %q", thinking)
	}
	if usage.TotalTokens != 15 {
		t.Fatalf("usage total = %d", usage.TotalTokens)
	}
	session.CommitAssistant(content)
	if _, err := session.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fp.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(fp.requests))
	}
	if fp.requests[1].Messages[0].Content != "hello" {
		t.Fatalf("retry request input = %q", fp.requests[1].Messages[0].Content)
	}
}

func TestProjectConfigOverridesGlobalActive(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	mkdir(t, filepath.Join(home, ".mewcode"))
	mkdir(t, project)
	write(t, filepath.Join(home, ".mewcode", "config.yaml"), `
active: openai
providers:
  openai:
    protocol: openai
    model: gpt
    base_url: https://api.openai.com/v1
    api_key: secret
  deepseek:
    protocol: openai
    model: deepseek-chat
    base_url: https://api.deepseek.com
    api_key: secret
`)
	write(t, filepath.Join(project, "mewcode.yaml"), `
active: deepseek
`)
	cfg, err := config.LoadWithOptions(config.LoadOptions{HomeDir: home, ProjectDir: project})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active != "deepseek" {
		t.Fatalf("active = %q, want deepseek", cfg.Active)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
