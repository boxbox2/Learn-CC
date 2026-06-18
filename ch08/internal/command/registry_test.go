package command

import (
	"context"
	"testing"
)

func TestRegistryRejectsInvalidDefinitions(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(Definition{Name: "help", Kind: KindReadOnly, Handler: noopHandler}); err == nil {
		t.Fatal("expected missing slash error")
	}
	if err := reg.Register(Definition{Name: "/help", Handler: noopHandler}); err == nil {
		t.Fatal("expected missing kind error")
	}
	if err := reg.Register(Definition{Name: "/help", Kind: KindReadOnly}); err == nil {
		t.Fatal("expected missing handler error")
	}
}

func TestRegistryPanicsOnNameConflict(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/help", Kind: KindReadOnly, Handler: noopHandler})
	mustRegister(t, reg, Definition{Name: "/HELP", Kind: KindReadOnly, Handler: noopHandler})
	expectPanic(t, reg.MustValidate)
}

func TestRegistryPanicsOnNameAliasConflict(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/help", Kind: KindReadOnly, Handler: noopHandler})
	mustRegister(t, reg, Definition{Name: "/status", Aliases: []string{"/HELP"}, Kind: KindReadOnly, Handler: noopHandler})
	expectPanic(t, reg.MustValidate)
}

func TestRegistryPanicsOnAliasAliasConflict(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/help", Aliases: []string{"/h"}, Kind: KindReadOnly, Handler: noopHandler})
	mustRegister(t, reg, Definition{Name: "/status", Aliases: []string{"/H"}, Kind: KindReadOnly, Handler: noopHandler})
	expectPanic(t, reg.MustValidate)
}

func TestRegistryVisibleSkipsHidden(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/help", Kind: KindReadOnly, Handler: noopHandler})
	mustRegister(t, reg, Definition{Name: "/hidden", Kind: KindReadOnly, Hidden: true, Handler: noopHandler})
	reg.MustValidate()
	visible := reg.Visible()
	if len(visible) != 1 || visible[0].Name != "/help" {
		t.Fatalf("visible = %+v", visible)
	}
}

func TestRegistryLookupClonesDefinition(t *testing.T) {
	reg := NewRegistry()
	mustRegister(t, reg, Definition{Name: "/help", Aliases: []string{"/h"}, Kind: KindReadOnly, Handler: noopHandler})
	reg.MustValidate()
	def, ok := reg.Lookup("/H")
	if !ok {
		t.Fatal("expected alias lookup")
	}
	def.Aliases[0] = "/mutated"
	def2, _ := reg.Lookup("/help")
	if def2.Aliases[0] != "/h" {
		t.Fatalf("aliases mutated: %+v", def2.Aliases)
	}
}

func mustRegister(t *testing.T, reg *Registry, def Definition) {
	t.Helper()
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
}

func expectPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	fn()
}

var noopHandler Handler = func(ctx context.Context, inv Invocation, c Controller) (Result, error) {
	return Result{}, nil
}
