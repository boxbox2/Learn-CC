package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mewcode/internal/chat"
	"mewcode/internal/config"
	"mewcode/internal/provider"
)

type fakeProvider struct {
	requests []provider.ChatRequest
	events   []provider.StreamEvent
}

func (f *fakeProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, error) {
	f.requests = append(f.requests, req)
	ch := make(chan provider.StreamEvent, len(f.events))
	for _, event := range f.events {
		ch <- event
	}
	close(ch)
	return ch, nil
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
