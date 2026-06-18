package provider

import (
	"context"
	"strings"
)

type Provider interface {
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}

func IsPromptTooLong(err error) bool {
	if err == nil {
		return false
	}
	return IsPromptTooLongText(err.Error())
}

func IsPromptTooLongText(text string) bool {
	text = strings.ToLower(text)
	needles := []string{
		"prompt_too_long",
		"context_length_exceeded",
		"maximum context length",
		"too many tokens",
		"context window",
		"input is too long",
	}
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
