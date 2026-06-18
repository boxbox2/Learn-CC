package skill

import (
	"context"
	"strings"

	"mewcode/internal/tool"
)

type LoadSkillTool struct {
	Catalog *Catalog
	Active  *ActiveStore
}

func (t LoadSkillTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "LoadSkill",
		Description: "Activate a skill by name, pinning its SOP into the active context and enabling its session-local specialized tools.",
		Parameters: tool.Schema{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill name to activate."},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
		Safety: tool.SafetyReadOnly,
	}
}

func (t LoadSkillTool) IsSystemTool() bool { return true }

func (t LoadSkillTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	var args struct {
		Name string `json:"name"`
	}
	if err := decodeArgs(req.Arguments, &args); err != nil {
		return tool.Failure("LoadSkill", req.ID, "invalid_arguments", err.Error())
	}
	name := NormalizeName(args.Name)
	if name == "" {
		return tool.Failure("LoadSkill", req.ID, "invalid_arguments", "name is required")
	}
	if t.Catalog == nil {
		return tool.Failure("LoadSkill", req.ID, "skills_unavailable", "skill catalog is not configured")
	}
	def, ok := t.Catalog.Get(name)
	if !ok {
		return tool.Failure("LoadSkill", req.ID, "unknown_skill", "unknown skill: "+name)
	}
	body := def.Body
	if latest, _, err := ParseSkill(def.Entry, def.Source); err == nil && strings.TrimSpace(latest.Body) != "" {
		body = latest.Body
		def.Tools = latest.Tools
	}
	if t.Active == nil {
		t.Active = NewActiveStore()
	}
	tools := makeExecTools(def)
	if err := t.Active.Activate(def.Metadata.Name, body, tools); err != nil {
		return tool.Failure("LoadSkill", req.ID, "skill_tool_conflict", err.Error())
	}
	return tool.Success("LoadSkill", req.ID, "Skill "+name+" activated. SOP pinned to env context.", map[string]any{
		"name":              name,
		"specialized_tools": len(tools),
	})
}
