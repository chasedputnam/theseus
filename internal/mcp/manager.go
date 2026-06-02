package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
)

// ToolSchema mirrors the MCP tool definition.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ServerConfig holds connection parameters for one MCP server.
type ServerConfig struct {
	ID            string
	Name          string
	Transport     string // "stdio" or "sse"
	Command       string
	Args          []string
	Env           map[string]string
	URL           string
	DisabledTools []string
	OAuthURL      string
	OAuthToken    string
}

// ServerConn represents a connected MCP server.
type ServerConn struct {
	Config   ServerConfig
	Tools    []ToolSchema
	Status   string // "connected", "error", "disconnected"
	Error    string
	client   mcpClient
}

// mcpClient is the interface for an MCP protocol client.
type mcpClient interface {
	ListTools(ctx context.Context) ([]ToolSchema, error)
	CallTool(ctx context.Context, name string, args map[string]any) (string, error)
	Close() error
}

// Manager manages MCP server connections.
type Manager struct {
	servers map[string]*ServerConn
	mu      sync.RWMutex
}

// New creates an MCP Manager.
func New() *Manager {
	return &Manager{servers: make(map[string]*ServerConn)}
}

// Connect connects to an MCP server.
func (m *Manager) Connect(ctx context.Context, cfg ServerConfig) error {
	var client mcpClient
	var err error

	switch cfg.Transport {
	case "stdio":
		client, err = newStdioClient(ctx, cfg)
	case "sse":
		client, err = newSSEClient(ctx, cfg)
	default:
		return fmt.Errorf("unknown transport: %s", cfg.Transport)
	}

	conn := &ServerConn{Config: cfg}
	if err != nil {
		conn.Status = "error"
		conn.Error = err.Error()
		m.mu.Lock()
		m.servers[cfg.ID] = conn
		m.mu.Unlock()
		return err
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		conn.Status = "error"
		conn.Error = err.Error()
		client.Close()
	} else {
		conn.Status = "connected"
		conn.Tools = tools
		conn.client = client
	}

	m.mu.Lock()
	m.servers[cfg.ID] = conn
	m.mu.Unlock()
	log.Printf("mcp: connected %s (%s), %d tools", cfg.Name, cfg.Transport, len(tools))
	return nil
}

// Disconnect closes a server connection.
func (m *Manager) Disconnect(serverID string) {
	m.mu.Lock()
	conn, ok := m.servers[serverID]
	if ok {
		if conn.client != nil {
			conn.client.Close()
		}
		conn.Status = "disconnected"
	}
	m.mu.Unlock()
}

// ListTools returns all tools from all connected servers, excluding disabled ones.
func (m *Manager) ListTools() []ToolSchema {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tools []ToolSchema
	for _, conn := range m.servers {
		if conn.Status != "connected" {
			continue
		}
		disabled := make(map[string]bool, len(conn.Config.DisabledTools))
		for _, t := range conn.Config.DisabledTools {
			disabled[t] = true
		}
		for _, t := range conn.Tools {
			if !disabled[t.Name] {
				tools = append(tools, t)
			}
		}
	}
	return tools
}

// CallTool calls a tool on the appropriate server.
func (m *Manager) CallTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
	m.mu.RLock()
	var targetConn *ServerConn
	for _, conn := range m.servers {
		if conn.Status != "connected" {
			continue
		}
		for _, t := range conn.Tools {
			if t.Name == toolName {
				targetConn = conn
				break
			}
		}
		if targetConn != nil {
			break
		}
	}
	m.mu.RUnlock()

	if targetConn == nil {
		return "", fmt.Errorf("tool %q not found on any connected MCP server", toolName)
	}
	return targetConn.client.CallTool(ctx, toolName, args)
}

// Status returns connection status for all servers.
func (m *Manager) Status() []map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var statuses []map[string]any
	for _, conn := range m.servers {
		enabledCount := 0
		disabled := make(map[string]bool, len(conn.Config.DisabledTools))
		for _, t := range conn.Config.DisabledTools {
			disabled[t] = true
		}
		for _, t := range conn.Tools {
			if !disabled[t.Name] {
				enabledCount++
			}
		}
		statuses = append(statuses, map[string]any{
			"id":                conn.Config.ID,
			"name":              conn.Config.Name,
			"transport":         conn.Config.Transport,
			"status":            conn.Status,
			"error":             conn.Error,
			"tool_count":        len(conn.Tools),
			"enabled_tool_count": enabledCount,
			"is_enabled":        conn.Status != "disconnected",
			"needs_oauth":       conn.Config.OAuthURL != "" && conn.Config.OAuthToken == "",
		})
	}
	return statuses
}

// Reconnect disconnects and reconnects a server by ID.
func (m *Manager) Reconnect(ctx context.Context, serverID string) error {
	m.mu.RLock()
	conn, ok := m.servers[serverID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server %q not found", serverID)
	}
	cfg := conn.Config
	m.Disconnect(serverID)
	return m.Connect(ctx, cfg)
}

// SetEnabled marks a server as enabled or disabled without disconnecting.
func (m *Manager) SetEnabled(serverID string, enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	conn, ok := m.servers[serverID]
	if !ok {
		return
	}
	if enabled && conn.Status == "disconnected" {
		conn.Status = "connected"
	} else if !enabled {
		conn.Status = "disconnected"
	}
}

// GetServerTools returns tools for a specific server by ID, with disabled flag.
func (m *Manager) GetServerTools(serverID string) ([]ToolSchema, []string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.servers[serverID]
	if !ok {
		return nil, nil, false
	}
	return conn.Tools, conn.Config.DisabledTools, true
}

// ServerIDs returns all server IDs.
func (m *Manager) ServerIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.servers))
	for id := range m.servers {
		ids = append(ids, id)
	}
	return ids
}

// SetToken stores an OAuth token for a server.
func (m *Manager) SetToken(id, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if conn, ok := m.servers[id]; ok {
		conn.Config.OAuthToken = token
	}
}

// GetOAuthURL returns the OAuth URL for a server, or empty string if not found.
func (m *Manager) GetOAuthURL(id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if conn, ok := m.servers[id]; ok {
		return conn.Config.OAuthURL
	}
	return ""
}

// parseArgs parses a JSON string into a map.
func parseArgs(argsJSON string) (map[string]any, error) {
	if argsJSON == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, err
	}
	return args, nil
}
