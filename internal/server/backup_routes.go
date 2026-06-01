package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/google/uuid"
)

func (s *Server) registerBackupRoutes() {
	s.mux.HandleFunc("/api/export", s.withAuth(s.handleExport))
	s.mux.HandleFunc("/api/import", s.withAuth(s.handleImport))
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	user := auth.CurrentUser(r)
	memories, _ := s.db.ListMemories(user)
	export := map[string]any{
		"version":     1,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"exported_by": user,
		"memories":    memories,
		"settings":    settings.Load(),
		"features":    settings.LoadFeatures(),
	}
	filename := fmt.Sprintf("theseus_backup_%s.json", time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	json.NewEncoder(w).Encode(export)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	user := auth.CurrentUser(r)
	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if v, _ := data["version"].(float64); v == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown backup version"})
		return
	}
	imported := map[string]int{}

	if sv, ok := data["settings"].(map[string]any); ok {
		current := settings.Load()
		for k, v := range sv {
			current[k] = v
		}
		settings.Save(current)
		imported["settings"] = 1
	}
	if fv, ok := data["features"].(map[string]any); ok {
		current := settings.LoadFeatures()
		for k, v := range fv {
			if b, ok := v.(bool); ok {
				current[k] = b
			}
		}
		settings.SaveFeatures(current)
		imported["features"] = 1
	}
	if mems, ok := data["memories"].([]any); ok {
		count := 0
		for _, m := range mems {
			mMap, ok := m.(map[string]any)
			if !ok {
				continue
			}
			text, _ := mMap["text"].(string)
			if text == "" {
				continue
			}
			category, _ := mMap["category"].(string)
			if category == "" {
				category = "fact"
			}
			source, _ := mMap["source"].(string)
			if source == "" {
				source = "import"
			}
			entry := &db.Memory{
				ID:       uuid.New().String(),
				Text:     text,
				Category: category,
				Source:   source,
				Owner:    sql.NullString{String: user, Valid: user != ""},
				Timestamp: time.Now().Unix(),
			}
			if err := s.db.AddMemory(entry); err != nil {
				log.Printf("db: AddMemory: %v", err)
			}
			count++
		}
		imported["memories"] = count
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "imported": imported})
}
