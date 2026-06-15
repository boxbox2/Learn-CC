package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestStdioTransportRoundTripAndEnv(t *testing.T) {
	transport := &StdioTransport{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestStdioHelperProcess"},
		Env: map[string]string{
			"GO_WANT_STDIO_HELPER": "1",
			"MCP_TEST_ENV":         "from-env",
		},
	}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer transport.Close(context.Background())
	if err := transport.Send(context.Background(), JSONRPCMessage{JSONRPC: "2.0", ID: int64(7), Method: "ping"}); err != nil {
		t.Fatal(err)
	}
	msg, err := transport.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if formatID(msg.ID) != "7" {
		t.Fatalf("id = %v, want 7", msg.ID)
	}
	var result map[string]string
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["env"] != "from-env" {
		t.Fatalf("env = %q, want from-env", result["env"])
	}
}

func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_STDIO_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, "diagnostic line")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		var msg JSONRPCMessage
		_ = json.Unmarshal(scanner.Bytes(), &msg)
		data, _ := json.Marshal(map[string]string{"env": os.Getenv("MCP_TEST_ENV")})
		response := JSONRPCMessage{JSONRPC: "2.0", ID: msg.ID, Result: data}
		encoded, _ := json.Marshal(response)
		fmt.Println(string(encoded))
	}
	os.Exit(0)
}
