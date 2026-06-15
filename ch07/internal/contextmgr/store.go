package contextmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ToolResultStore struct {
	Root string
}

func NewToolResultStore(root string) *ToolResultStore {
	return &ToolResultStore{Root: root}
}

func (s *ToolResultStore) PathFor(toolUseID string) string {
	if s == nil {
		return ""
	}
	return filepath.Join(s.Root, toolUseID)
}

func (s *ToolResultStore) Write(toolUseID string, content string) (path string, written bool, err error) {
	if s == nil {
		return "", false, fmt.Errorf("tool result store is nil")
	}
	if !safeToolUseID(toolUseID) {
		return "", false, fmt.Errorf("tool_use_id %q is not a safe file name", toolUseID)
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return "", false, err
	}
	path = s.PathFor(toolUseID)
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", false, err
	}
	return path, true, nil
}

func safeToolUseID(id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	return id == filepath.Base(id) && !strings.ContainsAny(id, `/\`)
}
