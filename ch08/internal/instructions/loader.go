package instructions

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const DefaultMaxDepth = 5

type SourceKind string

const (
	SourceProjectRoot SourceKind = "project_root"
	SourceProjectMew  SourceKind = "project_mewcode"
	SourceUser        SourceKind = "user"
)

type Source struct {
	Kind        SourceKind
	Path        string
	AllowedRoot string
	Priority    int
}

type Diagnostic struct {
	Path    string
	Message string
}

type LoadOptions struct {
	ProjectDir string
	HomeDir    string
	MaxDepth   int
	Sources    []Source
}

type LoadResult struct {
	Text        string
	Files       []string
	Diagnostics []Diagnostic
}

func Load(opts LoadOptions) (LoadResult, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = DefaultMaxDepth
	}
	sources := opts.Sources
	if len(sources) == 0 {
		sources = defaultSources(opts.ProjectDir, opts.HomeDir)
	}
	sort.SliceStable(sources, func(i, j int) bool {
		return sources[i].Priority < sources[j].Priority
	})
	var result LoadResult
	var parts []string
	for _, source := range sources {
		data, err := os.ReadFile(source.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return result, err
		}
		expanded, files, diagnostics, err := expandIncludes(string(data), source, opts.MaxDepth)
		result.Files = append(result.Files, source.Path)
		result.Files = append(result.Files, files...)
		result.Diagnostics = append(result.Diagnostics, diagnostics...)
		if err != nil {
			return result, err
		}
		content := strings.TrimSpace(expanded)
		if content != "" {
			parts = append(parts, "### "+string(source.Kind)+"\n"+content)
		}
	}
	result.Text = strings.Join(parts, "\n\n")
	return result, nil
}

func defaultSources(projectDir, homeDir string) []Source {
	projectDir = filepath.Clean(projectDir)
	userRoot := filepath.Join(homeDir, ".mewcode")
	return []Source{
		{
			Kind:        SourceProjectRoot,
			Path:        filepath.Join(projectDir, "AGENTS.md"),
			AllowedRoot: projectDir,
			Priority:    10,
		},
		{
			Kind:        SourceProjectMew,
			Path:        filepath.Join(projectDir, ".mewcode", "instructions.md"),
			AllowedRoot: projectDir,
			Priority:    20,
		},
		{
			Kind:        SourceUser,
			Path:        filepath.Join(userRoot, "instructions.md"),
			AllowedRoot: userRoot,
			Priority:    30,
		},
	}
}
