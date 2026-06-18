package tui

import (
	"context"

	"mewcode/internal/chat"
	"mewcode/internal/command"
	"mewcode/internal/config"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	"mewcode/internal/sessionstore"

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
	Submit(ctx context.Context, input string, mode chat.SubmitMode) (<-chan provider.StreamEvent, error)
	SubmitSkill(ctx context.Context, name, args string) (<-chan provider.StreamEvent, error)
	Retry(ctx context.Context) (<-chan provider.StreamEvent, error)
	CommitAssistant(content string)
	Compact(ctx context.Context) (<-chan provider.StreamEvent, error)
	ResetSession(ctx context.Context) error
	Status() chat.SessionStatus
	SessionList() ([]sessionstore.Summary, error)
	MemoryStatus() command.MemoryStatus
	PermissionStatus() command.PermissionStatus
	ListCatalogSkills() []command.SkillSummary
	ReloadSkillCommands(ctx context.Context, reg *command.Registry) error
	Close() error
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
	Commands    *command.Registry
	RootCancel  context.CancelFunc

	Input            string
	textarea         textarea.Model
	Output           []string
	ChatMode         command.ChatMode
	Completion       command.CompletionState
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

func NewModel(cfg config.AppConfig, runner ChatRunner, renderer MarkdownRenderer, commands *command.Registry, rootCancel context.CancelFunc) Model {
	activeCfg, _ := cfg.ActiveProvider()
	ta := textarea.New()
	ta.Placeholder = "Ask MewCode..."
	ta.Focus()
	ta.SetHeight(3)
	if commands == nil {
		commands = command.NewRegistry()
		commands.MustValidate()
	}
	return Model{
		Config:       cfg,
		Active:       cfg.Active,
		ProviderCfg:  activeCfg,
		Runner:       runner,
		Renderer:     renderer,
		Commands:     commands,
		RootCancel:   rootCancel,
		textarea:     ta,
		ChatMode:     command.ChatModeDefault,
		Status:       StatusIdle,
		ShowThinking: activeCfg.Thinking.ShowByDefault,
		Width:        80,
		Height:       24,
	}
}

func (m Model) Init() tea.Cmd {
	return textarea.Blink
}

func Run(ctx context.Context, cfg config.AppConfig, runner ChatRunner, renderer MarkdownRenderer, commands *command.Registry, rootCancel context.CancelFunc) error {
	p := tea.NewProgram(NewModel(cfg, runner, renderer, commands, rootCancel))
	_, err := p.Run()
	return err
}
