package app

import (
	"context"
	"fmt"
	"os"

	"mewcode/internal/chat"
	"mewcode/internal/config"
	"mewcode/internal/markdown"
	"mewcode/internal/provider"
	_ "mewcode/internal/provider/anthropic"
	_ "mewcode/internal/provider/openai"
	"mewcode/internal/tui"
)

type App struct{}

func New() App {
	return App{}
}

func (App) RunChat(ctx context.Context) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig(projectDir)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	providerCfg, err := cfg.ActiveProvider()
	if err != nil {
		return err
	}
	llm, err := provider.NewFactory().Create(cfg.Active, providerCfg)
	if err != nil {
		return err
	}
	session := chat.NewSession(llm, providerCfg)
	renderer := markdown.NewRenderer()
	return tui.Run(ctx, cfg, session, renderer)
}
