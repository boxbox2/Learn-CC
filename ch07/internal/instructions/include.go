package instructions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func expandIncludes(content string, source Source, maxDepth int) (string, []string, []Diagnostic, error) {
	visited := map[string]bool{}
	base := filepath.Dir(source.Path)
	return expandText(content, base, source.AllowedRoot, maxDepth, 0, visited)
}

func expandText(content, baseDir, allowedRoot string, maxDepth, depth int, visited map[string]bool) (string, []string, []Diagnostic, error) {
	var out []string
	var files []string
	var diagnostics []Diagnostic
	for _, line := range strings.Split(content, "\n") {
		target, ok := parseInclude(line)
		if !ok {
			out = append(out, line)
			continue
		}
		if depth >= maxDepth {
			return "", files, diagnostics, fmt.Errorf("include depth exceeds %d at %s", maxDepth, target)
		}
		path, diag, err := resolveInclude(baseDir, allowedRoot, target)
		if diag != nil {
			diagnostics = append(diagnostics, *diag)
			continue
		}
		if err != nil {
			return "", files, diagnostics, err
		}
		if visited[path] {
			diagnostics = append(diagnostics, Diagnostic{Path: path, Message: "include cycle skipped"})
			continue
		}
		visited[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				diagnostics = append(diagnostics, Diagnostic{Path: path, Message: "include file missing"})
				continue
			}
			return "", files, diagnostics, err
		}
		expanded, childFiles, childDiagnostics, err := expandText(string(data), filepath.Dir(path), allowedRoot, maxDepth, depth+1, visited)
		if err != nil {
			return "", files, diagnostics, err
		}
		files = append(files, path)
		files = append(files, childFiles...)
		diagnostics = append(diagnostics, childDiagnostics...)
		out = append(out, expanded)
	}
	return strings.Join(out, "\n"), files, diagnostics, nil
}

func parseInclude(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "@include ") {
		return "", false
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, "@include "))
	return target, target != ""
}

func resolveInclude(baseDir, allowedRoot, target string) (string, *Diagnostic, error) {
	if filepath.IsAbs(target) || strings.Contains(filepath.ToSlash(target), "../") || target == ".." || strings.HasPrefix(filepath.ToSlash(target), "..") {
		return "", &Diagnostic{Path: target, Message: "include path escapes allowed root"}, nil
	}
	candidate := filepath.Clean(filepath.Join(baseDir, target))
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", nil, err
	}
	absRoot, err := filepath.Abs(filepath.Clean(allowedRoot))
	if err != nil {
		return "", nil, err
	}
	realCandidate, err := filepath.EvalSymlinks(absCandidate)
	if err != nil {
		if os.IsNotExist(err) {
			realCandidate = absCandidate
		} else {
			return "", nil, err
		}
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		if os.IsNotExist(err) {
			realRoot = absRoot
		} else {
			return "", nil, err
		}
	}
	if !withinRoot(realCandidate, realRoot) {
		return "", &Diagnostic{Path: target, Message: "include path escapes allowed root"}, nil
	}
	return realCandidate, nil, nil
}

func withinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
