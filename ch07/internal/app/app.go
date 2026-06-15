package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"mewcode/internal/agent"
	"mewcode/internal/chat"
	"mewcode/internal/command"
	commandbuiltin "mewcode/internal/command/builtin"
	"mewcode/internal/config"
	"mewcode/internal/contextmgr"
	"mewcode/internal/instructions"
	"mewcode/internal/markdown"
	"mewcode/internal/mcp"
	"mewcode/internal/memory"
	"mewcode/internal/permission"
	"mewcode/internal/provider"
	_ "mewcode/internal/provider/anthropic"
	_ "mewcode/internal/provider/openai"
	"mewcode/internal/sessionstore"
	"mewcode/internal/tool"
	"mewcode/internal/tool/builtin"
	"mewcode/internal/tui"
)

type App struct{}

func New() App {
	return App{}
}

type ChatOptions struct {
	Resume       string
	ListSessions bool
}

func (App) RunChat(ctx context.Context, opts ChatOptions) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}
	if opts.ListSessions {
		return listSessions(projectDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	loaded, err := config.LoadDetailedWithOptions(config.LoadOptions{HomeDir: home, ProjectDir: projectDir})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	instructionResult, err := instructions.Load(instructions.LoadOptions{HomeDir: home, ProjectDir: projectDir})
	if err != nil {
		return fmt.Errorf("load instructions: %w", err)
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
	memoryManager, err := memory.NewManager(memory.Options{ProjectDir: projectDir, HomeDir: home, Provider: llm, Config: providerCfg})
	if err != nil {
		return fmt.Errorf("create memory manager: %w", err)
	}
	if _, err := sessionstore.Cleanup(projectDir, 30*24*time.Hour, time.Now()); err != nil {
		log.Printf("session cleanup failed: %v", err)
	}
	registry := tool.NewRegistry()
	if err := builtin.RegisterDefaults(registry); err != nil {
		return fmt.Errorf("register tools: %w", err)
	}
	mcpManager := mcp.NewManager(cfg.MCP, registry)
	if err := mcpManager.Start(ctx); err != nil {
		return fmt.Errorf("start mcp: %w", err)
	}
	defer mcpManager.Close(context.Background())
	permissions := permission.NewManager(permission.ManagerOptions{
		Mode:   cfg.Permissions.Mode,
		Layers: loaded.PermissionLayers,
		Store:  permission.YAMLRuleStore{ProjectDir: projectDir},
	})
	contextManager, err := contextmgr.NewManager(projectDir, providerCfg, llm)
	if err != nil {
		return fmt.Errorf("create context manager: %w", err)
	}
	archive, initialHistory, restoredAt, err := openArchive(ctx, projectDir, opts.Resume, contextManager, registry)
	if err != nil {
		return err
	}
	session := chat.NewSessionWithOptions(llm, providerCfg, chat.SessionOptions{
		Tools:          registry,
		WorkingDir:     projectDir,
		Limits:         tool.DefaultLimits(),
		PathPolicy:     tool.PathPolicy{Root: projectDir},
		Context:        contextManager,
		Archive:        archive,
		Memory:         memoryManager,
		PromptCtx:      appPromptContext{Instructions: instructionResult.Text, Memory: memoryManager},
		InitialHistory: initialHistory,
		LastRestoredAt: restoredAt,
		Permissions:    permissions,
	})
	commands := command.NewRegistry()
	commandbuiltin.Register(commands)
	commands.MustValidate()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	renderer := markdown.NewRenderer()
	return tui.Run(runCtx, cfg, session, renderer, commands, cancel)
}

func listSessions(projectDir string) error {
	summaries, err := sessionstore.Scan(projectDir)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		fmt.Println("No saved sessions.")
		return nil
	}
	for _, summary := range summaries {
		fmt.Printf("%s\t%s\t%d messages\t%s\n", summary.ID, summary.UpdatedAt.Format(time.RFC3339), summary.MessageCount, summary.Title)
	}
	return nil
}

func openArchive(ctx context.Context, projectDir, resume string, contextManager *contextmgr.Manager, registry *tool.Registry) (*sessionstore.Writer, []provider.ChatMessage, time.Time, error) {
	if resume == "" {
		archive, err := sessionstore.Create(projectDir)
		if err != nil {
			return nil, nil, time.Time{}, fmt.Errorf("create session archive: %w", err)
		}
		return archive, nil, time.Time{}, nil
	}
	id := resume
	if resume == "latest" {
		summaries, err := sessionstore.Scan(projectDir)
		if err != nil {
			return nil, nil, time.Time{}, err
		}
		if len(summaries) == 0 {
			return nil, nil, time.Time{}, fmt.Errorf("no saved sessions to resume")
		}
		id = summaries[0].ID
	}
	if !sessionstore.ValidID(id) {
		return nil, nil, time.Time{}, fmt.Errorf("invalid session id %q", id)
	}
	path := filepath.Join(projectDir, ".mewcode", "sessions", id+".jsonl")
	restored, err := sessionstore.Restore(path)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("restore session %s: %w", id, err)
	}
	history := restored.Messages
	if contextManager != nil {
		allowed := agent.AllowedDefinitions(registry, agent.ToolSet{Mode: agent.ToolSetAll})
		estimate := contextManager.Estimator.Estimate(history, contextmgr.UsageAnchor{})
		if estimate.Tokens >= contextManager.Estimator.ContextWindow-contextmgr.ManualSafetyMarginTokens {
			managed, err := contextManager.ForceCompact(ctx, history, allowed)
			if err != nil {
				return nil, nil, time.Time{}, fmt.Errorf("compact restored session %s: %w", id, err)
			}
			history = managed.Messages
		}
	}
	archive, err := sessionstore.Open(projectDir, id)
	if err != nil {
		return nil, nil, time.Time{}, fmt.Errorf("open session archive %s: %w", id, err)
	}
	return archive, history, restored.LastMessageAt, nil
}

type appPromptContext struct {
	Instructions string
	Memory       *memory.Manager
}

func (c appPromptContext) CustomInstructions(ctx context.Context) string {
	return c.Instructions
}

func (c appPromptContext) LongTermMemory(ctx context.Context) string {
	if c.Memory == nil {
		return ""
	}
	return c.Memory.PromptIndex(ctx)
}
