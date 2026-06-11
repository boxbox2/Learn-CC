package anthropic

import (
	"mewcode/internal/config"
	"mewcode/internal/provider"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type Provider struct {
	name   string
	cfg    config.ProviderConfig
	client anthropicsdk.Client
}

func init() {
	provider.Register(config.ProtocolAnthropic, func(name string, cfg config.ProviderConfig) provider.Provider {
		return NewProvider(name, cfg)
	})
}

func NewProvider(name string, cfg config.ProviderConfig) *Provider {
	options := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		options = append(options, option.WithBaseURL(cfg.BaseURL))
	}
	return &Provider{
		name:   name,
		cfg:    cfg,
		client: anthropicsdk.NewClient(options...),
	}
}
