package tui

import (
	"mewcode/internal/provider"

	tea "github.com/charmbracelet/bubbletea"
)

type streamMsg provider.StreamEvent

func waitForEvent(ch <-chan provider.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-ch
		if !ok {
			return streamMsg{Type: provider.StreamEventTypeDone}
		}
		return streamMsg(event)
	}
}
