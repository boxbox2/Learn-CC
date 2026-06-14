package agent

import (
	"context"
	"fmt"
	"strings"

	"mewcode/internal/config"
	"mewcode/internal/permission"
	"mewcode/internal/prompt"
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
	Provider          provider.Provider
	Config            config.ProviderConfig
	Tools             *tool.Registry
	WorkDir           string
	Limits            tool.Limits
	Paths             tool.PathPolicy
	Options           Options
	Authorizer        permission.Authorizer
	PermissionManager *permission.Manager
}

type RunRequest struct {
	Messages    []provider.ChatMessage
	Mode        RunMode
	Tools       ToolSet
	PlanContext string
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
		allowedTools := AllowedDefinitions(r.Tools, toolSet)
		deferredToolNames := DeferredToolNames(r.Tools, toolSet)
		modelReq := provider.ChatRequest{
			SystemPrompt: prompt.BuildSystemPrompt(prompt.Options{}),
			SystemNotes:  systemNoteMessages(r.dynamicContext(req, allowedTools, deferredToolNames, iteration)),
			Messages:     append([]provider.ChatMessage(nil), messages...),
			Model:        r.Config.Model,
			Thinking:     r.Config.Thinking,
			Tools:        allowedTools,
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
			Authorizer: r.authorizer(out),
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

func (r *Runner) authorizer(out chan<- provider.StreamEvent) permission.Authorizer {
	if r.PermissionManager != nil {
		return r.PermissionManager.WithPrompter(eventPrompter{out: out})
	}
	return r.Authorizer
}

type eventPrompter struct {
	out chan<- provider.StreamEvent
}

func (p eventPrompter) Confirm(ctx context.Context, prompt permission.Prompt) permission.UserGrant {
	if prompt.Response == nil {
		prompt.Response = make(chan permission.UserGrant, 1)
	}
	if p.out == nil {
		return permission.GrantDeny
	}
	select {
	case p.out <- provider.StreamEvent{Type: provider.StreamEventTypePermissionRequest, Permission: &prompt}:
	case <-ctx.Done():
		return permission.GrantDeny
	}
	select {
	case grant := <-prompt.Response:
		return grant
	case <-ctx.Done():
		return permission.GrantDeny
	}
}

func (r *Runner) dynamicContext(req RunRequest, defs []tool.Definition, deferredToolNames []string, iteration int) prompt.DynamicContext {
	mode := string(req.Mode)
	if mode == "" {
		mode = prompt.ModeExecute
	}
	workDir := r.WorkDir
	if workDir == "" {
		workDir = r.Paths.Root
	}
	return prompt.DynamicContext{
		WorkDir:           workDir,
		Mode:              mode,
		Iteration:         iteration,
		ToolSummary:       toolSummary(defs),
		DeferredToolNames: deferredToolNames,
		PlanContext:       req.PlanContext,
	}
}

func systemNoteMessages(ctx prompt.DynamicContext) []provider.ChatMessage {
	notes := prompt.BuildSystemNotes(ctx, prompt.DefaultNotePolicy())
	messages := make([]provider.ChatMessage, 0, len(notes))
	for _, note := range notes {
		messages = append(messages, provider.ChatMessage{
			Role:    provider.RoleSystem,
			Content: note.TaggedContent(),
		})
	}
	return messages
}

func toolSummary(defs []tool.Definition) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
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
