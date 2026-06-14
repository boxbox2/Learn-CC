package config

import (
	"fmt"
	"strings"

	"mewcode/internal/permission"
)

const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
)

const (
	MCPTransportStdio = "stdio"
	MCPTransportHTTP  = "http"
)

type AppConfig struct {
	Active      string                    `yaml:"active"`
	Providers   map[string]ProviderConfig `yaml:"providers"`
	Permissions permission.Config         `yaml:"permissions"`
	MCP         MCPConfig                 `yaml:"mcp"`
}

type ProviderConfig struct {
	Protocol string         `yaml:"protocol"`
	Model    string         `yaml:"model"`
	BaseURL  string         `yaml:"base_url"`
	APIKey   string         `yaml:"api_key"`
	Thinking ThinkingConfig `yaml:"thinking"`
}

type ThinkingConfig struct {
	Enabled       bool `yaml:"enabled"`
	BudgetTokens  int  `yaml:"budget_tokens"`
	ShowByDefault bool `yaml:"show_by_default"`
}

type MCPConfig struct {
	Servers map[string]MCPServerConfig `yaml:"servers"`
}

type MCPServerConfig struct {
	Type    string            `yaml:"type"`
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

func (c AppConfig) ActiveProvider() (ProviderConfig, error) {
	if strings.TrimSpace(c.Active) == "" {
		return ProviderConfig{}, fmt.Errorf("active provider is required")
	}
	cfg, ok := c.Providers[c.Active]
	if !ok {
		return ProviderConfig{}, fmt.Errorf("active provider %q does not exist; available providers: %s", c.Active, strings.Join(c.ProviderNames(), ", "))
	}
	return cfg, nil
}

func (c AppConfig) ProviderNames() []string {
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, name)
	}
	return names
}

func Validate(cfg AppConfig) error {
	if len(cfg.Providers) == 0 {
		return fmt.Errorf("providers must contain at least one provider")
	}
	if strings.TrimSpace(cfg.Active) == "" {
		return fmt.Errorf("active provider is required")
	}
	activeProvider, ok := cfg.Providers[cfg.Active]
	if !ok {
		return fmt.Errorf("active provider %q does not exist; available providers: %s", cfg.Active, strings.Join(cfg.ProviderNames(), ", "))
	}
	if err := validateProvider(cfg.Active, activeProvider); err != nil {
		return err
	}
	for name, provider := range cfg.Providers {
		if name == cfg.Active {
			continue
		}
		if err := validateInactiveProvider(name, provider); err != nil {
			return err
		}
	}
	if err := validatePermissions(cfg.Permissions); err != nil {
		return err
	}
	if err := validateMCP(cfg.MCP); err != nil {
		return err
	}
	return nil
}

func validatePermissions(cfg permission.Config) error {
	switch cfg.Mode {
	case "", permission.ModeDefault, permission.ModeAcceptEdits, permission.ModePlan, permission.ModeBypassPermissions:
	default:
		return fmt.Errorf("permissions mode %q is not supported", cfg.Mode)
	}
	for _, rule := range cfg.Rules {
		if _, err := permission.ParseRule(rule); err != nil {
			return err
		}
	}
	return nil
}

func validateMCP(cfg MCPConfig) error {
	for name, server := range cfg.Servers {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("mcp server name is required")
		}
		transport := strings.TrimSpace(server.Type)
		switch transport {
		case MCPTransportStdio:
			if strings.TrimSpace(server.Command) == "" {
				return fmt.Errorf("mcp server %q command is required for stdio transport", name)
			}
			if strings.TrimSpace(server.URL) != "" {
				return fmt.Errorf("mcp server %q url is not supported for stdio transport", name)
			}
		case MCPTransportHTTP:
			if strings.TrimSpace(server.URL) == "" {
				return fmt.Errorf("mcp server %q url is required for http transport", name)
			}
			if strings.TrimSpace(server.Command) != "" {
				return fmt.Errorf("mcp server %q command is not supported for http transport", name)
			}
			if len(server.Args) > 0 {
				return fmt.Errorf("mcp server %q args are not supported for http transport", name)
			}
		default:
			return fmt.Errorf("mcp server %q type %q is not supported; expected stdio or http", name, server.Type)
		}
	}
	return nil
}

func validateInactiveProvider(name string, provider ProviderConfig) error {
	if strings.TrimSpace(provider.Protocol) == "" {
		return nil
	}
	switch provider.Protocol {
	case ProtocolOpenAI, ProtocolAnthropic:
	default:
		return fmt.Errorf("provider %q protocol %q is not supported; expected openai or anthropic", name, provider.Protocol)
	}
	if provider.Protocol == ProtocolAnthropic && provider.Thinking.Enabled {
		if provider.Thinking.BudgetTokens > 0 && provider.Thinking.BudgetTokens < 1024 {
			return fmt.Errorf("provider %q thinking budget_tokens must be at least 1024 when set", name)
		}
	}
	return nil
}

func validateProvider(name string, provider ProviderConfig) error {
	if strings.TrimSpace(provider.Protocol) == "" {
		return fmt.Errorf("provider %q protocol is required", name)
	}
	switch provider.Protocol {
	case ProtocolOpenAI, ProtocolAnthropic:
	default:
		return fmt.Errorf("provider %q protocol %q is not supported; expected openai or anthropic", name, provider.Protocol)
	}
	if strings.TrimSpace(provider.Model) == "" {
		return fmt.Errorf("provider %q model is required", name)
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return fmt.Errorf("provider %q base_url is required", name)
	}
	if strings.TrimSpace(provider.APIKey) == "" {
		return fmt.Errorf("provider %q api_key is required", name)
	}
	if provider.Protocol == ProtocolAnthropic && provider.Thinking.Enabled {
		if provider.Thinking.BudgetTokens > 0 && provider.Thinking.BudgetTokens < 1024 {
			return fmt.Errorf("provider %q thinking budget_tokens must be at least 1024 when set", name)
		}
	}
	return nil
}
