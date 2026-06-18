package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mewcode/internal/command"
	skillbuiltin "mewcode/internal/skill/builtin"
	"mewcode/internal/tool"
)

type testTool struct {
	name string
}

func (t testTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        t.name,
		Description: "test tool",
		Parameters:  tool.Schema{"type": "object"},
		Safety:      tool.SafetyReadOnly,
	}
}

func (t testTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	return tool.Success(t.name, req.ID, "ok", nil)
}

func TestParseSkillDefaultsAndToolJSON(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "sample", `---
name: sample
description: Sample skill
mode: strange
allowed_tools: [parse-file]
---

Run $ARGUMENTS.
`)
	if err := os.Mkdir(filepath.Join(dir, "sample", "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample", "tool.json"), []byte(`[{
		"name": "parse-file",
		"description": "Parse a file",
		"input_schema": {"type": "object"},
		"command": ["parse-file.sh"]
	}]`), 0o644); err != nil {
		t.Fatal(err)
	}

	def, warnings, err := ParseSkill(filepath.Join(dir, "sample", "SKILL.md"), SourceProject)
	if err != nil {
		t.Fatal(err)
	}
	if def.Metadata.Name != "sample" || def.Metadata.Mode != ModeInline {
		t.Fatalf("metadata = %+v", def.Metadata)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "unknown mode") {
		t.Fatalf("warnings = %+v", warnings)
	}
	if len(def.Tools) != 1 || def.Tools[0].Name != "parse-file" {
		t.Fatalf("tools = %+v", def.Tools)
	}
}

func TestCatalogPriorityAndSkipsMissingAllowedTool(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	userRoot := filepath.Join(home, ".mewcode", "skills")
	projectRoot := filepath.Join(project, ".mewcode", "skills")
	writeSkill(t, userRoot, "commit", `---
name: commit
description: User commit
---
user
`)
	writeSkill(t, projectRoot, "commit", `---
name: commit
description: Project commit
---
project
`)
	writeSkill(t, projectRoot, "bad", `---
name: bad
description: Bad skill
allowed_tools: [NotExist]
---
bad
`)
	reg := tool.NewRegistry()
	if err := reg.Register(testTool{name: "Read"}); err != nil {
		t.Fatal(err)
	}
	commands := command.NewRegistry()
	catalog, err := Load(LoadOptions{
		ProjectDir: project,
		HomeDir:    home,
		BuiltinFS:  skillbuiltin.FS,
		Tools:      reg,
		Commands:   commands,
	})
	if err != nil {
		t.Fatal(err)
	}
	def, ok := catalog.Get("commit")
	if !ok {
		t.Fatal("commit missing")
	}
	if def.Metadata.Description != "Project commit" {
		t.Fatalf("description = %q", def.Metadata.Description)
	}
	if _, ok := catalog.Get("bad"); ok {
		t.Fatal("bad skill should be skipped")
	}
	if !containsWarning(catalog.Snapshot().Warnings, `allowed_tool "NotExist" not registered`) {
		t.Fatalf("warnings = %+v", catalog.Snapshot().Warnings)
	}
}

func TestActiveStoreConflictAndRefresh(t *testing.T) {
	store := NewActiveStore()
	if err := store.Activate("one", "body1", []tool.Tool{testTool{name: "parse-file"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Activate("one", "body2", []tool.Tool{testTool{name: "parse-file"}}); err != nil {
		t.Fatal(err)
	}
	if got := store.PromptText(); !strings.Contains(got, "body2") || strings.Contains(got, "body1") {
		t.Fatalf("prompt = %q", got)
	}
	if err := store.Activate("two", "body", []tool.Tool{testTool{name: "parse-file"}}); err == nil {
		t.Fatal("expected conflict")
	}
}

func TestLoadSkillToolActivatesPromptAndOverlay(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, ".mewcode", "skills")
	writeSkill(t, root, "sample", `---
name: sample
description: Sample skill
allowed_tools: [parse-file]
---
Use parse-file.
`)
	if err := os.WriteFile(filepath.Join(root, "sample", "tool.json"), []byte(`[{
		"name": "parse-file",
		"description": "Parse a file",
		"input_schema": {"type": "object"},
		"command": ["parse-file.sh"]
	}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	catalog, err := Load(LoadOptions{ProjectDir: project, HomeDir: t.TempDir(), Tools: reg, Commands: command.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	active := NewActiveStore()
	result := LoadSkillTool{Catalog: catalog, Active: active}.Execute(context.Background(), tool.Request{
		ID:        "1",
		Arguments: []byte(`{"name":"sample"}`),
	})
	if !result.OK {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(active.PromptText(), "Use parse-file.") {
		t.Fatalf("active prompt = %q", active.PromptText())
	}
	if _, ok := active.Get("parse-file"); !ok {
		t.Fatal("specialized tool missing from active overlay")
	}
	if _, ok := reg.Get("parse-file"); ok {
		t.Fatal("specialized tool leaked into global registry")
	}
}

func TestBuiltinCatalogLoadsCommitReviewTest(t *testing.T) {
	reg := tool.NewRegistry()
	for _, name := range []string{"Bash", "Read", "Grep"} {
		if err := reg.Register(testTool{name: name}); err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := Load(LoadOptions{ProjectDir: t.TempDir(), HomeDir: t.TempDir(), BuiltinFS: skillbuiltin.FS, Commands: command.NewRegistry(), Tools: reg})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"commit", "review", "test"} {
		if _, ok := catalog.Get(name); !ok {
			t.Fatalf("missing builtin skill %s", name)
		}
	}
}

func writeSkill(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsWarning(warnings []LoadWarning, text string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning.Message, text) {
			return true
		}
	}
	return false
}
