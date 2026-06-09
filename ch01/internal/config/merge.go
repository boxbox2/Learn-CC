package config

func mergeConfig(base, override AppConfig) AppConfig {
	out := AppConfig{
		Active:    base.Active,
		Providers: map[string]ProviderConfig{},
	}
	for name, provider := range base.Providers {
		out.Providers[name] = provider
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
		current.Thinking = mergeThinking(current.Thinking, provider.Thinking)
		out.Providers[name] = current
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
