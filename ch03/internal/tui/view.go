package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	width := m.Width
	if width < defaultMinWidth {
		width = defaultMinWidth
	}
	var parts []string
	if m.Current.Thinking != "" && m.ShowThinking {
		parts = append(parts, thinkingStyle.Width(width).Render(m.Current.Thinking))
	}
	if m.Current.Content != "" {
		parts = append(parts, replyStyle.Width(width).Render(m.Current.Content))
	}
	if m.Current.ErrorText != "" {
		parts = append(parts, errorStyle.Width(width).Render("Error: "+m.Current.ErrorText))
	}
	parts = append(parts, m.textarea.View())
	parts = append(parts, m.statusBar())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) statusBar() string {
	model := m.ProviderCfg.Model
	if model == "" {
		model = "-"
	}
	items := []string{
		fmt.Sprintf("active=%s", m.Active),
		fmt.Sprintf("model=%s", model),
		fmt.Sprintf("state=%s", m.Status),
		fmt.Sprintf("prompt=%d completion=%d total=%d", m.Usage.PromptTokens, m.Usage.CompletionTokens, m.Usage.TotalTokens),
		"ctrl+t thinking",
		"ctrl+r retry",
	}
	if m.Progress != "" {
		items = append(items, "progress="+m.Progress)
	}
	return statusStyle.Render(strings.Join(items, " | "))
}

func userBlock(input string) string {
	return userStyle.Render("> " + input)
}

func errorBlock(text string) string {
	return errorStyle.Render("Error: " + text)
}
