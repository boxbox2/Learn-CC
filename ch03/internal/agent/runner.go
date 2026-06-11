package agent

import (
	"context"
	"fmt"
	"strings"

	"mewcode/internal/config"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

type RunMode string

const (
	RunModeExecute RunMode = "execute"
	RunModePlan    RunMode = "plan"
)

type Options struct {
	MaxIterations         int
	MaxConsecutiveUnknown int
}

type Runner struct {
	Provider provider.Provider
	Config   config.ProviderConfig
	Tools    *tool.Registry
	WorkDir  string
	Limits   tool.Limits
	Paths    tool.PathPolicy
	Options  Options
}

type RunRequest struct {
	Messages []provider.ChatMessage
	Mode     RunMode
	Tools    ToolSet
}

type Run struct {
	Events <-chan provider.StreamEvent
	Done   <-chan Result
}

type Result struct {
	Messages []provider.ChatMessage
	Text     string
	Usage    provider.Usage
	Stop     StopReason
	Err      error
}

func (r *Runner) Run(ctx context.Context, req RunRequest) (*Run, error) {
	if r.Provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	out := make(chan provider.StreamEvent)
	done := make(chan Result, 1)
	go func() {
		defer close(out)
		defer close(done)
		done <- r.run(ctx, req, out)
	}()
	return &Run{Events: out, Done: done}, nil
}

func (r *Runner) run(ctx context.Context, req RunRequest, out chan<- provider.StreamEvent) Result {
	options := r.options()
	limits := r.Limits
	if limits == (tool.Limits{}) {
		limits = tool.DefaultLimits()
	}
	paths := r.Paths
	if paths.Root == "" {
		paths.Root = r.WorkDir
	}
	toolSet := req.Tools
	if toolSet.Mode == "" {
		toolSet.Mode = ToolSetAll
	}
	messages := append([]provider.ChatMessage(nil), req.Messages...)
	collector := &StreamCollector{}
	unknownStreak := 0

	for iteration := 1; iteration <= options.MaxIterations; iteration++ {
		if ctx.Err() != nil {
			out <- provider.StreamEvent{Type: provider.StreamEventTypeCancelled}
			return Result{Messages: messages, Usage: collector.TotalUsage, Stop: StopCancelled, Err: ctx.Err()}
		}
		out <- progressEvent("model_call", iteration, options.MaxIterations, "calling model")
		modelReq := provider.ChatRequest{
			Messages: append([]provider.ChatMessage(nil), messages...),
			Model:    r.Config.Model,
			Thinking: r.Config.Thinking,
			Tools:    AllowedDefinitions(r.Tools, toolSet),
		}
		events, err := r.Provider.StreamChat(ctx, modelReq)
		if err != nil {
			out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
			return Result{Messages: messages, Usage: collector.TotalUsage, Stop: StopStreamError, Err: err}
		}
		round, err := collector.Collect(ctx, events, out)
		if round.Stop == StopCancelled {
			return Result{Messages: messages, Usage: collector.TotalUsage, Stop: StopCancelled, Err: err}
		}
		if err != nil {
			return Result{Messages: messages, Usage: collector.TotalUsage, Stop: StopStreamError, Err: err}
		}
		if len(round.ToolCalls) == 0 {
			if strings.TrimSpace(round.Text) != "" {
				messages = append(messages, provider.ChatMessage{Role: provider.RoleAssistant, Content: round.Text})
			}
			out <- progressEvent("final", iteration, options.MaxIterations, "final answer")
			out <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
			return Result{Messages: messages, Text: round.Text, Usage: collector.TotalUsage, Stop: StopFinal}
		}

		messages = append(messages, provider.ChatMessage{
			Role:      provider.RoleAssistant,
			Content:   round.Text,
			ToolCalls: append([]provider.ToolCall(nil), round.ToolCalls...),
		})
		out <- progressEvent("tool_execution", iteration, options.MaxIterations, "executing tools")
		executor := ToolExecutor{
			Registry:   r.Tools,
			WorkingDir: r.WorkDir,
			PathPolicy: paths,
			Limits:     limits,
		}
		results := executor.ExecuteToolBatches(ctx, round.ToolCalls, out)
		resultMessages := make([]provider.ToolResultMessage, 0, len(results))
		for _, result := range results {
			resultMessages = append(resultMessages, provider.ToolResultMessage{
				ID:      result.CallID,
				Name:    result.Tool,
				Content: ToolResultContent(result, limits),
			})
		}
		messages = append(messages, provider.ChatMessage{Role: provider.RoleUser, ToolResults: resultMessages})
		if allResultsUnknown(results) {
			unknownStreak++
		} else {
			unknownStreak = 0
		}
		if unknownStreak >= options.MaxConsecutiveUnknown {
			err := fmt.Errorf("stopped after %d consecutive unknown tool rounds", unknownStreak)
			out <- progressEvent("stop", iteration, options.MaxIterations, "unknown tool limit reached")
			out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
			return Result{Messages: messages, Usage: collector.TotalUsage, Stop: StopUnknownToolLimit, Err: err}
		}
	}
	err := fmt.Errorf("stopped after %d iterations", options.MaxIterations)
	out <- progressEvent("stop", options.MaxIterations, options.MaxIterations, "iteration limit reached")
	out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
	return Result{Messages: messages, Usage: collector.TotalUsage, Stop: StopMaxIterations, Err: err}
}

func (r *Runner) options() Options {
	options := r.Options
	if options.MaxIterations <= 0 {
		options.MaxIterations = 8
	}
	if options.MaxConsecutiveUnknown <= 0 {
		options.MaxConsecutiveUnknown = 2
	}
	return options
}

func progressEvent(phase string, iteration, maxIteration int, message string) provider.StreamEvent {
	return provider.StreamEvent{
		Type: provider.StreamEventTypeProgress,
		Progress: &provider.Progress{
			Phase:        phase,
			Iteration:    iteration,
			MaxIteration: maxIteration,
			Message:      message,
		},
	}
}
