package tool

import (
	"context"
	"encoding/json"
)

type Schema map[string]any

type Definition struct {
	Name        string
	Description string
	Parameters  Schema
}

type Request struct {
	ID         string
	Name       string
	Arguments  json.RawMessage
	WorkingDir string
	PathPolicy PathPolicy
	Limits     Limits
}

type Tool interface {
	Definition() Definition
	Execute(ctx context.Context, req Request) Result
}
