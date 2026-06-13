package tui

import (
	"context"

	"mewcode/internal/config"
	"mewcode/internal/permission"
	"mewcode/internal/provider"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

type RunStatus string

const (
	StatusIdle      RunStatus = "idle"
	StatusStreaming RunStatus = "streaming"
	StatusError     RunStatus = "error"
)

type MessageStatus string

const (
	MessageStatusStreaming MessageStatus = "streaming"
	MessageStatusDone      MessageStatus = "done"
	MessageStatusError     MessageStatus = "error"
)

type ChatRunner interface {
	Submit(ctx context.Context, input string) (<-chan provider.StreamEvent, error)
	Retry(ctx context.Context) (<-chan provider.StreamEvent, error)
	CommitAssistant(content string)
}

type MarkdownRenderer interface {
	Render(markdown string, width int) (string, error)
}

type UIMessage struct {
	ID        string
	Role      provider.Role
	Content   string
	Thinking  string
	ErrorText string
	Usage     provider.Usage
	Status    MessageStatus
}

type Model struct {
	Config      config.AppConfig
	Active      string
	ProviderCfg config.ProviderConfig
	Runner      ChatRunner
	Renderer    MarkdownRenderer

	Input            string
	textarea         textarea.Model
	Current          UIMessage
	Width            int
	Height           int
	Status           RunStatus
	ShowThinking     bool
	Usage            provider.Usage
	LastError        string
	CurrentTool      string
	Progress         string
	PermissionQueue  []permission.Prompt
	ActivePermission *permission.Prompt
	StreamCancel     context.CancelFunc
	events           <-chan provider.StreamEvent
}

func NewModel(cfg config.AppConfig, runner ChatRunner, renderer MarkdownRenderer) Model {
	activeCfg, _ := cfg.ActiveProvider()
	ta := textarea.New()
	ta.Placeholder = "Ask MewCode..."
	ta.Focus()
	ta.SetHeight(3)
	return Model{
		Config:       cfg,
		Active:       cfg.Active,
		ProviderCfg:  activeCfg,
		Runner:       runner,
		Renderer:     renderer,
		textarea:     ta,
		Status:       StatusIdle,
		ShowThinking: activeCfg.Thinking.ShowByDefault,
		Width:        80,
		Height:       24,
	}
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func Run(ctx context.Context, cfg config.AppConfig, runner ChatRunner, renderer MarkdownRenderer) error {
	p := tea.NewProgram(NewModel(cfg, runner, renderer))
	_, err := p.Run()
	return err
}
