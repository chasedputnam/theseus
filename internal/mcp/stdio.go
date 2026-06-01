package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
)

type stdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	nextID atomic.Int64
}

func newStdioClient(ctx context.Context, cfg ServerConfig) (mcpClient, error) {
	if cfg.Command == "" {
		return nil, fmt.Errorf("stdio transport requires command")
	}
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	// Set env
	cmd.Env = os.Environ()
	for k, v := range cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start MCP server: %w", err)
	}
	c := &stdioClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
	}
	// Initialize MCP session
	if err := c.initialize(ctx); err != nil {
		cmd.Process.Kill()
		return nil, fmt.Errorf("MCP initialize: %w", err)
	}
	return c, nil
}

func (c *stdioClient) initialize(ctx context.Context) error {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "theseus", "version": "1.0.0"},
		},
	}
	if _, err := c.sendRequest(ctx, req); err != nil {
		return err
	}
	// Send initialized notification
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	}
	data, _ := json.Marshal(notif)
	data = append(data, '\n')
	c.mu.Lock()
	c.stdin.Write(data)
	c.mu.Unlock()
	return nil
}

func (c *stdioClient) ListTools(ctx context.Context) ([]ToolSchema, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected tools/list response")
	}
	toolsRaw, _ := result["tools"].([]any)
	var tools []ToolSchema
	for _, t := range toolsRaw {
		tMap, ok := t.(map[string]any)
		if !ok {
			continue
		}
		tool := ToolSchema{
			Name:        fmt.Sprint(tMap["name"]),
			Description: fmt.Sprint(tMap["description"]),
		}
		if schema, ok := tMap["inputSchema"].(map[string]any); ok {
			tool.InputSchema = schema
		}
		tools = append(tools, tool)
	}
	return tools, nil
}

func (c *stdioClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	}
	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return "", err
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected tools/call response")
	}
	// Extract text content
	if content, ok := result["content"].([]any); ok {
		var sb bytes.Buffer
		for _, item := range content {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					sb.WriteString(fmt.Sprint(m["text"]))
				}
			}
		}
		return sb.String(), nil
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func (c *stdioClient) sendRequest(ctx context.Context, req map[string]any) (map[string]any, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	c.mu.Lock()
	_, err = c.stdin.Write(data)
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("write to MCP stdin: %w", err)
	}

	// Read response line
	done := make(chan struct {
		line string
		err  error
	}, 1)
	go func() {
		line, err := c.stdout.ReadString('\n')
		done <- struct {
			line string
			err  error
		}{line, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-done:
		if result.err != nil {
			return nil, result.err
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(result.line), &resp); err != nil {
			return nil, fmt.Errorf("parse MCP response: %w", err)
		}
		if errObj, ok := resp["error"]; ok {
			return nil, fmt.Errorf("MCP error: %v", errObj)
		}
		return resp, nil
	}
}

func (c *stdioClient) Close() error {
	c.stdin.Close()
	return c.cmd.Process.Kill()
}
