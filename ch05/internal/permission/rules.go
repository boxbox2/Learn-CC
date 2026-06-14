package permission

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var knownTools = map[string]bool{
	"Read":  true,
	"Glob":  true,
	"Grep":  true,
	"Write": true,
	"Edit":  true,
	"Bash":  true,
}

type compiledRule struct {
	rule    Rule
	tool    string
	field   string
	pattern string
}

type RuleEngine struct {
	layers []Layer
}

func NewRuleEngine(layers []Layer) RuleEngine {
	ordered := make([]Layer, 0, len(layers))
	for _, kind := range []SourceKind{SourceSession, SourceLocal, SourceProject, SourceUser} {
		for _, layer := range layers {
			if layer.Source.Kind == kind {
				ordered = append(ordered, layer)
			}
		}
	}
	return RuleEngine{layers: ordered}
}

func ParseRule(rule Rule) (compiledRule, error) {
	if rule.Action != ActionAllow && rule.Action != ActionDeny {
		return compiledRule{}, fmt.Errorf("permission rule action %q is not supported; expected allow or deny", rule.Action)
	}
	pattern := strings.TrimSpace(rule.Pattern)
	open := strings.Index(pattern, "(")
	if open <= 0 || !strings.HasSuffix(pattern, ")") {
		return compiledRule{}, fmt.Errorf("permission rule pattern %q must use Tool(pattern)", rule.Pattern)
	}
	toolName := strings.TrimSpace(pattern[:open])
	if !knownTools[toolName] {
		return compiledRule{}, fmt.Errorf("permission rule tool %q is not supported", toolName)
	}
	body := strings.TrimSpace(pattern[open+1 : len(pattern)-1])
	if body == "" {
		return compiledRule{}, fmt.Errorf("permission rule pattern %q has empty matcher", rule.Pattern)
	}
	field := ""
	value := body
	if idx := strings.Index(body, "="); idx > 0 {
		field = strings.TrimSpace(body[:idx])
		value = strings.TrimSpace(body[idx+1:])
		if field == "" || value == "" {
			return compiledRule{}, fmt.Errorf("permission rule pattern %q has invalid field matcher", rule.Pattern)
		}
	} else {
		field = primaryField(toolName, nil)
	}
	return compiledRule{rule: rule, tool: toolName, field: field, pattern: value}, nil
}

func (e RuleEngine) Evaluate(req Request) Decision {
	for _, layer := range e.layers {
		var allow *Rule
		for i := range layer.Rules {
			rule := layer.Rules[i]
			rule.Source = layer.Source
			compiled, err := ParseRule(rule)
			if err != nil {
				continue
			}
			if !compiled.matches(req) {
				continue
			}
			if compiled.rule.Action == ActionDeny {
				return Decision{
					Status: DecisionDeny,
					Reason: fmt.Sprintf("permission denied by %s rule %s", layer.Source.Kind, rule.Pattern),
					Source: layer.Source.Kind,
					Rule:   &rule,
				}
			}
			if allow == nil {
				copyRule := rule
				allow = &copyRule
			}
		}
		if allow != nil {
			return Decision{
				Status: DecisionAllow,
				Reason: fmt.Sprintf("permission allowed by %s rule %s", layer.Source.Kind, allow.Pattern),
				Source: layer.Source.Kind,
				Rule:   allow,
			}
		}
	}
	return Decision{Status: DecisionAsk, Reason: "no permission rule matched"}
}

func (r compiledRule) matches(req Request) bool {
	if req.Tool != r.tool {
		return false
	}
	field := r.field
	if field == "" {
		field = primaryField(req.Tool, req.Fields)
	}
	value, ok := req.Fields[field]
	if !ok {
		return false
	}
	return matchPattern(r.pattern, value, field == "path")
}

func primaryField(toolName string, fields map[string]string) string {
	switch toolName {
	case "Bash":
		return "command"
	case "Read", "Write", "Edit":
		return "path"
	case "Glob":
		return "pattern"
	case "Grep":
		if fields != nil {
			if _, ok := fields["path"]; ok {
				return "path"
			}
		}
		return "pattern"
	default:
		return ""
	}
}

func matchPattern(patternValue, value string, slash bool) bool {
	patternValue = strings.TrimSpace(patternValue)
	value = strings.TrimSpace(value)
	if slash {
		patternValue = filepath.ToSlash(patternValue)
		value = filepath.ToSlash(value)
	}
	if patternValue == value {
		return true
	}
	if !slash {
		return wildcardMatch(patternValue, value)
	}
	if ok, _ := path.Match(patternValue, value); ok {
		return true
	}
	if strings.Contains(patternValue, "**") {
		suffix := strings.TrimPrefix(patternValue, "**/")
		if ok, _ := path.Match(suffix, value); ok {
			return true
		}
	}
	return false
}

func wildcardMatch(patternValue, value string) bool {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range patternValue {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(value)
}
