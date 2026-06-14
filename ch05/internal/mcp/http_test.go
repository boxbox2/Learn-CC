package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPTransportJSONResponse(t *testing.T) {
	var gotMethod, gotAccept, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAccept = r.Header.Get("Accept")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()
	transport := &HTTPTransport{URL: server.URL, Headers: map[string]string{"Authorization": "Bearer test"}}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(context.Background(), JSONRPCMessage{JSONRPC: "2.0", ID: int64(1), Method: "ping"}); err != nil {
		t.Fatal(err)
	}
	msg, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotAccept != "application/json, text/event-stream" {
		t.Fatalf("accept = %q", gotAccept)
	}
	if gotAuth != "Bearer test" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if formatID(msg.ID) != "1" {
		t.Fatalf("id = %v, want 1", msg.ID)
	}
}

func TestHTTPTransportSSEResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message\n"))
		w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":\n"))
		w.Write([]byte("data: 2,\"result\":{\"ok\":true}}\n\n"))
	}))
	defer server.Close()
	transport := &HTTPTransport{URL: server.URL}
	if err := transport.Send(context.Background(), JSONRPCMessage{JSONRPC: "2.0", ID: int64(2), Method: "ping"}); err != nil {
		t.Fatal(err)
	}
	msg, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]bool
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatal(err)
	}
	if !result["ok"] {
		t.Fatalf("result = %+v", result)
	}
}

func TestHTTPTransportAcceptedNotification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	transport := &HTTPTransport{URL: server.URL}
	if err := transport.Send(context.Background(), JSONRPCMessage{JSONRPC: "2.0", Method: "notifications/initialized"}); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPTransportRejectsAcceptedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	transport := &HTTPTransport{URL: server.URL}
	err := transport.Send(context.Background(), JSONRPCMessage{JSONRPC: "2.0", ID: int64(1), Method: "ping"})
	if err == nil {
		t.Fatal("Send() nil error, want 202 request error")
	}
}
