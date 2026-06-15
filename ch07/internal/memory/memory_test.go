package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mewcode/internal/config"
	"mewcode/internal/provider"
)

type fakeProvider struct {
	reqs []provider.ChatRequest
	text string
	err  error
}

func (f *fakeProvider) StreamChat(ctx context.Context, req provider.ChatRequest) (<-chan provider.StreamEvent, error) {
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return nil, f.err
	}
	ch := make(chan provider.StreamEvent, 2)
	ch <- provider.StreamEvent{Type: provider.StreamEventTypeTextDelta, Delta: f.text}
	ch <- provider.StreamEvent{Type: provider.StreamEventTypeDone}
	close(ch)
	return ch, nil
}

func TestValidateChange(t *testing.T) {
	valid := Change{Action: ActionCreate, Type: NoteProjectKnowledge, Scope: ScopeProject, Filename: "note.md", Title: "Note", Content: "body"}
	if err := ValidateChange(valid); err != nil {
		t.Fatal(err)
	}
	valid.Filename = "../note.md"
	if err := ValidateChange(valid); err == nil {
		t.Fatal("expected invalid filename")
	}
}

func TestApplyWritesNoteAndIndex(t *testing.T) {
	m, err := NewManager(Options{ProjectDir: t.TempDir(), HomeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	err = m.Apply(context.Background(), ChangeSet{Changes: []Change{{
		Action: ActionCreate, Type: NoteProjectKnowledge, Scope: ScopeProject, Filename: "knowledge.md", Title: "Knowledge", Content: "Project fact.",
	}}}, Snapshot{SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.Project.NotesDir, "knowledge.md")); err != nil {
		t.Fatal(err)
	}
	index, err := ReadIndex(m.Project)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(index, "[project_knowledge] Knowledge") {
		t.Fatalf("index = %q", index)
	}
}

func TestExtractorParsesAndRejectsInvalid(t *testing.T) {
	fp := &fakeProvider{text: `{"changes":[{"action":"create","type":"project_knowledge","scope":"project","filename":"a.md","title":"A","content":"B","reason":"C"}]}`}
	extractor := Extractor{Provider: fp, Config: config.ProviderConfig{Model: "fake"}}
	changes, err := extractor.Extract(context.Background(), Snapshot{SessionID: "s"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.Changes) != 1 || len(fp.reqs[0].Tools) != 0 {
		t.Fatalf("changes=%+v req=%+v", changes, fp.reqs[0])
	}
	if _, err := ParseChangeSet(`not json`); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestIndexLimits(t *testing.T) {
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "- [project_knowledge] title - "+strings.Repeat("x", 200))
	}
	clamped := ClampIndex(strings.Join(lines, "\n"))
	if !IndexWithinLimit(clamped) {
		t.Fatalf("clamped index exceeds limit: lines=%d bytes=%d", len(strings.Split(strings.TrimSpace(clamped), "\n")), len([]byte(clamped)))
	}
}
