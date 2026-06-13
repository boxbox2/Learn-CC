package openai

import (
	"mewcode/internal/config"
	"mewcode/internal/provider"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type Provider struct {
	name   string
	cfg    config.ProviderConfig
	client openaisdk.Client
}

func init() {
	provider.Register(config.ProtocolOpenAI, func(name string, cfg config.ProviderConfig) provider.Provider {
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
		client: openaisdk.NewClient(options...),
	}
}
