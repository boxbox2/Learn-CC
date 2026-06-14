package tool

import (
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	tools      map[string]Tool
	discovered map[string]bool
}

func NewRegistry() *Registry {
	return &Registry{
		tools:      map[string]Tool{},
		discovered: map[string]bool{},
	}
}

func (r *Registry) Register(t Tool) error {
	def := t.Definition()
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return fmt.Errorf("tool name is required")
	}
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("tool %q is already registered", name)
	}
	r.tools[name] = t
	return nil
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Definitions() []Definition {
	return r.definitions(false, "", false, false)
}

func (r *Registry) DefinitionByName(name string) (Definition, bool) {
	t, ok := r.tools[name]
	if !ok {
		return Definition{}, false
	}
	return t.Definition(), true
}

func (r *Registry) MarkDiscovered(name string) bool {
	if !r.IsDeferred(name) {
		return false
	}
	r.discovered[name] = true
	return true
}

func (r *Registry) IsDeferred(name string) bool {
	t, ok := r.tools[name]
	if !ok {
		return false
	}
	deferred, ok := t.(DeferredTool)
	return ok && deferred.ShouldDefer()
}

func (r *Registry) IsDiscovered(name string) bool {
	return r.discovered[name]
}

func (r *Registry) VisibleDefinitions() []Definition {
	return r.definitions(true, "", false, false)
}

func (r *Registry) VisibleDefinitionsBySafety(safety Safety) []Definition {
	return r.definitions(true, safety, true, false)
}

func (r *Registry) DeferredNames() []string {
	return r.deferredNames("", false)
}

func (r *Registry) DeferredNamesBySafety(safety Safety) []string {
	return r.deferredNames(safety, true)
}

func (r *Registry) definitions(visibleOnly bool, safety Safety, filterSafety bool, deferredOnly bool) []Definition {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		if visibleOnly && r.IsDeferred(name) && !r.discovered[name] {
			continue
		}
		if deferredOnly && (!r.IsDeferred(name) || r.discovered[name]) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	defs := make([]Definition, 0, len(names))
	for _, name := range names {
		def := r.tools[name].Definition()
		if filterSafety && def.EffectiveSafety() != safety {
			continue
		}
		defs = append(defs, def)
	}
	return defs
}

func (r *Registry) DefinitionsBySafety(safety Safety) []Definition {
	return r.definitions(false, safety, true, false)
}

func (r *Registry) deferredNames(safety Safety, filterSafety bool) []string {
	defs := r.definitions(false, safety, filterSafety, true)
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}
