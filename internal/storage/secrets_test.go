package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := InitKey(filepath.Join(dir, ".app_key")); err != nil {
		t.Fatalf("InitKey: %v", err)
	}

	cases := []string{"hello world", "", "special chars: !@#$%^&*()", "unicode: 日本語"}
	for _, tc := range cases {
		enc, err := Encrypt(tc)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", tc, err)
		}
		if !IsEncrypted(enc) {
			t.Errorf("expected enc: prefix for %q, got %q", tc, enc)
		}
		dec, err := Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", enc, err)
		}
		if dec != tc {
			t.Errorf("round-trip mismatch: got %q, want %q", dec, tc)
		}
	}
}

func TestDecryptLegacyPlaintext(t *testing.T) {
	dir := t.TempDir()
	if err := InitKey(filepath.Join(dir, ".app_key")); err != nil {
		t.Fatalf("InitKey: %v", err)
	}
	plain := "legacy-password"
	got, err := Decrypt(plain)
	if err != nil {
		t.Fatalf("Decrypt plaintext: %v", err)
	}
	if got != plain {
		t.Errorf("expected passthrough %q, got %q", plain, got)
	}
}

func TestAtomicWriteJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	data := map[string]string{"key": "value"}
	if err := WriteJSON(path, data); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	// Verify file exists and no .tmp left behind
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not found: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp file should not exist")
	}
	var out map[string]string
	if err := ReadJSON(path, &out); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if out["key"] != "value" {
		t.Errorf("expected value, got %q", out["key"])
	}
}
