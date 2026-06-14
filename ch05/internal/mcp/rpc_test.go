package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type memoryTransport struct {
	sent     chan JSONRPCMessage
	incoming chan JSONRPCMessage
	closed   chan struct{}
	once     sync.Once
}

func newMemoryTransport() *memoryTransport {
	return &memoryTransport{
		sent:     make(chan JSONRPCMessage, 16),
		incoming: make(chan JSONRPCMessage, 16),
		closed:   make(chan struct{}),
	}
}

func (t *memoryTransport) Start(ctx context.Context) error { return nil }

func (t *memoryTransport) Send(ctx context.Context, msg JSONRPCMessage) error {
	select {
	case t.sent <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *memoryTransport) Receive(ctx context.Context) (JSONRPCMessage, error) {
	select {
	case msg := <-t.incoming:
		return msg, nil
	case <-t.closed:
		return JSONRPCMessage{}, context.Canceled
	case <-ctx.Done():
		return JSONRPCMessage{}, ctx.Err()
	}
}

func (t *memoryTransport) Close(ctx context.Context) error {
	t.once.Do(func() { close(t.closed) })
	return nil
}

func TestRPCSessionMatchesOutOfOrderResponses(t *testing.T) {
	transport := newMemoryTransport()
	session := NewSession(transport)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go session.Run(ctx)

	var wg sync.WaitGroup
	results := make([]string, 2)
	for i, method := range []string{"first", "second"} {
		wg.Add(1)
		go func(i int, method string) {
			defer wg.Done()
			var result struct {
				Value string `json:"value"`
			}
			if err := session.Call(ctx, method, nil, &result); err != nil {
				t.Errorf("Call(%s) error = %v", method, err)
				return
			}
			results[i] = result.Value
		}(i, method)
	}

	req1 := <-transport.sent
	req2 := <-transport.sent
	transport.incoming <- rpcResult(req2.ID, map[string]string{"value": req2.Method})
	transport.incoming <- JSONRPCMessage{JSONRPC: "2.0", ID: float64(999), Result: json.RawMessage(`{"ignored":true}`)}
	transport.incoming <- rpcResult(req1.ID, map[string]string{"value": req1.Method})
	wg.Wait()

	if results[0] != "first" || results[1] != "second" {
		t.Fatalf("results = %+v, want first/second", results)
	}
}

func TestRPCSessionCallContextCancel(t *testing.T) {
	transport := newMemoryTransport()
	session := NewSession(transport)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go session.Run(ctx)

	callCtx, cancelCall := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancelCall()
	var result map[string]any
	if err := session.Call(callCtx, "slow", nil, &result); err == nil {
		t.Fatal("Call() nil error, want context timeout")
	}
}

func rpcResult(id any, value any) JSONRPCMessage {
	data, _ := json.Marshal(value)
	return JSONRPCMessage{JSONRPC: "2.0", ID: id, Result: data}
}
