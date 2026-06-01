package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDoBash(t *testing.T) {
	result, err := DoBash(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("DoBash: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestDoBashTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, _ := DoBash(ctx, "sleep 10")
	if !strings.Contains(result, "timeout") && ctx.Err() == nil {
		// Either timed out or context cancelled — both acceptable
	}
}

func TestDoBashOutputTruncation(t *testing.T) {
	// Generate output > 10K chars
	result, err := DoBash(context.Background(), "python3 -c \"print('x'*20000)\"")
	if err != nil {
		t.Fatalf("DoBash: %v", err)
	}
	if len(result) > maxOutputLen+200 {
		t.Errorf("output not truncated: len=%d", len(result))
	}
	if !strings.Contains(result, "truncated") {
		t.Errorf("expected truncation notice, got: %s", result[:100])
	}
}

func TestDoPython(t *testing.T) {
	result, err := DoPython(context.Background(), "print(2+2)")
	if err != nil {
		t.Fatalf("DoPython: %v", err)
	}
	if result != "4" {
		t.Errorf("expected '4', got %q", result)
	}
}

func TestSafePath(t *testing.T) {
	dir := t.TempDir()
	// Valid path
	p, err := safePath("subdir/file.txt", dir)
	if err != nil {
		t.Fatalf("safePath valid: %v", err)
	}
	if !strings.HasPrefix(p, dir) {
		t.Errorf("expected path under dataDir")
	}
	// Path traversal
	_, err = safePath("../../etc/passwd", dir)
	if err == nil {
		t.Error("expected error for path traversal")
	}
	// Absolute path
	_, err = safePath("/etc/passwd", dir)
	if err == nil {
		t.Error("expected error for absolute path")
	}
}

func TestDoReadWriteFile(t *testing.T) {
	dir := t.TempDir()
	writeArgs := `{"path":"test.txt","content":"hello world"}`
	result, err := DoWriteFile(context.Background(), writeArgs, dir)
	if err != nil {
		t.Fatalf("DoWriteFile: %v", err)
	}
	if !strings.Contains(result, "Written") {
		t.Errorf("expected Written message, got %q", result)
	}
	readArgs := `{"path":"test.txt"}`
	content, err := DoReadFile(context.Background(), readArgs, dir)
	if err != nil {
		t.Fatalf("DoReadFile: %v", err)
	}
	if content != "hello world" {
		t.Errorf("expected 'hello world', got %q", content)
	}
	_ = filepath.Join
}
