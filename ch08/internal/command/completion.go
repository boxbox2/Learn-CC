package command

import "strings"

type CompletionItem struct {
	Canonical   string
	Display     string
	Description string
}

type CompletionResult struct {
	Items   []CompletionItem
	NoMatch bool
}

type CompletionState struct {
	Active      bool
	Query       string
	Items       []CompletionItem
	Highlighted int
	NoMatch     bool
}

func (r *Registry) Complete(input string) CompletionResult {
	if !strings.HasPrefix(input, "/") || strings.Contains(input, "\n") {
		return CompletionResult{}
	}
	query := normalizeName(input)
	var items []CompletionItem
	for _, def := range r.Visible() {
		if matchesCompletion(def, query) {
			items = append(items, CompletionItem{
				Canonical:   normalizeName(def.Name),
				Display:     def.Name,
				Description: def.Description,
			})
		}
	}
	if len(items) == 0 {
		return CompletionResult{NoMatch: true}
	}
	return CompletionResult{Items: items}
}

func matchesCompletion(def Definition, query string) bool {
	if strings.HasPrefix(normalizeName(def.Name), query) {
		return true
	}
	for _, alias := range def.Aliases {
		if strings.HasPrefix(normalizeName(alias), query) {
			return true
		}
	}
	return false
}
