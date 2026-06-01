package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxReadChars = 20_000

// DoReadFile reads a file relative to dataDir.
func DoReadFile(ctx context.Context, argsJSON string, dataDir string) (string, error) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args (expected JSON with path field): %w", err)
	}
	if args.Path == "" {
		return "", fmt.Errorf("path required")
	}
	safe, err := safePath(args.Path, dataDir)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(safe)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	result := string(data)
	if len(result) > maxReadChars {
		result = result[:maxReadChars] + fmt.Sprintf("\n... (truncated, %d chars total)", len(result))
	}
	return result, nil
}

// DoWriteFile writes content to a file relative to dataDir.
func DoWriteFile(ctx context.Context, argsJSON string, dataDir string) (string, error) {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if args.Path == "" {
		return "", fmt.Errorf("path required")
	}
	safe, err := safePath(args.Path, dataDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(safe, []byte(args.Content), 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fmt.Sprintf("Written %d bytes to %s", len(args.Content), args.Path), nil
}

// safePath resolves path relative to dataDir and ensures it stays within dataDir.
func safePath(path, dataDir string) (string, error) {
	// If absolute, reject
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute paths not allowed")
	}
	resolved := filepath.Join(dataDir, filepath.Clean(path))
	// Ensure it's still under dataDir
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return "", fmt.Errorf("resolve data dir: %w", err)
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if !strings.HasPrefix(absResolved, absData+string(filepath.Separator)) && absResolved != absData {
		return "", fmt.Errorf("path escapes data directory")
	}
	return resolved, nil
}
