package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const encPrefix = "enc:"

var (
	keyOnce sync.Once
	appKey  []byte
	keyErr  error
)

// InitKey loads or generates the app encryption key at keyPath (mode 0600).
func InitKey(keyPath string) error {
	keyOnce.Do(func() {
		appKey, keyErr = loadOrCreateKey(keyPath)
	})
	return keyErr
}

func loadOrCreateKey(path string) ([]byte, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("key file %s exists but has wrong length %d (expected 32); refusing to overwrite", path, len(data))
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	// Generate new 32-byte key
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if err := os.WriteFile(path, key, 0600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	return key, nil
}

// Encrypt encrypts plaintext with AES-256-GCM and returns "enc:<base64(nonce+ciphertext)>".
// Returns an error if the key has not been initialized.
func Encrypt(plaintext string) (string, error) {
	if appKey == nil {
		return "", fmt.Errorf("encryption key not initialized; call storage.InitKey first")
	}
	block, err := aes.NewCipher(appKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt decrypts a value produced by Encrypt. Passes through plaintext values unchanged.
func Decrypt(ciphertext string) (string, error) {
	if !strings.HasPrefix(ciphertext, encPrefix) {
		return ciphertext, nil // legacy plaintext passthrough
	}
	if appKey == nil {
		return ciphertext, nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, encPrefix))
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	block, err := aes.NewCipher(appKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted reports whether s was produced by Encrypt.
func IsEncrypted(s string) bool {
	return strings.HasPrefix(s, encPrefix)
}
