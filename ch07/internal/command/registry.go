package command

import (
	"fmt"
	"strings"
)

type Registry struct {
	defs  []Definition
	byKey map[string]int
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(def Definition) error {
	if strings.TrimSpace(def.Name) == "" {
		return fmt.Errorf("command name is required")
	}
	if !strings.HasPrefix(def.Name, "/") {
		return fmt.Errorf("command name %q must start with /", def.Name)
	}
	if def.Kind == "" {
		return fmt.Errorf("command %q kind is required", def.Name)
	}
	if def.Handler == nil {
		return fmt.Errorf("command %q handler is required", def.Name)
	}
	r.defs = append(r.defs, cloneDefinition(def))
	return nil
}

func (r *Registry) MustValidate() {
	byKey := map[string]int{}
	for i, def := range r.defs {
		keys := append([]string{def.Name}, def.Aliases...)
		for _, key := range keys {
			norm := normalizeName(key)
			if norm == "" {
				panic(fmt.Sprintf("command %q has empty name or alias", def.Name))
			}
			if existing, ok := byKey[norm]; ok {
				panic(fmt.Sprintf("command name conflict %q between %s and %s", norm, r.defs[existing].Name, def.Name))
			}
			byKey[norm] = i
		}
	}
	r.byKey = byKey
}

func (r *Registry) Lookup(name string) (Definition, bool) {
	if r == nil {
		return Definition{}, false
	}
	if r.byKey == nil {
		r.MustValidate()
	}
	idx, ok := r.byKey[normalizeName(name)]
	if !ok {
		return Definition{}, false
	}
	return cloneDefinition(r.defs[idx]), true
}

func (r *Registry) Visible() []Definition {
	if r == nil {
		return nil
	}
	out := make([]Definition, 0, len(r.defs))
	for _, def := range r.defs {
		if def.Hidden {
			continue
		}
		out = append(out, cloneDefinition(def))
	}
	return out
}

func normalizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.ToLower(name)
}

func cloneDefinition(def Definition) Definition {
	def.Aliases = append([]string(nil), def.Aliases...)
	return def
}
