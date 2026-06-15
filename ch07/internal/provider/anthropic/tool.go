package anthropic

import (
	"encoding/json"

	"mewcode/internal/provider"
	"mewcode/internal/tool"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
)

func toTools(defs []tool.Definition) []anthropicsdk.ToolUnionParam {
	out := make([]anthropicsdk.ToolUnionParam, 0, len(defs))
	for _, def := range defs {
		schema := toInputSchema(def.Parameters)
		param := anthropicsdk.ToolParam{
			Name:        def.Name,
			Description: anthropicsdk.String(def.Description),
			InputSchema: schema,
		}
		out = append(out, anthropicsdk.ToolUnionParam{OfTool: &param})
	}
	return out
}

func toInputSchema(schema tool.Schema) anthropicsdk.ToolInputSchemaParam {
	out := anthropicsdk.ToolInputSchemaParam{ExtraFields: map[string]any{}}
	if props, ok := schema["properties"]; ok {
		out.Properties = props
	}
	if required, ok := schema["required"].([]string); ok {
		out.Required = required
	} else if raw, ok := schema["required"].([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				out.Required = append(out.Required, s)
			}
		}
	}
	for key, value := range schema {
		if key != "type" && key != "properties" && key != "required" {
			out.ExtraFields[key] = value
		}
	}
	return out
}

func toolUseBlocks(calls []provider.ToolCall) []anthropicsdk.ContentBlockParamUnion {
	blocks := make([]anthropicsdk.ContentBlockParamUnion, 0, len(calls))
	for _, call := range calls {
		var input any
		if err := json.Unmarshal([]byte(call.Arguments), &input); err != nil {
			input = map[string]any{}
		}
		blocks = append(blocks, anthropicsdk.NewToolUseBlock(call.ID, input, call.Name))
	}
	return blocks
}

func toolResultBlocks(results []provider.ToolResultMessage) []anthropicsdk.ContentBlockParamUnion {
	blocks := make([]anthropicsdk.ContentBlockParamUnion, 0, len(results))
	for _, result := range results {
		blocks = append(blocks, anthropicsdk.NewToolResultBlock(result.ID, result.Content, isToolResultError(result.Content)))
	}
	return blocks
}

func toolCallsFromMessage(message anthropicsdk.Message) []provider.ToolCall {
	var calls []provider.ToolCall
	for _, block := range message.Content {
		if toolUse, ok := block.AsAny().(anthropicsdk.ToolUseBlock); ok {
			calls = append(calls, provider.ToolCall{
				ID:        toolUse.ID,
				Name:      toolUse.Name,
				Arguments: string(toolUse.Input),
			})
		}
	}
	return calls
}

func isToolResultError(content string) bool {
	var data struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return false
	}
	return !data.OK
}
