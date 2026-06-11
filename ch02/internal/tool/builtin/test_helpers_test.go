package builtin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mewcode/internal/tool"
)

func testRequest(root, id string, args map[string]any) tool.Request {
	data, _ := json.Marshal(args)
	return tool.Request{
		ID:         id,
		Name:       "test",
		Arguments:  data,
		WorkingDir: root,
		PathPolicy: tool.PathPolicy{Root: root},
		Limits:     tool.DefaultLimits(),
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func toolRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := RegisterDefaults(reg); err != nil {
		t.Fatal(err)
	}
	return reg
}
