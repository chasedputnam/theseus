package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"

	"golang.org/x/crypto/bcrypt"
)

func handle2FAStatus(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": mgr.HasTOTP(user)})
	}
}

func handle2FASetup(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := CurrentUser(r)
		if user == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		secret, uri, err := mgr.GenerateTOTP(user)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Use Google Charts QR code API — the frontend just sets it as an img src
		qrURL := "https://chart.googleapis.com/chart?cht=qr&chs=200x200&chl=" + url.QueryEscape(uri)
		writeJSON(w, http.StatusOK, map[string]any{
			"secret":  secret,
			"uri":     uri,
			"qr_code": qrURL,
		})
	}
}

func handle2FAConfirm(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := CurrentUser(r)
		if user == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if !mgr.ValidateTOTP(user, req.Code) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "Invalid code"})
			return
		}
		// Generate backup codes
		backupCodes := make([]string, 8)
		for i := range backupCodes {
			b := make([]byte, 5)
			rand.Read(b)
			backupCodes[i] = hex.EncodeToString(b)
		}
		writeJSON(w, http.StatusOK, map[string]any{"backup_codes": backupCodes})
	}
}

func handle2FADisable(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := CurrentUser(r)
		if user == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		mgr.mu.RLock()
		u, ok := mgr.config.Users[user]
		mgr.mu.RUnlock()
		if !ok || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "Invalid password"})
			return
		}
		if err := mgr.DisableTOTP(user); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
