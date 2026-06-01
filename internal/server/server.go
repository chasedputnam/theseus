package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/memory"
	"github.com/chaseputnam/theseus/internal/db"
	"github.com/chaseputnam/theseus/internal/settings"
	"github.com/chaseputnam/theseus/internal/storage"
)

// Config holds server startup configuration.
type Config struct {
	Port            int
	DataDir         string
	StaticDir       string
	AuthEnabled     bool
	LocalhostBypass bool
}

// Server is the main HTTP server.
type Server struct {
	cfg    Config
	db     *db.DB
	auth   *auth.Manager
	mux    *http.ServeMux
	memMgr *memory.Manager
}

// New creates and wires up the server.
func New(cfg Config) (*Server, error) {
	keyPath := filepath.Join(cfg.DataDir, ".app_key")
	if err := storage.InitKey(keyPath); err != nil {
		return nil, fmt.Errorf("init key: %w", err)
	}

	settings.Init(
		filepath.Join(cfg.DataDir, "settings.json"),
		filepath.Join(cfg.DataDir, "features.json"),
	)

	dbPath := filepath.Join(cfg.DataDir, "app.db")
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		dbPath = strings.TrimPrefix(dsn, "sqlite:///")
		dbPath = strings.TrimPrefix(dbPath, "sqlite://")
	}
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	authMgr := auth.New(
		filepath.Join(cfg.DataDir, "auth.json"),
		filepath.Join(cfg.DataDir, "sessions.json"),
	)

	if !authMgr.IsConfigured() {
		pass := generatePassword()
		if err := authMgr.Setup("admin", pass); err != nil {
			log.Printf("WARNING: failed to create admin: %v", err)
		} else {
			log.Printf("=== FIRST BOOT: admin password: %s ===", pass)
		}
	}

	chromaHost := os.Getenv("CHROMADB_HOST")
	chromaPort := 8100
	if p := os.Getenv("CHROMADB_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			chromaPort = n
		}
	}
	memMgr := memory.New(database, chromaHost, chromaPort)

	s := &Server{cfg: cfg, db: database, auth: authMgr, mux: http.NewServeMux(), memMgr: memMgr}
	s.registerRoutes()
	return s, nil
}

func (s *Server) registerRoutes() {
	// Auth routes (exempt from auth middleware)
	auth.RegisterRoutes(s.mux, s.auth)

	// Health / version (exempt)
	s.mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok"}`)
	})
	s.mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"version":"1.0.0","runtime":"go"}`)
	})

	// Feature modules
	s.registerSessionRoutes()
	s.registerDocumentRoutes()
	s.registerEndpointRoutes()
	s.registerCompareRoutes()
	s.registerResearchRoutes()
	s.registerNotesRoutes()
	s.registerCalendarRoutes()
	s.registerGalleryRoutes()
	s.registerCookbookRoutes()
	s.registerShellRoutes()
	s.registerTTSRoutes()
	s.registerContactsRoutes()
	s.registerVaultRoutes()
	s.registerWebhookRoutes()
	s.registerBackupRoutes()
	s.registerAdminRoutes()

	// Memory routes
	s.mux.HandleFunc("/api/memory", s.withAuth(s.handleMemory))
	s.mux.HandleFunc("/api/memory/", s.withAuth(s.handleMemoryOps))

	// Chat routes
	s.mux.HandleFunc("/api/chat", s.withAuth(s.handleChat))

	// Static files
	fs := http.FileServer(http.Dir(s.cfg.StaticDir))
	s.mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/static/sw.js" {
			w.Header().Set("Service-Worker-Allowed", "/")
		}
		fs.ServeHTTP(w, r)
	})
	s.mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "login.html"))
	})
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(s.cfg.StaticDir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			http.ServeFile(w, r, path)
			return
		}
		http.ServeFile(w, r, filepath.Join(s.cfg.StaticDir, "index.html"))
	})
}

// Handler returns the http.Handler with all middleware applied.
func (s *Server) Handler() http.Handler {
	exemptPrefixes := []string{
		"/api/chat", "/api/shell/stream", "/api/research",
		"/api/model/download", "/api/model/probe", "/api/model-endpoints",
		"/api/cookbook/setup", "/api/upload", "/api/image",
	}
	authMW := auth.Middleware(s.auth, s.cfg.AuthEnabled, s.cfg.LocalhostBypass)
	return SecurityHeadersMiddleware(
		RequestTimeoutMiddleware(45*time.Second, exemptPrefixes)(
			authMW(s.mux),
		),
	)
}

// withAuth wraps a handler to ensure the user is authenticated.
func (s *Server) withAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if auth.CurrentUser(r) == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"Not authenticated"}`))
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encodeJSON(w, v)
}

func encodeJSON(w http.ResponseWriter, v any) {
	import_json := func() {}
	_ = import_json
	encodeJSONImpl(w, v)
}

func generatePassword() string {
	b := make([]byte, 12)
	storage.RandBytes(b)
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for i, v := range b {
		b[i] = chars[int(v)%len(chars)]
	}
	return string(b)
}
