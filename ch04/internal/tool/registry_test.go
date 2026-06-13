package tool

import (
	"context"
	"testing"
)

type fakeTool struct {
	name   string
	safety Safety
}

func (f fakeTool) Definition() Definition {
	return Definition{Name: f.name, Description: "fake", Parameters: Schema{"type": "object"}, Safety: f.safety}
}

func (f fakeTool) Execute(ctx context.Context, req Request) Result {
	return Success(f.name, req.ID, "ok", nil)
}

func TestRegistryRegisterGetAndDefinitions(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(fakeTool{name: "B"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{name: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("A"); !ok {
		t.Fatal("expected tool A")
	}
	defs := reg.Definitions()
	if len(defs) != 2 || defs[0].Name != "A" || defs[1].Name != "B" {
		t.Fatalf("definitions not sorted: %+v", defs)
	}
	if err := reg.Register(fakeTool{name: "A"}); err == nil {
		t.Fatal("expected duplicate registration error")
	}
	if err := reg.Register(fakeTool{}); err == nil {
		t.Fatal("expected empty name error")
	}
}

func TestDefinitionsBySafety(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(fakeTool{name: "Read", safety: SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{name: "Write", safety: SafetySideEffect}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{name: "Default"}); err != nil {
		t.Fatal(err)
	}
	readOnly := reg.DefinitionsBySafety(SafetyReadOnly)
	if len(readOnly) != 1 || readOnly[0].Name != "Read" {
		t.Fatalf("read only = %+v", readOnly)
	}
	sideEffect := reg.DefinitionsBySafety(SafetySideEffect)
	if len(sideEffect) != 2 || sideEffect[0].Name != "Default" || sideEffect[1].Name != "Write" {
		t.Fatalf("side effect = %+v", sideEffect)
	}
}
