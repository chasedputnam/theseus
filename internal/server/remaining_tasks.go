package server

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chaseputnam/theseus/internal/auth"
	"github.com/chaseputnam/theseus/internal/storage"
	"github.com/google/uuid"
)

// ── Task 17: Presets routes ──────────────────────────────────────────────────

func (s *Server) presetsCustomFile(user string) string {
	return s.cfg.DataDir + "/presets_custom_" + sanitizeFilename(user) + ".json"
}

func (s *Server) handlePresetsCustom(w http.ResponseWriter, r *http.Request) {
	user := auth.CurrentUser(r)
	file := s.presetsCustomFile(user)
	id := strings.TrimPrefix(r.URL.Path, "/api/presets/custom/")
	id = strings.TrimSuffix(id, "/")

	switch r.Method {
	case http.MethodGet:
		var presets []map[string]any
		storage.ReadJSON(file, &presets)
		if presets == nil {
			presets = []map[string]any{}
		}
		writeJSON(w, http.StatusOK, presets)
	case http.MethodPost:
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		if req["id"] == nil {
			req["id"] = uuid.New().String()
		}
		var presets []map[string]any
		storage.ReadJSON(file, &presets)
		presets = append(presets, req)
		storage.WriteJSON(file, presets)
		writeJSON(w, http.StatusOK, req)
	case http.MethodDelete:
		var presets []map[string]any
		storage.ReadJSON(file, &presets)
		updated := presets[:0]
		for _, p := range presets {
			if fmt.Sprint(p["id"]) != id {
				updated = append(updated, p)
			}
		}
		storage.WriteJSON(file, updated)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePresetsExpand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Template  string            `json:"template"`
		Variables map[string]string `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	result := req.Template
	for k, v := range req.Variables {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}
	writeJSON(w, http.StatusOK, map[string]string{"expanded": result})
}

var builtinTemplates = []map[string]any{
	{"id": "summarize", "name": "Summarize", "template": "Summarize the following:\n\n{{content}}"},
	{"id": "explain", "name": "Explain", "template": "Explain the following in simple terms:\n\n{{content}}"},
	{"id": "translate", "name": "Translate", "template": "Translate the following to {{language}}:\n\n{{content}}"},
	{"id": "improve", "name": "Improve Writing", "template": "Improve the writing of the following:\n\n{{content}}"},
	{"id": "bullet", "name": "Bullet Points", "template": "Convert the following to bullet points:\n\n{{content}}"},
}

func (s *Server) handlePresetsTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/presets/templates/")
	id = strings.TrimSuffix(id, "/")
	if id != "" {
		for _, t := range builtinTemplates {
			if fmt.Sprint(t["id"]) == id {
				writeJSON(w, http.StatusOK, t)
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, builtinTemplates)
}

// ── Task 18: Gallery advanced routes ────────────────────────────────────────

func (s *Server) handleGalleryDownloadZip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	user := auth.CurrentUser(r)
	imgs, err := s.db.ListGalleryImages(user, "", 10000, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="gallery.zip"`)
	zw := zip.NewWriter(w)
	defer zw.Close()
	uploadDir := s.cfg.DataDir + "/generated_images"
	for _, img := range imgs {
		fh, err := os.Open(uploadDir + "/" + img.Filename)
		if err != nil {
			continue
		}
		f, err := zw.Create(img.Filename)
		if err != nil {
			fh.Close()
			continue
		}
		io.Copy(f, fh)
		fh.Close()
	}
}

// ── Task 19: Upload quota enforcement ────────────────────────────────────────

const uploadQuotaBytes int64 = 500 << 20 // 500 MB

func (s *Server) totalUploadSize(user string) int64 {
	all := s.loadUploadsMeta(user)
	var total int64
	for _, m := range all {
		total += m.Size
	}
	return total
}

// ── Task 22: Structured request logging middleware ───────────────────────────

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.status = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := lrw.ResponseWriter.Write(b)
	lrw.bytes += n
	return n, err
}

// LoggingMiddleware logs method, path, status, duration, and user after each request.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)
		user := auth.CurrentUser(r)
		log.Printf("method=%s path=%s status=%d bytes=%d duration=%s user=%s",
			r.Method, r.URL.Path, lrw.status, lrw.bytes, time.Since(start), user)
	})
}

// ── Task 23: Health check DB connectivity ────────────────────────────────────

func (s *Server) handleHealthWithDB(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := s.db.Ping(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintf(w, `{"status":"degraded","db":%q}`, err.Error())
		return
	}
	fmt.Fprintln(w, `{"status":"ok","db":"ok"}`)
}

// ── Task 21: Graceful shutdown ───────────────────────────────────────────────

// RunWithGracefulShutdown starts the server and shuts down gracefully on SIGTERM/SIGINT.
func RunWithGracefulShutdown(addr string, handler http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: handler}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-quit:
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		shutErr := srv.Shutdown(ctx)
		// Wait for background research goroutines to finish (up to remaining deadline)
		waitCh := make(chan struct{})
		go func() {
			researchWG.Wait()
			close(waitCh)
		}()
		select {
		case <-waitCh:
		case <-ctx.Done():
		}
		return shutErr
	}
}

// suppress unused import
