package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestNotificationsAndPing(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = original })

	handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: float64(1), Method: "initialize"})
	handleRequest(JSONRPCRequest{JSONRPC: "2.0", Method: "notifications/initialized"})
	handleRequest(JSONRPCRequest{JSONRPC: "2.0", Method: "notifications/cancelled"})
	handleRequest(JSONRPCRequest{JSONRPC: "2.0", ID: float64(2), Method: "ping"})
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two replies, got %d: %s", len(lines), data)
	}
	var response JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[1]), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != float64(2) || response.Error != nil {
		t.Fatalf("ping failed: %s", lines[1])
	}
}
