package tool

import (
	"path/filepath"
	"testing"
)

func TestPathPolicyResolveWithinRoot(t *testing.T) {
	root := t.TempDir()
	policy := PathPolicy{Root: root}
	got, err := policy.Resolve("a/../b.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "b.txt")
	if got != want {
		t.Fatalf("resolved = %q, want %q", got, want)
	}
}

func TestPathPolicyRejectsEscape(t *testing.T) {
	root := t.TempDir()
	policy := PathPolicy{Root: root}
	if _, err := policy.Resolve("../outside.txt"); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestPathPolicyDisplayPath(t *testing.T) {
	root := t.TempDir()
	policy := PathPolicy{Root: root}
	abs := filepath.Join(root, "dir", "file.go")
	if got := policy.DisplayPath(abs); got != "dir/file.go" {
		t.Fatalf("display path = %q", got)
	}
}
