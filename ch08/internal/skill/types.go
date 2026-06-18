package skill

import (
	"io"
	"io/fs"
	"regexp"
	"strings"
	"sync"

	"mewcode/internal/command"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

type Mode string

const (
	ModeInline Mode = "inline"
	ModeFork   Mode = "fork"
)

type ForkContext string

const (
	ForkContextNone   ForkContext = "none"
	ForkContextRecent ForkContext = "recent"
	ForkContextFull   ForkContext = "full"
)

type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceUser    Source = "user"
	SourceProject Source = "project"
)

type Metadata struct {
	Name         string
	Description  string
	AllowedTools []string
	Mode         Mode
	ForkContext  ForkContext
	Model        string
}

type Definition struct {
	Metadata Metadata
	Body     string
	Dir      string
	Entry    string
	Source   Source
	Tools    []ToolSpec
	Warnings []LoadWarning
}

type ToolSpec struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema tool.Schema `json:"input_schema"`
	Command     []string    `json:"command"`
}

type LoadWarning struct {
	Path    string
	Skill   string
	Message string
}

func (w LoadWarning) String() string {
	if strings.TrimSpace(w.Skill) != "" {
		return "skill " + w.Skill + ": " + w.Message
	}
	if strings.TrimSpace(w.Path) != "" {
		return w.Path + ": " + w.Message
	}
	return w.Message
}

type LoadOptions struct {
	ProjectDir string
	HomeDir    string
	BuiltinFS  fs.FS
	Tools      *tool.Registry
	Commands   *command.Registry
	Stderr     io.Writer
}

type Snapshot struct {
	Skills   map[string]Definition
	Ordered  []string
	Warnings []LoadWarning
}

type Catalog struct {
	mu       sync.RWMutex
	snapshot Snapshot
	opts     LoadOptions
}

type ExecuteRequest struct {
	Name    string
	Args    string
	History []provider.ChatMessage
	Mode    string
}

type ActiveTool struct {
	Skill string
	Tool  tool.Tool
}

type CatalogSummary struct {
	Name        string
	Description string
}

var skillNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func ValidName(name string) bool {
	name = NormalizeName(name)
	return len(name) >= 1 && len(name) <= 32 && skillNameRE.MatchString(name)
}
