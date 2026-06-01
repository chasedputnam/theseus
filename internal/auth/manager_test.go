package auth

import (
	"path/filepath"
	"testing"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	return New(
		filepath.Join(dir, "auth.json"),
		filepath.Join(dir, "sessions.json"),
	)
}

func TestSetupAndLogin(t *testing.T) {
	m := newTestManager(t)
	if err := m.Setup("admin", "password123"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if !m.IsConfigured() {
		t.Fatal("expected IsConfigured=true")
	}
	token, err := m.Login("admin", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if got := m.ValidateToken(token); got != "admin" {
		t.Errorf("ValidateToken: got %q, want admin", got)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	m := newTestManager(t)
	m.Setup("admin", "password123")
	if _, err := m.Login("admin", "wrong"); err == nil {
		t.Fatal("expected error for wrong password")
	}
	if _, err := m.Login("nobody", "password123"); err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestLogout(t *testing.T) {
	m := newTestManager(t)
	m.Setup("admin", "password123")
	token, _ := m.Login("admin", "password123")
	m.Logout(token)
	if got := m.ValidateToken(token); got != "" {
		t.Errorf("expected empty after logout, got %q", got)
	}
}

func TestAdminPrivileges(t *testing.T) {
	m := newTestManager(t)
	m.Setup("admin", "password123")
	m.CreateUser("user", "password123", false)

	if !m.IsAdmin("admin") {
		t.Error("admin should be admin")
	}
	if m.IsAdmin("user") {
		t.Error("user should not be admin")
	}
	if !m.HasPrivilege("admin", "can_use_bash") {
		t.Error("admin should have can_use_bash")
	}
	if m.HasPrivilege("user", "can_use_bash") {
		t.Error("user should not have can_use_bash")
	}
	if !m.HasPrivilege("user", "can_use_agent") {
		t.Error("user should have can_use_agent")
	}
}

func TestDeleteUserRevokesSession(t *testing.T) {
	m := newTestManager(t)
	m.Setup("admin", "password123")
	m.CreateUser("bob", "password123", false)
	token, _ := m.Login("bob", "password123")
	if got := m.ValidateToken(token); got != "bob" {
		t.Fatalf("expected bob, got %q", got)
	}
	if err := m.DeleteUser("bob", "admin"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if got := m.ValidateToken(token); got != "" {
		t.Errorf("expected empty after delete, got %q", got)
	}
}

func TestSetupOnlyOnce(t *testing.T) {
	m := newTestManager(t)
	m.Setup("admin", "password123")
	if err := m.Setup("admin2", "password123"); err == nil {
		t.Fatal("expected error on second Setup")
	}
}
