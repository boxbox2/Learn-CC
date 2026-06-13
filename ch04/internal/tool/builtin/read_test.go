package builtin

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSuccessMissingDirectoryEscapeAndTruncate(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "hello")
	req := testRequest(root, "call", map[string]any{"path": "a.txt"})
	res := ReadTool{}.Execute(context.Background(), req)
	if !res.OK {
		t.Fatalf("read failed: %+v", res)
	}
	if res.Data["content"] != "hello" {
		t.Fatalf("content = %v", res.Data["content"])
	}
	missing := testRequest(root, "call", map[string]any{"path": "missing.txt"})
	if res := (ReadTool{}).Execute(context.Background(), missing); res.OK || res.Error.Code != "read_failed" {
		t.Fatalf("missing result = %+v", res)
	}
	dir := testRequest(root, "call", map[string]any{"path": "."})
	if res := (ReadTool{}).Execute(context.Background(), dir); res.OK || res.Error.Code != "path_is_directory" {
		t.Fatalf("directory result = %+v", res)
	}
	escape := testRequest(root, "call", map[string]any{"path": "../outside.txt"})
	if res := (ReadTool{}).Execute(context.Background(), escape); res.OK || res.Error.Code != "invalid_path" {
		t.Fatalf("escape result = %+v", res)
	}
	writeFile(t, filepath.Join(root, "big.txt"), strings.Repeat("x", 200))
	trunc := testRequest(root, "call", map[string]any{"path": "big.txt"})
	trunc.Limits.MaxFileBytes = 80
	res = ReadTool{}.Execute(context.Background(), trunc)
	if !res.Truncated {
		t.Fatalf("expected truncated: %+v", res)
	}
}
