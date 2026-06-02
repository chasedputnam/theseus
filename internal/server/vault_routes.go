package server

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/storage"
)

type vaultConfig struct {
	ServerURL  string `json:"server_url"`
	Email      string `json:"email"`
	BWSession  string `json:"bw_session,omitempty"`
}

func (s *Server) registerVaultRoutes() {
	s.mux.HandleFunc("/api/vault/config", s.withAuth(s.handleVaultConfig))
	s.mux.HandleFunc("/api/vault/unlock", s.withAuth(s.handleVaultUnlock))
	s.mux.HandleFunc("/api/vault/lock", s.withAuth(s.handleVaultLock))
	s.mux.HandleFunc("/api/vault/login", s.withAuth(s.handleVaultLogin))
	s.mux.HandleFunc("/api/vault/logout", s.withAuth(s.handleVaultLogout))
	s.mux.HandleFunc("/api/vault/status", s.withAuth(s.handleVaultStatus))
}

func (s *Server) vaultFile() string {
	return filepath.Join(s.cfg.DataDir, "vault.json")
}

func (s *Server) loadVaultConfig() (*vaultConfig, error) {
	var cfg vaultConfig
	if err := storage.ReadJSON(s.vaultFile(), &cfg); err != nil {
		if os.IsNotExist(err) {
			return &vaultConfig{}, nil
		}
		return nil, err
	}
	return &cfg, nil
}

func (s *Server) saveVaultConfig(cfg *vaultConfig) error {
	if err := storage.WriteJSON(s.vaultFile(), cfg); err != nil {
		return err
	}
	return os.Chmod(s.vaultFile(), 0600)
}

func (s *Server) handleVaultConfig(w http.ResponseWriter, r *http.Request) {
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg, err := s.loadVaultConfig()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		bw := findBW()
		_, bwErr := exec.LookPath(bw)
		bwInstalled := bwErr == nil
		unlocked := cfg.BWSession != ""
		writeJSON(w, http.StatusOK, map[string]any{
			"server_url":   cfg.ServerURL,
			"email":        cfg.Email,
			"bw_installed": bwInstalled,
			"unlocked":     unlocked,
		})
	case http.MethodPost:
		var req struct {
			ServerURL string `json:"server_url"`
			Email     string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		cfg, _ := s.loadVaultConfig()
		cfg.ServerURL = req.ServerURL
		cfg.Email = req.Email
		if err := s.saveVaultConfig(cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleVaultUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	bw := findBW()
	cfg, _ := s.loadVaultConfig()

	// Configure server if set
	if cfg.ServerURL != "" {
		exec.Command(bw, "config", "server", cfg.ServerURL).Run()
	}

	// Unlock — pass password via stdin to avoid exposure in process list
	cmd := exec.Command(bw, "unlock", "--raw")
	cmd.Stdin = strings.NewReader(req.Password + "\n")
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Vault unlock failed"})
		return
	}
	session := strings.TrimSpace(string(out))
	cfg.BWSession = session
	s.saveVaultConfig(cfg)
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlocked"})
}

func (s *Server) handleVaultStatus(w http.ResponseWriter, r *http.Request) {
	cfg, _ := s.loadVaultConfig()
	bw := findBW()
	unlocked := false
	if cfg.BWSession != "" {
		cmd := exec.Command(bw, "status")
		cmd.Env = append(os.Environ(), "BW_SESSION="+cfg.BWSession)
		out, err := cmd.Output()
		if err == nil && strings.Contains(string(out), "unlocked") {
			unlocked = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": cfg.ServerURL != "" || cfg.Email != "",
		"unlocked":   unlocked,
	})
}

func (s *Server) handleVaultLock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	cfg, _ := s.loadVaultConfig()
	bw := findBW()
	if cfg.BWSession != "" {
		cmd := exec.Command(bw, "lock")
		cmd.Env = append(os.Environ(), "BW_SESSION="+cfg.BWSession)
		cmd.Run()
	}
	cfg.BWSession = ""
	s.saveVaultConfig(cfg)
	writeJSON(w, http.StatusOK, map[string]string{"status": "locked"})
}

func (s *Server) handleVaultLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	bw := findBW()
	cfg, _ := s.loadVaultConfig()
	if cfg.ServerURL != "" {
		exec.Command(bw, "config", "server", cfg.ServerURL).Run()
	}
	cmd := exec.Command(bw, "login", req.Email, "--raw")
	cmd.Stdin = strings.NewReader(req.Password + "\n")
	out, err := cmd.Output()
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Login failed"})
		return
	}
	session := strings.TrimSpace(string(out))
	cfg.Email = req.Email
	cfg.BWSession = session
	s.saveVaultConfig(cfg)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_in"})
}

func (s *Server) handleVaultLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	cfg, _ := s.loadVaultConfig()
	bw := findBW()
	if cfg.BWSession != "" {
		cmd := exec.Command(bw, "logout")
		cmd.Env = append(os.Environ(), "BW_SESSION="+cfg.BWSession)
		cmd.Run()
	}
	cfg.BWSession = ""
	s.saveVaultConfig(cfg)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func findBW() string {
	candidates := []string{
		"bw",
		"/usr/local/bin/bw",
		"/opt/homebrew/bin/bw",
	}
	for _, c := range candidates {
		if path, err := exec.LookPath(c); err == nil {
			return path
		}
	}
	return "bw"
}

