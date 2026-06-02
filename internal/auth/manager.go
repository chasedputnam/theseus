package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chaseputnam/theseus/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

const tokenTTL = 7 * 24 * time.Hour

// DefaultPrivileges for new non-admin users.
var DefaultPrivileges = map[string]any{
	"can_use_agent":        true,
	"can_use_browser":      true,
	"can_use_bash":         false,
	"can_use_documents":    true,
	"can_use_research":     true,
	"can_generate_images":  true,
	"can_manage_memory":    true,
	"max_messages_per_day": 0,
	"allowed_models":       []string{},
}

// AdminPrivileges grants everything.
var AdminPrivileges = map[string]any{
	"can_use_agent":        true,
	"can_use_browser":      true,
	"can_use_bash":         true,
	"can_use_documents":    true,
	"can_use_research":     true,
	"can_generate_images":  true,
	"can_manage_memory":    true,
	"max_messages_per_day": 0,
	"allowed_models":       []string{},
}

type User struct {
	PasswordHash string         `json:"password_hash"`
	IsAdmin      bool           `json:"is_admin"`
	Privileges   map[string]any `json:"privileges"`
	TOTPSecret   string         `json:"totp_secret,omitempty"`
	Created      float64        `json:"created"`
}

type SessionEntry struct {
	Username string  `json:"username"`
	Expiry   float64 `json:"expiry"`
}

type authConfig struct {
	Users         map[string]User `json:"users"`
	SignupEnabled bool            `json:"signup_enabled"`
}

// Manager handles multi-user auth.
type Manager struct {
	configPath   string
	sessionsPath string
	config       authConfig
	sessions     map[string]SessionEntry
	mu           sync.RWMutex
}

// New creates an AuthManager loading from configPath.
func New(configPath, sessionsPath string) *Manager {
	m := &Manager{
		configPath:   configPath,
		sessionsPath: sessionsPath,
		sessions:     make(map[string]SessionEntry),
	}
	m.load()
	m.loadSessions()
	return m
}

func (m *Manager) load() {
	var cfg authConfig
	if err := storage.ReadJSON(m.configPath, &cfg); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("auth: load config: %v", err)
		}
		cfg = authConfig{Users: make(map[string]User)}
	}
	if cfg.Users == nil {
		cfg.Users = make(map[string]User)
	}
	m.config = cfg
}

func (m *Manager) loadSessions() {
	var sessions map[string]SessionEntry
	if err := storage.ReadJSON(m.sessionsPath, &sessions); err != nil {
		sessions = make(map[string]SessionEntry)
	}
	now := float64(time.Now().Unix())
	pruned := make(map[string]SessionEntry)
	for k, v := range sessions {
		if v.Expiry > now {
			pruned[k] = v
		}
	}
	m.sessions = pruned
}

// saveSessions persists sessions to disk. Caller must NOT hold mu.
func (m *Manager) saveSessions() {
	m.mu.RLock()
	snap := make(map[string]SessionEntry, len(m.sessions))
	for k, v := range m.sessions {
		snap[k] = v
	}
	m.mu.RUnlock()
	m.writeSessionsSnap(snap)
}

// saveSessionsLocked persists sessions to disk. Caller MUST hold mu (any mode).
func (m *Manager) saveSessionsLocked() {
	snap := make(map[string]SessionEntry, len(m.sessions))
	for k, v := range m.sessions {
		snap[k] = v
	}
	m.writeSessionsSnap(snap)
}

func (m *Manager) writeSessionsSnap(snap map[string]SessionEntry) {
	if err := storage.WriteJSON(m.sessionsPath, snap); err != nil {
		log.Printf("auth: save sessions: %v", err)
	}
}

func (m *Manager) save() {
	if err := storage.WriteJSON(m.configPath, m.config); err != nil {
		log.Printf("auth: save config: %v", err)
	}
}



// IsConfigured returns true if at least one user exists.
func (m *Manager) IsConfigured() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.config.Users) > 0
}

// SignupEnabled returns whether open signup is allowed.
func (m *Manager) SignupEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config.SignupEnabled
}

// SetSignupEnabled toggles open signup.
func (m *Manager) SetSignupEnabled(v bool) {
	m.mu.Lock()
	m.config.SignupEnabled = v
	m.mu.Unlock()
	m.save()
}

// Setup creates the first admin user. Only works when no users exist.
func (m *Manager) Setup(username, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.config.Users) > 0 {
		return fmt.Errorf("already configured")
	}
	return m.createUserLocked(username, password, true)
}

// CreateUser creates a new user account.
func (m *Manager) CreateUser(username, password string, isAdmin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.createUserLocked(username, password, isAdmin)
}

func (m *Manager) createUserLocked(username, password string, isAdmin bool) error {
	username = strings.ToLower(strings.TrimSpace(username))
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if _, exists := m.config.Users[username]; exists {
		return fmt.Errorf("user already exists")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	src := DefaultPrivileges
	if isAdmin {
		src = AdminPrivileges
	}
	privs := make(map[string]any, len(src))
	for k, v := range src {
		privs[k] = v
	}
	m.config.Users[username] = User{
		PasswordHash: string(hash),
		IsAdmin:      isAdmin,
		Privileges:   privs,
		Created:      float64(time.Now().Unix()),
	}
	m.save()
	return nil
}

// DeleteUser removes a user and revokes all their sessions.
func (m *Manager) DeleteUser(username, requestingUser string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	username = strings.ToLower(strings.TrimSpace(username))
	if username == requestingUser {
		return fmt.Errorf("cannot delete yourself")
	}
	reqUser, ok := m.config.Users[requestingUser]
	if !ok || !reqUser.IsAdmin {
		return fmt.Errorf("admin only")
	}
	if _, ok := m.config.Users[username]; !ok {
		return fmt.Errorf("user not found")
	}
	delete(m.config.Users, username)
	// Revoke all sessions for deleted user
	for token, sess := range m.sessions {
		if sess.Username == username {
			delete(m.sessions, token)
		}
	}
	m.save()
	m.saveSessionsLocked()
	return nil
}

// Login validates credentials and returns a session token.
func (m *Manager) Login(username, password string) (string, error) {
	m.mu.RLock()
	user, ok := m.config.Users[strings.ToLower(strings.TrimSpace(username))]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", fmt.Errorf("invalid credentials")
	}
	return m.createToken(strings.ToLower(strings.TrimSpace(username)))
}

func (m *Manager) createToken(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	entry := SessionEntry{
		Username: username,
		Expiry:   float64(time.Now().Add(tokenTTL).Unix()),
	}
	m.mu.Lock()
	m.sessions[token] = entry
	m.mu.Unlock()
	m.saveSessions()
	return token, nil
}

// ValidateToken returns the username for a valid token, or "" if invalid/expired.
func (m *Manager) ValidateToken(token string) string {
	m.mu.RLock()
	entry, ok := m.sessions[token]
	m.mu.RUnlock()
	if !ok {
		return ""
	}
	if float64(time.Now().Unix()) > entry.Expiry {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
		m.saveSessions()
		return ""
	}
	return entry.Username
}

// Logout revokes a session token.
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
	m.saveSessions()
}

// InvalidateUserSessions revokes all session tokens for a given username.
func (m *Manager) InvalidateUserSessions(username string) {
	m.mu.Lock()
	for token, entry := range m.sessions {
		if entry.Username == username {
			delete(m.sessions, token)
		}
	}
	m.mu.Unlock()
	m.saveSessions()
}

// IsAdmin returns true if username is an admin.
func (m *Manager) IsAdmin(username string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.config.Users[username]
	return ok && u.IsAdmin
}

// HasPrivilege returns true if the user has the given privilege.
func (m *Manager) HasPrivilege(username, privilege string) bool {
	m.mu.RLock()
	u, ok := m.config.Users[username]
	m.mu.RUnlock()
	if !ok {
		return false
	}
	if u.IsAdmin {
		return true
	}
	v, exists := u.Privileges[privilege]
	if !exists {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	}
	return false
}

// GetUser returns a copy of the user record.
func (m *Manager) GetUser(username string) (User, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.config.Users[username]
	return u, ok
}

// ListUsers returns all usernames.
func (m *Manager) ListUsers() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.config.Users))
	for k := range m.config.Users {
		names = append(names, k)
	}
	return names
}

// UpdatePrivileges replaces a user's privilege map.
func (m *Manager) UpdatePrivileges(username string, privs map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.config.Users[username]
	if !ok {
		return fmt.Errorf("user not found")
	}
	u.Privileges = privs
	m.config.Users[username] = u
	m.save()
	return nil
}

// ChangePassword updates a user's password hash.
func (m *Manager) ChangePassword(username, newPassword string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.config.Users[username]
	if !ok {
		return fmt.Errorf("user not found")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hash)
	m.config.Users[username] = u
	m.save()
	return nil
}

// UsersJSON returns the users map as JSON (without password hashes).
func (m *Manager) UsersJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type safeUser struct {
		Username   string         `json:"username"`
		IsAdmin    bool           `json:"is_admin"`
		Privileges map[string]any `json:"privileges"`
		HasTOTP    bool           `json:"has_totp"`
		Created    float64        `json:"created"`
	}
	users := make([]safeUser, 0, len(m.config.Users))
	for name, u := range m.config.Users {
		users = append(users, safeUser{
			Username:   name,
			IsAdmin:    u.IsAdmin,
			Privileges: u.Privileges,
			HasTOTP:    u.TOTPSecret != "",
			Created:    u.Created,
		})
	}
	return json.Marshal(users)
}
