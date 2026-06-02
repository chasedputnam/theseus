package auth

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
)

// RegisterRoutes mounts all auth routes onto mux.
func RegisterRoutes(mux *http.ServeMux, mgr *Manager) {
	rl := newLoginRateLimiter()
	mux.HandleFunc("/api/auth/login", handleLogin(mgr, rl))
	mux.HandleFunc("/api/auth/logout", handleLogout(mgr))
	mux.HandleFunc("/api/auth/signup", handleSignup(mgr))
	mux.HandleFunc("/api/auth/setup", handleSetup(mgr))
	mux.HandleFunc("/api/auth/status", handleStatus(mgr))
	mux.HandleFunc("/api/auth/features", handleFeatures())
	mux.HandleFunc("/api/auth/settings", handleAuthSettings(mgr))
	mux.HandleFunc("/api/auth/users", handleUsers(mgr))
	mux.HandleFunc("/api/auth/users/", handleUserOps(mgr))
	mux.HandleFunc("/api/auth/password", handleChangePassword(mgr))
	mux.HandleFunc("/api/auth/change-password", handleChangePassword(mgr))
	mux.HandleFunc("/api/auth/signup-toggle", handleSignupToggle(mgr))
	mux.HandleFunc("/api/auth/2fa/status", handle2FAStatus(mgr))
	mux.HandleFunc("/api/auth/2fa/setup", handle2FASetup(mgr))
	mux.HandleFunc("/api/auth/2fa/confirm", handle2FAConfirm(mgr))
	mux.HandleFunc("/api/auth/2fa/disable", handle2FADisable(mgr))
	RegisterIntegrationRoutes(mux, mgr)
}

// loginRateLimiter tracks failed login attempts per IP.
type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{attempts: make(map[string][]time.Time)}
}

func (rl *loginRateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	window := now.Add(-60 * time.Second)
	prev := rl.attempts[ip]
	var recent []time.Time
	for _, t := range prev {
		if t.After(window) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		// No recent attempts — evict this IP entirely to prevent unbounded map growth.
		delete(rl.attempts, ip)
		rl.attempts[ip] = []time.Time{now}
		return true
	}
	if len(recent) >= 10 {
		rl.attempts[ip] = recent
		return false
	}
	rl.attempts[ip] = append(recent, now)
	return true
}

func loginClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.SplitN(fwd, ",", 2)[0]
	}
	return clientIP(r)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleLogin(mgr *Manager, rl *loginRateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !rl.allow(loginClientIP(r)) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests"})
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			TOTPCode string `json:"totp_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		// Validate TOTP if enabled
		if mgr.HasTOTP(req.Username) {
			if !mgr.ValidateTOTP(req.Username, req.TOTPCode) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid 2FA code"})
				return
			}
		}
		token, err := mgr.Login(req.Username, req.Password)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid credentials"})
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(tokenTTL),
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "username": req.Username})
	}
}

func handleLogout(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(SessionCookieName); err == nil {
			mgr.Logout(cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:    SessionCookieName,
			Value:   "",
			Path:    "/",
			MaxAge:  -1,
			Expires: time.Unix(0, 0),
		})
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleSignup(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !mgr.SignupEnabled() {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Signup disabled"})
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if err := mgr.CreateUser(req.Username, req.Password, false); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleSetup(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if err := mgr.Setup(req.Username, req.Password); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleStatus(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated":  user != "",
			"username":       user,
			"is_admin":       mgr.IsAdmin(user),
			"is_configured":  mgr.IsConfigured(),
			"configured":     mgr.IsConfigured(),
			"signup_enabled": mgr.SignupEnabled(),
		})
	}
}

func handleFeatures() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{
			"auth": true,
		})
	}
}

func handleAuthSettings(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		dataDir := filepath.Dir(mgr.configPath)
		settingsFile := dataDir + "/user_settings_" + storage.SanitizeFilename(user) + ".json"

		if r.Method == http.MethodPost {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			// signup_enabled is admin-only
			if _, hasSignup := req["signup_enabled"]; hasSignup {
				if !RequireAdmin(mgr, w, r) {
					return
				}
				if v, ok := req["signup_enabled"].(bool); ok {
					mgr.SetSignupEnabled(v)
				}
				delete(req, "signup_enabled")
			}
			// Certain keys are global settings (search, model defaults)
			globalKeys := map[string]bool{
				"search_provider": true, "search_url": true, "search_result_count": true,
				"brave_api_key": true, "google_pse_key": true, "google_pse_cx": true,
				"tavily_api_key": true, "serper_api_key": true,
				"default_endpoint_id": true, "default_model": true,
			}
			globalUpdates := map[string]any{}
			for k, v := range req {
				if globalKeys[k] {
					globalUpdates[k] = v
				}
			}
			if len(globalUpdates) > 0 {
				current := settings.Load()
				for k, v := range globalUpdates {
					current[k] = v
				}
				settings.Save(current)
			}
			// Merge remaining keys into per-user settings file
			if len(req) > 0 {
				var existing map[string]any
				storage.ReadJSON(settingsFile, &existing)
				if existing == nil {
					existing = map[string]any{}
				}
				for k, v := range req {
					existing[k] = v
				}
				storage.WriteJSON(settingsFile, existing)
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		// GET: merge user settings with admin-level signup_enabled
		var userSettings map[string]any
		storage.ReadJSON(settingsFile, &userSettings)
		if userSettings == nil {
			userSettings = map[string]any{}
		}
		userSettings["signup_enabled"] = mgr.SignupEnabled()
		writeJSON(w, http.StatusOK, userSettings)
	}
}

func handleUsers(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !RequireAdmin(mgr, w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			data, _ := mgr.UsersJSON()
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
		case http.MethodPost:
			var req struct {
				Username string `json:"username"`
				Password string `json:"password"`
				IsAdmin  bool   `json:"is_admin"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			if err := mgr.CreateUser(req.Username, req.Password, req.IsAdmin); err != nil {
				writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handleUserOps(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !RequireAdmin(mgr, w, r) {
			return
		}
		// Extract username from path: /api/auth/users/{username}[/subpath]
		path := r.URL.Path[len("/api/auth/users/"):]
		parts := strings.SplitN(path, "/", 2)
		username := parts[0]
		subpath := ""
		if len(parts) > 1 {
			subpath = parts[1]
		}
		if username == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username required"})
			return
		}
		// Handle /privileges sub-route
		if subpath == "privileges" {
			if r.Method != http.MethodPut && r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			if err := mgr.UpdatePrivileges(username, req); err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			u, ok := mgr.GetUser(username)
			if !ok {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"username": username, "privileges": u.Privileges, "is_admin": u.IsAdmin})
		case http.MethodDelete:
			requestingUser := CurrentUser(r)
			if err := mgr.DeleteUser(username, requestingUser); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		case http.MethodPut:
			var req struct {
				Privileges map[string]any `json:"privileges"`
				IsAdmin    *bool          `json:"is_admin"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			if req.Privileges != nil {
				if err := mgr.UpdatePrivileges(username, req.Privileges); err != nil {
					writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
					return
				}
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handleSignupToggle(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !RequireAdmin(mgr, w, r) {
			return
		}
		mgr.SetSignupEnabled(!mgr.SignupEnabled())
		writeJSON(w, http.StatusOK, map[string]bool{"signup_enabled": mgr.SignupEnabled()})
	}
}

func handleChangePassword(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user := CurrentUser(r)
		var req struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		// Verify current password
		if _, err := mgr.Login(user, req.CurrentPassword); err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid current password"})
			return
		}
		if err := mgr.ChangePassword(user, req.NewPassword); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		mgr.InvalidateUserSessions(user)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
