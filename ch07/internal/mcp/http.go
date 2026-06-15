package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

type HTTPTransport struct {
	URL     string
	Headers map[string]string
	Client  *http.Client

	receive chan JSONRPCMessage
	once    sync.Once
}

func (t *HTTPTransport) Start(ctx context.Context) error {
	if strings.TrimSpace(t.URL) == "" {
		return fmt.Errorf("http mcp url is required")
	}
	if t.Client == nil {
		t.Client = http.DefaultClient
	}
	t.once.Do(func() {
		t.receive = make(chan JSONRPCMessage, 32)
	})
	return nil
}

func (t *HTTPTransport) Send(ctx context.Context, msg JSONRPCMessage) error {
	if t.Client == nil || t.receive == nil {
		if err := t.Start(ctx); err != nil {
			return err
		}
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range t.Headers {
		req.Header.Set(key, value)
	}
	resp, err := t.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted {
		if msg.ID != nil {
			return fmt.Errorf("http mcp request %v returned 202 without a response", msg.ID)
		}
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("http mcp status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch contentType {
	case "application/json", "":
		var response JSONRPCMessage
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return err
		}
		return t.enqueue(ctx, response)
	case "text/event-stream":
		return t.readSSE(ctx, resp.Body)
	default:
		return fmt.Errorf("unsupported http mcp content type %q", contentType)
	}
}

func (t *HTTPTransport) Receive(ctx context.Context) (JSONRPCMessage, error) {
	if t.receive == nil {
		if err := t.Start(ctx); err != nil {
			return JSONRPCMessage{}, err
		}
	}
	select {
	case msg, ok := <-t.receive:
		if !ok {
			return JSONRPCMessage{}, io.EOF
		}
		return msg, nil
	case <-ctx.Done():
		return JSONRPCMessage{}, ctx.Err()
	}
}

func (t *HTTPTransport) Close(ctx context.Context) error {
	if t.receive != nil {
		close(t.receive)
		t.receive = nil
	}
	return nil
}

func (t *HTTPTransport) enqueue(ctx context.Context, msg JSONRPCMessage) error {
	select {
	case t.receive <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *HTTPTransport) readSSE(ctx context.Context, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	var event string
	var data []string
	flush := func() error {
		if len(data) == 0 {
			event = ""
			return nil
		}
		if event == "" || event == "message" {
			raw := strings.Join(data, "\n")
			if strings.TrimSpace(raw) != "" {
				var msg JSONRPCMessage
				if err := json.Unmarshal([]byte(raw), &msg); err != nil {
					return err
				}
				if err := t.enqueue(ctx, msg); err != nil {
					return err
				}
			}
		}
		event = ""
		data = nil
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}
