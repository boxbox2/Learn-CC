package config

import "mewcode/internal/permission"

func mergeConfig(base, override AppConfig) AppConfig {
	out := AppConfig{
		Active:      base.Active,
		Providers:   map[string]ProviderConfig{},
		Permissions: base.Permissions,
		MCP:         MCPConfig{Servers: map[string]MCPServerConfig{}},
	}
	for name, provider := range base.Providers {
		out.Providers[name] = provider
	}
	for name, server := range base.MCP.Servers {
		out.MCP.Servers[name] = server
	}
	if override.Active != "" {
		out.Active = override.Active
	}
	for name, provider := range override.Providers {
		current := out.Providers[name]
		if provider.Protocol != "" {
			current.Protocol = provider.Protocol
		}
		if provider.Model != "" {
			current.Model = provider.Model
		}
		if provider.BaseURL != "" {
			current.BaseURL = provider.BaseURL
		}
		if provider.APIKey != "" {
			current.APIKey = provider.APIKey
		}
		if provider.ContextWindow != 0 {
			current.ContextWindow = provider.ContextWindow
		}
		current.Thinking = mergeThinking(current.Thinking, provider.Thinking)
		out.Providers[name] = current
	}
	if override.Permissions.Mode != "" {
		out.Permissions.Mode = override.Permissions.Mode
	}
	if len(override.Permissions.Rules) > 0 {
		out.Permissions.Rules = append(append([]permission.Rule(nil), out.Permissions.Rules...), override.Permissions.Rules...)
	}
	for name, server := range override.MCP.Servers {
		out.MCP.Servers[name] = server
	}
	return out
}

func mergeThinking(base, override ThinkingConfig) ThinkingConfig {
	out := base
	if override.Enabled {
		out.Enabled = true
	}
	if override.BudgetTokens != 0 {
		out.BudgetTokens = override.BudgetTokens
	}
	if override.ShowByDefault {
		out.ShowByDefault = true
	}
	return out
}
