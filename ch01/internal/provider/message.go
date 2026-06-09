package provider

import "mewcode/internal/config"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

type ChatMessage struct {
	Role    Role
	Content string
}

type ChatRequest struct {
	Messages []ChatMessage
	Model    string
	Thinking config.ThinkingConfig
}
