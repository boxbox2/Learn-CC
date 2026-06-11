package tool

import (
	"context"
	"testing"
)

type fakeTool struct {
	name string
}

func (f fakeTool) Definition() Definition {
	return Definition{Name: f.name, Description: "fake", Parameters: Schema{"type": "object"}}
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
