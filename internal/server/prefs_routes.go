package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/storage"
)

func (s *Server) registerPrefsRoutes() {
	s.mux.HandleFunc("/api/prefs/theme", s.withAuth(s.handlePrefsTheme))
	s.mux.HandleFunc("/api/prefs/custom-themes", s.withAuth(s.handlePrefsCustomThemes))
	s.mux.HandleFunc("/api/prefs/custom-themes/", s.withAuth(s.handlePrefsCustomThemes))
	s.mux.HandleFunc("/api/prefs/", s.withAuth(s.handlePrefsByKey))
}

func (s *Server) prefsFile(user string) string {
	return fmt.Sprintf("%s/prefs_%s.json", s.cfg.DataDir, sanitizeFilename(user))
}

func (s *Server) loadPrefs(user string) map[string]any {
	var prefs map[string]any
	storage.ReadJSON(s.prefsFile(user), &prefs)
	if prefs == nil {
		prefs = map[string]any{}
	}
	return prefs
}

func (s *Server) savePrefs(user string, prefs map[string]any) error {
	return storage.WriteJSON(s.prefsFile(user), prefs)
}

func (s *Server) customThemesFile(user string) string {
	return s.cfg.DataDir + "/custom_themes_" + sanitizeFilename(user) + ".json"
}

func (s *Server) handlePrefsCustomThemes(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	file := s.customThemesFile(user)
	var themes []map[string]any
	storage.ReadJSON(file, &themes)
	if themes == nil {
		themes = []map[string]any{}
	}

	// DELETE /api/prefs/custom-themes/{name}
	name := strings.TrimPrefix(r.URL.Path, "/api/prefs/custom-themes/")
	if name != "" && r.Method == http.MethodDelete {
		newThemes := make([]map[string]any, 0, len(themes))
		for _, t := range themes {
			if n, _ := t["name"].(string); n != name {
				newThemes = append(newThemes, t)
			}
		}
		storage.WriteJSON(file, newThemes)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"themes": themes})
	case http.MethodPost:
		var theme map[string]any
		if err := json.NewDecoder(r.Body).Decode(&theme); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		themes = append(themes, theme)
		storage.WriteJSON(file, themes)
		writeJSON(w, http.StatusOK, theme)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePrefsTheme(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	prefs := s.loadPrefs(user)
	switch r.Method {
	case http.MethodGet:
		theme, _ := prefs["theme"].(string)
		if theme == "" {
			theme = "dark"
		}
		writeJSON(w, http.StatusOK, map[string]string{"theme": theme})
	case http.MethodPost, http.MethodPut:
		var req struct{ Theme string `json:"theme"` }
		json.NewDecoder(r.Body).Decode(&req)
		prefs["theme"] = req.Theme
		s.savePrefs(user, prefs)
		writeJSON(w, http.StatusOK, map[string]string{"theme": req.Theme})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePrefsByKey(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	key := strings.TrimPrefix(r.URL.Path, "/api/prefs/")
	prefs := s.loadPrefs(user)
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": prefs[key]})
	case http.MethodPost, http.MethodPut:
		var req struct{ Value any `json:"value"` }
		json.NewDecoder(r.Body).Decode(&req)
		prefs[key] = req.Value
		s.savePrefs(user, prefs)
		writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": req.Value})
	case http.MethodDelete:
		delete(prefs, key)
		s.savePrefs(user, prefs)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
