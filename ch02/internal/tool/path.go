package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PathPolicy struct {
	Root string
}

func (p PathPolicy) Resolve(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	root := p.Root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = cwd
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootAbs = filepath.Clean(rootAbs)
	cleaned := filepath.Clean(filepath.FromSlash(path))
	var candidate string
	if filepath.IsAbs(cleaned) {
		candidate = cleaned
	} else {
		candidate = filepath.Join(rootAbs, cleaned)
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	candidateAbs = filepath.Clean(candidateAbs)
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q escapes project root", path)
	}
	return candidateAbs, nil
}

func (p PathPolicy) DisplayPath(abs string) string {
	root := p.Root
	if root == "" {
		return abs
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return filepath.ToSlash(rel)
}
