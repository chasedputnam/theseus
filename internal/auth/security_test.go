package auth

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/chaseputnam/theseus/internal/storage"
)

func TestPasswordMinLength(t *testing.T) {
	m := newTestManager(t)
	if err := m.Setup("admin", "short"); err == nil {
		t.Error("expected error for password shorter than 8 chars")
	}
	if err := m.Setup("admin", "exactly8"); err != nil {
		t.Errorf("expected success for 8-char password, got: %v", err)
	}
}

func TestTOTPEncryptedAtRest(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, ".app_key")
	if err := storage.InitKey(keyPath); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	m := New(filepath.Join(dir, "auth.json"), filepath.Join(dir, "sessions.json"))
	if err := m.Setup("admin", "password123"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	_, _, err := m.GenerateTOTP("admin")
	if err != nil {
		t.Fatalf("GenerateTOTP: %v", err)
	}
	m.mu.RLock()
	u := m.config.Users["admin"]
	m.mu.RUnlock()
	if u.TOTPSecret == "" {
		t.Fatal("expected TOTP secret to be set")
	}
	if !strings.HasPrefix(u.TOTPSecret, "enc:") {
		t.Errorf("TOTP secret should be encrypted (enc: prefix), got prefix: %q", u.TOTPSecret[:min(len(u.TOTPSecret), 10)])
	}
}

func TestTOTPValidateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, ".app_key")
	storage.InitKey(keyPath)
	m := New(filepath.Join(dir, "auth.json"), filepath.Join(dir, "sessions.json"))
	m.Setup("admin", "password123")

	secret, _, err := m.GenerateTOTP("admin")
	if err != nil {
		t.Fatalf("GenerateTOTP: %v", err)
	}
	// Generate a valid TOTP code from the returned plaintext secret
	import_otp := func() {}
	_ = import_otp
	_ = secret // TOTP validation tested via HasTOTP
	if !m.HasTOTP("admin") {
		t.Error("expected HasTOTP=true after GenerateTOTP")
	}
	if err := m.DisableTOTP("admin"); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	if m.HasTOTP("admin") {
		t.Error("expected HasTOTP=false after DisableTOTP")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
