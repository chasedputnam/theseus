package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

func (s *Server) registerAssistantRoutes() {
	s.mux.HandleFunc("/api/assistant/config", s.withAuth(s.handleAssistantConfig))
	s.mux.HandleFunc("/api/assistant/personas", s.withAuth(s.handleAssistantPersonas))
	s.mux.HandleFunc("/api/assistant/personas/", s.withAuth(s.handleAssistantPersonaByID))
}

func (s *Server) assistantPersonasFile(user string) string {
	return s.cfg.DataDir + "/personas_" + sanitizeFilename(user) + ".json"
}

type AssistantPersona struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt"`
	Avatar       string `json:"avatar"`
	IsDefault    bool   `json:"is_default"`
}

func (s *Server) handleAssistantConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{
			"system_prompt":    settings.GetString("system_prompt"),
			"assistant_name":   settings.GetString("assistant_name"),
			"default_model":    settings.GetString("default_model"),
			"default_endpoint": settings.GetString("default_endpoint_id"),
		})
	case http.MethodPost, http.MethodPut:
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		current := settings.Load()
		for k, v := range req {
			current[k] = v
		}
		if err := settings.Save(current); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAssistantPersonas(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	file := s.assistantPersonasFile(user)
	switch r.Method {
	case http.MethodGet:
		var personas []*AssistantPersona
		storage.ReadJSON(file, &personas)
		if personas == nil {
			personas = []*AssistantPersona{}
		}
		writeJSON(w, http.StatusOK, personas)
	case http.MethodPost:
		var p AssistantPersona
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		p.ID = uuid.New().String()
		var personas []*AssistantPersona
		storage.ReadJSON(file, &personas)
		personas = append(personas, &p)
		storage.WriteJSON(file, personas)
		writeJSON(w, http.StatusOK, &p)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAssistantPersonaByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	id := strings.TrimPrefix(r.URL.Path, "/api/assistant/personas/")
	file := s.assistantPersonasFile(user)
	var personas []*AssistantPersona
	storage.ReadJSON(file, &personas)

	switch r.Method {
	case http.MethodGet:
		for _, p := range personas {
			if p.ID == id {
				writeJSON(w, http.StatusOK, p)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case http.MethodPut, http.MethodPatch:
		var req AssistantPersona
		json.NewDecoder(r.Body).Decode(&req)
		for i, p := range personas {
			if p.ID == id {
				req.ID = id
				personas[i] = &req
				storage.WriteJSON(file, personas)
				writeJSON(w, http.StatusOK, &req)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case http.MethodDelete:
		for i, p := range personas {
			if p.ID == id {
				personas = append(personas[:i], personas[i+1:]...)
				storage.WriteJSON(file, personas)
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
