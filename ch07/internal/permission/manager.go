package permission

import (
	"context"
	"fmt"
	"sync"
)

type Manager struct {
	mode     Mode
	layers   []Layer
	state    *managerState
	prompter Prompter
	store    RuleStore
}

type managerState struct {
	mu           sync.Mutex
	sessionRules []Rule
}

type ManagerOptions struct {
	Mode     Mode
	Layers   []Layer
	Prompter Prompter
	Store    RuleStore
}

func NewManager(opts ManagerOptions) *Manager {
	return &Manager{
		mode:     opts.Mode,
		layers:   append([]Layer(nil), opts.Layers...),
		state:    &managerState{},
		prompter: opts.Prompter,
		store:    opts.Store,
	}
}

func (m *Manager) Mode() Mode {
	if m == nil {
		return ""
	}
	return m.mode
}

func (m *Manager) WithPrompter(prompter Prompter) *Manager {
	if m == nil {
		return nil
	}
	copy := *m
	copy.prompter = prompter
	return &copy
}

func (m *Manager) Authorize(ctx context.Context, req Request) Decision {
	if hard := CheckHardBlock(req); hard.Status == DecisionDeny {
		return hard
	}
	engine := NewRuleEngine(m.layersWithSession())
	if decision := engine.Evaluate(req); decision.Status == DecisionAllow || decision.Status == DecisionDeny {
		if decision.Status == DecisionDeny {
			decision.Suggestions = deniedSuggestions(req)
		}
		return decision
	}
	modeDecision := EvaluateMode(m.mode, req)
	if modeDecision.Status == DecisionAllow || modeDecision.Status == DecisionDeny {
		return modeDecision
	}
	if m.prompter == nil {
		return deniedDecision(req, "permission requires confirmation, but no prompter is configured")
	}
	prompt := Prompt{
		ID:       req.CallID,
		Tool:     req.Tool,
		Summary:  summaryForRequest(req),
		Reason:   modeDecision.Reason,
		Options:  []UserGrant{GrantOnce, GrantSession, GrantPermanent, GrantDeny},
		Response: make(chan UserGrant, 1),
	}
	switch m.prompter.Confirm(ctx, prompt) {
	case GrantOnce:
		return Decision{Status: DecisionAllow, Reason: "user allowed this tool call"}
	case GrantSession:
		m.addSessionAllow(req)
		return Decision{Status: DecisionAllow, Reason: "user allowed matching calls for this session"}
	case GrantPermanent:
		pattern := patternForRequest(req)
		if m.store != nil {
			if err := m.store.AppendAllowRule(pattern); err != nil {
				return deniedDecision(req, fmt.Sprintf("failed to write permanent permission: %v", err))
			}
		}
		m.state.mu.Lock()
		m.state.sessionRules = append(m.state.sessionRules, Rule{Action: ActionAllow, Pattern: pattern})
		m.state.mu.Unlock()
		return Decision{Status: DecisionAllow, Reason: "user permanently allowed matching calls for this project"}
	default:
		return deniedDecision(req, "user denied permission")
	}
}

func (m *Manager) layersWithSession() []Layer {
	layers := make([]Layer, 0, len(m.layers)+1)
	m.state.mu.Lock()
	sessionRules := append([]Rule(nil), m.state.sessionRules...)
	m.state.mu.Unlock()
	if len(sessionRules) > 0 {
		layers = append(layers, Layer{
			Source: Source{Kind: SourceSession},
			Rules:  sessionRules,
		})
	}
	layers = append(layers, m.layers...)
	return layers
}

func (m *Manager) addSessionAllow(req Request) {
	m.state.mu.Lock()
	defer m.state.mu.Unlock()
	m.state.sessionRules = append(m.state.sessionRules, Rule{Action: ActionAllow, Pattern: patternForRequest(req)})
}

func deniedDecision(req Request, reason string) Decision {
	return Decision{Status: DecisionDeny, Reason: reason, Suggestions: deniedSuggestions(req)}
}

func deniedSuggestions(req Request) []string {
	switch req.Tool {
	case "Bash":
		return []string{
			"Try a narrower command that only inspects project state.",
			"Use read-only tools when possible.",
			"Ask the user to allow a more specific Bash rule.",
		}
	case "Write", "Edit":
		return []string{
			"Read the relevant file first and propose a smaller edit.",
			"Ask the user to allow a specific path pattern.",
			"Continue with analysis if editing is not allowed.",
		}
	default:
		return []string{
			"Try a narrower tool call.",
			"Ask the user for explicit permission.",
		}
	}
}

func summaryForRequest(req Request) string {
	if req.Tool == "Bash" {
		return req.Fields["command"]
	}
	if path, ok := req.Fields["path"]; ok {
		return path
	}
	if pattern, ok := req.Fields["pattern"]; ok {
		return pattern
	}
	return string(req.Arguments)
}

func patternForRequest(req Request) string {
	field := primaryField(req.Tool, req.Fields)
	value := req.Fields[field]
	if value == "" {
		value = "*"
	}
	return fmt.Sprintf("%s(%s=%s)", req.Tool, field, value)
}
