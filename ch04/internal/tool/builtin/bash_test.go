package builtin

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"mewcode/internal/tool"
)

func TestBashSuccessFailureTimeoutAndTruncate(t *testing.T) {
	root := t.TempDir()
	res := BashTool{}.Execute(context.Background(), testRequest(root, "call", map[string]any{"command": echoCommand("hello")}))
	if !res.OK || !strings.Contains(res.Data["stdout"].(string), "hello") {
		t.Fatalf("success result = %+v", res)
	}
	res = BashTool{}.Execute(context.Background(), testRequest(root, "call", map[string]any{"command": "exit 7"}))
	if !res.OK || intFromAny(res.Data["exit_code"]) != 7 {
		t.Fatalf("failure exit result = %+v", res)
	}
	req := testRequest(root, "call", map[string]any{"command": sleepCommand(), "timeout_ms": 50})
	req.Limits.CommandTimeout = 50 * time.Millisecond
	res = BashTool{}.Execute(context.Background(), req)
	if res.OK || res.Error == nil || res.Error.Code != "timeout" {
		t.Fatalf("timeout result = %+v", res)
	}
	req = testRequest(root, "call", map[string]any{"command": bigOutputCommand()})
	req.Limits = tool.DefaultLimits()
	req.Limits.MaxStdoutBytes = 20
	res = BashTool{}.Execute(context.Background(), req)
	if !res.Truncated {
		t.Fatalf("expected truncated: %+v", res)
	}
}

func echoCommand(text string) string {
	if runtime.GOOS == "windows" {
		return "Write-Output " + text
	}
	return "printf " + text
}

func sleepCommand() string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep -Milliseconds 500"
	}
	return "sleep 1"
}

func bigOutputCommand() string {
	if runtime.GOOS == "windows" {
		return "Write-Output ('x' * 200)"
	}
	return "printf '%*s' 200 | tr ' ' x"
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return -1
	}
}
