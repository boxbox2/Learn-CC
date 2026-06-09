package chat

import (
	"context"
	"fmt"
	"strings"

	"mewcode/internal/config"
	"mewcode/internal/provider"
)

type Session struct {
	provider provider.Provider
	cfg      config.ProviderConfig

	History     []provider.ChatMessage
	LastRequest *provider.ChatRequest
}

func NewSession(p provider.Provider, cfg config.ProviderConfig) *Session {
	return &Session{provider: p, cfg: cfg}
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
	}
	s.LastRequest = &req
	return s.provider.StreamChat(ctx, req)
}

func (s *Session) Retry(ctx context.Context) (<-chan provider.StreamEvent, error) {
	if s.LastRequest == nil {
		return nil, fmt.Errorf("no request to retry")
	}
	req := *s.LastRequest
	req.Messages = append([]provider.ChatMessage(nil), s.LastRequest.Messages...)
	return s.provider.StreamChat(ctx, req)
}

func (s *Session) CommitAssistant(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	s.History = append(s.History, provider.ChatMessage{Role: provider.RoleAssistant, Content: content})
}
