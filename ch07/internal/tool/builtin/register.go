package builtin

import "mewcode/internal/tool"

func RegisterDefaults(reg *tool.Registry) error {
	for _, t := range []tool.Tool{
		ReadTool{},
		WriteTool{},
		EditTool{},
		BashTool{},
		GlobTool{},
		GrepTool{},
	} {
		if err := reg.Register(t); err != nil {
			return err
		}
	}
	if err := reg.Register(ToolSearch{Registry: reg}); err != nil {
		return err
	}
	return nil
}

func objectSchema(properties map[string]any, required ...string) tool.Schema {
	return tool.Schema{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func intProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func boolProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}
