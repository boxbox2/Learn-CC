package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
)

type RPCSession struct {
	transport Transport

	mu      sync.Mutex
	nextID  int64
	pending map[string]chan JSONRPCMessage
	closed  bool
}

func NewSession(transport Transport) *RPCSession {
	return &RPCSession{
		transport: transport,
		pending:   map[string]chan JSONRPCMessage{},
	}
}

func (s *RPCSession) Call(ctx context.Context, method string, params any, result any) error {
	id, key, ch, err := s.registerPending()
	if err != nil {
		return err
	}
	msg, err := requestMessage(id, method, params)
	if err != nil {
		s.removePending(key)
		return err
	}
	if err := s.transport.Send(ctx, msg); err != nil {
		s.removePending(key)
		return err
	}
	select {
	case response, ok := <-ch:
		s.removePending(key)
		if !ok {
			return fmt.Errorf("json-rpc session is closed")
		}
		if response.Error != nil {
			return response.Error
		}
		if result == nil {
			return nil
		}
		if len(response.Result) == 0 {
			return fmt.Errorf("json-rpc response %s has no result", key)
		}
		return json.Unmarshal(response.Result, result)
	case <-ctx.Done():
		s.removePending(key)
		return ctx.Err()
	}
}

func (s *RPCSession) Notify(ctx context.Context, method string, params any) error {
	msg, err := requestMessage(nil, method, params)
	if err != nil {
		return err
	}
	return s.transport.Send(ctx, msg)
}

func (s *RPCSession) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			s.Close(context.Background())
			return ctx.Err()
		}
		msg, err := s.transport.Receive(ctx)
		if err != nil {
			s.Close(context.Background())
			return err
		}
		if msg.ID == nil {
			continue
		}
		key := formatID(msg.ID)
		ch := s.lookupPending(key)
		if ch == nil {
			continue
		}
		select {
		case ch <- msg:
		case <-ctx.Done():
			s.Close(context.Background())
			return ctx.Err()
		}
	}
}

func (s *RPCSession) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		for key, ch := range s.pending {
			close(ch)
			delete(s.pending, key)
		}
	}
	s.mu.Unlock()
	if s.transport != nil {
		return s.transport.Close(ctx)
	}
	return nil
}

func (s *RPCSession) registerPending() (int64, string, chan JSONRPCMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, "", nil, fmt.Errorf("json-rpc session is closed")
	}
	s.nextID++
	id := s.nextID
	key := strconv.FormatInt(id, 10)
	ch := make(chan JSONRPCMessage, 1)
	s.pending[key] = ch
	return id, key, ch, nil
}

func (s *RPCSession) lookupPending(key string) chan JSONRPCMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	return s.pending[key]
}

func (s *RPCSession) removePending(key string) {
	s.mu.Lock()
	delete(s.pending, key)
	s.mu.Unlock()
}

func requestMessage(id any, method string, params any) (JSONRPCMessage, error) {
	msg := JSONRPCMessage{JSONRPC: "2.0", ID: id, Method: method}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return JSONRPCMessage{}, err
		}
		msg.Params = data
	}
	return msg, nil
}

func formatID(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		if math.Trunc(v) == v {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}
