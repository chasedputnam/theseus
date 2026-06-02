package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chaseputnam/theseus/internal/mcp"
)

func (s *Server) registerMCPRoutes() {
	s.mux.HandleFunc("/api/mcp/servers", s.withAuth(s.handleMCPServers))
	s.mux.HandleFunc("/api/mcp/servers/", s.withAuth(s.handleMCPServerByID))
	s.mux.HandleFunc("/api/mcp/tools", s.withAuth(s.handleMCPTools))
	s.mux.HandleFunc("/api/mcp/call", s.withAuth(s.handleMCPCall))
	s.mux.HandleFunc("/api/mcp/oauth/authorize/", s.withAuth(s.handleMCPOAuthAuthorize))
	s.mux.HandleFunc("/api/mcp/oauth/callback", s.handleMCPOAuthCallback)
}

func (s *Server) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"servers": s.mcpManager.Status()})
	case http.MethodPost:
		var cfg mcp.ServerConfig
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/json") {
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
		} else {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				r.ParseForm()
			}
			cfg.Name = r.FormValue("name")
			cfg.Transport = r.FormValue("transport")
			cfg.Command = r.FormValue("command")
			cfg.URL = r.FormValue("url")
			if args := r.FormValue("args"); args != "" {
				json.Unmarshal([]byte(args), &cfg.Args)
			}
			if env := r.FormValue("env"); env != "" {
				json.Unmarshal([]byte(env), &cfg.Env)
			}
		}
		if cfg.ID == "" {
			cfg.ID = cfg.Name
		}
		result := map[string]any{"id": cfg.ID}
		if err := s.mcpManager.Connect(context.Background(), cfg); err != nil {
			result["connected"] = false
			result["error"] = err.Error()
		} else {
			result["connected"] = true
			tools, _, _ := s.mcpManager.GetServerTools(cfg.ID)
			result["tool_count"] = len(tools)
		}
		writeJSON(w, http.StatusOK, result)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMCPOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/mcp/oauth/authorize/")
	oauthURL := s.mcpManager.GetOAuthURL(id)
	if oauthURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oauth not configured for this server"})
		return
	}
	http.Redirect(w, r, oauthURL, http.StatusFound)
}

func (s *Server) handleMCPOAuthCallback(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("state")
	token := r.URL.Query().Get("code")
	if id != "" && token != "" {
		found := false
		for _, srv := range s.mcpManager.Status() {
			if srvID, _ := srv["id"].(string); srvID == id {
				found = true
				break
			}
		}
		if found {
			s.mcpManager.SetToken(id, token)
		}
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`<html><body><p>Authorization complete. You may close this tab.</p></body></html>`))
}

func (s *Server) handleMCPServerByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mcp/servers/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if action == "tools" && r.Method == http.MethodGet {
		tools, disabledTools, ok := s.mcpManager.GetServerTools(id)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
			return
		}
		type toolEntry struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			IsDisabled  bool   `json:"is_disabled"`
		}
		disabled := make(map[string]bool, len(disabledTools))
		for _, t := range disabledTools {
			disabled[t] = true
		}
		result := make([]toolEntry, 0, len(tools))
		for _, t := range tools {
			result = append(result, toolEntry{
				Name:        t.Name,
				Description: t.Description,
				IsDisabled:  disabled[t.Name],
			})
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if action == "reconnect" && r.Method == http.MethodPost {
		if err := s.mcpManager.Reconnect(context.Background(), id); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"connected": false, "error": err.Error()})
			return
		}
		tools, _, _ := s.mcpManager.GetServerTools(id)
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "tool_count": len(tools)})
		return
	}
	switch r.Method {
	case http.MethodDelete:
		s.mcpManager.Disconnect(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
	case http.MethodPatch:
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			r.ParseForm()
		}
		if v := r.FormValue("is_enabled"); v != "" {
			enabled := v == "true" || v == "1"
			s.mcpManager.SetEnabled(id, enabled)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMCPTools(w http.ResponseWriter, r *http.Request) {
	tools := s.mcpManager.ListTools()
	if tools == nil {
		tools = []mcp.ToolSchema{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (s *Server) handleMCPCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	result, err := s.mcpManager.CallTool(r.Context(), req.Tool, req.Args)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": result})
}
