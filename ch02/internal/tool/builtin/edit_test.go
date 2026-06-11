package builtin

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditUniqueCRLFFallbackAndFailureNoWrite(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	writeFile(t, path, "hello world")
	req := testRequest(root, "call", map[string]any{"path": "a.txt", "old_string": "world", "new_string": "mew"})
	res := EditTool{}.Execute(context.Background(), req)
	if !res.OK {
		t.Fatalf("edit failed: %+v", res)
	}
	if got := readFile(t, path); got != "hello mew" {
		t.Fatalf("content = %q", got)
	}
	writeFile(t, path, "func main() {\r\n\tprintln(\"old\")\r\n}\r\n")
	req = testRequest(root, "call", map[string]any{
		"path":       "a.txt",
		"old_string": "func main() {\n\tprintln(\"old\")\n}",
		"new_string": "func main() {\n\tprintln(\"new\")\n}",
	})
	res = EditTool{}.Execute(context.Background(), req)
	if !res.OK || res.Data["normalized"] != true {
		t.Fatalf("crlf result = %+v", res)
	}
	if !strings.Contains(readFile(t, path), "\r\n\tprintln(\"new\")\r\n") {
		t.Fatalf("did not preserve CRLF: %q", readFile(t, path))
	}
	before := readFile(t, path)
	req = testRequest(root, "call", map[string]any{"path": "a.txt", "old_string": "missing", "new_string": "x"})
	if res := (EditTool{}).Execute(context.Background(), req); res.OK {
		t.Fatalf("expected failure: %+v", res)
	}
	if got := readFile(t, path); got != before {
		t.Fatalf("file changed after failed edit: %q", got)
	}
	writeFile(t, path, "same same")
	req = testRequest(root, "call", map[string]any{"path": "a.txt", "old_string": "same", "new_string": "x"})
	if res := (EditTool{}).Execute(context.Background(), req); res.OK {
		t.Fatalf("expected multiple failure: %+v", res)
	}
}
