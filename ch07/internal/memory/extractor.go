package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mewcode/internal/config"
	"mewcode/internal/provider"
)

type Extractor struct {
	Provider provider.Provider
	Config   config.ProviderConfig
}

func (e Extractor) Extract(ctx context.Context, snapshot Snapshot, existingIndex string) (ChangeSet, error) {
	if e.Provider == nil {
		return ChangeSet{}, nil
	}
	req := provider.ChatRequest{
		SystemPrompt: memorySystemPrompt(),
		Messages: []provider.ChatMessage{
			{Role: provider.RoleSystem, Content: "Existing memory index:\n" + existingIndex},
			{Role: provider.RoleUser, Content: snapshotText(snapshot)},
		},
		Model:    e.Config.Model,
		Thinking: e.Config.Thinking,
	}
	events, err := e.Provider.StreamChat(ctx, req)
	if err != nil {
		return ChangeSet{}, err
	}
	var text strings.Builder
	for event := range events {
		if event.Type == provider.StreamEventTypeTextDelta {
			text.WriteString(event.Delta)
		}
		if event.Type == provider.StreamEventTypeError {
			return ChangeSet{}, fmt.Errorf(event.ErrorText)
		}
	}
	return ParseChangeSet(text.String())
}

func ParseChangeSet(text string) (ChangeSet, error) {
	text = strings.TrimSpace(text)
	var changes ChangeSet
	if err := json.Unmarshal([]byte(text), &changes); err != nil {
		return ChangeSet{}, err
	}
	for _, change := range changes.Changes {
		if err := ValidateChange(change); err != nil {
			return ChangeSet{}, err
		}
	}
	return changes, nil
}

func memorySystemPrompt() string {
	return `Extract durable memory updates from the conversation. Return only JSON with shape {"changes":[{"action":"create|update|delete|noop","type":"user_preference|correction_feedback|project_knowledge|reference_material","scope":"user|project","filename":"name.md","title":"short title","content":"markdown content","reason":"why"}]}. Do not return markdown.`
}

func snapshotText(snapshot Snapshot) string {
	var b strings.Builder
	b.WriteString("Session: " + snapshot.SessionID + "\n")
	for _, msg := range snapshot.Messages {
		b.WriteString(string(msg.Role) + ": " + msg.Content + "\n")
	}
	b.WriteString("Final: " + snapshot.FinalText)
	return b.String()
}
