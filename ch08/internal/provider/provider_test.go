package provider

import (
	"errors"
	"testing"
)

func TestIsPromptTooLong(t *testing.T) {
	cases := []string{
		"prompt_too_long",
		"context_length_exceeded",
		"maximum context length is 128000 tokens",
		"too many tokens in prompt",
	}
	for _, tc := range cases {
		if !IsPromptTooLong(errors.New(tc)) {
			t.Fatalf("IsPromptTooLong(%q) = false", tc)
		}
	}
	if IsPromptTooLong(errors.New("network timeout")) {
		t.Fatal("network timeout should not be prompt too long")
	}
}
