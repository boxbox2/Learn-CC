package sessionstore

import (
	"fmt"
	"time"

	"mewcode/internal/provider"
)

const Version = 1

type Line struct {
	Version     int          `json:"version"`
	SessionID   string       `json:"session_id"`
	Seq         int64        `json:"seq"`
	TS          time.Time    `json:"ts"`
	Role        string       `json:"role"`
	Content     string       `json:"content"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	ToolResults []ToolResult `json:"tool_results,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	Status     string `json:"status"`
}

func (l Line) Validate(expectedSessionID string) error {
	if l.Version != Version {
		return fmt.Errorf("unsupported version %d", l.Version)
	}
	if l.SessionID == "" || l.SessionID != expectedSessionID {
		return fmt.Errorf("session id mismatch")
	}
	if l.Seq <= 0 {
		return fmt.Errorf("seq is required")
	}
	if l.TS.IsZero() {
		return fmt.Errorf("timestamp is required")
	}
	switch l.Role {
	case "system", "user", "assistant", "tool":
	default:
		return fmt.Errorf("invalid role %q", l.Role)
	}
	if l.Role != "assistant" && len(l.ToolCalls) > 0 {
		return fmt.Errorf("tool_calls only allowed for assistant")
	}
	if l.Role != "tool" && len(l.ToolResults) > 0 {
		return fmt.Errorf("tool_results only allowed for tool")
	}
	return nil
}

func LineFromMessage(sessionID string, seq int64, msg provider.ChatMessage, now time.Time) Line {
	role := string(msg.Role)
	if len(msg.ToolResults) > 0 {
		role = "tool"
	}
	line := Line{
		Version:   Version,
		SessionID: sessionID,
		Seq:       seq,
		TS:        now,
		Role:      role,
		Content:   msg.Content,
	}
	for _, call := range msg.ToolCalls {
		line.ToolCalls = append(line.ToolCalls, ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	for _, result := range msg.ToolResults {
		line.ToolResults = append(line.ToolResults, ToolResult{ToolCallID: result.ID, Name: result.Name, Content: result.Content, Status: "ok"})
	}
	return line
}

func MessageFromLine(line Line) provider.ChatMessage {
	msg := provider.ChatMessage{Role: provider.Role(line.Role), Content: line.Content}
	if line.Role == "tool" {
		msg.Role = provider.RoleUser
	}
	for _, call := range line.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, provider.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	for _, result := range line.ToolResults {
		msg.ToolResults = append(msg.ToolResults, provider.ToolResultMessage{ID: result.ToolCallID, Name: result.Name, Content: result.Content})
	}
	return msg
}
