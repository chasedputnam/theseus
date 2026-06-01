package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type sseClient struct {
	url        string
	httpClient *http.Client
	nextID     atomic.Int64
}

func newSSEClient(ctx context.Context, cfg ServerConfig) (mcpClient, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("SSE transport requires url")
	}
	c := &sseClient{
		url:        cfg.URL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	// Verify connectivity
	if _, err := c.ListTools(ctx); err != nil {
		return nil, fmt.Errorf("SSE MCP connect: %w", err)
	}
	return c, nil
}

func (c *sseClient) ListTools(ctx context.Context) ([]ToolSchema, error) {
	resp, err := c.rpc(ctx, "tools/list", map[string]any{})
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

func (c *sseClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	resp, err := c.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected tools/call response")
	}
	if content, ok := result["content"].([]any); ok {
		var sb bytes.Buffer
		for _, item := range content {
			if m, ok := item.(map[string]any); ok && m["type"] == "text" {
				sb.WriteString(fmt.Sprint(m["text"]))
			}
		}
		return sb.String(), nil
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func (c *sseClient) rpc(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := c.nextID.Add(1)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read SSE response
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var result map[string]any
		if err := json.Unmarshal([]byte(payload), &result); err != nil {
			continue
		}
		if errObj, ok := result["error"]; ok {
			return nil, fmt.Errorf("MCP SSE error: %v", errObj)
		}
		return result, nil
	}
	return nil, fmt.Errorf("no response from SSE MCP server")
}

func (c *sseClient) Close() error { return nil }
