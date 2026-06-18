package skill

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func Load(opts LoadOptions) (*Catalog, error) {
	c := &Catalog{opts: opts}
	if err := c.Reload(context.Background()); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Catalog) Reload(ctx context.Context) error {
	if c == nil {
		return nil
	}
	snap := Snapshot{Skills: map[string]Definition{}}
	for _, layer := range c.layers() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		defs, warnings := c.scanLayer(layer)
		snap.Warnings = append(snap.Warnings, warnings...)
		for _, def := range defs {
			if warning, ok := c.validateDefinition(def); !ok {
				snap.Warnings = append(snap.Warnings, warning)
				continue
			}
			snap.Skills[def.Metadata.Name] = def
		}
	}
	snap.Ordered = orderedSkillNames(snap.Skills)
	c.mu.Lock()
	c.snapshot = snap
	c.mu.Unlock()
	c.writeWarnings(snap.Warnings)
	return nil
}

func (c *Catalog) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{Skills: map[string]Definition{}}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := Snapshot{
		Skills:   make(map[string]Definition, len(c.snapshot.Skills)),
		Ordered:  append([]string(nil), c.snapshot.Ordered...),
		Warnings: append([]LoadWarning(nil), c.snapshot.Warnings...),
	}
	for name, def := range c.snapshot.Skills {
		out.Skills[name] = cloneDefinition(def)
	}
	return out
}

func (c *Catalog) Get(name string) (Definition, bool) {
	snap := c.Snapshot()
	def, ok := snap.Skills[NormalizeName(name)]
	return cloneDefinition(def), ok
}

func (c *Catalog) Summaries() []CatalogSummary {
	snap := c.Snapshot()
	out := make([]CatalogSummary, 0, len(snap.Ordered))
	for _, name := range snap.Ordered {
		def := snap.Skills[name]
		out = append(out, CatalogSummary{Name: def.Metadata.Name, Description: def.Metadata.Description})
	}
	return out
}

type catalogLayer struct {
	source Source
	root   string
	fsys   fs.FS
}

func (c *Catalog) layers() []catalogLayer {
	return []catalogLayer{
		{source: SourceBuiltin, fsys: c.opts.BuiltinFS},
		{source: SourceUser, root: filepath.Join(c.opts.HomeDir, ".mewcode", "skills")},
		{source: SourceProject, root: filepath.Join(c.opts.ProjectDir, ".mewcode", "skills")},
	}
}

func (c *Catalog) scanLayer(layer catalogLayer) ([]Definition, []LoadWarning) {
	if layer.fsys != nil {
		return scanFSLayer(layer.fsys, layer.source)
	}
	if layer.root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(layer.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []LoadWarning{{Path: layer.root, Message: err.Error()}}
	}
	var defs []Definition
	var warnings []LoadWarning
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(layer.root, entry.Name())
		path := filepath.Join(dir, "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				warnings = append(warnings, LoadWarning{Path: dir, Skill: entry.Name(), Message: "missing SKILL.md"})
				continue
			}
			warnings = append(warnings, LoadWarning{Path: dir, Skill: entry.Name(), Message: err.Error()})
			continue
		}
		def, ws, err := ParseSkill(path, layer.source)
		warnings = append(warnings, ws...)
		if err != nil {
			warnings = append(warnings, LoadWarning{Path: path, Skill: entry.Name(), Message: err.Error()})
			continue
		}
		defs = append(defs, def)
	}
	return defs, warnings
}

func scanFSLayer(fsys fs.FS, source Source) ([]Definition, []LoadWarning) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, []LoadWarning{{Message: err.Error()}}
	}
	var defs []Definition
	var warnings []LoadWarning
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryPath := filepath.ToSlash(filepath.Join(entry.Name(), "SKILL.md"))
		data, err := fs.ReadFile(fsys, entryPath)
		if err != nil {
			warnings = append(warnings, LoadWarning{Path: entryPath, Skill: entry.Name(), Message: "missing SKILL.md"})
			continue
		}
		def, ws, err := parseSkillData(entryPath, string(data), source)
		warnings = append(warnings, ws...)
		if err != nil {
			warnings = append(warnings, LoadWarning{Path: entryPath, Skill: entry.Name(), Message: err.Error()})
			continue
		}
		toolPath := filepath.ToSlash(filepath.Join(entry.Name(), "tool.json"))
		if toolData, err := fs.ReadFile(fsys, toolPath); err == nil {
			tmp, err := parseToolJSONBytes(toolPath, toolData)
			if err != nil {
				warnings = append(warnings, LoadWarning{Path: toolPath, Skill: entry.Name(), Message: err.Error()})
				continue
			}
			def.Tools = tmp
		}
		def.Dir = entry.Name()
		def.Entry = entryPath
		defs = append(defs, def)
	}
	return defs, warnings
}

func parseToolJSONBytes(path string, data []byte) ([]ToolSpec, error) {
	tmp, err := os.CreateTemp("", "mewcode-tool-*.json")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	specs, err := parseToolJSON(tmp.Name())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return specs, nil
}

func (c *Catalog) validateDefinition(def Definition) (LoadWarning, bool) {
	if c.opts.Commands != nil && c.opts.Commands.HasNameOrAlias("/"+def.Metadata.Name) {
		return LoadWarning{Path: def.Entry, Skill: def.Metadata.Name, Message: "command name conflicts with built-in command"}, false
	}
	toolNames := map[string]bool{}
	if c.opts.Tools != nil {
		for _, toolName := range c.opts.Tools.Names() {
			toolNames[toolName] = true
		}
	}
	for _, spec := range def.Tools {
		toolNames[spec.Name] = true
	}
	for _, allowed := range def.Metadata.AllowedTools {
		if !toolNames[allowed] {
			return LoadWarning{Path: def.Entry, Skill: def.Metadata.Name, Message: fmt.Sprintf("allowed_tool %q not registered, skipped", allowed)}, false
		}
	}
	return LoadWarning{}, true
}

func (c *Catalog) writeWarnings(warnings []LoadWarning) {
	if c.opts.Stderr == nil {
		return
	}
	for _, warning := range warnings {
		if strings.TrimSpace(warning.Message) != "" {
			fmt.Fprintln(c.opts.Stderr, warning.String())
		}
	}
}

func orderedSkillNames(skills map[string]Definition) []string {
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cloneDefinition(def Definition) Definition {
	def.Metadata.AllowedTools = append([]string(nil), def.Metadata.AllowedTools...)
	def.Tools = append([]ToolSpec(nil), def.Tools...)
	def.Warnings = append([]LoadWarning(nil), def.Warnings...)
	return def
}

func WriteWarnings(w io.Writer, warnings []LoadWarning) {
	for _, warning := range warnings {
		fmt.Fprintln(w, warning.String())
	}
}
