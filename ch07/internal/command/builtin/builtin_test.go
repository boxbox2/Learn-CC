package builtin

import (
	"context"
	"strings"
	"testing"

	"mewcode/internal/command"
	"mewcode/internal/provider"
)

type fakeController struct {
	mode       command.ChatMode
	visible    []command.Definition
	messages   []string
	sent       []string
	compacted  bool
	cleared    bool
	shutdown   bool
	session    command.SessionStatus
	memory     command.MemoryStatus
	permission command.PermissionStatus
	status     command.StatusSnapshot
}

func (f *fakeController) AgentState() command.AgentState { return command.AgentStateIdle }
func (f *fakeController) Mode() command.ChatMode         { return f.mode }
func (f *fakeController) SetMode(mode command.ChatMode)  { f.mode = mode }
func (f *fakeController) Usage() provider.Usage          { return f.status.Usage }
func (f *fakeController) VisibleCommands() []command.Definition {
	return append([]command.Definition(nil), f.visible...)
}
func (f *fakeController) ShowLocalMessage(msg string) { f.messages = append(f.messages, msg) }
func (f *fakeController) SendUserMessage(ctx context.Context, msg string) error {
	f.sent = append(f.sent, msg)
	return nil
}
func (f *fakeController) Compact(ctx context.Context) error {
	f.compacted = true
	return nil
}
func (f *fakeController) ClearAndResetSession(ctx context.Context) error {
	f.cleared = true
	return nil
}
func (f *fakeController) SessionStatus(ctx context.Context) (command.SessionStatus, error) {
	return f.session, nil
}
func (f *fakeController) MemoryStatus(ctx context.Context) command.MemoryStatus { return f.memory }
func (f *fakeController) PermissionStatus(ctx context.Context) command.PermissionStatus {
	return f.permission
}
func (f *fakeController) AppStatus(ctx context.Context) command.StatusSnapshot { return f.status }
func (f *fakeController) Shutdown(ctx context.Context) error {
	f.shutdown = true
	return nil
}

func TestRegisterBuiltins(t *testing.T) {
	reg := command.NewRegistry()
	Register(reg)
	reg.MustValidate()
	visible := reg.Visible()
	if len(visible) != 11 {
		t.Fatalf("visible commands = %d, want 11", len(visible))
	}
	for _, name := range []string{"/help", "/compact", "/clear", "/plan", "/do", "/session", "/memory", "/permission", "/status", "/review", "/exit"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Fatalf("missing %s", name)
		}
	}
}

func TestHelpListsVisibleCommands(t *testing.T) {
	reg := command.NewRegistry()
	Register(reg)
	reg.MustValidate()
	fc := &fakeController{visible: reg.Visible()}
	def, _ := reg.Lookup("/help")
	if _, err := def.Handler(context.Background(), command.Invocation{Definition: def}, fc); err != nil {
		t.Fatal(err)
	}
	out := strings.Join(fc.messages, "\n")
	if !strings.Contains(out, "/status") || !strings.Contains(out, "/exit") {
		t.Fatalf("help output = %q", out)
	}
}

func TestModeCommands(t *testing.T) {
	reg := command.NewRegistry()
	Register(reg)
	reg.MustValidate()
	fc := &fakeController{}
	def, _ := reg.Lookup("/plan")
	res, err := def.Handler(context.Background(), command.Invocation{Definition: def}, fc)
	if err != nil {
		t.Fatal(err)
	}
	if fc.mode != command.ChatModePlan || !res.ModeChanged || len(fc.sent) != 0 {
		t.Fatalf("plan mode=%s res=%+v sent=%+v", fc.mode, res, fc.sent)
	}
	def, _ = reg.Lookup("/do")
	res, err = def.Handler(context.Background(), command.Invocation{Definition: def}, fc)
	if err != nil {
		t.Fatal(err)
	}
	if fc.mode != command.ChatModeDefault || !res.ModeChanged || len(fc.sent) != 0 {
		t.Fatalf("do mode=%s res=%+v sent=%+v", fc.mode, res, fc.sent)
	}
}

func TestStatusCommandsAreReadOnly(t *testing.T) {
	reg := command.NewRegistry()
	Register(reg)
	reg.MustValidate()
	fc := &fakeController{
		session:    command.SessionStatus{ID: "s1", MessageCount: 2, Sessions: []command.SessionSummary{{ID: "s1", Title: "hello"}}},
		memory:     command.MemoryStatus{UserAvailable: true, ProjectAvailable: true},
		permission: command.PermissionStatus{Mode: "default"},
		status:     command.StatusSnapshot{Active: "test", Model: "fake", AgentState: command.AgentStateIdle, Mode: command.ChatModeDefault, Usage: provider.Usage{TotalTokens: 3}},
	}
	for _, name := range []string{"/session", "/memory", "/permission", "/status"} {
		def, _ := reg.Lookup(name)
		if _, err := def.Handler(context.Background(), command.Invocation{Definition: def}, fc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(fc.messages) != 4 || len(fc.sent) != 0 || fc.compacted || fc.cleared || fc.shutdown {
		t.Fatalf("controller state messages=%d sent=%d compact=%v clear=%v shutdown=%v", len(fc.messages), len(fc.sent), fc.compacted, fc.cleared, fc.shutdown)
	}
}

func TestCompactClearReviewExit(t *testing.T) {
	reg := command.NewRegistry()
	Register(reg)
	reg.MustValidate()
	fc := &fakeController{}
	for _, name := range []string{"/compact", "/clear", "/review", "/exit"} {
		def, _ := reg.Lookup(name)
		if _, err := def.Handler(context.Background(), command.Invocation{Definition: def}, fc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if !fc.compacted || !fc.cleared || !fc.shutdown {
		t.Fatalf("compact=%v clear=%v shutdown=%v", fc.compacted, fc.cleared, fc.shutdown)
	}
	if len(fc.sent) != 1 || !strings.Contains(fc.sent[0], "review") {
		t.Fatalf("sent = %+v", fc.sent)
	}
}
