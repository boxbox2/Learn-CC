package tui

import "github.com/charmbracelet/lipgloss"

var (
	statusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	thinkingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	replyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	userStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
)
