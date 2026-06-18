package tool

import (
	"os"
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

func TestPathPolicyRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	policy := PathPolicy{Root: root}
	if _, err := policy.Resolve("link.txt"); err == nil {
		t.Fatal("expected symlink escape error")
	}
}

func TestPathPolicyResolveNewFileThroughRealParent(t *testing.T) {
	root := t.TempDir()
	policy := PathPolicy{Root: root}
	got, err := policy.Resolve("new/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "new", "file.txt")
	if got != want {
		t.Fatalf("resolved = %q, want %q", got, want)
	}
}
