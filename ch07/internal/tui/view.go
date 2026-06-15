package tui

import (
	"fmt"
	"strings"

	"mewcode/internal/command"
	"mewcode/internal/permission"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) View() string {
	width := m.Width
	if width < defaultMinWidth {
		width = defaultMinWidth
	}
	var parts []string
	if len(m.Output) > 0 {
		parts = append(parts, strings.Join(m.Output, "\n"))
	}
	if m.Current.Thinking != "" && m.ShowThinking {
		parts = append(parts, thinkingStyle.Width(width).Render(m.Current.Thinking))
	}
	if m.Current.Content != "" {
		parts = append(parts, replyStyle.Width(width).Render(m.Current.Content))
	}
	if m.Current.ErrorText != "" {
		parts = append(parts, errorStyle.Width(width).Render("Error: "+m.Current.ErrorText))
	}
	if m.ActivePermission != nil {
		parts = append(parts, permissionBlock(*m.ActivePermission, width))
	}
	parts = append(parts, m.textarea.View())
	if menu := m.completionMenu(); menu != "" {
		parts = append(parts, menu)
	}
	parts = append(parts, m.statusBar())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) statusBar() string {
	model := m.ProviderCfg.Model
	if model == "" {
		model = "-"
	}
	usage := fmt.Sprintf("prompt=%d completion=%d total=%d", m.Usage.PromptTokens, m.Usage.CompletionTokens, m.Usage.TotalTokens)
	if m.Usage.CachedTokens > 0 {
		usage += fmt.Sprintf(" cached=%d", m.Usage.CachedTokens)
	}
	items := []string{
		modeBadge(m.ChatMode),
		fmt.Sprintf("active=%s", m.Active),
		fmt.Sprintf("model=%s", model),
		fmt.Sprintf("state=%s", m.Status),
		usage,
		"ctrl+t thinking",
		"ctrl+r retry",
	}
	if m.Progress != "" {
		items = append(items, "progress="+m.Progress)
	}
	return statusStyle.Render(strings.Join(items, " | "))
}

func modeBadge(mode command.ChatMode) string {
	if mode == command.ChatModePlan {
		return "[PLAN]"
	}
	return "[DEFAULT]"
}

func (m Model) completionMenu() string {
	if !m.Completion.Active {
		return ""
	}
	if m.Completion.NoMatch {
		return completionStyle.Render("无匹配")
	}
	if len(m.Completion.Items) == 0 {
		return ""
	}
	lines := make([]string, 0, len(m.Completion.Items))
	for i, item := range m.Completion.Items {
		prefix := "  "
		if i == m.Completion.Highlighted {
			prefix = "> "
		}
		line := prefix + item.Canonical
		if item.Description != "" {
			line += "  " + item.Description
		}
		lines = append(lines, line)
	}
	return completionStyle.Render(strings.Join(lines, "\n"))
}

func userBlock(input string) string {
	return userStyle.Render("> " + input)
}

func errorBlock(text string) string {
	return errorStyle.Render("Error: " + text)
}

func permissionBlock(prompt permission.Prompt, width int) string {
	summary := trimOneLine(prompt.Summary, 120)
	text := fmt.Sprintf(
		"Permission required: %s(%s)\n%s\n[o] once  [s] session  [p] permanent  [n] deny",
		prompt.Tool,
		summary,
		prompt.Reason,
	)
	return warningStyle.Width(width).Render(text)
}
