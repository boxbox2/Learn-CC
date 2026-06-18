package skill

import (
	"context"
	"fmt"
	"strings"

	"mewcode/internal/agent"
	"mewcode/internal/config"
	"mewcode/internal/contextmgr"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

type Executor struct {
	Catalog     *Catalog
	Active      *ActiveStore
	Tools       *tool.Registry
	Provider    provider.Provider
	Config      config.ProviderConfig
	WorkDir     string
	Limits      tool.Limits
	Paths       tool.PathPolicy
	Context     *contextmgr.Manager
	Permissions *permission.Manager
}

func RenderPrompt(def Definition, args string) string {
	body := strings.TrimSpace(def.Body)
	args = strings.TrimSpace(args)
	if strings.Contains(body, "$ARGUMENTS") {
		return strings.ReplaceAll(body, "$ARGUMENTS", args)
	}
	if args != "" {
		body += "\n\n## User Request\n\n" + args
	}
	return body
}

func prependSuggestedTools(def Definition, prompt string) string {
	if len(def.Metadata.AllowedTools) == 0 {
		return prompt
	}
	return "Suggested tools for this skill: " + strings.Join(def.Metadata.AllowedTools, ", ") + "\n\n" + prompt
}

func (e *Executor) Definition(name string) (Definition, error) {
	if e == nil || e.Catalog == nil {
		return Definition{}, fmt.Errorf("skill catalog is not configured")
	}
	def, ok := e.Catalog.Get(name)
	if !ok {
		return Definition{}, fmt.Errorf("unknown skill: %s", NormalizeName(name))
	}
	if latest, _, err := ParseSkill(def.Entry, def.Source); err == nil && strings.TrimSpace(latest.Body) != "" {
		latest.Source = def.Source
		return latest, nil
	}
	return def, nil
}

func RenderExecutionPrompt(def Definition, args string) string {
	return prependSuggestedTools(def, RenderPrompt(def, args))
}

func (e *Executor) Activate(def Definition) error {
	if e == nil {
		return fmt.Errorf("skill executor is not configured")
	}
	if e.Active == nil {
		return fmt.Errorf("active skill store is not configured")
	}
	return e.Active.Activate(def.Metadata.Name, def.Body, makeExecTools(def))
}

func (e *Executor) RunFork(ctx context.Context, def Definition, rendered string, history []provider.ChatMessage) (agent.Result, error) {
	if e == nil || e.Provider == nil {
		return agent.Result{}, fmt.Errorf("provider is not configured")
	}
	childHistory, err := e.forkHistory(ctx, def, history)
	if err != nil {
		return agent.Result{}, err
	}
	childHistory = append(childHistory, provider.ChatMessage{Role: provider.RoleUser, Content: rendered})
	cfg := e.Config
	if strings.TrimSpace(def.Metadata.Model) != "" {
		cfg.Model = strings.TrimSpace(def.Metadata.Model)
	}
	run, err := (&agent.Runner{
		Provider:          e.Provider,
		Config:            cfg,
		Tools:             e.Tools,
		WorkDir:           e.WorkDir,
		Limits:            e.Limits,
		Paths:             e.Paths,
		PermissionManager: e.Permissions,
		ContextManager:    e.Context,
	}).Run(ctx, agent.RunRequest{
		Messages: childHistory,
		Mode:     agent.RunModeExecute,
		Tools: agent.ToolSet{
			Mode:    agent.ToolSetAll,
			Names:   def.Metadata.AllowedTools,
			Overlay: e.Active,
		},
	})
	if err != nil {
		return agent.Result{}, err
	}
	for range run.Events {
	}
	result := <-run.Done
	return result, result.Err
}

func (e *Executor) forkHistory(ctx context.Context, def Definition, history []provider.ChatMessage) ([]provider.ChatMessage, error) {
	mode := def.Metadata.ForkContext
	switch mode {
	case ForkContextRecent:
		if len(history) > 5 {
			history = history[len(history)-5:]
		}
		return append([]provider.ChatMessage(nil), history...), nil
	case ForkContextFull:
		if e != nil && e.Context != nil {
			allowed := agent.AllowedDefinitions(e.Tools, agent.ToolSet{
				Mode:    agent.ToolSetAll,
				Names:   def.Metadata.AllowedTools,
				Overlay: e.Active,
			})
			managed, err := e.Context.ForceCompact(ctx, history, allowed)
			if err != nil {
				return nil, err
			}
			return append([]provider.ChatMessage(nil), managed.Messages...), nil
		}
		return []provider.ChatMessage{{Role: provider.RoleSystem, Content: "Parent conversation summary is unavailable because the context manager is not configured."}}, nil
	default:
		return nil, nil
	}
}
