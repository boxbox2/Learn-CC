package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"mewcode/internal/permission"
	"mewcode/internal/provider"
	"mewcode/internal/tool"
)

type ToolSetMode string

const (
	ToolSetAll      ToolSetMode = "all"
	ToolSetReadOnly ToolSetMode = "read_only"
)

type ToolSet struct {
	Mode    ToolSetMode
	Names   []string
	Overlay tool.Overlay
}

type ToolBatch struct {
	Parallel bool
	Calls    []provider.ToolCall
}

type ToolExecutor struct {
	Registry   *tool.Registry
	Overlay    tool.Overlay
	Names      []string
	WorkingDir string
	PathPolicy tool.PathPolicy
	Limits     tool.Limits
	Authorizer permission.Authorizer
}

func AllowedDefinitions(registry *tool.Registry, set ToolSet) []tool.Definition {
	names := nameSet(set.Names)
	var defs []tool.Definition
	switch set.Mode {
	case ToolSetReadOnly:
		if registry != nil {
			defs = append(defs, registry.VisibleDefinitionsBySafety(tool.SafetyReadOnly)...)
		}
		if set.Overlay != nil {
			defs = append(defs, set.Overlay.VisibleDefinitionsBySafety(tool.SafetyReadOnly)...)
		}
	default:
		if registry != nil {
			defs = append(defs, registry.VisibleDefinitions()...)
		}
		if set.Overlay != nil {
			defs = append(defs, set.Overlay.VisibleDefinitions()...)
		}
	}
	if len(names) == 0 {
		return defs
	}
	filtered := make([]tool.Definition, 0, len(defs))
	for _, def := range defs {
		if names[def.Name] || isSystemDefinition(def.Name, registry) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

func DeferredToolNames(registry *tool.Registry, set ToolSet) []string {
	names := nameSet(set.Names)
	var out []string
	switch set.Mode {
	case ToolSetReadOnly:
		if registry != nil {
			out = append(out, registry.DeferredNamesBySafety(tool.SafetyReadOnly)...)
		}
		if set.Overlay != nil {
			out = append(out, set.Overlay.DeferredNamesBySafety(tool.SafetyReadOnly)...)
		}
	default:
		if registry != nil {
			out = append(out, registry.DeferredNames()...)
		}
		if set.Overlay != nil {
			out = append(out, set.Overlay.DeferredNames()...)
		}
	}
	if len(names) == 0 {
		return out
	}
	filtered := make([]string, 0, len(out))
	for _, name := range out {
		if names[name] || isSystemDefinition(name, registry) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func PlanToolBatches(calls []provider.ToolCall, registry *tool.Registry) []ToolBatch {
	return planToolBatches(calls, registry, nil)
}

func planToolBatches(calls []provider.ToolCall, registry *tool.Registry, overlay tool.Overlay) []ToolBatch {
	var batches []ToolBatch
	var readBatch []provider.ToolCall
	flushReadBatch := func() {
		if len(readBatch) == 0 {
			return
		}
		batches = append(batches, ToolBatch{Parallel: true, Calls: append([]provider.ToolCall(nil), readBatch...)})
		readBatch = nil
	}
	for _, call := range calls {
		if isReadOnlyTool(call.Name, registry, overlay) {
			readBatch = append(readBatch, call)
			continue
		}
		flushReadBatch()
		batches = append(batches, ToolBatch{Parallel: false, Calls: []provider.ToolCall{call}})
	}
	flushReadBatch()
	return batches
}

func (e ToolExecutor) ExecuteToolBatches(ctx context.Context, calls []provider.ToolCall, out chan<- provider.StreamEvent) []tool.Result {
	batches := planToolBatches(calls, e.Registry, e.Overlay)
	byID := make(map[string]tool.Result, len(calls))
	for _, batch := range batches {
		if ctx.Err() != nil {
			break
		}
		var results []tool.Result
		if batch.Parallel {
			results = e.executeParallel(ctx, batch.Calls)
		} else {
			results = e.executeSerial(ctx, batch.Calls)
		}
		for _, result := range results {
			byID[result.CallID] = result
		}
	}
	ordered := make([]tool.Result, 0, len(calls))
	for _, call := range calls {
		result, ok := byID[call.ID]
		if !ok {
			result = tool.Failure(call.Name, call.ID, "cancelled", "tool execution was cancelled")
		}
		ordered = append(ordered, result)
		if out != nil {
			copy := result
			out <- provider.StreamEvent{Type: provider.StreamEventTypeToolResult, ToolResult: &copy}
		}
	}
	return ordered
}

func (e ToolExecutor) executeParallel(ctx context.Context, calls []provider.ToolCall) []tool.Result {
	results := make([]tool.Result, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		if ctx.Err() != nil {
			results[i] = tool.Failure(call.Name, call.ID, "cancelled", "tool execution was cancelled")
			continue
		}
		wg.Add(1)
		go func(i int, call provider.ToolCall) {
			defer wg.Done()
			results[i] = e.ExecuteToolCall(ctx, call)
		}(i, call)
	}
	wg.Wait()
	return results
}

func (e ToolExecutor) executeSerial(ctx context.Context, calls []provider.ToolCall) []tool.Result {
	results := make([]tool.Result, 0, len(calls))
	for _, call := range calls {
		if ctx.Err() != nil {
			results = append(results, tool.Failure(call.Name, call.ID, "cancelled", "tool execution was cancelled"))
			continue
		}
		results = append(results, e.ExecuteToolCall(ctx, call))
	}
	return results
}

func (e ToolExecutor) ExecuteToolCall(ctx context.Context, call provider.ToolCall) tool.Result {
	exec, ok := e.lookup(call.Name)
	if !ok {
		if e.Registry == nil && e.Overlay == nil {
			return tool.Failure(call.Name, call.ID, "tools_unavailable", "tools are not configured")
		}
		return tool.Failure(call.Name, call.ID, "unknown_tool", fmt.Sprintf("tool %q is not registered", call.Name))
	}
	def := exec.Definition()
	if !e.allowedByWhitelist(call.Name, def) {
		return tool.Failure(call.Name, call.ID, "unknown_tool", fmt.Sprintf("tool %q is not available in the current tool set", call.Name))
	}
	if e.Registry == nil && e.Overlay == nil {
		return tool.Failure(call.Name, call.ID, "tools_unavailable", "tools are not configured")
	}
	if e.Authorizer != nil {
		decision := e.Authorizer.Authorize(ctx, permission.Request{
			CallID:     call.ID,
			Tool:       call.Name,
			Arguments:  []byte(call.Arguments),
			Fields:     permissionFields(call),
			Safety:     def.EffectiveSafety(),
			WorkingDir: e.WorkingDir,
			PathPolicy: e.PathPolicy,
		})
		if decision.Status == permission.DecisionDeny {
			code := "permission_denied"
			if decision.IsHardBlock() {
				code = "permission_hard_denied"
			}
			result := tool.Failure(call.Name, call.ID, code, decision.Reason)
			if len(decision.Suggestions) > 0 {
				result.Data = map[string]any{"suggestions": decision.Suggestions}
			}
			return result
		}
	}
	req := tool.Request{
		ID:         call.ID,
		Name:       call.Name,
		Arguments:  []byte(call.Arguments),
		WorkingDir: e.WorkingDir,
		PathPolicy: e.PathPolicy,
		Limits:     e.Limits,
	}
	result := exec.Execute(ctx, req)
	if result.Tool == "" {
		result.Tool = call.Name
	}
	if result.CallID == "" {
		result.CallID = call.ID
	}
	return result
}

func (e ToolExecutor) lookup(name string) (tool.Tool, bool) {
	if e.Overlay != nil {
		if exec, ok := e.Overlay.Get(name); ok {
			return exec, true
		}
	}
	if e.Registry != nil {
		return e.Registry.Get(name)
	}
	return nil, false
}

func (e ToolExecutor) allowedByWhitelist(name string, def tool.Definition) bool {
	names := nameSet(e.Names)
	if len(names) == 0 {
		return true
	}
	return names[name] || isSystemDefinition(name, e.Registry)
}

func permissionFields(call provider.ToolCall) map[string]string {
	var args map[string]any
	fields := map[string]string{}
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return fields
	}
	addString := func(name string) {
		if value, ok := args[name].(string); ok && value != "" {
			fields[name] = value
		}
	}
	switch call.Name {
	case "Bash":
		addString("command")
	case "Read", "Write", "Edit":
		addString("path")
	case "Glob":
		addString("pattern")
	case "Grep":
		addString("path")
		addString("pattern")
	}
	return fields
}

func isReadOnlyTool(name string, registry *tool.Registry, overlay tool.Overlay) bool {
	exec, ok := lookupTool(name, registry, overlay)
	if !ok {
		return false
	}
	return exec.Definition().EffectiveSafety() == tool.SafetyReadOnly
}

func lookupTool(name string, registry *tool.Registry, overlay tool.Overlay) (tool.Tool, bool) {
	if overlay != nil {
		if exec, ok := overlay.Get(name); ok {
			return exec, true
		}
	}
	if registry != nil {
		return registry.Get(name)
	}
	return nil, false
}

func nameSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		if name != "" {
			set[name] = true
		}
	}
	return set
}

func isSystemDefinition(name string, registry *tool.Registry) bool {
	return registry != nil && registry.IsSystem(name)
}

func ToolResultContent(result tool.Result, limits tool.Limits) string {
	content := result.JSON()
	maxBytes := limits.MaxResultBytes
	if maxBytes <= 0 {
		maxBytes = tool.DefaultLimits().MaxResultBytes
	}
	limited := tool.LimitText(content, maxBytes)
	if !limited.Truncated {
		return content
	}
	truncated := tool.Result{
		Tool:          result.Tool,
		CallID:        result.CallID,
		OK:            result.OK,
		Summary:       result.Summary,
		Error:         result.Error,
		Truncated:     true,
		OriginalBytes: limited.OriginalBytes,
		ReturnedBytes: limited.ReturnedBytes,
		Data: map[string]any{
			"truncated_result_json": limited.Text,
		},
	}
	return truncated.JSON()
}

func allResultsUnknown(results []tool.Result) bool {
	if len(results) == 0 {
		return false
	}
	for _, result := range results {
		if result.Error == nil || result.Error.Code != "unknown_tool" {
			return false
		}
	}
	return true
}
