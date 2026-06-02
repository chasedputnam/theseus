package auth

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

// Integration holds a third-party API key integration.
type Integration struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Key        string    `json:"key"` // AES-256-GCM encrypted at rest
	Owner      string    `json:"owner"`
	BaseURL    string    `json:"base_url"`
	AuthType   string    `json:"auth_type"`
	AuthHeader string    `json:"auth_header"`
	Preset     string    `json:"preset"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
}

func integrationsFile(dataDir, user string) string {
	return dataDir + "/integrations_" + storage.SanitizeFilename(user) + ".json"
}

func loadIntegrations(dataDir, user string) []*Integration {
	var items []*Integration
	storage.ReadJSON(integrationsFile(dataDir, user), &items)
	if items == nil {
		items = []*Integration{}
	}
	return items
}

func saveIntegrations(dataDir, user string, items []*Integration) error {
	return storage.WriteJSON(integrationsFile(dataDir, user), items)
}

func maskIntegration(i *Integration) map[string]any {
	return map[string]any{
		"id": i.ID, "name": i.Name, "type": i.Type,
		"key": "***", "owner": i.Owner, "created_at": i.CreatedAt,
		"base_url": i.BaseURL, "auth_type": i.AuthType,
		"auth_header": i.AuthHeader, "preset": i.Preset,
		"enabled": i.Enabled,
	}
}

var integrationPresets = []map[string]any{
	{"type": "openai", "name": "OpenAI", "fields": []string{"api_key"}, "base_url": "https://api.openai.com/v1"},
	{"type": "anthropic", "name": "Anthropic", "fields": []string{"api_key"}, "base_url": "https://api.anthropic.com"},
	{"type": "groq", "name": "Groq", "fields": []string{"api_key"}, "base_url": "https://api.groq.com/openai/v1"},
	{"type": "mistral", "name": "Mistral", "fields": []string{"api_key"}, "base_url": "https://api.mistral.ai/v1"},
	{"type": "cohere", "name": "Cohere", "fields": []string{"api_key"}, "base_url": "https://api.cohere.ai/v1"},
	{"type": "together", "name": "Together AI", "fields": []string{"api_key"}, "base_url": "https://api.together.xyz/v1"},
	{"type": "openrouter", "name": "OpenRouter", "fields": []string{"api_key"}, "base_url": "https://openrouter.ai/api/v1"},
}

// RegisterIntegrationRoutes mounts integration routes onto mux.
func RegisterIntegrationRoutes(mux *http.ServeMux, mgr *Manager) {
	dataDir := filepath.Dir(mgr.configPath)
	mux.HandleFunc("/api/auth/integrations/presets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"presets": integrationPresets})
	})
	mux.HandleFunc("/api/auth/integrations/", func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/auth/integrations/")
		parts := strings.SplitN(path, "/", 2)
		id := parts[0]
		action := ""
		if len(parts) > 1 {
			action = parts[1]
		}
		items := loadIntegrations(dataDir, user)
		// Handle test sub-action
		if action == "test" && r.Method == http.MethodPost {
			var target *Integration
			for _, item := range items {
				if item.ID == id {
					target = item
					break
				}
			}
			if target == nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			// Basic connectivity test — just verify the key is set
			hasKey := target.Key != "" && target.Key != "***"
			if hasKey {
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "Integration configured"})
			} else {
				writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "No API key set"})
			}
			return
		}
		switch r.Method {
		case http.MethodPut, http.MethodPatch:
			var req Integration
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			for i, item := range items {
				if item.ID == id {
					req.ID = id
					req.Owner = user
					req.CreatedAt = item.CreatedAt
					if req.Key != "" && req.Key != "***" && !storage.IsEncrypted(req.Key) {
						enc, _ := storage.Encrypt(req.Key)
						req.Key = enc
					} else if req.Key == "" || req.Key == "***" {
						req.Key = item.Key
					}
					items[i] = &req
					saveIntegrations(dataDir, user, items)
					writeJSON(w, http.StatusOK, maskIntegration(&req))
					return
				}
			}
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		case http.MethodDelete:
			for i, item := range items {
				if item.ID == id {
					items = append(items[:i], items[i+1:]...)
					saveIntegrations(dataDir, user, items)
					writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
					return
				}
			}
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/auth/integrations", func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		items := loadIntegrations(dataDir, user)
		switch r.Method {
		case http.MethodGet:
			masked := make([]map[string]any, len(items))
			for i, item := range items {
				masked[i] = maskIntegration(item)
			}
			writeJSON(w, http.StatusOK, map[string]any{"integrations": masked})
		case http.MethodPost:
			var req Integration
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			req.ID = uuid.New().String()
			req.Owner = user
			req.CreatedAt = time.Now().UTC()
			if req.Key != "" {
				enc, err := storage.Encrypt(req.Key)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encrypt failed"})
					return
				}
				req.Key = enc
			}
			items = append(items, &req)
			saveIntegrations(dataDir, user, items)
			writeJSON(w, http.StatusOK, maskIntegration(&req))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
