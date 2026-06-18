package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type frontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	AllowedTools []string `yaml:"allowed_tools"`
	Mode         string   `yaml:"mode"`
	ForkContext  string   `yaml:"fork_context"`
	Model        string   `yaml:"model"`
}

func ParseSkill(path string, source Source) (Definition, []LoadWarning, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, nil, err
	}
	return parseSkillData(path, string(data), source)
}

func parseSkillData(path, data string, source Source) (Definition, []LoadWarning, error) {
	metaText, body, err := splitFrontmatter(data)
	if err != nil {
		return Definition{}, nil, err
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(metaText), &fm); err != nil {
		return Definition{}, nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	meta, warnings, err := normalizeMetadata(fm, path)
	if err != nil {
		return Definition{}, warnings, err
	}
	def := Definition{
		Metadata: meta,
		Body:     strings.TrimSpace(body),
		Dir:      filepath.Dir(path),
		Entry:    path,
		Source:   source,
		Warnings: warnings,
	}
	tools, err := parseToolJSON(filepath.Join(def.Dir, "tool.json"))
	if err != nil {
		return Definition{}, warnings, err
	}
	def.Tools = tools
	return def, warnings, nil
}

func splitFrontmatter(data string) (string, string, error) {
	data = strings.TrimPrefix(data, "\ufeff")
	if !strings.HasPrefix(data, "---\n") && !strings.HasPrefix(data, "---\r\n") {
		return "", "", fmt.Errorf("missing frontmatter")
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(data, "---\r\n"), "---\n")
	idx := strings.Index(trimmed, "\n---")
	if idx < 0 {
		return "", "", fmt.Errorf("unterminated frontmatter")
	}
	meta := trimmed[:idx]
	rest := trimmed[idx+1:]
	if strings.HasPrefix(rest, "---\r\n") {
		rest = strings.TrimPrefix(rest, "---\r\n")
	} else {
		rest = strings.TrimPrefix(rest, "---\n")
	}
	return meta, rest, nil
}

func normalizeMetadata(fm frontmatter, path string) (Metadata, []LoadWarning, error) {
	name := NormalizeName(fm.Name)
	if !ValidName(name) {
		return Metadata{}, nil, fmt.Errorf("invalid skill name %q", fm.Name)
	}
	description := strings.TrimSpace(fm.Description)
	if description == "" {
		return Metadata{}, nil, fmt.Errorf("description is required")
	}
	mode := Mode(strings.TrimSpace(fm.Mode))
	var warnings []LoadWarning
	switch mode {
	case "", ModeInline:
		mode = ModeInline
	case ModeFork:
	default:
		warnings = append(warnings, LoadWarning{Path: path, Skill: name, Message: fmt.Sprintf("unknown mode %q, using inline", fm.Mode)})
		mode = ModeInline
	}
	forkContext := ForkContext(strings.TrimSpace(fm.ForkContext))
	switch forkContext {
	case "", ForkContextNone:
		forkContext = ForkContextNone
	case ForkContextRecent, ForkContextFull:
	default:
		warnings = append(warnings, LoadWarning{Path: path, Skill: name, Message: fmt.Sprintf("unknown fork_context %q, using none", fm.ForkContext)})
		forkContext = ForkContextNone
	}
	allowed := make([]string, 0, len(fm.AllowedTools))
	for _, name := range fm.AllowedTools {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed = append(allowed, name)
		}
	}
	return Metadata{
		Name:         name,
		Description:  description,
		AllowedTools: allowed,
		Mode:         mode,
		ForkContext:  forkContext,
		Model:        strings.TrimSpace(fm.Model),
	}, warnings, nil
}

func parseToolJSON(path string) ([]ToolSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var specs []ToolSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return nil, fmt.Errorf("parse tool.json: %w", err)
	}
	for i, spec := range specs {
		spec.Name = strings.TrimSpace(spec.Name)
		spec.Description = strings.TrimSpace(spec.Description)
		if !ValidName(spec.Name) {
			return nil, fmt.Errorf("tool.json tool %d has invalid name %q", i, spec.Name)
		}
		if spec.Description == "" {
			return nil, fmt.Errorf("tool.json tool %q description is required", spec.Name)
		}
		if len(spec.InputSchema) == 0 {
			return nil, fmt.Errorf("tool.json tool %q input_schema is required", spec.Name)
		}
		if len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
			return nil, fmt.Errorf("tool.json tool %q command is required", spec.Name)
		}
		specs[i] = spec
	}
	return specs, nil
}
