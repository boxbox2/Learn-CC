package tui

import (
	"context"
	"strings"
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	"mewcode/internal/tool"

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
	runner.events <- provider.StreamEvent{Type: provider.StreamEventTypeUsage, Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, CachedTokens: 7}}
	updated, _ = m.Update(streamMsg(<-runner.events))
	m = updated.(Model)
	if !strings.Contains(m.View(), "total=15") || !strings.Contains(m.View(), "cached=7") {
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

func TestToolLineAndSummary(t *testing.T) {
	line := toolCallLine(provider.ToolCall{Name: "Read", Arguments: `{"path":"internal/chat/session.go"}`})
	if line != "● Read(internal/chat/session.go)" {
		t.Fatalf("tool line = %q", line)
	}
	bash := toolCallLine(provider.ToolCall{Name: "Bash", Arguments: `{"command":"go test ./..."}`})
	if bash != "● Bash(go test ./...)" {
		t.Fatalf("bash line = %q", bash)
	}
	summary := toolResultSummary(tool.Result{Tool: "Read", OK: true, Summary: "Read 10 bytes", Truncated: true, OriginalBytes: 100, ReturnedBytes: 10})
	if !strings.Contains(summary, "ok: Read 10 bytes") || !strings.Contains(summary, "truncated") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestPermissionQueueProcessesPromptsInOrder(t *testing.T) {
	m := newTestModel()
	first := permission.Prompt{ID: "1", Tool: "Write", Summary: "a.go", Reason: "confirm", Response: make(chan permission.UserGrant, 1)}
	second := permission.Prompt{ID: "2", Tool: "Bash", Summary: "go test ./...", Reason: "confirm", Response: make(chan permission.UserGrant, 1)}
	updated, _ := m.Update(streamMsg(provider.StreamEvent{Type: provider.StreamEventTypePermissionRequest, Permission: &first}))
	m = updated.(Model)
	updated, _ = m.Update(streamMsg(provider.StreamEvent{Type: provider.StreamEventTypePermissionRequest, Permission: &second}))
	m = updated.(Model)
	if m.ActivePermission == nil || m.ActivePermission.ID != "1" {
		t.Fatalf("active prompt = %#v, want first", m.ActivePermission)
	}
	if len(m.PermissionQueue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(m.PermissionQueue))
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	m = updated.(Model)
	if got := <-first.Response; got != permission.GrantOnce {
		t.Fatalf("first response = %s", got)
	}
	if m.ActivePermission == nil || m.ActivePermission.ID != "2" {
		t.Fatalf("active prompt = %#v, want second", m.ActivePermission)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = updated.(Model)
	if got := <-second.Response; got != permission.GrantDeny {
		t.Fatalf("second response = %s", got)
	}
	if m.ActivePermission != nil {
		t.Fatalf("active prompt = %#v, want nil", m.ActivePermission)
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
