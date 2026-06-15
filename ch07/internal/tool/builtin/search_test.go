package builtin

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGlobAndGrep(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package main\nfunc main() {}\n")
	writeFile(t, filepath.Join(root, "dir", "b.txt"), "needle\nnope\n")
	globReq := testRequest(root, "call", map[string]any{"pattern": "*.go"})
	glob := GlobTool{}.Execute(context.Background(), globReq)
	if !glob.OK {
		t.Fatalf("glob failed: %+v", glob)
	}
	matches := glob.Data["matches"].([]string)
	if len(matches) != 1 || matches[0] != "a.go" {
		t.Fatalf("glob matches = %+v", matches)
	}
	grepReq := testRequest(root, "call", map[string]any{"pattern": "needle"})
	grep := GrepTool{}.Execute(context.Background(), grepReq)
	if !grep.OK {
		t.Fatalf("grep failed: %+v", grep)
	}
	if len(grep.Data["matches"].([]grepMatch)) != 1 {
		t.Fatalf("grep matches = %+v", grep.Data["matches"])
	}
	none := GrepTool{}.Execute(context.Background(), testRequest(root, "call", map[string]any{"pattern": "absent"}))
	if !none.OK || len(none.Data["matches"].([]grepMatch)) != 0 {
		t.Fatalf("empty grep = %+v", none)
	}
	escape := GrepTool{}.Execute(context.Background(), testRequest(root, "call", map[string]any{"pattern": "x", "path": "../outside"}))
	if escape.OK || escape.Error.Code != "invalid_path" {
		t.Fatalf("escape grep = %+v", escape)
	}
}

func TestRegisterDefaults(t *testing.T) {
	reg := toolRegistry(t)
	for _, name := range []string{"Read", "Write", "Edit", "Bash", "Glob", "Grep"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("missing tool %s", name)
		}
	}
}
