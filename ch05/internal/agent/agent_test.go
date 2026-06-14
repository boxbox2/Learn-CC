package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
	"mewcode/internal/tool/builtin"
)

type fakeProvider struct {
	requests  []provider.ChatRequest
	responses [][]provider.StreamEvent
}

func (f *fakeProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, error) {
	f.requests = append(f.requests, req)
	events := []provider.StreamEvent{{Type: provider.StreamEventTypeDone}}
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

type fakeTool struct {
	name   string
	safety tool.Safety
}

func (f fakeTool) Definition() tool.Definition {
	return tool.Definition{Name: f.name, Description: "fake", Parameters: tool.Schema{"type": "object"}, Safety: f.safety}
}

func (f fakeTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success(f.name, req.ID, "ok", nil)
}

type deferredAgentTool struct{}

func (deferredAgentTool) Definition() tool.Definition {
	return tool.Definition{Name: "DeferredRemote", Description: "remote", Parameters: tool.Schema{"type": "object"}, Safety: tool.SafetySideEffect}
}

func (deferredAgentTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success("DeferredRemote", req.ID, "ok", nil)
}

func (deferredAgentTool) ShouldDefer() bool {
	return true
}

func TestCollectorForwardsAndCollects(t *testing.T) {
	events := make(chan provider.StreamEvent, 5)
	events <- provider.StreamEvent{Type: provider.StreamEventTypeTextDelta, Delta: "hi "}
	events <- provider.StreamEvent{Type: provider.StreamEventTypeTextDelta, Delta: "there"}
	events <- provider.StreamEvent{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "1", Name: "Read"}}}
	events <- provider.StreamEvent{Type: provider.StreamEventTypeUsage, Usage: provider.Usage{TotalTokens: 3}}
	events <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	close(events)
	out := make(chan provider.StreamEvent, 5)
	result, err := (&StreamCollector{}).Collect(context.Background(), events, out)
	close(out)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "hi there" || len(result.ToolCalls) != 1 || result.Usage.TotalTokens != 3 {
		t.Fatalf("result = %+v", result)
	}
	var forwarded int
	for range out {
		forwarded++
	}
	if forwarded != 4 {
		t.Fatalf("forwarded events = %d, want 4", forwarded)
	}
}

func TestPlanToolBatchesSplitsUnknown(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{name: "Read", safety: tool.SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{name: "Grep", safety: tool.SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	batches := PlanToolBatches([]provider.ToolCall{
		{ID: "1", Name: "Read"},
		{ID: "2", Name: "MissingTool"},
		{ID: "3", Name: "Grep"},
	}, reg)
	if len(batches) != 3 {
		t.Fatalf("batches = %+v", batches)
	}
	if !batches[0].Parallel || batches[0].Calls[0].Name != "Read" {
		t.Fatalf("batch 0 = %+v", batches[0])
	}
	if batches[1].Parallel || batches[1].Calls[0].Name != "MissingTool" {
		t.Fatalf("batch 1 = %+v", batches[1])
	}
	if !batches[2].Parallel || batches[2].Calls[0].Name != "Grep" {
		t.Fatalf("batch 2 = %+v", batches[2])
	}
}

func TestRunnerLoopsUntilFinalText(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(fakeTool{name: "Read", safety: tool.SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "1", Name: "Read", Arguments: `{}`}}}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "2", Name: "Read", Arguments: `{}`}}}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeTextDelta, Delta: "done"}, {Type: provider.StreamEventTypeDone}},
	}}
	runner := &Runner{Provider: fp, Config: config.ProviderConfig{Model: "fake"}, Tools: reg, Options: Options{MaxIterations: 4}}
	run, err := runner.Run(context.Background(), RunRequest{
		Messages: []provider.ChatMessage{{Role: provider.RoleUser, Content: "go"}},
		Tools:    ToolSet{Mode: ToolSetAll},
	})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(run.Events)
	result := <-run.Done
	if result.Stop != StopFinal || result.Text != "done" {
		t.Fatalf("result = %+v", result)
	}
	if len(fp.requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(fp.requests))
	}
	if fp.requests[0].SystemPrompt == "" || len(fp.requests[0].SystemNotes) == 0 {
		t.Fatalf("missing system prompt or notes: %+v", fp.requests[0])
	}
	if len(result.Messages) != 6 {
		t.Fatalf("messages = %+v", result.Messages)
	}
	for _, message := range result.Messages {
		if message.Role == provider.RoleSystem {
			t.Fatalf("system note leaked into result messages: %+v", result.Messages)
		}
	}
}

func TestRunnerStopsOnUnknownToolLimit(t *testing.T) {
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "1", Name: "Missing", Arguments: `{}`}}}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "2", Name: "Missing", Arguments: `{}`}}}, {Type: provider.StreamEventTypeDone}},
	}}
	runner := &Runner{Provider: fp, Config: config.ProviderConfig{Model: "fake"}, Tools: tool.NewRegistry(), Options: Options{MaxIterations: 4, MaxConsecutiveUnknown: 2}}
	run, err := runner.Run(context.Background(), RunRequest{
		Messages: []provider.ChatMessage{{Role: provider.RoleUser, Content: "go"}},
		Tools:    ToolSet{Mode: ToolSetAll},
	})
	if err != nil {
		t.Fatal(err)
	}
	var sawError bool
	for event := range run.Events {
		if event.Type == provider.StreamEventTypeError {
			sawError = true
		}
	}
	result := <-run.Done
	if !sawError || result.Stop != StopUnknownToolLimit {
		t.Fatalf("sawError=%v result=%+v", sawError, result)
	}
}

func TestRunnerPlanModeSystemNoteFrequency(t *testing.T) {
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "1", Name: "Missing", Arguments: `{}`}}}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "2", Name: "Missing", Arguments: `{}`}}}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "3", Name: "Missing", Arguments: `{}`}}}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "4", Name: "Missing", Arguments: `{}`}}}, {Type: provider.StreamEventTypeDone}},
	}}
	runner := &Runner{
		Provider: fp,
		Config:   config.ProviderConfig{Model: "fake"},
		Tools:    tool.NewRegistry(),
		Options:  Options{MaxIterations: 4, MaxConsecutiveUnknown: 10},
	}
	run, err := runner.Run(context.Background(), RunRequest{
		Messages: []provider.ChatMessage{{Role: provider.RoleUser, Content: "plan"}},
		Mode:     RunModePlan,
		Tools:    ToolSet{Mode: ToolSetReadOnly},
	})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(run.Events)
	<-run.Done
	if len(fp.requests) != 4 {
		t.Fatalf("requests = %d, want 4", len(fp.requests))
	}
	want := []string{"plan_mode_full", "plan_mode_brief", "plan_mode_brief", "plan_mode_full"}
	for i, req := range fp.requests {
		if len(req.SystemNotes) < 2 {
			t.Fatalf("request %d notes = %+v", i, req.SystemNotes)
		}
		if !strings.Contains(req.SystemNotes[1].Content, `kind="`+want[i]+`"`) {
			t.Fatalf("request %d note = %q, want %s", i, req.SystemNotes[1].Content, want[i])
		}
	}
}

func TestRunnerPlanContextSystemNote(t *testing.T) {
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeTextDelta, Delta: "done"}, {Type: provider.StreamEventTypeDone}},
	}}
	runner := &Runner{Provider: fp, Config: config.ProviderConfig{Model: "fake"}}
	run, err := runner.Run(context.Background(), RunRequest{
		Messages:    []provider.ChatMessage{{Role: provider.RoleUser, Content: "execute"}},
		Mode:        RunModeExecute,
		Tools:       ToolSet{Mode: ToolSetAll},
		PlanContext: "approved plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(run.Events)
	result := <-run.Done
	if result.Stop != StopFinal {
		t.Fatalf("result = %+v", result)
	}
	var found bool
	for _, note := range fp.requests[0].SystemNotes {
		if strings.Contains(note.Content, `kind="plan_context"`) && strings.Contains(note.Content, "approved plan") {
			found = true
		}
	}
	if !found {
		t.Fatalf("plan context note missing: %+v", fp.requests[0].SystemNotes)
	}
	for _, message := range result.Messages {
		if strings.Contains(message.Content, "approved plan") {
			t.Fatalf("plan context leaked into result messages: %+v", result.Messages)
		}
	}
}

func TestRunnerDeferredToolSearchMakesToolVisibleNextRound(t *testing.T) {
	reg := tool.NewRegistry()
	if err := builtin.RegisterDefaults(reg); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(deferredAgentTool{}); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"name": "DeferredRemote"})
	fp := &fakeProvider{responses: [][]provider.StreamEvent{
		{{Type: provider.StreamEventTypeToolCallDone, ToolCalls: []provider.ToolCall{{ID: "1", Name: "ToolSearch", Arguments: string(args)}}}, {Type: provider.StreamEventTypeDone}},
		{{Type: provider.StreamEventTypeTextDelta, Delta: "done"}, {Type: provider.StreamEventTypeDone}},
	}}
	runner := &Runner{Provider: fp, Config: config.ProviderConfig{Model: "fake"}, Tools: reg, Options: Options{MaxIterations: 3}}
	run, err := runner.Run(context.Background(), RunRequest{
		Messages: []provider.ChatMessage{{Role: provider.RoleUser, Content: "use remote"}},
		Tools:    ToolSet{Mode: ToolSetAll},
	})
	if err != nil {
		t.Fatal(err)
	}
	drainEvents(run.Events)
	result := <-run.Done
	if result.Stop != StopFinal {
		t.Fatalf("result = %+v", result)
	}
	if len(fp.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(fp.requests))
	}
	if hasToolDefinition(fp.requests[0].Tools, "DeferredRemote") {
		t.Fatalf("first request unexpectedly included deferred tool schema: %+v", fp.requests[0].Tools)
	}
	var sawDeferredName bool
	for _, note := range fp.requests[0].SystemNotes {
		if strings.Contains(note.Content, "Searchable deferred tools: DeferredRemote") {
			sawDeferredName = true
		}
	}
	if !sawDeferredName {
		t.Fatalf("first request notes missing deferred tool name: %+v", fp.requests[0].SystemNotes)
	}
	if !hasToolDefinition(fp.requests[1].Tools, "DeferredRemote") {
		t.Fatalf("second request tools = %+v, want DeferredRemote", fp.requests[1].Tools)
	}
}

func drainEvents(events <-chan provider.StreamEvent) {
	for range events {
	}
}

func hasToolDefinition(defs []tool.Definition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
