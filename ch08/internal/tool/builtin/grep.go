package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"mewcode/internal/tool"
)

type GrepTool struct{}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Regex   bool   `json:"regex,omitempty"`
}

type grepMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

func (GrepTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "Grep",
		Description: "Search text content in project files. Use to find symbols, text, and call sites before editing.",
		Safety:      tool.SafetyReadOnly,
		Parameters: objectSchema(map[string]any{
			"pattern": stringProp("Text or regex pattern to search for."),
			"path":    stringProp("Optional file or directory to search within."),
			"regex":   boolProp("Treat pattern as a regular expression."),
		}, "pattern"),
	}
}

func (GrepTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	var args grepArgs
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		return tool.Failure("Grep", req.ID, "invalid_arguments", err.Error())
	}
	if args.Pattern == "" {
		return tool.Failure("Grep", req.ID, "invalid_arguments", "pattern is required")
	}
	rootArg := args.Path
	if rootArg == "" {
		rootArg = "."
	}
	root, err := req.PathPolicy.Resolve(rootArg)
	if err != nil {
		return tool.Failure("Grep", req.ID, "invalid_path", err.Error())
	}
	var re *regexp.Regexp
	if args.Regex {
		re, err = regexp.Compile(args.Pattern)
		if err != nil {
			return tool.Failure("Grep", req.ID, "invalid_regex", err.Error())
		}
	}
	limit := positiveOrDefault(req.Limits.MaxMatches, tool.DefaultLimits().MaxMatches)
	var matches []grepMatch
	searchFile := func(abs string) {
		if len(matches) >= limit {
			return
		}
		if _, err := req.PathPolicy.ResolveVisited(abs); err != nil {
			return
		}
		fileMatches := grepFile(abs, req.PathPolicy, args.Pattern, re, limit-len(matches))
		matches = append(matches, fileMatches...)
	}
	info, err := os.Stat(root)
	if err != nil {
		return tool.Failure("Grep", req.ID, "grep_failed", err.Error())
	}
	if !info.IsDir() {
		searchFile(root)
	} else {
		_ = filepath.WalkDir(root, func(abs string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				if _, err := req.PathPolicy.ResolveVisited(abs); err != nil {
					return filepath.SkipDir
				}
				return nil
			}
			searchFile(abs)
			if len(matches) >= limit {
				return filepath.SkipAll
			}
			return nil
		})
	}
	result := tool.Success("Grep", req.ID, fmt.Sprintf("Found %d matches", len(matches)), map[string]any{
		"matches": matches,
	})
	if len(matches) >= limit {
		result.Truncated = true
	}
	return result
}

func grepFile(abs string, policy tool.PathPolicy, pattern string, re *regexp.Regexp, limit int) []grepMatch {
	f, err := os.Open(abs)
	if err != nil {
		return nil
	}
	defer f.Close()
	if looksBinary(f) {
		return nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil
	}
	var matches []grepMatch
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		ok := false
		if re != nil {
			ok = re.MatchString(line)
		} else {
			ok = strings.Contains(line, pattern)
		}
		if ok {
			matches = append(matches, grepMatch{
				Path:    policy.DisplayPath(abs),
				Line:    lineNo,
				Snippet: trimSnippet(line),
			})
			if len(matches) >= limit {
				break
			}
		}
	}
	return matches
}

func looksBinary(r io.Reader) bool {
	buf := make([]byte, 8000)
	n, _ := r.Read(buf)
	return strings.Contains(string(buf[:n]), "\x00")
}

func trimSnippet(line string) string {
	line = strings.TrimSpace(line)
	limited := tool.LimitText(line, 240)
	return limited.Text
}
