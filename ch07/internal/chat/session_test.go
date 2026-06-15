package chat

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"mewcode/internal/agent"
	"mewcode/internal/config"
	"mewcode/internal/contextmgr"
	"mewcode/internal/provider"
	"mewcode/internal/sessionstore"
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

func TestSessionArchivesUserAndAssistant(t *testing.T) {
	project := t.TempDir()
	archive, err := sessionstore.Create(project)
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeTextDelta, Delta: "world"}, {Type: provider.StreamEventTypeDone}},
	}}
	s := NewSessionWithOptions(fp, config.ProviderConfig{Model: "fake"}, SessionOptions{Archive: archive})
	drainSubmit(t, s, "hello")
	result, err := sessionstore.Restore(archive.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 || result.Messages[0].Content != "hello" || result.Messages[1].Content != "world" {
		t.Fatalf("restored = %+v", result.Messages)
	}
}

func TestSessionInitialHistoryAddsStaleNote(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	s := NewSessionWithOptions(&fakeProvider{}, config.ProviderConfig{Model: "fake"}, SessionOptions{
		InitialHistory: []provider.ChatMessage{{Role: provider.RoleUser, Content: "old"}},
		LastRestoredAt: now.Add(-25 * time.Hour),
		Now:            func() time.Time { return now },
	})
	if len(s.History) != 2 || s.History[1].Role != provider.RoleSystem || !strings.Contains(s.History[1].Content, "restored") {
		t.Fatalf("history = %+v", s.History)
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
	events, err := s.Submit(context.Background(), "use tools", SubmitModeDefault)
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

func TestSessionListListsArchives(t *testing.T) {
	project := t.TempDir()
	archive, err := sessionstore.Create(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Append(provider.ChatMessage{Role: provider.RoleUser, Content: "remember this"}); err != nil {
		t.Fatal(err)
	}
	s := NewSessionWithOptions(&fakeProvider{}, config.ProviderConfig{Model: "fake"}, SessionOptions{WorkingDir: project})
	summaries, err := s.SessionList()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != archive.ID() || summaries[0].Title != "remember this" {
		t.Fatalf("summaries = %+v", summaries)
	}
}

func TestPlanModeSubmitUsesReadOnlyTools(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{name: "Read", safety: tool.SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{name: "Write", safety: tool.SafetySideEffect}); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeTextDelta, Delta: "plan text"}, {Type: provider.StreamEventTypeDone}},
	}}
	s := NewSessionWithOptions(fp, config.ProviderConfig{Model: "fake"}, SessionOptions{Tools: reg})
	drainPlan(t, s, "update docs")
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
	drainPlan(t, s, "task")
	if s.LastPlan == "" {
		t.Fatal("expected plan")
	}
	drainSubmit(t, s, "ordinary chat")
	if s.LastPlan != "" {
		t.Fatalf("LastPlan should be cleared, got %q", s.LastPlan)
	}
}

func TestSlashInputIsOrdinaryChat(t *testing.T) {
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeTextDelta, Delta: "ok"}, {Type: provider.StreamEventTypeDone}},
	}}
	s := NewSession(fp, config.ProviderConfig{Model: "fake"})
	drainSubmit(t, s, "/status")
	if len(fp.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(fp.requests))
	}
	last := fp.requests[0].Messages[len(fp.requests[0].Messages)-1]
	if last.Content != "/status" {
		t.Fatalf("last message = %+v", last)
	}
}

func TestSessionStatus(t *testing.T) {
	project := t.TempDir()
	archive, err := sessionstore.Create(project)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSessionWithOptions(&fakeProvider{}, config.ProviderConfig{Model: "fake"}, SessionOptions{WorkingDir: project, Archive: archive})
	s.History = []provider.ChatMessage{{Role: provider.RoleUser, Content: "hello"}}
	s.LastPlan = "plan"
	status := s.Status()
	if status.ID != archive.ID() || status.Path != archive.Path() || status.MessageCount != 1 || !status.HasPlan {
		t.Fatalf("status = %+v", status)
	}
}

func TestResetSessionCreatesNewArchive(t *testing.T) {
	project := t.TempDir()
	archive, err := sessionstore.Create(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Append(provider.ChatMessage{Role: provider.RoleUser, Content: "old"}); err != nil {
		t.Fatal(err)
	}
	s := NewSessionWithOptions(&fakeProvider{}, config.ProviderConfig{Model: "fake"}, SessionOptions{WorkingDir: project, Archive: archive})
	s.History = []provider.ChatMessage{{Role: provider.RoleUser, Content: "old"}}
	s.LastPlan = "plan"
	s.LastRequest = &agent.RunRequest{}
	s.lastCommitted = "old"
	oldID := s.sessionID()
	oldPath := archive.Path()
	if err := s.ResetSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.sessionID() == "" || s.sessionID() == oldID {
		t.Fatalf("session id = %q old=%q", s.sessionID(), oldID)
	}
	if len(s.History) != 0 || s.LastPlan != "" || s.LastRequest != nil || s.lastCommitted != "" {
		t.Fatalf("session not reset: history=%+v plan=%q req=%+v committed=%q", s.History, s.LastPlan, s.LastRequest, s.lastCommitted)
	}
	if err := s.record(provider.ChatMessage{Role: provider.RoleUser, Content: "new"}); err != nil {
		t.Fatal(err)
	}
	oldData, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(oldData), "new") {
		t.Fatalf("old archive received new message: %s", oldData)
	}
	newData, err := os.ReadFile(s.sessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(newData), "new") {
		t.Fatalf("new archive missing new message: %s", newData)
	}
}

func TestSessionClose(t *testing.T) {
	project := t.TempDir()
	archive, err := sessionstore.Create(project)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSessionWithOptions(&fakeProvider{}, config.ProviderConfig{Model: "fake"}, SessionOptions{WorkingDir: project, Archive: archive})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCompactCommandUsesSummaryPath(t *testing.T) {
	fp := &fakeProvider{responses: [][]provider.StreamEvent{{
		{Type: provider.StreamEventTypeTextDelta, Delta: "<analysis>draft</analysis><summary>1. Main request and intent\nkeep going\n2. Key technical concepts\n3. Files and code snippets\n4. Errors and fixes\n5. Problem-solving process\n6. User messages, preserving original wording when possible\n7. TODOs\n8. Current work\n9. Possible next steps</summary>"},
		{Type: provider.StreamEventTypeDone},
	}}}
	cfg := config.ProviderConfig{Protocol: config.ProtocolOpenAI, Model: "fake", ContextWindow: 100000}
	manager, err := contextmgr.NewManager(t.TempDir(), cfg, fp)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSessionWithOptions(fp, cfg, SessionOptions{Context: manager})
	s.History = []provider.ChatMessage{
		{Role: provider.RoleUser, Content: "original task"},
		{Role: provider.RoleAssistant, Content: "working"},
	}
	events, err := s.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var sawContext bool
	for event := range events {
		if event.Type == provider.StreamEventTypeContext {
			sawContext = true
		}
	}
	if !sawContext {
		t.Fatal("expected context event")
	}
	if len(fp.requests) != 1 {
		t.Fatalf("provider requests = %d, want summary request only", len(fp.requests))
	}
	if len(fp.requests[0].Tools) != 0 {
		t.Fatalf("summary request tools = %+v, want none", fp.requests[0].Tools)
	}
	if len(s.History) < 2 || s.History[0].Role != provider.RoleSystem || !strings.Contains(s.History[0].Content, "Main request") {
		t.Fatalf("compacted history = %+v", s.History)
	}
}

func TestCompactReplacesArchive(t *testing.T) {
	project := t.TempDir()
	archive, err := sessionstore.Create(project)
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{{
		{Type: provider.StreamEventTypeTextDelta, Delta: "<analysis>draft</analysis><summary>1. Main request and intent\nkeep going\n2. Key technical concepts\n3. Files and code snippets\n4. Errors and fixes\n5. Problem-solving process\n6. User messages, preserving original wording when possible\n7. TODOs\n8. Current work\n9. Possible next steps</summary>"},
		{Type: provider.StreamEventTypeDone},
	}}}
	cfg := config.ProviderConfig{Protocol: config.ProtocolOpenAI, Model: "fake", ContextWindow: 100000}
	manager, err := contextmgr.NewManager(t.TempDir(), cfg, fp)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSessionWithOptions(fp, cfg, SessionOptions{Context: manager, Archive: archive})
	s.History = []provider.ChatMessage{{Role: provider.RoleUser, Content: "old"}}
	events, err := s.Compact(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	drain(events)
	data, err := os.ReadFile(archive.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "old") || !strings.Contains(string(data), "Main request") {
		t.Fatalf("archive = %s", data)
	}
}

func drainSubmit(t *testing.T, s *Session, input string) {
	t.Helper()
	events, err := s.Submit(context.Background(), input, SubmitModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	drain(events)
}

func drainPlan(t *testing.T, s *Session, input string) {
	t.Helper()
	events, err := s.Submit(context.Background(), input, SubmitModePlan)
	if err != nil {
		t.Fatal(err)
	}
	drain(events)
}

func drain(events <-chan provider.StreamEvent) {
	for range events {
	}
}
