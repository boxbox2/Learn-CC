package prompt

import (
	"fmt"
	"strings"
)

const (
	ModeExecute = "execute"
	ModePlan    = "plan"
)

type DynamicContext struct {
	WorkDir           string
	Mode              string
	Iteration         int
	ToolSummary       []string
	DeferredToolNames []string
	PlanContext       string
}

type SystemNote struct {
	Kind    string
	Content string
}

func (n SystemNote) TaggedContent() string {
	kind := strings.TrimSpace(n.Kind)
	if kind == "" {
		kind = "general"
	}
	return fmt.Sprintf("<mewcode_system_note kind=%q>\n%s\n</mewcode_system_note>", kind, strings.TrimSpace(n.Content))
}

type NotePolicy struct {
	FullEvery int
}

func DefaultNotePolicy() NotePolicy {
	return NotePolicy{FullEvery: 3}
}

func BuildSystemNotes(ctx DynamicContext, policy NotePolicy) []SystemNote {
	if policy.FullEvery <= 0 {
		policy = DefaultNotePolicy()
	}
	notes := []SystemNote{environmentNote(ctx)}
	switch ctx.Mode {
	case ModePlan:
		if shouldUseFullNote(ctx.Iteration, policy.FullEvery) {
			notes = append(notes, fullPlanNote())
		} else {
			notes = append(notes, briefPlanNote())
		}
	default:
		notes = append(notes, executeNote())
	}
	if strings.TrimSpace(ctx.PlanContext) != "" {
		notes = append(notes, SystemNote{
			Kind: "plan_context",
			Content: "Execute the approved plan below. Treat it as system-provided execution context, not as a new user message.\n\n" +
				strings.TrimSpace(ctx.PlanContext),
		})
	}
	return notes
}

func shouldUseFullNote(iteration, fullEvery int) bool {
	if iteration <= 1 {
		return true
	}
	return (iteration-1)%fullEvery == 0
}

func environmentNote(ctx DynamicContext) SystemNote {
	tools := strings.TrimSpace(strings.Join(ctx.ToolSummary, ", "))
	if tools == "" {
		tools = "none"
	}
	deferred := strings.TrimSpace(strings.Join(ctx.DeferredToolNames, ", "))
	if deferred == "" {
		deferred = "none"
	}
	workDir := strings.TrimSpace(ctx.WorkDir)
	if workDir == "" {
		workDir = "."
	}
	mode := strings.TrimSpace(ctx.Mode)
	if mode == "" {
		mode = ModeExecute
	}
	content := fmt.Sprintf("Working directory: %s\nMode: %s\nAvailable tools: %s\nSearchable deferred tools: %s", workDir, mode, tools, deferred)
	return SystemNote{Kind: "environment", Content: content}
}

func fullPlanNote() SystemNote {
	return SystemNote{
		Kind: "plan_mode_full",
		Content: strings.Join([]string{
			"You are in read-only planning mode.",
			"Use only read-only tools to inspect files, search code, and understand the project.",
			"Do not modify files, run side-effect commands, or claim that implementation work has been completed.",
			"Produce a clear plan for the user's task based on what you inspected.",
		}, "\n"),
	}
}

func briefPlanNote() SystemNote {
	return SystemNote{
		Kind:    "plan_mode_brief",
		Content: "Still in read-only planning mode: inspect only, do not modify files or run side-effect commands.",
	}
}

func executeNote() SystemNote {
	return SystemNote{
		Kind: "execute_mode",
		Content: strings.Join([]string{
			"You are in execution mode.",
			"Use the available tools to inspect, modify, and verify as needed.",
			"Read relevant files before editing and report verification evidence in the final answer.",
		}, "\n"),
	}
}
