package chat

import (
	"context"
	"fmt"
	"strings"

	"mewcode/internal/config"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

type Session struct {
	provider provider.Provider
	cfg      config.ProviderConfig
	tools    *tool.Registry
	workDir  string
	limits   tool.Limits
	paths    tool.PathPolicy

	History     []provider.ChatMessage
	LastRequest *provider.ChatRequest
}

func NewSession(p provider.Provider, cfg config.ProviderConfig) *Session {
	return &Session{provider: p, cfg: cfg, limits: tool.DefaultLimits()}
}

type SessionOptions struct {
	Tools      *tool.Registry
	WorkingDir string
	Limits     tool.Limits
	PathPolicy tool.PathPolicy
}

func NewSessionWithOptions(p provider.Provider, cfg config.ProviderConfig, opts SessionOptions) *Session {
	s := NewSession(p, cfg)
	s.tools = opts.Tools
	s.workDir = opts.WorkingDir
	s.limits = opts.Limits
	if s.limits == (tool.Limits{}) {
		s.limits = tool.DefaultLimits()
	}
	s.paths = opts.PathPolicy
	if s.paths.Root == "" {
		s.paths.Root = opts.WorkingDir
	}
	return s
}

func (s *Session) Submit(ctx context.Context, input string) (<-chan provider.StreamEvent, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("input is required")
	}
	s.History = append(s.History, provider.ChatMessage{Role: provider.RoleUser, Content: input})
	req := provider.ChatRequest{
		Messages: append([]provider.ChatMessage(nil), s.History...),
		Model:    s.cfg.Model,
		Thinking: s.cfg.Thinking,
		Tools:    s.toolDefinitions(),
	}
	s.LastRequest = &req
	return s.run(ctx, req, true)
}

func (s *Session) Retry(ctx context.Context) (<-chan provider.StreamEvent, error) {
	if s.LastRequest == nil {
		return nil, fmt.Errorf("no request to retry")
	}
	req := *s.LastRequest
	req.Messages = append([]provider.ChatMessage(nil), s.LastRequest.Messages...)
	return s.run(ctx, req, true)
}

func (s *Session) CommitAssistant(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	s.History = append(s.History, provider.ChatMessage{Role: provider.RoleAssistant, Content: content})
}

func (s *Session) toolDefinitions() []tool.Definition {
	if s.tools == nil {
		return nil
	}
	return s.tools.Definitions()
}

func (s *Session) run(ctx context.Context, req provider.ChatRequest, allowTools bool) (<-chan provider.StreamEvent, error) {
	out := make(chan provider.StreamEvent)
	events, err := s.provider.StreamChat(ctx, req)
	if err != nil {
		close(out)
		return nil, err
	}
	go func() {
		defer close(out)
		s.consume(ctx, out, events, allowTools)
	}()
	return out, nil
}

func (s *Session) consume(ctx context.Context, out chan<- provider.StreamEvent, events <-chan provider.StreamEvent, allowTools bool) {
	for event := range events {
		switch event.Type {
		case provider.StreamEventTypeToolCallDone:
			if !allowTools {
				out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: "continuous tool calls are not supported in this stage"}
				return
			}
			if len(event.ToolCalls) == 0 {
				continue
			}
			out <- event
			s.History = append(s.History, provider.ChatMessage{Role: provider.RoleAssistant, ToolCalls: append([]provider.ToolCall(nil), event.ToolCalls...)})
			results := s.executeToolCalls(ctx, event.ToolCalls)
			messages := make([]provider.ToolResultMessage, 0, len(results))
			for i := range results {
				result := results[i]
				out <- provider.StreamEvent{Type: provider.StreamEventTypeToolResult, ToolResult: &result}
				messages = append(messages, provider.ToolResultMessage{ID: result.CallID, Name: result.Tool, Content: s.toolResultContent(result)})
			}
			s.History = append(s.History, provider.ChatMessage{Role: provider.RoleUser, ToolResults: messages})
			nextReq := provider.ChatRequest{
				Messages: append([]provider.ChatMessage(nil), s.History...),
				Model:    s.cfg.Model,
				Thinking: s.cfg.Thinking,
			}
			nextEvents, err := s.provider.StreamChat(ctx, nextReq)
			if err != nil {
				out <- provider.StreamEvent{Type: provider.StreamEventTypeError, ErrorText: err.Error()}
				return
			}
			s.consume(ctx, out, nextEvents, false)
			return
		case provider.StreamEventTypeDone:
			out <- event
			return
		default:
			out <- event
		}
	}
}

func (s *Session) executeToolCalls(ctx context.Context, calls []provider.ToolCall) []tool.Result {
	results := make([]tool.Result, 0, len(calls))
	for _, call := range calls {
		results = append(results, s.executeToolCall(ctx, call))
	}
	return results
}

func (s *Session) executeToolCall(ctx context.Context, call provider.ToolCall) tool.Result {
	if s.tools == nil {
		return tool.Failure(call.Name, call.ID, "tools_unavailable", "tools are not configured")
	}
	exec, ok := s.tools.Get(call.Name)
	if !ok {
		return tool.Failure(call.Name, call.ID, "unknown_tool", fmt.Sprintf("tool %q is not registered", call.Name))
	}
	req := tool.Request{
		ID:         call.ID,
		Name:       call.Name,
		Arguments:  []byte(call.Arguments),
		WorkingDir: s.workDir,
		PathPolicy: s.paths,
		Limits:     s.limits,
	}
	result := exec.Execute(ctx, req)
	if result.Tool == "" {
		result.Tool = call.Name
	}
	if result.CallID == "" {
		result.CallID = call.ID
	}
	return result
}

func (s *Session) toolResultContent(result tool.Result) string {
	content := result.JSON()
	maxBytes := s.limits.MaxResultBytes
	if maxBytes <= 0 {
		maxBytes = tool.DefaultLimits().MaxResultBytes
	}
	limited := tool.LimitText(content, maxBytes)
	if !limited.Truncated {
		return content
	}
	truncated := tool.Result{
		Tool:          result.Tool,
		CallID:        result.CallID,
		OK:            result.OK,
		Summary:       result.Summary,
		Error:         result.Error,
		Truncated:     true,
		OriginalBytes: limited.OriginalBytes,
		ReturnedBytes: limited.ReturnedBytes,
		Data: map[string]any{
			"truncated_result_json": limited.Text,
		},
	}
	return truncated.JSON()
}
