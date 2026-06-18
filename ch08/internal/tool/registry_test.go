package tool

import (
	"context"
	"testing"
)

type fakeTool struct {
	name   string
	safety Safety
	defer_ bool
}

func (f fakeTool) Definition() Definition {
	return Definition{Name: f.name, Description: "fake", Parameters: Schema{"type": "object"}, Safety: f.safety}
}

func (f fakeTool) Execute(ctx context.Context, req Request) Result {
	return Success(f.name, req.ID, "ok", nil)
}

func (f fakeTool) ShouldDefer() bool {
	return f.defer_
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

func TestRegistryDeferredVisibility(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(fakeTool{name: "Immediate", safety: SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{name: "Deferred", safety: SafetySideEffect, defer_: true}); err != nil {
		t.Fatal(err)
	}
	defs := reg.VisibleDefinitions()
	if len(defs) != 1 || defs[0].Name != "Immediate" {
		t.Fatalf("visible definitions = %+v, want only Immediate", defs)
	}
	names := reg.DeferredNames()
	if len(names) != 1 || names[0] != "Deferred" {
		t.Fatalf("deferred names = %+v, want Deferred", names)
	}
	if !reg.MarkDiscovered("Deferred") {
		t.Fatal("MarkDiscovered returned false")
	}
	defs = reg.VisibleDefinitions()
	if len(defs) != 2 || defs[0].Name != "Deferred" || defs[1].Name != "Immediate" {
		t.Fatalf("visible after discovery = %+v", defs)
	}
	if got := reg.DeferredNames(); len(got) != 0 {
		t.Fatalf("deferred names after discovery = %+v, want none", got)
	}
}

func TestRegistryVisibleDefinitionsBySafetyHonorsDeferred(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(fakeTool{name: "Read", safety: SafetyReadOnly}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(fakeTool{name: "DeferredRead", safety: SafetyReadOnly, defer_: true}); err != nil {
		t.Fatal(err)
	}
	readOnly := reg.VisibleDefinitionsBySafety(SafetyReadOnly)
	if len(readOnly) != 1 || readOnly[0].Name != "Read" {
		t.Fatalf("read only visible = %+v", readOnly)
	}
	names := reg.DeferredNamesBySafety(SafetyReadOnly)
	if len(names) != 1 || names[0] != "DeferredRead" {
		t.Fatalf("deferred read only names = %+v", names)
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
