package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
)

func (s *Server) registerAdminRoutes() {
	s.mux.HandleFunc("/api/diagnostics", s.withAuth(s.handleDiagnostics))
	s.mux.HandleFunc("/api/admin/wipe", s.withAuth(s.handleAdminWipe))
	s.mux.HandleFunc("/api/cleanup", s.withAuth(s.handleCleanup))
	s.mux.HandleFunc("/api/prefs", s.withAuth(s.handlePrefs))
	s.mux.HandleFunc("/api/presets", s.withAuth(s.handlePresets))
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	dbStats := s.db.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"go_version":    runtime.Version(),
		"goroutines":    runtime.NumGoroutine(),
		"memory_alloc":  memStats.Alloc,
		"memory_sys":    memStats.Sys,
		"db_open_conns": dbStats.OpenConnections,
		"db_in_use":     dbStats.InUse,
		"settings": map[string]any{
			"search_provider": settings.GetString("search_provider"),
			"tts_provider":    settings.GetString("tts_provider"),
			"stt_provider":    settings.GetString("stt_provider"),
		},
	})
}

func (s *Server) handleAdminWipe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	user := auth.CurrentUser(r)
	s.db.Exec(`DELETE FROM memories WHERE owner=?`, user)
	s.db.Exec(`DELETE FROM notes WHERE owner=?`, user)
	s.db.Exec(`DELETE FROM sessions WHERE owner=?`, user)
	writeJSON(w, http.StatusOK, map[string]string{"status": "wiped"})
}

func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	s.db.Exec(`DELETE FROM sessions WHERE archived=1 AND last_accessed < datetime('now', '-30 days')`)
	s.db.Exec(`DELETE FROM gallery_images WHERE is_active=0 AND updated_at < datetime('now', '-7 days')`)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handlePrefs(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	prefsFile := fmt.Sprintf("%s/prefs_%s.json", s.cfg.DataDir, sanitizeFilename(user))
	switch r.Method {
	case http.MethodGet:
		var prefs map[string]any
		if err := storage.ReadJSON(prefsFile, &prefs); err != nil {
			prefs = map[string]any{}
		}
		writeJSON(w, http.StatusOK, prefs)
	case http.MethodPost:
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		var prefs map[string]any
		storage.ReadJSON(prefsFile, &prefs)
		if prefs == nil {
			prefs = map[string]any{}
		}
		for k, v := range req {
			prefs[k] = v
		}
		storage.WriteJSON(prefsFile, prefs)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	presetsFile := s.cfg.DataDir + "/presets.json"
	switch r.Method {
	case http.MethodGet:
		var presets []map[string]any
		if err := storage.ReadJSON(presetsFile, &presets); err != nil {
			presets = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, presets)
	case http.MethodPost:
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		var presets []map[string]any
		storage.ReadJSON(presetsFile, &presets)
		presets = append(presets, req)
		storage.WriteJSON(presetsFile, presets)
		writeJSON(w, http.StatusOK, req)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func sanitizeFilename(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
