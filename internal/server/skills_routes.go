package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/memory"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

func (s *Server) registerSkillsRoutes() {
	s.mux.HandleFunc("/api/skills", s.withAuth(s.handleSkills))
	s.mux.HandleFunc("/api/skills/add", s.withAuth(s.handleSkillsAdd))
	s.mux.HandleFunc("/api/skills/audit-all", s.withAuth(s.handleSkillsAuditAll))
	s.mux.HandleFunc("/api/skills/audit-all/status", s.withAuth(s.handleSkillsAuditAllStatus))
	s.mux.HandleFunc("/api/skills/audit-all/cancel", s.withAuth(s.handleSkillsAuditAllCancel))
	s.mux.HandleFunc("/api/skills/builtin", s.withAuth(s.handleSkillsBuiltin))
	s.mux.HandleFunc("/api/skills/builtin/", s.withAuth(s.handleSkillsBuiltinByName))
	s.mux.HandleFunc("/api/skills/", s.withAuth(s.handleSkillByID))
}

func (s *Server) skillsManager() *memory.SkillsManager {
	return memory.NewSkillsManager(filepath.Join(s.cfg.DataDir, "skills"))
}

func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	sm := s.skillsManager()
	switch r.Method {
	case http.MethodGet:
		skills, err := sm.List(user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if skills == nil {
			skills = []*memory.Skill{}
		}
		writeJSON(w, http.StatusOK, skills)
	case http.MethodPost:
		var skill memory.Skill
		if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		skill.Owner = user
		if skill.Status == "" {
			skill.Status = "draft"
		}
		if err := sm.Save(&skill); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, &skill)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSkillByID(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	sm := s.skillsManager()
	path := strings.TrimPrefix(r.URL.Path, "/api/skills/")

	// path is "category/name" or "category/name/publish"
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	category, name := parts[0], parts[1]
	action := ""
	if len(parts) == 3 {
		action = parts[2]
	}

	if action == "publish" && r.Method == http.MethodPost {
		if err := sm.Publish(category, name); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "published"})
		return
	}

	if action == "markdown" && r.Method == http.MethodGet {
		skillDir := filepath.Join(s.cfg.DataDir, "skills", category, name)
		data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "SKILL.md not found"})
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(data)
		return
	}

	if action == "test" && r.Method == http.MethodPost {
		runID := uuid.New().String()
		runFile := filepath.Join(s.cfg.DataDir, "skill_test_runs_"+sanitizeFilename(user)+".json")
		var runs []map[string]any
		storage.ReadJSON(runFile, &runs)
		run := map[string]any{"run_id": runID, "skill": category + "/" + name, "status": "pending", "started_at": time.Now().UTC()}
		runs = append(runs, run)
		storage.WriteJSON(runFile, runs)
		writeJSON(w, http.StatusOK, map[string]string{"run_id": runID})
		return
	}

	if action == "test-status" && r.Method == http.MethodGet {
		runFile := filepath.Join(s.cfg.DataDir, "skill_test_runs_"+sanitizeFilename(user)+".json")
		var runs []map[string]any
		storage.ReadJSON(runFile, &runs)
		for i := len(runs) - 1; i >= 0; i-- {
			if runs[i]["skill"] == category+"/"+name {
				writeJSON(w, http.StatusOK, runs[i])
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "no_runs"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		skill, err := sm.Get(category, name)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, skill)
	case http.MethodPut, http.MethodPatch:
		var skill memory.Skill
		if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		skill.Category = category
		skill.Name = name
		skill.Owner = user
		if err := sm.Save(&skill); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, &skill)
	case http.MethodDelete:
		if err := sm.Delete(category, name); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSkillsAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	sm := s.skillsManager()
	var req struct {
		Category string `json:"category"`
		Name     string `json:"name"`
		Content  string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}
	if req.Category == "" {
		req.Category = "custom"
	}
	skill := &memory.Skill{
		Category: req.Category,
		Name:     req.Name,
		Owner:    user,
		Status:   "draft",
	}
	if err := sm.Save(skill); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Write SKILL.md content if provided
	if req.Content != "" {
		skillDir := filepath.Join(s.cfg.DataDir, "skills", req.Category, req.Name)
		os.MkdirAll(skillDir, 0755)
		os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(req.Content), 0644)
	}
	writeJSON(w, http.StatusOK, skill)
}

func (s *Server) handleSkillsAuditAll(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	sm := s.skillsManager()
	skills, err := sm.List(user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(skills), "status": "ok"})
}

func (s *Server) handleSkillsAuditAllStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "idle", "running": false})
}

func (s *Server) handleSkillsAuditAllCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (s *Server) builtinSkillsDir() string {
	dir := os.Getenv("CRUSH_SKILLS_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".claude", "skills")
	}
	return dir
}

func (s *Server) handleSkillsBuiltinByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/skills/builtin/")
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	builtinDir := s.builtinSkillsDir()
	overridesDir := filepath.Join(s.cfg.DataDir, "skill-overrides")

	switch r.Method {
	case http.MethodGet:
		overridePath := filepath.Join(overridesDir, name+".md")
		defaultPath := filepath.Join(builtinDir, name, "SKILL.md")
		defaultBytes, _ := os.ReadFile(defaultPath)
		overrideBytes, _ := os.ReadFile(overridePath)
		text := string(defaultBytes)
		if len(overrideBytes) > 0 {
			text = string(overrideBytes)
		}
		writeJSON(w, http.StatusOK, map[string]string{"text": text, "default": string(defaultBytes)})
	case http.MethodPut:
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := os.MkdirAll(overridesDir, 0755); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile(filepath.Join(overridesDir, name+".md"), []byte(body.Text), 0644); err != nil {
			http.Error(w, "write failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodDelete:
		os.Remove(filepath.Join(overridesDir, name+".md"))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSkillsBuiltin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	builtinDir := s.builtinSkillsDir()
	sm := memory.NewSkillsManager(builtinDir)
	skills, err := sm.List("")
	if err != nil || skills == nil {
		skills = []*memory.Skill{}
	}
	writeJSON(w, http.StatusOK, skills)
}

