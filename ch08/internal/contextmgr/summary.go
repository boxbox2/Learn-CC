package contextmgr

import (
	"context"
	"fmt"
	"math"
	"strings"

	"mewcode/internal/config"
	"mewcode/internal/provider"
)

type Summarizer struct {
	Provider provider.Provider
	Config   config.ProviderConfig
}

func NewSummarizer(p provider.Provider, cfg config.ProviderConfig) *Summarizer {
	return &Summarizer{Provider: p, Config: cfg}
}

func (s *Summarizer) Summarize(ctx context.Context, req SummaryRequest) (SummaryResult, error) {
	if s == nil || s.Provider == nil {
		return SummaryResult{}, fmt.Errorf("summary provider is required")
	}
	groups := groupConversation(req.Messages)
	if len(groups) == 0 {
		return SummaryResult{}, fmt.Errorf("cannot summarize empty conversation")
	}
	preDropped := 0
	if req.SafetyMargin > 0 && req.ContextWindow > 0 {
		limit := req.ContextWindow - SummaryOutputReserveTokens - req.SafetyMargin
		for len(groups) > 1 && EstimateBytes(len(BuildSummaryPrompt(flattenGroups(groups)))) > limit {
			groups = groups[1:]
			preDropped++
		}
		if EstimateBytes(len(BuildSummaryPrompt(flattenGroups(groups)))) > limit {
			return SummaryResult{}, fmt.Errorf("summary request exceeds context window after precheck")
		}
	}
	dropped := 0
	attempt := 0
	for {
		messages := flattenGroups(groups)
		if len(messages) == 0 {
			return SummaryResult{}, fmt.Errorf("cannot summarize empty conversation")
		}
		text, err := s.callSummary(ctx, req, messages)
		if err == nil {
			summary, err := ExtractSummary(text)
			if err != nil {
				return SummaryResult{}, err
			}
			return SummaryResult{Summary: summary, DroppedGroups: preDropped + dropped}, nil
		}
		if !provider.IsPromptTooLong(err) {
			return SummaryResult{}, err
		}
		if len(groups) == 0 {
			return SummaryResult{}, err
		}
		drop := 1
		if attempt >= SummaryPTLDirectRetries {
			drop = int(math.Ceil(float64(len(groups)) * SummaryPTLDropRatio))
			if drop < 1 {
				drop = 1
			}
		}
		if drop >= len(groups) {
			return SummaryResult{}, err
		}
		groups = groups[drop:]
		dropped += drop
		attempt++
	}
}

func (s *Summarizer) callSummary(ctx context.Context, req SummaryRequest, messages []provider.ChatMessage) (string, error) {
	chatReq := BuildSummaryChatRequest(messages, s.Config)
	events, err := s.Provider.StreamChat(ctx, chatReq)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case event, ok := <-events:
			if !ok {
				return b.String(), nil
			}
			switch event.Type {
			case provider.StreamEventTypeTextDelta:
				b.WriteString(event.Delta)
			case provider.StreamEventTypeError:
				return "", fmt.Errorf(event.ErrorText)
			}
		}
	}
}

func BuildSummaryChatRequest(messages []provider.ChatMessage, cfg config.ProviderConfig) provider.ChatRequest {
	return provider.ChatRequest{
		SystemPrompt: summarySystemPrompt(),
		Messages: []provider.ChatMessage{
			{Role: provider.RoleUser, Content: BuildSummaryPrompt(messages)},
		},
		Model:    cfg.Model,
		Thinking: cfg.Thinking,
		Tools:    nil,
	}
}

func BuildSummaryPrompt(messages []provider.ChatMessage) string {
	var b strings.Builder
	b.WriteString("Summarize the conversation below. Do not call tools. First write a private draft inside <analysis>, then write the final structured summary inside <summary>. The final summary must contain exactly these sections:\n")
	sections := []string{
		"1. Main request and intent",
		"2. Key technical concepts",
		"3. Files and code snippets",
		"4. Errors and fixes",
		"5. Problem-solving process",
		"6. User messages, preserving original wording when possible",
		"7. TODOs",
		"8. Current work",
		"9. Possible next steps",
	}
	for _, section := range sections {
		b.WriteString("- ")
		b.WriteString(section)
		b.WriteByte('\n')
	}
	b.WriteString("\nConversation:\n")
	for i, message := range messages {
		b.WriteString(fmt.Sprintf("\n--- message %d role=%s ---\n", i+1, message.Role))
		if message.Content != "" {
			b.WriteString(message.Content)
			b.WriteByte('\n')
		}
		for _, call := range message.ToolCalls {
			b.WriteString(fmt.Sprintf("[tool_call id=%s name=%s args=%s]\n", call.ID, call.Name, call.Arguments))
		}
		for _, result := range message.ToolResults {
			b.WriteString(fmt.Sprintf("[tool_result id=%s name=%s]\n%s\n", result.ID, result.Name, result.Content))
		}
	}
	return b.String()
}

func summarySystemPrompt() string {
	return "You are summarizing a conversation for context compaction. You must not call any tools. Output <analysis>draft</analysis> followed by <summary>final summary</summary>."
}

func ExtractSummary(text string) (string, error) {
	startTag := "<summary>"
	endTag := "</summary>"
	start := strings.Index(text, startTag)
	if start < 0 {
		return "", fmt.Errorf("summary response missing %s", startTag)
	}
	start += len(startTag)
	end := strings.Index(text[start:], endTag)
	if end < 0 {
		return "", fmt.Errorf("summary response missing %s", endTag)
	}
	summary := strings.TrimSpace(text[start : start+end])
	if summary == "" {
		return "", fmt.Errorf("summary response is empty")
	}
	return summary, nil
}

func groupConversation(messages []provider.ChatMessage) [][]provider.ChatMessage {
	var groups [][]provider.ChatMessage
	var current []provider.ChatMessage
	flush := func() {
		if len(current) > 0 {
			groups = append(groups, current)
			current = nil
		}
	}
	for _, message := range messages {
		if message.Role == provider.RoleUser && len(message.ToolResults) == 0 {
			flush()
		}
		current = append(current, message)
	}
	flush()
	return groups
}

func flattenGroups(groups [][]provider.ChatMessage) []provider.ChatMessage {
	var messages []provider.ChatMessage
	for _, group := range groups {
		messages = append(messages, group...)
	}
	return cloneMessages(messages)
}
