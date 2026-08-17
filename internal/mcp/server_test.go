package mcp

import "testing"

func TestNewServer(t *testing.T) {
	s := NewServer()
	if s == nil {
		t.Fatal("expected MCP server")
	}
}
