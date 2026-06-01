package auth

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type contextKey string

const UserKey contextKey = "current_user"

const SessionCookieName = "session_token"
const InternalToolHeader = "X-Theseus-Internal"

// internalToolToken is set once at startup and acts as a startup-time constant.
var internalToolToken string

func SetInternalToken(t string) { internalToolToken = t }
func GetInternalToken() string  { return internalToolToken }

var authExemptExact = map[string]bool{
	"/api/auth/setup":                true,
	"/api/auth/signup":               true,
	"/api/auth/login":                true,
	"/api/auth/logout":               true,
	"/api/auth/status":               true,
	"/api/auth/features":             true,
	"/api/auth/settings":             true,
	"/api/auth/integrations/presets": true,
	"/api/health":                    true,
	"/api/version":                   true,
	"/login":                         true,
}

var authExemptPrefixes = []string{"/static"}

func isAuthExempt(path string) bool {
	if authExemptExact[path] {
		return true
	}
	for _, p := range authExemptPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isLoopback(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1"
}

// Middleware returns an http.Handler that enforces authentication.
func Middleware(mgr *Manager, authEnabled, localhostBypass bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !authEnabled {
				ctx := context.WithValue(r.Context(), UserKey, "admin")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			path := r.URL.Path
			if isAuthExempt(path) {
				next.ServeHTTP(w, r)
				return
			}

			// Internal tool loopback bypass
			if internalToolToken != "" {
				hdr := r.Header.Get(InternalToolHeader)
				ip := clientIP(r)
				if hdr == internalToolToken && isLoopback(ip) {
					owner := r.Header.Get("X-Theseus-Owner")
					if owner == "" {
						owner = "internal-tool"
					}
					ctx := context.WithValue(r.Context(), UserKey, owner)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Localhost bypass (dev mode)
			if localhostBypass && isLoopback(clientIP(r)) {
				ctx := context.WithValue(r.Context(), UserKey, "admin")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Not configured yet
			if !mgr.IsConfigured() {
				if !strings.HasPrefix(path, "/api/") {
					http.Redirect(w, r, "/login", http.StatusFound)
					return
				}
				http.Error(w, `{"error":"Setup required"}`, http.StatusUnauthorized)
				return
			}

			// Try Bearer token
			if bearer := r.Header.Get("Authorization"); strings.HasPrefix(bearer, "Bearer ") {
				token := strings.TrimPrefix(bearer, "Bearer ")
				if username := mgr.ValidateToken(token); username != "" {
					ctx := context.WithValue(r.Context(), UserKey, username)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"Invalid token"}`))
				return
			}

			// Try session cookie
			if cookie, err := r.Cookie(SessionCookieName); err == nil {
				if username := mgr.ValidateToken(cookie.Value); username != "" {
					ctx := context.WithValue(r.Context(), UserKey, username)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Unauthenticated
			if strings.HasPrefix(path, "/api/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"Not authenticated"}`))
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
		})
	}
}

// CurrentUser extracts the authenticated username from context.
func CurrentUser(r *http.Request) string {
	v, _ := r.Context().Value(UserKey).(string)
	return v
}

// RequireAdmin returns false and writes 403 if the user is not an admin.
func RequireAdmin(mgr *Manager, w http.ResponseWriter, r *http.Request) bool {
	user := CurrentUser(r)
	if user == "internal-tool" || mgr.IsAdmin(user) {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"error":"Admin only"}`))
	return false
}
