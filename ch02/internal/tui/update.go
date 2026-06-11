package tui

import (
	"context"
	"strings"

	"mewcode/internal/provider"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		m.textarea.SetWidth(msg.Width)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case streamMsg:
		return m.handleStream(provider.StreamEvent(msg))
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		if m.Status == StatusStreaming && m.StreamCancel != nil {
			m.StreamCancel()
			m.Status = StatusIdle
			m.StreamCancel = nil
			return m, tea.Println(m.Current.Content)
		}
		return m, tea.Quit
	case keyThinking:
		m.ShowThinking = !m.ShowThinking
		return m, nil
	case keyRetry:
		if m.Status == StatusStreaming {
			return m, nil
		}
		return m.startRetry()
	case keyEnter:
		if m.Status == StatusStreaming {
			return m, nil
		}
		if strings.TrimSpace(m.textarea.Value()) == commandExit {
			return m, tea.Quit
		}
		return m.startSubmit()
	}
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m Model) startSubmit() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.textarea.Value())
	if input == "" {
		return m, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := m.Runner.Submit(ctx, input)
	if err != nil {
		cancel()
		m.Status = StatusError
		m.LastError = err.Error()
		m.Current.ErrorText = err.Error()
		return m, nil
	}
	m.StreamCancel = cancel
	m.events = events
	m.Current = UIMessage{Role: provider.RoleAssistant, Status: MessageStatusStreaming}
	m.Status = StatusStreaming
	m.textarea.Reset()
	return m, tea.Batch(tea.Println(userBlock(input)), waitForEvent(events))
}

func (m Model) startRetry() (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	events, err := m.Runner.Retry(ctx)
	if err != nil {
		cancel()
		m.Status = StatusError
		m.LastError = err.Error()
		m.Current.ErrorText = err.Error()
		return m, nil
	}
	m.StreamCancel = cancel
	m.events = events
	m.Current = UIMessage{Role: provider.RoleAssistant, Status: MessageStatusStreaming}
	m.Status = StatusStreaming
	return m, waitForEvent(events)
}

func (m Model) handleStream(event provider.StreamEvent) (tea.Model, tea.Cmd) {
	switch event.Type {
	case provider.StreamEventTypeTextDelta:
		m.Current.Content += event.Delta
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeThinkingDelta:
		m.Current.Thinking += event.Delta
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeUsage:
		m.Usage = event.Usage
		m.Current.Usage = event.Usage
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeToolCallStart:
		if event.ToolCall != nil {
			m.CurrentTool = event.ToolCall.Name
			return m, tea.Batch(tea.Println(toolCallLine(*event.ToolCall)), waitForEvent(m.events))
		}
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeToolCallDone:
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeToolResult:
		if event.ToolResult != nil {
			return m, tea.Batch(tea.Println(toolResultSummary(*event.ToolResult)), waitForEvent(m.events))
		}
		return m, waitForEvent(m.events)
	case provider.StreamEventTypeError:
		m.Status = StatusError
		m.LastError = event.ErrorText
		m.Current.ErrorText = event.ErrorText
		m.Current.Status = MessageStatusError
		m.StreamCancel = nil
		return m, tea.Println(errorBlock(event.ErrorText))
	case provider.StreamEventTypeCancelled:
		m.Status = StatusIdle
		m.StreamCancel = nil
		m.Current.Status = MessageStatusDone
		return m, tea.Println(m.Current.Content)
	case provider.StreamEventTypeDone:
		m.Status = StatusIdle
		m.StreamCancel = nil
		m.Current.Status = MessageStatusDone
		content := m.Current.Content
		if strings.TrimSpace(content) != "" {
			m.Runner.CommitAssistant(content)
		}
		rendered := content
		if m.Renderer != nil {
			if out, err := m.Renderer.Render(content, m.Width); err == nil {
				rendered = out
			}
		}
		m.Current = UIMessage{}
		return m, tea.Println(rendered)
	}
	return m, nil
}
