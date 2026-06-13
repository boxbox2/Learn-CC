package provider

import (
	"mewcode/internal/config"
	"mewcode/internal/tool"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type ChatMessage struct {
	Role        Role
	Content     string
	ToolCalls   []ToolCall
	ToolResults []ToolResultMessage
}

type ChatRequest struct {
	SystemPrompt string
	SystemNotes  []ChatMessage
	Messages     []ChatMessage
	Model        string
	Thinking     config.ThinkingConfig
	Tools        []tool.Definition
}
