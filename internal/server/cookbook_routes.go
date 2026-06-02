package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/cookbook"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

func (s *Server) registerCookbookRoutes() {
	s.mux.HandleFunc("/api/cookbook/hardware", s.withAuth(s.handleCookbookHardware))
	s.mux.HandleFunc("/api/cookbook/models", s.withAuth(s.handleCookbookModels))
	s.mux.HandleFunc("/api/cookbook/download", s.withAuth(s.handleCookbookDownload))
	s.mux.HandleFunc("/api/cookbook/serve", s.withAuth(s.handleCookbookServe))
	s.mux.HandleFunc("/api/cookbook/serve/", s.withAuth(s.handleCookbookServeByID))
	s.mux.HandleFunc("/api/cookbook/packages", s.withAuth(s.handleCookbookPackages))
	s.mux.HandleFunc("/api/cookbook/state", s.withAuth(s.handleCookbookState))
	s.mux.HandleFunc("/api/cookbook/tasks/status", s.withAuth(s.handleCookbookTasksStatus))
	s.mux.HandleFunc("/api/cookbook/ssh-key", s.withAuth(s.handleCookbookSSHKey))
	s.mux.HandleFunc("/api/cookbook/gpus", s.withAuth(s.handleCookbookGPUs))
	s.mux.HandleFunc("/api/cookbook/kill-pid", s.withAuth(s.handleCookbookKillPID))
	s.mux.HandleFunc("/api/cookbook/setup", s.withAuth(s.handleCookbookSetup))
	s.mux.HandleFunc("/api/cookbook/hf-latest", s.withAuth(s.handleCookbookHFLatest))
	s.mux.HandleFunc("/api/model/download", s.withAuth(s.handleModelDownload))
	s.mux.HandleFunc("/api/model/serve", s.withAuth(s.handleModelServe))
	s.mux.HandleFunc("/api/model/cached", s.withAuth(s.handleModelCached))
	s.mux.HandleFunc("/api/hwfit/system", s.withAuth(s.handleHWFitSystem))
	s.mux.HandleFunc("/api/hwfit/models", s.withAuth(s.handleHWFitModels))
	s.mux.HandleFunc("/api/hwfit/image-models", s.withAuth(s.handleHWFitImageModels))
}

type cookbookState struct {
	Active    bool   `json:"active"`
	Session   string `json:"session"`
	Model     string `json:"model"`
	Port      int    `json:"port"`
	PID       int    `json:"pid"`
	Status    string `json:"status"`
	TaskType  string `json:"task_type"`
	TaskDone  bool   `json:"task_done"`
}

func (s *Server) cookbookStateFile() string {
	return s.cfg.DataDir + "/cookbook_state.json"
}

func (s *Server) handleCookbookPackages(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("q")
	models, err := cookbook.LoadCatalog()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if filter != "" {
		filtered := make([]*cookbook.ModelEntry, 0)
		for _, m := range models {
			if strings.Contains(strings.ToLower(m.Name), strings.ToLower(filter)) {
				filtered = append(filtered, m)
			}
		}
		models = filtered
	}
	if models == nil {
		models = []*cookbook.ModelEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"packages": models, "total": len(models)})
}

func (s *Server) handleCookbookState(w http.ResponseWriter, r *http.Request) {
	var state cookbookState
	if err := storage.ReadJSON(s.cookbookStateFile(), &state); err != nil {
		state = cookbookState{Status: "idle"}
	}
	if state.Session != "" {
		state.Active = cookbook.IsSessionAlive(state.Session)
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleCookbookTasksStatus(w http.ResponseWriter, r *http.Request) {
	var state cookbookState
	if err := storage.ReadJSON(s.cookbookStateFile(), &state); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false, "task_type": "", "done": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":    state.Active,
		"task_type": state.TaskType,
		"done":      state.TaskDone,
		"session":   state.Session,
	})
}

func (s *Server) handleCookbookSSHKey(w http.ResponseWriter, r *http.Request) {
	pubKeyFile := s.cfg.DataDir + "/cookbook_id_rsa.pub"
	privKeyFile := s.cfg.DataDir + "/cookbook_id_rsa"
	if data, err := os.ReadFile(pubKeyFile); err == nil {
		writeJSON(w, http.StatusOK, map[string]string{"public_key": strings.TrimSpace(string(data))})
		return
	}
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	os.WriteFile(privKeyFile, privPEM, 0600)
	pubKey, err := ssh.NewPublicKey(&privKey.PublicKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pubKeyStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pubKey)))
	os.WriteFile(pubKeyFile, []byte(pubKeyStr), 0644)
	writeJSON(w, http.StatusOK, map[string]string{"public_key": pubKeyStr})
}

func (s *Server) handleCookbookGPUs(w http.ResponseWriter, r *http.Request) {
	profile := cookbook.Detect()
	writeJSON(w, http.StatusOK, map[string]any{
		"gpu_name":    profile.GPUName,
		"gpu_vram_mb": profile.VRAM,
		"ram_mb":      profile.RAM,
	})
}

func (s *Server) handleCookbookKillPID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		PID int `json:"pid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PID <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pid required"})
		return
	}
	proc, err := os.FindProcess(req.PID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "process not found"})
		return
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleModelDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		RepoID             string `json:"repo_id"`
		Include            string `json:"include"`
		HFToken            string `json:"hf_token"`
		RemoteHost         string `json:"remote_host"`
		SSHPort            string `json:"ssh_port"`
		LocalDir           string `json:"local_dir"`
		EnvPrefix          string `json:"env_prefix"`
		Platform           string `json:"platform"`
		DisableHFTransfer  bool   `json:"disable_hf_transfer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	sessionName := "theseus-dl-" + uuid.New().String()[:8]
	cacheDir := req.LocalDir
	if cacheDir == "" {
		cacheDir = s.cfg.DataDir + "/huggingface"
	}
	var cmd string
	if req.Include != "" {
		cmd = "huggingface-cli download " + cookbook.ShellQuote(req.RepoID) + " " + cookbook.ShellQuote(req.Include) + " --local-dir " + cookbook.ShellQuote(cacheDir)
	} else {
		cmd = "huggingface-cli download " + cookbook.ShellQuote(req.RepoID) + " --local-dir " + cookbook.ShellQuote(cacheDir)
	}
	if req.HFToken != "" {
		cmd = "HF_TOKEN=" + cookbook.ShellQuote(req.HFToken) + " " + cmd
	}
	if req.DisableHFTransfer {
		cmd = "HF_HUB_DISABLE_PROGRESS_BARS=1 " + cmd
	}
	if req.EnvPrefix != "" {
		cmd = req.EnvPrefix + " && " + cmd
	}
	logDir := "/tmp/theseus-tmux"
	os.MkdirAll(logDir, 0755)
	logFile := logDir + "/" + sessionName + ".log"
	cmd += " 2>&1 | tee " + cookbook.ShellQuote(logFile)
	if req.RemoteHost != "" {
		sshArgs := buildSSHArgs(s.cfg.DataDir, req.RemoteHost, req.SSHPort)
		cmd = sshArgs + " " + cookbook.ShellQuote(cmd)
		if err := runRemoteCmd(cmd, sessionName, logFile); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	} else {
		if err := cookbook.StartTmux(sessionName, cmd, logFile); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionName})
}

func (s *Server) handleModelServe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		RepoID     string `json:"repo_id"`
		Cmd        string `json:"cmd"`
		RemoteHost string `json:"remote_host"`
		SSHPort    string `json:"ssh_port"`
		EnvPrefix  string `json:"env_prefix"`
		HFToken    string `json:"hf_token"`
		GPUs       string `json:"gpus"`
		Platform   string `json:"platform"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Cmd == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "cmd required"})
		return
	}
	sessionName := "theseus-serve-" + uuid.New().String()[:8]
	logDir := "/tmp/theseus-tmux"
	os.MkdirAll(logDir, 0755)
	logFile := logDir + "/" + sessionName + ".log"
	cmd := req.Cmd
	if req.EnvPrefix != "" {
		cmd = req.EnvPrefix + " && " + cmd
	}
	cmd += " 2>&1 | tee " + cookbook.ShellQuote(logFile)
	if req.RemoteHost != "" {
		sshArgs := buildSSHArgs(s.cfg.DataDir, req.RemoteHost, req.SSHPort)
		cmd = sshArgs + " " + cookbook.ShellQuote(cmd)
		if err := runRemoteCmd(cmd, sessionName, logFile); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	} else {
		if err := cookbook.StartTmux(sessionName, cmd, logFile); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionName})
}

func (s *Server) handleCookbookSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !auth.RequireAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		Host    string `json:"host"`
		SSHPort string `json:"ssh_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	sshArgs := buildSSHArgs(s.cfg.DataDir, req.Host, req.SSHPort)
	// Detect platform via uname
	out, err := exec.Command("sh", "-c", sshArgs+" uname -s").Output()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "SSH connection failed: " + err.Error()})
		return
	}
	uname := strings.TrimSpace(strings.ToLower(string(out)))
	platform := "linux"
	switch {
	case strings.Contains(uname, "darwin"):
		platform = "macos"
	case strings.Contains(uname, "mingw"), strings.Contains(uname, "cygwin"), strings.Contains(uname, "windows"):
		platform = "windows"
	case strings.Contains(uname, "android"):
		platform = "termux"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "platform": platform})
}

func buildSSHArgs(dataDir, host, port string) string {
	privKey := dataDir + "/cookbook_id_rsa"
	args := "ssh -o StrictHostKeyChecking=no -o BatchMode=yes"
	if port != "" {
		args += " -p " + port
	}
	if _, err := os.Stat(privKey); err == nil {
		args += " -i " + cookbook.ShellQuote(privKey)
	}
	return args + " " + host
}

func runRemoteCmd(sshCmd, sessionName, logFile string) error {
	return cookbook.StartTmux(sessionName, sshCmd, logFile)
}

func (s *Server) handleCookbookHFLatest(w http.ResponseWriter, r *http.Request) {
	vramGB := r.URL.Query().Get("vram_gb")
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	profile := cookbook.Detect()
	if vramGB != "" {
		if v, err := strconv.Atoi(vramGB); err == nil {
			profile.VRAM = v * 1024
		}
	}
	models, err := cookbook.RecommendModels(profile, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if limit < len(models) {
		models = models[:limit]
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleModelCached(w http.ResponseWriter, r *http.Request) {
	cacheDir := s.cfg.DataDir + "/huggingface"
	if modelDir := r.URL.Query().Get("model_dir"); modelDir != "" {
		cacheDir = modelDir
	}
	entries, _ := os.ReadDir(cacheDir)
	type cachedModel struct {
		RepoID string `json:"repo_id"`
		Status string `json:"status"`
		Size   string `json:"size"`
	}
	models := []cachedModel{}
	for _, e := range entries {
		if e.IsDir() {
			models = append(models, cachedModel{RepoID: e.Name(), Status: "ready", Size: "unknown"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleHWFitSystem(w http.ResponseWriter, r *http.Request) {
	profile := cookbook.Detect()
	writeJSON(w, http.StatusOK, map[string]any{
		"system": map[string]any{
			"gpu_name":    profile.GPUName,
			"gpu_vram_mb": profile.VRAM,
			"ram_mb":      profile.RAM,
		},
	})
}

func (s *Server) handleHWFitModels(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleHWFitImageModels(w http.ResponseWriter, r *http.Request) {
	profile := cookbook.Detect()
	filter := r.URL.Query().Get("q")
	models, err := cookbook.RecommendModels(profile, filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	imageModels := []*cookbook.ModelEntry{}
	for _, m := range models {
		for _, tag := range m.Tags {
			if tag == "image" || tag == "diffusion" || tag == "sdxl" || tag == "flux" {
				imageModels = append(imageModels, m)
				break
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": imageModels})
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
