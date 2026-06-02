package cookbook

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TmuxSession manages a tmux session for long-running processes.
type TmuxSession struct {
	Name    string
	LogFile string
}

// StartDownload starts a model download in a tmux session.
func StartDownload(repoID, filename, cacheDir, hfToken, sessionName string) (*TmuxSession, error) {
	if err := ensureTmux(); err != nil {
		return nil, err
	}
	logDir := filepath.Join(os.TempDir(), "theseus-tmux")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, sessionName+".log")

	var cmd string
	if filename != "" {
		cmd = fmt.Sprintf("huggingface-cli download %s %s --local-dir %s",
			ShellQuote(repoID), ShellQuote(filename), ShellQuote(cacheDir))
	} else {
		cmd = fmt.Sprintf("huggingface-cli download %s --local-dir %s",
			ShellQuote(repoID), ShellQuote(cacheDir))
	}
	if hfToken != "" {
		cmd = fmt.Sprintf("HF_TOKEN=%s %s", ShellQuote(hfToken), cmd)
	}
	cmd += fmt.Sprintf(" 2>&1 | tee %s", ShellQuote(logFile))

	return startTmuxSession(sessionName, cmd, logFile)
}

// StartServe starts a model server in a tmux session.
func StartServe(backend, modelPath, extraArgs, sessionName string) (*TmuxSession, error) {
	if err := ensureTmux(); err != nil {
		return nil, err
	}
	logDir := filepath.Join(os.TempDir(), "theseus-tmux")
	os.MkdirAll(logDir, 0755)
	logFile := filepath.Join(logDir, sessionName+".log")

	var cmd string
	switch backend {
	case "vllm":
		cmd = fmt.Sprintf("python -m vllm.entrypoints.openai.api_server --model %s %s",
			ShellQuote(modelPath), extraArgs)
	case "llama.cpp":
		cmd = fmt.Sprintf("llama-server -m %s %s", ShellQuote(modelPath), extraArgs)
	default:
		return nil, fmt.Errorf("unknown backend: %s", backend)
	}
	cmd += fmt.Sprintf(" 2>&1 | tee %s", ShellQuote(logFile))

	return startTmuxSession(sessionName, cmd, logFile)
}

// TailLog returns the last N lines of a tmux session log.
func TailLog(logFile string, lines int) (string, error) {
	out, err := exec.Command("tail", "-n", fmt.Sprintf("%d", lines), logFile).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// KillSession kills a tmux session.
func KillSession(sessionName string) error {
	return exec.Command("tmux", "kill-session", "-t", sessionName).Run()
}

// IsSessionAlive returns true if the tmux session is running.
func IsSessionAlive(sessionName string) bool {
	err := exec.Command("tmux", "has-session", "-t", sessionName).Run()
	return err == nil
}

func startTmuxSession(name, cmd, logFile string) (*TmuxSession, error) {
	// Kill existing session if any
	exec.Command("tmux", "kill-session", "-t", name).Run()
	time.Sleep(100 * time.Millisecond)

	if err := exec.Command("tmux", "new-session", "-d", "-s", name, cmd).Run(); err != nil {
		return nil, fmt.Errorf("start tmux session: %w", err)
	}
	return &TmuxSession{Name: name, LogFile: logFile}, nil
}

func ensureTmux() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found — install tmux to use Cookbook serving")
	}
	return nil
}

// ShellQuote wraps s in single quotes, escaping any single quotes inside.
func ShellQuote(s string) string {
	// Wrap in single quotes, escaping any single quotes inside
	s = strings.ReplaceAll(s, `'`, `'"'"'`)
	return `'` + s + `'`
}

// StartTmux starts a command in a named tmux session, logging to logFile.
func StartTmux(name, cmd, logFile string) error {
	_, err := startTmuxSession(name, cmd, logFile)
	return err
}
