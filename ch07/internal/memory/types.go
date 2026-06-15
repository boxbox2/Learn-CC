package memory

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mewcode/internal/provider"
)

const (
	MaxIndexLines = 200
	MaxIndexBytes = 25 * 1024
)

type Scope string

const (
	ScopeUser    Scope = "user"
	ScopeProject Scope = "project"
)

type NoteType string

const (
	NoteUserPreference     NoteType = "user_preference"
	NoteCorrectionFeedback NoteType = "correction_feedback"
	NoteProjectKnowledge   NoteType = "project_knowledge"
	NoteReferenceMaterial  NoteType = "reference_material"
)

const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
	ActionNoop   = "noop"
)

type Domain struct {
	Scope    Scope
	RootDir  string
	NotesDir string
	Index    string
	mu       sync.Mutex
}

type Note struct {
	ID            string
	Type          NoteType
	Scope         Scope
	Title         string
	Content       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	SourceSession string
}

type ChangeSet struct {
	Changes []Change `json:"changes"`
}

type Change struct {
	Action   string   `json:"action"`
	Type     NoteType `json:"type"`
	Scope    Scope    `json:"scope"`
	Filename string   `json:"filename"`
	Title    string   `json:"title"`
	Content  string   `json:"content"`
	Reason   string   `json:"reason"`
}

type Snapshot struct {
	SessionID string
	Messages  []provider.ChatMessage
	FinalText string
	CreatedAt time.Time
}

func ValidateChange(change Change) error {
	switch change.Action {
	case ActionCreate, ActionUpdate, ActionDelete, ActionNoop:
	default:
		return fmt.Errorf("invalid action %q", change.Action)
	}
	switch change.Type {
	case NoteUserPreference, NoteCorrectionFeedback, NoteProjectKnowledge, NoteReferenceMaterial:
	default:
		return fmt.Errorf("invalid note type %q", change.Type)
	}
	switch change.Scope {
	case ScopeUser, ScopeProject:
	default:
		return fmt.Errorf("invalid scope %q", change.Scope)
	}
	if change.Action == ActionNoop {
		return nil
	}
	if change.Filename == "" || filepath.Base(change.Filename) != change.Filename || strings.Contains(change.Filename, "..") {
		return fmt.Errorf("invalid filename %q", change.Filename)
	}
	if strings.TrimSpace(change.Title) == "" {
		return fmt.Errorf("title is required")
	}
	if (change.Action == ActionCreate || change.Action == ActionUpdate) && strings.TrimSpace(change.Content) == "" {
		return fmt.Errorf("content is required")
	}
	return nil
}

func cloneMessages(messages []provider.ChatMessage) []provider.ChatMessage {
	out := make([]provider.ChatMessage, len(messages))
	for i, msg := range messages {
		out[i] = msg
		out[i].ToolCalls = append([]provider.ToolCall(nil), msg.ToolCalls...)
		out[i].ToolResults = append([]provider.ToolResultMessage(nil), msg.ToolResults...)
	}
	return out
}
