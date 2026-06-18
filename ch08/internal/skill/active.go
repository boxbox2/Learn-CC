package skill

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"mewcode/internal/tool"
)

type ActiveStore struct {
	mu     sync.RWMutex
	order  []string
	bodies map[string]string
	tools  map[string]ActiveTool
}

func NewActiveStore() *ActiveStore {
	return &ActiveStore{bodies: map[string]string{}, tools: map[string]ActiveTool{}}
}

func (s *ActiveStore) Activate(name, body string, tools []tool.Tool) error {
	if s == nil {
		return nil
	}
	name = NormalizeName(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.bodies == nil {
		s.bodies = map[string]string{}
	}
	if s.tools == nil {
		s.tools = map[string]ActiveTool{}
	}
	if _, exists := s.bodies[name]; !exists {
		s.order = append(s.order, name)
	}
	for _, t := range tools {
		toolName := t.Definition().Name
		if existing, ok := s.tools[toolName]; ok && existing.Skill != name {
			return fmt.Errorf("specialized tool %q already active for skill %q", toolName, existing.Skill)
		}
	}
	for toolName, active := range s.tools {
		if active.Skill == name {
			delete(s.tools, toolName)
		}
	}
	s.bodies[name] = strings.TrimSpace(body)
	for _, t := range tools {
		s.tools[t.Definition().Name] = ActiveTool{Skill: name, Tool: t}
	}
	return nil
}

func (s *ActiveStore) Clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = nil
	s.bodies = map[string]string{}
	s.tools = map[string]ActiveTool{}
}

func (s *ActiveStore) List() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.order...)
}

func (s *ActiveStore) PromptText() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var parts []string
	for _, name := range s.order {
		body := strings.TrimSpace(s.bodies[name])
		if body == "" {
			continue
		}
		parts = append(parts, "### Skill: "+name+"\n\n"+body)
	}
	return strings.Join(parts, "\n\n")
}

func (s *ActiveStore) Get(name string) (tool.Tool, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	active, ok := s.tools[name]
	return active.Tool, ok
}

func (s *ActiveStore) Definitions() []tool.Definition {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	defs := make([]tool.Definition, 0, len(names))
	for _, name := range names {
		defs = append(defs, s.tools[name].Tool.Definition())
	}
	return defs
}

func (s *ActiveStore) VisibleDefinitions() []tool.Definition {
	return s.Definitions()
}

func (s *ActiveStore) VisibleDefinitionsBySafety(safety tool.Safety) []tool.Definition {
	defs := s.Definitions()
	out := make([]tool.Definition, 0, len(defs))
	for _, def := range defs {
		if def.EffectiveSafety() == safety {
			out = append(out, def)
		}
	}
	return out
}

func (s *ActiveStore) DeferredNames() []string {
	return nil
}

func (s *ActiveStore) DeferredNamesBySafety(safety tool.Safety) []string {
	return nil
}
