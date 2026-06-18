package builtin

import (
	"context"
	"fmt"
	"strings"

	"mewcode/internal/command"
)

func handleHelp(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	var b strings.Builder
	b.WriteString("available commands:\n")
	for _, def := range c.VisibleCommands() {
		fmt.Fprintf(&b, "%-12s %s", def.Name, def.Description)
		if len(def.Aliases) > 0 {
			fmt.Fprintf(&b, " (aliases: %s)", strings.Join(def.Aliases, ", "))
		}
		if def.Usage != "" {
			fmt.Fprintf(&b, "\n             usage: %s", def.Usage)
		}
		b.WriteByte('\n')
	}
	msg := strings.TrimSpace(b.String())
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg}, nil
}

func handlePlan(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	c.SetMode(command.ChatModePlan)
	msg := "Switched to [PLAN] mode."
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg, ModeChanged: true}, nil
}

func handleDo(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	c.SetMode(command.ChatModeDefault)
	msg := "Switched to [DEFAULT] mode."
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg, ModeChanged: true}, nil
}

func handleCompact(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	if err := c.Compact(ctx); err != nil {
		return command.Result{}, err
	}
	msg := "Compacting context..."
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg}, nil
}

func handleClear(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	if err := c.ClearAndResetSession(ctx); err != nil {
		return command.Result{}, err
	}
	msg := "Cleared screen and started a new session."
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg, Cleared: true}, nil
}

func handleSession(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	status, err := c.SessionStatus(ctx)
	if err != nil {
		return command.Result{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "current session: %s", valueOrDash(status.ID))
	if status.Path != "" {
		fmt.Fprintf(&b, "\npath: %s", status.Path)
	}
	fmt.Fprintf(&b, "\nmessages: %d", status.MessageCount)
	fmt.Fprintf(&b, "\nhas plan: %t", status.HasPlan)
	if len(status.Sessions) > 0 {
		b.WriteString("\n\nsaved sessions:")
		for _, summary := range status.Sessions {
			title := valueOrDash(summary.Title)
			fmt.Fprintf(&b, "\n%s  %s  %d messages  %s", summary.ID, valueOrDash(summary.UpdatedAt), summary.MessageCount, title)
			if summary.CorruptLineCount > 0 {
				fmt.Fprintf(&b, "  (%d bad lines skipped)", summary.CorruptLineCount)
			}
		}
	}
	msg := b.String()
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg}, nil
}

func handleMemory(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	status := c.MemoryStatus(ctx)
	msg := fmt.Sprintf("memory:\nuser: %t\nproject: %t\nlast error: %s", status.UserAvailable, status.ProjectAvailable, valueOrDash(status.LastError))
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg}, nil
}

func handlePermission(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	status := c.PermissionStatus(ctx)
	msg := fmt.Sprintf("permission:\nmode: %s\nactive prompt: %t\nqueued prompts: %d", valueOrDash(status.Mode), status.ActivePrompt, status.QueuedPrompts)
	if status.ActiveToolName != "" {
		msg += "\nactive tool: " + status.ActiveToolName
	}
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg}, nil
}

func handleStatus(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	status := c.AppStatus(ctx)
	usage := status.Usage
	msg := fmt.Sprintf(
		"status:\nactive: %s\nmodel: %s\nstate: %s\nmode: %s\nsession: %s\ntokens: prompt=%d completion=%d total=%d cached=%d",
		valueOrDash(status.Active),
		valueOrDash(status.Model),
		status.AgentState,
		status.Mode,
		valueOrDash(status.SessionID),
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		usage.CachedTokens,
	)
	if status.Progress != "" {
		msg += "\nprogress: " + status.Progress
	}
	if status.CurrentTool != "" {
		msg += "\ntool: " + status.CurrentTool
	}
	if status.LastError != "" {
		msg += "\nlast error: " + status.LastError
	}
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg}, nil
}

func handleSkill(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	skills := c.ListCatalogSkills()
	if len(skills) == 0 {
		msg := "No skills loaded."
		c.ShowLocalMessage(msg)
		return command.Result{Message: msg}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Available skills (%d):", len(skills))
	for _, skill := range skills {
		status := ""
		if skill.Active {
			status = " [active]"
		}
		fmt.Fprintf(&b, "\n  /%-16s %s%s", skill.Name, valueOrDash(skill.Description), status)
	}
	b.WriteString("\nType /<skill-name> to invoke a skill.")
	msg := b.String()
	c.ShowLocalMessage(msg)
	return command.Result{Message: msg}, nil
}

func handleExit(ctx context.Context, inv command.Invocation, c command.Controller) (command.Result, error) {
	if err := c.Shutdown(ctx); err != nil {
		return command.Result{}, err
	}
	return command.Result{ShouldQuit: true}, nil
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
