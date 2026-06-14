package prompt

import (
	"strings"
	"testing"
)

func TestBuildSystemPromptOrder(t *testing.T) {
	got := BuildSystemPrompt(Options{
		CustomInstructions: "custom",
		ActiveSkills:       []string{"skill-a"},
		LongTermMemory:     "memory",
	})
	wantOrder := []string{
		"## Identity",
		"## System Constraints",
		"## Task Modes",
		"## Action Execution",
		"## Tool Use",
		"## Tone",
		"## Text Output",
		"## Environment",
		"## Custom Instructions",
		"## Active Skills",
		"## Long Term Memory",
	}
	last := -1
	for _, marker := range wantOrder {
		idx := strings.Index(got, marker)
		if idx <= last {
			t.Fatalf("%q out of order in:\n%s", marker, got)
		}
		last = idx
	}
}

func TestBuildSystemPromptDeterministicAndStable(t *testing.T) {
	opts := Options{CustomInstructions: "custom", ActiveSkills: []string{"a", "b"}}
	first := BuildSystemPrompt(opts)
	second := BuildSystemPrompt(opts)
	if first != second {
		t.Fatal("system prompt is not deterministic")
	}
	for _, forbidden := range []string{"D:\\", "iteration", "LastPlan"} {
		if strings.Contains(first, forbidden) {
			t.Fatalf("stable prompt contains dynamic value %q", forbidden)
		}
	}
}

func TestBuildSystemNotesPlanFrequency(t *testing.T) {
	for _, tt := range []struct {
		iteration int
		kind      string
	}{
		{1, "plan_mode_full"},
		{2, "plan_mode_brief"},
		{3, "plan_mode_brief"},
		{4, "plan_mode_full"},
	} {
		notes := BuildSystemNotes(DynamicContext{Mode: ModePlan, Iteration: tt.iteration}, DefaultNotePolicy())
		if len(notes) < 2 {
			t.Fatalf("notes = %+v", notes)
		}
		if notes[1].Kind != tt.kind {
			t.Fatalf("iteration %d note kind = %q, want %q", tt.iteration, notes[1].Kind, tt.kind)
		}
	}
}

func TestSystemNoteTaggedContent(t *testing.T) {
	note := SystemNote{Kind: "environment", Content: "hello"}
	tagged := note.TaggedContent()
	if !strings.Contains(tagged, `<mewcode_system_note kind="environment">`) || !strings.Contains(tagged, "hello") {
		t.Fatalf("tagged content = %q", tagged)
	}
}

func TestBuildSystemNotesIncludesEnvironmentAndPlanContext(t *testing.T) {
	notes := BuildSystemNotes(DynamicContext{
		WorkDir:     `D:\repo`,
		Mode:        ModeExecute,
		Iteration:   1,
		ToolSummary: []string{"Read", "Edit"},
		PlanContext: "approved plan",
	}, DefaultNotePolicy())
	if notes[0].Kind != "environment" || !strings.Contains(notes[0].Content, `D:\repo`) || !strings.Contains(notes[0].Content, "Read, Edit") {
		t.Fatalf("environment note = %+v", notes[0])
	}
	if notes[len(notes)-1].Kind != "plan_context" || !strings.Contains(notes[len(notes)-1].Content, "approved plan") {
		t.Fatalf("plan context note missing: %+v", notes)
	}
}
