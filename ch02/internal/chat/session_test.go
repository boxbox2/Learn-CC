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

func TestSessionHistoryAndRetry(t *testing.T) {
	fp := &fakeProvider{}
	s := NewSession(fp, config.ProviderConfig{Model: "fake"})

	if _, err := s.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	s.CommitAssistant("world")
	if _, err := s.Submit(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if got := len(fp.requests[1].Messages); got != 3 {
		t.Fatalf("second request messages = %d, want 3", got)
	}
	if _, err := s.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(fp.requests); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
	if fp.requests[2].Messages[2].Content != "again" {
		t.Fatalf("retry used wrong request: %+v", fp.requests[2].Messages)
	}
}

type fakeTool struct{}

func (fakeTool) Definition() tool.Definition {
	return tool.Definition{Name: "Echo", Description: "echo", Parameters: tool.Schema{"type": "object"}}
}

func (fakeTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success("Echo", req.ID, "echo ok", map[string]any{"arguments": string(req.Arguments)})
}

type largeTool struct{}

func (largeTool) Definition() tool.Definition {
	return tool.Definition{Name: "Large", Description: "large", Parameters: tool.Schema{"type": "object"}}
}

func (largeTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success("Large", req.ID, "large ok", map[string]any{"content": strings.Repeat("x", 500)})
}

func TestSessionToolRoundTrip(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{}); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{
			{ID: "call_1", Name: "Echo", Arguments: `{"value":1}`},
			{ID: "call_2", Name: "Echo", Arguments: `{"value":2}`},
		}}},
		{
			{Type: provider.StreamEventTypeTextDelta, Delta: "done"},
			{Type: provider.StreamEventTypeDone},
		},
	}}
	s := NewSessionWithOptions(fp, config.ProviderConfig{Model: "fake"}, SessionOptions{
		Tools:      reg,
		WorkingDir: t.TempDir(),
		PathPolicy: tool.PathPolicy{Root: t.TempDir()},
		Limits:     tool.DefaultLimits(),
	})
	events, err := s.Submit(context.Background(), "use tools")
	if err != nil {
		t.Fatal(err)
	}
	var got []provider.StreamEventType
	for event := range events {
		got = append(got, event.Type)
	}
	if len(fp.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(fp.requests))
	}
	if len(fp.requests[0].Tools) != 1 {
		t.Fatalf("first request tools = %d, want 1", len(fp.requests[0].Tools))
	}
	if len(fp.requests[1].Tools) != 0 {
		t.Fatalf("second request tools = %d, want 0", len(fp.requests[1].Tools))
	}
	if len(s.History) != 3 {
		t.Fatalf("history = %+v", s.History)
	}
	if len(s.History[1].ToolCalls) != 2 || len(s.History[2].ToolResults) != 2 {
		t.Fatalf("tool history = %+v", s.History)
	}
	if !hasEvent(got, provider.StreamEventTypeToolResult) || !hasEvent(got, provider.StreamEventTypeTextDelta) {
		t.Fatalf("events = %+v", got)
	}
}

func TestSessionRejectsSecondToolRound(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{}); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "call_1", Name: "Echo", Arguments: `{}`}}}},
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "call_2", Name: "Echo", Arguments: `{}`}}}},
	}}
	s := NewSessionWithOptions(fp, config.ProviderConfig{Model: "fake"}, SessionOptions{Tools: reg})
	events, err := s.Submit(context.Background(), "again")
	if err != nil {
		t.Fatal(err)
	}
	var errorText string
	for event := range events {
		if event.Type == provider.StreamEventTypeError {
			errorText = event.ErrorText
		}
	}
	if errorText == "" {
		t.Fatal("expected second tool round error")
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
	events, err := s.Submit(context.Background(), "large")
	if err != nil {
		t.Fatal(err)
	}
	for range events {
	}
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

func hasEvent(events []provider.StreamEventType, want provider.StreamEventType) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
