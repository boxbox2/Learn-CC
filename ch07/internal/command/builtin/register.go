package builtin

import "mewcode/internal/command"

func Register(reg *command.Registry) {
	must(reg.Register(command.Definition{
		Name:        "/help",
		Description: "Show available slash commands.",
		Usage:       "/help",
		Kind:        command.KindReadOnly,
		Handler:     handleHelp,
	}))
	must(reg.Register(command.Definition{
		Name:        "/compact",
		Description: "Compact the current conversation context.",
		Usage:       "/compact",
		Kind:        command.KindUI,
		Handler:     handleCompact,
	}))
	must(reg.Register(command.Definition{
		Name:        "/clear",
		Description: "Clear the visible screen and start a new session archive.",
		Usage:       "/clear",
		Kind:        command.KindUI,
		Handler:     handleClear,
	}))
	must(reg.Register(command.Definition{
		Name:        "/plan",
		Description: "Switch to plan mode.",
		Usage:       "/plan",
		Kind:        command.KindUI,
		Handler:     handlePlan,
	}))
	must(reg.Register(command.Definition{
		Name:        "/do",
		Description: "Switch back to default execution mode.",
		Usage:       "/do",
		Kind:        command.KindUI,
		Handler:     handleDo,
	}))
	must(reg.Register(command.Definition{
		Name:        "/session",
		Description: "Show current and saved session information.",
		Usage:       "/session",
		Kind:        command.KindReadOnly,
		Handler:     handleSession,
	}))
	must(reg.Register(command.Definition{
		Name:        "/memory",
		Description: "Show long-term memory status.",
		Usage:       "/memory",
		Kind:        command.KindReadOnly,
		Handler:     handleMemory,
	}))
	must(reg.Register(command.Definition{
		Name:        "/permission",
		Description: "Show permission mode and prompt status.",
		Usage:       "/permission",
		Kind:        command.KindReadOnly,
		Handler:     handlePermission,
	}))
	must(reg.Register(command.Definition{
		Name:        "/status",
		Description: "Show provider, model, mode, state, and usage.",
		Usage:       "/status",
		Kind:        command.KindReadOnly,
		Handler:     handleStatus,
	}))
	must(reg.Register(command.Definition{
		Name:        "/review",
		Description: "Ask AI to review the current code changes.",
		Usage:       "/review",
		Kind:        command.KindPrompt,
		Handler:     handleReview,
	}))
	must(reg.Register(command.Definition{
		Name:        "/exit",
		Description: "Safely shut down the TUI.",
		Usage:       "/exit",
		Kind:        command.KindExit,
		Handler:     handleExit,
	}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
