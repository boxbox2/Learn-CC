package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
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
	if len(events) == 0 {
		events = []provider.StreamEvent{{Type: provider.StreamEventTypeDone}}
	}
	ch := make(chan provider.StreamEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch, nil
}

type fakeTool struct {
	name   string
	safety tool.Safety
}

func (f fakeTool) Definition() tool.Definition {
	return tool.Definition{Name: f.name, Description: "fake", Parameters: tool.Schema{"type": "object"}, Safety: f.safety}
}

func (f fakeTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success(f.name, req.ID, "ok", map[string]any{"arguments": string(req.Arguments)})
}

type largeTool struct{}

func (largeTool) Definition() tool.Definition {
	return tool.Definition{Name: "Large", Description: "large", Parameters: tool.Schema{"type": "object"}}
}

func (largeTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success("Large", req.ID, "large ok", map[string]any{"content": strings.Repeat("x", 500)})
}

func TestSessionHistoryAndRetry(t *testing.T) {
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeTextDelta, Delta: "world"}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeTextDelta, Delta: "again ok"}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeTextDelta, Delta: "retry ok"}, {Type: provider.StreamEventTypeDone}},
	}}
	s := NewSession(fp, config.ProviderConfig{Model: "fake"})

	drainSubmit(t, s, "hello")
	drainSubmit(t, s, "again")
	if got := len(fp.requests[1].Messages); got != 3 {
		t.Fatalf("second request messages = %d, want 3", got)
	}
	events, err := s.Retry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	drain(events)
	if got := len(fp.requests); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
	if fp.requests[2].Messages[2].Content != "again" {
		t.Fatalf("retry used wrong request: %+v", fp.requests[2].Messages)
	}
}

func TestSessionToolLoopContinues(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{name: "Echo", safety: tool.SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "Echo", Arguments: `{"value":1}`},
		}}},
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{
			{ID: "call_2", Name: "Echo", Arguments: `{"value":2}`},
		}}},
		{{Type: provider.StreamEventTypeTextDelta, Delta: "done"}, {Type: provider.StreamEventTypeDone}},
	}}
	s := NewSessionWithOptions(fp, config.ProviderConfig{Model: "fake"}, SessionOptions{Tools: reg, Limits: tool.DefaultLimits()})
	events, err := s.Submit(context.Background(), "use tools")
	if err != nil {
		t.Fatal(err)
	}
	var sawResults int
	for event := range events {
		if event.Type == provider.StreamEventTypeToolResult {
			sawResults++
		}
	}
	if len(fp.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(fp.requests))
	}
	if len(fp.requests[0].Tools) != 1 || len(fp.requests[1].Tools) != 1 {
		t.Fatalf("tool definitions should remain enabled across loop: %d %d", len(fp.requests[0].Tools), len(fp.requests[1].Tools))
	}
	if sawResults != 2 {
		t.Fatalf("tool results = %d, want 2", sawResults)
	}
	if len(s.History) != 6 {
		t.Fatalf("history = %+v", s.History)
	}
}

func TestSessionTruncatesToolResultBeforeHistory(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(largeTool{}); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "Large", Arguments: `{}`}}}},
		{{Type: provider.StreamEventTypeDone}},
	}}
	limits := tool.DefaultLimits()
	limits.MaxResultBytes = 160
	s := NewSessionWithOptions(fp, config.ProviderConfig{Model: "fake"}, SessionOptions{Tools: reg, Limits: limits})
	drainSubmit(t, s, "large")
	if len(fp.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(fp.requests))
	}
	results := fp.requests[1].Messages[len(fp.requests[1].Messages)-1].ToolResults
	if len(results) != 1 {
		t.Fatalf("tool results = %+v", results)
	}
	if len(results[0].Content) > limits.MaxResultBytes+200 {
		t.Fatalf("tool result content was not bounded: %d bytes", len(results[0].Content))
	}
	var decoded tool.Result
	if err := json.Unmarshal([]byte(results[0].Content), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Truncated || decoded.OriginalBytes == 0 || decoded.ReturnedBytes == 0 {
		t.Fatalf("truncated result = %+v", decoded)
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input string
		kind  CommandKind
		value string
	}{
		{"hello", CommandChat, "hello"},
		{"/planning notes", CommandChat, "/planning notes"},
		{"/done", CommandChat, "/done"},
		{"/plan fix it", CommandPlan, "fix it"},
		{"/do", CommandDo, ""},
	}
	for _, tt := range tests {
		cmd, err := ParseCommand(tt.input)
		if err != nil {
			t.Fatalf("ParseCommand(%q): %v", tt.input, err)
		}
		if cmd.Kind != tt.kind || cmd.Input != tt.value {
			t.Fatalf("ParseCommand(%q) = %+v", tt.input, cmd)
		}
	}
	for _, input := range []string{"", "/plan", "/do now"} {
		if _, err := ParseCommand(input); err == nil {
			t.Fatalf("ParseCommand(%q) expected error", input)
		}
	}
}

func TestPlanModeAndDo(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{name: "Read", safety: tool.SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{name: "Write", safety: tool.SafetySideEffect}); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeTextDelta, Delta: "plan text"}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeTextDelta, Delta: "done"}, {Type: provider.StreamEventTypeDone}},
	}}
	s := NewSessionWithOptions(fp, config.ProviderConfig{Model: "fake"}, SessionOptions{Tools: reg})
	drainSubmit(t, s, "/plan update docs")
	if s.LastPlan != "plan text" {
		t.Fatalf("LastPlan = %q", s.LastPlan)
	}
	if len(fp.requests[0].Tools) != 1 || fp.requests[0].Tools[0].Name != "Read" {
		t.Fatalf("plan tools = %+v", fp.requests[0].Tools)
	}
	if got := fp.requests[0].Messages[len(fp.requests[0].Messages)-1].Content; got != "update docs" {
		t.Fatalf("plan user message = %q", got)
	}
	if len(fp.requests[0].SystemNotes) < 2 || !strings.Contains(fp.requests[0].SystemNotes[1].Content, `kind="plan_mode_full"`) {
		t.Fatalf("plan system notes = %+v", fp.requests[0].SystemNotes)
	}
	drainSubmit(t, s, "/do")
	if len(fp.requests[1].Tools) != 2 {
		t.Fatalf("do tools = %+v", fp.requests[1].Tools)
	}
	last := fp.requests[1].Messages[len(fp.requests[1].Messages)-1].Content
	if strings.Contains(last, "plan text") || last != "Execute the approved plan." {
		t.Fatalf("do user message = %q", last)
	}
	var foundPlanContext bool
	for _, note := range fp.requests[1].SystemNotes {
		if strings.Contains(note.Content, `kind="plan_context"`) && strings.Contains(note.Content, "plan text") {
			foundPlanContext = true
		}
	}
	if !foundPlanContext {
		t.Fatalf("do system notes = %+v", fp.requests[1].SystemNotes)
	}
	for _, message := range s.History {
		if message.Role == provider.RoleSystem {
			t.Fatalf("system note leaked into history: %+v", s.History)
		}
	}
}

func TestChatClearsLastPlan(t *testing.T) {
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeTextDelta, Delta: "plan"}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeTextDelta, Delta: "chat"}, {Type: provider.StreamEventTypeDone}},
	}}
	s := NewSession(fp, config.ProviderConfig{Model: "fake"})
	drainSubmit(t, s, "/plan task")
	if s.LastPlan == "" {
		t.Fatal("expected plan")
	}
	drainSubmit(t, s, "ordinary chat")
	if s.LastPlan != "" {
		t.Fatalf("LastPlan should be cleared, got %q", s.LastPlan)
	}
	if _, err := s.Submit(context.Background(), "/do"); err == nil {
		t.Fatal("expected /do without active plan to fail")
	}
}

func drainSubmit(t *testing.T, s *Session, input string) {
	t.Helper()
	events, err := s.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	drain(events)
}

func drain(events <-chan provider.StreamEvent) {
	for range events {
	}
}
