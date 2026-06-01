package auth

import (
	"fmt"
	"strings"

	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/pquerna/otp/totp"
)

// GenerateTOTP creates a new TOTP secret for the user and returns the provisioning URI.
// The secret is encrypted at rest using storage.Encrypt.
func (m *Manager) GenerateTOTP(username string) (secret, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Theseus",
		AccountName: username,
	})
	if err != nil {
		return "", "", err
	}
	encrypted, err := storage.Encrypt(key.Secret())
	if err != nil {
		return "", "", fmt.Errorf("encrypt TOTP secret: %w", err)
	}
	m.mu.Lock()
	u, ok := m.config.Users[username]
	if !ok {
		m.mu.Unlock()
		return "", "", fmt.Errorf("user not found")
	}
	u.TOTPSecret = encrypted
	m.config.Users[username] = u
	m.mu.Unlock()
	m.save()
	return key.Secret(), key.URL(), nil
}

// ValidateTOTP checks a TOTP code for the user.
func (m *Manager) ValidateTOTP(username, code string) bool {
	m.mu.RLock()
	u, ok := m.config.Users[username]
	m.mu.RUnlock()
	if !ok || u.TOTPSecret == "" {
		return false
	}
	secret, err := storage.Decrypt(u.TOTPSecret)
	if err != nil {
		return false
	}
	return totp.Validate(strings.TrimSpace(code), secret)
}

// HasTOTP returns true if the user has TOTP enabled.
func (m *Manager) HasTOTP(username string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.config.Users[username]
	return ok && u.TOTPSecret != ""
}

// DisableTOTP removes the TOTP secret for the user.
func (m *Manager) DisableTOTP(username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.config.Users[username]
	if !ok {
		return fmt.Errorf("user not found")
	}
	u.TOTPSecret = ""
	m.config.Users[username] = u
	m.save()
	return nil
}
