package command

import (
	"context"

	"mewcode/internal/provider"
)

type Kind string

const (
	KindReadOnly Kind = "read_only"
	KindUI       Kind = "ui"
	KindPrompt   Kind = "prompt"
	KindExit     Kind = "exit"
	KindLocal    Kind = "local"
)

type AgentState string

const (
	AgentStateIdle    AgentState = "idle"
	AgentStateRunning AgentState = "running"
)

type ChatMode string

const (
	ChatModeDefault ChatMode = "default"
	ChatModePlan    ChatMode = "plan"
)

type Definition struct {
	Name        string
	Aliases     []string
	Description string
	Usage       string
	Kind        Kind
	ArgHint     string
	Hidden      bool
	AcceptsArgs bool
	SkillName   string
	Handler     Handler
}

type Handler func(context.Context, Invocation, Controller) (Result, error)

type Invocation struct {
	Raw        string
	Name       string
	Canonical  string
	Args       string
	Definition Definition
}

type ParseResult struct {
	Empty   bool
	Chat    bool
	Input   string
	Command *Invocation
	Unknown string
}

type Result struct {
	Message     string
	SentToAI    bool
	ModeChanged bool
	Cleared     bool
	ShouldQuit  bool
}

type SessionStatus struct {
	ID           string
	Path         string
	MessageCount int
	HasPlan      bool
	Sessions     []SessionSummary
}

type SessionSummary struct {
	ID               string
	Title            string
	MessageCount     int
	UpdatedAt        string
	CorruptLineCount int
}

type MemoryStatus struct {
	UserAvailable    bool
	ProjectAvailable bool
	LastError        string
}

type PermissionStatus struct {
	Mode           string
	ActivePrompt   bool
	QueuedPrompts  int
	ActiveToolName string
}

type StatusSnapshot struct {
	Active       string
	Model        string
	AgentState   AgentState
	Mode         ChatMode
	Usage        provider.Usage
	Progress     string
	LastError    string
	CurrentTool  string
	SessionID    string
	ProviderName string
}

type Controller interface {
	AgentState() AgentState
	Mode() ChatMode
	SetMode(ChatMode)
	Usage() provider.Usage
	VisibleCommands() []Definition
	ShowLocalMessage(string)
	SendUserMessage(context.Context, string) error
	Compact(context.Context) error
	ClearAndResetSession(context.Context) error
	SessionStatus(context.Context) (SessionStatus, error)
	MemoryStatus(context.Context) MemoryStatus
	PermissionStatus(context.Context) PermissionStatus
	AppStatus(context.Context) StatusSnapshot
	ListCatalogSkills() []SkillSummary
	RunSkill(context.Context, string, string) error
	Shutdown(context.Context) error
}

type SkillSummary struct {
	Name        string
	Description string
	Active      bool
}
