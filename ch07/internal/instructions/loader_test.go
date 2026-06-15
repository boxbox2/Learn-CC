package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrdersSources(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	mustWrite(t, filepath.Join(project, "AGENTS.md"), "project root")
	mustWrite(t, filepath.Join(project, ".mewcode", "instructions.md"), "project mew")
	mustWrite(t, filepath.Join(home, ".mewcode", "instructions.md"), "user")
	result, err := Load(LoadOptions{ProjectDir: project, HomeDir: home})
	if err != nil {
		t.Fatal(err)
	}
	root := strings.Index(result.Text, "project root")
	mew := strings.Index(result.Text, "project mew")
	user := strings.Index(result.Text, "user")
	if !(root >= 0 && root < mew && mew < user) {
		t.Fatalf("unexpected order:\n%s", result.Text)
	}
}

func TestLoadMissingSources(t *testing.T) {
	result, err := Load(LoadOptions{ProjectDir: t.TempDir(), HomeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "" {
		t.Fatalf("text = %q, want empty", result.Text)
	}
}

func TestIncludeExpandsAndSkipsCycles(t *testing.T) {
	project := t.TempDir()
	mustWrite(t, filepath.Join(project, "AGENTS.md"), "before\n@include child.md\nafter")
	mustWrite(t, filepath.Join(project, "child.md"), "child\n@include AGENTS.md")
	result, err := Load(LoadOptions{ProjectDir: project, HomeDir: t.TempDir(), MaxDepth: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "child") || len(result.Diagnostics) == 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestIncludeRejectsEscape(t *testing.T) {
	project := t.TempDir()
	parent := filepath.Dir(project)
	mustWrite(t, filepath.Join(parent, "secret.md"), "secret")
	mustWrite(t, filepath.Join(project, "AGENTS.md"), "@include ../secret.md")
	result, err := Load(LoadOptions{ProjectDir: project, HomeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Text, "secret") || len(result.Diagnostics) == 0 {
		t.Fatalf("escape not rejected: %+v", result)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
