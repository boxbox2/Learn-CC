package sessionstore

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mewcode/internal/provider"
)

type RestoreResult struct {
	Messages       []provider.ChatMessage
	Summary        Summary
	Truncated      bool
	TruncateReason string
	LastMessageAt  time.Time
	Diagnostics    []Diagnostic
}

func Restore(path string) (RestoreResult, error) {
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	result := RestoreResult{Summary: Summary{ID: id}}
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	var messages []provider.ChatMessage
	for scanner.Scan() {
		var line Line
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			result.Summary.CorruptLineCount++
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Path: path, Message: "bad json line skipped"})
			continue
		}
		if err := line.Validate(id); err != nil {
			result.Summary.CorruptLineCount++
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Path: path, Message: err.Error()})
			continue
		}
		result.Summary.MessageCount++
		if result.Summary.Title == "" && line.Role == "user" && strings.TrimSpace(line.Content) != "" {
			result.Summary.Title = firstLine(line.Content, 80)
		}
		result.Summary.UpdatedAt = line.TS
		result.LastMessageAt = line.TS
		messages = append(messages, MessageFromLine(line))
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	result.Messages, result.Truncated, result.TruncateReason = truncateUnmatchedTools(messages)
	return result, nil
}

func truncateUnmatchedTools(messages []provider.ChatMessage) ([]provider.ChatMessage, bool, string) {
	for i, msg := range messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		pending := map[string]bool{}
		for _, call := range msg.ToolCalls {
			pending[call.ID] = true
		}
		for j := i + 1; j < len(messages) && len(pending) > 0; j++ {
			for _, result := range messages[j].ToolResults {
				delete(pending, result.ID)
			}
		}
		if len(pending) > 0 {
			return append([]provider.ChatMessage(nil), messages[:i]...), true, "assistant tool call missing matching tool result"
		}
	}
	return messages, false, ""
}
