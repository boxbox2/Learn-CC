package chat

import (
	"context"
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/provider"
)

type fakeProvider struct {
	requests []provider.ChatRequest
}

func (f *fakeProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, error) {
	f.requests = append(f.requests, req)
	ch := make(chan provider.StreamEvent, 1)
	ch <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	close(ch)
	return ch, nil
}

func TestSessionHistoryAndRetry(t *testing.T) {
	fp := &fakeProvider{}
	s := NewSession(fp, config.ProviderConfig{Model: "fake"})

	if _, err := s.Submit(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	s.CommitAssistant("world")
	if _, err := s.Submit(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if got := len(fp.requests[1].Messages); got != 3 {
		t.Fatalf("second request messages = %d, want 3", got)
	}
	if _, err := s.Retry(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(fp.requests); got != 3 {
		t.Fatalf("requests = %d, want 3", got)
	}
	if fp.requests[2].Messages[2].Content != "again" {
		t.Fatalf("retry used wrong request: %+v", fp.requests[2].Messages)
	}
}
