package tool

import (
	"context"
	"encoding/json"
)

type Schema map[string]any

type Safety string

const (
	SafetyReadOnly   Safety = "read_only"
	SafetySideEffect Safety = "side_effect"
)

type Definition struct {
	Name        string
	Description string
	Parameters  Schema
	Safety      Safety
}

func (d Definition) EffectiveSafety() Safety {
	if d.Safety == SafetyReadOnly {
		return SafetyReadOnly
	}
	return SafetySideEffect
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

type DeferredTool interface {
	ShouldDefer() bool
}
