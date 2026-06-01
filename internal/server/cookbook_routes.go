package server

import (
	"encoding/json"
	"net/http"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/cookbook"
	"github.com/google/uuid"
)

func (s *Server) registerCookbookRoutes() {
	s.mux.HandleFunc("/api/cookbook/hardware", s.withAuth(s.handleCookbookHardware))
	s.mux.HandleFunc("/api/cookbook/models", s.withAuth(s.handleCookbookModels))
	s.mux.HandleFunc("/api/cookbook/download", s.withAuth(s.handleCookbookDownload))
	s.mux.HandleFunc("/api/cookbook/serve", s.withAuth(s.handleCookbookServe))
	s.mux.HandleFunc("/api/cookbook/serve/", s.withAuth(s.handleCookbookServeByID))
}

func (s *Server) handleCookbookHardware(w http.ResponseWriter, r *http.Request) {
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	profile := cookbook.Detect()
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) handleCookbookModels(w http.ResponseWriter, r *http.Request) {
	profile := cookbook.Detect()
	filter := r.URL.Query().Get("q")
	models, err := cookbook.RecommendModels(profile, filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if models == nil {
		models = []*cookbook.ModelEntry{}
	}
	writeJSON(w, http.StatusOK, models)
}

func (s *Server) handleCookbookDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		RepoID   string `json:"repo_id"`
		Filename string `json:"filename"`
		HFToken  string `json:"hf_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	sessionName := "theseus-dl-" + uuid.New().String()[:8]
	cacheDir := s.cfg.DataDir + "/huggingface"
	sess, err := cookbook.StartDownload(req.RepoID, req.Filename, cacheDir, req.HFToken, sessionName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"session": sess.Name,
		"log":     sess.LogFile,
		"status":  "started",
	})
}

func (s *Server) handleCookbookServe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		Backend   string `json:"backend"`
		ModelPath string `json:"model_path"`
		ExtraArgs string `json:"extra_args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	sessionName := "theseus-serve-" + uuid.New().String()[:8]
	sess, err := cookbook.StartServe(req.Backend, req.ModelPath, req.ExtraArgs, sessionName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"session": sess.Name,
		"log":     sess.LogFile,
		"status":  "started",
	})
}

func (s *Server) handleCookbookServeByID(w http.ResponseWriter, r *http.Request) {
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	sessionName := r.URL.Path[len("/api/cookbook/serve/"):]
	switch r.Method {
	case http.MethodGet:
		alive := cookbook.IsSessionAlive(sessionName)
		log, _ := cookbook.TailLog("/tmp/theseus-tmux/"+sessionName+".log", 50)
		writeJSON(w, http.StatusOK, map[string]any{
			"session": sessionName,
			"alive":   alive,
			"log":     log,
		})
	case http.MethodDelete:
		cookbook.KillSession(sessionName)
		writeJSON(w, http.StatusOK, map[string]string{"status": "killed"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
