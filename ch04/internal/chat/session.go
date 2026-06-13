package chat

import (
	"context"
	"fmt"
	"strings"

	"mewcode/internal/agent"
	"mewcode/internal/config"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

type Session struct {
	provider    provider.Provider
	cfg         config.ProviderConfig
	tools       *tool.Registry
	workDir     string
	limits      tool.Limits
	paths       tool.PathPolicy
	agent       *agent.Runner
	permissions *permission.Manager

	History     []provider.ChatMessage
	LastPlan    string
	LastRequest *agent.RunRequest

	lastCommitted string
}

func NewSession(p provider.Provider, cfg config.ProviderConfig) *Session {
	return NewSessionWithOptions(p, cfg, SessionOptions{Limits: tool.DefaultLimits()})
}

type SessionOptions struct {
	Tools       *tool.Registry
	WorkingDir  string
	Limits      tool.Limits
	PathPolicy  tool.PathPolicy
	Agent       *agent.Runner
	Permissions *permission.Manager
}

func NewSessionWithOptions(p provider.Provider, cfg config.ProviderConfig, opts SessionOptions) *Session {
	limits := opts.Limits
	if limits == (tool.Limits{}) {
		limits = tool.DefaultLimits()
	}
	paths := opts.PathPolicy
	if paths.Root == "" {
		paths.Root = opts.WorkingDir
	}
	s := &Session{
		provider:    p,
		cfg:         cfg,
		tools:       opts.Tools,
		workDir:     opts.WorkingDir,
		limits:      limits,
		paths:       paths,
		agent:       opts.Agent,
		permissions: opts.Permissions,
	}
	if s.agent == nil {
		s.agent = &agent.Runner{
			Provider:          p,
			Config:            cfg,
			Tools:             opts.Tools,
			WorkDir:           opts.WorkingDir,
			Limits:            limits,
			Paths:             paths,
			PermissionManager: opts.Permissions,
		}
	}
	return s
}

type CommandKind string

const (
	CommandChat CommandKind = "chat"
	CommandPlan CommandKind = "plan"
	CommandDo   CommandKind = "do"
)

type Command struct {
	Kind  CommandKind
	Input string
}

func ParseCommand(input string) (Command, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Command{}, fmt.Errorf("input is required")
	}
	if input == "/plan" || strings.HasPrefix(input, "/plan ") {
		rest := strings.TrimSpace(strings.TrimPrefix(input, "/plan"))
		if rest == "" {
			return Command{}, fmt.Errorf("/plan requires a task")
		}
		return Command{Kind: CommandPlan, Input: rest}, nil
	}
	if input == "/do" || strings.HasPrefix(input, "/do ") {
		rest := strings.TrimSpace(strings.TrimPrefix(input, "/do"))
		if rest != "" {
			return Command{}, fmt.Errorf("/do does not accept a new task in this stage")
		}
		return Command{Kind: CommandDo}, nil
	}
	return Command{Kind: CommandChat, Input: input}, nil
}

func (s *Session) Submit(ctx context.Context, input string) (<-chan provider.StreamEvent, error) {
	command, err := ParseCommand(input)
	if err != nil {
		return nil, err
	}
	switch command.Kind {
	case CommandPlan:
		return s.submitPlan(ctx, command.Input)
	case CommandDo:
		return s.submitDo(ctx)
	default:
		return s.submitChat(ctx, command.Input)
	}
}

func (s *Session) Retry(ctx context.Context) (<-chan provider.StreamEvent, error) {
	if s.LastRequest == nil {
		return nil, fmt.Errorf("no request to retry")
	}
	req := *s.LastRequest
	req.Messages = append([]provider.ChatMessage(nil), s.LastRequest.Messages...)
	savePlan := ""
	if req.Mode == agent.RunModePlan {
		savePlan = "plan"
	}
	return s.run(ctx, req, savePlan)
}

func (s *Session) CommitAssistant(content string) {
	content = strings.TrimSpace(content)
	if content == "" || content == s.lastCommitted {
		return
	}
	s.History = append(s.History, provider.ChatMessage{Role: provider.RoleAssistant, Content: content})
	s.lastCommitted = content
}

func (s *Session) submitChat(ctx context.Context, input string) (<-chan provider.StreamEvent, error) {
	s.LastPlan = ""
	messages := append([]provider.ChatMessage(nil), s.History...)
	messages = append(messages, provider.ChatMessage{Role: provider.RoleUser, Content: input})
	req := agent.RunRequest{Messages: messages, Mode: agent.RunModeExecute, Tools: agent.ToolSet{Mode: agent.ToolSetAll}}
	return s.run(ctx, req, "")
}

func (s *Session) submitPlan(ctx context.Context, input string) (<-chan provider.StreamEvent, error) {
	messages := append([]provider.ChatMessage(nil), s.History...)
	messages = append(messages, provider.ChatMessage{Role: provider.RoleUser, Content: input})
	req := agent.RunRequest{Messages: messages, Mode: agent.RunModePlan, Tools: agent.ToolSet{Mode: agent.ToolSetReadOnly}}
	return s.run(ctx, req, "plan")
}

func (s *Session) submitDo(ctx context.Context) (<-chan provider.StreamEvent, error) {
	if strings.TrimSpace(s.LastPlan) == "" {
		return nil, fmt.Errorf("no active plan; run /plan <task> first")
	}
	messages := append([]provider.ChatMessage(nil), s.History...)
	messages = append(messages, provider.ChatMessage{Role: provider.RoleUser, Content: "Execute the approved plan."})
	req := agent.RunRequest{Messages: messages, Mode: agent.RunModeExecute, Tools: agent.ToolSet{Mode: agent.ToolSetAll}, PlanContext: s.LastPlan}
	return s.run(ctx, req, "")
}

func (s *Session) run(ctx context.Context, req agent.RunRequest, savePlan string) (<-chan provider.StreamEvent, error) {
	run, err := s.agent.Run(ctx, req)
	if err != nil {
		return nil, err
	}
	s.LastRequest = &agent.RunRequest{
		Messages:    append([]provider.ChatMessage(nil), req.Messages...),
		Mode:        req.Mode,
		Tools:       req.Tools,
		PlanContext: req.PlanContext,
	}
	out := make(chan provider.StreamEvent)
	go func() {
		defer close(out)
		var terminal *provider.StreamEvent
		for event := range run.Events {
			switch event.Type {
			case provider.StreamEventTypeDone, provider.StreamEventTypeCancelled, provider.StreamEventTypeError:
				copy := event
				terminal = &copy
			default:
				out <- event
			}
		}
		result, ok := <-run.Done
		if !ok {
			return
		}
		if len(result.Messages) > 0 {
			s.History = append([]provider.ChatMessage(nil), result.Messages...)
		}
		if result.Stop == agent.StopFinal {
			s.lastCommitted = strings.TrimSpace(result.Text)
			if savePlan == "plan" {
				s.LastPlan = strings.TrimSpace(result.Text)
			}
		}
		if terminal != nil {
			out <- *terminal
		}
	}()
	return out, nil
}
