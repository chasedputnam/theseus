package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/webhook"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) registerWebhookRoutes() {
	s.mux.HandleFunc("/api/webhooks", s.withAuth(s.handleWebhooks))
	s.mux.HandleFunc("/api/webhooks/", s.withAuth(s.handleWebhookByID))
	s.mux.HandleFunc("/api/tokens", s.withAuth(s.handleAPITokens))
	s.mux.HandleFunc("/api/tokens/", s.withAuth(s.handleAPITokenByID))
}

func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		hooks, err := s.db.ListWebhooks()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if hooks == nil {
			hooks = []*db.Webhook{}
		}
		writeJSON(w, http.StatusOK, hooks)
	case http.MethodPost:
		var req struct {
			Name   string
			URL    string
			Secret string
			Events string
		}
		if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			var j struct {
				Name   string `json:"name"`
				URL    string `json:"url"`
				Secret string `json:"secret"`
				Events string `json:"events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&j); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			req.Name, req.URL, req.Secret, req.Events = j.Name, j.URL, j.Secret, j.Events
		} else {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				r.ParseForm()
			}
			req.Name = r.FormValue("name")
			req.URL = r.FormValue("url")
			req.Secret = r.FormValue("secret")
			req.Events = r.FormValue("events")
		}
		if err := webhook.ValidateWebhookURL(req.URL); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		hook := &db.Webhook{
			ID:       uuid.New().String(),
			Name:     req.Name,
			URL:      req.URL,
			Secret:   sql.NullString{String: req.Secret, Valid: req.Secret != ""},
			Events:   req.Events,
			IsActive: true,
		}
		if err := s.db.CreateWebhook(hook); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, hook)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWebhookByID(w http.ResponseWriter, r *http.Request) {
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/webhooks/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	hook, err := s.db.GetWebhook(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if action == "test" && r.Method == http.MethodPost {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Post(hook.URL, "application/json", strings.NewReader(`{"test":true}`))
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		resp.Body.Close()
		writeJSON(w, http.StatusOK, map[string]any{"ok": resp.StatusCode < 400, "status": resp.StatusCode})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, hook)
	case http.MethodPut, http.MethodPatch:
		var req struct {
			Name     *string `json:"name"`
			URL      *string `json:"url"`
			Events   *string `json:"events"`
			IsActive *bool   `json:"is_active"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Name != nil {
			hook.Name = *req.Name
		}
		if req.URL != nil {
			hook.URL = *req.URL
		}
		if req.Events != nil {
			hook.Events = *req.Events
		}
		if req.IsActive != nil {
			hook.IsActive = *req.IsActive
		}
		if err := s.db.UpdateWebhook(hook); err != nil {
			log.Printf("db: UpdateWebhook: %v", err)
		}
		writeJSON(w, http.StatusOK, hook)
	case http.MethodDelete:
		if err := s.db.DeleteWebhook(id); err != nil {
			log.Printf("db: DeleteWebhook: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// --- API Tokens ---

func (s *Server) handleAPITokens(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	switch r.Method {
	case http.MethodGet:
		tokens, err := s.db.ListAPITokens(user, s.auth.IsAdmin(user))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tokens == nil {
			tokens = []*db.APIToken{}
		}
		writeJSON(w, http.StatusOK, tokens)
	case http.MethodPost:
		var name, scopes string
		if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			var req struct {
				Name   string `json:"name"`
				Scopes string `json:"scopes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			name, scopes = req.Name, req.Scopes
		} else {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				r.ParseForm()
			}
			name = r.FormValue("name")
			scopes = r.FormValue("scopes")
		}
		if scopes == "" {
			scopes = "chat"
		}
		// Generate token
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token generation failed"})
			return
		}
		token := hex.EncodeToString(raw)
		prefix := token[:8]
		hash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token hash failed"})
			return
		}

		apiToken := &db.APIToken{
			ID:          uuid.New().String(),
			Owner:       sql.NullString{String: user, Valid: user != ""},
			Name:        name,
			TokenHash:   string(hash),
			TokenPrefix: prefix,
			Scopes:      scopes,
			IsActive:    true,
		}
		if err := s.db.CreateAPIToken(apiToken); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// Return full token ONCE
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     apiToken.ID,
			"name":   apiToken.Name,
			"token":  token, // only returned on creation
			"prefix": prefix,
			"scopes": scopes,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAPITokenByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	id := strings.TrimPrefix(r.URL.Path, "/api/tokens/")
	if r.Method == http.MethodDelete {
		token, err := s.db.GetAPIToken(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if token.Owner.Valid && token.Owner.String != user && !s.auth.IsAdmin(user) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if err := s.db.DeleteAPIToken(id); err != nil {
			log.Printf("db: DeleteAPIToken: %v", err)
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

