package tools

import (
	"github.com/chaseputnam/theseus/internal/agent"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafePathTraversal(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		path    string
		wantErr bool
	}{
		{"subdir/file.txt", false},
		{"../../etc/passwd", true},
		{"/etc/passwd", true},
		{"../outside", true},
		{"valid/nested/path.txt", false},
	}
	for _, tc := range cases {
		_, err := safePath(tc.path, dir)
		if tc.wantErr && err == nil {
			t.Errorf("safePath(%q) expected error, got nil", tc.path)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("safePath(%q) unexpected error: %v", tc.path, err)
		}
	}
}

func TestDoReadFileRequiresJSON(t *testing.T) {
	dir := t.TempDir()
	// Plain string (no JSON) should now return an error
	_, err := DoReadFile(context.Background(), "somefile.txt", dir)
	if err == nil {
		t.Error("expected error for non-JSON args")
	}
	if !strings.Contains(err.Error(), "JSON") && !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected JSON parse error, got: %v", err)
	}
}

func TestDispatcherPrivilegeCheck(t *testing.T) {
	d := New(&Deps{DataDir: t.TempDir()})
	// User without can_use_bash should be blocked
	privs := map[string]any{"can_use_bash": false}
	_, err := d.Execute(context.Background(), agent.ToolBlock{ToolType: "bash", Content: "echo hi"}, "user", privs)
	if err == nil {
		t.Error("expected permission denied for bash without can_use_bash")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected permission denied error, got: %v", err)
	}
}

func TestDispatcherBashAllowedWithPrivilege(t *testing.T) {
	d := New(&Deps{DataDir: t.TempDir()})
	privs := map[string]any{"can_use_bash": true}
	result, err := d.Execute(context.Background(), agent.ToolBlock{ToolType: "bash", Content: "echo hello"}, "user", privs)
	if err != nil {
		t.Fatalf("Execute bash: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in result, got: %q", result)
	}
}

func TestBashTimeoutReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	// Context already expired
	<-ctx.Done()
	_, err := DoBash(ctx, "echo hi")
	if err == nil {
		t.Error("expected error for expired context")
	}
}

var _ = filepath.Join // keep import
