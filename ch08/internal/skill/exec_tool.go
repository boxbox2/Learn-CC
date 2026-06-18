package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"mewcode/internal/tool"
)

type ExecTool struct {
	Spec    ToolSpec
	BaseDir string
	Timeout time.Duration
}

func (e ExecTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        e.Spec.Name,
		Description: e.Spec.Description,
		Parameters:  e.Spec.InputSchema,
		Safety:      tool.SafetySideEffect,
	}
}

func (e ExecTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cmdPath, args, err := e.command()
	if err != nil {
		return tool.Failure(e.Spec.Name, req.ID, "invalid_command", err.Error())
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cmdPath, args...)
	cmd.Dir = e.BaseDir
	cmd.Stdin = bytes.NewReader(req.Arguments)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return tool.Failure(e.Spec.Name, req.ID, "command_failed", msg)
	}
	return tool.Success(e.Spec.Name, req.ID, strings.TrimSpace(stdout.String()), map[string]any{"stdout": stdout.String()})
}

func (e ExecTool) command() (string, []string, error) {
	if len(e.Spec.Command) == 0 {
		return "", nil, fmt.Errorf("command is required")
	}
	first := strings.TrimSpace(e.Spec.Command[0])
	if first == "" {
		return "", nil, fmt.Errorf("command is required")
	}
	if filepath.IsAbs(first) {
		return first, e.Spec.Command[1:], nil
	}
	base, err := filepath.Abs(e.BaseDir)
	if err != nil {
		return "", nil, err
	}
	target := filepath.Join(base, "references", first)
	resolved, err := filepath.Abs(target)
	if err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(filepath.Join(base, "references"), resolved)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", nil, fmt.Errorf("relative command must stay under references")
	}
	return resolved, e.Spec.Command[1:], nil
}

func makeExecTools(def Definition) []tool.Tool {
	tools := make([]tool.Tool, 0, len(def.Tools))
	for _, spec := range def.Tools {
		tools = append(tools, ExecTool{Spec: spec, BaseDir: def.Dir, Timeout: 30 * time.Second})
	}
	return tools
}

func decodeArgs(raw []byte, target any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	return json.Unmarshal(raw, target)
}
