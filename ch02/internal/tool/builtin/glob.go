package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"mewcode/internal/tool"
)

type GlobTool struct{}

type globArgs struct {
	Pattern string `json:"pattern"`
}

func (GlobTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "Glob",
		Description: "Find project files matching a glob-like pattern.",
		Parameters: objectSchema(map[string]any{
			"pattern": stringProp("File name or glob pattern."),
		}, "pattern"),
	}
}

func (GlobTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	var args globArgs
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		return tool.Failure("Glob", req.ID, "invalid_arguments", err.Error())
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return tool.Failure("Glob", req.ID, "invalid_arguments", "pattern is required")
	}
	root, err := req.PathPolicy.Resolve(".")
	if err != nil {
		return tool.Failure("Glob", req.ID, "invalid_path", err.Error())
	}
	limit := positiveOrDefault(req.Limits.MaxMatches, tool.DefaultLimits().MaxMatches)
	var matches []string
	err = filepath.WalkDir(root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if globMatch(args.Pattern, rel) {
			matches = append(matches, rel)
			if len(matches) >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return tool.Failure("Glob", req.ID, "glob_failed", err.Error())
	}
	sort.Strings(matches)
	result := tool.Success("Glob", req.ID, fmt.Sprintf("Found %d files", len(matches)), map[string]any{
		"matches": matches,
	})
	if len(matches) >= limit {
		result.Truncated = true
		result.ReturnedBytes = len(matches)
	}
	return result
}

func globMatch(pattern, rel string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	base := path.Base(rel)
	if ok, _ := path.Match(pattern, rel); ok {
		return true
	}
	if ok, _ := path.Match(pattern, base); ok {
		return true
	}
	if strings.Contains(pattern, "**") {
		suffix := strings.TrimPrefix(pattern, "**/")
		if ok, _ := path.Match(suffix, rel); ok {
			return true
		}
		if ok, _ := path.Match(suffix, base); ok {
			return true
		}
	}
	return strings.Contains(rel, pattern)
}
