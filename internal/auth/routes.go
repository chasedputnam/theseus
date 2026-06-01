package auth

import (
	"encoding/json"
	"net/http"
	"time"
)

// RegisterRoutes mounts all auth routes onto mux.
func RegisterRoutes(mux *http.ServeMux, mgr *Manager) {
	mux.HandleFunc("/api/auth/login", handleLogin(mgr))
	mux.HandleFunc("/api/auth/logout", handleLogout(mgr))
	mux.HandleFunc("/api/auth/signup", handleSignup(mgr))
	mux.HandleFunc("/api/auth/setup", handleSetup(mgr))
	mux.HandleFunc("/api/auth/status", handleStatus(mgr))
	mux.HandleFunc("/api/auth/features", handleFeatures())
	mux.HandleFunc("/api/auth/settings", handleAuthSettings(mgr))
	mux.HandleFunc("/api/auth/users", handleUsers(mgr))
	mux.HandleFunc("/api/auth/users/", handleUserOps(mgr))
	mux.HandleFunc("/api/auth/password", handleChangePassword(mgr))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func handleLogin(mgr *Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		if r.Method == http.MethodPost {
			if !RequireAdmin(mgr, w, r) {
				return
			}
			var req struct {
				SignupEnabled *bool `json:"signup_enabled"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
				return
			}
			if req.SignupEnabled != nil {
				mgr.SetSignupEnabled(*req.SignupEnabled)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"signup_enabled": mgr.SignupEnabled(),
		})
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
		// Extract username from path: /api/auth/users/{username}
		username := r.URL.Path[len("/api/auth/users/"):]
		if username == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username required"})
			return
		}
		switch r.Method {
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
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
