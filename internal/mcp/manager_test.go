package mcp

import (
	"context"
	"testing"
)

func TestManagerListToolsEmpty(t *testing.T) {
	m := New()
	tools := m.ListTools()
	if tools == nil {
		tools = []ToolSchema{}
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}

func TestManagerDisabledTools(t *testing.T) {
	m := New()
	// Inject a fake connected server
	m.mu.Lock()
	m.servers["test"] = &ServerConn{
		Config: ServerConfig{
			ID:            "test",
			Name:          "Test",
			DisabledTools: []string{"dangerous_tool"},
		},
		Status: "connected",
		Tools: []ToolSchema{
			{Name: "safe_tool", Description: "safe"},
			{Name: "dangerous_tool", Description: "dangerous"},
		},
	}
	m.mu.Unlock()

	tools := m.ListTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (disabled filtered), got %d", len(tools))
	}
	if tools[0].Name != "safe_tool" {
		t.Errorf("expected safe_tool, got %s", tools[0].Name)
	}
}

func TestManagerCallToolNotFound(t *testing.T) {
	m := New()
	_, err := m.CallTool(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent tool")
	}
}

func TestParseArgs(t *testing.T) {
	args, err := parseArgs(`{"key":"value","num":42}`)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args["key"] != "value" {
		t.Errorf("expected value, got %v", args["key"])
	}
	// Empty
	args2, err := parseArgs("")
	if err != nil {
		t.Fatalf("parseArgs empty: %v", err)
	}
	if len(args2) != 0 {
		t.Errorf("expected empty map, got %v", args2)
	}
}
