package command

import "testing"

func TestParseZeroArgumentCommands(t *testing.T) {
	reg := testRegistry(t)
	tests := []struct {
		input     string
		canonical string
	}{
		{"/help", "/help"},
		{"/HELP", "/help"},
		{"/Help", "/help"},
		{"/h", "/help"},
		{"/help   ", "/help"},
	}
	for _, tt := range tests {
		got := reg.Parse(tt.input)
		if got.Command == nil {
			t.Fatalf("Parse(%q) command = nil, result=%+v", tt.input, got)
		}
		if got.Command.Canonical != tt.canonical {
			t.Fatalf("Parse(%q) canonical = %q, want %q", tt.input, got.Command.Canonical, tt.canonical)
		}
	}
}

func TestParseChatAndEmpty(t *testing.T) {
	reg := testRegistry(t)
	if got := reg.Parse("   "); !got.Empty {
		t.Fatalf("empty parse = %+v", got)
	}
	got := reg.Parse("hello")
	if !got.Chat || got.Input != "hello" {
		t.Fatalf("chat parse = %+v", got)
	}
}

func TestParseRejectsTrailingText(t *testing.T) {
	reg := testRegistry(t)
	got := reg.Parse("/review internal/tui")
	if got.Unknown == "" || got.Command != nil {
		t.Fatalf("trailing text parse = %+v", got)
	}
}

func TestParseUnknownCommand(t *testing.T) {
	reg := testRegistry(t)
	got := reg.Parse("/missing")
	if got.Unknown != "/missing" {
		t.Fatalf("unknown = %+v", got)
	}
}

func TestCanExecute(t *testing.T) {
	tests := []struct {
		kind  Kind
		state AgentState
		want  bool
	}{
		{KindReadOnly, AgentStateIdle, true},
		{KindReadOnly, AgentStateRunning, true},
		{KindUI, AgentStateIdle, true},
		{KindUI, AgentStateRunning, false},
		{KindPrompt, AgentStateIdle, true},
		{KindPrompt, AgentStateRunning, false},
		{KindExit, AgentStateIdle, true},
		{KindExit, AgentStateRunning, true},
		{Kind("unknown"), AgentStateIdle, false},
	}
	for _, tt := range tests {
		if got := CanExecute(tt.kind, tt.state); got != tt.want {
			t.Fatalf("CanExecute(%s, %s) = %v, want %v", tt.kind, tt.state, got, tt.want)
		}
	}
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/help", Aliases: []string{"/h"}, Kind: KindReadOnly, Handler: noopHandler})
	mustRegister(t, reg, Definition{Name: "/review", Kind: KindPrompt, Handler: noopHandler})
	reg.MustValidate()
	return reg
}
