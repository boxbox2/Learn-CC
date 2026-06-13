package app

import (
	"context"
	"fmt"
	"os"

	"mewcode/internal/chat"
	"mewcode/internal/config"
	"mewcode/internal/markdown"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	_ "mewcode/internal/provider/anthropic"
	_ "mewcode/internal/provider/openai"
	"mewcode/internal/tool"
	"mewcode/internal/tool/builtin"
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
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	loaded, err := config.LoadDetailedWithOptions(config.LoadOptions{HomeDir: home, ProjectDir: projectDir})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg := loaded.Config
	providerCfg, err := cfg.ActiveProvider()
	if err != nil {
		return err
	}
	llm, err := provider.NewFactory().Create(cfg.Active, providerCfg)
	if err != nil {
		return err
	}
	registry := tool.NewRegistry()
	if err := builtin.RegisterDefaults(registry); err != nil {
		return fmt.Errorf("register tools: %w", err)
	}
	permissions := permission.NewManager(permission.ManagerOptions{
		Mode:   cfg.Permissions.Mode,
		Layers: loaded.PermissionLayers,
		Store:  permission.YAMLRuleStore{ProjectDir: projectDir},
	})
	session := chat.NewSessionWithOptions(llm, providerCfg, chat.SessionOptions{
		Tools:       registry,
		WorkingDir:  projectDir,
		Limits:      tool.DefaultLimits(),
		PathPolicy:  tool.PathPolicy{Root: projectDir},
		Permissions: permissions,
	})
	renderer := markdown.NewRenderer()
	return tui.Run(ctx, cfg, session, renderer)
}
