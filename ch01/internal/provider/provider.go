package provider

import (
	"context"
)

type Provider interface {
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
