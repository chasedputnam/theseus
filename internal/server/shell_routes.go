package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/creack/pty"
)

const (
	shellExecTimeout   = 30 * time.Second
	shellStreamTimeout = 120 * time.Second
	shellMaxOutput     = 200_000
)

func (s *Server) registerShellRoutes() {
	s.mux.HandleFunc("/api/shell/exec", s.withAuth(s.handleShellExec))
	s.mux.HandleFunc("/api/shell/stream", s.withAuth(s.handleShellStream))
	s.mux.HandleFunc("/api/shell/tmux/", s.withAuth(s.handleShellTmuxLog))
}

func requireShellAdmin(mgr interface{ IsAdmin(string) bool }, w http.ResponseWriter, r *http.Request) bool {
	user := auth.CurrentUser(r)
	if user == "internal-tool" || mgr.IsAdmin(user) {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "Admin only"})
	return false
}

func (s *Server) handleShellExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireShellAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"` // seconds, 0 = default
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	timeout := shellExecTimeout
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", req.Command)
	out, err := cmd.CombinedOutput()
	result := string(out)
	if len(result) > shellMaxOutput {
		result = result[:shellMaxOutput] + fmt.Sprintf("\n... (truncated, %d chars total)", len(result))
	}

	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			writeJSON(w, http.StatusGatewayTimeout, map[string]string{
				"error": "Command timed out", "output": result,
			})
			return
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"output":    result,
		"exit_code": exitCode,
	})
}

func (s *Server) handleShellStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireShellAdmin(s.auth, w, r) {
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	sendEvent := func(event, data string) {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), shellStreamTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", req.Command)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		sendEvent("error", fmt.Sprintf(`{"error":%q}`, err.Error()))
		return
	}
	defer ptmx.Close()

	buf := make([]byte, 4096)
	totalBytes := 0
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			totalBytes += n
			if totalBytes > shellMaxOutput {
				sendEvent("output", fmt.Sprintf(`{"data":%q}`, "[output truncated]"))
				break
			}
			d, _ := json.Marshal(map[string]string{"data": string(buf[:n])})
			sendEvent("output", string(d))
		}
		if err != nil {
			if err != io.EOF {
				sendEvent("error", fmt.Sprintf(`{"error":%q}`, err.Error()))
			}
			break
		}
	}
	cmd.Wait()
	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	d, _ := json.Marshal(map[string]any{"status": "done", "exit_code": exitCode})
	sendEvent("done", string(d))
}

func (s *Server) handleShellTmuxLog(w http.ResponseWriter, r *http.Request) {
	if !requireShellAdmin(s.auth, w, r) {
		return
	}
	sessionName := strings.TrimPrefix(r.URL.Path, "/api/shell/tmux/")
	// Validate sessionName to prevent path traversal
	for _, c := range sessionName {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session name"})
			return
		}
	}
	logFile := filepath.Join(os.TempDir(), "theseus-tmux", sessionName+".log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "log not found"})
		return
	}
	content := string(data)
	if len(content) > shellMaxOutput {
		content = content[len(content)-shellMaxOutput:]
	}
	writeJSON(w, http.StatusOK, map[string]string{"log": content})
}
