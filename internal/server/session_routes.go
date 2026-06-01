package server

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/google/uuid"
)

func (s *Server) registerSessionRoutes() {
	s.mux.HandleFunc("/api/sessions", s.withAuth(s.handleSessions))
	s.mux.HandleFunc("/api/sessions/", s.withAuth(s.handleSessionByID))
	s.mux.HandleFunc("/api/settings", s.withAuth(s.handleSettings))
	s.mux.HandleFunc("/api/features", s.withAuth(s.handleFeatures))
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		archived := r.URL.Query().Get("archived") == "true"
		sessions, err := s.db.ListSessions(user, archived)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if sessions == nil {
			sessions = []*db.Session{}
		}
		writeJSON(w, http.StatusOK, sessions)
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			EndpointURL string `json:"endpoint_url"`
			Model       string `json:"model"`
			RAG         bool   `json:"rag"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		sess := &db.Session{
			ID:          uuid.New().String(),
			Name:        req.Name,
			EndpointURL: req.EndpointURL,
			Model:       req.Model,
			RAG:         req.RAG,
			Owner:       sql.NullString{String: user, Valid: user != ""},
			Headers:     "{}",
		}
		if err := s.db.CreateSession(sess); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, sess)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/")
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	sess, err := s.db.GetSession(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if sess.Owner.Valid && sess.Owner.String != user && !s.auth.IsAdmin(user) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	switch action {
	case "messages":
		msgs, err := s.db.ListMessages(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if msgs == nil {
			msgs = []*db.ChatMessage{}
		}
		writeJSON(w, http.StatusOK, msgs)
		return
	case "archive":
		if r.Method == http.MethodPost {
			sess.Archived = true
			if err := s.db.UpdateSession(sess); err != nil {
				log.Printf("db: UpdateSession: %v", err)
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
		return
	case "unarchive":
		if r.Method == http.MethodPost {
			sess.Archived = false
			if err := s.db.UpdateSession(sess); err != nil {
				log.Printf("db: UpdateSession: %v", err)
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
		return
	case "star":
		if r.Method == http.MethodPost {
			sess.IsImportant = !sess.IsImportant
			if err := s.db.UpdateSession(sess); err != nil {
				log.Printf("db: UpdateSession: %v", err)
			}
			writeJSON(w, http.StatusOK, map[string]bool{"is_important": sess.IsImportant})
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, sess)
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Name        *string `json:"name"`
			EndpointURL *string `json:"endpoint_url"`
			Model       *string `json:"model"`
			RAG         *bool   `json:"rag"`
			Folder      *string `json:"folder"`
			IsImportant *bool   `json:"is_important"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req.Name != nil {
			sess.Name = *req.Name
		}
		if req.EndpointURL != nil {
			sess.EndpointURL = *req.EndpointURL
		}
		if req.Model != nil {
			sess.Model = *req.Model
		}
		if req.RAG != nil {
			sess.RAG = *req.RAG
		}
		if req.Folder != nil {
			sess.Folder = sql.NullString{String: *req.Folder, Valid: *req.Folder != ""}
		}
		if req.IsImportant != nil {
			sess.IsImportant = *req.IsImportant
		}
		if err := s.db.UpdateSession(sess); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, sess)
	case http.MethodDelete:
		if err := s.db.DeleteSession(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, settings.Load())
	case http.MethodPost:
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
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, settings.LoadFeatures())
	case http.MethodPost:
		var req map[string]bool
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		current := settings.LoadFeatures()
		for k, v := range req {
			current[k] = v
		}
		if err := settings.SaveFeatures(current); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
