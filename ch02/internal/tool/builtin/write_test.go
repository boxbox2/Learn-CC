package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreateOverwriteAndEscape(t *testing.T) {
	root := t.TempDir()
	req := testRequest(root, "call", map[string]any{"path": "dir/a.txt", "content": "one"})
	res := WriteTool{}.Execute(context.Background(), req)
	if !res.OK {
		t.Fatalf("write failed: %+v", res)
	}
	req = testRequest(root, "call", map[string]any{"path": "dir/a.txt", "content": "two"})
	res = WriteTool{}.Execute(context.Background(), req)
	if !res.OK {
		t.Fatalf("overwrite failed: %+v", res)
	}
	data, err := os.ReadFile(filepath.Join(root, "dir", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "two" {
		t.Fatalf("content = %q", data)
	}
	escape := testRequest(root, "call", map[string]any{"path": "../outside.txt", "content": "no"})
	if res := (WriteTool{}).Execute(context.Background(), escape); res.OK || res.Error.Code != "invalid_path" {
		t.Fatalf("escape result = %+v", res)
	}
}
