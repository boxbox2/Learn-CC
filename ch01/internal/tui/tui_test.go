package tui

import (
	"context"
	"strings"
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/provider"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeRunner struct {
	submitCalled int
	retryCalled  int
	committed    string
	events       chan provider.StreamEvent
}

func (f *fakeRunner) Submit(ctx context.Context, input string) (<-chan provider.StreamEvent, error) {
	f.submitCalled++
	return f.events, nil
}

func (f *fakeRunner) Retry(ctx context.Context) (<-chan provider.StreamEvent, error) {
	f.retryCalled++
	return f.events, nil
}

func (f *fakeRunner) CommitAssistant(content string) {
	f.committed = content
}

type fakeRenderer struct {
	called int
}

func (r *fakeRenderer) Render(markdown string, width int) (string, error) {
	r.called++
	return "rendered:" + markdown, nil
}

func TestInitialViewContainsInputAndStatus(t *testing.T) {
	m := newTestModel()
	view := m.View()
	if !strings.Contains(view, "Ask MewCode") {
		t.Fatalf("view missing input: %q", view)
	}
	if !strings.Contains(view, "active=deepseek") {
		t.Fatalf("view missing active provider: %q", view)
	}
}

func TestStreamTextThinkingUsageAndDone(t *testing.T) {
	m := newTestModel()
	runner := m.Runner.(*fakeRunner)
	renderer := m.Renderer.(*fakeRenderer)
	runner.events = make(chan provider.StreamEvent, 4)
	m.textarea.SetValue("hello")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	runner.events <- provider.StreamEvent{Type: provider.StreamEventTypeTextDelta, Delta: "hi"}
	updated, _ = m.Update(streamMsg(<-runner.events))
	m = updated.(Model)
	if !strings.Contains(m.View(), "hi") {
		t.Fatalf("text delta missing from view: %q", m.View())
	}
	runner.events <- provider.StreamEvent{Type: provider.StreamEventTypeThinkingDelta, Delta: "hidden-reasoning"}
	updated, _ = m.Update(streamMsg(<-runner.events))
	m = updated.(Model)
	if strings.Contains(m.View(), "hidden-reasoning") {
		t.Fatalf("thinking should be hidden by default: %q", m.View())
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	if !strings.Contains(m.View(), "hidden-reasoning") {
		t.Fatalf("thinking not shown after toggle: %q", m.View())
	}
	runner.events <- provider.StreamEvent{Type: provider.StreamEventTypeUsage, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}
	updated, _ = m.Update(streamMsg(<-runner.events))
	m = updated.(Model)
	if !strings.Contains(m.View(), "total=15") {
		t.Fatalf("usage missing from view: %q", m.View())
	}
	runner.events <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	updated, _ = m.Update(streamMsg(<-runner.events))
	m = updated.(Model)
	if runner.committed != "hi" {
		t.Fatalf("committed = %q, want hi", runner.committed)
	}
	if renderer.called != 1 {
		t.Fatalf("renderer calls = %d, want 1", renderer.called)
	}
}

func TestRetryAndCancel(t *testing.T) {
	m := newTestModel()
	runner := m.Runner.(*fakeRunner)
	runner.events = make(chan provider.StreamEvent)
	m.Status = StatusError
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)
	if runner.retryCalled != 1 {
		t.Fatalf("retry calls = %d, want 1", runner.retryCalled)
	}
	cancelled := false
	m.Status = StatusStreaming
	m.StreamCancel = func() { cancelled = true }
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)
	if !cancelled {
		t.Fatal("stream cancel was not called")
	}
	if m.Status != StatusIdle {
		t.Fatalf("status = %s, want idle", m.Status)
	}
}

func newTestModel() Model {
	cfg := config.AppConfig{
		Active: "deepseek",
		Providers: map[string]config.ProviderConfig{
			"deepseek": {
				Protocol: config.ProtocolOpenAI,
				Model:    "deepseek-reasoner",
				BaseURL:  "https://example.com",
				APIKey:   "secret",
				Thinking: config.ThinkingConfig{ShowByDefault: false},
			},
		},
	}
	return NewModel(cfg, &fakeRunner{}, &fakeRenderer{})
}
