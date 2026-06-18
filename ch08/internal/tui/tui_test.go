package tui

import (
	"context"
	"strings"
	"testing"

	"mewcode/internal/chat"
	"mewcode/internal/command"
	commandbuiltin "mewcode/internal/command/builtin"
	"mewcode/internal/config"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	"mewcode/internal/sessionstore"
	"mewcode/internal/tool"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeRunner struct {
	submitCalled  int
	skillCalled   int
	retryCalled   int
	committed     string
	events        chan provider.StreamEvent
	inputs        []string
	skillNames    []string
	skillArgs     []string
	modes         []chat.SubmitMode
	compactCalled int
	resetCalled   int
	closeCalled   int
	status        chat.SessionStatus
	sessions      []sessionstore.Summary
	memory        command.MemoryStatus
	permission    command.PermissionStatus
}

func (f *fakeRunner) Submit(ctx context.Context, input string, mode chat.SubmitMode) (<-chan provider.StreamEvent, error) {
	f.submitCalled++
	f.inputs = append(f.inputs, input)
	f.modes = append(f.modes, mode)
	return f.events, nil
}

func (f *fakeRunner) SubmitSkill(ctx context.Context, name, args string) (<-chan provider.StreamEvent, error) {
	f.skillCalled++
	f.skillNames = append(f.skillNames, name)
	f.skillArgs = append(f.skillArgs, args)
	return f.events, nil
}

func (f *fakeRunner) Retry(ctx context.Context) (<-chan provider.StreamEvent, error) {
	f.retryCalled++
	return f.events, nil
}

func (f *fakeRunner) CommitAssistant(content string) {
	f.committed = content
}

func (f *fakeRunner) Compact(ctx context.Context) (<-chan provider.StreamEvent, error) {
	f.compactCalled++
	return f.events, nil
}

func (f *fakeRunner) ResetSession(ctx context.Context) error {
	f.resetCalled++
	return nil
}

func (f *fakeRunner) Status() chat.SessionStatus {
	return f.status
}

func (f *fakeRunner) SessionList() ([]sessionstore.Summary, error) {
	return f.sessions, nil
}

func (f *fakeRunner) MemoryStatus() command.MemoryStatus {
	return f.memory
}

func (f *fakeRunner) PermissionStatus() command.PermissionStatus {
	return f.permission
}

func (f *fakeRunner) ListCatalogSkills() []command.SkillSummary {
	return []command.SkillSummary{{Name: "review", Description: "Review changes"}}
}

func (f *fakeRunner) ReloadSkillCommands(ctx context.Context, reg *command.Registry) error {
	return nil
}

func (f *fakeRunner) Close() error {
	f.closeCalled++
	return nil
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

func TestSubmitRouting(t *testing.T) {
	m := newTestModel()
	runner := m.Runner.(*fakeRunner)
	runner.events = closedEvents()
	m.textarea.SetValue("   ")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if runner.submitCalled != 0 {
		t.Fatalf("empty input submitted")
	}
	m.textarea.SetValue("hello")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if runner.submitCalled != 1 || runner.inputs[0] != "hello" || runner.modes[0] != chat.SubmitModeDefault {
		t.Fatalf("submit calls=%d inputs=%+v modes=%+v", runner.submitCalled, runner.inputs, runner.modes)
	}
	m.Status = StatusIdle
	m.textarea.SetValue("/missing")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !strings.Contains(m.View(), "/help") {
		t.Fatalf("missing help guidance: %q", m.View())
	}
	m.textarea.SetValue("/plan")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.ChatMode != command.ChatModePlan || runner.submitCalled != 1 {
		t.Fatalf("plan mode=%s submit=%d", m.ChatMode, runner.submitCalled)
	}
	m.textarea.SetValue("/do")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.ChatMode != command.ChatModeDefault || runner.submitCalled != 1 {
		t.Fatalf("do mode=%s submit=%d", m.ChatMode, runner.submitCalled)
	}
}

func TestSkillCommandRunsSkill(t *testing.T) {
	m := newTestModel()
	runner := m.Runner.(*fakeRunner)
	runner.events = closedEvents()
	m.textarea.SetValue("/review internal/tui")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if runner.skillCalled != 1 || runner.skillNames[0] != "review" || runner.skillArgs[0] != "internal/tui" {
		t.Fatalf("skill calls=%d names=%+v args=%+v", runner.skillCalled, runner.skillNames, runner.skillArgs)
	}
}

func TestRunningGate(t *testing.T) {
	m := newTestModel()
	runner := m.Runner.(*fakeRunner)
	m.Status = StatusStreaming
	m.textarea.SetValue("/status")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !strings.Contains(m.View(), "status:") {
		t.Fatalf("status did not run: %q", m.View())
	}
	m.textarea.SetValue("/clear")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if runner.resetCalled != 0 || !strings.Contains(m.View(), "请等待当前任务完成") {
		t.Fatalf("clear gate reset=%d view=%q", runner.resetCalled, m.View())
	}
	m.textarea.SetValue("/review")
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if runner.skillCalled != 0 {
		t.Fatalf("review submitted while running")
	}
}

func TestExitCancelsRunningAndCloses(t *testing.T) {
	m := newTestModel()
	runner := m.Runner.(*fakeRunner)
	cancelled := false
	rootCancelled := false
	m.RootCancel = func() { rootCancelled = true }
	m.Status = StatusStreaming
	m.StreamCancel = func() { cancelled = true }
	prompt := permission.Prompt{ID: "1", Tool: "Write", Response: make(chan permission.UserGrant, 1)}
	m.ActivePermission = &prompt
	m.textarea.SetValue("/exit")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || !cancelled || !rootCancelled || runner.closeCalled != 1 {
		t.Fatalf("cmd=%v cancelled=%v root=%v close=%d", cmd, cancelled, rootCancelled, runner.closeCalled)
	}
	if got := <-prompt.Response; got != permission.GrantDeny {
		t.Fatalf("permission response = %s", got)
	}
}

func TestClearResetsVisibleState(t *testing.T) {
	m := newTestModel()
	runner := m.Runner.(*fakeRunner)
	m.ChatMode = command.ChatModePlan
	m.Output = []string{"old"}
	m.Current = UIMessage{Content: "current"}
	m.Progress = "working"
	m.LastError = "bad"
	m.CurrentTool = "Read"
	m.Usage = provider.Usage{TotalTokens: 10}
	m.textarea.SetValue("/clear")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if runner.resetCalled != 1 || strings.Contains(m.View(), "old") || !m.Usage.IsZero() || m.ChatMode != command.ChatModePlan {
		t.Fatalf("reset=%d usage=%+v mode=%s view=%q", runner.resetCalled, m.Usage, m.ChatMode, m.View())
	}
}

func TestCompletionKeys(t *testing.T) {
	m := newTestModel()
	runner := m.Runner.(*fakeRunner)
	m.textarea.SetValue("/st")
	m.refreshCompletion()
	if !m.Completion.Active || len(m.Completion.Items) != 1 {
		t.Fatalf("completion = %+v", m.Completion)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	if got := m.textarea.Value(); got != "/status" {
		t.Fatalf("tab value = %q", got)
	}
	if runner.submitCalled != 0 {
		t.Fatalf("tab executed command")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.Completion.Active || m.textarea.Value() != "/status" {
		t.Fatalf("esc completion=%+v value=%q", m.Completion, m.textarea.Value())
	}
	m.textarea.SetValue("/st")
	m.refreshCompletion()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !strings.Contains(m.View(), "status:") {
		t.Fatalf("enter did not execute status: %q", m.View())
	}
	m.textarea.SetValue("/zzz")
	m.refreshCompletion()
	if !m.Completion.NoMatch {
		t.Fatalf("expected no match")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !strings.Contains(m.View(), "/help") {
		t.Fatalf("no match enter view=%q", m.View())
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
	reg := command.NewRegistry()
	commandbuiltin.Register(reg)
	mustRegisterSkill(reg, "review")
	reg.MustValidate()
	return NewModel(cfg, &fakeRunner{}, &fakeRenderer{}, reg, nil)
}

func mustRegisterSkill(reg *command.Registry, name string) {
	err := reg.Register(command.Definition{
		Name:        "/" + name,
		Description: "Test skill",
		Kind:        command.KindPrompt,
		AcceptsArgs: true,
		SkillName:   name,
		Handler: func(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
			if err := c.RunSkill(ctx, name, inv.Args); err != nil {
				return command.Result{}, err
			}
			return command.Result{SentToAI: true}, nil
		},
	})
	if err != nil {
		panic(err)
	}
}

func closedEvents() chan provider.StreamEvent {
	ch := make(chan provider.StreamEvent)
	close(ch)
	return ch
}
