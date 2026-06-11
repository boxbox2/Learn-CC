package agent

import (
	"context"
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
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
	if len(result.Messages) != 6 {
		t.Fatalf("messages = %+v", result.Messages)
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

func drainEvents(events <-chan provider.StreamEvent) {
	for range events {
	}
}
