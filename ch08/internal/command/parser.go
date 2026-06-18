package command

import "strings"

func (r *Registry) Parse(input string) ParseResult {
	raw := input
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ParseResult{Empty: true}
	}
	if !strings.HasPrefix(trimmed, "/") {
		return ParseResult{Chat: true, Input: trimmed}
	}
	name, args, ok := commandNameAndArgs(trimmed)
	if !ok {
		return ParseResult{Unknown: trimmed}
	}
	def, found := r.Lookup(name)
	if !found {
		return ParseResult{Unknown: name}
	}
	if strings.TrimSpace(args) != "" && !def.AcceptsArgs {
		return ParseResult{Unknown: trimmed}
	}
	inv := Invocation{
		Raw:        raw,
		Name:       name,
		Canonical:  normalizeName(def.Name),
		Args:       strings.TrimSpace(args),
		Definition: def,
	}
	return ParseResult{Command: &inv}
}

func commandNameAndArgs(input string) (string, string, bool) {
	for i, r := range input {
		if i == 0 {
			continue
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			name := input[:i]
			return name, input[i:], true
		}
	}
	return input, "", true
}

func CanExecute(kind Kind, state AgentState) bool {
	switch kind {
	case KindReadOnly, KindExit, KindLocal:
		return state == AgentStateIdle || state == AgentStateRunning
	case KindUI, KindPrompt:
		return state == AgentStateIdle
	default:
		return false
	}
}
