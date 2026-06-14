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
	rootReal, err := p.realRoot()
	if err != nil {
		return "", err
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	var candidate string
	if filepath.IsAbs(cleaned) {
		candidate = cleaned
	} else {
		candidate = filepath.Join(rootReal, cleaned)
	}
	candidateReal, err := resolveExistingOrParent(candidate)
	if err != nil {
		return "", err
	}
	if !withinRoot(rootReal, candidateReal) {
		return "", fmt.Errorf("path %q escapes project root", path)
	}
	return candidateReal, nil
}

func (p PathPolicy) ResolveVisited(path string) (string, error) {
	rootReal, err := p.realRoot()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	real = filepath.Clean(real)
	if !withinRoot(rootReal, real) {
		return "", fmt.Errorf("path %q escapes project root", path)
	}
	return real, nil
}

func (p PathPolicy) ShouldSkipEscapedDir(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 || !info.IsDir() {
		return false, nil
	}
	if _, err := p.ResolveVisited(path); err != nil {
		return true, nil
	}
	return false, nil
}

func (p PathPolicy) realRoot() (string, error) {
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
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(rootReal), nil
}

func resolveExistingOrParent(candidate string) (string, error) {
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	candidateAbs = filepath.Clean(candidateAbs)
	if real, err := filepath.EvalSymlinks(candidateAbs); err == nil {
		return filepath.Clean(real), nil
	}
	var missing []string
	current := candidateAbs
	for {
		if _, err := os.Lstat(current); err == nil {
			realParent, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				realParent = filepath.Join(realParent, missing[i])
			}
			return filepath.Clean(realParent), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no existing parent for path %q", candidate)
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func withinRoot(rootAbs, candidateAbs string) bool {
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
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
