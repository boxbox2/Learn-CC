package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type StdioTransport struct {
	Command string
	Args    []string
	Env     map[string]string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	sendMu sync.Mutex
}

func (t *StdioTransport) Start(ctx context.Context) error {
	if strings.TrimSpace(t.Command) == "" {
		return fmt.Errorf("stdio command is required")
	}
	cmd := exec.CommandContext(ctx, t.Command, t.Args...)
	cmd.Env = mergeEnv(os.Environ(), t.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go io.Copy(io.Discard, stderr)
	t.cmd = cmd
	t.stdin = stdin
	t.stdout = bufio.NewReader(stdout)
	return nil
}

func (t *StdioTransport) Send(ctx context.Context, msg JSONRPCMessage) error {
	if t.stdin == nil {
		return fmt.Errorf("stdio transport is not started")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (t *StdioTransport) Receive(ctx context.Context) (JSONRPCMessage, error) {
	if t.stdout == nil {
		return JSONRPCMessage{}, fmt.Errorf("stdio transport is not started")
	}
	line, err := t.stdout.ReadBytes('\n')
	if err != nil {
		if ctx.Err() != nil {
			return JSONRPCMessage{}, ctx.Err()
		}
		return JSONRPCMessage{}, err
	}
	var msg JSONRPCMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return JSONRPCMessage{}, err
	}
	return msg, nil
}

func (t *StdioTransport) Close(ctx context.Context) error {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- t.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = t.cmd.Process.Kill()
		return ctx.Err()
	case <-time.After(2 * time.Second):
		_ = t.cmd.Process.Kill()
		return nil
	}
}

func mergeEnv(base []string, override map[string]string) []string {
	if len(override) == 0 {
		return base
	}
	values := map[string]string{}
	order := make([]string, 0, len(base)+len(override))
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = item
	}
	for key, value := range override {
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = key + "=" + value
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, values[key])
	}
	return out
}
