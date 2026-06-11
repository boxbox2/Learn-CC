package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"mewcode/internal/tool"
)

type BashTool struct{}

type bashArgs struct {
	Command   string `json:"command"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

func (BashTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "Bash",
		Description: "Run a shell command in the current project directory.",
		Parameters: objectSchema(map[string]any{
			"command":    stringProp("Command to execute."),
			"timeout_ms": intProp("Optional timeout in milliseconds."),
		}, "command"),
	}
}

func (BashTool) Execute(ctx context.Context, req tool.Request) tool.Result {
	var args bashArgs
	if err := json.Unmarshal(req.Arguments, &args); err != nil {
		return tool.Failure("Bash", req.ID, "invalid_arguments", err.Error())
	}
	if args.Command == "" {
		return tool.Failure("Bash", req.ID, "invalid_arguments", "command is required")
	}
	limits := req.Limits
	if limits.CommandTimeout <= 0 {
		limits = tool.DefaultLimits()
	}
	timeout := limits.CommandTimeout
	if args.TimeoutMS > 0 {
		timeout = time.Duration(args.TimeoutMS) * time.Millisecond
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := shellCommand(cmdCtx, args.Command)
	cmd.Dir = req.WorkingDir
	configureCommandCleanup(cmd)
	stdout := &limitedBuffer{limit: positiveOrDefault(limits.MaxStdoutBytes, tool.DefaultLimits().MaxStdoutBytes)}
	stderr := &limitedBuffer{limit: positiveOrDefault(limits.MaxStderrBytes, tool.DefaultLimits().MaxStderrBytes)}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	timedOut := cmdCtx.Err() == context.DeadlineExceeded
	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	result := tool.Success("Bash", req.ID, fmt.Sprintf("Command exited with code %d", exitCode), map[string]any{
		"command":   args.Command,
		"exit_code": exitCode,
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"timeout":   timedOut,
	})
	if timedOut {
		result.OK = false
		result.Error = &tool.ToolError{Code: "timeout", Message: "command timed out"}
		result.Summary = "Command timed out"
	}
	result.Truncated = stdout.truncated || stderr.truncated
	result.OriginalBytes = stdout.written + stderr.written
	result.ReturnedBytes = stdout.buf.Len() + stderr.buf.Len()
	return result
}

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func positiveOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	written   int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.written += len(p)
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	text := b.buf.String()
	if b.truncated {
		text += fmt.Sprintf("\n[... truncated after %d bytes ...]", b.limit)
	}
	return text
}
